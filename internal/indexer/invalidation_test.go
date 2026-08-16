package indexer

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/syntax"
)

func TestClassifyTypeScriptChangeMapsScopes(t *testing.T) {
	previous := syntax.SyntaxInventory{
		Language: syntax.LanguageTypeScript,
		Candidates: []syntax.SyntaxCandidate{{
			Kind: syntax.CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run()",
		}},
	}
	body := previous
	body.Candidates = append(body.Candidates, syntax.SyntaxCandidate{Kind: syntax.CandidateCall, NodeKind: "call_expression", Name: "helper()", Signature: "helper()"})
	bodyPlan := ClassifyTypeScriptChange(TypeScriptChange{
		RepositoryKey: "repo", PackageKey: "pkg", FileKey: "file", Path: "src/run.ts",
		Previous: previous, Current: body, ChangedRanges: []syntax.SyntaxRange{{StartByte: 20, EndByte: 30}},
	})
	if bodyPlan.Language != facts.LanguageTypeScript || bodyPlan.Class != ChangeBodyOnly {
		t.Fatalf("body plan = %#v", bodyPlan)
	}
	if !bodyPlan.Has(ActionReindexFile) || len(bodyPlan.Actions) != 1 {
		t.Fatalf("body actions = %v, want only REINDEX_FILE", bodyPlan.Actions)
	}

	imports := previous
	imports.Candidates = append([]syntax.SyntaxCandidate(nil), previous.Candidates...)
	imports.Candidates = append(imports.Candidates, syntax.SyntaxCandidate{Kind: syntax.CandidateImport, NodeKind: "import_statement", Name: "./new", Signature: "import x from \"./new\""})
	importPlan := ClassifyTypeScriptChange(TypeScriptChange{Previous: previous, Current: imports, ChangedRanges: []syntax.SyntaxRange{{StartByte: 8, EndByte: 9}}})
	if importPlan.Class != ChangeImportsChanged || !importPlan.Has(ActionReindexFile) || !importPlan.Has(ActionInvalidateModuleResolution) || !importPlan.Has(ActionResolveReferences) {
		t.Fatalf("import plan = %#v", importPlan)
	}

	signature := previous
	signature.Candidates = []syntax.SyntaxCandidate{{
		Kind: syntax.CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run(value: number)",
	}}
	signaturePlan := ClassifyTypeScriptChange(TypeScriptChange{Previous: previous, Current: signature})
	if signaturePlan.Class != ChangeSignatureChanged || !signaturePlan.Has(ActionReindexProvider) || !signaturePlan.Has(ActionInvalidateConsumers) || !signaturePlan.Has(ActionResolveReferences) {
		t.Fatalf("signature plan = %#v", signaturePlan)
	}

	exported := previous
	exported.Candidates = []syntax.SyntaxCandidate{
		{Kind: syntax.CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run()"},
		{Kind: syntax.CandidateExport, NodeKind: "export_statement", Name: "run", Signature: "export { run }"},
	}
	exportPlan := ClassifyTypeScriptChange(TypeScriptChange{Previous: previous, Current: exported})
	if exportPlan.Class != ChangeExportsChanged || !exportPlan.Has(ActionInvalidateConsumers) {
		t.Fatalf("export plan = %#v", exportPlan)
	}
}

func TestClassifyTypeScriptManifestProjectAndDeletion(t *testing.T) {
	manifest := ClassifyTypeScriptChange(TypeScriptChange{Path: "package.json", Manifest: true})
	if manifest.Class != ChangeManifestChanged || !manifest.Has(ActionRebuildRegistry) || !manifest.Has(ActionInvalidateModuleResolution) || !manifest.Has(ActionReindexProject) {
		t.Fatalf("manifest plan = %#v", manifest)
	}
	project := ClassifyTypeScriptChange(TypeScriptChange{Path: "tsconfig.json", ProjectConfig: true, Deleted: true})
	if project.Class != ChangeProjectConfig || !project.Has(ActionRemoveFile) || !project.Has(ActionRebuildRegistry) || !project.Has(ActionReindexProject) {
		t.Fatalf("project config plan = %#v", project)
	}
	deleted := ClassifyTypeScriptChange(TypeScriptChange{Path: "src/removed.ts", Deleted: true})
	if deleted.Class != ChangeFileDeleted || !deleted.Has(ActionRemoveFile) || !deleted.Has(ActionInvalidateConsumers) || !deleted.Has(ActionResolveReferences) {
		t.Fatalf("deleted plan = %#v", deleted)
	}
}

func TestClassifyTypeScriptInfersConfiguredFileKindsAndErrors(t *testing.T) {
	manifest := ClassifyTypeScriptChange(TypeScriptChange{Path: "workspace/package.json"})
	if manifest.Class != ChangeManifestChanged {
		t.Fatalf("manifest-by-path plan = %#v", manifest)
	}
	config := ClassifyTypeScriptChange(TypeScriptChange{Path: "packages/app/tsconfig.base.json"})
	if config.Class != ChangeProjectConfig {
		t.Fatalf("config-by-path plan = %#v", config)
	}
	withErrors := ClassifyTypeScriptChange(TypeScriptChange{
		Path:     "src/broken.ts",
		Previous: syntax.SyntaxInventory{Language: syntax.LanguageTypeScript, HasErrors: true},
		Current:  syntax.SyntaxInventory{Language: syntax.LanguageTypeScript},
	})
	if withErrors.Class != ChangeUnknown || !withErrors.Has(ActionReindexProject) {
		t.Fatalf("syntax-error plan = %#v", withErrors)
	}
}

func TestClassifyTypeScriptIsConservativeAndCopiesRanges(t *testing.T) {
	ranges := []syntax.SyntaxRange{{StartByte: 10, EndByte: 20}, {StartByte: 2, EndByte: 4}}
	plan := ClassifyTypeScriptChange(TypeScriptChange{
		Previous:      syntax.SyntaxInventory{Language: syntax.LanguageTypeScript},
		Current:       syntax.SyntaxInventory{Language: syntax.LanguageGo},
		ChangedRanges: ranges,
	})
	if plan.Class != ChangeUnknown || !plan.Has(ActionReindexProject) {
		t.Fatalf("unknown plan = %#v", plan)
	}
	if plan.ChangedRanges[0].StartByte != 2 || plan.ChangedRanges[1].StartByte != 10 {
		t.Fatalf("ranges are not source ordered: %v", plan.ChangedRanges)
	}
	plan.ChangedRanges[0].StartByte = 99
	if ranges[1].StartByte == 99 {
		t.Fatal("plan retained caller range storage")
	}
	for left, action := range plan.Actions {
		for right := left + 1; right < len(plan.Actions); right++ {
			if action == plan.Actions[right] {
				t.Fatalf("duplicate action %q in %v", action, plan.Actions)
			}
		}
	}
}

func TestClassifyGoChangeMapsAllRequiredSignals(t *testing.T) {
	tests := []struct {
		name    string
		change  GoChange
		class   ChangeKind
		actions []InvalidationAction
	}{
		{name: "body", change: GoChange{BodyChanged: true}, class: ChangeBodyOnly, actions: []InvalidationAction{ActionReindexFile}},
		{name: "signature", change: GoChange{SignatureChanged: true}, class: ChangeSignatureChanged, actions: []InvalidationAction{ActionReindexProvider, ActionInvalidateConsumers, ActionResolveReferences}},
		{name: "import", change: GoChange{ImportsChanged: true}, class: ChangeImportsChanged, actions: []InvalidationAction{ActionReindexPackage, ActionInvalidateModuleResolution, ActionResolveReferences}},
		{name: "go.mod", change: GoChange{GoModChanged: true}, class: ChangeGoModChanged, actions: []InvalidationAction{ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject}},
		{name: "replace", change: GoChange{GoModChanged: true, ReplaceChanged: true}, class: ChangeReplaceChanged, actions: []InvalidationAction{ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject}},
		{name: "file deletion", change: GoChange{FileDeleted: true}, class: ChangeFileDeleted, actions: []InvalidationAction{ActionRemoveFile, ActionInvalidateConsumers, ActionResolveReferences}},
		{name: "package deletion", change: GoChange{PackageDeleted: true}, class: ChangePackageDeleted, actions: []InvalidationAction{ActionRemovePackage, ActionRemoveFile, ActionInvalidateConsumers, ActionResolveReferences}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := ClassifyGoChange(test.change)
			if plan.Language != facts.LanguageGo || plan.Class != test.class {
				t.Fatalf("plan = %#v, want language=%q class=%q", plan, facts.LanguageGo, test.class)
			}
			for _, action := range test.actions {
				if !plan.Has(action) {
					t.Fatalf("plan = %#v, missing action %q", plan, action)
				}
			}
		})
	}
}

