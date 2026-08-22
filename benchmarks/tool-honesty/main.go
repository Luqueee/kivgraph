// Command tool-honesty drives every MCP tool of a real binary against a corpus
// with a deliberate blind spot, and checks one invariant: no tool asserts an
// absence the graph cannot support.
//
// It exists because the defect it guards against is invisible to every other
// harness here. A measurement compares an index against a written truth and
// answers "are the edges right". This one asks the question a session actually
// asks -- "does anything use this?" -- and reads what the tool says when the
// answer is no. `find_references` used to answer an empty list with "the edges
// are type-checked, so this is an absence rather than a miss" while the index
// held a recorded failure naming that very symbol. Nothing measured that,
// because nothing was wrong with the edges.
//
// The corpus is two repositories. `pure` has nothing the index cannot read, so
// every answer about it must be COMPLETE: without it the verdict could be a
// constant. `blinded` loads and holds a package excluded by a build tag, which
// the pass records as PACKAGE_NOT_BUILDABLE with no file -- a scope that bounds
// every answer about that repository, whatever it was asked.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	benchmarkName    = "tool-honesty"
	defaultDirectory = "benchmarks/tool-honesty"
	fixtureRoot      = "testdata/honesty"
	command          = "go run ./benchmarks/tool-honesty --kivgraph <binary>"
	callTimeout      = 60 * time.Second
	// gate is the token this corpus can justify. Two Go repositories prove the
	// invariant holds and that it is not a constant; they do not prove it holds
	// for every language and every shape of failure.
	gate = "TOOL_HONESTY_PASS_WITH_LIMITS"
)

// verdict values the tools publish. They are duplicated here on purpose: a
// harness that imported them could not catch a rename that breaks every client.
const (
	verdictComplete   = "COMPLETE"
	verdictLowerBound = "LOWER_BOUND"
)

// The arms. Go always runs; Rust runs when its toolchain answers.
const (
	languageGo   = "go"
	languageRust = "rust"
)

// check is one question and what the answer may claim. Want is the verdict the
// graph can support; a tool that publishes no verdict for a question whose
// empty answer reads as proof is the defect this harness exists to catch.
type check struct {
	Name string `json:"name"`
	Tool string `json:"tool"`
	// Language names the arm. A check is skipped, and said to be skipped, when
	// its language has no working toolchain: a missing analyzer must not turn
	// into a FAIL that points at the wrong place.
	Language string `json:"language"`
	// Why says what a reader loses if this check stops holding.
	Why       string         `json:"why"`
	Arguments map[string]any `json:"arguments"`
	// WantVerdict is the verdict this answer must carry. Empty means the tool
	// may omit it, which is only allowed when the answer claims no absence.
	WantVerdict string `json:"want_verdict"`
	// WantEmpty says the answer must have no rows: a check that silently
	// started answering something else would stop testing what it names.
	WantEmpty bool `json:"want_empty"`
	// ForbidPhrase must not appear in the guidance.
	ForbidPhrase string `json:"forbid_phrase,omitempty"`
	// WantPhrase must appear in the guidance.
	WantPhrase string `json:"want_phrase,omitempty"`
	// WantError says the tool refuses the question instead of answering an
	// empty list, which is the other honest shape.
	WantError bool `json:"want_error"`
}

type checkResult struct {
	Name        string `json:"name"`
	Tool        string `json:"tool"`
	Language    string `json:"language"`
	Passed      bool   `json:"passed"`
	Skipped     bool   `json:"skipped,omitempty"`
	SkipReason  string `json:"skip_reason,omitempty"`
	Total       int    `json:"total"`
	Verdict     string `json:"verdict"`
	BlindSpots  int    `json:"blind_spots"`
	Scopes      int    `json:"invisible_scopes"`
	Guidance    string `json:"guidance,omitempty"`
	Failure     string `json:"failure,omitempty"`
	Errored     bool   `json:"errored"`
	ErrorDetail string `json:"error_detail,omitempty"`
}

type report struct {
	Benchmark   string        `json:"benchmark"`
	Command     string        `json:"command"`
	Date        string        `json:"date"`
	Environment environment   `json:"environment"`
	Corpus      corpus        `json:"corpus"`
	Checks      []checkResult `json:"checks"`
	Totals      totals        `json:"totals"`
	Gate        string        `json:"gate"`
	Findings    []string      `json:"findings"`
	Limitations []string      `json:"limitations"`
}

type environment struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Go   string `json:"go"`
	// Rust records the toolchain the Rust arm ran against, or why it did not.
	Rust string `json:"rust"`
}

