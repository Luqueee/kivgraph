// Command graph-tools-comparison prices the same questions on five tools that
// all describe themselves as code graphs, plus the tools an agent already has.
//
// The comparison is the whole point, and so is the shape of it. A benchmark that
// measured only its own payload always looks good: it reports a lean response,
// never counts the reads an agent performs afterwards, and never asks what the
// alternative would have cost. So every arm answers the same question against
// the same manual ground truth, the reads are inside every arm that needs them,
// and `grep` plus reading the file is one of the arms.
//
// Three families, because five tools that share a category do not share a
// question: asking only "who calls this" would put a BFS explorer and a
// blast-radius tool at zero for being outside their purpose rather than for
// being wrong. Each is asked in its own vocabulary and scored on the answer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	benchmarkName    = "graph-tools-comparison"
	defaultDirectory = "benchmarks/graph-tools-comparison"
)

type config struct {
	Kivgraph   string
	Graft      string
	CRG        string
	Graphify   string
	CMM        string
	Corpus     string
	CorpusCopy string
	Directory  string
	StateRoot  string
	// KivgraphHome is the isolated HOME holding kivgraph's configuration and its
	// published generation over the whole corpus.
	KivgraphHome string
	// GraftContext is graft's context directory, which lives outside the corpus.
	GraftContext string
	SkipIndexing bool
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Kivgraph, "kivgraph", "kivgraph", "kivgraph executable")
	flag.StringVar(&cfg.Graft, "graft", "graft", "graft executable")
	flag.StringVar(&cfg.CRG, "crg", "/private/tmp/crg-venv/bin/code-review-graph", "code-review-graph executable")
	flag.StringVar(&cfg.Graphify, "graphify", "graphify", "graphify executable")
	flag.StringVar(&cfg.CMM, "codebase-memory", "codebase-memory-mcp", "codebase-memory-mcp executable")
	flag.StringVar(&cfg.Corpus, "corpus", "/Users/adria/Documents/programacion/projects/kena", "corpus root")
	flag.StringVar(&cfg.CorpusCopy, "corpus-copy", "/private/tmp/5way/kena-copy",
		"private copy of the corpus, for tools that write beside the code they read")
	flag.StringVar(&cfg.Directory, "dir", defaultDirectory, "benchmark directory to write results into")
	flag.StringVar(&cfg.StateRoot, "state-root", "/private/tmp/5way", "directory holding each arm's isolated state")
	flag.StringVar(&cfg.KivgraphHome, "kivgraph-home", "/tmp/kivbench-graft-home", "isolated HOME for kivgraph")
	flag.StringVar(&cfg.GraftContext, "graft-context", "/private/tmp/5way/graft-ctx", "graft context directory")
	flag.BoolVar(&cfg.SkipIndexing, "skip-indexing", false, "reuse every existing index instead of rebuilding")
	flag.Parse()

	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
}

// arms is the order every table prints in: ours first because it is the one
// under test, the alternatives next, and the tools an agent already has last,
// because they are the denominator the others have to beat.
var arms = []string{"kivgraph", "graft", "code-review-graph", "graphify", "codebase-memory", "native"}

type results struct {
	Benchmark   string                    `json:"benchmark"`
	GeneratedAt time.Time                 `json:"generated_at"`
	Commit      string                    `json:"commit"`
	Tokenizer   string                    `json:"tokenizer"`
	Environment map[string]string         `json:"environment"`
	Corpus      corpusFacts               `json:"corpus"`
	Versions    map[string]string         `json:"versions"`
	Isolation   map[string]string         `json:"isolation"`
	Indexing    map[string]indexCost      `json:"indexing"`
	Questions   []questionResult          `json:"questions"`
	Aggregate   map[string]agg            `json:"aggregate"`
	Families    map[string]map[string]agg `json:"aggregate_by_family"`
}

type corpusFacts struct {
	Path         string `json:"path"`
	Repositories int    `json:"git_repositories"`
	CodeFiles    int    `json:"code_files"`
}

// indexCost is what an arm paid before answering anything, and over what.
type indexCost struct {
	Command string  `json:"command"`
	MS      float64 `json:"ms"`
	Scope   string  `json:"scope"`
	Bytes   int64   `json:"state_bytes,omitempty"`
	Needs   string  `json:"needs,omitempty"`
}

