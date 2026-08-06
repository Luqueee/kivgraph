// Package indexer contains the language-specific invalidation plans that sit
// between file changes and canonical fact deltas.
package indexer

import (
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/syntax"
)

// ChangeKind is a conservative classification of the observable impact of a
// changed file. It is never a semantic relationship and never creates a graph
// edge by itself.
type ChangeKind string

const (
	ChangeBodyOnly           ChangeKind = "BODY_ONLY"
	ChangeSignatureChanged   ChangeKind = "SIGNATURE_CHANGED"
	ChangeImportsChanged     ChangeKind = "IMPORTS_CHANGED"
	ChangeExportsChanged     ChangeKind = "EXPORTS_CHANGED"
	ChangeDeclarationAdded   ChangeKind = "DECLARATION_ADDED"
	ChangeDeclarationRemoved ChangeKind = "DECLARATION_REMOVED"
	ChangeManifestChanged    ChangeKind = "MANIFEST_CHANGED"
	ChangeProjectConfig      ChangeKind = "PROJECT_CONFIG_CHANGED"
	ChangeFileDeleted        ChangeKind = "FILE_DELETED"
	ChangeGoModChanged       ChangeKind = "GO_MOD_CHANGED"
	ChangeReplaceChanged     ChangeKind = "REPLACE_CHANGED"
	ChangePackageDeleted     ChangeKind = "PACKAGE_DELETED"
	ChangeUnknown            ChangeKind = "UNKNOWN"
)

// InvalidationAction is an instruction for the later index/delta coordinator.
// Actions describe scope, not facts: executing one must still obtain facts from
// the authoritative language engine.
type InvalidationAction string

const (
	ActionReindexFile                InvalidationAction = "REINDEX_FILE"
	ActionReindexProvider            InvalidationAction = "REINDEX_PROVIDER"
	ActionReindexPackage             InvalidationAction = "REINDEX_PACKAGE"
	ActionInvalidateConsumers        InvalidationAction = "INVALIDATE_CONSUMERS"
	ActionResolveReferences          InvalidationAction = "RESOLVE_REFERENCES"
	ActionRebuildRegistry            InvalidationAction = "REBUILD_REGISTRY"
	ActionInvalidateModuleResolution InvalidationAction = "INVALIDATE_MODULE_RESOLUTION"
	ActionReindexProject             InvalidationAction = "REINDEX_PROJECT"
	ActionRemoveFile                 InvalidationAction = "REMOVE_FILE"
	ActionRemovePackage              InvalidationAction = "REMOVE_PACKAGE"
)

// InvalidationPlan identifies the affected source and the conservative work
// required before a canonical delta can be produced.
type InvalidationPlan struct {
	Language      facts.Language       `json:"language"`
	RepositoryKey string               `json:"repository_key,omitempty"`
	PackageKey    string               `json:"package_key,omitempty"`
	FileKey       string               `json:"file_key,omitempty"`
	Path          string               `json:"path,omitempty"`
	Class         ChangeKind           `json:"class"`
	ChangedRanges []syntax.SyntaxRange `json:"changed_ranges,omitempty"`
	Actions       []InvalidationAction `json:"actions"`
}

// Has reports whether the plan contains action.
func (plan InvalidationPlan) Has(action InvalidationAction) bool {
	for _, candidate := range plan.Actions {
		if candidate == action {
			return true
		}
	}
	return false
}

func newPlan(language facts.Language, repositoryKey, packageKey, fileKey, path string) InvalidationPlan {
	return InvalidationPlan{
		Language:      language,
		RepositoryKey: repositoryKey,
		PackageKey:    packageKey,
		FileKey:       fileKey,
		Path:          path,
		Class:         ChangeUnknown,
		Actions:       make([]InvalidationAction, 0, 4),
	}
}

func addActions(plan *InvalidationPlan, actions ...InvalidationAction) {
	for _, action := range actions {
		if plan.Has(action) {
			continue
		}
		plan.Actions = append(plan.Actions, action)
	}
}

func copyRanges(ranges []syntax.SyntaxRange) []syntax.SyntaxRange {
	return syntax.SortChangedRanges(ranges)
}
