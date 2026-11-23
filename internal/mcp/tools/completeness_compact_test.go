package tools

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

func scopeRow(repository, reason, pkg, detail string, occurrences int) BlindSpot {
	return BlindSpot{Reason: reason, Repository: repository, RequestedPackage: pkg, Detail: detail, Occurrences: occurrences}
}

func TestCompactCompletenessOfNothingIsNothing(t *testing.T) {
	if got := compactCompletenessOf(nil); got != nil {
		t.Fatalf("compactCompletenessOf(nil) = %#v, want nil: an answer that did not check must not read as one that did", got)
	}
	if got := compactProfiles(nil); got != nil {
		t.Fatalf("compactProfiles(nil) = %#v, want nil", got)
	}
}

func TestCompactCompletenessWithNoScopesStaysAVerdictAlone(t *testing.T) {
	encoded, err := json.Marshal(compactCompletenessOf(&Completeness{Verdict: VerdictComplete}))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"verdict":"COMPLETE"}` {
		t.Fatalf("encoded = %s, want only the verdict", encoded)
	}
}

// A detail is the group's only when every row of the group says it: a text that
// some rows carry, or that rows disagree on, is about a package, and it is
// still in the full view.
func TestCompactScopesKeepADetailOnlyWhenTheGroupSharesIt(t *testing.T) {
	cases := []struct {
		name       string
		rows       []BlindSpot
		wantDetail string
		wantNote   string
	}{
		{"one row keeps its detail", []BlindSpot{scopeRow("r", "A", "p1", "why", 1)}, "why", ""},
		{"rows that agree keep it once", []BlindSpot{scopeRow("r", "A", "p1", "why", 1), scopeRow("r", "A", "p2", "why", 1)}, "why", ""},
		{"rows that disagree drop it and say so", []BlindSpot{scopeRow("r", "A", "p1", "one", 1), scopeRow("r", "A", "p2", "two", 1)}, "", detailsInFullView},
		{"a row with none among rows that have one drops it and says so", []BlindSpot{scopeRow("r", "A", "p1", "why", 1), scopeRow("r", "A", "p2", "", 1)}, "", detailsInFullView},
		{"no row has one: nothing was dropped", []BlindSpot{scopeRow("r", "A", "p1", "", 1), scopeRow("r", "A", "p2", "", 1)}, "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			groups, note := compactScopes(test.rows)
			if len(groups) != 1 || groups[0].Detail != test.wantDetail || note != test.wantNote {
				t.Fatalf("groups = %#v, note = %q; want one group with detail %q and note %q", groups, note, test.wantDetail, test.wantNote)
			}
		})
	}
}

func TestCompactScopesNeverMergeAcrossRepositoryReasonOrFile(t *testing.T) {
	rows := []BlindSpot{
		scopeRow("a", "R", "p", "", 1),
		scopeRow("b", "R", "p", "", 1),
		scopeRow("a", "S", "p", "", 1),
		{Reason: "R", Repository: "a", FilePath: "x.go", RequestedPackage: "p", Occurrences: 1},
	}
	groups, _ := compactScopes(rows)
	if len(groups) != 4 {
		t.Fatalf("groups = %#v, want four: repository, reason and file each keep rows apart", groups)
	}
}

// Hoisting a value that some group lacks would give that group a repository it
// never had; hoisting one every group shares says nothing the header does not.
// The blind spots count: one verdict has one repository or none.
func TestCompactCompletenessHoistsTheRepositoryOnlyWhenEveryGroupNamesIt(t *testing.T) {
	spot := BlindSpot{Reason: "M", Repository: "a", FilePath: "f.go", StartLine: 1, RequestedSymbol: "S"}
	cases := []struct {
		name   string
		source Completeness
		want   string
	}{
		{"scopes agree", Completeness{Verdict: VerdictLowerBound, InvisibleScopes: []BlindSpot{scopeRow("a", "R", "p", "", 1), scopeRow("a", "S", "q", "", 1)}}, "a"},
		{"scopes and failed references agree", Completeness{Verdict: VerdictLowerBound, BlindSpots: []BlindSpot{spot}, InvisibleScopes: []BlindSpot{scopeRow("a", "R", "p", "", 1)}}, "a"},
		{"a failed reference is in another repository", Completeness{Verdict: VerdictLowerBound, BlindSpots: []BlindSpot{{Reason: "M", Repository: "b", FilePath: "g.go", StartLine: 2}}, InvisibleScopes: []BlindSpot{scopeRow("a", "R", "p", "", 1)}}, ""},
		{"one scope names none", Completeness{Verdict: VerdictLowerBound, InvisibleScopes: []BlindSpot{scopeRow("a", "R", "p", "", 1), {Reason: "IMPLEMENTATIONS_NOT_ANALYZED", Detail: "d"}}}, ""},
		{"nothing to hoist", Completeness{Verdict: VerdictComplete}, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := compactCompletenessOf(&test.source)
			if got.Repository != test.want {
				t.Fatalf("repository = %q, want %q", got.Repository, test.want)
			}
			// Hoisted: no group repeats it. Not hoisted: every group keeps
			// exactly the repository its rows had, none included.
			var gotGroups, wantGroups []string
			for _, group := range got.BlindSpots {
				gotGroups = append(gotGroups, group.Repository)
			}
			for _, group := range got.InvisibleScopes {
				gotGroups = append(gotGroups, group.Repository)
			}
			for _, row := range test.source.BlindSpots {
				wantGroups = append(wantGroups, row.Repository)
			}
			for _, row := range test.source.InvisibleScopes {
				wantGroups = append(wantGroups, row.Repository)
			}
			if test.want != "" {
				for _, repository := range gotGroups {
					if repository != "" {
						t.Fatalf("groups %v still name a repository the header holds", gotGroups)
					}
				}
			} else if !reflect.DeepEqual(gotGroups, wantGroups) {
				t.Fatalf("groups name %v, want what their rows named: %v", gotGroups, wantGroups)
			}
		})
	}
}

func TestCompactScopesCountsAndNamesEachPackageOnce(t *testing.T) {
	groups, _ := compactScopes([]BlindSpot{
		scopeRow("r", "A", "p1", "", 18),
		scopeRow("r", "A", "p2", "", 2),
		// The same package under a second detail is one package.
		scopeRow("r", "A", "p1", "other", 3),
		// A row that counted nothing adds nothing and still names its package.
		scopeRow("r", "A", "p3", "", 0),
	})
	want := scopeGroup{Reason: "A", Repository: "r", Packages: []string{"p1", "p2", "p3"}, Occurrences: 23}
	if len(groups) != 1 || !reflect.DeepEqual(groups[0], want) {
		t.Fatalf("groups = %#v, want %#v", groups, want)
	}
}

// A row that names no package -- a legacy generation that cannot say which -- is
// still a scope: the group keeps its reason and its detail.
func TestCompactScopesKeepARowThatNamesNoPackage(t *testing.T) {
	groups, _ := compactScopes([]BlindSpot{{Reason: "IMPLEMENTATIONS_NOT_ANALYZED", Detail: "Rebuild this legacy generation."}})
	want := scopeGroup{Reason: "IMPLEMENTATIONS_NOT_ANALYZED", Detail: "Rebuild this legacy generation."}
	if len(groups) != 1 || !reflect.DeepEqual(groups[0], want) {
		t.Fatalf("groups = %#v, want %#v", groups, want)
	}
}

// What a reader decides with is the same in both spellings: compared to the
// brute-force reading of the full rows, every (repository, reason, package) is
// present once and the occurrences add up. Rows are generated, not picked.
func TestCompactScopesLoseNoPackageReasonOrOccurrence(t *testing.T) {
	reasons := []string{"PACKAGE_NOT_BUILDABLE", "MODULE_NOT_LOADED", "REPLACE_CONFLICT"}
	repositories := []string{"a", "b"}
	for seed := 0; seed < 200; seed++ {
		var rows []BlindSpot
		wantOccurrences := 0
		want := map[[3]string]bool{}
		for at := 0; at < 1+seed%17; at++ {
			row := scopeRow(
				repositories[(seed+at)%len(repositories)],
				reasons[(seed*3+at)%len(reasons)],
				"pkg/"+string(rune('a'+(seed+at*7)%11)),
				[]string{"", "d1", "d2"}[(seed+at)%3],
				1+(seed+at)%5,
			)
			rows = append(rows, row)
			wantOccurrences += row.Occurrences
			want[[3]string{row.Repository, row.Reason, row.RequestedPackage}] = true
		}
		groups, _ := compactScopes(rows)
		got := map[[3]string]bool{}
		gotOccurrences := 0
		for _, group := range groups {
			repository := group.Repository
			for _, pkg := range group.Packages {
				key := [3]string{repository, group.Reason, pkg}
				if got[key] {
					t.Fatalf("seed %d: %v named twice in %#v", seed, key, groups)
				}
				got[key] = true
			}
			gotOccurrences += group.Occurrences
		}
		if !reflect.DeepEqual(got, want) || gotOccurrences != wantOccurrences {
			t.Fatalf("seed %d: packages %v occurrences %d, want %v and %d", seed, sortedScopeKeys(got), gotOccurrences, sortedScopeKeys(want), wantOccurrences)
		}
	}
}

func sortedScopeKeys(set map[[3]string]bool) [][3]string {
	keys := make([][3]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1]+keys[i][2] < keys[j][0]+keys[j][1]+keys[j][2] })
	return keys
}

// The cut is what makes a list a lower bound, and the fallback is the recovery
// action: neither is the compact view's to rewrite.
func TestCompactCompletenessPassesTheCountsAndTheFallbackThrough(t *testing.T) {
	source := &Completeness{
		Verdict:             VerdictLowerBound,
		MoreBlindSpots:      3,
		MoreInvisibleScopes: 9,
		Fallback:            &Fallback{Pattern: `\bS\b`, Paths: []string{"/x"}},
	}
	got := compactCompletenessOf(source)
	if got.MoreBlindSpots != 3 || got.MoreInvisibleScopes != 9 || got.Fallback != source.Fallback {
		t.Fatalf("compact = %#v, want both counters and the fallback as they were", got)
	}
}

// A failed reference is evidence about one place. Grouped, every place stays
// and only what the failures share is said once.
func TestCompactBlindSpotsKeepEveryPlaceAndMergeOnlyWhatIsSharedWhole(t *testing.T) {
	shared := BlindSpot{Reason: "M", Repository: "r", RequestedSymbol: "S", RequestedPackage: "pkg", Detail: "d"}
	at := func(base BlindSpot, file string, line uint32) BlindSpot {
		base.FilePath, base.StartLine = file, line
		return base
	}
	other := func(change func(*BlindSpot)) BlindSpot {
		row := at(shared, "z.go", 9)
		change(&row)
		return row
	}
	rows := []BlindSpot{
		at(shared, "a.go", 3), at(shared, "b.go", 4),
		other(func(b *BlindSpot) { b.Reason = "N" }),
		other(func(b *BlindSpot) { b.Repository = "q" }),
		other(func(b *BlindSpot) { b.RequestedSymbol = "T" }),
		other(func(b *BlindSpot) { b.RequestedPackage = "pkg2" }),
		other(func(b *BlindSpot) { b.Detail = "e" }),
		at(shared, "c.go", 0),
		at(shared, "", 0), at(shared, "", 0),
	}
	groups, _ := compactBlindSpots(rows)
	if len(groups) != 6 {
		t.Fatalf("groups = %#v, want six: reason, repository, symbol, package and detail each keep failures apart", groups)
	}
	want := blindSpotGroup{
		Reason: "M", Repository: "r", RequestedSymbol: "S", RequestedPackage: "pkg", Detail: "d",
		At: []string{"a.go:3", "b.go:4", "c.go"}, Unlocated: 2,
	}
	if !reflect.DeepEqual(groups[0], want) {
		t.Fatalf("first group = %#v, want %#v", groups[0], want)
	}
	if empty, cut := compactBlindSpots(nil); empty != nil || cut {
		t.Fatal("no failed references must be no groups")
	}
}

// Compared to the brute-force reading of the rows: every place of every failure
// is in exactly one group, and the unlocated ones add up.
func TestCompactBlindSpotsLoseNoPlace(t *testing.T) {
	for seed := 0; seed < 200; seed++ {
		var rows []BlindSpot
		want := map[string]int{}
		wantUnlocated := 0
		for at := 0; at < 1+seed%23; at++ {
			row := BlindSpot{
				Reason: []string{"M", "N"}[(seed+at)%2], Repository: "r",
				RequestedSymbol: []string{"S", "T", "U"}[(seed*5+at)%3],
			}
			if (seed+at)%7 != 0 {
				row.FilePath, row.StartLine = "f"+string(rune('a'+(seed+at*3)%9))+".go", uint32(1+(seed+at)%4)
			}
			rows = append(rows, row)
			if row.FilePath == "" && row.StartLine == 0 {
				wantUnlocated++
				continue
			}
			want[row.Reason+"|"+row.RequestedSymbol+"|"+placeLabel(row.FilePath, row.StartLine)]++
		}
		got := map[string]int{}
		gotUnlocated := 0
		blindGroups, _ := compactBlindSpots(rows)
		for _, group := range blindGroups {
			gotUnlocated += group.Unlocated
			for _, place := range group.At {
				got[group.Reason+"|"+group.RequestedSymbol+"|"+place]++
			}
		}
		if !reflect.DeepEqual(got, want) || gotUnlocated != wantUnlocated {
			t.Fatalf("seed %d: places %v unlocated %d, want %v and %d", seed, got, gotUnlocated, want, wantUnlocated)
		}
	}
}

// The full view is the spelling every client written so far reads, so it must
// not move by a byte.
func TestFullViewKeepsOneRowPerScope(t *testing.T) {
	completeness := &Completeness{
		Verdict: VerdictLowerBound,
		InvisibleScopes: []BlindSpot{
			scopeRow("r", "PACKAGE_NOT_BUILDABLE", "p1", "d", 1),
			scopeRow("r", "PACKAGE_NOT_BUILDABLE", "p2", "d", 1),
		},
	}
	encoded, err := json.Marshal(Response[[]int]{Completeness: completeness, View: ViewFull, Results: []int{}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Completeness Completeness `json:"completeness"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Completeness, *completeness) {
		t.Fatalf("full view = %#v, want the rows as built", decoded.Completeness)
	}
}

