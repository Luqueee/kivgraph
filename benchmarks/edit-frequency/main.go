// Command edit-frequency measures what the graph costs an agent that edits.
//
// ADR 0057 retired the delta route on a ceiling measured against a corpus-level
// scenario: one rebuild against another, on a corpus that had just been pulled.
// Issue 106 asks whether that is the workload that matters, and names a
// different one -- an agent editing files across steps within a single task,
// where the question is not what one rebuild costs against another but how
// often a rebuild is triggered and what the session pays while it runs.
//
// This harness measures that workload, and it measures the three things the
// issue asks for before any code is written:
//
//   - the cost of the current full-rebuild path after a single-file edit, with
//     the fact cache warm, which is the condition an editing agent is always in;
//   - the split of that cost into analysis -- which the fact cache already makes
//     incremental -- and publication, which stays whole;
//   - the cost of the grep-and-read arm over the same corpus for the same
//     questions, which is the line the graph has to stay under.
//
// It never writes inside a registered repository. The edits land in a private
// copy of one repository's tracked files, and the generations land in a private
// root: a run leaves the machine's own graph exactly where it found it.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/indexing"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// schemaVersion travels in the artifact. A run that changes what a field means
// raises it, so two files cannot be compared as if they measured the same thing.
const schemaVersion = "edit-frequency-v1"

func main() {
	options := parseFlags()
	if err := run(context.Background(), options); err != nil {
		fmt.Fprintf(os.Stderr, "edit-frequency: %v\n", err)
		os.Exit(1)
	}
}

type flags struct {
	configPath  string
	scratchRepo string
	scratchRoot string
	root        string
	cache       string
	edits       int
	questions   string
	grepRepeats int
	output      string
	keep        bool
}

func parseFlags() flags {
	var options flags
	flag.StringVar(&options.configPath, "config", "", "kivgraph config to read the corpus from; empty resolves the default")
	flag.StringVar(&options.scratchRepo, "scratch-repository", "kivgraph", "name of the registered repository the edits are applied to")
	flag.StringVar(&options.scratchRoot, "scratch", "", "where the private copy of that repository lives; empty picks a temporary directory")
	flag.StringVar(&options.root, "root", "", "private generation root; empty picks a temporary directory")
	flag.StringVar(&options.cache, "fact-cache", "", "private fact cache directory; empty picks a temporary directory")
	flag.IntVar(&options.edits, "edits", 8, "how many edit-then-reindex steps to measure")
	flag.StringVar(&options.questions, "questions", "", "newline-separated symbol names for the grep arm; empty uses the built-in set")
	flag.IntVar(&options.grepRepeats, "grep-repeats", 3, "how many times to time each grep question")
	flag.StringVar(&options.output, "output", "", "where to write results.json; empty writes to stdout only")
	flag.BoolVar(&options.keep, "keep", false, "keep the scratch copy and the generation root after the run")
	flag.Parse()
	return options
}

// ---------------------------------------------------------------------------
// results
// ---------------------------------------------------------------------------

type stageCost struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
}

// passCost is one full pass, split where the issue asks it to be split.
//
// Analysis is everything up to the first rebuild stage: process start, the
// language engines, the merge. Publication is everything from there: staging,
// the bulk load, the snapshot, integrity, the golden probes and the swap. The
// boundary is not an estimate -- RunFull calls the rebuild progress sink with
// the name of each stage as it starts, so the first call is the instant the
// analysis half ended.
type passCost struct {
	Step               int         `json:"step"`
	Label              string      `json:"label"`
	EditedFile         string      `json:"edited_file,omitempty"`
	TotalSeconds       float64     `json:"total_seconds"`
	AnalysisSeconds    float64     `json:"analysis_seconds"`
	PublicationSeconds float64     `json:"publication_seconds"`
	Stages             []stageCost `json:"publication_stages"`
	CacheMode          string      `json:"cache_mode"`
	CacheHits          int         `json:"cache_hits"`
	CacheMisses        int         `json:"cache_misses"`
	CacheVerified      int         `json:"cache_verified"`
	Files              int         `json:"files"`
	Symbols            int         `json:"symbols"`
	Edges              int         `json:"edges"`
	GenerationID       string      `json:"generation_id"`
}

