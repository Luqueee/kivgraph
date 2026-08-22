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

// check is one question and what the answer may claim. Want is the verdict the
// graph can support; a tool that publishes no verdict for a question whose
// empty answer reads as proof is the defect this harness exists to catch.
type check struct {
	Name string `json:"name"`
	Tool string `json:"tool"`
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
	Passed      bool   `json:"passed"`
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
}

type corpus struct {
	Repositories []string `json:"repositories"`
	Symbols      int      `json:"symbols"`
	Unresolved   int      `json:"unresolved"`
	// Reasons is the server's own account of why: a scope count with no reason
	// behind it could not tell a fixture that changed from one that broke.
	Reasons []string `json:"unresolved_by_reason"`
	// Scopes is what makes the blinded arm blinded. Zero means the fixture
	// stopped producing the failure it exists to produce, and every
	// LOWER_BOUND check below would pass for the wrong reason.
	Scopes int `json:"invisible_scopes"`
}

type totals struct {
	Checks int `json:"checks"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
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

	workspace, home, err := prepareCorpus(ctx)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(workspace))

	if err := index(ctx, binary, home, workspace); err != nil {
		return err
	}

	session, stop, err := connect(ctx, binary, home)
	if err != nil {
		return err
	}
	defer stop()

	measured := report{
		Benchmark:   benchmarkName,
		Command:     command,
		Date:        time.Now().UTC().Format("2006-01-02"),
		Environment: environment{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version()},
		Findings:    findings(),
		Limitations: limitations(),
	}
	measured.Corpus, err = readCorpus(ctx, session)
	if err != nil {
		return err
	}
	if measured.Corpus.Scopes == 0 {
		return errors.New("the blinded fixture produced no unreadable scope: every lower-bound check below would pass for the wrong reason")
	}

	for _, testCase := range checks() {
		result := measure(ctx, session, testCase)
		measured.Checks = append(measured.Checks, result)
		measured.Totals.Checks++
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
			if !result.Passed {
				fmt.Fprintf(os.Stderr, "  %s: %s\n", result.Name, result.Failure)
			}
		}
		return fmt.Errorf("%d of %d checks failed", measured.Totals.Failed, measured.Totals.Checks)
	}
	fmt.Printf("  %d checks, every one passed\n", measured.Totals.Checks)
	return nil
}

// checks is the table. Every row names the claim it prices, and the pure
// repository is measured beside the blinded one on purpose: a verdict that
// never says COMPLETE is decoration, and one that never says LOWER_BOUND is a
// lie waiting for a corpus.
func checks() []check {
	return []check{
		{
			Name: "a declaration lookup that finds nothing while a package is unreadable",
			Tool: "find_symbol",
			Why: "`Shadow` is declared in the excluded package and nowhere else. " +
				"Answering `nothing declares this name` without the bound is what sends an agent to invent it.",
			Arguments:   map[string]any{"name": "Shadow", "mode": "exact"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			WantPhrase: "completeness",
		},
		{
			Name: "the same lookup narrowed to the repository that can answer it",
			Tool: "find_symbol",
			Why: "A search narrowed to `pure` is not bounded by another repository's blind spot. " +
				"Charging it for one would make the verdict a constant on any corpus with a single bad package.",
			Arguments:   map[string]any{"name": "Shadow", "mode": "exact", "repo": "pure"},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name:        "a declaration lookup that finds nothing in a corpus that can answer",
			Tool:        "find_symbol",
			Why:         "The control. `Absent` is in no source at all, and inside `pure` that is a real absence.",
			Arguments:   map[string]any{"name": "Absent", "mode": "exact", "repo": "pure"},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name: "who calls a symbol nobody calls, in a repository with nothing hidden",
			Tool: "find_references",
			Why: "`Lonely` is declared and never used. This is the answer the product is bought for, " +
				"and here it is a real absence: COMPLETE is the claim, and it has to be earned.",
			Arguments:   map[string]any{"name": "Lonely", "direction": "incoming"},
			WantVerdict: verdictComplete, WantEmpty: true,
			WantPhrase: "absence rather than a miss",
		},
		{
			Name: "who calls a symbol in a repository the index could not fully read",
			Tool: "find_references",
			Why: "`Visible` is used only from the excluded package, which the graph cannot see. " +
				"An empty answer here is a minimum, and calling it an absence is the defect.",
			Arguments:   map[string]any{"name": "Visible", "direction": "incoming"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			ForbidPhrase: "absence rather than a miss",
		},
		{
			Name:        "what a symbol reaches, outward, with nothing hidden",
			Tool:        "trace_dependencies",
			Why:         "`Lonely` reaches nothing, and in `pure` that is the whole answer.",
			Arguments:   map[string]any{"repository": "pure", "path": "pure.go", "qualified_name": "Lonely", "depth": 3},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name: "what a symbol reaches when its repository holds an unreadable package",
			Tool: "trace_dependencies",
			Why: "An outward answer is bounded by the failures this symbol's repository holds. " +
				"The bound is a different question from `who asked for this name`, and it used to go unasked.",
			Arguments:   map[string]any{"repository": "blinded", "path": "visible.go", "qualified_name": "Visible", "depth": 3},
			WantVerdict: verdictLowerBound,
		},
		{
			Name:        "what breaks if I change this, with nothing hidden",
			Tool:        "get_blast_radius",
			Why:         "The impact question over a repository the index read whole.",
			Arguments:   map[string]any{"repository": "pure", "path": "pure.go", "qualified_name": "Lonely", "depth": 2},
			WantVerdict: verdictComplete, WantEmpty: true,
		},
		{
			Name: "who uses this from another repository",
			Tool: "find_cross_repo_consumers",
			Why: "This tool has no native competitor, so its empty answer is sold as a finding: " +
				"nobody outside uses this. A package unreadable in any repository can hide exactly that consumer, " +
				"which is why its scope check is global and not per repository.",
			Arguments:   map[string]any{"repository": "pure", "path": "pure.go", "qualified_name": "Reachable"},
			WantVerdict: verdictLowerBound, WantEmpty: true,
			ForbidPhrase: "no repository in the published graph consumes this symbol",
		},
		{
			Name:        "what is declared under a path the index read whole",
			Tool:        "get_file_outline",
			Why:         "An outline of a clean repository claims nothing it cannot support.",
			Arguments:   map[string]any{"repository": "pure", "path": "pure.go"},
			WantVerdict: "",
		},
		{
			Name: "what is declared under a path whose repository hides a package",
			Tool: "get_file_outline",
			Why: "An outline that lists declarations while a package of the same repository is invisible " +
				"is a minimum. There is no symbol name here, so the only failure that can bound it is the scope.",
			Arguments:   map[string]any{"repository": "blinded", "path": "visible.go"},
			WantVerdict: verdictLowerBound,
		},
		{
			Name: "a symbol that is not there, asked of the reader",
			Tool: "get_symbol",
			Why: "get_symbol and get_source answer about a symbol the caller already named. " +
				"They refuse a miss instead of answering an empty list, which is the other honest shape: " +
				"if that ever changes into an empty answer, it needs a verdict like the rest.",
			Arguments: map[string]any{"stable_key": "there-is-no-such-key"},
			WantError: true,
		},
		{
			Name: "source for a symbol that is not there",
			Tool: "get_source",
			Why:  "Same shape, same reason.",
			Arguments: map[string]any{"symbols": []any{
				map[string]any{"stable_key": "there-is-no-such-key"},
			}},
			WantError: true,
		},
	}
}

func measure(ctx context.Context, session *sdkmcp.ClientSession, testCase check) checkResult {
	result := checkResult{Name: testCase.Name, Tool: testCase.Tool}
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
func readCorpus(ctx context.Context, session *sdkmcp.ClientSession) (corpus, error) {
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
	// was written to produce. These two reasons are the ones observed while
	// building it: a package excluded by a build tag, and a module that does
	// not load. Both are recorded without a file, which is what makes them
	// bound every answer about their repository instead of one reference.
	for _, reason := range decoded.Results.UnresolvedByReason {
		measured.Reasons = append(measured.Reasons, fmt.Sprintf("%s=%d", reason.Key, reason.Count))
		if reason.Key == "PACKAGE_NOT_BUILDABLE" || reason.Key == "MODULE_NOT_LOADED" {
			measured.Scopes += reason.Count
		}
	}
	sort.Strings(measured.Reasons)
	return measured, nil
}

// prepareCorpus copies the fixtures out of the repository and makes each copy a
// git repository, because the registry takes one git repository per entry. The
// fixtures themselves are never touched.
func prepareCorpus(ctx context.Context) (string, string, error) {
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
	for _, name := range []string{"pure", "blinded"} {
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
	return workspace, home, nil
}

func index(ctx context.Context, binary, home, workspace string) error {
	initArguments := []string{"init", "--languages", "go"}
	for _, name := range []string{"pure", "blinded"} {
		initArguments = append(initArguments, "--repository", name+"="+filepath.Join(workspace, name))
	}
	for _, arguments := range [][]string{initArguments, {"index", "--full"}} {
		process := exec.CommandContext(ctx, binary, arguments...)
		process.Env = append(os.Environ(), "HOME="+home)
		if output, err := process.CombinedOutput(); err != nil {
			return fmt.Errorf("kivgraph %s: %w: %s", arguments[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func connect(ctx context.Context, binary, home string) (*sdkmcp.ClientSession, func(), error) {
	process := exec.Command(binary, "serve")
	process.Env = append(os.Environ(), "HOME="+home)
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
	}
}

func limitations() []string {
	return []string{
		"The corpus is two Go repositories: it proves the invariant holds and that it is not a constant, not that it holds for every language and every shape of failure.",
		"The blind spot is one kind -- a package excluded by a build tag, recorded as `PACKAGE_NOT_BUILDABLE` with no file. A module that fails to load records `MODULE_NOT_LOADED` the same way, and both were observed while building this fixture; the other reasons are not exercised here.",
		"This harness reads the envelope, not the rows: it prices what a tool claims about its own completeness, and says nothing about whether the edges are right. That is what `benchmarks/go-semantic`, `rust-semantic`, `python-semantic` and `dart-semantic` measure.",
		"It needs a binary built with the `ladybug` tag, and git on the PATH. Neither is faked when absent: the run fails with the reason.",
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
	fmt.Fprintf(&text, "|dato|valor|\n|---|---|\n")
	fmt.Fprintf(&text, "|comando|`%s`|\n", out.Command)
	fmt.Fprintf(&text, "|fecha|%s|\n", out.Date)
	fmt.Fprintf(&text, "|plataforma|%s/%s, %s|\n", out.Environment.OS, out.Environment.Arch, out.Environment.Go)
	fmt.Fprintf(&text, "|corpus|%s|\n", strings.Join(out.Corpus.Repositories, ", "))
	fmt.Fprintf(&text, "|símbolos|%d|\n", out.Corpus.Symbols)
	fmt.Fprintf(&text, "|no resueltas|%d|\n", out.Corpus.Unresolved)
	fmt.Fprintf(&text, "|ámbitos ilegibles|%d|\n", out.Corpus.Scopes)
	fmt.Fprintf(&text, "|comprobaciones|%d, %d pasan|\n\n", out.Totals.Checks, out.Totals.Passed)

	text.WriteString("## Comprobaciones\n\n")
	text.WriteString("|tool|pregunta|filas|veredicto|estado|\n|---|---|---|---|---|\n")
	for _, result := range out.Checks {
		state := "ok"
		if !result.Passed {
			state = "**FALLA**"
		}
		verdict := result.Verdict
		if verdict == "" {
			verdict = "-"
		}
		if result.Errored {
			verdict = "rechaza"
		}
		fmt.Fprintf(&text, "|`%s`|%s|%d|`%s`|%s|\n", result.Tool, result.Name, result.Total, verdict, state)
	}
	text.WriteString("\nCada fila dice qué se pierde si deja de cumplirse:\n\n")
	for _, result := range out.Checks {
		fmt.Fprintf(&text, "- **%s** (`%s`)", result.Name, result.Tool)
		if result.Failure != "" {
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
