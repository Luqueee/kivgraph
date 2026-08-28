// Package csharploader turns the C# code of a registered repository into
// semantic facts, through the SCIP index scip-dotnet emits.
//
// scip-dotnet drives Roslyn -- the compiler's own analysis -- over a solution
// or project, so its targets are resolved by the thing that would compile the
// code. It runs `dotnet restore` first, which is why indexing a C# repository
// needs the SDK and a network on the first pass.
//
// And a restore writes `obj/` and `bin/` into the project it restores, which
// AGENTS.md forbids without an exception. So the repository is never the
// directory handed to the indexer: the loader materialises the working tree
// elsewhere with internal/scratchtree and throws it away afterwards. See
// ADR 0082.
package csharploader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/scratchtree"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// DefaultCommand is the indexer this loader drives.
const DefaultCommand = "scip-dotnet"

// DefaultMaximumIndexTime bounds a restore and an index that will not finish.
const DefaultMaximumIndexTime = 20 * time.Minute

// Options configures one C# facts producer.
type Options struct {
	Command string
	// Project is the .sln or .csproj to index. Empty discovers one: a single
	// solution wins over projects, because that is what the compiler would
	// build.
	Project string
	// TargetDirectory is where the index file is written. It lives outside
	// every indexed repository, like java.target_directory.
	TargetDirectory  string
	Repository       workspace.Repository
	IncludeTests     bool
	IncludeGenerated bool
	MaximumIndexTime time.Duration
	// SkipRestore passes --skip-dotnet-restore. It is off by default: an
	// index built without a restore silently loses every symbol that comes
	// from a package, which looks like a repository with fewer dependencies
	// rather than an index that was not allowed to resolve them.
	SkipRestore bool
}

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
		return facts.SemanticPayload{}, errors.New("csharp indexer: repository has no path")
	}

	// The configuration is checked before the machine is. A target directory
	// pointing inside the repository is wrong on every machine, and reporting
	// it as "scip-dotnet is not installed" sends the reader to install a tool
	// that would not have helped.
	output, err := outputPath(options, root)
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
		return facts.SemanticPayload{}, fmt.Errorf(
			"csharp indexer %q is unavailable: %w", fields[0], exec.ErrNotFound)
	}

	// The restore and the build get a tree of their own; `obj/` and `bin/`
	// die with it.
	tree, err := scratchtree.Materialise(ctx, options.Repository,
		filepath.Join(filepath.Dir(filepath.Dir(output)), "trees"))
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("csharp indexer: %w", err)
	}
	defer func() { _ = tree.Close() }()

	// Discovery runs in the tree, so a project that exists only in the working
	// tree is found and one that exists only in a stale `obj/` is not.
	project, err := resolveProject(tree.Path, options.Project)
	if err != nil {
		return facts.SemanticPayload{}, err
	}

	arguments := append([]string{}, fields[1:]...)
	arguments = append(arguments, "index", project, "--output", output,
		"--working-directory", tree.Path)
	if options.SkipRestore {
		arguments = append(arguments, "--skip-dotnet-restore")
	}

	limit := options.MaximumIndexTime
	if limit <= 0 {
		limit = DefaultMaximumIndexTime
	}
	runContext, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	process := exec.CommandContext(runContext, executable, arguments...)
	process.Dir = tree.Path
	combined, runErr := process.CombinedOutput()
	if runErr != nil {
		if runContext.Err() != nil && ctx.Err() == nil {
			return facts.SemanticPayload{}, fmt.Errorf(
				"csharp indexer did not finish within %s for %q", limit, options.Repository.Name)
		}
		return facts.SemanticPayload{}, fmt.Errorf("csharp indexer failed for %q: %w: %s",
			options.Repository.Name, runErr, lastLines(string(combined), 12))
	}

	data, err := os.ReadFile(output)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("read csharp index: %w", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("decode csharp index: %w", err)
	}
	// Two roots that are not interchangeable: the tree is where files are
	// read, the repository is what the facts are about. The package name
	// reaches every stable key and the tree is a fresh directory per pass, so
	// deriving the identity from it would change every key on every run.
	return convert(index, options, tree.Path, root, project)
}

// Convert turns a decoded index into a payload, separately from Run so a test
// can drive it from a recorded index without the .NET SDK.
func Convert(index scipwire.Index, options Options, root, project string) (facts.SemanticPayload, error) {
	return convert(index, options, root, root, project)
}