type grepQuestion struct {
	Symbol         string  `json:"symbol"`
	Matches        int     `json:"matches"`
	MatchedFiles   int     `json:"matched_files"`
	FileBytes      int64   `json:"file_bytes"`
	SearchSeconds  float64 `json:"search_seconds"`
	ReadSeconds    float64 `json:"read_seconds"`
	Seconds        float64 `json:"seconds"`
	Failed         bool    `json:"failed,omitempty"`
	FailureMessage string  `json:"failure_message,omitempty"`
}

type summary struct {
	Passes int `json:"passes"`
	// Edit passes only: the warm-up is excluded from every statistic here,
	// because a warm-up is the one pass an editing agent never runs.
	MedianTotalSeconds       float64 `json:"median_total_seconds"`
	MedianAnalysisSeconds    float64 `json:"median_analysis_seconds"`
	MedianPublicationSeconds float64 `json:"median_publication_seconds"`
	FastestTotalSeconds      float64 `json:"fastest_total_seconds"`
	SlowestTotalSeconds      float64 `json:"slowest_total_seconds"`
	PublicationShare         float64 `json:"publication_share"`
	AnalysisShare            float64 `json:"analysis_share"`
	// GrepSecondsPerQuestion is the median of the grep arm: one search over
	// the corpus plus reading every file it matched.
	GrepSecondsPerQuestion float64 `json:"grep_seconds_per_question"`
	// RebuildsPerGrepQuestion is the crossover, in the only unit the two arms
	// share: how many grep-and-read questions one rebuild pays for. An agent
	// that asks fewer questions than this between two edits is spending more
	// wall clock on the graph than grep would have cost it.
	RebuildsPerGrepQuestion float64 `json:"questions_paid_for_by_one_rebuild"`
}

type environment struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	CPUs int    `json:"cpus"`
	Go   string `json:"go"`
}

type corpusInfo struct {
	Repositories      int      `json:"repositories"`
	Languages         []string `json:"languages"`
	ScratchRepository string   `json:"scratch_repository"`
	ScratchPath       string   `json:"scratch_path"`
	ScratchCommit     string   `json:"scratch_commit"`
	ScratchGoFiles    int      `json:"scratch_go_files"`
}

type results struct {
	Benchmark   string         `json:"benchmark"`
	Schema      string         `json:"schema"`
	Command     string         `json:"command"`
	Commit      string         `json:"commit"`
	GeneratedAt string         `json:"generated_at"`
	Environment environment    `json:"environment"`
	Corpus      corpusInfo     `json:"corpus"`
	SearchTool  string         `json:"search_tool"`
	Warmup      *passCost      `json:"warmup"`
	Passes      []passCost     `json:"edit_passes"`
	Grep        []grepQuestion `json:"grep_arm"`
	Summary     summary        `json:"summary"`
	Limitations []string       `json:"limitations"`
}

// ---------------------------------------------------------------------------
// the run
// ---------------------------------------------------------------------------

