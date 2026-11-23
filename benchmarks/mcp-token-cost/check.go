package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// defaultTolerance is how much a figure may grow before the check fails: a
// tokenizer-stable measurement of a pinned corpus moves only when the server
// changed, so five per cent is room for one added field, not for a regression
// to hide in.
const defaultTolerance = 0.05

// loadBaseline reads the committed results of the corpus in directory.
func loadBaseline(directory string) (results, error) {
	path := filepath.Join(directory, "results.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return results{}, fmt.Errorf("read the baseline %s: %w (record one with a run that writes it)", path, err)
	}
	baseline := results{}
	if err := json.Unmarshal(data, &baseline); err != nil {
		return results{}, fmt.Errorf("parse the baseline %s: %w", path, err)
	}
	return baseline, nil
}

// checkAgainst compares a fresh run with the baseline committed for the same
// corpus and returns what got worse, one line each. It returns nothing when
// nothing did.
//
// What counts as worse is what an agent would notice. An answer that costs more
// tokens, or finds fewer references, is a regression; one that finds more, or
// costs less, is not, and the baseline is refreshed by an ordinary run when
// somebody wants to keep it. What is not comparable at all -- a corpus that
// moved, a question set or a capture that changed -- is a refusal to compare,
// not a pass: a ratchet that measured a different thing would tighten around
// the wrong number.
func checkAgainst(baseline, fresh results, tolerance float64) []string {
	var failures []string
	fail := func(format string, arguments ...any) {
		failures = append(failures, fmt.Sprintf(format, arguments...))
	}

	if !fresh.Corpus.Fresh {
		fail("the corpus is not the tree that was indexed (indexed %s, now %s)", short(fresh.Corpus.IndexedCommit), short(fresh.Corpus.CurrentCommit))
	}
	if fresh.Corpus.IndexedCommit != baseline.Corpus.IndexedCommit {
		fail("the corpus moved from %s to %s: record a new baseline", short(baseline.Corpus.IndexedCommit), short(fresh.Corpus.IndexedCommit))
	}
	if fresh.QuestionSet.Version != baseline.QuestionSet.Version {
		fail("the question set is version %d and the baseline's is %d: record a new baseline", fresh.QuestionSet.Version, baseline.QuestionSet.Version)
	}
	if len(fresh.Questions) != len(baseline.Questions) {
		fail("%d questions were measured and the baseline has %d: record a new baseline", len(fresh.Questions), len(baseline.Questions))
		return failures
	}

	limit := func(value int) int {
		return int(math.Floor(float64(value) * (1 + tolerance)))
	}
	for index, was := range baseline.Questions {
		now := fresh.Questions[index]
		if now.Symbol != was.Symbol {
			fail("question %d is %s and the baseline's is %s: record a new baseline", index, now.Symbol, was.Symbol)
			continue
		}
		if now.Native.Answer != was.Native.Answer {
			fail("%s: the captured native answer is %d tokens and the baseline's is %d: record a new baseline", now.Symbol, now.Native.Answer, was.Native.Answer)
		}
		if now.References < was.References {
			fail("%s: %d references where the baseline found %d", now.Symbol, now.References, was.References)
		}
		if now.Today.Calls > limit(was.Today.Calls) {
			fail("%s: the answer costs %d tokens, over the baseline's %d by more than %.0f%%", now.Symbol, now.Today.Calls, was.Today.Calls, tolerance*100)
		}
	}
	if fresh.Totals.TodayAnswer > limit(baseline.Totals.TodayAnswer) {
		fail("all answers cost %d tokens, over the baseline's %d by more than %.0f%%", fresh.Totals.TodayAnswer, baseline.Totals.TodayAnswer, tolerance*100)
	}
	if fresh.Totals.Projected > limit(baseline.Totals.Projected) {
		fail("the served session costs %d tokens, over the baseline's %d by more than %.0f%%", fresh.Totals.Projected, baseline.Totals.Projected, tolerance*100)
	}
	return failures
}

// checkRun is --check: it measures, compares, prints what got worse and writes
// nothing, so it can run on every commit without touching the committed report.
func checkRun(cfg config, fresh results) error {
	baseline, err := loadBaseline(cfg.Directory)
	if err != nil {
		return err
	}
	failures := checkAgainst(baseline, fresh, cfg.Tolerance)
	if len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, "regression:", failure)
		}
		return fmt.Errorf("%d regression(s) against %s", len(failures), filepath.Join(cfg.Directory, "results.json"))
	}
	fmt.Printf("%s: %d questions, answers %d tokens (baseline %d), no regression within %.0f%%\n",
		cfg.Directory, len(fresh.Questions), fresh.Totals.TodayAnswer, baseline.Totals.TodayAnswer, cfg.Tolerance*100)
	return nil
}
