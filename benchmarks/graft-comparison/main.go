// Command graft-comparison measures what the same structural questions cost,
// and how correct the answers are, on three arms over one corpus: kivgraph,
// NanoNets graft, and the tools every agent already has.
//
// The third arm is the point. A comparison between two graph tools that never
// prices the alternative rewards whichever one answers in fewer bytes, including
// when it answers nothing: an empty page is always the cheapest page. So every
// question is also answered with a corpus-wide regex search plus a full read of
// each file that declares the name, which is the minimum a reader needs to tell
// homonyms apart, and correctness is scored against a ground truth neither
// server produced.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	benchmarkName    = "kivgraph-vs-graft"
	defaultDirectory = "benchmarks/graft-comparison"
)

// graft's tool names, kept together because they are the contract this harness
// depends on and a rename upstream should fail in one place.
const (
	graftTrace   = "graft_trace_calls"
	graftFindAll = "graft_find_all"
	graftFileAPI = "graft_file_api"
)

type config struct {
	Kivgraph        string
	Graft           string
	GraftContext    string
	GraftLSPContext string
	Corpus          string
	Home            string
	Directory       string
	ScopeContext    string
	SkipIndexing    bool
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Kivgraph, "kivgraph", "kivgraph", "kivgraph executable to measure")
	flag.StringVar(&cfg.Graft, "graft", "graft", "graft executable to measure")
	flag.StringVar(&cfg.GraftContext, "graft-context", "/private/tmp/graft-kena-ctx", "graft context directory for the whole corpus")
	flag.StringVar(&cfg.GraftLSPContext, "graft-lsp-context", "/private/tmp/graft-kena-lsp", "graft context built with --lsp, the opt-in compiler-grade tier")
	flag.StringVar(&cfg.ScopeContext, "scope-context-root", "/private/tmp", "directory holding the per-scope graft contexts")
	flag.StringVar(&cfg.Corpus, "corpus", "/Users/adria/Documents/programacion/projects/kena", "corpus root")
	flag.StringVar(&cfg.Home, "home", "/tmp/kivbench-graft-home", "isolated HOME holding kivgraph's configuration and generation")
	flag.StringVar(&cfg.Directory, "dir", defaultDirectory, "benchmark directory to write results into")
	flag.BoolVar(&cfg.SkipIndexing, "skip-indexing", false, "reuse the existing graft context and kivgraph generation instead of rebuilding both")
	flag.Parse()

	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// results is what the run writes. Every number in the report comes from here.
type results struct {
	Benchmark   string                 `json:"benchmark"`
	GeneratedAt time.Time              `json:"generated_at"`
	Commit      string                 `json:"commit"`
	Tokenizer   string                 `json:"tokenizer"`
	Environment map[string]string      `json:"environment"`
	Corpus      corpusFacts            `json:"corpus"`
	Versions    map[string]string      `json:"versions"`
	Surfaces    map[string]surface     `json:"surfaces"`
	Indexing    map[string]indexCost   `json:"indexing"`
	Wiring      map[string]wiringFacts `json:"wiring"`
	Questions   []questionResult       `json:"questions"`
	Census      *censusResult          `json:"census"`
	Auxiliary   auxiliaryResults       `json:"auxiliary"`
	ScopeProbes []scopeResult          `json:"scope_probes"`
	Aggregate   map[string]agg         `json:"aggregate"`
}

type corpusFacts struct {
	Path         string `json:"path"`
	Repositories int    `json:"git_repositories"`
	CodeFiles    int    `json:"code_files_searched"`
}

type armResult struct {
	Calls     []observation `json:"calls"`
	Tokens    int           `json:"tokens"`
	MS        float64       `json:"ms"`
	CallCount int           `json:"call_count"`
	// Score is absent when the question has no ground truth: an impact
	// traversal and a directory outline are payload comparisons, and printing a
	// zero for them would read as a wrong answer instead of an unscored one.
	Score        *score `json:"score,omitempty"`
	Note         string `json:"note,omitempty"`
	Attributed   bool   `json:"attributed_to_subject,omitempty"`
	AmbiguousBy  int    `json:"ambiguous_declarations,omitempty"`
	DeclaredNone bool   `json:"declared_no_callers,omitempty"`
	// Rows is how many facts the payload carried, for the questions scored by
	// payload rather than against a truth: tokens alone cannot say whether a
	// cheaper answer said less or merely said it more compactly.
	Rows int `json:"rows,omitempty"`
	// Pages is how many pages the answer needed. It is only above zero when a
	// first page did not hold the whole set, which is a cost of the surface and
	// not of the corpus.
	Pages int `json:"pages,omitempty"`
}

