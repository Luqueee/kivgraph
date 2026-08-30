package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/version"
)

// indexProjectOptions carries the flags of the convenient `kivgraph index`
// form. The explicit `index --full` form remains separate because it indexes
// the repositories already registered in the selected configuration.
type indexProjectOptions struct {
	ConfigPath       string
	RepositoriesPath string
	ResolverVersion  string
	JSONOutput       bool
}

// indexProjectFlagSet declares the flags for the current-directory form. It
// deliberately mirrors the useful flags of `index --full`, while choosing a
// project-local configuration when neither path is supplied.
func indexProjectFlagSet(options *indexProjectOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("index", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.StringVar(&options.RepositoriesPath, "repositories", "", "repository registry file override")
	flags.StringVar(&options.ResolverVersion, "resolver-version", version.Value, "resolver version recorded in the graph")
	flags.BoolVar(&options.JSONOutput, "json", false, "write the pass as a JSON event stream on stdout")
	return flags
}

// runIndexProject detects and indexes the repository containing the current
// working directory. Its default state is `.kivgraph` in that repository,
// matching the one-command local workflow while keeping the shared user
// registry untouched.
func runIndexProject(args []string, stdout, stderr io.Writer) int {
	var options indexProjectOptions
	flags := indexProjectFlagSet(&options)
	if parsed, code := parseCommandFlags("index", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "index: unexpected arguments: %v", flags.Args())
		return 2
	}

	projectRoot, err := currentProjectRoot()
	if err != nil {
		writeCommandError(stderr, "index: %v", err)
		return 1
	}
	languages, err := config.DetectLanguages(projectRoot)
	if err != nil {
		writeCommandError(stderr, "index: detect languages: %v", err)
		return 1
	}
	if len(languages) == 0 {
		writeCommandError(stderr, "index: no supported source language detected in %s", projectRoot)
		return 1
	}

	configPath, repositoriesPath := projectIndexPaths(projectRoot, options)
	if err := rejectProjectStateSymlink(projectRoot, options, configPath, repositoriesPath); err != nil {
		writeCommandError(stderr, "index: %v", err)
		return 1
	}
	initialised, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	})
	if err != nil {
		writeCommandError(stderr, "index: initialize project: %v", err)
		return 1
	}
	if _, err := config.MigrateProjectSyntheticWorkFile(initialised.ConfigPath); err != nil {
		writeCommandError(stderr, "index: migrate project configuration: %v", err)
		return 1
	}
	loaded, err := config.Load(initialised.ConfigPath)
	if err != nil {
		writeCommandError(stderr, "index: load project configuration: %v", err)
		return 1
	}
	repositoryName, changed, err := upsertCurrentProject(
		loaded.RepositoriesPath, projectRoot, languages,
	)
	if err != nil {
		writeCommandError(stderr, "index: register project: %v", err)
		return 1
	}

	messageWriter := stdout
	if options.JSONOutput {
		// `index --full --json` is an event-stream protocol. Detection and
		// registry messages must stay off stdout or they would corrupt it.
		messageWriter = stderr
	}
	writeInfo(messageWriter, "project: %s", projectRoot)
	writeInfo(messageWriter, "languages: %s", joinLanguages(languages))
	if initialised.ConfigCreated || initialised.RepositoriesCreated || changed {
		writeSuccess(messageWriter, "project registry: %s (%s)", repositoryName, loaded.RepositoriesPath)
	} else {
		writeInfo(messageWriter, "project registry: %s (existing)", repositoryName)
	}

	fullArgs := []string{
		"--config", loaded.ConfigPath,
		"--repositories", loaded.RepositoriesPath,
		"--resolver-version", options.ResolverVersion,
	}
	if options.JSONOutput {
		fullArgs = append(fullArgs, "--json")
	}
	return runIndexFull(fullArgs, stdout, stderr)
}

func currentProjectRoot() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	absolute, err := filepath.Abs(workingDirectory)
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve current directory %q: %w", absolute, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect current directory %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("current path %q is not a directory", resolved)
	}
	projectRoot, err := containingRepositoryRoot(filepath.Clean(resolved))
	if err != nil {
		return "", err
	}
	if filepath.Clean(projectRoot) == filepath.Clean(filepath.Dir(projectRoot)) {
		return "", errors.New("refusing to index the filesystem root")
	}
	return filepath.Clean(projectRoot), nil
}

// containingRepositoryRoot finds the nearest Git repository marker. A project
// without Git metadata keeps the current directory as its root, so the local
// command remains useful in unpacked source trees too.
func containingRepositoryRoot(start string) (string, error) {
	candidate := filepath.Clean(start)
	for {
		marker := filepath.Join(candidate, ".git")
		if _, err := os.Lstat(marker); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect repository marker %q: %w", marker, err)
		}

		parent := filepath.Dir(candidate)
		if parent == candidate {
			return start, nil
		}
		candidate = parent
	}
}

func projectIndexPaths(projectRoot string, options indexProjectOptions) (string, string) {
	configPath := options.ConfigPath
	if configPath == "" {
		configPath = filepath.Join(projectRoot, ".kivgraph", "config.yaml")
	}
	repositoriesPath := options.RepositoriesPath
	if repositoriesPath == "" {
		repositoriesPath = filepath.Join(filepath.Dir(configPath), "repositories.yaml")
	}
	return configPath, repositoriesPath
}

func rejectProjectStateSymlink(
	projectRoot string,
	options indexProjectOptions,
	configPath string,
	repositoriesPath string,
) error {
	if options.ConfigPath != "" || options.RepositoriesPath != "" {
		return nil
	}
	stateDirectory := filepath.Join(projectRoot, ".kivgraph")
	info, err := os.Lstat(stateDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect project state %q: %w", stateDirectory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symbolic-link project state %q", stateDirectory)
	}
	for _, path := range []string{configPath, repositoriesPath} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect project state file %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symbolic-link project state file %q", path)
		}
	}
	return nil
}

// upsertCurrentProject adds the current repository to a registry or updates
// its detected languages. It preserves any repository-specific manifests,
// roots and exclusions already recorded for the same path.
func upsertCurrentProject(path, projectRoot string, languages []string) (string, bool, error) {
	registry, err := config.LoadRepositories(path)
	if err != nil {
		return "", false, err
	}
	canonicalRoot := filepath.Clean(projectRoot)
	matchingPath := -1
	projectName := -1
	for index, repository := range registry.Repositories {
		if filepath.Clean(repository.Path) == canonicalRoot {
			matchingPath = index
		}
		if repository.Name == "project" {
			projectName = index
		}
	}

	if matchingPath >= 0 {
		repository := &registry.Repositories[matchingPath]
		changed := !slices.Equal(repository.Languages, languages)
		if changed {
			repository.Languages = slices.Clone(languages)
			if err := config.SaveRepositories(path, registry); err != nil {
				return "", false, err
			}
		}
		return repository.Name, changed, nil
	}
	if projectName >= 0 {
		return "", false, fmt.Errorf(
			"repository name %q is already registered for %s", "project", registry.Repositories[projectName].Path,
		)
	}

	registry.Repositories = append(registry.Repositories, config.Repository{
		Name:      "project",
		Path:      canonicalRoot,
		Languages: slices.Clone(languages),
	})
	if err := config.SaveRepositories(path, registry); err != nil {
		return "", false, err
	}
	return "project", true, nil
}

func joinLanguages(languages []string) string {
	if len(languages) == 0 {
		return "none"
	}
	joined := languages[0]
	for _, language := range languages[1:] {
		joined += "," + language
	}
	return joined
}