func run(ctx context.Context, options flags) error {
	if options.edits < 1 {
		return fmt.Errorf("-edits must be at least 1")
	}

	loaded, err := config.Load(options.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	source, index, err := findRepository(loaded.Repositories, options.scratchRepo)
	if err != nil {
		return err
	}

	scratch, cleanupScratch, err := temporaryDirectory(options.scratchRoot, "edit-frequency-scratch")
	if err != nil {
		return fmt.Errorf("scratch directory: %w", err)
	}
	root, cleanupRoot, err := temporaryDirectory(options.root, "edit-frequency-root")
	if err != nil {
		return fmt.Errorf("generation root: %w", err)
	}
	cacheDirectory, cleanupCache, err := temporaryDirectory(options.cache, "edit-frequency-cache")
	if err != nil {
		return fmt.Errorf("fact cache: %w", err)
	}
	if !options.keep {
		defer cleanupScratch()
		defer cleanupRoot()
		defer cleanupCache()
	}

	// The private root must not be the machine's own. A benchmark that
	// publishes into the live store would leave the user's graph pointing at a
	// generation built from files this harness edited.
	liveRoot := filepath.Dir(loaded.Config.Storage.DatabasePath)
	if sameDirectory(root, liveRoot) {
		return fmt.Errorf("refusing to publish into the live generation root %q", liveRoot)
	}
	if within(source.Path, scratch) || within(scratch, source.Path) {
		return fmt.Errorf("refusing to edit inside the registered repository %q", source.Path)
	}

	fmt.Fprintf(os.Stderr, "corpus:   %d repositories from %s\n", len(loaded.Repositories.Repositories), loaded.RepositoriesPath)
	fmt.Fprintf(os.Stderr, "scratch:  %s -> %s\n", source.Path, scratch)
	fmt.Fprintf(os.Stderr, "root:     %s\n", root)

	commit, err := copyTrackedFiles(ctx, source.Path, scratch)
	if err != nil {
		return fmt.Errorf("copy %q: %w", source.Path, err)
	}
	if err := initialiseScratchHistory(ctx, scratch, source.Name, commit); err != nil {
		return fmt.Errorf("initialise history of %q: %w", scratch, err)
	}
	if err := linkDependencyDirectories(source.Path, scratch); err != nil {
		return fmt.Errorf("link dependencies of %q: %w", source.Path, err)
	}

	targets, err := editTargets(scratch, source.Exclusions)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("no Go file in %q to edit", scratch)
	}

	// The substituted entry keeps the name, the languages and the exclusions
	// of the registered repository: only its path moves. An exclusion dropped
	// here would index a tree the real pass never reads, and the two costs
	// would not be comparable.
	repositories := loaded.Repositories
	repositories.Repositories = append([]config.Repository(nil), repositories.Repositories...)
	repositories.Repositories[index].Path = scratch

	registry, err := workspace.NewRegistry(ctx, repositories)
	if err != nil {
		return fmt.Errorf("register repositories: %w", err)
	}

	base := indexing.OptionsFromConfig(loaded.Config)
	base.Repositories = registry.List()
	base.WorkingDirectory = scratch
	base.ResolverVersion = resolverVersion(loaded.Config)
	base.Root = root
	base.CacheMode = indexer.CacheOn
	base.CacheDirectory = cacheDirectory
	base.SyntheticWorkFile = filepath.Join(root, "go.work")
	base.RustTargetDirectory = filepath.Join(root, "rust-target")
	base.JavaTargetDirectory = filepath.Join(root, "java-target")
	base.CSharpTargetDirectory = filepath.Join(root, "csharp-target")
	base.Metrics = nil

	out := results{
		Benchmark:   "edit-frequency",
		Schema:      schemaVersion,
		Command:     strings.Join(os.Args, " "),
		Commit:      describeCommit(ctx),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Environment: environment{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Go: runtime.Version()},
		Corpus: corpusInfo{
			Repositories:      len(repositories.Repositories),
			Languages:         corpusLanguages(repositories),
			ScratchRepository: source.Name,
			ScratchPath:       scratch,
			ScratchCommit:     commit,
			ScratchGoFiles:    len(targets),
		},
	}

	// The warm-up is the pass that fills the fact cache. It is measured and
	// published, and it is excluded from every summary: an editing agent never
	// runs it, and mixing a cold pass into the median is how a benchmark
	// reports a cost nobody pays.
	fmt.Fprintf(os.Stderr, "\n[warm-up] cold fact cache\n")
	warmup, err := measurePass(ctx, base, 0, "warm-up", "")
	if err != nil {
		return fmt.Errorf("warm-up pass: %w", err)
	}
	out.Warmup = &warmup
	reportPass(warmup)

	for step := 1; step <= options.edits; step++ {
		target := targets[(step-1)%len(targets)]
		if err := applyEdit(target, step); err != nil {
			return fmt.Errorf("edit %s: %w", target, err)
		}
		relative, _ := filepath.Rel(scratch, target)
		fmt.Fprintf(os.Stderr, "\n[edit %d/%d] %s\n", step, options.edits, relative)
		cost, err := measurePass(ctx, base, step, "edit", relative)
		if err != nil {
			return fmt.Errorf("pass after edit %d: %w", step, err)
		}
		out.Passes = append(out.Passes, cost)
		reportPass(cost)
	}

	tool := resolveSearchTool()
	fmt.Fprintf(os.Stderr, "\n[search arm: %s]\n", tool.name)
	out.SearchTool = tool.name
	out.Grep = measureGrepArm(ctx, tool, registry.List(), questionSet(options.questions), options.grepRepeats)
	for _, question := range out.Grep {
		if question.Failed {
			fmt.Fprintf(os.Stderr, "  %-28s %s\n", question.Symbol, question.FailureMessage)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-28s %6.3f s  %d matches in %d files\n",
			question.Symbol, question.Seconds, question.Matches, question.MatchedFiles)
	}

	out.Summary = summarise(out.Passes, out.Grep)
	out.Limitations = limitations(source.Name, options.edits)

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if options.output != "" {
		if err := os.WriteFile(options.output, append(encoded, '\n'), 0o644); err != nil {
			return fmt.Errorf("write %q: %w", options.output, err)
		}
		fmt.Fprintf(os.Stderr, "\nwrote %s\n", options.output)
	} else {
		fmt.Println(string(encoded))
	}
	reportSummary(out.Summary)
	return nil
}

