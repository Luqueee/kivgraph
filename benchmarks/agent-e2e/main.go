// Command agent-e2e measures whether a context layer helps an agent ship a real
// change, which is the only question a user of one actually has.
//
// The earlier benchmark priced single questions. This one runs the same coding
// agent three ways over the same real commits -- cold, with kivgraph mounted, with
// graft mounted -- and scores each run against the files the author changed. No
// judge model and no similarity score: git says which files the arm wrote, and the
// commit says which files it should have written.
//
// The corpus is never modified. Every arm works in a private copy whose target
// repository is rebuilt at the commit's parent as a one-commit repository, so the
// answer is not reachable from the state the agent sees; the harness asserts that
// instead of trusting it.
package main

import (
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
	benchmarkName    = "agent-e2e"
	defaultDirectory = "benchmarks/agent-e2e"
)

type config struct {
	Kivgraph     string
	Graft        string
	Agent        string
	Model        string
	Corpus       string
	Root         string
	Home         string
	AgentHome    string
	GraftContext string
	Directory    string
	Trials       int
	Only         string
	Arms         string
	Setup        bool
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Kivgraph, "kivgraph", "kivgraph", "kivgraph executable")
	flag.StringVar(&cfg.Graft, "graft", "graft", "graft executable")
	flag.StringVar(&cfg.Agent, "agent", "claude", "headless coding agent executable")
	flag.StringVar(&cfg.Model, "model", "sonnet", "model every arm runs on")
	flag.StringVar(&cfg.Corpus, "corpus", "/Users/adria/Documents/programacion/projects/kena", "corpus read to build the private copy")
	flag.StringVar(&cfg.Root, "root", "/private/tmp/e2e-kena", "private copy the arms work in")
	flag.StringVar(&cfg.Home, "home", "/private/tmp/e2e-kivhome", "isolated HOME holding kivgraph's state for the copy")
	flag.StringVar(&cfg.AgentHome, "agent-home", os.Getenv("HOME"), "HOME the agent runs under, for its credentials")
	flag.StringVar(&cfg.GraftContext, "graft-context", "/private/tmp/e2e-graft-ctx", "graft context for the copy")
	flag.StringVar(&cfg.Directory, "dir", defaultDirectory, "benchmark directory")
	flag.IntVar(&cfg.Trials, "trials", 1, "trials per task per arm")
	flag.StringVar(&cfg.Only, "only", "", "comma-separated task ids to run, empty for all")
	flag.StringVar(&cfg.Arms, "arms", "cold,kivgraph,graft", "comma-separated arms to run")
	flag.BoolVar(&cfg.Setup, "setup", true, "rebuild the private copy and register both indexes first")
	flag.Parse()

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type results struct {
	Benchmark   string            `json:"benchmark"`
	GeneratedAt time.Time         `json:"generated_at"`
	Commit      string            `json:"commit"`
	Model       string            `json:"model"`
	Agent       string            `json:"agent"`
	Environment map[string]string `json:"environment"`
	Corpus      string            `json:"corpus"`
	Arms        []string          `json:"arms"`
	Trials      int               `json:"trials"`
	Tasks       []taskRecord      `json:"tasks"`
	Runs        []runResult       `json:"runs"`
	Aggregate   map[string]armAgg `json:"aggregate"`
	Setup       setupCost         `json:"setup"`
}

type taskRecord struct {
	ID       string   `json:"id"`
	Repo     string   `json:"repo"`
	Language string   `json:"language"`
	Commit   string   `json:"commit"`
	Subject  string   `json:"subject"`
	Truth    []string `json:"truth_files"`
	Prompt   string   `json:"prompt"`
}

type setupCost struct {
	IndexKivgraphMS []float64 `json:"index_kivgraph_ms"`
	IndexGraftMS    []float64 `json:"index_graft_ms"`
}

