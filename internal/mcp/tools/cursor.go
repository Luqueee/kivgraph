package tools

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	// CursorFormatVersion changes whenever the encoded cursor body changes.
	// Existing cursors then fail closed as CURSOR_INVALID instead of being
	// interpreted under a different layout.
	CursorFormatVersion uint8 = 1

	// SortingVersionStableKeyV1 identifies the current deterministic ordering
	// used for exact symbol result pages: ascending stable key.
	SortingVersionStableKeyV1 = "stable-key-v1"

	maxSortingVersionLength = 128
)

// Cursor is the validated state needed to resume one immutable query page.
// The wire representation is deliberately opaque; callers should construct it
// with NewCursor and exchange the value returned by Encode.
type Cursor struct {
	SnapshotID     uint64
	QueryHash      string
	Offset         int
	SortingVersion string
	Checksum       string
}

// cursorBody is the versioned, checksum-covered portion of Cursor. Its field
// order is part of CursorFormatVersion because encoding/json preserves struct
// field order when marshaling.
type cursorBody struct {
	Version        uint8  `json:"version"`
	SnapshotID     uint64 `json:"snapshot_id"`
	QueryHash      string `json:"query_hash"`
	Offset         int    `json:"offset"`
	SortingVersion string `json:"sorting_version"`
}

// cursorWire uses pointers so decoding can distinguish a missing required
// field from a valid zero value such as snapshot_id=0 or offset=0.
type cursorWire struct {
	Version        *uint8  `json:"version"`
	SnapshotID     *uint64 `json:"snapshot_id"`
	QueryHash      *string `json:"query_hash"`
	Offset         *int    `json:"offset"`
	SortingVersion *string `json:"sorting_version"`
	Checksum       *string `json:"checksum"`
}

// HashQuery computes the lowercase SHA-256 hash of the canonical JSON form of
// a query identity. The identity must include the tool name and every argument
// that can affect result membership or ordering.
func HashQuery(query any) (string, error) {
	encoded, err := json.Marshal(query)
	if err != nil {
		return "", WrapToolError(CodeInvalidArgument, "query cannot be hashed", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// NewCursor validates the query and pagination identity and computes its
// checksum.
func NewCursor(snapshotID uint64, queryHash string, offset int, sortingVersion string) (Cursor, error) {
	cursor := Cursor{
		SnapshotID:     snapshotID,
		QueryHash:      queryHash,
		Offset:         offset,
		SortingVersion: sortingVersion,
	}
	if err := cursor.validate(false); err != nil {
		return Cursor{}, err
	}
	cursor.Checksum = cursor.checksum()
	return cursor, nil
}

// Encode returns the URL-safe, unpadded opaque cursor token.
func (cursor Cursor) Encode() (string, error) {
	if err := cursor.validate(true); err != nil {
		return "", err
	}
	version := CursorFormatVersion
	snapshotID := cursor.SnapshotID
	queryHash := cursor.QueryHash
	offset := cursor.Offset
	sortingVersion := cursor.SortingVersion
	checksum := cursor.Checksum
	wire, err := json.Marshal(cursorWire{
		Version:        &version,
		SnapshotID:     &snapshotID,
		QueryHash:      &queryHash,
		Offset:         &offset,
		SortingVersion: &sortingVersion,
		Checksum:       &checksum,
	})
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(wire), nil
}

// DecodeCursor validates and decodes one opaque cursor token.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, invalidCursor("cursor is empty")
	}
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, invalidCursor("cursor is not valid base64url")
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire cursorWire
	if err := decoder.Decode(&wire); err != nil {
		return Cursor{}, invalidCursor("cursor payload is not valid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Cursor{}, invalidCursor("cursor payload contains trailing data")
	}

	cursor, err := cursorFromWire(wire)
	if err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}

// ValidateAgainst checks that a decoded cursor belongs to the active
// snapshot, query and sorting contract. A snapshot mismatch is distinct from
// a malformed or cross-query cursor so callers can report expiration and ask
// the client to restart pagination.
func (cursor Cursor) ValidateAgainst(snapshotID uint64, queryHash, sortingVersion string) error {
	if err := cursor.validate(true); err != nil {
		return err
	}
	if !validQueryHash(queryHash) || !validSortingVersion(sortingVersion) {
		return invalidCursor("active query identity is invalid")
	}
	if cursor.SnapshotID != snapshotID {
		return NewToolError(CodeCursorSnapshotExpired, "cursor belongs to an older snapshot")
	}
	if cursor.QueryHash != queryHash || cursor.SortingVersion != sortingVersion {
		return invalidCursor("cursor does not match the active query")
	}
	return nil
}

func cursorFromWire(wire cursorWire) (Cursor, error) {
	if wire.Version == nil || wire.SnapshotID == nil || wire.QueryHash == nil || wire.Offset == nil || wire.SortingVersion == nil || wire.Checksum == nil {
		return Cursor{}, invalidCursor("cursor payload is missing a required field")
	}
	if *wire.Version != CursorFormatVersion {
		return Cursor{}, invalidCursor("cursor format version is unsupported")
	}
	cursor := Cursor{
		SnapshotID:     *wire.SnapshotID,
		QueryHash:      *wire.QueryHash,
		Offset:         *wire.Offset,
		SortingVersion: *wire.SortingVersion,
		Checksum:       *wire.Checksum,
	}
	if err := cursor.validate(true); err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}

func (cursor Cursor) body() cursorBody {
	return cursorBody{
		Version:        CursorFormatVersion,
		SnapshotID:     cursor.SnapshotID,
		QueryHash:      cursor.QueryHash,
		Offset:         cursor.Offset,
		SortingVersion: cursor.SortingVersion,
	}
}

func (cursor Cursor) checksum() string {
	encoded, _ := json.Marshal(cursor.body())
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (cursor Cursor) validate(requireChecksum bool) error {
	if !validQueryHash(cursor.QueryHash) {
		return invalidCursor("query_hash must be a lowercase SHA-256 hex digest")
	}
	if cursor.Offset < 0 {
		return invalidCursor("offset must not be negative")
	}
	if !validSortingVersion(cursor.SortingVersion) {
		return invalidCursor("sorting_version is invalid")
	}
	if !requireChecksum {
		return nil
	}
	if !validQueryHash(cursor.Checksum) {
		return invalidCursor("checksum must be a lowercase SHA-256 hex digest")
	}
	if !hmac.Equal([]byte(cursor.Checksum), []byte(cursor.checksum())) {
		return invalidCursor("cursor checksum does not match its contents")
	}
	return nil
}

func validQueryHash(value string) bool {
	return validSHA256Hex(value)
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSortingVersion(value string) bool {
	return value != "" && len(value) <= maxSortingVersionLength && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t")
}

func invalidCursor(message string) error {
	return NewToolError(CodeCursorInvalid, message)
}