func (a *armResult) add(o observation) {
	a.Calls = append(a.Calls, o)
	a.Tokens += o.Tokens
	a.MS += o.MS
	a.CallCount++
}

type questionResult struct {
	ID       string                `json:"id"`
	Ask      string                `json:"question"`
	Language string                `json:"language"`
	Subject  subject               `json:"subject"`
	Truth    []string              `json:"ground_truth"`
	Arms     map[string]*armResult `json:"arms"`
	Native   nativeArm             `json:"native"`
}

type censusResult struct {
	Ask    string                `json:"question"`
	Truth  []string              `json:"ground_truth"`
	Arms   map[string]*armResult `json:"arms"`
	Native nativeArm             `json:"native"`
}

type auxiliaryResults struct {
	Outline     map[string]*armResult `json:"directory_outline"`
	BlastRadius map[string]*armResult `json:"blast_radius"`
	CrossRepo   map[string]*armResult `json:"cross_repository_consumers"`
}

type scopeResult struct {
	ID      string     `json:"id"`
	Root    string     `json:"graft_build_root"`
	Subject subject    `json:"subject"`
	Truth   []string   `json:"ground_truth"`
	Arm     *armResult `json:"graft"`
}

type agg struct {
	Tokens        int     `json:"tokens_total"`
	Calls         int     `json:"calls_total"`
	MS            float64 `json:"ms_total"`
	PrecisionMean float64 `json:"precision_mean"`
	RecallMean    float64 `json:"recall_mean"`
	Exact         int     `json:"exact_answers"`
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
	out := results{
		Benchmark: benchmarkName, GeneratedAt: time.Now().UTC(), Tokenizer: encodingName,
		Environment: map[string]string{
			"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(),
		},
		Corpus:   corpusFacts{Path: cfg.Corpus, Repositories: len(repos.dirs)},
		Versions: map[string]string{},
		Surfaces: map[string]surface{},
		Auxiliary: auxiliaryResults{
			Outline:     map[string]*armResult{},
			BlastRadius: map[string]*armResult{},
			CrossRepo:   map[string]*armResult{},
		},
		Indexing:  map[string]indexCost{},
		Wiring:    map[string]wiringFacts{},
		Aggregate: map[string]agg{},
	}
	out.Commit = capture("git", "rev-parse", "--short", "HEAD")
	out.Versions["kivgraph"] = capture(cfg.Kivgraph, "version")
	out.Versions["graft"] = capture(cfg.Graft, "--version")

	// Indexing runs first: it is part of the comparison, and kivgraph's query
	// phase reads the generation this publishes.
	if !cfg.SkipIndexing {
		if out.Indexing["graft"], err = measureGraftIndex(cfg); err != nil {
			return err
		}
		if out.Indexing["graft_lsp"], err = measureGraftLSPIndex(cfg); err != nil {
			return err
		}
		if out.Indexing["kivgraph"], err = measureKivgraphIndex(cfg); err != nil {
			return err
		}
	}
	// What each graft build actually wrote, read from the graph itself: it is how
	// the `--lsp` claim is checked instead of trusted.
	for name, dir := range map[string]string{"graft": cfg.GraftContext, "graft_lsp": cfg.GraftLSPContext} {
		facts, factsErr := readWiringFacts(dir)
		if factsErr != nil {
			return factsErr
		}
		out.Wiring[name] = facts
	}

	kiv, err := dial(ctx, "kivgraph", cfg.Kivgraph, []string{"serve"}, map[string]string{"HOME": cfg.Home})
	if err != nil {
		return err
	}
	defer kiv.close()
	gra, err := dial(ctx, "graft", cfg.Graft, []string{"--dir", cfg.GraftContext, "mcp", cfg.Corpus}, nil)
	if err != nil {
		return err
	}
	defer gra.close()
	// The third arm: the same tool over the graph its opt-in compiler-grade flag
	// produced. It answers whether `--lsp` changes any of these answers.
	lsp, err := dial(ctx, "graft-lsp", cfg.Graft, []string{"--dir", cfg.GraftLSPContext, "mcp", cfg.Corpus}, nil)
	if err != nil {
		return err
	}
	defer lsp.close()
	// The captures are provenance, so they are written even when the run fails:
	// a parser that could not read a response is exactly when the bytes it
	// choked on are worth having on disk.
	defer func() {
		_ = writeRaw(cfg.Directory, map[string]*server{"kivgraph": kiv, "graft": gra, "graft-lsp": lsp})
	}()

	if out.Surfaces["kivgraph"], err = kiv.measureSurface(ctx, tokens); err != nil {
		return err
	}
	if out.Surfaces["graft"], err = gra.measureSurface(ctx, tokens); err != nil {
		return err
	}

	for _, q := range questions {
		measured, err := measureQuestion(ctx, tokens, repos, cfg, kiv, gra, lsp, q)
		if err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
		out.Questions = append(out.Questions, measured)
		out.Corpus.CodeFiles = measured.Native.SearchedFiles
	}

	censusMeasured, err := measureCensus(ctx, tokens, repos, cfg, kiv, gra, lsp)
	if err != nil {
		return fmt.Errorf("census: %w", err)
	}
	out.Census = censusMeasured

	if err := measureAuxiliary(ctx, tokens, repos, kiv, gra, &out); err != nil {
		return err
	}
	if err := measureScopes(ctx, tokens, cfg, &out); err != nil {
		return err
	}
	out.Aggregate = aggregate(out)

	if err := writeRaw(cfg.Directory, map[string]*server{"kivgraph": kiv, "graft": gra, "graft-lsp": lsp}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(cfg.Directory, "results.json"), out); err != nil {
		return err
	}
	printSummary(out)
	return nil
}

