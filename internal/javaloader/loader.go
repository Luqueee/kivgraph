// Package javaloader turns the Java code of a registered repository into
// semantic facts, through the SCIP index scip-java emits.
//
// scip-java runs the project's own build -- Maven, Gradle, sbt or mill -- with
// the SemanticDB javac plugin attached, so its targets are resolved by the
// compiler that would compile the code. That is what makes the edges
// EXACT_TYPECHECKED rather than candidates, and it is also the cost: indexing
// a Java repository means building it.
package javaloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// DefaultCommand is the indexer this loader drives.
const DefaultCommand = "scip-java"

// Options configures one Java facts producer.
type Options struct {
	// Command is the scip-java executable, with arguments.
	Command string
	// BuildTool names the build system explicitly. Empty lets scip-java
	// detect it, which is what it does well; naming it is for a repository
	// that carries two.
	BuildTool string
	// TargetDirectory is where the indexer writes. It is outside every
	// indexed repository on purpose -- see JavaTargetDirectory in the
	// indexer options.
	TargetDirectory string
	Repository      workspace.Repository
	IncludeTests    bool
	// IncludeGenerated keeps the files the build produced. They are excluded
	// by default: a graph whose symbols are mostly generated accessors
	// answers questions about code nobody wrote.
	IncludeGenerated bool
	// MaximumIndexTime bounds one repository's build. Zero is
	// DefaultMaximumIndexTime.
	MaximumIndexTime time.Duration
}

// DefaultMaximumIndexTime bounds a build that will not finish. It is generous
// because a cold Maven build resolves its dependencies over the network.
const DefaultMaximumIndexTime = 20 * time.Minute

// Run indexes one repository and returns its semantic payload.
func Run(ctx context.Context, options Options) (facts.SemanticPayload, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root := options.Repository.RealPath
	if root == "" {
		root = options.Repository.Path
	}
	if strings.TrimSpace(root) == "" {
		return facts.SemanticPayload{}, errors.New("java indexer: repository has no path")
	}

	command := strings.TrimSpace(options.Command)
	if command == "" {
		command = DefaultCommand
	}
	fields := strings.Fields(command)
	executable, err := exec.LookPath(fields[0])
	if err != nil {
		// exec.ErrNotFound is what the pass reads to isolate the repository
		// instead of failing every other one, so it must survive wrapping.
		return facts.SemanticPayload{}, fmt.Errorf("java indexer %q is unavailable: %w", fields[0], exec.ErrNotFound)
	}

	output, targetRoot, err := outputPaths(options, root)
	if err != nil {
		return facts.SemanticPayload{}, err
	}

	arguments := append([]string{}, fields[1:]...)
	arguments = append(arguments, "index", "--output", output, "--targetroot", targetRoot)
	if tool := strings.TrimSpace(options.BuildTool); tool != "" {
		arguments = append(arguments, "--build-tool", tool)
	}

	limit := options.MaximumIndexTime
	if limit <= 0 {
		limit = DefaultMaximumIndexTime
	}
	runContext, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	process := exec.CommandContext(runContext, executable, arguments...)
	process.Dir = root
	combined, runErr := process.CombinedOutput()
	if runErr != nil {
		if runContext.Err() != nil && ctx.Err() == nil {
			return facts.SemanticPayload{}, fmt.Errorf(
				"java indexer did not finish within %s for %q", limit, options.Repository.Name)
		}
		return facts.SemanticPayload{}, fmt.Errorf("java indexer failed for %q: %w: %s",
			options.Repository.Name, runErr, lastLines(string(combined), 12))
	}

	data, err := os.ReadFile(output)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("read java index: %w", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("decode java index: %w", err)
	}
	return Convert(index, options, root)
}

// Convert turns a decoded index into a payload. It is separate from Run so a
// test can drive it from a recorded index without a JDK.
func Convert(index scipwire.Index, options Options, root string) (facts.SemanticPayload, error) {
	name, manifest := packageIdentity(root)
	return scip.Convert(index, scip.Options{
		Language:        facts.LanguageJava,
		Repository:      options.Repository.Name,
		Package:         name,
		PackageRoot:     root,
		ManifestPath:    manifest,
		Analyzer:        DefaultCommand,
		AnalyzerVersion: index.ToolVersion,
		// scip-java's index is produced by javac itself through the
		// SemanticDB plugin, so every target it names was resolved by a type
		// checker.
		Authoritative: true,
		ReadFile: func(relative string) ([]byte, error) {
			return os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		},
		IncludeFile: func(relative string) bool {
			return includeFile(relative, options)
		},
		Generated: isGenerated,
	})
}

// outputPaths decides where the indexer writes, and refuses to write inside
// the repository it is reading.
func outputPaths(options Options, root string) (string, string, error) {
	base := strings.TrimSpace(options.TargetDirectory)
	if base == "" {
		return "", "", errors.New("java indexer: a target directory outside the repository is required")
	}
	absolute, err := filepath.Abs(base)
	if err != nil {
		return "", "", fmt.Errorf("resolve java target directory: %w", err)
	}
	// A target directory inside the repository would make the pass modify
	// what it came to read, and the next pass would then index its own
	// output. The Rust loader refuses the same shape for the same reason.
	if within(absolute, root) {
		return "", "", fmt.Errorf(
			"java target directory %q is inside the indexed repository %q", absolute, root)
	}
	directory := filepath.Join(absolute, sanitise(options.Repository.Name))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", fmt.Errorf("create java target directory: %w", err)
	}
	return filepath.Join(directory, "index.scip"), filepath.Join(directory, "targetroot"), nil
}

func within(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sanitise(name string) string {
	replaced := strings.Map(func(character rune) rune {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '-', character == '_', character == '.':
			return character
		default:
			return '-'
		}
	}, name)
	if replaced == "" {
		return "repository"
	}
	return replaced
}

// packageIdentity names the unit the symbols belong to, from the manifest the
// repository carries. It is deliberately shallow: the package name reaches the
// stable key of every symbol, so deriving it from a manifest that is present
// beats deriving it from the index, whose package field a multi-module build
// varies per module.
func packageIdentity(root string) (string, string) {
	for _, candidate := range []string{
		"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle",
		"settings.gradle.kts", "build.sbt",
	} {
		path := filepath.Join(root, candidate)
		if _, err := os.Stat(path); err == nil {
			return filepath.Base(root), path
		}
	}
	return filepath.Base(root), ""
}

// includeFile decides what enters the graph.
func includeFile(relative string, options Options) bool {
	if !strings.HasSuffix(relative, ".java") {
		return false
	}
	slashed := filepath.ToSlash(relative)
	if !options.IncludeGenerated && isGenerated(slashed) {
		return false
	}
	if !options.IncludeTests && isTest(slashed) {
		return false
	}
	return true
}

// isTest is the layout Maven, Gradle and sbt all share. It is a path rule and
// not a guess about content: `src/test/java` is where the three of them put
// tests, and a file outside it is not treated as one.
func isTest(path string) bool {
	return strings.Contains(path, "src/test/") || strings.Contains(path, "src/it/")
}

// isGenerated names the directories a build writes into. A file under one of
// them was produced by the build, not written by anyone.
func isGenerated(path string) bool {
	slashed := filepath.ToSlash(path)
	for _, marker := range []string{
		"target/generated-sources/", "target/generated-test-sources/",
		"build/generated/", "build/generated-src/",
	} {
		if strings.Contains(slashed, marker) {
			return true
		}
	}
	return false
}

func lastLines(value string, count int) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