// convert reads sources from one root and takes the payload's identity from
// the other. They differ whenever a scratch tree is involved.
func convert(index scipwire.Index, options Options, sources, repository, project string) (facts.SemanticPayload, error) {
	name := strings.TrimSuffix(filepath.Base(project), filepath.Ext(project))
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(repository)
	}
	manifest := ""
	if project != "" {
		// The project may live in the scratch tree, so the manifest path is
		// made relative to whichever root actually contains it.
		if relative, err := filepath.Rel(sources, project); err == nil &&
			!strings.HasPrefix(relative, "..") {
			manifest = filepath.ToSlash(relative)
		} else if relative, err := filepath.Rel(repository, project); err == nil {
			manifest = filepath.ToSlash(relative)
		}
	}
	return scip.Convert(index, scip.Options{
		Language:        facts.LanguageCSharp,
		Repository:      options.Repository.Name,
		Package:         name,
		PackageRoot:     repository,
		ManifestPath:    manifest,
		Analyzer:        DefaultCommand,
		AnalyzerVersion: index.ToolVersion,
		// Roslyn resolved every target, so the references are type-checked.
		Authoritative: true,
		ReadFile: func(relative string) ([]byte, error) {
			return os.ReadFile(filepath.Join(sources, filepath.FromSlash(relative)))
		},
		IncludeFile: func(relative string) bool {
			return includeFile(relative, options)
		},
		Generated: isGenerated,
	})
}

// resolveProject finds what to index.
//
// A solution wins over a project because it is what the compiler builds:
// indexing one .csproj of a multi-project repository silently drops every
// other one, which reads as a repository with less code rather than an index
// that looked at part of it.
func resolveProject(root, configured string) (string, error) {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		path := trimmed
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("csharp project %q does not exist: %w", configured, err)
		}
		return path, nil
	}
	var solutions, projects []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "obj", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".sln":
			solutions = append(solutions, path)
		case ".csproj", ".vbproj":
			projects = append(projects, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("discover csharp project: %w", err)
	}
	sort.Strings(solutions)
	sort.Strings(projects)
	if len(solutions) > 0 {
		return solutions[0], nil
	}
	if len(projects) > 0 {
		return projects[0], nil
	}
	return "", fmt.Errorf("no .sln or .csproj under %q", root)
}

func outputPath(options Options, root string) (string, error) {
	base := strings.TrimSpace(options.TargetDirectory)
	if base == "" {
		return "", errors.New("csharp indexer: a target directory outside the repository is required")
	}
	absolute, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve csharp target directory: %w", err)
	}
	if within(absolute, root) {
		return "", fmt.Errorf(
			"csharp target directory %q is inside the indexed repository %q", absolute, root)
	}
	directory := filepath.Join(absolute, sanitise(options.Repository.Name))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create csharp target directory: %w", err)
	}
	return filepath.Join(directory, "index.scip"), nil
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

func includeFile(relative string, options Options) bool {
	if !strings.HasSuffix(strings.ToLower(relative), ".cs") {
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

// isGenerated names what the build writes.
//
// `obj/` is not a convention here, it is where the SDK puts the sources it
// generates: a `.NETCoreApp,Version=v8.0.AssemblyAttributes.cs` and a
// `GlobalUsings.g.cs` appear in every index of every project. Publishing them
// puts symbols in the graph that no one wrote and that vanish on `clean`.
func isGenerated(path string) bool {
	slashed := filepath.ToSlash(path)
	if strings.HasPrefix(slashed, "obj/") || strings.HasPrefix(slashed, "bin/") ||
		strings.Contains(slashed, "/obj/") || strings.Contains(slashed, "/bin/") {
		return true
	}
	lower := strings.ToLower(filepath.Base(slashed))
	return strings.HasSuffix(lower, ".g.cs") ||
		strings.HasSuffix(lower, ".generated.cs") ||
		strings.HasSuffix(lower, ".designer.cs")
}

// isTest is the layout the .NET templates produce. It is a path and filename
// rule, never a guess about content.
func isTest(path string) bool {
	slashed := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(slashed, "/tests/") || strings.HasPrefix(slashed, "tests/") {
		return true
	}
	base := filepath.Base(slashed)
	return strings.HasSuffix(base, "tests.cs") || strings.HasSuffix(base, "test.cs")
}

func lastLines(value string, count int) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
