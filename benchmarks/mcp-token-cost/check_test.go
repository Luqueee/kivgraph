package main

import (
	"slices"
	"strings"
	"testing"
)

func baselineResults() results {
	return results{
		Corpus:      corpus{IndexedCommit: "aaaaaaaa11111111", CurrentCommit: "aaaaaaaa11111111", Fresh: true},
		QuestionSet: questionSetMeta{Version: 2},
		Questions: []questionResult{
			{Symbol: "Alpha", References: 10, Native: arm{Answer: 400}, Today: arm{Calls: 1000}},
			{Symbol: "Beta", References: 3, Native: arm{Answer: 90}, Today: arm{Calls: 200}},
		},
		Totals: totals{TodayAnswer: 1200, Projected: 5000},
	}
}

// withChange returns the baseline with one edit applied, so each case names
// the single thing it moved.
func withChange(change func(*results)) results {
	fresh := baselineResults()
	fresh.Questions = slices.Clone(fresh.Questions)
	change(&fresh)
	return fresh
}

func TestCheckAgainstRefusesWhatItCannotCompareAndNamesWhatGotWorse(t *testing.T) {
	for name, test := range map[string]struct {
		fresh results
		// contains is a fragment of the one line the case must produce; empty
		// means the check must pass.
		contains string
	}{
		"the same run": {fresh: baselineResults()},
		"an answer at the edge of the tolerance": {
			fresh: withChange(func(r *results) { r.Questions[0].Today.Calls = 1050; r.Totals.TodayAnswer = 1250 }),
		},
		"an answer one token past it": {
			fresh:    withChange(func(r *results) { r.Questions[0].Today.Calls = 1051 }),
			contains: "Alpha: the answer costs 1051 tokens",
		},
		"all answers past it": {
			fresh:    withChange(func(r *results) { r.Totals.TodayAnswer = 1261 }),
			contains: "all answers cost 1261 tokens",
		},
		"the served session past it": {
			fresh:    withChange(func(r *results) { r.Totals.Projected = 5251 }),
			contains: "the served session costs 5251 tokens",
		},
		"cheaper": {
			fresh: withChange(func(r *results) {
				r.Questions[0].Today.Calls = 300
				r.Totals.TodayAnswer = 500
				r.Totals.Projected = 100
			}),
		},
		"fewer references": {
			fresh:    withChange(func(r *results) { r.Questions[1].References = 2 }),
			contains: "Beta: 2 references where the baseline found 3",
		},
		"more references": {
			fresh: withChange(func(r *results) { r.Questions[1].References = 4 }),
		},
		"the corpus is not the indexed tree": {
			fresh:    withChange(func(r *results) { r.Corpus.Fresh = false; r.Corpus.CurrentCommit = "bbbbbbbb22222222" }),
			contains: "not the tree that was indexed",
		},
		"the corpus moved": {
			fresh: withChange(func(r *results) {
				r.Corpus.IndexedCommit = "bbbbbbbb22222222"
				r.Corpus.CurrentCommit = "bbbbbbbb22222222"
			}),
			contains: "the corpus moved from aaaaaaaa to bbbbbbbb",
		},
		"another question set": {
			fresh:    withChange(func(r *results) { r.QuestionSet.Version = 3 }),
			contains: "question set is version 3",
		},
		"a question fewer": {
			fresh:    withChange(func(r *results) { r.Questions = r.Questions[:1] }),
			contains: "1 questions were measured and the baseline has 2",
		},
		"a question in another place": {
			fresh: withChange(func(r *results) {
				r.Questions[0], r.Questions[1] = r.Questions[1], r.Questions[0]
			}),
			contains: "question 0 is Beta",
		},
		"another native capture": {
			fresh:    withChange(func(r *results) { r.Questions[0].Native.Answer = 401 }),
			contains: "the captured native answer is 401 tokens",
		},
	} {
		failures := checkAgainst(baselineResults(), test.fresh, defaultTolerance)
		if test.contains == "" {
			if len(failures) != 0 {
				t.Errorf("%s: failed with %q, want a pass", name, failures)
			}
			continue
		}
		if len(failures) == 0 || !strings.Contains(strings.Join(failures, "\n"), test.contains) {
			t.Errorf("%s: failures = %q, want one containing %q", name, failures, test.contains)
		}
	}
}
