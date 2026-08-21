// Package goloader loads Go packages with full type information for the
// registered repositories.
//
// Loading always goes through go/packages with the complete semantic mode of
// the plan: anything less would force Kivgraph to reconstruct type identity by
// hand. Every load creates a fresh go/types universe, so objects of different
// loads are never mixed; callers must normalise before the session ends.
package goloader

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// LoadMode is the semantic mode required by the analysis phases.
const LoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedTypesSizes |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedModule

// ErrNoPatterns reports a load request with nothing to load.
var ErrNoPatterns = errors.New("no load patterns were provided")

// ErrNoPackages reports that the patterns matched no package at all.
var ErrNoPackages = errors.New("patterns matched no Go package")

// ErrorKind classifies one partial failure reported by the loader.
type ErrorKind string

const (
	// ListError comes from the go command: resolution or configuration.
	ListError ErrorKind = "LIST"
	// ParseError is a syntax failure in a source file.
	ParseError ErrorKind = "PARSE"
	// TypeError is a semantic failure reported by the type checker.
	TypeError ErrorKind = "TYPE"
	// UnknownError is anything go/packages could not classify.
	UnknownError ErrorKind = "UNKNOWN"
)

// PackageError is one partial failure attached to the package that produced it.
type PackageError struct {
	PackageID   string
	PackagePath string
	Kind        ErrorKind
	Position    string
	Message     string
}

// noPackageDiagnostics are the go command messages that report a directory
// declaring no package under the current build configuration. Selecting no
// file is a configuration outcome, not a failure of the load: the directory
// contributes nothing to this build, exactly as if it held no Go source.
var noPackageDiagnostics = []string{
	"build constraints exclude all Go files in ",
	"no Go files in ",
	"no non-test Go files in ",
}

// NoPackage reports whether the diagnostic states that the directory declares
// no package the current build configuration selects.
func (failure PackageError) NoPackage() bool {
	if failure.Kind != ListError {
		return false
	}
	for _, prefix := range noPackageDiagnostics {
		if strings.HasPrefix(failure.Message, prefix) {
			return true
		}
	}
	return false
}

// toolchainSkew reports whether the diagnostic is the advisory go/packages
// attaches to a package that already reports one when the go command is newer
// than the toolchain that built this binary. It qualifies other diagnostics
// and never reports a failure of its own, so on its own it says nothing about
// the code.
func (failure PackageError) toolchainSkew() bool {
	return failure.Kind == UnknownError &&
		failure.Position == "-" &&
		strings.HasPrefix(failure.Message, "This application uses version go1.") &&
		strings.Contains(failure.Message, "of 'go list'")
}

// Blocking reports whether the diagnostic makes the facts of its package
// untrustworthy. Only a blocking diagnostic may abort an index.
func (failure PackageError) Blocking() bool {
	return !failure.NoPackage() && !failure.toolchainSkew()
}

// Module is the module metadata observed for a loaded package.
type Module struct {
	Path         string
	Version      string
	Directory    string
	ManifestPath string
	// ReplacedBy is the module path that replaced Path, when a replace
	// directive applied. Empty when the module was used directly.
	ReplacedBy string
	// ReplacedDirectory is the directory the replacement resolved to.
	ReplacedDirectory string
	Main              bool
}

// Result is one complete load: a private type universe plus its diagnostics.
type Result struct {
	Fset *token.FileSet
	// Packages are the roots the patterns matched, sorted by package path.
	Packages []*packages.Package
	// Modules are the modules observed across roots, sorted by module path.
	Modules []Module
	// Errors are partial failures. A load can succeed with errors present.
	Errors []PackageError
}

// BlockingErrors returns the diagnostics that make the facts of this load
// untrustworthy, in the order of Errors.
func (result Result) BlockingErrors() []PackageError {
	blocking := make([]PackageError, 0, len(result.Errors))
	for _, failure := range result.Errors {
		if failure.Blocking() {
			blocking = append(blocking, failure)
		}
	}
	return blocking
}

// Options configures one load.
type Options struct {
	// Directory is the working directory of the go command.
	Directory string
	// WorkFile is the synthetic go.work of LUQUE-0801. Empty disables
	// workspace mode.
	WorkFile string
	// Patterns default to "./..." when empty.
	Patterns []string
	// IncludeTests loads test packages as well.
	IncludeTests bool
	// GOOS and GOARCH select the target platform for the go command. Empty
	// values preserve the toolchain defaults.
	GOOS       string
	GOARCH     string
	CGOEnabled *bool
	// BuildTags are the build constraints the load satisfies, passed to the
	// go command as -tags. A package guarded by a tag that is absent here
	// declares no file to select and contributes nothing to the graph.
	BuildTags []string
	// AllowNetwork permits the go command to reach a module proxy. Indexing
	// is hermetic by default: a missing dependency is reported, not fetched.
	AllowNetwork bool
	// Environment overrides entries of the inherited environment.
	Environment []string
}

