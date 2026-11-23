// Command prepare builds the frozen corpus a mcp-token-cost run is measured
// against: a third-party repository at a pinned commit, indexed by the server
// under test in a scratch home.
//
// A corpus that moves invalidates the native captures committed beside it, so
// the benchmark's corpora are not the working tree of anything. Each one is a
// directory with a corpus.json naming the repository, the tag and the full
// commit it must be at, the languages to register, and the commands that make
// its dependencies available to an analyzer that works offline.
//
// It never touches the user's own configuration: HOME is a scratch directory,
// and only the caches a toolchain needs are pointed back at the real ones, so a
// pass does not download the world again.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type spec struct {
	Name       string   `json:"name"`
	Repository string   `json:"repository"`
	URL        string   `json:"url"`
	Tag        string   `json:"tag"`
	SHA        string   `json:"sha"`
	Languages  []string `json:"languages"`
	BuildTags  []string `json:"build_tags,omitempty"`
	Exclusions []string `json:"exclusions,omitempty"`
	// Prepare runs in the checkout, with a shell, once, after it is at its
	// commit. It makes dependencies readable offline and must leave the tree
	// clean: a corpus that is dirty is not the corpus that was captured.
	Prepare []string `json:"prepare,omitempty"`
}

func main() {
	corpus := flag.String("corpus", "", "directory of the corpus, holding corpus.json")
	server := flag.String("server", "kivgraph", "kivgraph executable to index with and measure (built with the ladybug tag)")
	state := flag.String("state", "/tmp/kivgraph-corpora", "where checkouts and scratch homes live; the default is fixed because a native capture carries the path it searched")
	then := flag.String("then", "none", "what to do once the corpus is indexed: none, measure or check")
	flag.Parse()
	if err := run(*corpus, *server, *state, *then); err != nil {
		fmt.Fprintln(os.Stderr, "prepare:", err)
		os.Exit(1)
	}
}

func run(corpusDirectory, server, state, then string) error {
	if corpusDirectory == "" {
		return fmt.Errorf("--corpus is required")
	}
	if then != "none" && then != "measure" && then != "check" {
		return fmt.Errorf("--then is %q, want none, measure or check", then)
	}
	definition, err := loadSpec(corpusDirectory)
	if err != nil {
		return err
	}
	executable, err := exec.LookPath(server)
	if err != nil {
		return fmt.Errorf("find the server %q: %w", server, err)
	}
	if executable, err = filepath.Abs(executable); err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	work := filepath.Join(state, definition.Name)
	checkout := filepath.Join(work, "src")
	home := filepath.Join(work, "home")
	environment := scratchEnvironment(home, root)

	if err := ensureCheckout(checkout, definition, environment); err != nil {
		return err
	}
	for _, command := range definition.Prepare {
		fmt.Printf("prepare: %s\n", command)
		if err := execute(checkout, environment, "sh", "-c", command); err != nil {
			return fmt.Errorf("prepare %q: %w", command, err)
		}
	}
	if err := requireClean(checkout, environment); err != nil {
		return fmt.Errorf("after the prepare commands: %w", err)
	}
	if err := indexCorpus(executable, work, checkout, home, definition, environment); err != nil {
		return err
	}

	command := []string{
		"go", "run", "./benchmarks/mcp-token-cost", "--server", executable,
		"--dir", corpusDirectory, "--repository", definition.Repository,
	}
	if then == "check" {
		command = append(command, "--check")
	}
	if then == "none" {
		fmt.Printf("indexed. Measure with (from %s):\n  HOME=%s %s\n", root, home, strings.Join(command, " "))
		return nil
	}
	return execute(root, environment, command[0], command[1:]...)
}

func loadSpec(directory string) (spec, error) {
	path := filepath.Join(directory, "corpus.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return spec{}, fmt.Errorf("read %s: %w", path, err)
	}
	definition := spec{}
	if err := json.Unmarshal(data, &definition); err != nil {
		return spec{}, fmt.Errorf("parse %s: %w", path, err)
	}
	switch {
	case definition.Name == "" || definition.Repository == "" || definition.URL == "":
		return spec{}, fmt.Errorf("%s needs a name, a repository and a url", path)
	case len(definition.SHA) != 40:
		return spec{}, fmt.Errorf("%s: sha %q is not a full commit; a tag can move, a commit cannot", path, definition.SHA)
	case len(definition.Languages) == 0:
		return spec{}, fmt.Errorf("%s names no language", path)
	}
	return definition, nil
}

