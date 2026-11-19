// Command language-recall measures, per language, how many of the files that
// really depend on a declaration Kivgraph reports as referencing it.
//
// The ground truth is the language's own type checker. A declaration is renamed
// in a copy of the source, the project is type-checked again, and the files
// that gain an error are, by construction, the files that use the declaration.
// Kivgraph is then asked who references the same declaration and the two file
// sets are compared.
//
// What this does and does not measure is in the report. In short: the oracle
// and Kivgraph share each language's type checker, so type resolution itself is
// not on trial. What is on trial is everything Kivgraph does around it --
// which files and build variants it loads, how it turns resolved uses into
// edges, and what it filters.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	benchmarkName = "language-recall"
	command       = "go run ./benchmarks/language-recall -kivgraph <binary> -repo-root ."
)

type config struct {
	Kivgraph  string
	RepoRoot  string
	Work      string
	Output    string
	Languages string
	Repos     string
	Count     int
	Seed      int64
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var cfg config
	flag.StringVar(&cfg.Kivgraph, "kivgraph", "", "path to a kivgraph binary built with the ladybug tag")
	flag.StringVar(&cfg.RepoRoot, "repo-root", ".", "a Kivgraph checkout: its ts-worker runs the TypeScript facts")
	flag.StringVar(&cfg.Work, "work", "", "directory for the cloned corpus; kept between runs when given")
	flag.StringVar(&cfg.Output, "output", "benchmarks/"+benchmarkName, "directory for results.json and report.md")
	flag.StringVar(&cfg.Languages, "languages", "go,typescript,rust,dart", "comma-separated languages to measure")
	flag.StringVar(&cfg.Repos, "repos", "", "comma-separated repositories to measure instead of the default corpus, extra ones included")
	flag.IntVar(&cfg.Count, "count", 100, "declarations sampled per language")
	flag.Int64Var(&cfg.Seed, "seed", 1, "sampling seed")
	flag.Parse()
	if strings.TrimSpace(cfg.Kivgraph) == "" {
		return errors.New("-kivgraph is required: this harness measures a real binary")
	}
	binary, err := filepath.Abs(cfg.Kivgraph)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if cfg.Count < 1 {
		return errors.New("-count must be at least 1")
	}
	work := cfg.Work
	if work == "" {
		if work, err = os.MkdirTemp("", benchmarkName+"-"); err != nil {
			return err
		}
		defer os.RemoveAll(work)
	}
	if work, err = filepath.Abs(work); err != nil {
		return err
	}

	wanted := map[string]bool{}
	for _, language := range strings.Split(cfg.Languages, ",") {
		wanted[strings.TrimSpace(language)] = true
	}
	named := map[string]bool{}
	for _, name := range strings.Split(cfg.Repos, ",") {
		if name = strings.TrimSpace(name); name != "" {
			named[name] = true
		}
	}
	var repositories []corpusRepository
	for _, repository := range corpus() {
		if len(named) > 0 {
			if named[repository.Name] {
				repositories = append(repositories, repository)
			}
			continue
		}
		if wanted[repository.Language] && !repository.Extra {
			repositories = append(repositories, repository)
		}
	}
	if len(repositories) == 0 {
		return fmt.Errorf("no corpus repository for %q", cfg.Languages)
	}

	started := time.Now()
	oracles := map[string]*oracle{}
	for i := range repositories {
		repository := &repositories[i]
		if err := prepare(ctx, work, repository); err != nil {
			return fmt.Errorf("prepare %s: %w", repository.Name, err)
		}
		oracle, err := newOracle(ctx, *repository)
		if err != nil {
			return fmt.Errorf("baseline of %s: %w", repository.Name, err)
		}
		oracles[repository.Name] = oracle
	}

	measured := results{
		Benchmark: benchmarkName,
		Command:   command,
		Date:      time.Now().UTC().Format("2006-01-02"),
		Seed:      cfg.Seed,
		Count:     cfg.Count,
		Environment: environment{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version(),
			Kivgraph: kivgraphVersion(ctx, binary), Node: toolVersion(ctx, "node", "--version"),
			TypeScript: typescriptVersion(ctx, work, repositories),
			Rust:       toolVersion(ctx, "rustc", "--version"),
		},
		Corpus:      describe(repositories),
		Limitations: limitations(),
	}
	// Two configurations of Kivgraph are measured for the languages that read
	// test files: the default, which reads them, and the language with
	// `include_tests: false`. They differ in one setting, and the gap between
	// them is what reading tests buys, which a single run would fold into the
	// recall.
	arms := []arm{{Name: "default"}}
	for _, language := range []string{"go", "dart"} {
		if wanted[language] {
			arms = append(arms, arm{Name: language + ".tests_excluded", Language: language, ExcludeTests: language})
		}
	}
	for _, current := range arms {
		var members []corpusRepository
		for _, repository := range repositories {
			if current.Language == "" || repository.Language == current.Language {
				members = append(members, repository)
			}
		}
		results, err := measureArm(ctx, binary, root, current, members, oracles, cfg)
		if err != nil {
			return fmt.Errorf("%s: %w", current.Name, err)
		}
		for _, result := range results {
			measured.Languages = append(measured.Languages, result)
			fmt.Printf("%-10s %-17s %d declarations, %d with dependents: file recall %.1f%%, file precision %.1f%%\n",
				result.Language, result.Arm, result.Sampled, result.WithDependents,
				100*result.Recall.Value, 100*result.Precision.Value)
		}
	}
	measured.WallSeconds = time.Since(started).Seconds()
	return writeResults(cfg.Output, measured)
}

// arm is one configuration of Kivgraph. Language narrows it to one language's
// repositories; empty means all of them.
type arm struct {
	Name         string
	Language     string
	ExcludeTests string // the config section whose include_tests is switched off
}

// measureArm indexes the corpus with a configuration, serves it, and measures
// each language in it.
func measureArm(ctx context.Context, binary, root string, current arm, members []corpusRepository, oracles map[string]*oracle, cfg config) ([]languageResult, error) {
	state, err := os.MkdirTemp("", benchmarkName+"-home-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(state)
	if err := index(ctx, binary, root, state, members, current.ExcludeTests); err != nil {
		return nil, err
	}
	session, stop, err := connect(ctx, binary, state)
	if err != nil {
		return nil, err
	}
	defer stop()
	var out []languageResult
	for _, language := range []string{"go", "typescript", "rust", "dart"} {
		var byLanguage []corpusRepository
		for _, repository := range members {
			if repository.Language == language {
				byLanguage = append(byLanguage, repository)
			}
		}
		if len(byLanguage) == 0 {
			continue
		}
		result, err := measureLanguage(ctx, session, oracles, byLanguage, language, cfg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", language, err)
		}
		result.Arm = current.Name
		out = append(out, result)
	}
	return out, nil
}
