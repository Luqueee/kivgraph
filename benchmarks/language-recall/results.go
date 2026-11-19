package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func sqrt(value float64) float64 { return math.Sqrt(value) }

type environment struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Go         string `json:"go"`
	Kivgraph   string `json:"kivgraph"`
	Node       string `json:"node,omitempty"`
	TypeScript string `json:"typescript,omitempty"`
	Rust       string `json:"rust,omitempty"`
}

type metric struct {
	Value   float64 `json:"value"`
	Low     float64 `json:"low"`
	High    float64 `json:"high"`
	Num     int     `json:"numerator"`
	Den     int     `json:"denominator"`
	Symbols int     `json:"symbols"`
}

type kindResult struct {
	Kind    string `json:"kind"`
	Symbols int    `json:"symbols_with_dependents"`
	Recall  metric `json:"file_recall"`
}

type languageResult struct {
	Arm                       string         `json:"arm"`
	Language                  string         `json:"language"`
	Listed                    int            `json:"declarations_listed"`
	Eligible                  int            `json:"declarations_eligible"`
	Skipped                   map[string]int `json:"not_sampled"`
	Sampled                   int            `json:"declarations_measured"`
	WithDependents            int            `json:"with_dependents"`
	OracleFailures            int            `json:"oracle_failures"`
	KivgraphFailures          int            `json:"kivgraph_failures"`
	Recall                    metric         `json:"file_recall"`
	RecallNonTest             metric         `json:"file_recall_non_test"`
	RecallTest                metric         `json:"file_recall_test"`
	RecallPrivate             metric         `json:"file_recall_private_names"`
	RecallNonPrivate          metric         `json:"file_recall_without_private_names"`
	Precision                 metric         `json:"file_precision"`
	FullRecallSymbols         metric         `json:"symbols_fully_recalled"`
	WithoutDependentsReported metric         `json:"symbols_without_dependents_that_report_references"`
	Verdicts                  map[string]int `json:"completeness_verdicts"`
	CompleteWithMiss          int            `json:"complete_verdicts_that_missed_a_non_test_file"`
	ByKind                    []kindResult   `json:"by_kind"`
	Symbols                   []symbolResult `json:"symbols"`
}

type results struct {
	Benchmark   string             `json:"benchmark"`
	Command     string             `json:"command"`
	Date        string             `json:"date"`
	Seed        int64              `json:"seed"`
	Count       int                `json:"declarations_per_language"`
	Environment environment        `json:"environment"`
	Corpus      []corpusRepository `json:"corpus"`
	Languages   []languageResult   `json:"languages"`
	Limitations []string           `json:"limitations"`
	WallSeconds float64            `json:"wall_seconds"`
}