// measureQuestion prices one "who calls this" question on all three arms.
//
// The sequences are the ones each surface documents for the question, not ones
// invented to flatter either: all three start from the bare name the asker has.
// graft traces callers of it in one call, the native arm searches and then reads
// every declaring file, and kivgraph asks for its references directly -- which
// answers in one call when the name is unique, and when it is not comes back
// refusing to pick, listing the candidates as the same triple every tool
// accepts.
//
// This arm used to resolve the symbol with `find_symbol` first, which is the
// same answer by a dearer route: that page listed 22 rows for `withRetry`,
// imports and re-exports included, and cost 750 tokens where the refusal costs
// 129. Starting from the name was always supported; until this run nothing in
// the tool's description said so, so the harness modelled a caller who reads
// the surface and pays for the lookup.
func measureQuestion(
	ctx context.Context, tokens *counter, repos repositories, cfg config,
	kiv, gra, lsp *server, q question,
) (questionResult, error) {
	truth := repos.canonicalAll(q.Callers)
	out := questionResult{
		ID: q.ID, Ask: q.Ask, Language: q.Language, Subject: q.Subject,
		Truth: truth, Arms: map[string]*armResult{},
	}

	kivArm, err := kivgraphArm(ctx, tokens, repos, kiv, q, truth, "")
	if err != nil {
		return questionResult{}, err
	}
	out.Arms["kivgraph"] = kivArm
	// The same sequence at the granularity the question actually asks. Every
	// question here is "which files call this" and every score is computed over
	// files, so the `files` view answers it whole; the compact rows additionally
	// carry the line of each reference, which nothing here asked for. graft has
	// no equivalent mode -- `graft callers` takes direction, depth, `--in`,
	// `--json` and `--no-refresh`, and always answers with caller blocks and
	// line ranges -- so this arm is reported beside the comparable one rather
	// than in place of it.
	filesArm, err := kivgraphArm(ctx, tokens, repos, kiv, q, truth, "files")
	if err != nil {
		return questionResult{}, err
	}
	out.Arms["kivgraph_files"] = filesArm

	out.Arms["graft"] = graftArm(ctx, tokens, repos, gra, "graft", q, truth)
	out.Arms["graft_lsp"] = graftArm(ctx, tokens, repos, lsp, "graft_lsp", q, truth)

	native, err := measureNative(tokens, cfg.Corpus, q.Subject.Symbol, q.Declarations)
	if err != nil {
		return questionResult{}, err
	}
	out.Native = native
	return out, nil
}

