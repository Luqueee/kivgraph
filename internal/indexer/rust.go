package indexer

import (
	"path/filepath"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/syntax"
)

// RustChange carries the syntax evidence and the workspace metadata for one
// Rust file transition. A manifest or lockfile change is supplied by discovery
// because it decides which crates exist at all.
type RustChange struct {
	RepositoryKey string
	PackageKey    string
	FileKey       string
	Path          string

	Previous      syntax.SyntaxInventory
	Current       syntax.SyntaxInventory
	ChangedRanges []syntax.SyntaxRange

	Deleted  bool
	Manifest bool
	// BuildScript marks `build.rs`, whose output decides what the analyzer
	// sees of the crate that owns it.
	BuildScript bool
}

// ClassifyRustChange converts syntax evidence into a conservative invalidation
// plan.
//
// The unit the analyzer can rerun is the whole Cargo workspace: `rust-analyzer
// scip` has no per file mode. A body-only edit still reindexes just the file's
// facts, but anything that can change a signature, a manifest or the build
// script reindexes the project, because that is the smallest thing the engine
// can actually recompute.
func ClassifyRustChange(change RustChange) InvalidationPlan {
	plan := newPlan(facts.LanguageRust, change.RepositoryKey, change.PackageKey, change.FileKey, change.Path)
	plan.ChangedRanges = copyRanges(change.ChangedRanges)

	manifest := change.Manifest || isCargoManifest(change.Path)
	buildScript := change.BuildScript || isCargoBuildScript(change.Path)
	switch {
	case manifest:
		plan.Class = ChangeManifestChanged
		if change.Deleted {
			addActions(&plan, ActionRemoveFile)
		}
		addActions(&plan, ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject)
	case buildScript:
		// A build script decides which code exists, and its output is read
		// through cargo rather than from the file.
		plan.Class = ChangeProjectConfig
		if change.Deleted {
			addActions(&plan, ActionRemoveFile)
		}
		addActions(&plan, ActionReindexProject)
	case change.Deleted:
		plan.Class = ChangeFileDeleted
		addActions(&plan, ActionRemoveFile, ActionInvalidateConsumers, ActionResolveReferences)
	case change.Previous.HasErrors || change.Current.HasErrors:
		plan.Class = ChangeUnknown
		addActions(&plan, ActionReindexProject)
	default:
		classification := syntax.ClassifyChanges(change.Previous, change.Current, change.ChangedRanges)
		plan.Class = ChangeKind(classification.Class)
		switch plan.Class {
		case ChangeBodyOnly:
			addActions(&plan, ActionReindexFile)
		case ChangeImportsChanged:
			// A `use` decides which crate a name comes from, so the target
			// of every binding it introduces may have moved.
			addActions(&plan, ActionReindexFile, ActionInvalidateModuleResolution, ActionResolveReferences)
		case ChangeExportsChanged, ChangeSignatureChanged, ChangeDeclarationAdded, ChangeDeclarationRemoved:
			addActions(&plan, ActionReindexProvider, ActionInvalidateConsumers, ActionResolveReferences)
		default:
			// An incomplete inventory is not evidence for a narrow scope.
			plan.Class = ChangeUnknown
			addActions(&plan, ActionReindexProject)
		}
	}
	return plan
}

func isCargoManifest(path string) bool {
	switch filepath.Base(filepath.Clean(path)) {
	case "Cargo.toml", "Cargo.lock":
		return true
	default:
		return false
	}
}

func isCargoBuildScript(path string) bool {
	return filepath.Base(filepath.Clean(path)) == "build.rs"
}
