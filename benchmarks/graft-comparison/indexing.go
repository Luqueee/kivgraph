package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// indexCost is what a surface charges before it can answer anything: the wall
// clock of a build, what the build produced, and what it needed installed to
// produce it. A query benchmark that omits it compares two steady states and
// hides the price of reaching them.
type indexCost struct {
	Command    string  `json:"command"`
	ColdMS     float64 `json:"cold_ms"`
	WarmMS     float64 `json:"warm_ms"`
	Files      int     `json:"files"`
	Nodes      int     `json:"nodes"`
	Edges      int     `json:"edges"`
	Unresolved int     `json:"unresolved,omitempty"`
	// Languages is what each front end actually contributed. A run that loses a
	// toolchain still publishes a generation, so a total alone cannot say whether
	// a language was measured or was absent: the first attempt at this benchmark
	// published `passed: true` with zero Go and zero Rust symbols. Recording the
	// breakdown is what lets a later reader tell one run from another, and it is
	// why the previous run's Kivgraph arm cannot be recomposed from what it wrote.
	Languages  map[string]int `json:"languages,omitempty"`
	Generation string         `json:"generation,omitempty"`
	StateBytes int64          `json:"state_bytes,omitempty"`
	Needs      string         `json:"needs"`
	// Cold and Warm do not mean the same thing on both surfaces, so each says
	// what it timed. Pretending otherwise would compare an empty cache against a
	// populated one and call the difference a speed.
	Cold string `json:"cold_is"`
	Warm string `json:"warm_is"`
}

var (
	graftWiring = regexp.MustCompile(`wiring: (\d+) nodes .*?, (\d+) edges`)
	graftParsed = regexp.MustCompile(`parsed: (\d+) of (\d+) files`)
)

// measureGraftIndex times a cold build and a warm rebuild of the same tree. Cold
// is what a new checkout pays; warm is what every later query pays, because a
// graft query refreshes the graph against the working tree before answering.
func measureGraftIndex(cfg config) (indexCost, error) {
	out := indexCost{
		Command: fmt.Sprintf("graft --dir %s build %s", cfg.GraftContext, cfg.Corpus),
		Needs:   "node; no API key and no language toolchain (tree-sitter is linked in)",
		Cold:    "context directory deleted first: nothing replayed from cache",
		Warm:    "same build re-run with the extract cache populated and no file changed",
	}
	if err := os.RemoveAll(cfg.GraftContext); err != nil {
		return out, fmt.Errorf("clear %s: %w", cfg.GraftContext, err)
	}
	cold, coldOutput, err := timeCommand(cfg.Graft, "--dir", cfg.GraftContext, "build", cfg.Corpus)
	if err != nil {
		return out, fmt.Errorf("graft cold build: %w (%s)", err, lastLines(coldOutput, 3))
	}
	out.ColdMS = cold
	if wiring := graftWiring.FindStringSubmatch(coldOutput); wiring != nil {
		out.Nodes, _ = strconv.Atoi(wiring[1])
		out.Edges, _ = strconv.Atoi(wiring[2])
	}
	if parsed := graftParsed.FindStringSubmatch(coldOutput); parsed != nil {
		out.Files, _ = strconv.Atoi(parsed[2])
	}
	warm, warmOutput, err := timeCommand(cfg.Graft, "--dir", cfg.GraftContext, "build", cfg.Corpus)
	if err != nil {
		return out, fmt.Errorf("graft warm build: %w (%s)", err, lastLines(warmOutput, 3))
	}
	out.WarmMS = warm
	out.StateBytes = directorySize(cfg.GraftContext)
	return out, nil
}

// kivgraphIndexResult decodes the last line of `kivgraph index --full --json`.
type kivgraphIndexResult struct {
	Result struct {
		Passed       bool   `json:"passed"`
		GenerationID string `json:"generation_id"`
		Counts       struct {
			Files      int `json:"files"`
			Symbols    int `json:"symbols"`
			Edges      int `json:"edges"`
			Unresolved int `json:"unresolved"`
		} `json:"counts"`
		Index struct {
			GoDefinitions     int `json:"go_definitions"`
			TypeScriptSymbols int `json:"typescript_symbols"`
			RustSymbols       int `json:"rust_symbols"`
		} `json:"index"`
	} `json:"result"`
}

// measureKivgraphIndex times a full rebuild and the cached rebuild after it, and
// leaves a published generation behind for the query phase to read.
func measureKivgraphIndex(cfg config) (indexCost, error) {
	out := indexCost{
		Command: "kivgraph index --full --json",
		Needs:   "the Go module cache for every indexed module, plus cargo for every Rust workspace; without them a load fails and its symbols are absent",
		Cold:    "fact cache and Rust target directory deleted first: every front end loads from nothing",
		Warm:    "a second full rebuild immediately after, which still re-loads every compiler front end",
	}
	// Symmetry with the graft arm: its cold build starts from an empty context, so
	// this one starts from an empty derived cache. Only the isolated HOME is
	// touched, and both directories are regenerable by definition.
	for _, derived := range []string{"/.local/state/kivgraph/factcache", "/.local/state/kivgraph/rust-target"} {
		if err := os.RemoveAll(cfg.Home + derived); err != nil {
			return out, fmt.Errorf("clear %s: %w", cfg.Home+derived, err)
		}
	}
	first, firstOutput, err := timeCommand2(cfg.Corpus, map[string]string{"HOME": cfg.Home}, cfg.Kivgraph, "index", "--full", "--json")
	if err != nil {
		return out, fmt.Errorf("kivgraph full index: %w (%s)", err, lastLines(firstOutput, 2))
	}
	out.ColdMS = first
	decoded, err := lastJSONLine(firstOutput)
	if err != nil {
		return out, err
	}
	out.Files = decoded.Result.Counts.Files
	out.Nodes = decoded.Result.Counts.Symbols
	out.Edges = decoded.Result.Counts.Edges
	out.Unresolved = decoded.Result.Counts.Unresolved
	out.Generation = decoded.Result.GenerationID
	out.Languages = map[string]int{
		"go":         decoded.Result.Index.GoDefinitions,
		"typescript": decoded.Result.Index.TypeScriptSymbols,
		"rust":       decoded.Result.Index.RustSymbols,
	}

	second, secondOutput, err := timeCommand2(cfg.Corpus, map[string]string{"HOME": cfg.Home}, cfg.Kivgraph, "index", "--full", "--json")
	if err != nil {
		return out, fmt.Errorf("kivgraph cached index: %w (%s)", err, lastLines(secondOutput, 2))
	}
	out.WarmMS = second
	if again, err := lastJSONLine(secondOutput); err == nil {
		out.Generation = again.Result.GenerationID
	}
	out.StateBytes = directorySize(cfg.Home + "/.local/state/kivgraph")
	return out, nil
}