type corpus struct {
	// Languages are the arms that actually ran. A corpus that quietly lost one
	// would still report every check of the other as passing.
	Languages    []string `json:"languages"`
	Repositories []string `json:"repositories"`
	Symbols      int      `json:"symbols"`
	Unresolved   int      `json:"unresolved"`
	// Reasons is the server's own account of why: a scope count with no reason
	// behind it could not tell a fixture that changed from one that broke.
	Reasons []string `json:"unresolved_by_reason"`
	// Scopes is what makes a blinded arm blinded. Zero means the fixtures
	// stopped producing the failures they exist to produce, and every
	// LOWER_BOUND check below would pass for the wrong reason.
	Scopes int `json:"invisible_scopes"`
	// ScopesByLanguage is the same count per arm, which is what proves the two
	// blind spots are independent: a Go answer must not be bounded by a Rust
	// failure, and that is only checkable if each arm has its own.
	ScopesByLanguage map[string]int `json:"invisible_scopes_by_language"`
}

type totals struct {
	Checks  int `json:"checks"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// envelope is the part of every tool answer this harness reads.
type envelope struct {
	Total        int `json:"total"`
	Returned     int `json:"returned"`
	Completeness *struct {
		Verdict         string           `json:"verdict"`
		BlindSpots      []map[string]any `json:"blind_spots"`
		InvisibleScopes []map[string]any `json:"invisible_scopes"`
	} `json:"completeness"`
	Guidance string `json:"guidance"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
}

type config struct {
	Kivgraph  string
	Directory string
}

func run(ctx context.Context) error {
	var cfg config
	flag.StringVar(&cfg.Kivgraph, "kivgraph", "", "path to a kivgraph binary built with the ladybug tag")
	flag.StringVar(&cfg.Directory, "output", defaultDirectory, "directory for results.json and report.md")
	flag.Parse()
	if strings.TrimSpace(cfg.Kivgraph) == "" {
		return errors.New("-kivgraph is required: this harness measures a real binary, not a hand-built snapshot")
	}
	binary, err := filepath.Abs(cfg.Kivgraph)
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("binary %q is not available: %w", binary, err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required to register a repository: %w", err)
	}

	// The Rust arm needs a toolchain, and its absence is declared rather than
	// turned into a failure that points at the wrong place.
	rustToolchain, rustAnalyzer := resolveRust(ctx)
	languages := []string{languageGo}
	if rustAnalyzer != "" {
		languages = append(languages, languageRust)
	}

	workspace, home, err := prepareCorpus(ctx, languages)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(workspace))

	if err := index(ctx, binary, home, workspace, languages, rustAnalyzer); err != nil {
		return err
	}

	session, stop, err := connect(ctx, binary, home)
	if err != nil {
		return err
	}
	defer stop()

	measured := report{
		Benchmark: benchmarkName,
		Command:   command,
		Date:      time.Now().UTC().Format("2006-01-02"),
		Environment: environment{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version(), Rust: rustToolchain,
		},
		Findings:    findings(),
		Limitations: limitations(),
	}
	measured.Corpus, err = readCorpus(ctx, session, languages)
	if err != nil {
		return err
	}
	measured.Corpus.Languages = languages
	for _, language := range languages {
		if measured.Corpus.ScopesByLanguage[language] == 0 {
			return fmt.Errorf("the blinded %s fixture produced no unreadable scope: every lower-bound check of that arm would pass for the wrong reason", language)
		}
	}

	running := make(map[string]bool, len(languages))
	for _, language := range languages {
		running[language] = true
	}
	for _, testCase := range checks() {
		measured.Totals.Checks++
		if !running[testCase.Language] {
			measured.Checks = append(measured.Checks, checkResult{
				Name: testCase.Name, Tool: testCase.Tool, Language: testCase.Language,
				Skipped: true, SkipReason: rustToolchain,
			})
			measured.Totals.Skipped++
			continue
		}
		result := measure(ctx, session, testCase)
		measured.Checks = append(measured.Checks, result)
		if result.Passed {
			measured.Totals.Passed++
			continue
		}
		measured.Totals.Failed++
	}
	measured.Gate = gate

	if err := writeResults(cfg.Directory, measured); err != nil {
		return err
	}
	fmt.Println(measured.Gate)
	if measured.Totals.Failed != 0 {
		for _, result := range measured.Checks {
			if !result.Passed && !result.Skipped {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", result.Name, result.Failure)
			}
		}
		return fmt.Errorf("%d of %d checks failed", measured.Totals.Failed, measured.Totals.Checks)
	}
	fmt.Printf("  %d checks over %s, %d passed, %d skipped\n",
		measured.Totals.Checks, strings.Join(measured.Corpus.Languages, " and "),
		measured.Totals.Passed, measured.Totals.Skipped)
	return nil
}