// scratchEnvironment is the environment every command runs in: a home that is
// not the user's, with the toolchain caches that live under the real one
// pointed back at it.
func scratchEnvironment(home, root string) []string {
	environment := append(os.Environ(), "HOME="+home, "KIVGRAPH_TELEMETRY=0")
	realHome, _ := os.UserHomeDir()
	for _, cache := range []struct{ variable, relative string }{
		{"CARGO_HOME", ".cargo"},
		{"RUSTUP_HOME", ".rustup"},
		{"npm_config_cache", ".npm"},
	} {
		if os.Getenv(cache.variable) != "" || realHome == "" {
			continue
		}
		if path := filepath.Join(realHome, cache.relative); isDirectory(path) {
			environment = append(environment, cache.variable+"="+path)
		}
	}
	// A module cache under the scratch home would be empty, and an analyzer that
	// works offline would find no dependency.
	if output, err := exec.Command("go", "env", "GOMODCACHE", "GOCACHE").Output(); err == nil {
		lines := strings.Fields(string(output))
		if len(lines) == 2 {
			environment = append(environment, "GOMODCACHE="+lines[0], "GOCACHE="+lines[1])
		}
	}
	if analyzer := pinnedAnalyzer(root); analyzer != "" {
		environment = append(environment, "PATH="+analyzer+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return environment
}

// pinnedAnalyzer is the directory of the rust-analyzer this repository pins, if
// it was fetched, so a Rust corpus does not depend on the user's PATH.
func pinnedAnalyzer(root string) string {
	matches, _ := filepath.Glob(filepath.Join(root, ".tooling", "rust-analyzer", "*", runtime.GOOS+"-"+runtime.GOARCH))
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ensureCheckout leaves checkout at exactly definition.SHA. An existing
// checkout is verified and never moved: a directory somebody edited is not
// silently reset, because that would hide that the captures describe a
// different tree.
func ensureCheckout(checkout string, definition spec, environment []string) error {
	if !isDirectory(filepath.Join(checkout, ".git")) {
		if err := os.MkdirAll(checkout, 0o755); err != nil {
			return err
		}
		for _, arguments := range [][]string{
			{"init", "-q"},
			{"remote", "add", "origin", definition.URL},
			{"fetch", "-q", "--depth", "1", "origin", definition.SHA},
			{"checkout", "-q", "--detach", "FETCH_HEAD"},
		} {
			if err := execute(checkout, environment, "git", arguments...); err != nil {
				return fmt.Errorf("git %s: %w", arguments[0], err)
			}
		}
	}
	head, err := output(checkout, environment, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != definition.SHA {
		return fmt.Errorf("%s is at %s and the corpus is pinned to %s (%s); remove it to fetch again", checkout, head, definition.SHA, definition.Tag)
	}
	return requireClean(checkout, environment)
}

func requireClean(checkout string, environment []string) error {
	status, err := output(checkout, environment, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("%s is not clean:\n%s", checkout, status)
	}
	return nil
}

// indexCorpus registers the checkout in a fresh scratch home and publishes a
// generation of it.
func indexCorpus(server, work, checkout, home string, definition spec, environment []string) error {
	if err := os.RemoveAll(home); err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	if err := execute(work, environment, server, "init",
		"--repository", definition.Repository+"="+checkout,
		"--languages", strings.Join(definition.Languages, ",")); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	configuration := filepath.Join(home, ".config", "kivgraph")
	if len(definition.BuildTags) > 0 {
		if err := replaceOnce(filepath.Join(configuration, "config.yaml"),
			"build_tags: []", "build_tags: ["+strings.Join(definition.BuildTags, ", ")+"]"); err != nil {
			return err
		}
	}
	if len(definition.Exclusions) > 0 {
		// The registry holds exactly one repository, so its entry is the last
		// thing in the file and a key at the end belongs to it.
		registry := filepath.Join(configuration, "repositories.yaml")
		file, err := os.OpenFile(registry, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintf(file, "      exclusions: [%s]\n", strings.Join(definition.Exclusions, ", "))
		if closeErr := file.Close(); writeErr != nil || closeErr != nil {
			return fmt.Errorf("write %s: %w", registry, firstError(writeErr, closeErr))
		}
	}
	if err := execute(work, environment, server, "index", "--full"); err != nil {
		return fmt.Errorf("index --full: %w", err)
	}
	return nil
}

func replaceOnce(path, from, to string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), from) {
		return fmt.Errorf("%s has no %q to set", path, from)
	}
	return os.WriteFile(path, []byte(strings.Replace(string(data), from, to, 1)), 0o644)
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// execute runs a command with its output on ours, so a long index shows it is
// working and a failure shows why.
func execute(directory string, environment []string, name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func output(directory string, environment []string, name string, arguments ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
