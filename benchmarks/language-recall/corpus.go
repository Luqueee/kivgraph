package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// corpusRepository is a public repository pinned to a commit. The corpus is
// public on purpose: a measurement nobody else can repeat is a claim.
type corpusRepository struct {
	Language string `json:"language"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Commit   string `json:"commit"`
	// Project is what the oracle type-checks: the repository's own tsconfig for
	// TypeScript, empty for Go (every package, tests included). Kivgraph reads
	// the same project, so the two are asked about the same files.
	Project string `json:"project,omitempty"`
	// Setup installs dependencies. The type checkers need them to resolve
	// imports, and so does Kivgraph.
	Setup []string `json:"-"`

	// Extra marks a repository measured only when named with -repos. The default
	// corpus is the one the published results cover; these answer one question
	// at a time and cost far more to type-check.
	Extra bool `json:"-"`

	Path string `json:"-"`
}

// javaClasspathFile is where a Java repository's setup leaves its classpath,
// under the build directory the repository already ignores.
const javaClasspathFile = "target/kivgraph.classpath"

func corpus() []corpusRepository {
	return []corpusRepository{
		{
			Language: "go", Name: "cobra", URL: "https://github.com/spf13/cobra.git",
			Commit: "adbc8813901bba65827259daa8e22ff94ec1f30e",
			Setup:  []string{"go mod download"},
		},
		{
			Language: "typescript", Name: "ky", URL: "https://github.com/sindresorhus/ky.git",
			Commit:  "3541888878c4d303a8bc2ad61815f4e048f55a5d",
			Project: "tsconfig.json",
			Setup:   []string{"npm install --no-audit --no-fund --ignore-scripts --fetch-retries=5 --fetch-timeout=120000"},
		},
		{
			Language: "rust", Name: "fd", URL: "https://github.com/sharkdp/fd.git",
			Commit: "b80cd53e65615060a62a5ea349a0d78b65d7029e",
			Setup:  []string{"cargo fetch --locked"},
		},
		{
			Language: "dart", Name: "collection", URL: "https://github.com/dart-lang/collection.git",
			Commit: "96afcc2fce622ea7e75053af5b73f8db07491bc4",
			Setup:  []string{"dart pub get"},
		},
		// Java and C# are measured only when asked for with -languages: their
		// indexers are not part of a default installation.
		{
			Language: "java", Name: "jsoup", URL: "https://github.com/jhy/jsoup.git",
			Commit: "d90d57521d931db7bf7ee464a4c0a1a9293db088",
			// The oracle runs javac itself, over the main and the test sources at
			// once, so it needs the classpath Maven resolves.
			Setup: []string{"mvn -q dependency:build-classpath -Dmdep.outputFile=" + javaClasspathFile + " -Dmdep.includeScope=test"},
		},
		{
			Language: "csharp", Name: "GuardClauses", URL: "https://github.com/ardalis/GuardClauses.git",
			Commit: "ad43aa02babf3bc8aee8efc2258f5ad3571c8ec5",
			Setup:  []string{"dotnet restore"},
		},
		{
			Language: "typescript", Name: "got", URL: "https://github.com/sindresorhus/got.git",
			Commit: "6b5aafc1cd538998728be6f45c65b0f80bae01b2", Extra: true,
			Project: "tsconfig.json",
			Setup:   []string{"npm install --no-audit --no-fund --ignore-scripts --fetch-retries=5 --fetch-timeout=120000"},
		},
		{
			Language: "rust", Name: "ripgrep", URL: "https://github.com/BurntSushi/ripgrep.git",
			Commit: "fdf7d95485ec69c599418b7e7154baa4518fed2c", Extra: true,
			Setup: []string{"cargo fetch --locked"},
		},
		{
			Language: "dart", Name: "args", URL: "https://github.com/dart-lang/args.git",
			Commit: "7a2dfb528768f0a2e5680f711dbfb948f1d6c255", Extra: true,
			Setup: []string{"dart pub get"},
		},
		{
			Language: "go", Name: "gin", URL: "https://github.com/gin-gonic/gin.git",
			Commit: "0f09c3a9b4626d9fe9979cf4d0521d6ae32ab646", Extra: true,
			Setup: []string{"go mod download"},
		},
		{
			Language: "go", Name: "caddy", URL: "https://github.com/caddyserver/caddy.git",
			Commit: "16618f454ca6f78f37dfd85a90573d8ffd43c1c8", Extra: true,
			Setup: []string{"go mod download"},
		},
		{
			Language: "go", Name: "prometheus", URL: "https://github.com/prometheus/prometheus.git",
			Commit: "cb4e01dd1cb1673d4bcde6f3a16fb767b7f8eb88", Extra: true,
			Setup: []string{"go mod download"},
		},
	}
}

func describe(repositories []corpusRepository) []corpusRepository {
	return append([]corpusRepository(nil), repositories...)
}

// prepare clones the repository at its commit, or reuses a clone already there,
// and runs its setup. A clone that moved off the commit is an error: a result
// must name the source it was measured on.
func prepare(ctx context.Context, work string, repository *corpusRepository) error {
	repository.Path = filepath.Join(work, repository.Name)
	if _, err := os.Stat(filepath.Join(repository.Path, ".git")); err != nil {
		if err := os.MkdirAll(work, 0o755); err != nil {
			return err
		}
		if output, err := exec.CommandContext(ctx, "git", "clone", "-q", repository.URL, repository.Path).CombinedOutput(); err != nil {
			return fmt.Errorf("clone: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if output, err := exec.CommandContext(ctx, "git", "-C", repository.Path, "checkout", "-q", repository.Commit).CombinedOutput(); err != nil {
			return fmt.Errorf("checkout %s: %w: %s", repository.Commit, err, strings.TrimSpace(string(output)))
		}
	}
	head, err := exec.CommandContext(ctx, "git", "-C", repository.Path, "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(head)); got != repository.Commit {
		return fmt.Errorf("the clone is at %s, want %s", got, repository.Commit)
	}
	if status, err := exec.CommandContext(ctx, "git", "-C", repository.Path, "status", "--porcelain", "--untracked-files=no").Output(); err != nil || len(strings.TrimSpace(string(status))) != 0 {
		return fmt.Errorf("the clone has uncommitted changes: a previous run did not restore it")
	}
	for _, line := range repository.Setup {
		fields := strings.Fields(line)
		process := exec.CommandContext(ctx, fields[0], fields[1:]...)
		process.Dir = repository.Path
		process.Env = toolEnvironment()
		if output, err := process.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", line, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

// toolEnvironment keeps the toolchains this machine has, and stops pnpm from
// fetching the version a repository's `packageManager` field names.
func toolEnvironment() []string {
	return append(
		os.Environ(),
		"npm_config_pm_on_fail=ignore", "pnpm_config_pm_on_fail=ignore", "COREPACK_ENABLE_STRICT=0",
		"KIVGRAPH_TELEMETRY=0",
	)
}

func toolVersion(ctx context.Context, name string, arguments ...string) string {
	output, err := exec.CommandContext(ctx, name, arguments...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func kivgraphVersion(ctx context.Context, binary string) string {
	return toolVersion(ctx, binary, "version")
}

// typescriptVersion is the compiler the repository itself resolves, which is
// the one the oracle runs.
func typescriptVersion(ctx context.Context, work string, repositories []corpusRepository) string {
	for _, repository := range repositories {
		if repository.Language != "typescript" {
			continue
		}
		process := exec.CommandContext(ctx, "npx", "--no-install", "tsc", "--version")
		process.Dir = repository.Path
		process.Env = toolEnvironment()
		if output, err := process.Output(); err == nil {
			return strings.TrimSpace(string(output))
		}
	}
	return ""
}