type questionResult struct {
	ID     string                `json:"id"`
	Family string                `json:"family"`
	Ask    string                `json:"question"`
	Truth  []string              `json:"ground_truth"`
	Arms   map[string]*armResult `json:"arms"`
}

type agg struct {
	Tokens        int     `json:"tokens_total"`
	Calls         int     `json:"calls_total"`
	MS            float64 `json:"ms_total"`
	PrecisionMean float64 `json:"precision_mean"`
	RecallMean    float64 `json:"recall_mean"`
	Exact         int     `json:"exact_answers"`
	Answered      int     `json:"answered"`
	Of            int     `json:"of"`
}

func run(ctx context.Context, cfg config) error {
	tokens, err := newCounter()
	if err != nil {
		return err
	}
	repos, err := discoverRepositories(cfg.Corpus)
	if err != nil {
		return err
	}
	commit, err := currentCommit()
	if err != nil {
		return err
	}
	out := results{
		Benchmark: benchmarkName, GeneratedAt: time.Now().UTC(), Commit: commit,
		Tokenizer: encodingName,
		Environment: map[string]string{
			"os": osName(), "arch": archName(), "go": goVersion(),
		},
		Corpus:    corpusFacts{Path: cfg.Corpus, Repositories: len(repos.dirs)},
		Versions:  map[string]string{},
		Isolation: isolationNotes(cfg),
		Indexing:  map[string]indexCost{},
		Questions: []questionResult{},
		Aggregate: map[string]agg{},
		Families:  map[string]map[string]agg{},
	}
	out.Versions["kivgraph"] = capture(cfg.Kivgraph, "version")
	out.Versions["graft"] = capture(cfg.Graft, "--version")
	out.Versions["code-review-graph"] = capture(cfg.CRG, "--version")
	out.Versions["graphify"] = capture(cfg.Graphify, "--version")
	out.Versions["codebase-memory"] = capture(cfg.CMM, "--version")

	// The native arm needs the same addresses as the rest, and nothing else
	// about the repository map, so it borrows the two functions rather than the
	// map itself.
	canonicalOf, canonicalAll = repos.canonical, repos.canonicalAll

	fmt.Printf("%s  %s  %s  corpus %d repos\n", benchmarkName, commit, encodingName, len(repos.dirs))
	for _, name := range arms {
		if version := out.Versions[name]; version != "" {
			fmt.Printf("  %-18s %s\n", name, version)
		}
	}

	captures := map[string]string{}
	if err := buildEveryIndex(ctx, cfg, &out); err != nil {
		return err
	}

	kiv, err := dial(ctx, "kivgraph", cfg.Kivgraph, []string{"serve"}, map[string]string{"HOME": cfg.KivgraphHome})
	if err != nil {
		return err
	}
	defer kiv.close()
	if err := assertCorpusIsPublished(ctx, tokens, kiv); err != nil {
		return err
	}

	for _, q := range questions {
		result := questionResult{
			ID: q.ID, Family: q.Family, Ask: q.Ask,
			Truth: q.Truth, Arms: map[string]*armResult{},
		}
		measured := []struct {
			name string
			run  func() (*armResult, error)
		}{
			{"kivgraph", func() (*armResult, error) { return measureKivgraph(ctx, tokens, repos, kiv, q) }},
			{"graft", func() (*armResult, error) {
				return measureGraft(ctx, tokens, repos, captures, cfg.Graft, cfg.GraftContext, cfg.Corpus, q)
			}},
			{"code-review-graph", func() (*armResult, error) {
				return measureCRG(ctx, tokens, repos, captures, cfg.CRG, cfg.Corpus, crgData(cfg), crgHome(cfg), q)
			}},
			{"graphify", func() (*armResult, error) {
				return measureGraphify(ctx, tokens, repos, captures, cfg.Graphify, cfg.CorpusCopy, graphifyHome(cfg), q)
			}},
			{"codebase-memory", func() (*armResult, error) {
				return measureCodebaseMemory(ctx, tokens, repos, captures, cfg.CMM, cfg.Corpus, cmmHome(cfg), q)
			}},
			{"native", func() (*armResult, error) { return measureNative(tokens, cfg.Corpus, q) }},
		}
		for _, arm := range measured {
			answered, err := arm.run()
			if err != nil {
				return fmt.Errorf("%s %s: %w", q.ID, arm.name, err)
			}
			result.Arms[arm.name] = answered
		}
		out.Questions = append(out.Questions, result)
		printQuestion(result)
	}

	for key, value := range kiv.captures {
		captures[key] = value
	}
	if err := writeRaw(cfg.Directory, captures); err != nil {
		return err
	}
	out.Aggregate, out.Families = aggregate(out)
	printAggregate(out)
	return writeJSON(filepath.Join(cfg.Directory, "results.json"), out)
}

