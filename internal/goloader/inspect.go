package goloader

import (
	"context"
	"fmt"
	"sort"

	"golang.org/x/tools/go/packages"
)

// InspectMode asks the go command for names and files and nothing else. It is
// what `go list` alone can answer, so it costs a fraction of a load: no type
// universe is built, no dependency is parsed.
const InspectMode = packages.NeedName | packages.NeedFiles | packages.NeedModule

// InspectedPackage is one package the go command resolved, with whatever it
// refused to resolve about it.
type InspectedPackage struct {
	// PackagePath is the import path, or the directory when the go command
	// could not name one.
	PackagePath string
	// Errors are the go command's own messages, verbatim. A package whose
	// files are all excluded by build constraints is reported here and
	// nowhere else.
	Errors []string
}

// InspectResult is one list-only pass over a module.
type InspectResult struct {
	Packages []InspectedPackage
}

// Inspect lists the packages of one module under the same environment and the
// same build tags a load would use, without building a type universe.
//
// It exists so a caller can ask "would this module contribute" without paying
// for the answer to "what does it contain". Asking the go command is the only
// honest way: the rule that decides whether a file is selected is the build
// configuration's, and reimplementing it here would answer for a build nobody
// runs.
func Inspect(ctx context.Context, options Options) (InspectResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return InspectResult{}, err
	}
	directory, err := resolveDirectory(options.Directory)
	if err != nil {
		return InspectResult{}, err
	}
	patterns := options.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	tagFlags, err := buildTagFlags(options.BuildTags)
	if err != nil {
		return InspectResult{}, err
	}
	environment, err := loadEnvironment(options)
	if err != nil {
		return InspectResult{}, err
	}
	loaded, err := packages.Load(&packages.Config{
		Mode:       InspectMode,
		Context:    ctx,
		Dir:        directory,
		Env:        environment,
		Tests:      options.IncludeTests,
		BuildFlags: tagFlags,
	}, patterns...)
	if err != nil {
		return InspectResult{}, fmt.Errorf("list packages of %q: %w", directory, err)
	}

	result := InspectResult{Packages: make([]InspectedPackage, 0, len(loaded))}
	for _, entry := range loaded {
		inspected := InspectedPackage{PackagePath: entry.PkgPath}
		if inspected.PackagePath == "" {
			inspected.PackagePath = entry.ID
		}
		for _, packageError := range entry.Errors {
			inspected.Errors = append(inspected.Errors, packageError.Msg)
		}
		result.Packages = append(result.Packages, inspected)
	}
	sort.Slice(result.Packages, func(left, right int) bool {
		return result.Packages[left].PackagePath < result.Packages[right].PackagePath
	})
	return result, nil
}
