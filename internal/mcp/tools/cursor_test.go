package tools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

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
	queryHash, err := HashQuery(map[string]any{"tool": "find_symbol", "name": "Widget", "limit": 50})
	if err != nil {
		t.Fatal(err)
	}
	want, err := NewCursor(42, queryHash, 50, SortingVersionStableKeyV1)
	if err != nil {
		t.Fatalf("NewCursor() error = %v", err)
	}
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

	got, err := DecodeCursor(first)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded cursor = %#v, want %#v", got, want)
	}
	if err := got.ValidateAgainst(42, queryHash, SortingVersionStableKeyV1); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestDecodeCursorRejectsTamperingAndMalformedPayloads(t *testing.T) {
	queryHash, err := HashQuery("find_symbol:Widget")
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := NewCursor(7, queryHash, 10, SortingVersionStableKeyV1)
	if err != nil {
		t.Fatal(err)
	}
	token, err := cursor.Encode()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	var wire cursorWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	changedOffset := 11
	wire.Offset = &changedOffset
	changedPayload, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	tampered := base64.RawURLEncoding.EncodeToString(changedPayload)
	if _, err := DecodeCursor(tampered); ErrorCode(err) != CodeCursorInvalid {
		t.Fatalf("DecodeCursor(tampered) error code = %q, want %q", ErrorCode(err), CodeCursorInvalid)
	}

	for _, malformed := range []string{
		"",
		"not-base64",
		base64.RawURLEncoding.EncodeToString([]byte(`{"version":1}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"snapshot_id":7,"query_hash":"` + queryHash + `","offset":10,"sorting_version":"` + SortingVersionStableKeyV1 + `","checksum":"bad","extra":true}`)),
	} {
		if _, err := DecodeCursor(malformed); ErrorCode(err) != CodeCursorInvalid {
			t.Errorf("DecodeCursor(%q) error code = %q, want %q", malformed, ErrorCode(err), CodeCursorInvalid)
		}
	}
}

func TestCursorValidationDistinguishesExpiredAndMismatchedQueries(t *testing.T) {
	queryHash, err := HashQuery("find_symbol:Widget")
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := NewCursor(7, queryHash, 10, SortingVersionStableKeyV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := cursor.ValidateAgainst(8, queryHash, SortingVersionStableKeyV1); ErrorCode(err) != CodeCursorSnapshotExpired {
		t.Fatalf("expired cursor error code = %q, want %q", ErrorCode(err), CodeCursorSnapshotExpired)
	}
	otherQuery, err := HashQuery("find_symbol:Other")
	if err != nil {
		t.Fatal(err)
	}
	if err := cursor.ValidateAgainst(7, otherQuery, SortingVersionStableKeyV1); ErrorCode(err) != CodeCursorInvalid {
		t.Fatalf("cross-query cursor error code = %q, want %q", ErrorCode(err), CodeCursorInvalid)
	}
	if err := cursor.ValidateAgainst(7, queryHash, "qualified-name-v1"); ErrorCode(err) != CodeCursorInvalid {
		t.Fatalf("sorting mismatch error code = %q, want %q", ErrorCode(err), CodeCursorInvalid)
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
		{name: "negative offset", queryHash: strings.Repeat("a", 64), offset: -1, sortingVersion: SortingVersionStableKeyV1},
		{name: "empty sorting version", queryHash: strings.Repeat("a", 64), offset: 0, sortingVersion: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCursor(1, test.queryHash, test.offset, test.sortingVersion); ErrorCode(err) != CodeCursorInvalid {
				t.Fatalf("NewCursor() error code = %q, want %q", ErrorCode(err), CodeCursorInvalid)
			}
		})
	}
}

func TestToolErrorCauseRemainsAvailableForCursorErrors(t *testing.T) {
	cause := errors.New("cursor cause")
	err := WrapToolError(CodeCursorInvalid, "invalid cursor", cause)
	if !errors.Is(err, cause) {
		t.Fatal("cursor tool error lost its internal cause")
	}
}