// kivgraphArm prices the cheapest correct path to one reference question: ask by
// name, and when several declarations carry it, name the one the question is
// about. An empty view takes the default compact rows; "files" answers only
// which files hold the references.
func kivgraphArm(
	ctx context.Context, tokens *counter, repos repositories, kiv *server,
	q question, truth []string, view string,
) (*armResult, error) {
	label := "kivgraph"
	if view != "" {
		label += "-" + view
	}
	arm := &armResult{}
	arguments := map[string]any{"name": q.Subject.Symbol, "direction": "incoming"}
	if view != "" {
		arguments["view"] = view
	}
	answer := kiv.call(ctx, tokens, q.ID+"-"+label+"-find_references-by-name", "find_references", arguments)
	arm.add(answer)
	// Only ambiguity is recoverable, and it is not a failed measurement: the
	// refusal is what tells the caller which declarations exist. Any other
	// error stands as this arm's answer rather than being retried into silence.
	if answer.Failed && strings.Contains(answer.Error, "AMBIGUOUS_SYMBOL") {
		arm.AmbiguousBy = ambiguousDeclarations(answer.Error)
		arguments = map[string]any{
			"qualified_name": q.Subject.Name,
			"repository":     q.Subject.Repo,
			"path":           q.Subject.Path,
			"direction":      "incoming",
		}
		if view != "" {
			arguments["view"] = view
		}
		answer = kiv.call(ctx, tokens, q.ID+"-"+label+"-find_references-p1", "find_references", arguments)
		arm.add(answer)
	}
	claimed := []string{}
	for page := 1; !answer.Failed; page++ {
		files, decoded, err := kivgraphFiles(answer.Text)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, files...)
		if decoded.NextCursor == nil || *decoded.NextCursor == "" {
			break
		}
		arm.Pages = page + 1
		arguments["cursor"] = *decoded.NextCursor
		answer = kiv.call(ctx, tokens,
			fmt.Sprintf("%s-%s-find_references-p%d", q.ID, label, page+1), "find_references", arguments)
		arm.add(answer)
	}
	arm.Score = scoreAgainst(withoutDeclaring(claimed, repos.canonical(q.Subject.corpusPath())), truth)
	return arm, nil
}

// graftArm prices one graft trace and attributes only the callers graft placed
// under the subject's own declaration.
func graftArm(
	ctx context.Context, tokens *counter, repos repositories, srv *server, label string,
	q question, truth []string,
) *armResult {
	graArm := &armResult{}
	trace := srv.call(ctx, tokens, q.ID+"-"+label+"-trace", graftTrace,
		map[string]any{"symbol": q.Subject.Symbol})
	graArm.add(trace)
	if !trace.Failed {
		blocks := parseGraftTrace(trace.Text)
		callers, found := graftCallersOf(blocks, q.Subject.corpusPath())
		graArm.Attributed = found
		for _, block := range blocks {
			if block.Path == q.Subject.corpusPath() {
				graArm.AmbiguousBy = block.Ambiguous
				graArm.DeclaredNone = block.Empty
			}
		}
		if !found {
			graArm.Note = "graft answered about no declaration at the subject's path"
		}
		graArm.Score = scoreAgainst(
			withoutDeclaring(repos.canonicalAll(callers), repos.canonical(q.Subject.corpusPath())),
			truth,
		)
	}
	return graArm
}

// withoutDeclaring drops the declaring file from a claimed set. Both surfaces
// name it -- one as the subject, the other as a block header -- and the truth is
// about callers, so counting it would penalise both for answering the question.
func withoutDeclaring(files []string, declaring string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		if file != declaring {
			out = append(out, file)
		}
	}
	return out
}

