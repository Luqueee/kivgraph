package goloader

import (
	"context"
	"sort"
)

// PackageDependency is one dependency between two packages, deduplicated
// across every use that crosses the boundary between them.
//
// A package can be used by many symbols in many files, but the graph wants
// one edge per dependency, not one per occurrence: deduplicating here, in the
// loader, keeps the grouping next to the raw material it consumes instead of
// pushing every caller of this pass to repeat the same (source, target)
// grouping that facts.NormalizeGo would otherwise have to redo for both
// PACKAGE_DEPENDS_ON and MODULE_DEPENDS_ON. References stay one per
// occurrence by design, because each is its own edge with its own evidence;
// a package dependency is a summary fact instead, so it is built as one from
// the start.
type PackageDependency struct {
	Repository  string
	ModulePath  string
	PackagePath string

	// TargetModulePath is empty for the standard library, exactly like
	// Use.TargetModulePath.
	TargetModulePath  string
	TargetPackagePath string

	// The fields below describe the witness use chosen to represent the
	// whole dependency: the earliest one, by file name then position. A
	// package edge summarises many uses and cannot anchor its evidence to
	// all of them at once, so a deterministic witness is picked instead of
	// an arbitrary one — the first one observed in iteration order would
	// depend on how the caller happened to gather uses, and would not
	// survive a rerun in a different order byte for byte.
	FileName    string
	Name        string
	Offset      int
	EndOffset   int
	StartLine   int
	StartColumn int
}

// ResolvePackageDependencies groups every use that crosses a package
// boundary into one PackageDependency per (source, target) package pair.
//
// Whether a pair becomes PACKAGE_DEPENDS_ON, additionally MODULE_DEPENDS_ON,
// or nothing at all because its target cannot be attributed to an indexed
// package, is decided later by facts.NormalizeGo, exactly like it already
// decides for a plain reference. This pass only proves the pair exists and
// picks its evidence, so it takes no CrossRepositoryReference input of its
// own: the witness it returns carries the file name and offset of a real
// use, which is precisely how NormalizeGo already keys CrossRepositoryReference
// to resolve a reference's target, so the same lookup resolves a
// dependency's target too.
func ResolvePackageDependencies(ctx context.Context, uses []Use) ([]PackageDependency, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	byPair := make(map[string]PackageDependency)
	for _, use := range uses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if use.TargetPackagePath == "" || use.TargetPackagePath == use.PackagePath {
			continue // A package never depends on itself.
		}
		key := use.PackagePath + "\x00" + use.TargetPackagePath
		if existing, seen := byPair[key]; seen && !isEarlierWitness(use, existing) {
			continue
		}
		byPair[key] = PackageDependency{
			Repository:        use.Repository,
			ModulePath:        use.ModulePath,
			PackagePath:       use.PackagePath,
			TargetModulePath:  use.TargetModulePath,
			TargetPackagePath: use.TargetPackagePath,
			FileName:          use.FileName,
			Name:              use.Name,
			Offset:            use.Offset,
			EndOffset:         use.EndOffset,
			StartLine:         use.StartLine,
			StartColumn:       use.StartColumn,
		}
	}

	dependencies := make([]PackageDependency, 0, len(byPair))
	for _, dependency := range byPair {
		dependencies = append(dependencies, dependency)
	}
	sort.Slice(dependencies, func(left, right int) bool {
		if dependencies[left].PackagePath != dependencies[right].PackagePath {
			return dependencies[left].PackagePath < dependencies[right].PackagePath
		}
		return dependencies[left].TargetPackagePath < dependencies[right].TargetPackagePath
	})
	return dependencies, nil
}

// isEarlierWitness reports whether use would be a more deterministic witness
// of its pair than the one already chosen: earlier by file name, then by
// position within that file. Input order is never trusted — every candidate
// is compared explicitly — so the result does not depend on how the caller
// happened to gather its uses.
func isEarlierWitness(use Use, chosen PackageDependency) bool {
	if use.FileName != chosen.FileName {
		return use.FileName < chosen.FileName
	}
	return use.Offset < chosen.Offset
}