func TestCompactViewSpellsScopesAsGroupsAndProfilesLikeTheEnvelope(t *testing.T) {
	completeness := &Completeness{
		Verdict: VerdictLowerBound,
		InvisibleScopes: []BlindSpot{
			scopeRow("r", "PACKAGE_NOT_BUILDABLE", "p1", "same", 1),
			scopeRow("r", "PACKAGE_NOT_BUILDABLE", "p2", "same", 1),
		},
	}
	encoded, err := json.Marshal(Response[[]int]{
		Completeness: completeness, View: ViewCompact, Results: []int{},
		Profiles: []ProfileSnapshot{{Name: "x", SnapshotID: 4, Completeness: completeness}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantCompleteness = `{"verdict":"LOWER_BOUND","repository":"r","invisible_scopes":[{"reason":"PACKAGE_NOT_BUILDABLE","packages":["p1","p2"],"occurrences":2,"detail":"same"}]}`
	var decoded struct {
		Completeness json.RawMessage `json:"completeness"`
		Profiles     []struct {
			Completeness json.RawMessage `json:"completeness"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Completeness) != wantCompleteness || len(decoded.Profiles) != 1 || string(decoded.Profiles[0].Completeness) != wantCompleteness {
		t.Fatalf("compact envelope = %s, want both verdicts spelled as %s", encoded, wantCompleteness)
	}
}

// A detail is whatever a tool printed, and in a compact answer it is one line of
// bounded size: the rest is in the full view, and the answer says so. The cut
// goes where it is cheapest to be wrong -- at a line, at the bound, and never in
// the middle of a character.
func TestShortDetailKeepsOneBoundedLine(t *testing.T) {
	exact := strings.Repeat("a", maximumCompactDetail)
	over := exact + "b"
	multibyte := strings.Repeat("a", maximumCompactDetail-1) + "\u00e9" // the last character straddles the bound
	for name, test := range map[string]struct {
		in, want string
		cut      bool
	}{
		"empty":                     {"", "", false},
		"a short line":              {"declared in a build cache entry", "declared in a build cache entry", false},
		"exactly the bound":         {exact, exact, false},
		"one byte over":             {over, exact + "\u2026", true},
		"a character on the bound":  {multibyte, strings.Repeat("a", maximumCompactDetail-1) + "\u2026", true},
		"two lines":                 {"first\nsecond", "first\u2026", true},
		"a trailing newline":        {"only line\n", "only line\u2026", true},
		"a leading empty line":      {"\nsecond", "\u2026", true},
		"carriage return and space": {"first \r\nsecond", "first\u2026", true},
	} {
		got, cut := shortDetail(test.in)
		if got != test.want || cut != test.cut {
			t.Errorf("%s: shortDetail(%q) = (%q, %t), want (%q, %t)", name, test.in, got, cut, test.want, test.cut)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: %q is not valid UTF-8", name, got)
		}
	}
}

// What was cut is said once, wherever it was cut: in a scope that stands alone,
// or in a failed reference.
func TestACutDetailSendsTheReaderToTheFullView(t *testing.T) {
	long := strings.Repeat("x", 4000)
	for name, source := range map[string]Completeness{
		"a scope alone in its group": {Verdict: VerdictLowerBound, InvisibleScopes: []BlindSpot{scopeRow("r", "R", "p", long, 1)}},
		"a failed reference":         {Verdict: VerdictLowerBound, BlindSpots: []BlindSpot{{Reason: "M", Repository: "r", FilePath: "a.go", StartLine: 1, Detail: long}}},
	} {
		got := compactCompletenessOf(&source)
		if got.Details != detailsInFullView {
			t.Errorf("%s: details = %q, want %q", name, got.Details, detailsInFullView)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > 1000 {
			t.Errorf("%s: the compact verdict is %d bytes for a detail of %d", name, len(encoded), len(long))
		}
	}
	// A detail that fits is not a reason to send anyone anywhere.
	whole := compactCompletenessOf(&Completeness{Verdict: VerdictLowerBound, InvisibleScopes: []BlindSpot{scopeRow("r", "R", "p", "short", 1)}})
	if whole.Details != "" || whole.InvisibleScopes[0].Detail != "short" {
		t.Errorf("a detail that fits was touched: %#v", whole)
	}
}