func measureCensus(
	ctx context.Context, tokens *counter, repos repositories, cfg config, kiv, gra, lsp *server,
) (*censusResult, error) {
	truth := repos.canonicalAll(census.Callers)
	out := &censusResult{Ask: census.Ask, Truth: truth, Arms: map[string]*armResult{}}

	kivArm := &armResult{}
	found := kiv.call(ctx, tokens, "Q5-kivgraph-find_symbol", "find_symbol",
		map[string]any{"name": census.Subject.Symbol})
	kivArm.add(found)
	if !found.Failed {
		declarations, _, err := kivgraphDeclarations(found.Text)
		if err != nil {
			return nil, err
		}
		kivArm.Score = scoreAgainst(declarations, truth)
	}
	out.Arms["kivgraph"] = kivArm

	for label, srv := range map[string]*server{"graft": gra, "graft_lsp": lsp} {
		arm := &armResult{}
		trace := srv.call(ctx, tokens, "Q5-"+label+"-trace", graftTrace,
			map[string]any{"symbol": census.Subject.Symbol})
		arm.add(trace)
		if !trace.Failed {
			arm.Score = scoreAgainst(
				repos.canonicalAll(graftDeclarations(parseGraftTrace(trace.Text))), truth)
			arm.Note = "the census is a by-product of graft_trace_calls: it answers about every declaration sharing the name"
		}
		out.Arms[label] = arm
	}

	native, err := measureNative(tokens, cfg.Corpus, census.Subject.Symbol, census.Declarations)
	if err != nil {
		return nil, err
	}
	out.Native = native
	return out, nil
}

// measureAuxiliary prices the three questions where the surfaces are shaped
// differently rather than merely resolved differently.
func measureAuxiliary(
	ctx context.Context, tokens *counter, repos repositories, kiv, gra *server, out *results,
) error {
	// The outline question is "what is declared in this package". It is asked of
	// a directory holding several files, because a one-file directory cannot show
	// the difference between one call and one call per file, which is the whole
	// shape difference between the two surfaces here.
	const outlineDir = "src/cluster/worker/ipc"
	const outlineRepo = "core"
	outlineCorpus := "packages/core/" + outlineDir

	kivOutline := &armResult{}
	outline := kiv.call(ctx, tokens, "aux-outline-kivgraph", "get_file_outline",
		map[string]any{"repository": outlineRepo, "path": outlineDir})
	kivOutline.add(outline)
	kivOutline.Rows = totalOf(outline.Text)
	out.Auxiliary.Outline["kivgraph"] = kivOutline

	graOutline := &armResult{}
	entries, err := os.ReadDir(filepath.Join(out.Corpus.Path, outlineCorpus))
	if err != nil {
		return fmt.Errorf("read %s: %w", outlineCorpus, err)
	}
	files := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && codeExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			files = append(files, outlineCorpus+"/"+entry.Name())
		}
	}
	sort.Strings(files)
	for _, file := range files {
		answer := gra.call(ctx, tokens, "aux-outline-graft-"+filepath.Base(file), graftFileAPI,
			map[string]any{"file": file})
		graOutline.add(answer)
		graOutline.Rows += strings.Count(answer.Text, "\n- L")
	}
	graOutline.Note = fmt.Sprintf(
		"graft has no directory outline: one graft_file_api per file, %d files in this directory", len(files))
	out.Auxiliary.Outline["graft"] = graOutline

	impact := questions[2].Subject
	kivImpact := &armResult{}
	radius := kiv.call(ctx, tokens, "aux-impact-kivgraph", "get_blast_radius",
		map[string]any{
			"qualified_name": impact.Name, "repository": impact.Repo, "path": impact.Path, "depth": 2,
		})
	kivImpact.add(radius)
	kivImpact.Rows = totalOf(radius.Text)
	out.Auxiliary.BlastRadius["kivgraph"] = kivImpact

	graImpact := &armResult{}
	traced := gra.call(ctx, tokens, "aux-impact-graft", graftTrace,
		map[string]any{"symbol": impact.Symbol, "depth": 2})
	graImpact.add(traced)
	graImpact.Rows = strings.Count(traced.Text, "calls ←")
	graImpact.Note = "graft_trace_calls with depth 2; rows counted are the `calls ←` lines"
	out.Auxiliary.BlastRadius["graft"] = graImpact

	provider := questions[0].Subject
	kivConsumers := &armResult{}
	consumers := kiv.call(ctx, tokens, "aux-xrepo-kivgraph", "find_cross_repo_consumers",
		map[string]any{
			"qualified_name": provider.Name, "repository": provider.Repo, "path": provider.Path,
		})
	kivConsumers.add(consumers)
	if !consumers.Failed {
		kivConsumers.Score = scoreAgainst(repositoriesNamed(consumers.Text, crossRepoConsumers), crossRepoConsumers)
	}
	out.Auxiliary.CrossRepo["kivgraph"] = kivConsumers

	graConsumers := &armResult{}
	hits := gra.call(ctx, tokens, "aux-xrepo-graft", graftFindAll,
		map[string]any{"pattern": "@kena/shared"})
	graConsumers.add(hits)
	if !hits.Failed {
		named := map[string]bool{}
		for _, path := range graftGrepFiles(hits.Text) {
			canonical := repos.canonical(path)
			if repository := strings.SplitN(canonical, ":", 2)[0]; repository != "" {
				named[repository] = true
			}
		}
		delete(named, provider.Repo)
		graConsumers.Score = scoreAgainst(sortedKeys(named), crossRepoConsumers)
		graConsumers.Note = "graft models no package dimension; graft_find_all over the specifier is the closest available answer"
	}
	out.Auxiliary.CrossRepo["graft"] = graConsumers
	return nil
}

