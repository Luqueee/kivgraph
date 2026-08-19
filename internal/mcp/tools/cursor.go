package tools

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
)

const (
	// CursorFormatVersion changes whenever the encoded cursor body changes.
	// Existing cursors then fail closed as CURSOR_INVALID instead of being
	// interpreted under a different layout. Version 2 replaced the
	// base64-wrapped JSON object with the binary body below, which costs about
	// a tenth of the tokens on every truncated page.
	CursorFormatVersion uint8 = 2

	// SortingVersionStableKeyV1 identifies the current deterministic ordering
	// used for exact symbol result pages: ascending stable key.
	SortingVersionStableKeyV1 = "stable-key-v1"

	maxSortingVersionLength = 128

	// cursorQueryDigestLength and cursorSortingDigestLength are the bytes the
	// body spends on the query identity, and cursorChecksumLength the bytes it
	// spends on integrity. See cursorIdentity for why truncating is enough.
	cursorQueryDigestLength   = 8
	cursorSortingDigestLength = 4
	cursorChecksumLength      = 8

	cursorIdentityLength = cursorQueryDigestLength + cursorSortingDigestLength

	// minCursorBodyLength and maxCursorBodyLength bound the decoder before it
	// allocates or scans: one version byte, two uvarints, the identity digests
	// and the checksum.
	minCursorBodyLength = 1 + 2 + cursorIdentityLength + cursorChecksumLength
	maxCursorBodyLength = 1 + 2*binary.MaxVarintLen64 + cursorIdentityLength + cursorChecksumLength

	// maxCursorOffset keeps a decoded offset inside int on every platform and
	// stays orders of magnitude above any page a caller can walk to.
	maxCursorOffset = math.MaxInt32
)

// Cursor is the validated state needed to resume one immutable query page.
// The wire representation is deliberately opaque; callers should construct it
// with NewCursor and exchange the value returned by Encode.
//
// QueryHash and SortingVersion are the identity a caller passes in. They are
// not on the wire, so a cursor returned by DecodeCursor leaves them empty and
// carries only the digests that ValidateAgainst compares.
type Cursor struct {
	SnapshotID     uint64
	QueryHash      string
	Offset         int
	SortingVersion string
	Checksum       string

	identity cursorIdentity
}

// cursorIdentity is the part of the query contract that survives encoding: the
// leading bytes of the query hash and a digest of the sorting version. Spelling
// the whole SHA-256 hex hash and the sorting version string on the wire cost
// 221 tokens per truncated page; these 12 bytes cost nothing worth counting.
//
// This trades a 256-bit mismatch check for a 64-bit one on the query and a
// 32-bit one on the ordering contract. That is enough because the cursor is an
// anti-mixup check, not a credential: the digests are not secret, they never
// authorize anything, and the only thing they must prevent is resuming page two
// of one query inside a different query. A caller gains nothing from forging a
// match, and an accidental collision across the handful of cursors alive at
// once is unreachable.
type cursorIdentity struct {
	query   [cursorQueryDigestLength]byte
	sorting [cursorSortingDigestLength]byte
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
	if !validQueryHash(queryHash) {
		return Cursor{}, invalidCursor("query_hash must be a lowercase SHA-256 hex digest")
	}
	if offset < 0 {
		return Cursor{}, invalidCursor("offset must not be negative")
	}
	if offset > maxCursorOffset {
		return Cursor{}, invalidCursor("offset is out of range")
	}
	if !validSortingVersion(sortingVersion) {
		return Cursor{}, invalidCursor("sorting_version is invalid")
	}
	cursor := Cursor{
		SnapshotID:     snapshotID,
		QueryHash:      queryHash,
		Offset:         offset,
		SortingVersion: sortingVersion,
		identity:       newCursorIdentity(queryHash, sortingVersion),
	}
	cursor.Checksum = cursor.checksum()
	return cursor, nil
}

// Encode returns the URL-safe, unpadded opaque cursor token, 31 characters for
// a live snapshot id and a reachable offset.
func (cursor Cursor) Encode() (string, error) {
	if err := cursor.validate(); err != nil {
		return "", err
	}
	body := cursor.checksummedPrefix()
	digest := sha256.Sum256(body)
	// checksummedPrefix reserves maxCursorBodyLength, so this never reallocates.
	body = append(body, digest[:cursorChecksumLength]...)
	return base64.RawURLEncoding.EncodeToString(body), nil
}