// Each arm's isolated state. graphify is the reason the corpus copy exists: it
// writes `graphify-out/` beside the code it reads, so it never sees the real
// tree. The others keep their state outside the corpus but still get a private
// HOME, because two of them keep a registry there and the user's own index must
// not be read or replaced by a benchmark.
func crgData(cfg config) string      { return filepath.Join(cfg.StateRoot, "crg-data") }
func crgHome(cfg config) string      { return filepath.Join(cfg.StateRoot, "crg-home") }
func graphifyHome(cfg config) string { return filepath.Join(cfg.StateRoot, "graphify-home") }
func cmmHome(cfg config) string      { return filepath.Join(cfg.StateRoot, "cmm-home") }

// subjectRepositories is the set of repositories the questions actually name.
// Two arms build one repository at a time, so building all 37 would price an
// index nine tenths of which no question reads. What each arm indexed is
// recorded in `indexing[...].scope`, so the cost is never compared across two
// different scopes without saying so.
func subjectRepositories() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, q := range questions {
		if !seen[q.Subject.Dir] {
			seen[q.Subject.Dir] = true
			out = append(out, q.Subject.Dir)
		}
	}
	return out
}

// buildEveryIndex pays each arm's entry price and records it. It is part of the
// comparison: an answer that needs a 40-second index is not the same product as
// one that needs a 3-second index, and neither is the same as `grep`.
func buildEveryIndex(ctx context.Context, cfg config, out *results) error {
	// Every arm's state directory is created before it is handed over: two of
	// them chdir into their HOME and fail on a path that does not exist yet,
	// which is a setup bug that looks exactly like a broken tool.
	for _, directory := range []string{
		cfg.StateRoot, cfg.KivgraphHome, crgData(cfg), crgHome(cfg), graphifyHome(cfg), cmmHome(cfg),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
	}
	if cfg.SkipIndexing {
		fmt.Println("\nreusing every existing index (--skip-indexing)")
		return nil
	}
	// Every arm is timed from an empty state, because "cold" has to mean the
	// same thing in every row: graft answered in 2.6 s over a context it had
	// already built and in 23.5 s over none, and printing those side by side
	// would compare a cache against a build.
	for _, derived := range []string{cfg.GraftContext, crgData(cfg), filepath.Join(cmmHome(cfg), ".cache")} {
		if err := os.RemoveAll(derived); err != nil {
			return fmt.Errorf("clear %s: %w", derived, err)
		}
	}
	for _, dir := range subjectRepositories() {
		if err := os.RemoveAll(filepath.Join(cfg.CorpusCopy, dir, "graphify-out")); err != nil {
			return fmt.Errorf("clear graphify output for %s: %w", dir, err)
		}
	}
	fmt.Println("\nindexing")

	started := time.Now()
	if err := prepareKivgraphHome(ctx, cfg, repositoryList(cfg)); err != nil {
		return err
	}
	kivgraphIndex := exec.CommandContext(ctx, cfg.Kivgraph, "index", "--full", "--json")
	kivgraphIndex.Dir = cfg.Corpus
	kivgraphIndex.Env = append(os.Environ(), "HOME="+cfg.KivgraphHome)
	output, err := kivgraphIndex.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kivgraph index: %w (%s)", err, lastLine(string(output)))
	}
	// The exit code is not the whole answer: an index can end with a published
	// generation, or it can end reporting `passed: false` with a reason, and a
	// benchmark that measured the second as if it were the first would price a
	// graph that was never built. A half-second full index over 37 repositories
	// is the shape of that mistake.
	symbols, err := publishedSymbols(string(output))
	if err != nil {
		return fmt.Errorf("kivgraph index: %w", err)
	}
	out.Indexing["kivgraph"] = indexCost{
		Command: "kivgraph index --full", MS: sinceMS(started),
		Scope: kivgraphScope(symbols),
		Bytes: directorySize(filepath.Join(cfg.KivgraphHome, ".local/state/kivgraph")),
		Needs: "the Go module cache per indexed module and cargo per Rust workspace; without them a load fails and its symbols are absent",
	}
	fmt.Printf("  %-18s %6.1f s  %d symbols\n", "kivgraph", sinceMS(started)/1000, symbols)

	started = time.Now()
	graftBuild := exec.CommandContext(ctx, cfg.Graft, "--dir", cfg.GraftContext, "build", cfg.Corpus)
	if output, err := graftBuild.CombinedOutput(); err != nil {
		return fmt.Errorf("graft build: %w (%s)", err, lastLine(string(output)))
	}
	out.Indexing["graft"] = indexCost{
		Command: "graft build", MS: sinceMS(started), Scope: "the whole corpus, 37 repositories",
		Bytes: directorySize(cfg.GraftContext), Needs: "nothing: the structural tier needs no key and no toolchain",
	}
	fmt.Printf("  %-18s %6.1f s\n", "graft", sinceMS(started)/1000)

	started = time.Now()
	if _, err := indexCodebaseMemory(ctx, cfg.CMM, cfg.Corpus, cmmHome(cfg)); err != nil {
		return fmt.Errorf("codebase-memory index: %w", err)
	}
	out.Indexing["codebase-memory"] = indexCost{
		Command: "codebase-memory-mcp cli index_repository", MS: sinceMS(started),
		Scope: "the whole corpus, 37 repositories", Bytes: directorySize(filepath.Join(cmmHome(cfg), ".cache")),
		Needs: "nothing beyond the binary",
	}
	fmt.Printf("  %-18s %6.1f s\n", "codebase-memory", sinceMS(started)/1000)

	crgTotal, graphifyTotal := 0.0, 0.0
	for _, dir := range subjectRepositories() {
		elapsed, err := buildCRG(ctx, cfg.CRG, filepath.Join(cfg.Corpus, dir), crgData(cfg), crgHome(cfg))
		if err != nil {
			return fmt.Errorf("code-review-graph build %s: %w", dir, err)
		}
		crgTotal += elapsed
		elapsed, err = buildGraphify(ctx, cfg.Graphify, filepath.Join(cfg.CorpusCopy, dir), graphifyHome(cfg))
		if err != nil {
			return fmt.Errorf("graphify update %s: %w", dir, err)
		}
		graphifyTotal += elapsed
	}
	scope := fmt.Sprintf("one graph per repository; built the %d the questions name, not all 37", len(subjectRepositories()))
	out.Indexing["code-review-graph"] = indexCost{
		Command: "code-review-graph build --repo <r> --data-dir <d>", MS: crgTotal, Scope: scope,
		Bytes: directorySize(crgData(cfg)), Needs: "nothing beyond the binary",
	}
	out.Indexing["graphify"] = indexCost{
		Command: "graphify update <r>", MS: graphifyTotal,
		Scope: scope + "; run against the private copy, because it writes beside the code it reads",
		Needs: "nothing for the structural pass; a provider key only for the semantic one, which was not measured",
	}
	fmt.Printf("  %-18s %6.1f s\n  %-18s %6.1f s\n", "code-review-graph", crgTotal/1000, "graphify", graphifyTotal/1000)
	out.Indexing["native"] = indexCost{Command: "none", Scope: "nothing is indexed", Needs: "nothing"}
	return nil
}

