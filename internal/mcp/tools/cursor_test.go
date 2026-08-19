package tools

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// decodeToken returns the raw binary body behind an encoded cursor so tests can
// tamper with the layout instead of with a JSON object.
func decodeToken(t *testing.T, token string) []byte {
	t.Helper()
	body, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("base64 decode(%q) error = %v", token, err)
	}
	return body
}

func newTestCursor(t *testing.T, snapshotID uint64, query any, offset int) (Cursor, string) {
	t.Helper()
	queryHash, err := HashQuery(query)
	if err != nil {
		t.Fatalf("HashQuery(%v) error = %v", query, err)
	}
	cursor, err := NewCursor(snapshotID, queryHash, offset, SortingVersionStableKeyV1)
	if err != nil {
		t.Fatalf("NewCursor() error = %v", err)
	}
	return cursor, queryHash
}

func TestHashQueryIsDeterministicForEquivalentJSONObjects(t *testing.T) {
	first, err := HashQuery(map[string]any{"tool": "find_symbol", "name": "Widget", "limit": 50})
	if err != nil {
		t.Fatalf("HashQuery(first) error = %v", err)
	}
	second, err := HashQuery(map[string]any{"limit": 50, "name": "Widget", "tool": "find_symbol"})
	if err != nil {
		t.Fatalf("HashQuery(second) error = %v", err)
	}
	if first != second {
		t.Fatalf("equivalent query hashes differ: %q != %q", first, second)
	}
	changed, err := HashQuery(map[string]any{"tool": "find_symbol", "name": "Other", "limit": 50})
	if err != nil {
		t.Fatalf("HashQuery(changed) error = %v", err)
	}
	if changed == first {
		t.Fatal("different queries produced the same hash")
	}
	if len(first) != 64 || first != strings.ToLower(first) {
		t.Fatalf("query hash = %q, want lowercase SHA-256 hex", first)
	}
}