// armAgg is one arm's outcome over the whole matrix. Correctness comes first
// because that is what a cheaper arm has to be compared against.
type armAgg struct {
	Runs          int     `json:"runs"`
	Exact         int     `json:"exact"`
	PrecisionMean float64 `json:"precision_mean"`
	RecallMean    float64 `json:"recall_mean"`
	CostUSD       float64 `json:"cost_usd_total"`
	CostMean      float64 `json:"cost_usd_mean"`
	TokensMean    float64 `json:"tokens_mean"`
	ToolCallsMean float64 `json:"tool_calls_mean"`
	ContextMean   float64 `json:"context_tool_calls_mean"`
	SecondsMean   float64 `json:"seconds_mean"`
	Errored       int     `json:"errored"`
	Leaks         int     `json:"leaks"`
}

func run(cfg config) error {
	set, err := loadTasks(filepath.Join(cfg.Directory, "tasks.json"))
	if err != nil {
		return err
	}
	selected := selectTasks(set.Tasks, cfg.Only)
	if len(selected) == 0 {
		return fmt.Errorf("no task matched --only %q", cfg.Only)
	}
	chosen := []arm{}
	for _, candidate := range arms(cfg) {
		if strings.Contains(cfg.Arms, candidate.Name) {
			chosen = append(chosen, candidate)
		}
	}
	if len(chosen) == 0 {
		return fmt.Errorf("no arm matched --arms %q", cfg.Arms)
	}

	space := workspace{Root: cfg.Root, Corpus: cfg.Corpus}
	index := indexer{
		Kivgraph: cfg.Kivgraph, Graft: cfg.Graft, Home: cfg.Home,
		GraftContext: cfg.GraftContext, Root: cfg.Root,
	}
	out := results{
		Benchmark: benchmarkName, GeneratedAt: time.Now().UTC(), Model: cfg.Model,
		Agent: cfg.Agent, Corpus: cfg.Corpus, Trials: cfg.Trials,
		Environment: map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()},
		Aggregate:   map[string]armAgg{},
	}
	out.Commit = strings.TrimSpace(capture("git", "rev-parse", "--short", "HEAD"))
	for _, candidate := range chosen {
		out.Arms = append(out.Arms, candidate.Name)
	}

	if cfg.Setup {
		fmt.Println("building the private copy...")
		if err := space.build(); err != nil {
			return err
		}
		fmt.Println("registering kivgraph against the copy...")
		if err := index.register(); err != nil {
			return err
		}
	}

	for _, t := range selected {
		fmt.Printf("\n=== %s (%s, %d files) ===\n", t.ID, t.Language, t.NTruth)
		if err := space.prepare(t); err != nil {
			return err
		}
		kivMS, graftMS, err := index.reindex()
		if err != nil {
			return err
		}
		out.Setup.IndexKivgraphMS = append(out.Setup.IndexKivgraphMS, kivMS)
		out.Setup.IndexGraftMS = append(out.Setup.IndexGraftMS, graftMS)
		fmt.Printf("  indexed: kivgraph %.1fs, graft %.1fs\n", kivMS/1000, graftMS/1000)
		out.Tasks = append(out.Tasks, taskRecord{
			ID: t.ID, Repo: t.Repo, Language: t.Language, Commit: t.Commit,
			Subject: t.Subject, Truth: t.Truth, Prompt: prompt(t, cfg.Root),
		})

		for trial := 1; trial <= cfg.Trials; trial++ {
			for _, candidate := range chosen {
				if err := space.restore(t); err != nil {
					return err
				}
				result, err := candidate.run(cfg, t, trial)
				if err != nil {
					return fmt.Errorf("%s/%s: %w", t.ID, candidate.Name, err)
				}
				inside, outside, err := space.changedFiles(t)
				if err != nil {
					return err
				}
				result.score(t, inside, outside)
				result.auditLeak(t)
				out.Runs = append(out.Runs, result)
				fmt.Printf("  %-9s trial %d: P=%.2f R=%.2f  %d files, %d calls (%d ctx), $%.4f, %.0fs%s\n",
					candidate.Name, trial, result.Precision, result.Recall, len(result.Wrote),
					result.ToolCalls, result.ContextUse, result.CostUSD, result.MS/1000,
					leakNote(result))
				if err := writeJSON(filepath.Join(cfg.Directory, "results.json"), &out); err != nil {
					return err
				}
			}
		}
		if err := space.restore(t); err != nil {
			return err
		}
	}
	out.Aggregate = aggregate(out.Runs)
	if err := writeJSON(filepath.Join(cfg.Directory, "results.json"), &out); err != nil {
		return err
	}
	printSummary(out)
	return nil
}

