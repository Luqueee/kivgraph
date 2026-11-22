// Package javaloader turns the Java code of a registered repository into
// semantic facts, through the SCIP index scip-java emits.
//
// scip-java runs the project's own build -- Maven, Gradle, sbt or mill -- with
// the SemanticDB javac plugin attached, so its targets are resolved by the
// compiler that would compile the code. That is what makes the edges
// EXACT_TYPECHECKED rather than candidates, and it is also the cost: indexing
// a Java repository means building it.
//
// And a build writes into the directory it builds. `--targetroot` moves the
// SemanticDB output out, but Maven's `target/` and Gradle's `build/` belong to
// the build tool, so the repository is never the directory handed to it: the
// loader materialises the working tree elsewhere with internal/scratchtree and
// throws it away afterwards. AGENTS.md states the rule without an exception
// and this is what keeps it true.
package javaloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/proctree"
	"github.com/Luqueee/kivgraph/internal/scip"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/scratchtree"
	"github.com/Luqueee/kivgraph/internal/testpath"
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

	// The configuration is checked before the machine is. A target directory
	// pointing inside the repository is wrong on every machine, and reporting
	// it as "scip-java is not installed" sends the reader to install a tool
	// that would not have helped.
	output, targetRoot, err := outputPaths(options, root)
	if err != nil {
		return facts.SemanticPayload{}, err
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

	// The build gets a tree of its own. Everything it writes -- `target/`,
	// `.gradle/`, a lockfile it decides to refresh -- dies with it.
	tree, err := scratchtree.Materialise(ctx, options.Repository, filepath.Join(
		filepath.Dir(filepath.Dir(output)), "trees"))
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("java indexer: %w", err)
	}
	defer func() { _ = tree.Close() }()

	limit := options.MaximumIndexTime
	if limit <= 0 {
		limit = DefaultMaximumIndexTime
	}
	runContext, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	excluded, err := workspace.ExclusionMatcher(root, options.Repository.Exclusions)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("java indexer: %w", err)
	}
	// One index for the repository, from every build it holds. A reference
	// from one build to a declaration of another is the same symbol string in
	// both, so it resolves once the documents are side by side.
	var index scipwire.Index
	builds, err := buildRoots(tree.Path, excluded)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("java indexer: discover builds: %w", err)
	}
	for number, build := range builds {
		buildOutput, buildTarget := output, targetRoot
		if number > 0 {
			// The indexer reads the format from the extension.
			buildOutput = fmt.Sprintf("%s.%d.scip", strings.TrimSuffix(output, ".scip"), number)
			buildTarget = fmt.Sprintf("%s.%d", targetRoot, number)
		}
		arguments := append([]string{}, fields[1:]...)
		arguments = append(arguments, "index", "--output", buildOutput, "--targetroot", buildTarget)
		if tool := strings.TrimSpace(options.BuildTool); tool != "" {
			arguments = append(arguments, "--build-tool", tool)
		}
		process := exec.CommandContext(runContext, executable, arguments...)
		process.Dir = filepath.Join(tree.Path, filepath.FromSlash(build))
		combined, runErr := proctree.CombinedOutput(process)
		if runErr != nil {
			if runContext.Err() != nil && ctx.Err() == nil {
				return facts.SemanticPayload{}, fmt.Errorf(
					"java indexer did not finish within %s for %q", limit, options.Repository.Name)
			}
			return facts.SemanticPayload{}, fmt.Errorf("java indexer failed for %q%s: %w: %s",
				options.Repository.Name, buildLabel(build), runErr, lastLines(string(combined), 12))
		}
		data, err := os.ReadFile(buildOutput)
		if err != nil {
			return facts.SemanticPayload{}, fmt.Errorf("read java index%s: %w", buildLabel(build), err)
		}
		decoded, err := scipwire.Decode(data)
		if err != nil {
			return facts.SemanticPayload{}, fmt.Errorf("decode java index%s: %w", buildLabel(build), err)
		}
		index = mergeBuild(index, decoded, build)
	}
	// Two roots, and conflating them is a defect that would not look like one:
	// the tree is where the files are read from, the repository is what the
	// facts are about. The package name reaches every stable key, and the tree
	// is a fresh temporary directory per pass -- deriving it from there would
	// give the same code a different identity on every run.
	return convert(index, options, tree.Path, root)
}