// publishedSymbols reads the last result line of `index --full --json`. It
// insists the run passed, and reports the symbol count when the run rebuilt.
//
// A count of zero is not a failure here: an index over an unchanged tree
// republishes the generation it already had and says so with empty counters, in
// half a second. What the benchmark actually needs is that a snapshot is
// readable afterwards, and that is asserted where it is observable -- on the
// server's tool list, in assertCorpusIsPublished -- rather than guessed from a
// counter that legitimately reads zero.
func publishedSymbols(output string) (int, error) {
	report := struct {
		Result struct {
			Passed       bool   `json:"passed"`
			GenerationID string `json:"generation_id"`
			Error        string `json:"error"`
			Counts       struct {
				Symbols int `json:"symbols"`
			} `json:"counts"`
		} `json:"result"`
	}{}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if !strings.Contains(line, `"event":"result"`) {
			continue
		}
		if err := json.Unmarshal([]byte(line), &report); err != nil {
			return 0, fmt.Errorf("parse index report: %w", err)
		}
		found = true
	}
	if !found {
		return 0, fmt.Errorf("index produced no result event")
	}
	if !report.Result.Passed {
		reason := report.Result.Error
		if reason == "" {
			reason = "no reason given"
		}
		return 0, fmt.Errorf("index did not publish: %s", reason)
	}
	return report.Result.Counts.Symbols, nil
}

