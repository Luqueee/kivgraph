package goloader

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/Luqueee/ladygraph/internal/goworkspace"
)

// UnresolvedReason classifies a Go fact Ladygraph could not turn into an exact
// edge.
type UnresolvedReason string

const (
	// UnresolvedModuleProviderNotFound means no registered repository
	// declares the module that owns the target.
	UnresolvedModuleProviderNotFound UnresolvedReason = "MODULE_PROVIDER_NOT_FOUND"
	// UnresolvedPackageNotLoaded means an imported package produced no type
	// information, so nothing inside it can be referenced.
	UnresolvedPackageNotLoaded UnresolvedReason = "PACKAGE_NOT_LOADED"
	// UnresolvedObjectPathUnavailable means the target cannot be addressed
	// from outside its package.
	UnresolvedObjectPathUnavailable UnresolvedReason = "OBJECT_PATH_UNAVAILABLE"
	// UnresolvedAmbiguousModuleProvider means several repositories declare
	// the module path of the target.
	UnresolvedAmbiguousModuleProvider UnresolvedReason = "AMBIGUOUS_MODULE_PROVIDER"
	// UnresolvedReplaceConflict means the synthetic workspace could not agree
	// on a replacement for a module.
	UnresolvedReplaceConflict UnresolvedReason = "REPLACE_CONFLICT"
	// UnresolvedTypecheckFailed means the package does not typecheck, so its
	// facts are not trustworthy.
	UnresolvedTypecheckFailed UnresolvedReason = "TYPECHECK_FAILED"
)

// UnresolvedReference is one classified failure with its evidence.
type UnresolvedReference struct {
	Repository  string
	PackagePath string
	FileName    string
	// RequestedModulePath is the module the fact needed, when known.
	RequestedModulePath string
	// RequestedPackagePath is the package the fact needed, when known.
	RequestedPackagePath string
	// RequestedSymbol is the qualified name of the target, when known.
	RequestedSymbol string
	Reason          UnresolvedReason
	// Detail carries the observed evidence: diagnostics or candidates.
	Detail string
	// Candidates lists the competing providers of an ambiguous module.
	Candidates []ModuleProvider

	StartLine   int
	StartColumn int
	Offset      int
}

// UnresolvedOptions supplies the facts that do not come from the load itself.
type UnresolvedOptions struct {
	// Repository is the repository being indexed.
	Repository string
	// WorkspaceConflicts are the conflicts reported by LUQUE-0801 when the
	// synthetic workspace was built.
	WorkspaceConflicts []goworkspace.Conflict
}

// ClassifyUnresolved reports every Go fact that did not become an exact edge.
//
// Nothing here is inferred: each reason is backed by a load diagnostic, a
// registry answer or a workspace conflict.
func ClassifyUnresolved(
	ctx context.Context,
	result Result,
	references []CrossRepositoryReference,
	options UnresolvedOptions,
) ([]UnresolvedReference, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	unresolved := make([]UnresolvedReference, 0)
	unresolved = append(unresolved, fromReferences(references, options.Repository)...)

	imports, err := fromMissingImports(ctx, result, options.Repository)
	if err != nil {
		return nil, err
	}
	unresolved = append(unresolved, imports...)
	unresolved = append(unresolved, fromDiagnostics(result, options.Repository)...)
	unresolved = append(unresolved, fromWorkspaceConflicts(options)...)

	sort.SliceStable(unresolved, func(left, right int) bool {
		if unresolved[left].FileName != unresolved[right].FileName {
			return unresolved[left].FileName < unresolved[right].FileName
		}
		if unresolved[left].Offset != unresolved[right].Offset {
			return unresolved[left].Offset < unresolved[right].Offset
		}
		if unresolved[left].Reason != unresolved[right].Reason {
			return unresolved[left].Reason < unresolved[right].Reason
		}
		return unresolved[left].RequestedSymbol < unresolved[right].RequestedSymbol
	})
	return unresolved, nil
}

// fromReferences maps the statuses of cross-repository resolution.
func fromReferences(
	references []CrossRepositoryReference,
	repository string,
) []UnresolvedReference {
	unresolved := make([]UnresolvedReference, 0)
	for _, reference := range references {
		reason, failed := referenceReason(reference.Status)
		if !failed {
			continue
		}
		entry := UnresolvedReference{
			Repository:           repository,
			PackagePath:          reference.PackagePath,
			FileName:             reference.FileName,
			RequestedModulePath:  reference.TargetModulePath,
			RequestedPackagePath: reference.TargetPackagePath,
			RequestedSymbol:      reference.TargetQualifiedName,
			Reason:               reason,
			StartLine:            reference.StartLine,
			StartColumn:          reference.StartColumn,
			Offset:               reference.Offset,
		}
		if reason == UnresolvedAmbiguousModuleProvider {
			entry.Candidates = append([]ModuleProvider(nil), reference.Providers...)
			entry.Detail = candidateDetail(reference.Providers)
		}
		unresolved = append(unresolved, entry)
	}
	return unresolved
}