// repositoriesNamed reports which of the expected repositories a response
// mentions. The cross-repository answer is a set of repositories, so the score
// is over names rather than paths.
func repositoriesNamed(text string, expected []string) []string {
	out := []string{}
	for _, name := range expected {
		if strings.Contains(text, `"`+name+`"`) || strings.Contains(text, name+":") {
			out = append(out, name)
		}
	}
	return out
}

// measureScopes re-asks two questions against graft builds of a single
// repository and a single Go package. It separates what graft's extractor can
// resolve from what the scope it was pointed at let it resolve.
func measureScopes(ctx context.Context, tokens *counter, cfg config, out *results) error {
	for _, probe := range scopeProbes {
		contextDir := filepath.Join(cfg.ScopeContext, "graft-scope-"+probe.ID)
		root := filepath.Join(cfg.Corpus, probe.Root)
		build := exec.Command(cfg.Graft, "--dir", contextDir, "build", root)
		if output, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("graft build %s: %w (%s)", root, err, lastLines(string(output), 5))
		}
		scoped, err := dial(ctx, "graft:"+probe.ID, cfg.Graft, []string{"--dir", contextDir, "mcp", root}, nil)
		if err != nil {
			return err
		}
		arm := &armResult{}
		trace := scoped.call(ctx, tokens, "scope-"+probe.ID, graftTrace,
			map[string]any{"symbol": probe.Subject.Symbol})
		arm.add(trace)
		if !trace.Failed {
			blocks := parseGraftTrace(trace.Text)
			target := strings.TrimPrefix(probe.Subject.corpusPath(), probe.Root+"/")
			callers, found := graftCallersOf(blocks, target)
			arm.Attributed = found
			for _, block := range blocks {
				if block.Path == target {
					arm.AmbiguousBy = block.Ambiguous
					arm.DeclaredNone = block.Empty
				}
			}
			arm.Score = scoreAgainst(withoutDeclaring(callers, target), probe.Callers)
		}
		out.ScopeProbes = append(out.ScopeProbes, scopeResult{
			ID: probe.ID, Root: probe.Root, Subject: probe.Subject, Truth: probe.Callers, Arm: arm,
		})
		if err := writeRaw(cfg.Directory, map[string]*server{"graft-scope-" + probe.ID: scoped}); err != nil {
			scoped.close()
			return err
		}
		scoped.close()
	}
	return nil
}