func TestHashQueryRejectsUnmarshallableInput(t *testing.T) {
	_, err := HashQuery(func() {})
	if ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("ErrorCode(HashQuery(function)) = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
}

func TestCursorRoundTripIsOpaqueAndDeterministic(t *testing.T) {
	want, queryHash := newTestCursor(t, 42, map[string]any{"tool": "find_symbol", "name": "Widget", "limit": 50}, 50)
	first, err := want.Encode()
	if err != nil {
		t.Fatalf("Cursor.Encode() error = %v", err)
	}
	second, err := want.Encode()
	if err != nil {
		t.Fatalf("Cursor.Encode() second error = %v", err)
	}
	if first != second {
		t.Fatalf("same cursor encoded differently: %q != %q", first, second)
	}
	if strings.ContainsAny(first, "+/=\n\r") {
		t.Fatalf("cursor = %q, want unpadded URL-safe base64", first)
	}
	if len(first) >= 50 {
		t.Fatalf("cursor = %q (%d chars), want a body under 50 characters", first, len(first))
	}

	got, err := DecodeCursor(first)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	// The body carries digests, not the hash text or the sorting version, so a
	// decoded cursor equals the original with exactly those two fields dropped.
	wantDecoded := want
	wantDecoded.QueryHash = ""
	wantDecoded.SortingVersion = ""
	if !reflect.DeepEqual(got, wantDecoded) {
		t.Fatalf("decoded cursor = %#v, want %#v", got, wantDecoded)
	}
	if got.SnapshotID != 42 || got.Offset != 50 {
		t.Fatalf("decoded position = (%d, %d), want (42, 50)", got.SnapshotID, got.Offset)
	}
	if err := got.ValidateAgainst(42, queryHash, SortingVersionStableKeyV1); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestCursorEncodingSurvivesLargeSnapshotsAndOffsets(t *testing.T) {
	cursor, queryHash := newTestCursor(t, 1<<62, "find_references:Widget", maxCursorOffset)
	token, err := cursor.Encode()
	if err != nil {
		t.Fatalf("Cursor.Encode() error = %v", err)
	}
	got, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if got.SnapshotID != 1<<62 || got.Offset != maxCursorOffset {
		t.Fatalf("decoded position = (%d, %d), want (%d, %d)", got.SnapshotID, got.Offset, uint64(1)<<62, maxCursorOffset)
	}
	if err := got.ValidateAgainst(1<<62, queryHash, SortingVersionStableKeyV1); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestDecodeCursorRejectsTamperingAndMalformedPayloads(t *testing.T) {
	cursor, queryHash := newTestCursor(t, 7, "find_symbol:Widget", 10)
	token, err := cursor.Encode()
	if err != nil {
		t.Fatal(err)
	}
	body := decodeToken(t, token)

	shiftedOffset := append([]byte(nil), body...)
	shiftedOffset[2]++
	truncated := append([]byte(nil), body[:len(body)-1]...)
	trailing := append(append([]byte(nil), body...), 0x00)
	brokenChecksum := append([]byte(nil), body...)
	brokenChecksum[len(brokenChecksum)-1] ^= 0xff
	// Eleven continuation bytes overflow binary.Uvarint's 64-bit budget.
	brokenVarint := append([]byte{CursorFormatVersion, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}, body[3:]...)
	unknownVersion := append([]byte(nil), body...)
	unknownVersion[0] = CursorFormatVersion + 1

	// A non-minimal varint spells the same numbers with different bytes; the
	// checksum is recomputed over the canonical layout, so it must be rejected
	// even though it decodes.
	nonCanonical := append([]byte{CursorFormatVersion, 0x87, 0x00}, body[2:]...)

	// A version 1 token was base64-wrapped JSON. It must fail closed rather
	// than be reinterpreted under the binary layout.
	versionOneJSON := base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"snapshot_id":7,"query_hash":"` + queryHash + `","offset":10,"sorting_version":"` + SortingVersionStableKeyV1 + `","checksum":"` + strings.Repeat("a", 64) + `"}`))
	versionOneBinary := append([]byte(nil), body...)
	versionOneBinary[0] = 1

	for name, malformed := range map[string]string{
		"empty":                 "",
		"not base64":            "not-base64",
		"tampered offset":       base64.RawURLEncoding.EncodeToString(shiftedOffset),
		"truncated buffer":      base64.RawURLEncoding.EncodeToString(truncated),
		"short buffer":          base64.RawURLEncoding.EncodeToString(body[:5]),
		"trailing bytes":        base64.RawURLEncoding.EncodeToString(trailing),
		"broken checksum":       base64.RawURLEncoding.EncodeToString(brokenChecksum),
		"unterminated varint":   base64.RawURLEncoding.EncodeToString(brokenVarint),
		"non-canonical varint":  base64.RawURLEncoding.EncodeToString(nonCanonical),
		"unknown version":       base64.RawURLEncoding.EncodeToString(unknownVersion),
		"version 1 json body":   versionOneJSON,
		"version 1 binary body": base64.RawURLEncoding.EncodeToString(versionOneBinary),
	} {
		if _, err := DecodeCursor(malformed); ErrorCode(err) != CodeCursorInvalid {
			t.Errorf("DecodeCursor(%s = %q) error code = %q, want %q", name, malformed, ErrorCode(err), CodeCursorInvalid)
		}
	}
}

func TestCursorValidationDistinguishesExpiredAndMismatchedQueries(t *testing.T) {
	cursor, queryHash := newTestCursor(t, 7, "find_symbol:Widget", 10)
	token, err := cursor.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	otherQuery, err := HashQuery("find_symbol:Other")
	if err != nil {
		t.Fatal(err)
	}

	// Both the freshly built cursor and the decoded one answer identically:
	// ValidateAgainst compares digests, which is all the wire form carries.
	for name, subject := range map[string]Cursor{"new": cursor, "decoded": decoded} {
		if err := subject.ValidateAgainst(7, queryHash, SortingVersionStableKeyV1); err != nil {
			t.Fatalf("%s: ValidateAgainst(active) error = %v", name, err)
		}
		if err := subject.ValidateAgainst(8, queryHash, SortingVersionStableKeyV1); ErrorCode(err) != CodeCursorSnapshotExpired {
			t.Fatalf("%s: expired cursor error code = %q, want %q", name, ErrorCode(err), CodeCursorSnapshotExpired)
		}
		if err := subject.ValidateAgainst(7, otherQuery, SortingVersionStableKeyV1); ErrorCode(err) != CodeCursorInvalid {
			t.Fatalf("%s: cross-query cursor error code = %q, want %q", name, ErrorCode(err), CodeCursorInvalid)
		}
		if err := subject.ValidateAgainst(7, queryHash, "qualified-name-v1"); ErrorCode(err) != CodeCursorInvalid {
			t.Fatalf("%s: sorting mismatch error code = %q, want %q", name, ErrorCode(err), CodeCursorInvalid)
		}
		if err := subject.ValidateAgainst(7, "not-a-hash", SortingVersionStableKeyV1); ErrorCode(err) != CodeCursorInvalid {
			t.Fatalf("%s: invalid active hash error code = %q, want %q", name, ErrorCode(err), CodeCursorInvalid)
		}
	}
}

func TestNewCursorRejectsInvalidIdentity(t *testing.T) {
	for _, test := range []struct {
		name           string
		queryHash      string
		offset         int
		sortingVersion string
	}{
		{name: "bad hash", queryHash: "not-a-hash", offset: 0, sortingVersion: SortingVersionStableKeyV1},
		{name: "uppercase hash", queryHash: strings.Repeat("A", 64), offset: 0, sortingVersion: SortingVersionStableKeyV1},
		{name: "negative offset", queryHash: strings.Repeat("a", 64), offset: -1, sortingVersion: SortingVersionStableKeyV1},
		{name: "offset out of range", queryHash: strings.Repeat("a", 64), offset: maxCursorOffset + 1, sortingVersion: SortingVersionStableKeyV1},
		{name: "empty sorting version", queryHash: strings.Repeat("a", 64), offset: 0, sortingVersion: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCursor(1, test.queryHash, test.offset, test.sortingVersion); ErrorCode(err) != CodeCursorInvalid {
				t.Fatalf("NewCursor() error code = %q, want %q", ErrorCode(err), CodeCursorInvalid)
			}
		})
	}
}

func TestCursorAssembledByHandCannotEncode(t *testing.T) {
	hand := Cursor{SnapshotID: 7, QueryHash: strings.Repeat("a", 64), Offset: 10, SortingVersion: SortingVersionStableKeyV1, Checksum: strings.Repeat("b", 16)}
	if _, err := hand.Encode(); ErrorCode(err) != CodeCursorInvalid {
		t.Fatalf("Encode() error code = %q, want %q", ErrorCode(err), CodeCursorInvalid)
	}
}

func TestToolErrorCauseRemainsAvailableForCursorErrors(t *testing.T) {
	cause := errors.New("cursor cause")
	err := WrapToolError(CodeCursorInvalid, "invalid cursor", cause)
	if !errors.Is(err, cause) {
		t.Fatal("cursor tool error lost its internal cause")
	}
}

func TestCursorBodyLayoutBoundsMatchVarintLimits(t *testing.T) {
	if maxCursorBodyLength != 1+2*binary.MaxVarintLen64+cursorIdentityLength+cursorChecksumLength {
		t.Fatalf("maxCursorBodyLength = %d, want the widest layout", maxCursorBodyLength)
	}
	cursor, _ := newTestCursor(t, 1<<62, "find_references:Widget", maxCursorOffset)
	if got := len(cursor.checksummedPrefix()) + cursorChecksumLength; got > maxCursorBodyLength {
		t.Fatalf("widest body = %d bytes, want at most %d", got, maxCursorBodyLength)
	}
}