func referenceReason(status CrossRepositoryStatus) (UnresolvedReason, bool) {
	switch status {
	case ModuleProviderNotFound:
		return UnresolvedModuleProviderNotFound, true
	case AmbiguousModuleProvider:
		return UnresolvedAmbiguousModuleProvider, true
	case ObjectPathUnavailable:
		return UnresolvedObjectPathUnavailable, true
	case ReplaceConflictTarget:
		return UnresolvedReplaceConflict, true
	default:
		return "", false
	}
}

func candidateDetail(providers []ModuleProvider) string {
	repositories := make([]string, 0, len(providers))
	for _, provider := range providers {
		repositories = append(repositories, provider.Repository)
	}
	sort.Strings(repositories)
	return "repositories: " + strings.Join(repositories, ", ")
}

// fromMissingImports reports imports that produced no type information.
//
// A package that did not load leaves no resolved use behind, so the failure
// has to be read from the import site itself.
func fromMissingImports(
	ctx context.Context,
	result Result,
	repository string,
) ([]UnresolvedReference, error) {
	unresolved := make([]UnresolvedReference, 0)
	for _, loaded := range result.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, file := range loaded.Syntax {
			for _, specification := range file.Imports {
				importPath, err := strconv.Unquote(specification.Path.Value)
				if err != nil {
					continue
				}
				if importedIsLoaded(loaded, importPath) {
					continue
				}
				place := result.Fset.Position(specification.Path.Pos())
				unresolved = append(unresolved, UnresolvedReference{
					Repository:           repository,
					PackagePath:          loaded.PkgPath,
					FileName:             place.Filename,
					RequestedPackagePath: importPath,
					Reason:               UnresolvedPackageNotLoaded,
					Detail:               importDetail(loaded, importPath),
					StartLine:            place.Line,
					StartColumn:          place.Column,
					Offset:               place.Offset,
				})
			}
		}
	}
	return unresolved, nil
}

// importedIsLoaded reports whether an import produced usable type
// information. A package the go command could not resolve is still present in
// the graph as a stub, so its diagnostics are the reliable signal.
func importedIsLoaded(loaded *packages.Package, importPath string) bool {
	imported := loaded.Imports[importPath]
	if imported == nil || imported.Types == nil || !imported.Types.Complete() {
		return false
	}
	return len(imported.Errors) == 0
}

func importDetail(loaded *packages.Package, importPath string) string {
	imported := loaded.Imports[importPath]
	if imported == nil {
		return "import not present in the load graph"
	}
	for _, failure := range imported.Errors {
		return failure.Msg
	}
	return "package loaded without complete type information"
}

// fromDiagnostics reports packages whose own diagnostics make their facts
// untrustworthy.
func fromDiagnostics(result Result, repository string) []UnresolvedReference {
	unresolved := make([]UnresolvedReference, 0)
	for _, failure := range result.Errors {
		if failure.Kind != TypeError && failure.Kind != ParseError {
			continue
		}
		fileName, line, column := splitPosition(failure.Position)
		unresolved = append(unresolved, UnresolvedReference{
			Repository:           repository,
			PackagePath:          failure.PackagePath,
			FileName:             fileName,
			RequestedPackagePath: failure.PackagePath,
			Reason:               UnresolvedTypecheckFailed,
			Detail:               string(failure.Kind) + ": " + failure.Message,
			StartLine:            line,
			StartColumn:          column,
		})
	}
	return unresolved
}

// fromWorkspaceConflicts reports replacements the synthetic workspace refused.
func fromWorkspaceConflicts(options UnresolvedOptions) []UnresolvedReference {
	unresolved := make([]UnresolvedReference, 0)
	for _, conflict := range options.WorkspaceConflicts {
		if conflict.Kind != goworkspace.ReplaceConflict {
			continue
		}
		unresolved = append(unresolved, UnresolvedReference{
			Repository:          options.Repository,
			RequestedModulePath: conflict.Subject,
			Reason:              UnresolvedReplaceConflict,
			Detail:              strings.Join(conflict.Details, "; "),
		})
	}
	return unresolved
}

// splitPosition parses the `file:line:column` form used by go/packages.
func splitPosition(position string) (string, int, int) {
	if position == "" {
		return "", 0, 0
	}
	parts := strings.Split(position, ":")
	if len(parts) < 3 {
		return position, 0, 0
	}
	line, lineErr := strconv.Atoi(parts[len(parts)-2])
	column, columnErr := strconv.Atoi(parts[len(parts)-1])
	if lineErr != nil || columnErr != nil {
		return position, 0, 0
	}
	return strings.Join(parts[:len(parts)-2], ":"), line, column
}