// buildMarkers are the files that make a directory the root of a build.
var buildMarkers = []string{
	"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle",
	"settings.gradle.kts", "build.sbt",
}

func holdsBuild(directory string) bool {
	for _, marker := range buildMarkers {
		if info, err := os.Stat(filepath.Join(directory, marker)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// buildRoots lists, relative to root and sorted, the directories the indexer
// is run in.
//
// It was run at the root and nowhere else, so a repository that keeps several
// builds side by side -- `backend/` and `tools/`, each with its own
// `settings.gradle` -- had no build where the indexer looked and contributed
// nothing. A build at the root is still the only one: it is the one that says
// which of the directories below are its modules. Without one, the outermost
// build of each branch is taken, and what is below it is that build's.
func buildRoots(root string, excluded func(string) bool) ([]string, error) {
	if holdsBuild(root) {
		return []string{"."}, nil
	}
	builds := make([]string, 0)
	// A subtree that cannot be read is an error, not a missing build: a silent
	// skip would drop its build from the index and still report a pass.
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !entry.IsDir() || path == root {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return filepath.SkipDir
		}
		relative = filepath.ToSlash(relative)
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || excluded(relative) || workspace.IsSkippedDirectory(path) {
			return filepath.SkipDir
		}
		if holdsBuild(path) {
			builds = append(builds, relative)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(builds) == 0 {
		// Nothing to choose between: the indexer is asked at the root, and
		// says what it found there.
		return []string{"."}, nil
	}
	sort.Strings(builds)
	return builds, nil
}

func buildLabel(build string) string {
	if build == "." {
		return ""
	}
	return " (build " + build + ")"
}

// mergeBuild adds the documents of one build to the repository's index, with
// their paths made relative to the repository.
func mergeBuild(index, build scipwire.Index, directory string) scipwire.Index {
	if index.ToolName == "" {
		index.ToolName, index.ToolVersion = build.ToolName, build.ToolVersion
		index.ProjectRoot, index.TextEncoding = build.ProjectRoot, build.TextEncoding
	}
	for _, document := range build.Documents {
		if directory != "." {
			document.RelativePath = directory + "/" + strings.TrimPrefix(filepath.ToSlash(document.RelativePath), "./")
		}
		index.Documents = append(index.Documents, document)
	}
	index.ExternalSymbols = append(index.ExternalSymbols, build.ExternalSymbols...)
	return index
}

// Convert turns a decoded index into a payload, reading the sources from root.
// It is separate from Run so a test can drive it from a recorded index without
// a JDK.
func Convert(index scipwire.Index, options Options, root string) (facts.SemanticPayload, error) {
	return convert(index, options, root, root)
}

// convert reads from sources and takes the payload's identity from repository.
// They are the same directory only when no scratch tree is involved.
func convert(index scipwire.Index, options Options, sources, repository string) (facts.SemanticPayload, error) {
	name, manifest := packageIdentity(repository)
	excluded, err := workspace.ExclusionMatcher(repository, options.Repository.Exclusions)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("java indexer: %w", err)
	}
	return scip.Convert(index, scip.Options{
		Language:        facts.LanguageJava,
		Repository:      options.Repository.Name,
		Package:         name,
		PackageRoot:     repository,
		ManifestPath:    manifest,
		Analyzer:        DefaultCommand,
		AnalyzerVersion: index.ToolVersion,
		// scip-java's index is produced by javac itself through the
		// SemanticDB plugin, so every target it names was resolved by a type
		// checker.
		Authoritative: true,
		ReadFile: func(relative string) ([]byte, error) {
			return os.ReadFile(filepath.Join(sources, filepath.FromSlash(relative)))
		},
		IncludeFile: func(relative string) bool {
			return includeFile(relative, options) && !excluded(relative)
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
			"java target directory %q is inside the indexed repository %q", absolute, root,
		)
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
	for _, candidate := range buildMarkers {
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
	if !options.IncludeTests && testpath.Is(slashed) {
		return false
	}
	return true
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