func lastJSONLine(output string) (kivgraphIndexResult, error) {
	decoded := kivgraphIndexResult{}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if err := json.Unmarshal([]byte(lines[index]), &decoded); err == nil && decoded.Result.GenerationID != "" {
			return decoded, nil
		}
	}
	return decoded, fmt.Errorf("no result line in kivgraph index output")
}

func timeCommand(name string, arguments ...string) (float64, string, error) {
	return timeCommand2("", nil, name, arguments...)
}

func timeCommand2(directory string, environment map[string]string, name string, arguments ...string) (float64, string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Env = os.Environ()
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	started := time.Now()
	output, err := command.CombinedOutput()
	elapsed := float64(time.Since(started).Milliseconds())
	return elapsed, string(output), err
}

func directorySize(path string) int64 {
	total := int64(0)
	_ = filepathWalkSize(path, &total)
	return total
}

// wiringFacts are what a graft build actually produced, read from the graph it
// wrote rather than from its console summary. The `--lsp` claim -- compiler-grade
// call edges when a language server is installed -- is checked here: if the flag
// added anything, the relation and confidence counts say so.
type wiringFacts struct {
	Path        string         `json:"path"`
	Nodes       int            `json:"nodes"`
	Edges       int            `json:"edges"`
	Scopes      []string       `json:"scopes"`
	Relations   map[string]int `json:"relations"`
	Confidence  map[string]int `json:"confidence"`
	LSPResolved int            `json:"lsp_resolved_edges"`
}

// readWiringFacts decodes graft/.graph/wiring.json.
func readWiringFacts(contextDir string) (wiringFacts, error) {
	path := contextDir + "/.graph/wiring.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		return wiringFacts{}, fmt.Errorf("read %s: %w", path, err)
	}
	decoded := struct {
		Meta struct {
			NodeCount int `json:"nodeCount"`
			EdgeCount int `json:"edgeCount"`
			Scopes    []struct {
				Prefix  string   `json:"prefix"`
				Markers []string `json:"markers"`
			} `json:"scopes"`
		} `json:"meta"`
		Edges []struct {
			Relation   string `json:"relation"`
			Confidence string `json:"confidence"`
		} `json:"edges"`
	}{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return wiringFacts{}, fmt.Errorf("parse %s: %w", path, err)
	}
	out := wiringFacts{
		Path: path, Nodes: decoded.Meta.NodeCount, Edges: decoded.Meta.EdgeCount,
		Relations: map[string]int{}, Confidence: map[string]int{},
	}
	for _, scope := range decoded.Meta.Scopes {
		out.Scopes = append(out.Scopes, scope.Prefix+" ("+strings.Join(scope.Markers, ",")+")")
	}
	for _, edge := range decoded.Edges {
		out.Relations[edge.Relation]++
		out.Confidence[edge.Confidence]++
		if strings.Contains(strings.ToLower(edge.Confidence), "lsp") {
			out.LSPResolved++
		}
	}
	return out, nil
}

// measureGraftLSPIndex times the opt-in compiler-grade build. It is the same tree
// and the same tool with one flag added, so the comparison isolates what the flag
// buys and what it costs.
func measureGraftLSPIndex(cfg config) (indexCost, error) {
	out := indexCost{
		Command: fmt.Sprintf("graft --dir %s build %s --lsp", cfg.GraftLSPContext, cfg.Corpus),
		Needs:   "a language server on PATH per language: gopls, rust-analyzer, typescript-language-server",
		Cold:    "context directory deleted first, every language server cold",
		Warm:    "not measured: the flag's cost is the cold pass",
	}
	if err := os.RemoveAll(cfg.GraftLSPContext); err != nil {
		return out, fmt.Errorf("clear %s: %w", cfg.GraftLSPContext, err)
	}
	cold, output, err := timeCommand(cfg.Graft, "--dir", cfg.GraftLSPContext, "build", cfg.Corpus, "--lsp")
	if err != nil {
		return out, fmt.Errorf("graft --lsp build: %w (%s)", err, lastLines(output, 3))
	}
	out.ColdMS = cold
	if wiring := graftWiring.FindStringSubmatch(output); wiring != nil {
		out.Nodes, _ = strconv.Atoi(wiring[1])
		out.Edges, _ = strconv.Atoi(wiring[2])
	}
	if parsed := graftParsed.FindStringSubmatch(output); parsed != nil {
		out.Files, _ = strconv.Atoi(parsed[2])
	}
	out.StateBytes = directorySize(cfg.GraftLSPContext)
	return out, nil
}
