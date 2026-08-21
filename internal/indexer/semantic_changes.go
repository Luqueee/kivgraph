package indexer

import (
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/syntax"
)

// SemanticChange describes a Python or Dart source transition. The current
// full semantic loaders still rebuild their project as the execution unit;
// these plans make the invalidation boundary explicit and conservative so a
// future delta coordinator cannot mistake a syntax hint for a graph edge.
type SemanticChange struct {
	RepositoryKey string
	PackageKey    string
	FileKey       string
	Path          string
	Previous      syntax.SyntaxInventory
	Current       syntax.SyntaxInventory
	ChangedRanges []syntax.SyntaxRange
	Deleted       bool
}

// ClassifyPythonChange returns the narrowest safe plan for the AST/semantic
// Python unit. Dependency manifests and parse errors force a project rebuild;
// source-only changes may be narrowed once the semantic provider is wired to a
// delta coordinator.
func ClassifyPythonChange(change SemanticChange) InvalidationPlan {
	return classifySemanticChange(facts.LanguagePython, change, isPythonManifest)
}

// ClassifyDartChange returns the narrowest safe plan for a Dart package. Dart
// Analysis Server is project-oriented, so a declaration or import change
// invalidates the package's consumers and a pubspec/configuration change
// rebuilds the project.
func ClassifyDartChange(change SemanticChange) InvalidationPlan {
	return classifySemanticChange(facts.LanguageDart, change, isDartManifest)
}

func classifySemanticChange(language facts.Language, change SemanticChange, manifest func(string) bool) InvalidationPlan {
	plan := newPlan(language, change.RepositoryKey, change.PackageKey, change.FileKey, change.Path)
	plan.ChangedRanges = copyRanges(change.ChangedRanges)
	switch {
	case manifest(change.Path):
		plan.Class = ChangeManifestChanged
		if change.Deleted {
			addActions(&plan, ActionRemoveFile)
		}
		addActions(&plan, ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject)
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
			addActions(&plan, ActionReindexFile, ActionInvalidateModuleResolution, ActionResolveReferences)
		case ChangeExportsChanged, ChangeSignatureChanged, ChangeDeclarationAdded, ChangeDeclarationRemoved:
			addActions(&plan, ActionReindexProvider, ActionInvalidateConsumers, ActionResolveReferences)
		default:
			plan.Class = ChangeUnknown
			addActions(&plan, ActionReindexProject)
		}
	}
	return plan
}

func isPythonManifest(path string) bool {
	switch filepath.Base(filepath.Clean(path)) {
	case "pyproject.toml", "setup.py", "setup.cfg", "requirements.txt", "Pipfile", "Pipfile.lock", "poetry.lock", "uv.lock":
		return true
	default:
		return strings.HasPrefix(filepath.Base(filepath.Clean(path)), "requirements-") && strings.HasSuffix(filepath.Base(filepath.Clean(path)), ".txt")
	}
}

func isDartManifest(path string) bool {
	switch filepath.Base(filepath.Clean(path)) {
	case "pubspec.yaml", "pubspec.lock", "analysis_options.yaml", "package_config.json":
		return true
	default:
		return false
	}
}