func TestClassifyGoSourceChangeDetectsBodySignatureAndImports(t *testing.T) {
	base := []byte("package sample\n\nimport \"fmt\"\n\nfunc Run(value int) int {\n\tfmt.Println(value)\n\treturn value\n}\n")
	body := []byte("package sample\n\nimport \"fmt\"\n\nfunc Run(value int) int {\n\tfmt.Println(value + 1)\n\treturn value\n}\n")
	bodyPlan, err := ClassifyGoSourceChange(GoSourceChange{GoChange: GoChange{Path: "run.go"}, Previous: base, Current: body})
	if err != nil {
		t.Fatalf("body classification error = %v", err)
	}
	if bodyPlan.Class != ChangeBodyOnly || !bodyPlan.Has(ActionReindexFile) {
		t.Fatalf("body plan = %#v", bodyPlan)
	}

	signature := []byte("package sample\n\nimport \"fmt\"\n\nfunc Run(value string) int {\n\tfmt.Println(value)\n\treturn 1\n}\n")
	signaturePlan, err := ClassifyGoSourceChange(GoSourceChange{GoChange: GoChange{Path: "run.go"}, Previous: base, Current: signature})
	if err != nil {
		t.Fatalf("signature classification error = %v", err)
	}
	if signaturePlan.Class != ChangeSignatureChanged || !signaturePlan.Has(ActionInvalidateConsumers) {
		t.Fatalf("signature plan = %#v", signaturePlan)
	}

	imports := []byte("package sample\n\nimport \"os\"\n\nfunc Run(value int) int {\n\tos.Stdout.Write(nil)\n\treturn value\n}\n")
	importPlan, err := ClassifyGoSourceChange(GoSourceChange{GoChange: GoChange{Path: "run.go"}, Previous: base, Current: imports})
	if err != nil {
		t.Fatalf("import classification error = %v", err)
	}
	if importPlan.Class != ChangeImportsChanged || !importPlan.Has(ActionReindexPackage) {
		t.Fatalf("import plan = %#v", importPlan)
	}
}

