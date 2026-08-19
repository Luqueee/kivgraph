package tools

import (
	"encoding/json"
	"testing"
)

// TestNormalizeViewDefaultsToCompactAndRefusesWhatItCannotAnswer pins the two
// halves of the argument: the default is the cheap shape, and a tool that does
// not answer in files says so instead of answering something else.
func TestNormalizeViewDefaultsToCompactAndRefusesWhatItCannotAnswer(t *testing.T) {
	for value, want := range map[string]string{"": ViewCompact, ViewCompact: ViewCompact, ViewFull: ViewFull} {
		got, err := normalizeView(value, false)
		if err != nil || got != want {
			t.Fatalf("normalizeView(%q, false) = %q, %v; want %q", value, got, err, want)
		}
	}
	if got, err := normalizeView(ViewFiles, true); err != nil || got != ViewFiles {
		t.Fatalf("normalizeView(files, true) = %q, %v", got, err)
	}
	for _, value := range []string{ViewFiles, "summary", "COMPACT"} {
		if _, err := normalizeView(value, false); err == nil {
			t.Fatalf("normalizeView(%q, false) accepted an unsupported view", value)
		}
	}
}

// TestHoistStringLiftsOnlyWhatEveryRowShares is the rule the compact shape rests
// on: a column reaches the header only when reading it there is reading it for
// every row.
func TestHoistStringLiftsOnlyWhatEveryRowShares(t *testing.T) {
	cases := map[string]struct {
		rows []string
		want string
	}{
		"all agree":   {[]string{"go", "go", "go"}, "go"},
		"one differs": {[]string{"go", "go", "rust"}, ""},
		"one empty":   {[]string{"go", "", "go"}, ""},
		"first empty": {[]string{"", "go"}, ""},
		"none":        {nil, ""},
	}
	for name, testCase := range cases {
		rows := testCase.rows
		got := hoistString(len(rows), func(index int) string { return rows[index] })
		if got != testCase.want {
			t.Fatalf("%s: hoistString = %q, want %q", name, got, testCase.want)
		}
	}
}

// TestCompactRowTailStaysPositional covers why a row is a bare string until it
// is not: a tail entry is read by position, so an omitted column has to be
// omitted for the whole page, never for one row.
func TestCompactRowTailStaysPositional(t *testing.T) {
	bare := compactRowTail("pkg.Caller@12-20")
	if bare != "pkg.Caller@12-20" {
		t.Fatalf("row with nothing left over = %#v, want the bare label", bare)
	}
	withTail, ok := compactRowTail("pkg.Caller@12", "", "CALLS_DIRECT", "").([]any)
	if !ok || len(withTail) != 2 || withTail[0] != "pkg.Caller@12" || withTail[1] != "CALLS_DIRECT" {
		t.Fatalf("row with one column left over = %#v", withTail)
	}
}

// TestCompactEnvelopeDropsWhatCarriedNothing is the envelope half of ADR 0046.
// Under the full view the nulls stay, because a client is allowed to rely on
// every field being present.
func TestCompactEnvelopeDropsWhatCarriedNothing(t *testing.T) {
	snapshotID := uint64(7)
	ageMS := int64(1234)
	response := Response[[]string]{
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &ageMS,
		Total:         0,
		Returned:      0,
		Coverage:      Coverage{},
		Results:       []string{},
		View:          ViewCompact,
	}
	compact := map[string]json.RawMessage{}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal compact envelope: %v", err)
	}
	if err := json.Unmarshal(encoded, &compact); err != nil {
		t.Fatalf("unmarshal compact envelope: %v", err)
	}
	for _, absent := range []string{"snapshot_age_ms", "truncated", "next_cursor", "coverage"} {
		if _, present := compact[absent]; present {
			t.Fatalf("compact envelope carries %q: %s", absent, encoded)
		}
	}
	// A zero total is a fact -- a proven absence -- so it is never dropped.
	for _, required := range []string{"snapshot_id", "total", "returned", "results"} {
		if _, present := compact[required]; !present {
			t.Fatalf("compact envelope dropped %q: %s", required, encoded)
		}
	}

	response.View = ViewFull
	response.Coverage = Coverage{Exact: 2}
	full := map[string]json.RawMessage{}
	encoded, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal full envelope: %v", err)
	}
	if err := json.Unmarshal(encoded, &full); err != nil {
		t.Fatalf("unmarshal full envelope: %v", err)
	}
	for _, required := range []string{"snapshot_id", "snapshot_age_ms", "total", "returned", "truncated", "next_cursor", "coverage", "results"} {
		if _, present := full[required]; !present {
			t.Fatalf("full envelope dropped %q: %s", required, encoded)
		}
	}
	if string(full["coverage"]) == "" || string(full["next_cursor"]) != "null" {
		t.Fatalf("full envelope = %s, want the nulls kept", encoded)
	}
}

// TestNameIsLastSegmentIgnoresTheDiscriminator covers the case that put `name`
// back on twenty-nine rows of a real page: one export whose qualified name
// carries the snapshot's `#2` discriminator.
func TestNameIsLastSegmentIgnoresTheDiscriminator(t *testing.T) {
	cases := map[string]struct {
		name, qualifiedName string
		want                bool
	}{
		"plain": {"getField", "getField", true},
		// A TypeScript private field keeps its `#`, and the qualified name still
		// spells the name at its end, so the row needs no separate `name`.
		"private field":        {"count", "Counter.#count", true},
		"discriminated":        {"getRequiredField", "getRequiredField#2", true},
		"discriminated method": {"clone", "Vec::clone#11", true},
		"unrelated":            {"other", "pkg.Merge", false},
		"empty name":           {"", "pkg.Merge", false},
	}
	for label, testCase := range cases {
		if got := nameIsLastSegment(testCase.name, testCase.qualifiedName); got != testCase.want {
			t.Fatalf("%s: nameIsLastSegment(%q, %q) = %t", label, testCase.name, testCase.qualifiedName, got)
		}
	}
}
