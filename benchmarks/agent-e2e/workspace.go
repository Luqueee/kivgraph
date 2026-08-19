package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// taskSet is the frozen question set: real merged commits, their parent, and the
// files the author actually changed.
type taskSet struct {
	Version int    `json:"version"`
	Corpus  string `json:"corpus"`
	Tasks   []task `json:"tasks"`
}

// task is one re-implementation. The prompt is built from the commit's own
// subject and first paragraph with every path-like token removed, so the
// statement says what was wanted without saying where it lives: finding the
// files is the work being measured.
type task struct {
	ID       string   `json:"id"`
	Repo     string   `json:"repo"`
	Commit   string   `json:"commit"`
	Short    string   `json:"short"`
	Parent   string   `json:"parent"`
	Language string   `json:"language"`
	Subject  string   `json:"subject"`
	Intent   string   `json:"prompt_intent"`
	Truth    []string `json:"truth_files"`
	NTruth   int      `json:"n_truth"`
}

func loadTasks(path string) (taskSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return taskSet{}, fmt.Errorf("read %s: %w", path, err)
	}
	set := taskSet{}
	if err := json.Unmarshal(raw, &set); err != nil {
		return taskSet{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(set.Tasks) == 0 {
		return taskSet{}, fmt.Errorf("%s holds no tasks", path)
	}
	return set, nil
}

// workspace is the private copy every arm works in. The indexed repositories are
// never touched: the corpus is read to build this copy and nothing writes back.
type workspace struct {
	Root   string
	Corpus string
}

// build makes the copy once. node_modules is excluded: no arm runs a build or a
// test -- the shell is not in the tool list -- so the only thing it would change
// is how long the copy takes.
func (w workspace) build() error {
	if err := os.MkdirAll(w.Root, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", w.Root, err)
	}
	out, err := exec.Command("rsync", "-a", "--delete", "--exclude=node_modules",
		w.Corpus+"/", w.Root+"/").CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy corpus: %w (%s)", err, lastLines(string(out), 3))
	}
	return w.normalize()
}

// normalize discards whatever was uncommitted in the corpus when it was copied.
//
// Without it the copy starts dirty -- two repositories of this corpus carry an
// edited `go.sum` -- and the scorer would read those as files the agent wrote.
// The corpus itself is never touched: this runs inside the copy.
func (w workspace) normalize() error {
	repos, err := gitRepositories(w.Root)
	if err != nil {
		return fmt.Errorf("discover repositories: %w", err)
	}
	for _, repo := range repos {
		target := filepath.Join(w.Root, repo)
		for _, arguments := range [][]string{{"checkout", "--", "."}, {"clean", "-fdq"}} {
			if out, err := exec.Command("git", append([]string{"-C", target}, arguments...)...).CombinedOutput(); err != nil {
				return fmt.Errorf("normalize %s: %w (%s)", repo, err, lastLines(string(out), 2))
			}
		}
	}
	return nil
}

// prepare puts one repository of the copy at a task's parent commit, as a fresh
// repository holding exactly one commit.
//
// The single commit is the point. Checking the parent out inside a clone would
// leave the answer in the object database, one `git log` away, and the arm that
// happened to look would score perfectly for the wrong reason. Here the future
// does not exist: the commit being re-implemented is unreachable, which the
// harness asserts rather than assumes.
func (w workspace) prepare(t task) error {
	target := filepath.Join(w.Root, t.Repo)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("clear %s: %w", target, err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	source := filepath.Join(w.Corpus, t.Repo)
	archive := exec.Command("git", "-C", source, "archive", t.Parent)
	extract := exec.Command("tar", "-x", "-C", target)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe git archive: %w", err)
	}
	extract.Stdin = pipe
	if err := extract.Start(); err != nil {
		return fmt.Errorf("start tar: %w", err)
	}
	if err := archive.Run(); err != nil {
		return fmt.Errorf("git archive %s at %s: %w", t.Repo, t.Parent, err)
	}
	if err := extract.Wait(); err != nil {
		return fmt.Errorf("extract %s: %w", t.Repo, err)
	}
	for _, arguments := range [][]string{
		{"init", "-q", "-b", "bench"},
		{"-c", "user.email=bench@local", "-c", "user.name=bench", "add", "-A"},
		// The message names nothing. It used to say "state at <short>^", and an
		// agent that read .git/logs/HEAD -- one did -- would find the identifier of
		// the very commit it was re-implementing. It could not fetch that commit,
		// so no file list escaped, but a benchmark should not put the answer's name
		// inside the workspace it hands out.
		{"-c", "user.email=bench@local", "-c", "user.name=bench", "commit", "-qm", "workspace state"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", target}, arguments...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s in %s: %w (%s)", arguments[0], t.Repo, err, strings.TrimSpace(string(out)))
		}
	}
	return w.assertNoLeak(t)
}

// assertNoLeak refuses to run a task whose answer is reachable from the state the
// agent will see.
func (w workspace) assertNoLeak(t task) error {
	target := filepath.Join(w.Root, t.Repo)
	if err := exec.Command("git", "-C", target, "cat-file", "-e", t.Commit).Run(); err == nil {
		return fmt.Errorf("%s: the commit under test is reachable in the prepared state", t.ID)
	}
	depth, err := exec.Command("git", "-C", target, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("count history of %s: %w", t.Repo, err)
	}
	if strings.TrimSpace(string(depth)) != "1" {
		return fmt.Errorf("%s: prepared state holds %s commits, want 1", t.ID, strings.TrimSpace(string(depth)))
	}
	return nil
}