// measurePass runs one full pass and splits it where the rebuild begins.
func measurePass(ctx context.Context, base indexing.FullOptions, step int, label, edited string) (passCost, error) {
	options := base
	var rebuildStarted time.Time
	options.RebuildProgress = func(rebuild.StageName) {
		if rebuildStarted.IsZero() {
			rebuildStarted = time.Now()
		}
	}

	start := time.Now()
	result, err := indexing.RunFull(ctx, options)
	total := time.Since(start)
	if err != nil {
		return passCost{}, err
	}

	cost := passCost{
		Step:          step,
		Label:         label,
		EditedFile:    edited,
		TotalSeconds:  total.Seconds(),
		CacheMode:     string(result.IndexReport.Cache.Mode),
		CacheHits:     result.IndexReport.Cache.Hits,
		CacheMisses:   result.IndexReport.Cache.Misses,
		CacheVerified: result.IndexReport.Cache.Verified,
		Files:         result.Counts.Files,
		Symbols:       result.Counts.Symbols,
		Edges:         result.Counts.Edges,
		GenerationID:  result.RebuildReport.GenerationID,
	}
	if !rebuildStarted.IsZero() {
		cost.AnalysisSeconds = rebuildStarted.Sub(start).Seconds()
		cost.PublicationSeconds = total.Seconds() - cost.AnalysisSeconds
	}
	for _, stage := range result.RebuildReport.Stages {
		cost.Stages = append(cost.Stages, stageCost{Name: string(stage.Name), Seconds: stage.DurationMS / 1000})
	}
	return cost, nil
}

// applyEdit appends one exported function to a Go file.
//
// It is an addition rather than a rewrite because the question is what a pass
// costs after an edit, and an addition is the edit whose effect on the graph is
// unambiguous: a new symbol, and every fact the file already asserted still
// asserted. A body rewritten in place would leave the reader unable to tell a
// slow pass from a pass that had more to do.
func applyEdit(path string, step int) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, `
// editFrequencyProbe%d is written by benchmarks/edit-frequency. It is one
// appended declaration, standing in for the edit an agent makes between two
// questions.
func editFrequencyProbe%d() int { return %d }
`, step, step, step)
	return err
}

// ---------------------------------------------------------------------------
// the grep arm
// ---------------------------------------------------------------------------

// defaultQuestions are common names: the class where the graph claims its
// advantage, and therefore the class where a crossover measured against grep
// is worth measuring at all. A rare name in one small repository is already
// documented as grep's win, and adding it here would flatter neither arm.
var defaultQuestions = []string{
	"NewServer",
	"Run",
	"Load",
	"Close",
	"Options",
	"Report",
}

func questionSet(path string) []string {
	if path == "" {
		return defaultQuestions
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "edit-frequency: read questions %q: %v; using the built-in set\n", path, err)
		return defaultQuestions
	}
	var questions []string
	for _, line := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			questions = append(questions, trimmed)
		}
	}
	if len(questions) == 0 {
		return defaultQuestions
	}
	return questions
}

// measureGrepArm times what answering each question costs without the graph:
// one search over every registered repository, then reading every file it
// matched. Reading the files is not optional and is not padding -- a list of
// line numbers is not an answer to "who calls this", and the arm that skipped
// it would be measuring a different question from the one the graph answers.
func measureGrepArm(ctx context.Context, tool searchTool, repositories []workspace.Repository, questions []string, repeats int) []grepQuestion {
	roots := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		roots = append(roots, repository.Path)
	}
	if repeats < 1 {
		repeats = 1
	}

	measured := make([]grepQuestion, 0, len(questions))
	for _, symbol := range questions {
		question := grepQuestion{Symbol: symbol}
		for attempt := 0; attempt < repeats; attempt++ {
			searchStart := time.Now()
			matches, err := tool.search(ctx, symbol, roots)
			searchSeconds := time.Since(searchStart).Seconds()
			if err != nil {
				question.Failed = true
				question.FailureMessage = err.Error()
				break
			}
			files := distinctFiles(matches)
			readStart := time.Now()
			bytes := readAll(files)
			readSeconds := time.Since(readStart).Seconds()

			total := searchSeconds + readSeconds
			if attempt == 0 || total < question.Seconds {
				question.Matches = len(matches)
				question.MatchedFiles = len(files)
				question.FileBytes = bytes
				question.SearchSeconds = searchSeconds
				question.ReadSeconds = readSeconds
				question.Seconds = total
			}
		}
		measured = append(measured, question)
	}
	return measured
}