// checks is the table. Every row names the claim it prices, and the pure
// repository is measured beside the blinded one on purpose: a verdict that
// never says COMPLETE is decoration, and one that never says LOWER_BOUND is a
// lie waiting for a corpus.
//
// The two arms are one corpus rather than two runs, which buys a check neither
// could make alone: an answer scoped to a Go repository must stay COMPLETE
// while a Rust workspace of the same graph is unreadable, and the other way
// round. A verdict that leaked across languages would be a constant on any
// polyglot monorepo -- which is the only kind this product is for.
func checks() []check {
	return []check{
		{
			Name:     "a declaration lookup that finds nothing while a package is unreadable",
			Tool:     "find_symbol",
			Language: languageGo,
			Why: "`Shadow` is declared in the excluded package and nowhere else. " +
				"Answering `nothing declares this name` without the bound is what sends an agent to invent it.",
			Arguments:   map[string]any{"name": "Shadow", "mode": "exact"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			WantPhrase: "completeness",
		},
		{
			Name:     "the same lookup narrowed to the repository that can answer it",
			Tool:     "find_symbol",
			Language: languageGo,
			Why: "A search narrowed to `go-pure` is not bounded by another repository's blind spot. " +
				"Charging it for one would make the verdict a constant on any corpus with a single bad package.",
			Arguments:   map[string]any{"name": "Shadow", "mode": "exact", "repo": "go-pure"},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name:        "a declaration lookup that finds nothing in a corpus that can answer",
			Tool:        "find_symbol",
			Language:    languageGo,
			Why:         "The control. `Absent` is in no source at all, and inside `go-pure` that is a real absence.",
			Arguments:   map[string]any{"name": "Absent", "mode": "exact", "repo": "go-pure"},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name:     "who calls a symbol nobody calls, in a repository with nothing hidden",
			Tool:     "find_references",
			Language: languageGo,
			Why: "`Lonely` is declared and never used. This is the answer the product is bought for, " +
				"and here it is a real absence: COMPLETE is the claim, and it has to be earned.",
			Arguments:   map[string]any{"name": "Lonely", "direction": "incoming"},
			WantVerdict: verdictComplete, WantEmpty: true,
			WantPhrase: "absence rather than a miss",
		},
		{
			Name:     "who calls a symbol in a repository the index could not fully read",
			Tool:     "find_references",
			Language: languageGo,
			Why: "`Visible` is used only from the excluded package, which the graph cannot see. " +
				"An empty answer here is a minimum, and calling it an absence is the defect.",
			Arguments:   map[string]any{"name": "Visible", "direction": "incoming"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			ForbidPhrase: "absence rather than a miss",
		},
		{
			Name:        "what a symbol reaches, outward, with nothing hidden",
			Tool:        "trace_dependencies",
			Language:    languageGo,
			Why:         "`Lonely` reaches nothing, and in `go-pure` that is the whole answer.",
			Arguments:   map[string]any{"repository": "go-pure", "path": "pure.go", "qualified_name": "Lonely", "depth": 3},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name:     "what a symbol reaches when its repository holds an unreadable package",
			Tool:     "trace_dependencies",
			Language: languageGo,
			Why: "An outward answer is bounded by the failures this symbol's repository holds. " +
				"The bound is a different question from `who asked for this name`, and it used to go unasked.",
			Arguments:   map[string]any{"repository": "go-blinded", "path": "visible.go", "qualified_name": "Visible", "depth": 3},
			WantVerdict: verdictLowerBound,
		},
		{
			Name:        "what breaks if I change this, with nothing hidden",
			Tool:        "get_blast_radius",
			Language:    languageGo,
			Why:         "The impact question over a repository the index read whole.",
			Arguments:   map[string]any{"repository": "go-pure", "path": "pure.go", "qualified_name": "Lonely", "depth": 2},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name:     "who uses this from another repository",
			Tool:     "find_cross_repo_consumers",
			Language: languageGo,
			Why: "This tool has no native competitor, so its empty answer is sold as a finding: " +
				"nobody outside uses this. A package unreadable in any repository can hide exactly that consumer, " +
				"which is why its scope check is global and not per repository.",
			Arguments:   map[string]any{"repository": "go-pure", "path": "pure.go", "qualified_name": "Reachable"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			ForbidPhrase: "no repository in the published graph consumes this symbol",
		},
		{
			Name:        "what is declared under a path the index read whole",
			Tool:        "get_file_outline",
			Language:    languageGo,
			Why:         "An outline of a clean repository claims nothing it cannot support.",
			Arguments:   map[string]any{"repository": "go-pure", "path": "pure.go"},
			WantVerdict: "",
		},
		{
			Name:     "what is declared under a path whose repository hides a package",
			Tool:     "get_file_outline",
			Language: languageGo,
			Why: "An outline that lists declarations while a package of the same repository is invisible " +
				"is a minimum. There is no symbol name here, so the only failure that can bound it is the scope.",
			Arguments:   map[string]any{"repository": "go-blinded", "path": "visible.go"},
			WantVerdict: verdictLowerBound,
		},
		{
			Name:     "a symbol that is not there, asked of the reader",
			Tool:     "get_symbol",
			Language: languageGo,
			Why: "get_symbol and get_source answer about a symbol the caller already named. " +
				"They refuse a miss instead of answering an empty list, which is the other honest shape: " +
				"if that ever changes into an empty answer, it needs a verdict like the rest.",
			Arguments: map[string]any{"stable_key": "there-is-no-such-key"},
			WantError: true,
		},
		{
			Name:     "source for a symbol that is not there",
			Tool:     "get_source",
			Language: languageGo,
			Why:      "Same shape, same reason.",
			Arguments: map[string]any{"symbols": []any{
				map[string]any{"stable_key": "there-is-no-such-key"},
			}},
			WantError: true,
		},

		// The Rust arm. Its blind spot is a different reason from Go's --
		// `WORKSPACE_NOT_LOADED`, a workspace naming a member that is not
		// there -- so the arm prices the invariant rather than one loader.
		{
			Name:     "who calls a Rust symbol nobody calls, with nothing hidden",
			Tool:     "find_references",
			Language: languageRust,
			Why: "`rust_lonely` is declared and never used. The Rust control: without it, a constant " +
				"lower bound would pass every Rust check below.",
			Arguments:   map[string]any{"name": "rust_lonely", "direction": "incoming"},
			WantVerdict: verdictComplete, WantEmpty: true,
			WantPhrase: "absence rather than a miss",
		},
		{
			Name:     "who calls a Rust symbol whose repository holds a workspace that failed to load",
			Tool:     "find_references",
			Language: languageRust,
			Why: "`rust_visible` is used only from the crate in the broken workspace. The analyzer never " +
				"produced a crate graph for it, so the empty answer is a minimum -- and the reason is " +
				"recorded without a file, exactly like the Go one, which is what makes the invariant " +
				"about the shape of a failure and not about a language.",
			Arguments:   map[string]any{"name": "rust_visible", "direction": "incoming"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			ForbidPhrase: "absence rather than a miss",
		},
		{
			Name:     "a Rust declaration that only the unreadable workspace has",
			Tool:     "find_symbol",
			Language: languageRust,
			Why: "`rust_shadow` is in the source and in no graph. Narrowed to the Rust repository that " +
				"can answer, the same question is an absence; unnarrowed it is a minimum.",
			Arguments:   map[string]any{"name": "rust_shadow", "mode": "exact"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
		},
		{
			Name:     "a Go answer is not bounded by a Rust blind spot",
			Tool:     "find_references",
			Language: languageRust,
			Why: "This is the check neither arm could make alone, and the reason the two share one corpus. " +
				"`Lonely` lives in a Go repository the index read whole; the graph also holds an unreadable " +
				"Rust workspace. A verdict that leaked across languages would be a constant on every " +
				"polyglot monorepo, which is the only kind this product is for.",
			Arguments:   map[string]any{"name": "Lonely", "direction": "incoming"},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name:        "a Rust answer is not bounded by a Go blind spot",
			Tool:        "find_references",
			Language:    languageRust,
			Why:         "The mirror of the previous check, so neither direction can pass by accident.",
			Arguments:   map[string]any{"name": "rust_reachable", "direction": "incoming"},
			WantVerdict: verdictComplete,
		},
	}
}

func measure(ctx context.Context, session *sdkmcp.ClientSession, testCase check) checkResult {
	result := checkResult{Name: testCase.Name, Tool: testCase.Tool, Language: testCase.Language}
	callContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callContext, &sdkmcp.CallToolParams{
		Name: testCase.Tool, Arguments: testCase.Arguments,
	})
	text := ""
	if response != nil {
		for _, content := range response.Content {
			if block, ok := content.(*sdkmcp.TextContent); ok {
				text = block.Text
				break
			}
		}
	}
	failed := err != nil || (response != nil && response.IsError)
	result.Errored = failed
	if failed {
		result.ErrorDetail = strings.TrimSpace(text)
		if err != nil {
			result.ErrorDetail = err.Error()
		}
	}
	if testCase.WantError {
		result.Passed = failed
		if !failed {
			result.Failure = "the tool answered instead of refusing, so an absence here needs a verdict"
		}
		return result
	}
	if failed {
		result.Failure = "the call failed: " + result.ErrorDetail
		return result
	}

	var decoded envelope
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		result.Failure = fmt.Sprintf("decode answer: %v", err)
		return result
	}
	result.Total = decoded.Total
	result.Guidance = decoded.Guidance
	if decoded.Completeness != nil {
		result.Verdict = decoded.Completeness.Verdict
		result.BlindSpots = len(decoded.Completeness.BlindSpots)
		result.Scopes = len(decoded.Completeness.InvisibleScopes)
	}

	if testCase.WantEmpty && decoded.Total != 0 {
		result.Failure = fmt.Sprintf("answer holds %d rows, and this check prices what the tool says when it holds none", decoded.Total)
		return result
	}
	if testCase.WantVerdict != "" && result.Verdict != testCase.WantVerdict {
		result.Failure = fmt.Sprintf("verdict is %q, want %q", result.Verdict, testCase.WantVerdict)
		return result
	}
	if testCase.WantVerdict == verdictLowerBound && result.BlindSpots+result.Scopes == 0 {
		result.Failure = "the verdict is a lower bound and names nowhere to look"
		return result
	}
	if testCase.ForbidPhrase != "" && strings.Contains(decoded.Guidance, testCase.ForbidPhrase) {
		result.Failure = fmt.Sprintf("guidance claims %q over a recorded blind spot", testCase.ForbidPhrase)
		return result
	}
	if testCase.WantPhrase != "" && !strings.Contains(decoded.Guidance, testCase.WantPhrase) {
		result.Failure = fmt.Sprintf("guidance does not say %q, so nothing points the caller anywhere", testCase.WantPhrase)
		return result
	}
	result.Passed = true
	return result
}

// readCorpus asks the server what it is serving. A harness that assumed the
// corpus would keep passing after the fixture stopped producing its failure.
func readCorpus(ctx context.Context, session *sdkmcp.ClientSession, languages []string) (corpus, error) {
	callContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callContext, &sdkmcp.CallToolParams{
		Name: "graph_status", Arguments: map[string]any{},
	})
	if err != nil {
		return corpus{}, fmt.Errorf("graph_status: %w", err)
	}
	text := ""
	for _, content := range response.Content {
		if block, ok := content.(*sdkmcp.TextContent); ok {
			text = block.Text
			break
		}
	}
	var decoded struct {
		Results struct {
			Symbols      int `json:"symbols"`
			Unresolved   int `json:"unresolved"`
			Repositories []struct {
				Name string `json:"name"`
			} `json:"repository_freshness"`
			UnresolvedByReason []struct {
				Key   string `json:"key"`
				Count int    `json:"count"`
			} `json:"unresolved_by_reason"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return corpus{}, fmt.Errorf("decode graph_status: %w", err)
	}
	measured := corpus{
		Symbols:    decoded.Results.Symbols,
		Unresolved: decoded.Results.Unresolved,
	}
	for _, repository := range decoded.Results.Repositories {
		measured.Repositories = append(measured.Repositories, repository.Name)
	}
	sort.Strings(measured.Repositories)
	// The blind spot is read from the server's own account of what it holds,
	// because a fixture is only a claim until the pass records the failure it
	// was written to produce. Every reason here is recorded without a file,
	// which is what makes it bound every answer about its repository instead
	// of one reference, and each arm has its own: Go excludes a package by
	// build tag, Rust names a workspace member that is not there.
	measured.ScopesByLanguage = make(map[string]int, len(languages))
	for _, reason := range decoded.Results.UnresolvedByReason {
		measured.Reasons = append(measured.Reasons, fmt.Sprintf("%s=%d", reason.Key, reason.Count))
		if language, isScope := scopeReasons[reason.Key]; isScope {
			measured.Scopes += reason.Count
			measured.ScopesByLanguage[language] += reason.Count
		}
	}
	sort.Strings(measured.Reasons)
	return measured, nil
}

// scopeReasons are the fileless failures this corpus produces, and the arm each
// one belongs to. Keeping the language beside the reason is what lets the run
// assert that both arms are blinded independently: one shared counter could not
// tell two blind spots from one fixture doing all the work.
var scopeReasons = map[string]string{
	"PACKAGE_NOT_BUILDABLE": languageGo,
	"MODULE_NOT_LOADED":     languageGo,
	"WORKSPACE_NOT_LOADED":  languageRust,
	"ANALYZER_UNAVAILABLE":  languageRust,
	"TARGET_NOT_BUILDABLE":  languageRust,
}

// fixtures are the repositories of each arm, in registration order.
var fixtures = map[string][]string{
	languageGo:   {"go-pure", "go-blinded"},
	languageRust: {"rust-pure", "rust-blinded"},
}

// resolveRust reports the Rust toolchain the arm will use, and the analyzer to
// run. An empty analyzer means the arm is skipped, and the first result says
// why.
//
// The pinned analyzer is preferred over the PATH for the reason
// `internal/rustloader/AGENTS.md` gives: it is the one the bundle ships and the
// one whose version the project fixed. A rustup proxy on the PATH resolves to
// whatever default the machine has, or to nothing at all.
func resolveRust(ctx context.Context) (string, string) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return "unsupported platform for the pinned analyzer", ""
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		return "skipped: cargo is not on the PATH", ""
	}
	directory, err := exec.CommandContext(ctx, "scripts/fetch-rust-analyzer.sh").Output()
	if err != nil {
		return "skipped: scripts/fetch-rust-analyzer.sh did not resolve an analyzer", ""
	}
	analyzer := filepath.Join(strings.TrimSpace(string(directory)), "rust-analyzer")
	if _, err := os.Stat(analyzer); err != nil {
		return "skipped: the pinned analyzer is not present", ""
	}
	// rust-analyzer shells out to rustc for the sysroot, so a toolchain that
	// cannot answer means no workspace loads and the clean arm would be
	// indistinguishable from the blinded one.
	version, err := exec.CommandContext(ctx, "rustc", "--version").Output()
	if err != nil {
		return "skipped: rustc does not answer, so no workspace can load", ""
	}
	return strings.TrimSpace(string(version)), analyzer
}

// prepareCorpus copies the fixtures out of the repository and makes each copy a
// git repository, because the registry takes one git repository per entry. The
// fixtures themselves are never touched.
func prepareCorpus(ctx context.Context, languages []string) (string, string, error) {
	root, err := os.MkdirTemp("", "kivgraph-honesty-*")
	if err != nil {
		return "", "", fmt.Errorf("create corpus directory: %w", err)
	}
	// The workspace layer refuses a path with a symlink component, and the
	// temporary directory of macOS is one.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", "", fmt.Errorf("create home: %w", err)
	}
	for _, language := range languages {
		for _, name := range fixtures[language] {
			source := filepath.Join(fixtureRoot, name)
			target := filepath.Join(workspace, name)
			if err := os.CopyFS(target, os.DirFS(source)); err != nil {
				return "", "", fmt.Errorf("copy fixture %q: %w", source, err)
			}
			for _, arguments := range [][]string{
				{"init", "-q", "."},
				{"add", "-A"},
				{"-c", "user.email=bench@kivgraph", "-c", "user.name=bench", "commit", "-q", "-m", "fixture"},
			} {
				process := exec.CommandContext(ctx, "git", arguments...)
				process.Dir = target
				if output, err := process.CombinedOutput(); err != nil {
					return "", "", fmt.Errorf("git %s in %q: %w: %s", arguments[0], target, err, strings.TrimSpace(string(output)))
				}
			}
		}
	}
	return workspace, home, nil
}

func index(ctx context.Context, binary, home, workspace string, languages []string, rustAnalyzer string) error {
	initArguments := []string{"init", "--languages", strings.Join(languages, ",")}
	for _, language := range languages {
		for _, name := range fixtures[language] {
			initArguments = append(initArguments, "--repository", name+"="+filepath.Join(workspace, name))
		}
	}
	environment := indexEnvironment(home)
	for _, arguments := range [][]string{initArguments, {"index", "--full"}} {
		if arguments[0] == "index" && rustAnalyzer != "" {
			if err := pinAnalyzer(home, rustAnalyzer); err != nil {
				return err
			}
		}
		process := exec.CommandContext(ctx, binary, arguments...)
		process.Env = environment
		if output, err := process.CombinedOutput(); err != nil {
			return fmt.Errorf("kivgraph %s: %w: %s", arguments[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

// indexEnvironment isolates the state of Kivgraph without isolating the
// toolchains it drives.
//
// Measured while writing this: pointing HOME at a temporary directory makes
// rustup lose every toolchain, because RUSTUP_HOME defaults to $HOME/.rustup.
// The whole Rust corpus then fails to load and the clean arm becomes
// indistinguishable from the blinded one -- a green run that measured nothing.
// So the two are separated: HOME moves, the toolchain locations do not.
func indexEnvironment(home string) []string {
	environment := append(os.Environ(), "HOME="+home)
	if real, err := os.UserHomeDir(); err == nil {
		if _, set := os.LookupEnv("RUSTUP_HOME"); !set {
			environment = append(environment, "RUSTUP_HOME="+filepath.Join(real, ".rustup"))
		}
		if _, set := os.LookupEnv("CARGO_HOME"); !set {
			environment = append(environment, "CARGO_HOME="+filepath.Join(real, ".cargo"))
		}
	}
	return environment
}

// pinAnalyzer points the configuration at the analyzer this run resolved. The
// generated configuration names `rust-analyzer` on the PATH, which on a machine
// with a rustup proxy and no default toolchain resolves to a binary that
// refuses to run.
func pinAnalyzer(home, analyzer string) error {
	path := filepath.Join(home, ".config", "kivgraph", "config.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the generated configuration: %w", err)
	}
	const declared = "analyzer_command: rust-analyzer\n"
	if !strings.Contains(string(content), declared) {
		return fmt.Errorf("configuration %q does not declare the Rust analyzer command", path)
	}
	replaced := strings.Replace(string(content), declared, "analyzer_command: "+analyzer+"\n", 1)
	return os.WriteFile(path, []byte(replaced), 0o644)
}

func connect(ctx context.Context, binary, home string) (*sdkmcp.ClientSession, func(), error) {
	process := exec.Command(binary, "serve")
	process.Env = indexEnvironment(home)
	process.Stderr = os.NewFile(uintptr(0), os.DevNull)
	transport := &sdkmcp.CommandTransport{Command: process, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("connect to the server: %w", err)
	}
	return session, func() {
		_ = session.Close()
		cancel()
	}, nil
}

func findings() []string {
	return []string{
		"`find_references` answered an empty list with «the edges are type-checked, so this is an absence rather than a miss» while the index held an unresolved row naming that very symbol. The row was there, with its file and its line, and the tool did not read it: `addReferenceCoverage` counted edges whose confidence is Unresolved, which is a different fact. It is the check this harness exists to keep.",
		"Six tools answer a question whose empty or partial answer reads as proof, and until this phase two published a verdict. The other four -- `trace_dependencies`, `find_cross_repo_consumers`, `find_symbol`, `get_file_outline` -- said nothing, while `internal/mcp/instructions.go` told every agent to «read confidence and completeness before treating an empty or partial answer as proof of absence».",
		"An outward question is bounded by a different set of failures than an inward one. «Who calls this» is bounded by failures that asked for the name; «what does this reach» by failures the symbol itself made. Asking the naming question for a traversal would have missed every one of them, so `UnresolvedFromSymbol` exists for that direction.",
		"The scope of the check follows the scope of the question, and that is what keeps the verdict from becoming a constant. A search of the whole graph is bounded by every unreadable package in it; one narrowed to a repository only by that repository's. `find_cross_repo_consumers` is deliberately the other way round: a package unreadable anywhere can hide the consumer it is asked about.",
		"`get_symbol` and `get_source` refuse a symbol they cannot find instead of answering an empty list, so they claim no absence and need no verdict. The two checks here pin that shape: if either ever answers empty, it needs a verdict like the rest.",
		"Measured, not assumed: `\"completeness\":{\"verdict\":\"COMPLETE\"}` is 10 tokens under `cl100k_base` -- 16 % of a one-row `find_symbol` answer and 50 % of an empty one. So a lookup spends it where the answer could be mistaken for a proof (empty, partial) and on every lower bound, while the four relational tools always carry it: for «who calls this» and «what breaks if I change this», COMPLETE on a non-empty answer is the claim being bought.",
		"The invariant is about the shape of a failure, not about a language. A scope is a recorded failure with no file (`blindspots.go:88` filters on exactly that), and the two arms reach it through different reasons: Go excludes a package by build tag (`PACKAGE_NOT_BUILDABLE`), Rust names a workspace member that is not there (`WORKSPACE_NOT_LOADED`). Same envelope, same verdict, two loaders.",
		"The five languages split into two honest shapes when a whole repository fails, and nothing documented which did which. Go and Rust record a fileless row and continue, so the answers about that repository become lower bounds. TypeScript, Python and Dart return an error instead -- `semantic.go:83`, `full.go:1388` -- and the pass aborts without publishing, so no answer is served at all. Both refuse to claim an absence; only the first is measurable from a served graph.",
		"A verdict must not leak across languages, and one corpus is what makes that checkable. A Go answer scoped to a Go repository stays COMPLETE while a Rust workspace of the same graph is unreadable, and the mirror holds too. Leaking would make the verdict a constant on every polyglot monorepo, which is the only kind this product is for.",
		"Isolating HOME breaks the Rust toolchain, and it broke this harness first. `RUSTUP_HOME` defaults to `$HOME/.rustup`, so a temporary HOME leaves rustup with no toolchains, `rustc` stops answering, and every Rust workspace fails to load -- which would have made the clean arm indistinguishable from the blinded one and passed green having measured nothing. `indexEnvironment` moves HOME and leaves the toolchain locations alone.",
		"Measured while choosing the Rust fixture: an unresolvable *dependency* is not a blind spot. rust-analyzer loads the workspace anyway and degrades, so a crate depending on a package that cannot exist produced eight symbols and zero failures. A workspace naming a member directory that does not exist is what Cargo cannot resolve, and it is deterministic with or without network access.",
	}
}

func limitations() []string {
	return []string{
		"The corpus is two Go and two Rust repositories: it proves the invariant holds across the two languages that record a fileless failure and continue, and that the verdict is neither a constant nor leaky between them. TypeScript, Python and Dart abort the pass instead, so no served graph of theirs can be measured this way and none is measured here.",
		"Two of the three Rust scope reasons are unexercised. `WORKSPACE_NOT_LOADED` is the one this fixture produces; `ANALYZER_UNAVAILABLE` needs a missing analyzer and `TARGET_NOT_BUILDABLE` was not reachable by fixture at all -- an excluded crate is discovered as its own workspace and loads, so `collectEmptyCrates` never fires here.",
		"`MACRO_EXPANSION_DISABLED` is emitted for every Rust repository whenever `proc_macros` is off, which makes every answer about that repository a lower bound. It is literally true -- the index is incomplete by configuration and says so -- but it means the Rust verdict carries no information under that setting. The default is on, and this corpus runs with the default.",
		"This harness reads the envelope, not the rows: it prices what a tool claims about its own completeness, and says nothing about whether the edges are right. That is what `benchmarks/go-semantic`, `rust-semantic`, `python-semantic` and `dart-semantic` measure.",
		"It needs a binary built with the `ladybug` tag, and git on the PATH; the Rust arm also needs the pinned analyzer and a `rustc` that answers. The binary and git are required, and the Rust arm is skipped with its reason recorded rather than faked or failed.",
	}
}

func writeResults(directory string, out report) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", directory, err)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results.json: %w", err)
	}
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(render(out)), 0o644)
}