// restore returns the repository to the prepared state, discarding whatever an
// arm wrote. Every arm therefore starts from identical bytes.
func (w workspace) restore(t task) error {
	target := filepath.Join(w.Root, t.Repo)
	for _, arguments := range [][]string{{"checkout", "--", "."}, {"clean", "-fdq"}} {
		if out, err := exec.Command("git", append([]string{"-C", target}, arguments...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s in %s: %w (%s)", arguments[0], t.Repo, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// changedFiles is what an arm actually wrote, repository-relative, plus anything
// it wrote outside the task's repository. An edit in the wrong repository is a
// real outcome and is counted rather than ignored.
func (w workspace) changedFiles(t task) (inside []string, outside []string, err error) {
	target := filepath.Join(w.Root, t.Repo)
	out, err := exec.Command("git", "-C", target, "status", "--porcelain").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("status %s: %w", t.Repo, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[2:])
		if renamed := strings.Split(path, " -> "); len(renamed) == 2 {
			path = renamed[1]
		}
		inside = append(inside, strings.Trim(path, `"`))
	}
	sort.Strings(inside)
	return inside, w.otherRepositoryEdits(t), nil
}

// otherRepositoryEdits reports repositories other than the task's that an arm
// left dirty.
func (w workspace) otherRepositoryEdits(t task) []string {
	dirty := []string{}
	entries, err := gitRepositories(w.Root)
	if err != nil {
		return dirty
	}
	for _, repo := range entries {
		if repo == t.Repo {
			continue
		}
		out, err := exec.Command("git", "-C", filepath.Join(w.Root, repo), "status", "--porcelain").Output()
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(out)) != "" {
			dirty = append(dirty, repo)
		}
	}
	sort.Strings(dirty)
	return dirty
}

func gitRepositories(root string) ([]string, error) {
	out, err := exec.Command("find", root, "-maxdepth", "4", "-name", ".git", "-not", "-path", "*/node_modules/*").Output()
	if err != nil {
		return nil, err
	}
	repos := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		relative, err := filepath.Rel(root, filepath.Dir(line))
		if err != nil || relative == "." {
			continue
		}
		repos = append(repos, relative)
	}
	sort.Strings(repos)
	return repos, nil
}

// index rebuilds both graphs against the prepared state. It is charged to the
// setup of a task, not to an arm: every arm queries the same generation.
type indexer struct {
	Kivgraph     string
	Graft        string
	Home         string
	GraftContext string
	Root         string
}

func (i indexer) reindex() (kivgraphMS, graftMS float64, err error) {
	started := time.Now()
	// 0.2.1 has no delta mode on the CLI: a task state costs a full index.
	command := exec.Command(i.Kivgraph, "index", "--full", "--json")
	command.Dir = i.Root
	command.Env = append(os.Environ(), "HOME="+i.Home)
	if out, runErr := command.CombinedOutput(); runErr != nil {
		return 0, 0, fmt.Errorf("kivgraph index: %w (%s)", runErr, lastLines(string(out), 2))
	}
	kivgraphMS = float64(time.Since(started).Milliseconds())

	started = time.Now()
	build := exec.Command(i.Graft, "--dir", i.GraftContext, "build", i.Root)
	if out, runErr := build.CombinedOutput(); runErr != nil {
		return 0, 0, fmt.Errorf("graft build: %w (%s)", runErr, lastLines(string(out), 2))
	}
	graftMS = float64(time.Since(started).Milliseconds())
	return kivgraphMS, graftMS, nil
}

// register writes the isolated kivgraph configuration for the copy: the same 37
// repositories, at the copy's paths, with Go tests included so both surfaces see
// the same files.
func (i indexer) register() error {
	if err := os.RemoveAll(i.Home); err != nil {
		return fmt.Errorf("clear %s: %w", i.Home, err)
	}
	repos, err := gitRepositories(i.Root)
	if err != nil {
		return fmt.Errorf("discover repositories: %w", err)
	}
	arguments := []string{"init", "--languages", "go,typescript,rust"}
	for _, repo := range repos {
		arguments = append(arguments, "--repository", filepath.Base(repo)+"="+filepath.Join(i.Root, repo))
	}
	command := exec.Command(i.Kivgraph, arguments...)
	command.Env = append(os.Environ(), "HOME="+i.Home)
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("kivgraph init: %w (%s)", err, lastLines(string(out), 3))
	}
	config := filepath.Join(i.Home, ".config", "kivgraph", "config.yaml")
	raw, err := os.ReadFile(config)
	if err != nil {
		return fmt.Errorf("read %s: %w", config, err)
	}
	// Go tests are indexed, so both surfaces see the same files: graft always
	// parses them. This needed a fix in the loader -- a field of a nested
	// anonymous struct inside a function took an unrooted owner path, so every
	// file in a package that unmarshalled into the same shape declared one
	// `Errors.Message` and the fact set failed the DEFINES multiplicity at publish
	// time. The sweep recorded in report.md predates that fix and ran with Go
	// tests excluded on the kivgraph side only.
	patched := strings.Replace(string(raw), "include_tests: false", "include_tests: true", 1)
	// The generated configuration locates state through `~`, which resolves
	// against whatever HOME the reader has. The arms cannot set HOME: an `env`
	// block in an MCP config replaces the environment instead of extending it,
	// and a server spawned that way never finishes its handshake -- the host
	// reports only a tool-less server stuck in `pending`. So the paths are made
	// absolute here and `serve --config` reads them with the environment intact.
	patched = strings.ReplaceAll(patched, "~/", i.Home+"/")
	if err := os.WriteFile(config, []byte(patched), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", config, err)
	}
	return nil
}

// configPath is the isolated configuration both the indexer and the arm's server
// read, so they agree on where the generation lives.
func (i indexer) configPath() string {
	return filepath.Join(i.Home, ".config", "kivgraph", "config.yaml")
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}