// assertCorpusIsPublished is the check that matters, and the weaker one it
// replaced is why it exists: a server with no snapshot registers only
// `index_project`, but a server holding an EMPTY snapshot registers all eleven
// tools and then answers every question with `REPOSITORY_NOT_FOUND`. Both would
// have been recorded as a tool that scores zero everywhere -- a setup failure
// wearing a result's clothes.
//
// What it asserts is the property the questions need, not a round number: the
// published graph names every repository a question is about. The corpus holds
// 37 git repositories and the graph reports 35, because two carry no Go,
// TypeScript or Rust for it to index, and a benchmark that demanded 37 would
// fail on a perfectly good generation.
func assertCorpusIsPublished(ctx context.Context, tokens *counter, srv *server) error {
	answer := srv.call(ctx, tokens, "setup-list_repositories", "list_repositories", map[string]any{})
	if answer.Failed {
		return fmt.Errorf("kivgraph list_repositories: %s", answer.Error)
	}
	decoded := struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}{}
	if err := json.Unmarshal([]byte(answer.Text), &decoded); err != nil {
		return fmt.Errorf("parse list_repositories: %w", err)
	}
	published := make([]string, 0, len(decoded.Results))
	for _, repository := range decoded.Results {
		published = append(published, repository.Name)
	}
	for _, q := range questions {
		if !slices.Contains(published, q.Subject.Repo) {
			return fmt.Errorf("kivgraph published %d repositories but not %q, which %s is about",
				len(published), q.Subject.Repo, q.ID)
		}
	}
	return nil
}

func sinceMS(started time.Time) float64 {
	return float64(time.Since(started).Microseconds()) / 1000
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}

// aggregate totals every arm overall and per family. An arm that does not answer
// a family is counted in `Of` but not in `Answered`, so a mean is never a
// silent average over questions the tool was never asked.
func aggregate(out results) (map[string]agg, map[string]map[string]agg) {
	overall := map[string]agg{}
	families := map[string]map[string]agg{}
	for _, name := range arms {
		total := agg{}
		for _, q := range out.Questions {
			arm := q.Arms[name]
			if arm == nil {
				continue
			}
			byFamily := families[q.Family]
			if byFamily == nil {
				byFamily = map[string]agg{}
				families[q.Family] = byFamily
			}
			entry := byFamily[name]
			for _, target := range []*agg{&total, &entry} {
				target.Of++
				target.Tokens += arm.Tokens
				target.Calls += arm.CallCount
				target.MS += arm.MS
				if arm.Unsupported || arm.Score == nil {
					continue
				}
				target.Answered++
				target.PrecisionMean += arm.Score.Precision
				target.RecallMean += arm.Score.Recall
				if arm.Score.Precision == 1 && arm.Score.Recall == 1 {
					target.Exact++
				}
			}
			// Sums here, means afterwards. Averaging inside the loop divided an
			// already divided figure once per question and put `grep` at a
			// precision of 0.42 on four answers it got exactly right.
			byFamily[name] = entry
		}
		overall[name] = mean(total)
	}
	for _, byArm := range families {
		for name, entry := range byArm {
			byArm[name] = mean(entry)
		}
	}
	return overall, families
}

// mean divides by what was answered, never by what was asked: an arm that
// answered one of two questions perfectly has a precision of 1.00 over one
// answer, and the `answered` count next to it is what says so.
func mean(in agg) agg {
	if in.Answered > 0 {
		in.PrecisionMean /= float64(in.Answered)
		in.RecallMean /= float64(in.Answered)
	}
	return in
}

func printQuestion(q questionResult) {
	fmt.Printf("\n%-14s %s\n", q.ID, q.Family)
	for _, name := range arms {
		arm := q.Arms[name]
		if arm == nil {
			continue
		}
		if arm.Unsupported {
			fmt.Printf("  %-18s unsupported  %s\n", name, arm.Note)
			continue
		}
		fmt.Printf("  %-18s %6d tok %2d calls  P=%.2f R=%.2f\n",
			name, arm.Tokens, arm.CallCount, arm.Score.Precision, arm.Score.Recall)
	}
}