func TestClassifyGoModChangeDistinguishesReplace(t *testing.T) {
	before := []byte("module example.com/app\n\ngo 1.24\n\nrequire example.com/provider v1.0.0\n")
	after := []byte("module example.com/app\n\ngo 1.24\n\nrequire example.com/provider v1.1.0\n")
	plan, err := ClassifyGoModChange(GoChange{Path: "go.mod"}, before, after)
	if err != nil {
		t.Fatalf("go.mod classification error = %v", err)
	}
	if plan.Class != ChangeGoModChanged || plan.Has(ActionReindexFile) {
		t.Fatalf("go.mod plan = %#v", plan)
	}

	replaced := []byte("module example.com/app\n\ngo 1.24\n\nrequire example.com/provider v1.0.0\n\nreplace example.com/provider => ../provider\n")
	replacePlan, err := ClassifyGoModChange(GoChange{Path: "go.mod"}, before, replaced)
	if err != nil {
		t.Fatalf("replace classification error = %v", err)
	}
	if replacePlan.Class != ChangeReplaceChanged || !replacePlan.Has(ActionRebuildRegistry) || !replacePlan.Has(ActionInvalidateModuleResolution) {
		t.Fatalf("replace plan = %#v", replacePlan)
	}
}

func TestClassifyGoSourceChangeReportsParseErrors(t *testing.T) {
	plan, err := ClassifyGoSourceChange(GoSourceChange{GoChange: GoChange{Path: "broken.go"}, Previous: []byte("package broken\n"), Current: []byte("package broken\nfunc {")})
	if err == nil {
		t.Fatal("invalid Go source returned nil error")
	}
	if plan.Class != ChangeUnknown {
		t.Fatalf("parse-error plan = %#v, want UNKNOWN", plan)
	}
	if !plan.Has(ActionReindexProject) {
		t.Fatalf("parse-error plan = %#v, want REINDEX_PROJECT", plan)
	}
}

