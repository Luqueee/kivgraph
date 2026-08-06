package indexer

import (
	"path/filepath"
	"strings"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/syntax"
)

// TypeScriptChange carries the syntax evidence and the workspace metadata for
// one TypeScript or JavaScript file transition. Manifest and project-config
// changes are supplied by discovery because their filenames may be configured.
type TypeScriptChange struct {
	RepositoryKey string
	PackageKey    string
	FileKey       string
	Path          string

	Previous      syntax.SyntaxInventory
	Current       syntax.SyntaxInventory
	ChangedRanges []syntax.SyntaxRange

	Deleted       bool
	Manifest      bool
	ProjectConfig bool
}

// ClassifyTypeScriptChange converts syntax evidence into a conservative
// invalidation plan. Syntax candidates remain evidence only; no symbol or
// relationship is inferred here.
func ClassifyTypeScriptChange(change TypeScriptChange) InvalidationPlan {
	plan := newPlan(facts.LanguageTypeScript, change.RepositoryKey, change.PackageKey, change.FileKey, change.Path)
	plan.ChangedRanges = copyRanges(change.ChangedRanges)

	manifest := change.Manifest || isTypeScriptManifest(change.Path)
	projectConfig := change.ProjectConfig || isTypeScriptProjectConfig(change.Path)
	switch {
	case projectConfig:
		plan.Class = ChangeProjectConfig
		addProjectConfigActions(&plan, change.Deleted)
	case manifest:
		plan.Class = ChangeManifestChanged
		addProjectConfigActions(&plan, change.Deleted)
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
			// Import changes withdraw the old file-owned relations and may also
			// change the module-resolution target.
			addActions(&plan, ActionReindexFile, ActionInvalidateModuleResolution, ActionResolveReferences)
		case ChangeExportsChanged, ChangeSignatureChanged, ChangeDeclarationAdded, ChangeDeclarationRemoved:
			addActions(&plan, ActionReindexProvider, ActionInvalidateConsumers, ActionResolveReferences)
		default:
			// An incomplete or inconsistent syntax inventory is not evidence for
			// a narrow scope. Re-index the project rather than risking stale facts.
			plan.Class = ChangeUnknown
			addActions(&plan, ActionReindexProject)
		}
	}
	return plan
}

func isTypeScriptManifest(path string) bool {
	switch filepath.Base(filepath.Clean(path)) {
	case "package.json", "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "pnpm-workspace.yaml", "bun.lock", "bun.lockb":
		return true
	default:
		return false
	}
}

func isTypeScriptProjectConfig(path string) bool {
	base := filepath.Base(filepath.Clean(path))
	return base == "tsconfig.json" || base == "jsconfig.json" ||
		(strings.HasPrefix(base, "tsconfig.") && strings.HasSuffix(base, ".json")) ||
		(strings.HasPrefix(base, "jsconfig.") && strings.HasSuffix(base, ".json"))
}

func addProjectConfigActions(plan *InvalidationPlan, deleted bool) {
	if deleted {
		addActions(plan, ActionRemoveFile)
	}
	addActions(plan, ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject)
}