func printAggregate(out results) {
	fmt.Printf("\n%-18s %8s %6s %8s %6s %6s %8s\n", "arm", "tok", "calls", "ms", "P", "R", "exact")
	for _, name := range arms {
		a := out.Aggregate[name]
		if a.Of == 0 {
			continue
		}
		fmt.Printf("%-18s %8d %6d %8.1f %6.2f %6.2f %4d/%d\n",
			name, a.Tokens, a.Calls, a.MS, a.PrecisionMean, a.RecallMean, a.Exact, a.Answered)
	}
}

func isolationNotes(cfg config) map[string]string {
	return map[string]string{
		"kivgraph":          "isolated HOME at " + cfg.KivgraphHome + "; the published generation and the registry live there",
		"graft":             "context directory outside the corpus at " + cfg.GraftContext,
		"code-review-graph": "graph database in --data-dir outside the corpus; its registry in an isolated HOME",
		"graphify":          "writes graphify-out/ beside the code it reads, so it only ever runs against the private copy at " + cfg.CorpusCopy,
		"codebase-memory":   "index under an isolated HOME, so the user's own ~/.cache index is untouched",
		"native":            "reads only; nothing is written",
	}
}

func capture(command string, arguments ...string) string {
	out, err := exec.Command(command, arguments...).Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func currentCommit() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func osName() string   { return capture("uname", "-s") }
func archName() string { return capture("uname", "-m") }
func goVersion() string {
	fields := strings.Fields(capture("go", "version"))
	if len(fields) < 3 {
		return "unknown"
	}
	return fields[2]
}

// ---------- native arm ----------

var codeExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".cjs": true, ".rs": true, ".py": true, ".java": true, ".rb": true,
}

// measureNative prices the answer an agent reaches without any of these tools:
// search the corpus, then read what the search cannot disambiguate. It is the
// denominator, and it is correct by construction -- a reader who opens every
// declaration does tell the homonyms apart -- so its score is 1.00 and the
// question it answers is what it cost to get there.
func measureNative(tokens *counter, corpus string, q question) (*armResult, error) {
	arm := &armResult{}
	switch q.Family {
	case familyOutline:
		content, err := os.ReadFile(filepath.Join(corpus, q.Subject.corpusPath()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", q.Subject.corpusPath(), err)
		}
		arm.add(observation{Tool: "read", Tokens: tokens.count(string(content))})
		arm.Claimed = append([]string{}, q.Truth...)
		arm.Score = scoreAgainst(arm.Claimed, q.Truth)
		return arm, nil
	default:
		output, searched, err := searchCorpus(corpus, q.Subject.Symbol)
		if err != nil {
			return nil, err
		}
		arm.add(observation{Tool: "grep", Tokens: tokens.count(output)})
		for _, declaration := range q.Declarations {
			content, readErr := os.ReadFile(filepath.Join(corpus, declaration))
			if readErr != nil {
				return nil, fmt.Errorf("read %s: %w", declaration, readErr)
			}
			arm.add(observation{Tool: "read", Tokens: tokens.count(string(content))})
		}
		arm.Note = fmt.Sprintf("searched %d code files, then read %d declaring file(s)", searched, len(q.Declarations))
		arm.Claimed = nil
		for _, item := range q.Truth {
			arm.Claimed = append(arm.Claimed, canonicalOf(item))
		}
		arm.Score = scoreAgainst(arm.Claimed, canonicalAll(q.Truth))
		return arm, nil
	}
}

// canonicalOf and canonicalAll are set by run() so the native arm speaks the
// same addresses as every other one without threading the repository map
// through a signature that does not otherwise need it.
var (
	canonicalOf  = func(path string) string { return path }
	canonicalAll = func(paths []string) []string { return paths }
)

// searchCorpus imitates `rg -n` over the corpus: one `path:line:text` per hit.
func searchCorpus(corpus, name string) (string, int, error) {
	pattern, err := regexp.Compile(`\b` + regexp.QuoteMeta(name) + `\b`)
	if err != nil {
		return "", 0, err
	}
	builder := strings.Builder{}
	searched := 0
	walkErr := filepath.WalkDir(corpus, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", "target", "dist", "graphify-out":
				return fs.SkipDir
			}
			return nil
		}
		if !codeExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		searched++
		content, readErr := os.ReadFile(path)
		if readErr != nil || !pattern.Match(content) {
			return nil
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(path, corpus), "/")
		for index, line := range strings.Split(string(content), "\n") {
			if pattern.MatchString(line) {
				fmt.Fprintf(&builder, "%s:%d:%s\n", relative, index+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", 0, walkErr
	}
	return builder.String(), searched, nil
}

// ---------- output ----------

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(blob, '\n'), 0o644)
}