func aggregate(out results) map[string]agg {
	totals := map[string]agg{}
	for _, name := range []string{"kivgraph", "kivgraph_files", "graft", "graft_lsp"} {
		summary := agg{Of: len(out.Questions)}
		precision, recall := 0.0, 0.0
		for _, question := range out.Questions {
			arm := question.Arms[name]
			summary.Tokens += arm.Tokens
			summary.Calls += arm.CallCount
			summary.MS += arm.MS
			precision += arm.Score.Precision
			recall += arm.Score.Recall
			if arm.Score.Precision == 1 && arm.Score.Recall == 1 {
				summary.Exact++
			}
		}
		if len(out.Questions) > 0 {
			summary.PrecisionMean = precision / float64(len(out.Questions))
			summary.RecallMean = recall / float64(len(out.Questions))
		}
		totals[name] = summary
	}
	native := agg{Of: len(out.Questions), PrecisionMean: 1, RecallMean: 1, Exact: len(out.Questions)}
	for _, question := range out.Questions {
		native.Tokens += question.Native.Tokens
		native.Calls += 1 + question.Native.ReadFiles
	}
	totals["native"] = native
	return totals
}

func writeRaw(directory string, servers map[string]*server) error {
	base := filepath.Join(directory, "raw")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", base, err)
	}
	for name, srv := range servers {
		for capture, text := range srv.captures {
			path := filepath.Join(base, sanitize(name+"-"+capture)+".txt")
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}
	}
	return nil
}

func sanitize(name string) string {
	return strings.NewReplacer("/", "_", " ", "_", ":", "-").Replace(name)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func capture(command string, arguments ...string) string {
	output, err := exec.Command(command, arguments...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(output), "\n")[0])
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}

func printSummary(out results) {
	fmt.Printf("%s  %s  %s\n", out.Benchmark, out.Commit, out.Tokenizer)
	for name, s := range out.Surfaces {
		fmt.Printf("surface %-9s %2d tools, %5d tok resident (%d descriptions + %d instructions), %d tok schemas\n",
			name, s.Tools, s.Resident, s.DescriptionToks, s.InstructionToks, s.SchemaToks)
	}
	fmt.Printf("\n%-14s %26s %26s %26s %26s %9s\n", "", "kivgraph", "kivgraph files-view", "graft", "graft --lsp", "native")
	fmt.Printf("%-14s %8s %6s %5s %5s %8s %6s %5s %5s %8s %6s %5s %5s %8s %6s %5s %5s %9s\n",
		"question", "tok", "calls", "P", "R", "tok", "calls", "P", "R",
		"tok", "calls", "P", "R", "tok", "calls", "P", "R", "tok")
	for _, q := range out.Questions {
		k, f, g, l := q.Arms["kivgraph"], q.Arms["kivgraph_files"], q.Arms["graft"], q.Arms["graft_lsp"]
		fmt.Printf("%-14s %8d %6d %5.2f %5.2f %8d %6d %5.2f %5.2f %8d %6d %5.2f %5.2f %8d %6d %5.2f %5.2f %9d\n",
			q.ID, k.Tokens, k.CallCount, k.Score.Precision, k.Score.Recall,
			f.Tokens, f.CallCount, f.Score.Precision, f.Score.Recall,
			g.Tokens, g.CallCount, g.Score.Precision, g.Score.Recall,
			l.Tokens, l.CallCount, l.Score.Precision, l.Score.Recall, q.Native.Tokens)
	}
	for _, name := range []string{"kivgraph", "kivgraph_files", "graft", "graft_lsp", "native"} {
		a := out.Aggregate[name]
		fmt.Printf("%-9s total %6d tok, %2d calls, %7.1f ms, P=%.2f R=%.2f, exact %d/%d\n",
			name, a.Tokens, a.Calls, a.MS, a.PrecisionMean, a.RecallMean, a.Exact, a.Of)
	}
	for name, w := range out.Wiring {
		fmt.Printf("wiring %-10s %6d nodes, %6d edges, %d scopes, lsp_resolved=%d, calls=%d\n",
			name, w.Nodes, w.Edges, len(w.Scopes), w.LSPResolved, w.Relations["calls"])
	}
	for _, probe := range out.ScopeProbes {
		fmt.Printf("scope %-22s P=%.2f R=%.2f  (ambiguous=%d, declared-none=%v)\n",
			probe.ID, probe.Arm.Score.Precision, probe.Arm.Score.Recall,
			probe.Arm.AmbiguousBy, probe.Arm.DeclaredNone)
	}
}