// Load resolves the patterns and returns the packages with type information.
//
// Partial failures do not abort the load: a repository that does not compile
// still yields the packages that do, and every failure is reported.
func Load(ctx context.Context, options Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	directory, err := resolveDirectory(options.Directory)
	if err != nil {
		return Result{}, err
	}
	patterns := options.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			return Result{}, ErrNoPatterns
		}
	}
	tagFlags, err := buildTagFlags(options.BuildTags)
	if err != nil {
		return Result{}, err
	}

	environment, err := loadEnvironment(options)
	if err != nil {
		return Result{}, err
	}
	fset := token.NewFileSet()
	configuration := &packages.Config{
		Mode:       LoadMode,
		Context:    ctx,
		Dir:        directory,
		Env:        environment,
		Fset:       fset,
		Tests:      options.IncludeTests,
		BuildFlags: tagFlags,
	}

	roots, err := packages.Load(configuration, patterns...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, ctxErr
		}
		return Result{}, fmt.Errorf("load %v: %w", patterns, err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(roots) == 0 {
		return Result{}, ErrNoPackages
	}

	sort.Slice(roots, func(left, right int) bool {
		if roots[left].PkgPath != roots[right].PkgPath {
			return roots[left].PkgPath < roots[right].PkgPath
		}
		return roots[left].ID < roots[right].ID
	})
	return Result{
		Fset:     fset,
		Packages: roots,
		Modules:  collectModules(roots),
		Errors:   collectErrors(roots),
	}, nil
}

func resolveDirectory(directory string) (string, error) {
	trimmed := strings.TrimSpace(directory)
	if trimmed == "" {
		return "", fmt.Errorf("load directory must not be empty")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", directory, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("load directory %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("load directory %q is not a directory", absolute)
	}
	return absolute, nil
}

// loadEnvironment builds the go command environment. GOWORK selects the
// synthetic workspace and GOPROXY=off keeps indexing hermetic unless the
// caller opts into network access.
func loadEnvironment(options Options) ([]string, error) {
	environment := os.Environ()
	if value := strings.TrimSpace(options.GOOS); value != "" {
		environment = append(environment, "GOOS="+value)
	}
	if value := strings.TrimSpace(options.GOARCH); value != "" {
		environment = append(environment, "GOARCH="+value)
	}
	if options.CGOEnabled != nil {
		value := "0"
		if *options.CGOEnabled {
			value = "1"
		}
		environment = append(environment, "CGO_ENABLED="+value)
	}
	if workFile := strings.TrimSpace(options.WorkFile); workFile != "" {
		absolute, err := filepath.Abs(workFile)
		if err != nil {
			return nil, fmt.Errorf("resolve work file %q: %w", workFile, err)
		}
		if _, err := os.Stat(absolute); err != nil {
			return nil, fmt.Errorf("work file %q: %w", absolute, err)
		}
		environment = append(environment, "GOWORK="+absolute)
	}
	if !options.AllowNetwork {
		environment = append(environment, "GOPROXY=off", "GOFLAGS=-mod=readonly")
	}
	environment = append(environment, options.Environment...)
	return environment, nil
}

// buildTagFlags turns the requested build tags into the go command flag that
// carries them. A tag the go command cannot express is rejected here rather
// than silently reshaping the build configuration of the load.
func buildTagFlags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	selected := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			return nil, errors.New("build tag must not be empty")
		}
		if strings.ContainsAny(trimmed, ", \t\n") {
			return nil, fmt.Errorf("build tag %q: must not contain a comma or whitespace", tag)
		}
		selected = append(selected, trimmed)
	}
	return []string{"-tags=" + strings.Join(selected, ",")}, nil
}

func collectModules(roots []*packages.Package) []Module {
	seen := make(map[string]Module)
	packages.Visit(roots, nil, func(loaded *packages.Package) {
		module := loaded.Module
		if module == nil {
			return
		}
		entry := Module{
			Path:         module.Path,
			Version:      module.Version,
			Directory:    module.Dir,
			ManifestPath: module.GoMod,
			Main:         module.Main,
		}
		if module.Replace != nil {
			entry.ReplacedBy = module.Replace.Path
			entry.ReplacedDirectory = module.Replace.Dir
			if module.Replace.Dir != "" {
				entry.Directory = module.Replace.Dir
			}
			if module.Replace.GoMod != "" {
				entry.ManifestPath = module.Replace.GoMod
			}
		}
		key := entry.Path + "@" + entry.Version
		if _, exists := seen[key]; !exists {
			seen[key] = entry
		}
	})
	modules := make([]Module, 0, len(seen))
	for _, module := range seen {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(left, right int) bool {
		if modules[left].Path != modules[right].Path {
			return modules[left].Path < modules[right].Path
		}
		return modules[left].Version < modules[right].Version
	})
	return modules
}

func collectErrors(roots []*packages.Package) []PackageError {
	failures := make([]PackageError, 0)
	seen := make(map[string]struct{})
	packages.Visit(roots, nil, func(loaded *packages.Package) {
		for _, failure := range loaded.Errors {
			entry := PackageError{
				PackageID:   loaded.ID,
				PackagePath: loaded.PkgPath,
				Kind:        classify(failure.Kind),
				Position:    failure.Pos,
				Message:     failure.Msg,
			}
			key := strings.Join([]string{
				entry.PackageID, string(entry.Kind), entry.Position, entry.Message,
			}, "\x00")
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			failures = append(failures, entry)
		}
	})
	sort.Slice(failures, func(left, right int) bool {
		if failures[left].PackageID != failures[right].PackageID {
			return failures[left].PackageID < failures[right].PackageID
		}
		if failures[left].Position != failures[right].Position {
			return failures[left].Position < failures[right].Position
		}
		return failures[left].Message < failures[right].Message
	})
	return failures
}

func classify(kind packages.ErrorKind) ErrorKind {
	switch kind {
	case packages.ListError:
		return ListError
	case packages.ParseError:
		return ParseError
	case packages.TypeError:
		return TypeError
	default:
		return UnknownError
	}
}