// DecodeCursor validates and decodes one opaque cursor token. Every malformed
// input answers CURSOR_INVALID and never panics: bad base64, an unsupported
// version, a truncated or overlong buffer, a broken varint, trailing bytes, a
// wrong checksum.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, invalidCursor("cursor is empty")
	}
	if base64.RawURLEncoding.DecodedLen(len(token)) > maxCursorBodyLength {
		return Cursor{}, invalidCursor("cursor payload is too long")
	}
	body, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, invalidCursor("cursor is not valid base64url")
	}
	if len(body) < minCursorBodyLength {
		return Cursor{}, invalidCursor("cursor payload is truncated")
	}
	if body[0] != CursorFormatVersion {
		return Cursor{}, invalidCursor("cursor format version is unsupported")
	}

	rest := body[1:]
	snapshotID, read := readCanonicalUvarint(rest)
	if read == 0 {
		return Cursor{}, invalidCursor("cursor snapshot id is malformed")
	}
	rest = rest[read:]
	offset, read := readCanonicalUvarint(rest)
	if read == 0 {
		return Cursor{}, invalidCursor("cursor offset is malformed")
	}
	rest = rest[read:]
	// The layout after the varints is fixed, so an exact length is what
	// rejects both a short buffer and trailing bytes.
	if len(rest) != cursorIdentityLength+cursorChecksumLength {
		return Cursor{}, invalidCursor("cursor payload length is invalid")
	}
	if offset > maxCursorOffset {
		return Cursor{}, invalidCursor("cursor offset is out of range")
	}

	cursor := Cursor{SnapshotID: snapshotID, Offset: int(offset)}
	copy(cursor.identity.query[:], rest)
	copy(cursor.identity.sorting[:], rest[cursorQueryDigestLength:])
	cursor.Checksum = hex.EncodeToString(rest[cursorIdentityLength:])
	if err := cursor.validate(); err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}

// ValidateAgainst checks that a decoded cursor belongs to the active
// snapshot, query and sorting contract. A snapshot mismatch is distinct from
// a malformed or cross-query cursor so callers can report expiration and ask
// the client to restart pagination.
func (cursor Cursor) ValidateAgainst(snapshotID uint64, queryHash, sortingVersion string) error {
	if err := cursor.validate(); err != nil {
		return err
	}
	if !validQueryHash(queryHash) || !validSortingVersion(sortingVersion) {
		return invalidCursor("active query identity is invalid")
	}
	if cursor.SnapshotID != snapshotID {
		return NewToolError(CodeCursorSnapshotExpired, "cursor belongs to an older snapshot")
	}
	if cursor.identity != newCursorIdentity(queryHash, sortingVersion) {
		return invalidCursor("cursor does not match the active query")
	}
	return nil
}

// newCursorIdentity derives the wire identity. queryHash is already a SHA-256
// hex digest, so its bytes are a prefix of that digest rather than a second
// hash of its text; a sorting version is free text and is hashed.
func newCursorIdentity(queryHash, sortingVersion string) cursorIdentity {
	var identity cursorIdentity
	// Callers validate queryHash as full-length lowercase hex before reaching
	// here, so this cannot fail; a short read would leave zero bytes and only
	// ever cause a mismatch.
	_, _ = hex.Decode(identity.query[:], []byte(queryHash[:cursorQueryDigestLength*2]))
	sorting := sha256.Sum256([]byte(sortingVersion))
	copy(identity.sorting[:], sorting[:])
	return identity
}

// checksummedPrefix returns the bytes the checksum covers: the format version,
// the pagination position and the identity digests. Uvarint encoding is
// canonical, so the same cursor always produces the same bytes and Encode is
// deterministic.
func (cursor Cursor) checksummedPrefix() []byte {
	prefix := make([]byte, 0, maxCursorBodyLength)
	prefix = append(prefix, CursorFormatVersion)
	prefix = binary.AppendUvarint(prefix, cursor.SnapshotID)
	prefix = binary.AppendUvarint(prefix, uint64(cursor.Offset))
	prefix = append(prefix, cursor.identity.query[:]...)
	prefix = append(prefix, cursor.identity.sorting[:]...)
	return prefix
}

// readCanonicalUvarint returns the value and the bytes it consumed, or a zero
// length for anything binary.Uvarint would accept loosely. Rejecting a
// non-minimal spelling - a trailing 0x00 continuation - keeps one page
// addressable by exactly one token, so Encode and DecodeCursor stay inverse.
func readCanonicalUvarint(buffer []byte) (uint64, int) {
	value, read := binary.Uvarint(buffer)
	if read <= 0 || (read > 1 && buffer[read-1] == 0) {
		return 0, 0
	}
	return value, read
}

func (cursor Cursor) checksum() string {
	digest := sha256.Sum256(cursor.checksummedPrefix())
	return hex.EncodeToString(digest[:cursorChecksumLength])
}

// validate accepts only cursors produced by NewCursor or DecodeCursor: the
// checksum covers the identity digests, which are unexported, so a Cursor
// assembled field by field outside this file cannot pass.
func (cursor Cursor) validate() error {
	if cursor.Offset < 0 || cursor.Offset > maxCursorOffset {
		return invalidCursor("offset is out of range")
	}
	if !validLowercaseHex(cursor.Checksum, cursorChecksumLength) {
		return invalidCursor("checksum must be a lowercase hex digest")
	}
	if !hmac.Equal([]byte(cursor.Checksum), []byte(cursor.checksum())) {
		return invalidCursor("cursor checksum does not match its contents")
	}
	return nil
}

func validQueryHash(value string) bool {
	return validLowercaseHex(value, sha256.Size)
}

func validLowercaseHex(value string, size int) bool {
	if len(value) != size*2 || value != strings.ToLower(value) {
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