func limitations() []string {
	return []string{
		"The oracle and Kivgraph share each language's type checker for Go, TypeScript and Dart. Go is checked with `go/packages` and `go/types` on both sides, TypeScript with the repository's own `tsc` on one side and the same checker inside the worker on the other, Dart with `dart analyze` on one side and the Analysis Server on the other. Type resolution is not what this measures there: it measures which files and build variants are loaded, how resolved uses become edges, and what is filtered on the way out. Rust is the exception: the oracle is `rustc` through `cargo check` and Kivgraph reads `rust-analyzer`, two different front ends.",
		"The unit is the file, not the reference. A rename makes the compiler reject some uses and not others (after a failed import, the uses of the imported name are not reported), so reference-level ground truth would differ by language. A file is a dependent if it gains an error.",
		"Implementation and inheritance relations are left out of the truth: an error that says a type no longer satisfies an interface or a base class is not a use of the renamed name, and `find_references` is not the tool for it.",
		"The sample frame is what Kivgraph lists for each file, so a declaration Kivgraph never recorded cannot be sampled. A recall measured this way is conditional on the declaration being known.",
		"Only declarations whose name appears in its declaring position on its first line are renamed, so the rename is exact. Declarations written another way are counted as not sampled.",
		"Go and Dart declarations are sampled from non-test files; references from test files are part of the truth and are reported apart, and the default arm reads them (`go.include_tests` and `dart.include_tests` are true by default). The `tests_excluded` arm sets that key to false for one language, and its test files are then missed by construction.",
		"One repository per language: the figures describe these repositories at these commits, not the language. A second repository would show how much they move.",
		"A declaration with no dependents says nothing about recall. It is reported through the share of such declarations for which Kivgraph still reports a reference.",
		"The TypeScript dependencies are installed with `npm install` and are not pinned by a lockfile, so a rerun can resolve other versions. The Rust dependencies are fetched with `--locked`.",
		"`rustc` stops after name resolution when that fails, so an error that needs type inference can be hidden behind an unresolved name elsewhere. The same holds across a workspace: a crate that fails to compile hides every crate and target that depends on it, so on a multi-crate workspace the truth is a lower bound and the precision is too (on `ripgrep`, 25 of 33 extra files are in other crates and 8 in targets of the declaring crate). The Rust truth cannot contain a file that does not use the name.",
		"Renaming a Dart class breaks its constructors' own declarations (a constructor is written with the class's name), and an `implements` clause is an implementation relation: those errors are left out of the truth. The Dart dependencies are resolved with `dart pub get`, and the repository ships no lockfile.",
	}
}

func writeResults(directory string, measured results) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(measured, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(renderReport(measured)), 0o644)
}

func percent(m metric) string {
	if m.Den == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f %% (%.1f to %.1f)", 100*m.Value, 100*m.Low, 100*m.High)
}