// writeRaw persists every captured response, so a parsing claim in the report
// can be checked against the bytes it was made from.
func writeRaw(directory string, captures map[string]string) error {
	root := filepath.Join(directory, "raw")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(captures))
	for name := range captures {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, sanitize(name)+".txt"), []byte(captures[name]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func sanitize(name string) string {
	return strings.NewReplacer("/", "_", " ", "_", ":", "-", "\"", "").Replace(name)
}

func directorySize(path string) int64 {
	total := int64(0)
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

var errUnsupported = errors.New("family not supported by this arm")

// kivgraphScope says what the index covered, and distinguishes a rebuild from a
// republication of an unchanged tree instead of printing a zero as if it were a
// measurement.
func kivgraphScope(symbols int) string {
	if symbols == 0 {
		return "the whole corpus, 37 repositories; the tree was unchanged, so the existing generation was republished"
	}
	return fmt.Sprintf("the whole corpus, 37 repositories, %d symbols published", symbols)
}

// repositoryList is every repository the corpus holds, as `name=absolutePath`.
func repositoryList(cfg config) []string {
	discovered, err := discoverRepositories(cfg.Corpus)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(discovered.dirs))
	for _, dir := range discovered.dirs {
		out = append(out, discovered.names[dir]+"="+filepath.Join(cfg.Corpus, dir))
	}
	sort.Strings(out)
	return out
}

// prepareKivgraphHome builds the isolated state from nothing.
//
// Reusing whatever the last run left behind is how this arm ended up answering
// REPOSITORY_NOT_FOUND with all eleven tools registered: `index --full` over an
// unchanged tree republishes in half a second, and if what it republishes is an
// empty generation the run measures a tool that knows nothing. Starting from an
// empty directory costs the full index and makes the number reproducible.
func prepareKivgraphHome(ctx context.Context, cfg config, repositories []string) error {
	if len(repositories) == 0 {
		return fmt.Errorf("no repository to register under %s", cfg.Corpus)
	}
	if err := os.RemoveAll(cfg.KivgraphHome); err != nil {
		return fmt.Errorf("clear %s: %w", cfg.KivgraphHome, err)
	}
	if err := os.MkdirAll(cfg.KivgraphHome, 0o755); err != nil {
		return err
	}
	arguments := []string{"init", "--languages", "go,typescript,rust"}
	for _, repository := range repositories {
		arguments = append(arguments, "--repository", repository)
	}
	initialise := exec.CommandContext(ctx, cfg.Kivgraph, arguments...)
	initialise.Env = append(os.Environ(), "HOME="+cfg.KivgraphHome)
	if output, err := initialise.CombinedOutput(); err != nil {
		return fmt.Errorf("kivgraph init: %w (%s)", err, lastLine(string(output)))
	}
	// Go and TypeScript tests are part of the corpus every other arm parses, so
	// leaving them out of one index would compare two different corpora. The
	// four rivals parse with tree-sitter and see every file on disk; two of
	// Kivgraph's levers are what make it see the same set, and both are off by
	// default because they widen what a graph asserts. A test file that no
	// tsconfig claims needs `include_unclaimed_sources` to be indexed at all,
	// so without it one arm is answering about a smaller corpus than the rest.
	configPath := filepath.Join(cfg.KivgraphHome, ".config/kivgraph/config.yaml")
	blob, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}
	patched := strings.ReplaceAll(string(blob), "include_tests: false", "include_tests: true")
	patched = strings.ReplaceAll(patched, "include_unclaimed_sources: false", "include_unclaimed_sources: true")
	if !strings.Contains(patched, "include_unclaimed_sources: true") {
		return fmt.Errorf("%s declares no include_unclaimed_sources to enable", configPath)
	}
	return os.WriteFile(configPath, []byte(patched), 0o644)
}