func render(out report) string {
	var text strings.Builder
	text.WriteString("# Ninguna tool afirma una ausencia que no ha comprobado\n\n")
	text.WriteString("Este arnés no mide si las aristas están bien: eso lo miden los cuatro arneses\n")
	text.WriteString("semánticos. Mide **qué dice una tool cuando su respuesta está vacía**, que es\n")
	text.WriteString("la clase de defecto que ninguna medición veía y que sólo aparece usando el\n")
	text.WriteString("binario.\n\n")
	text.WriteString("Dos lenguajes en un solo corpus, y eso es el diseño: el invariante es sobre la\n")
	text.WriteString("**forma de un fallo** -- una fila sin archivo--, no sobre un cargador. Cada\n")
	text.WriteString("brazo llega a ella por un motivo distinto, y el corpus compartido permite la\n")
	text.WriteString("comprobación que ninguno haría solo: que un punto ciego de Rust no acote una\n")
	text.WriteString("respuesta de Go, ni al revés.\n\n")
	fmt.Fprintf(&text, "|dato|valor|\n|---|---|\n")
	fmt.Fprintf(&text, "|comando|`%s`|\n", out.Command)
	fmt.Fprintf(&text, "|fecha|%s|\n", out.Date)
	fmt.Fprintf(&text, "|plataforma|%s/%s, %s|\n", out.Environment.OS, out.Environment.Arch, out.Environment.Go)
	fmt.Fprintf(&text, "|toolchain Rust|%s|\n", out.Environment.Rust)
	fmt.Fprintf(&text, "|brazos|%s|\n", strings.Join(out.Corpus.Languages, ", "))
	fmt.Fprintf(&text, "|corpus|%s|\n", strings.Join(out.Corpus.Repositories, ", "))
	fmt.Fprintf(&text, "|símbolos|%d|\n", out.Corpus.Symbols)
	fmt.Fprintf(&text, "|no resueltas|%d (%s)|\n", out.Corpus.Unresolved, strings.Join(out.Corpus.Reasons, ", "))
	fmt.Fprintf(&text, "|ámbitos ilegibles|%d|\n", out.Corpus.Scopes)
	for _, language := range out.Corpus.Languages {
		fmt.Fprintf(&text, "|ámbitos de `%s`|%d|\n", language, out.Corpus.ScopesByLanguage[language])
	}
	fmt.Fprintf(&text, "|comprobaciones|%d, %d pasan, %d saltadas|\n\n",
		out.Totals.Checks, out.Totals.Passed, out.Totals.Skipped)

	text.WriteString("## Comprobaciones\n\n")
	text.WriteString("|brazo|tool|pregunta|filas|veredicto|estado|\n|---|---|---|---|---|---|\n")
	for _, result := range out.Checks {
		state := "ok"
		switch {
		case result.Skipped:
			state = "saltada"
		case !result.Passed:
			state = "**FALLA**"
		}
		verdict := result.Verdict
		if verdict == "" {
			verdict = "-"
		}
		if result.Errored {
			verdict = "rechaza"
		}
		if result.Skipped {
			verdict = "-"
		}
		fmt.Fprintf(&text, "|`%s`|`%s`|%s|%d|`%s`|%s|\n",
			result.Language, result.Tool, result.Name, result.Total, verdict, state)
	}
	text.WriteString("\nCada fila dice qué se pierde si deja de cumplirse:\n\n")
	for _, result := range out.Checks {
		fmt.Fprintf(&text, "- **%s** (`%s`, `%s`)", result.Name, result.Language, result.Tool)
		switch {
		case result.Skipped:
			fmt.Fprintf(&text, " -- saltada: %s", result.SkipReason)
		case result.Failure != "":
			fmt.Fprintf(&text, " -- FALLA: %s", result.Failure)
		}
		text.WriteString("\n")
	}

	text.WriteString("\n## Hallazgos\n\n")
	for _, finding := range out.Findings {
		text.WriteString("- " + finding + "\n")
	}
	text.WriteString("\n## Limitaciones\n\n")
	for _, limitation := range out.Limitations {
		text.WriteString("- " + limitation + "\n")
	}
	fmt.Fprintf(&text, "\n## Gate\n\n```text\n%s\n```\n", out.Gate)
	return text.String()
}