// searchTool is the text search the arm actually runs, resolved once.
//
// It prefers ripgrep, because that is the searcher the agent hosts ship and
// therefore the line the graph has to stay under. Falling back to the system
// grep needs the exclusions spelled out: ripgrep honours .gitignore and so
// never descends into an installed dependency tree, and a grep that did would
// be timing a search no session performs -- 7,5 GB of node_modules on this
// corpus, which would flatter the graph by an order of magnitude.
type searchTool struct {
	name      string
	arguments func(symbol string, roots []string) []string
}

func resolveSearchTool() searchTool {
	if _, err := exec.LookPath("rg"); err == nil {
		return searchTool{
			name: "ripgrep",
			arguments: func(symbol string, roots []string) []string {
				return append([]string{"--no-heading", "--line-number", "--word-regexp", "--", symbol}, roots...)
			},
		}
	}
	return searchTool{
		name: "grep",
		arguments: func(symbol string, roots []string) []string {
			return append([]string{
				"-rnw", "--binary-files=without-match",
				"--exclude-dir=node_modules", "--exclude-dir=.git", "--exclude-dir=dist",
				"--exclude-dir=target", "--exclude-dir=build", "--exclude-dir=vendor",
				"--exclude-dir=.next", "-e", symbol,
			}, roots...)
		},
	}
}

func (tool searchTool) binary() string {
	if tool.name == "ripgrep" {
		return "rg"
	}
	return "grep"
}

// search runs the word-boundary search a session would run. It runs the real
// searcher because that is what the session runs: a Go reimplementation would
// measure this harness rather than the arm.
func (tool searchTool) search(ctx context.Context, symbol string, roots []string) ([]string, error) {
	command := exec.CommandContext(ctx, tool.binary(), tool.arguments(symbol, roots)...)
	output, err := command.Output()
	if err != nil {
		// Both searchers exit 1 when they matched nothing, which is an answer
		// and not a failure. Anything else is a failure and is declared as one.
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("%s %q: %w", tool.name, symbol, err)
	}
	var matches []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) != "" {
			matches = append(matches, line)
		}
	}
	return matches, nil
}

func distinctFiles(matches []string) []string {
	seen := make(map[string]struct{}, len(matches))
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		index := strings.Index(match, ":")
		if index <= 0 {
			continue
		}
		path := match[:index]
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	return files
}

func readAll(files []string) int64 {
	var total int64
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		total += int64(len(content))
	}
	return total
}

// ---------------------------------------------------------------------------
// summary
// ---------------------------------------------------------------------------

func summarise(passes []passCost, questions []grepQuestion) summary {
	result := summary{Passes: len(passes)}
	if len(passes) == 0 {
		return result
	}

	totals := make([]float64, 0, len(passes))
	analyses := make([]float64, 0, len(passes))
	publications := make([]float64, 0, len(passes))
	for _, pass := range passes {
		totals = append(totals, pass.TotalSeconds)
		analyses = append(analyses, pass.AnalysisSeconds)
		publications = append(publications, pass.PublicationSeconds)
	}
	result.MedianTotalSeconds = median(totals)
	result.MedianAnalysisSeconds = median(analyses)
	result.MedianPublicationSeconds = median(publications)
	result.FastestTotalSeconds = minimum(totals)
	result.SlowestTotalSeconds = maximum(totals)
	if result.MedianTotalSeconds > 0 {
		result.AnalysisShare = result.MedianAnalysisSeconds / result.MedianTotalSeconds
		result.PublicationShare = result.MedianPublicationSeconds / result.MedianTotalSeconds
	}

	grepSeconds := make([]float64, 0, len(questions))
	for _, question := range questions {
		if !question.Failed {
			grepSeconds = append(grepSeconds, question.Seconds)
		}
	}
	if len(grepSeconds) > 0 {
		result.GrepSecondsPerQuestion = median(grepSeconds)
		if result.GrepSecondsPerQuestion > 0 {
			result.RebuildsPerGrepQuestion = result.MedianTotalSeconds / result.GrepSecondsPerQuestion
		}
	}
	return result
}