func renderReport(measured results) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Recall on real code, by language\n\n")
	fmt.Fprintf(&b, "How many of the files that depend on a declaration does `find_references` report? The ground truth is the language's own type checker: rename the declaration, type-check again, and the files that gain an error are the files that use it.\n\n")
	fmt.Fprintf(&b, "- Command: `%s`\n- Date: %s, seed %d, up to %d declarations per language\n", measured.Command, measured.Date, measured.Seed, measured.Count)
	fmt.Fprintf(&b, "- Kivgraph `%s` on %s/%s, %s", measured.Environment.Kivgraph, measured.Environment.OS, measured.Environment.Arch, measured.Environment.Go)
	if measured.Environment.TypeScript != "" {
		fmt.Fprintf(&b, ", %s, node %s", measured.Environment.TypeScript, measured.Environment.Node)
	}
	if measured.Environment.Rust != "" {
		fmt.Fprintf(&b, ", %s", measured.Environment.Rust)
	}
	fmt.Fprintf(&b, "\n\n## Corpus\n\n|language|repository|commit|\n|---|---|---|\n")
	for _, repository := range measured.Corpus {
		fmt.Fprintf(&b, "|%s|[%s](%s)|`%s`|\n", repository.Language, repository.Name, strings.TrimSuffix(repository.URL, ".git"), repository.Commit[:12])
	}
	fmt.Fprintf(&b, "\n## Result\n\nIntervals are 95 %% percentile intervals from resampling declarations.\n\n")
	fmt.Fprintf(&b, "|language|configuration|declarations measured|with dependents|file recall|file precision|fully recalled|\n|---|---|---|---|---|---|---|\n")
	for _, language := range measured.Languages {
		fmt.Fprintf(&b, "|%s|%s|%d|%d|%s|%s|%s|\n", language.Language, language.Arm, language.Sampled, language.WithDependents,
			percent(language.Recall), percent(language.Precision), percent(language.FullRecallSymbols))
	}
	fmt.Fprintf(&b, "\n`file recall` pools the dependent files of every declaration. `fully recalled` is the share of declarations with dependents for which no file was missed (Wilson interval). `go.tests_excluded` and `dart.tests_excluded` are the default configuration with that language's `include_tests: false`; each pair draws the same declarations.\n")
	for _, language := range measured.Languages {
		fmt.Fprintf(&b, "\n## %s (%s)\n\n", language.Language, language.Arm)
		fmt.Fprintf(&b, "- Declarations listed %d, eligible for the rename %d, measured %d (oracle failed on %d, Kivgraph refused %d).\n",
			language.Listed, language.Eligible, language.Sampled, language.OracleFailures, language.KivgraphFailures)
		fmt.Fprintf(&b, "- Recall on non-test files: %s. On test files: %s.\n", percent(language.RecallNonTest), percent(language.RecallTest))
		if language.RecallPrivate.Den > 0 {
			fmt.Fprintf(&b, "- Recall on ES private names (`#x`): %s over %d declarations. On every other declaration: %s.\n",
				percent(language.RecallPrivate), language.RecallPrivate.Symbols, percent(language.RecallNonPrivate))
		}
		fmt.Fprintf(&b, "- Declarations with no dependents for which Kivgraph still reports a file: %d of %d.\n",
			language.WithoutDependentsReported.Num, language.WithoutDependentsReported.Den)
		var verdicts []string
		for verdict, n := range language.Verdicts {
			verdicts = append(verdicts, fmt.Sprintf("%s %d", verdict, n))
		}
		sort.Strings(verdicts)
		fmt.Fprintf(&b, "- Completeness verdicts: %s.\n", strings.Join(verdicts, ", "))
		fmt.Fprintf(&b, "- Answers that said `COMPLETE` and missed at least one non-test file: %d of %d.\n", language.CompleteWithMiss, language.Verdicts["COMPLETE"])
		var skipped []string
		for reason, n := range language.Skipped {
			skipped = append(skipped, fmt.Sprintf("%s: %d", reason, n))
		}
		sort.Strings(skipped)
		fmt.Fprintf(&b, "- Not sampled: %s.\n\n", strings.Join(skipped, "; "))
		fmt.Fprintf(&b, "|kind|declarations with dependents|file recall|\n|---|---|---|\n")
		for _, kind := range language.ByKind {
			fmt.Fprintf(&b, "|%s|%d|%s|\n", kind.Kind, kind.Symbols, percent(kind.Recall))
		}
		var missed []symbolResult
		for _, symbol := range language.Symbols {
			if len(symbol.Missed) > 0 {
				missed = append(missed, symbol)
			}
		}
		if len(missed) > 0 {
			fmt.Fprintf(&b, "\nDeclarations with a missed file (first 15 of %d):\n\n|declaration|kind|missed|verdict|\n|---|---|---|---|\n", len(missed))
			for i, symbol := range missed {
				if i == 15 {
					break
				}
				fmt.Fprintf(&b, "|`%s` (%s:%d)|%s|%s|%s|\n", symbol.Name, symbol.Path, symbol.Line, symbol.Kind, strings.Join(symbol.Missed, ", "), symbol.Verdict)
			}
		}
		var extra []symbolResult
		for _, symbol := range language.Symbols {
			if len(symbol.Extra) > 0 {
				extra = append(extra, symbol)
			}
		}
		if len(extra) > 0 {
			fmt.Fprintf(&b, "\nDeclarations with a reported file the compiler did not flag (first 15 of %d). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.\n\n|declaration|kind|extra|\n|---|---|---|\n", len(extra))
			for i, symbol := range extra {
				if i == 15 {
					break
				}
				fmt.Fprintf(&b, "|`%s` (%s:%d)|%s|%s|\n", symbol.Name, symbol.Path, symbol.Line, symbol.Kind, strings.Join(symbol.Extra, ", "))
			}
		}
	}
	fmt.Fprintf(&b, "\n## What this does not show\n\n")
	for _, limitation := range measured.Limitations {
		fmt.Fprintf(&b, "- %s\n", limitation)
	}
	return b.String()
}