// TestClassifyRustChangeMatchesWhatTheEngineCanRecompute pins the Rust scopes:
// a body edit reindexes the file, and anything that decides which crates exist
// reindexes the project, because the analyzer's smallest unit is the workspace.
func TestClassifyRustChangeMatchesWhatTheEngineCanRecompute(t *testing.T) {
	body := ClassifyRustChange(RustChange{
		RepositoryKey: "repo", PackageKey: "crate", FileKey: "file", Path: "src/lib.rs",
		Previous: syntax.SyntaxInventory{
			Language: syntax.LanguageRust,
			Candidates: []syntax.SyntaxCandidate{{
				Kind: syntax.CandidateDeclaration, NodeKind: "function_item",
				Name: "run", Signature: "pub fn run()",
			}},
		},
		Current: syntax.SyntaxInventory{
			Language: syntax.LanguageRust,
			Candidates: []syntax.SyntaxCandidate{{
				Kind: syntax.CandidateDeclaration, NodeKind: "function_item",
				Name: "run", Signature: "pub fn run()",
			}},
		},
		ChangedRanges: []syntax.SyntaxRange{{StartByte: 40, EndByte: 52}},
	})
	if body.Language != facts.LanguageRust || body.Class != ChangeBodyOnly {
		t.Fatalf("body plan = %#v", body)
	}
	if !body.Has(ActionReindexFile) || len(body.Actions) != 1 {
		t.Fatalf("body actions = %#v", body.Actions)
	}

	signature := ClassifyRustChange(RustChange{
		RepositoryKey: "repo", Path: "src/lib.rs",
		Previous: syntax.SyntaxInventory{
			Language: syntax.LanguageRust,
			Candidates: []syntax.SyntaxCandidate{{
				Kind: syntax.CandidateDeclaration, NodeKind: "function_item",
				Name: "run", Signature: "pub fn run()",
			}},
		},
		Current: syntax.SyntaxInventory{
			Language: syntax.LanguageRust,
			Candidates: []syntax.SyntaxCandidate{{
				Kind: syntax.CandidateDeclaration, NodeKind: "function_item",
				Name: "run", Signature: "pub fn run(seed: i32)",
			}},
		},
		ChangedRanges: []syntax.SyntaxRange{{StartByte: 10, EndByte: 30}},
	})
	if !signature.Has(ActionInvalidateConsumers) || !signature.Has(ActionResolveReferences) {
		t.Fatalf("signature plan = %#v", signature)
	}

	for name, change := range map[string]RustChange{
		"manifest":     {Path: "crates/engine/Cargo.toml"},
		"lockfile":     {Path: "Cargo.lock"},
		"build script": {Path: "crates/engine/build.rs"},
	} {
		plan := ClassifyRustChange(change)
		if !plan.Has(ActionReindexProject) {
			t.Fatalf("%s plan = %#v, want the whole project reindexed", name, plan)
		}
	}

	deleted := ClassifyRustChange(RustChange{Path: "src/lib.rs", Deleted: true})
	if deleted.Class != ChangeFileDeleted || !deleted.Has(ActionRemoveFile) {
		t.Fatalf("deleted plan = %#v", deleted)
	}
}