func limitations(scratch string, edits int) []string {
	return []string{
		fmt.Sprintf("The edits land in a private copy of %q only. The other repositories of the corpus are read where they are and are never written to, so this measures the edit rate of an agent working inside one repository -- which is the workload issue 106 names, and not a corpus-wide pull.", scratch),
		fmt.Sprintf("Every step is one appended declaration in one file, %d of them. A larger edit changes what the analysis half costs and cannot change what the publication half costs, because publication rewrites the whole graph either way.", edits),
		"The warm-up pass fills a private fact cache, so the machine's own cache is neither read nor written. A run against a cache warmed by other work would see fewer misses in the warm-up and the same misses in every edit pass.",
		"The search arm times the real searcher plus reading every file it matched. It does not price the tokens either arm spends; those are measured in benchmarks/mcp-token-cost and benchmarks/graph-tools-comparison, and the report reads the crossover in both currencies.",
		"The fact cache is keyed on the fingerprint of the indexing binary, so a run driven by `go run` recompiles to a new path and can never hit it. Every pass here is driven by one built binary, and the reproduce command says so.",
		"Wall clock on a shared machine is not an SLO. No gate is asserted and no acceptance verdict is emitted.",
	}
}

func reportPass(cost passCost) {
	fmt.Fprintf(os.Stderr, "  total %6.3f s = analysis %6.3f s + publication %6.3f s   (cache %d hit / %d miss)\n",
		cost.TotalSeconds, cost.AnalysisSeconds, cost.PublicationSeconds, cost.CacheHits, cost.CacheMisses)
	for _, stage := range cost.Stages {
		if stage.Seconds > 0 {
			fmt.Fprintf(os.Stderr, "      %-14s %6.3f s\n", stage.Name, stage.Seconds)
		}
	}
}

func reportSummary(result summary) {
	fmt.Fprintf(os.Stderr, "\nmedian pass after one edit   %6.3f s\n", result.MedianTotalSeconds)
	fmt.Fprintf(os.Stderr, "  analysis                   %6.3f s  (%.1f %%)\n", result.MedianAnalysisSeconds, result.AnalysisShare*100)
	fmt.Fprintf(os.Stderr, "  publication                %6.3f s  (%.1f %%)\n", result.MedianPublicationSeconds, result.PublicationShare*100)
	fmt.Fprintf(os.Stderr, "median grep-and-read question %6.3f s\n", result.GrepSecondsPerQuestion)
	fmt.Fprintf(os.Stderr, "one rebuild buys              %6.1f grep-and-read questions\n", result.RebuildsPerGrepQuestion)
}

// ---------------------------------------------------------------------------
// scratch corpus
// ---------------------------------------------------------------------------

func findRepository(file config.RepositoriesFile, name string) (config.Repository, int, error) {
	for index, repository := range file.Repositories {
		if repository.Name == name {
			return repository, index, nil
		}
	}
	names := make([]string, 0, len(file.Repositories))
	for _, repository := range file.Repositories {
		names = append(names, repository.Name)
	}
	sort.Strings(names)
	return config.Repository{}, 0, fmt.Errorf("no registered repository named %q; registered: %s", name, strings.Join(names, ", "))
}

// copyTrackedFiles writes the tracked files of a repository into the scratch
// directory and reports the commit they came from.
//
// It copies what git tracks rather than what the directory holds: a build
// output tree is not source, and copying it would put gigabytes between the
// harness and the first measurement without adding a fact to the graph.
func copyTrackedFiles(ctx context.Context, source, destination string) (string, error) {
	commit, err := gitOutput(ctx, source, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	dirty, err := gitOutput(ctx, source, "status", "--porcelain")
	if err == nil && strings.TrimSpace(dirty) != "" {
		commit += "-dirty"
	}

	listed, err := gitOutput(ctx, source, "ls-files", "-z")
	if err != nil {
		return "", fmt.Errorf("list tracked files: %w", err)
	}
	for _, relative := range strings.Split(listed, "\x00") {
		if relative == "" {
			continue
		}
		from := filepath.Join(source, relative)
		info, err := os.Lstat(from)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		to := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return "", err
		}
		content, err := os.ReadFile(from)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(to, content, info.Mode().Perm()); err != nil {
			return "", err
		}
	}
	return commit, nil
}

