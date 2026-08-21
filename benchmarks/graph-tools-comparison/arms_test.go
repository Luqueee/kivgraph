package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestScoreAgainstScoresAnAbsence defends the convention an absence question
// depends on. Both ratios are undefined against an empty truth, and the
// question is only worth asking if answering it correctly -- claiming nothing
// -- is the score that beats claiming something.
func TestScoreAgainstScoresAnAbsence(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		claimed []string
		truth   []string
		want    score
	}{
		{
			name: "nothing claimed against nothing true is exact",
			want: score{Claimed: []string{}, Missing: []string{}, Spurious: []string{}, Precision: 1, Recall: 1},
		},
		{
			name:    "anything claimed against nothing true has nothing to miss",
			claimed: []string{"repo:b.go", "repo:a.go"},
			want: score{
				Claimed: []string{"repo:a.go", "repo:b.go"}, Missing: []string{},
				Spurious: []string{"repo:a.go", "repo:b.go"}, Precision: 0, Recall: 1,
			},
		},
		{
			name:    "nothing claimed against a real truth is not an absence",
			truth:   []string{"repo:a.go"},
			want:    score{Claimed: []string{}, Missing: []string{"repo:a.go"}, Spurious: []string{}},
			claimed: nil,
		},
		{
			name:    "one hit and one miss out of two claimed",
			claimed: []string{"repo:a.go", "repo:c.go"},
			truth:   []string{"repo:a.go", "repo:b.go"},
			want: score{
				Claimed: []string{"repo:a.go", "repo:c.go"}, Missing: []string{"repo:b.go"},
				Spurious: []string{"repo:c.go"}, Precision: 0.5, Recall: 0.5,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := scoreAgainst(testCase.claimed, testCase.truth)
			if !reflect.DeepEqual(testCase.want, *got) {
				t.Fatalf("scoreAgainst() = %#v, want %#v", *got, testCase.want)
			}
		})
	}
}

// TestPublishedCorpusRefusesAMissingLanguage defends the rule that was missing
// when three published sets measured kena without Rust in it.
//
// The counter that would have caught it was in the payload the harness already
// parsed, and the `Needs` line already said a load without its toolchain leaves
// its symbols absent. Nothing read either. So the contract is now a refusal, and
// it covers both shapes: a language that reports a failed load, and a language
// that reports nothing at all -- which is the one that actually happened, and the
// quieter of the two.
func TestPublishedCorpusRefusesAMissingLanguage(t *testing.T) {
	line := func(rustSymbols, notLoaded int) string {
		return fmt.Sprintf(`{"event":"result","result":{"passed":true,"generation_id":"000001",`+
			`"counts":{"symbols":123524},"index":{"go_definitions":19166,"typescript_symbols":123823,`+
			`"rust_symbols":%d,"rust_workspaces_not_loaded":%d}}}`, rustSymbols, notLoaded)
	}

	whole, err := publishedCorpus(line(3063, 0))
	if err != nil {
		t.Fatalf("publishedCorpus() error = %v", err)
	}
	if whole.Symbols != 123524 || whole.Languages["rust"] != 3063 {
		t.Fatalf("load = %#v, want the published counts", whole)
	}
	if err := whole.complete(); err != nil {
		t.Fatalf("complete() on a whole corpus = %v, want it accepted", err)
	}

	// The shape that happened: the pass published, and Rust contributed nothing.
	silent, err := publishedCorpus(line(0, 2))
	if err != nil {
		t.Fatal(err)
	}
	err = silent.complete()
	if err == nil {
		t.Fatal("complete() accepted a corpus with no Rust in it")
	}
	if !strings.Contains(err.Error(), "rust") || !strings.Contains(err.Error(), "cargo") {
		t.Fatalf("refusal = %v, want it to name the language and the fix", err)
	}

	// Zero symbols with no failed load is still a hole, and a quieter one.
	quiet, err := publishedCorpus(line(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := quiet.complete(); err == nil {
		t.Fatal("complete() accepted a corpus whose language published nothing")
	}

	// A republished generation reports empty counters by design and is not a
	// hole: judging it would refuse every second run over an unchanged tree.
	republished, err := publishedCorpus(`{"event":"result","result":{"passed":true,` +
		`"generation_id":"000001","counts":{"symbols":0},"index":{}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := republished.complete(); err != nil {
		t.Fatalf("complete() on a republished generation = %v, want it accepted", err)
	}
}