func leakNote(r runResult) string {
	notes := []string{}
	if r.Leak != "" {
		notes = append(notes, "LEAK: "+r.Leak)
	}
	if r.Errored {
		notes = append(notes, "ERROR: "+r.Error)
	}
	if len(r.Outside) > 0 {
		notes = append(notes, "also wrote in "+strings.Join(r.Outside, ","))
	}
	if len(notes) == 0 {
		return ""
	}
	return "  [" + strings.Join(notes, "; ") + "]"
}

func selectTasks(all []task, only string) []task {
	if strings.TrimSpace(only) == "" {
		return all
	}
	wanted := map[string]bool{}
	for _, id := range strings.Split(only, ",") {
		wanted[strings.TrimSpace(id)] = true
	}
	picked := []task{}
	for _, t := range all {
		if wanted[t.ID] {
			picked = append(picked, t)
		}
	}
	return picked
}

func aggregate(runs []runResult) map[string]armAgg {
	byArm := map[string][]runResult{}
	for _, r := range runs {
		byArm[r.Arm] = append(byArm[r.Arm], r)
	}
	out := map[string]armAgg{}
	for name, group := range byArm {
		summary := armAgg{Runs: len(group)}
		for _, r := range group {
			summary.PrecisionMean += r.Precision
			summary.RecallMean += r.Recall
			summary.CostUSD += r.CostUSD
			summary.TokensMean += float64(r.InTokens + r.OutTokens + r.CacheRead + r.CacheWrite)
			summary.ToolCallsMean += float64(r.ToolCalls)
			summary.ContextMean += float64(r.ContextUse)
			summary.SecondsMean += r.MS / 1000
			if r.Exact {
				summary.Exact++
			}
			if r.Errored {
				summary.Errored++
			}
			if r.Leak != "" {
				summary.Leaks++
			}
		}
		n := float64(len(group))
		summary.PrecisionMean /= n
		summary.RecallMean /= n
		summary.TokensMean /= n
		summary.ToolCallsMean /= n
		summary.ContextMean /= n
		summary.SecondsMean /= n
		summary.CostMean = summary.CostUSD / n
		out[name] = summary
	}
	return out
}

func printSummary(out results) {
	fmt.Printf("\n%s  %s  model=%s  agent=%s\n", out.Benchmark, out.Commit, out.Model, out.Agent)
	fmt.Printf("%-10s %5s %7s %6s %6s %9s %8s %7s %7s %6s\n",
		"arm", "runs", "exact", "P", "R", "cost$", "tokens", "calls", "ctx", "s")
	names := make([]string, 0, len(out.Aggregate))
	for name := range out.Aggregate {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a := out.Aggregate[name]
		fmt.Printf("%-10s %5d %3d/%-3d %6.2f %6.2f %9.4f %8.0f %7.1f %7.1f %6.0f\n",
			name, a.Runs, a.Exact, a.Runs, a.PrecisionMean, a.RecallMean,
			a.CostUSD, a.TokensMean, a.ToolCallsMean, a.ContextMean, a.SecondsMean)
	}
	for _, name := range names {
		if a := out.Aggregate[name]; a.Errored > 0 || a.Leaks > 0 {
			fmt.Printf("%-10s %d errored, %d leaked\n", name, a.Errored, a.Leaks)
		}
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func capture(command string, arguments ...string) string {
	out, err := exec.Command(command, arguments...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
