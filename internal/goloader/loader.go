// Package goloader loads Go packages with full type information for the
// registered repositories.
//
// Loading always goes through go/packages with the complete semantic mode of
// the plan: anything less would force Ladygraph to reconstruct type identity by
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

	environment, err := loadEnvironment(options)
	if err != nil {
		return Result{}, err
	}
	fset := token.NewFileSet()
	configuration := &packages.Config{
		Mode:    LoadMode,
		Context: ctx,
		Dir:     directory,
		Env:     environment,
		Fset:    fset,
		Tests:   options.IncludeTests,
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