// initialiseScratchHistory gives the copy a git history of its own.
//
// The registry reads a commit, a branch and a dirty flag from every repository
// it registers, and refuses one that has none. The copy therefore carries a
// single commit whose message names the commit it was taken from, so a
// generation built here can still be traced to the source tree -- and every
// edit after it shows up as dirty, which is what an agent's working tree looks
// like between two questions.
func initialiseScratchHistory(ctx context.Context, scratch, name, origin string) error {
	steps := [][]string{
		{"init", "--quiet", "--initial-branch=edit-frequency"},
		{"add", "--all"},
		{
			"-c", "user.name=edit-frequency",
			"-c", "user.email=edit-frequency@localhost",
			"commit", "--quiet", "--no-gpg-sign", "--allow-empty",
			"--message", fmt.Sprintf("scratch copy of %s at %s", name, origin),
		},
	}
	for _, step := range steps {
		if _, err := gitOutput(ctx, scratch, step...); err != nil {
			return fmt.Errorf("git %s: %w", strings.Join(step, " "), err)
		}
	}
	return nil
}

// linkDependencyDirectories points the scratch copy at the installed
// dependencies of the original.
//
// They are not tracked, so the copy above does not carry them, and a TypeScript
// project without them declares no program: the pass would index less than the
// real one and the two costs would not be comparable. A symlink is the right
// shape here -- the harness reads them and never writes through them.
func linkDependencyDirectories(source, destination string) error {
	return filepath.WalkDir(destination, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Name() == "node_modules" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(destination, path)
		if err != nil {
			return err
		}
		original := filepath.Join(source, relative, "node_modules")
		if info, statErr := os.Stat(original); statErr != nil || !info.IsDir() {
			return nil
		}
		link := filepath.Join(path, "node_modules")
		// A repository can track a node_modules of its own -- a fixture of an
		// installed package is one -- and the copy already carries it. Linking
		// over it would replace a tracked tree with the machine's install.
		if _, statErr := os.Lstat(link); statErr == nil {
			return nil
		}
		return os.Symlink(original, link)
	})
}

// editTargets is the ordered list of Go files the steps append to.
//
// The order is the sorted path order, which makes a run reproducible without a
// seed: the same corpus and the same step count edit the same files.
func editTargets(root string, exclusions []string) ([]string, error) {
	var targets []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "node_modules" || name == ".git" || name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			for _, exclusion := range exclusions {
				if relative == exclusion || strings.HasPrefix(relative, exclusion+string(os.PathSeparator)) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		targets = append(targets, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %q: %w", root, err)
	}
	sort.Strings(targets)
	return targets, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func temporaryDirectory(configured, pattern string) (string, func(), error) {
	if configured != "" {
		if err := os.MkdirAll(configured, 0o755); err != nil {
			return "", nil, err
		}
		return configured, func() {}, nil
	}
	path, err := os.MkdirTemp("", pattern+"-*")
	if err != nil {
		return "", nil, err
	}
	return path, func() { _ = os.RemoveAll(path) }, nil
}

func sameDirectory(left, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return leftResolved == rightResolved
}

func within(parent, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func corpusLanguages(file config.RepositoriesFile) []string {
	seen := make(map[string]struct{})
	for _, repository := range file.Repositories {
		for _, language := range repository.Languages {
			seen[language] = struct{}{}
		}
	}
	languages := make([]string, 0, len(seen))
	for language := range seen {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	return languages
}

// resolverVersion identifies the resolver a generation was built by. The
// harness has no release metadata to read, so it derives a stable value from
// the configuration and declares it: two runs of the same build agree, and a
// run cannot claim a released resolver it is not.
func resolverVersion(configuration config.Config) string {
	sum := sha256.Sum256([]byte(configuration.Storage.DatabasePath + "|edit-frequency"))
	return "edit-frequency-" + hex.EncodeToString(sum[:4])
}

func describeCommit(ctx context.Context) string {
	commit, err := gitOutput(ctx, ".", "rev-parse", "HEAD")
	if err != nil {
		return "unknown"
	}
	if dirty, err := gitOutput(ctx, ".", "status", "--porcelain"); err == nil && strings.TrimSpace(dirty) != "" {
		return commit + "-dirty"
	}
	return commit
}

func gitOutput(ctx context.Context, directory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func minimum(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[0]
}

func maximum(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)-1]
}
