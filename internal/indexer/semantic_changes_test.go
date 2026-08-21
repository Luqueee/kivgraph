package indexer

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/syntax"
)

func TestClassifyPythonManifestChangeRebuildsProject(t *testing.T) {
	plan := ClassifyPythonChange(SemanticChange{Path: "pyproject.toml"})
	if plan.Language != facts.LanguagePython || plan.Class != ChangeManifestChanged || !plan.Has(ActionRebuildRegistry) || !plan.Has(ActionReindexProject) {
		t.Fatalf("Python manifest plan = %#v", plan)
	}
}

func TestClassifyDartBodyChangeCanReindexFile(t *testing.T) {
	plan := ClassifyDartChange(SemanticChange{
		Path:          "lib/service.dart",
		Previous:      syntax.SyntaxInventory{Language: syntax.LanguageTypeScript},
		Current:       syntax.SyntaxInventory{Language: syntax.LanguageTypeScript},
		ChangedRanges: []syntax.SyntaxRange{{StartByte: 10, EndByte: 12}},
	})
	if plan.Language != facts.LanguageDart || plan.Class != ChangeBodyOnly || !plan.Has(ActionReindexFile) {
		t.Fatalf("Dart body plan = %#v", plan)
	}
}
