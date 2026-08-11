package facts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/rustloader"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

func rustFixtureInput() RustInput {
	return RustInput{
		Repository: workspace.Repository{Name: "acme/engine", Path: "/repos/engine", RealPath: "/repos/engine"},
		Analysis: rustloader.Analysis{
			Workspace: workspace.CargoWorkspace{ManifestPath: "/repos/engine/Cargo.toml", RootPath: "/repos/engine"},
			Crates: []workspace.CargoCrate{{
				ManifestPath: "/repos/engine/crates/engine/Cargo.toml",
				RootPath:     "/repos/engine/crates/engine",
				Name:         "engine",
				Version:      "1.4.0",
				Edition:      "2021",
			}},
			Files: []rustloader.IndexedFile{{
				Path:  "crates/engine/src/lib.rs",
				Crate: rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
			}},
			Definitions: []rustloader.Definition{{
				StableKey:         "engine-run",
				CanonicalIdentity: "canonical:engine:run",
				Symbol:            "rust-analyzer cargo engine 1.4.0 run().",
				Crate:             rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
				File:              "crates/engine/src/lib.rs",
				Name:              "run",
				QualifiedName:     "run",
				Kind:              string(rustloader.SuffixMethod),
				Exported:          true,
				Signature:         "pub fn run(seed: i32) -> Value",
				StartLine:         4, StartColumn: 7, StartOffset: 60,
				EndLine: 7, EndOffset: 140,
			}},
		},
	}
}

func rustReference(kind rustloader.ReferenceKind, use rustloader.UseKind, target, repository string, offset int) rustloader.Reference {
	return rustloader.Reference{
		SourceKey:        "engine-run",
		TargetKey:        target,
		TargetRepository: repository,
		Kind:             kind,
		Use:              use,
		SourceCrate:      rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
		TargetCrate:      rustloader.CrateRef{Name: "support", Version: "2.0.0"},
		File:             "crates/engine/src/lib.rs",
		StartLine:        5, StartColumn: 8, StartOffset: offset, EndOffset: offset + 6,
		Text: "double",
	}
}

func edgesOfKind(set Set, kind EdgeKind) []Edge {
	edges := make([]Edge, 0, 2)
	for _, edge := range set.Edges {
		if edge.Kind == kind {
			edges = append(edges, edge)
		}
	}
	return edges
}

// TestNormalizeRustBuildsAValidLocalGraph is the floor: a workspace whose
// targets are all its own must produce a set that stands on its own.
func TestNormalizeRustBuildsAValidLocalGraph(t *testing.T) {
	input := rustFixtureInput()
	input.Analysis.Definitions = append(input.Analysis.Definitions, rustloader.Definition{
		StableKey:         "engine-value",
		CanonicalIdentity: "canonical:engine:Value",
		Symbol:            "rust-analyzer cargo engine 1.4.0 Value#",
		Crate:             rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
		File:              "crates/engine/src/lib.rs",
		Name:              "Value", QualifiedName: "Value", Kind: string(rustloader.SuffixType),
		Exported: true, Signature: "pub struct Value",
		StartLine: 1, StartOffset: 10, EndLine: 3, EndOffset: 40,
	})
	local := rustReference(rustloader.ReferenceType, rustloader.UseNone, "engine-value", "", 100)
	local.TargetCrate = rustloader.CrateRef{Name: "engine", Version: "1.4.0"}
	input.Analysis.References = append(input.Analysis.References, local)

	set, report, err := NormalizeRust(context.Background(), input)
	if err != nil {
		t.Fatalf("NormalizeRust() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.EdgesWithoutSource != 0 || report.EdgesWithoutTarget != 0 || report.DefinitionsWithoutCrate != 0 {
		t.Fatalf("report = %#v", report)
	}
	if len(set.Repositories) != 1 || set.Repositories[0].Languages[0] != LanguageRust {
		t.Fatalf("repositories = %#v", set.Repositories)
	}
	if len(set.Packages) != 1 || set.Packages[0].Name != "engine" || set.Packages[0].Version != "1.4.0" {
		t.Fatalf("packages = %#v", set.Packages)
	}
	if set.Packages[0].Container != "." {
		t.Fatalf("package container = %q, want the workspace root", set.Packages[0].Container)
	}
	if len(set.Symbols) != 2 {
		t.Fatalf("symbols = %#v", set.Symbols)
	}
	typeUses := edgesOfKind(set, TypeUses)
	if len(typeUses) != 1 || typeUses[0].Confidence != ExactTypechecked || typeUses[0].Provenance != RustSyntaxType {
		t.Fatalf("type use edges = %#v", typeUses)
	}
	if typeUses[0].EvidenceKey == "" {
		t.Fatal("a reference edge must carry its observation")
	}
}

// TestNormalizeRustClassifiesEveryUseShape pins the relation each syntactic
// form produces, because the analyzer states none of them.
func TestNormalizeRustClassifiesEveryUseShape(t *testing.T) {
	tests := map[string]struct {
		kind       rustloader.ReferenceKind
		use        rustloader.UseKind
		wantEdge   EdgeKind
		wantSource Provenance
	}{
		"call":     {kind: rustloader.ReferenceCall, wantEdge: CallsDirect, wantSource: RustSyntaxCall},
		"type":     {kind: rustloader.ReferenceType, wantEdge: TypeUses, wantSource: RustSyntaxType},
		"plain":    {kind: rustloader.ReferenceUse, wantEdge: References, wantSource: RustAnalyzerUse},
		"import":   {kind: rustloader.ReferenceUse, use: rustloader.UseImport, wantEdge: ImportsSymbol, wantSource: RustAnalyzerUse},
		"reexport": {kind: rustloader.ReferenceUse, use: rustloader.UseReexport, wantEdge: Reexports, wantSource: RustAnalyzerUse},
		// A function named where a value goes. The callback keeps a syntax
		// provenance of its own, as GoASTCallback does; binding and returning
		// carry the class in the edge kind alone.
		"callback": {kind: rustloader.ReferenceCallback, wantEdge: PassesAsCallback, wantSource: RustSyntaxCallback},
		"assign":   {kind: rustloader.ReferenceAssign, wantEdge: AssignsFunction, wantSource: RustAnalyzerUse},
		"return":   {kind: rustloader.ReferenceReturn, wantEdge: ReturnsFunction, wantSource: RustAnalyzerUse},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := rustFixtureInput()
			reference := rustReference(test.kind, test.use, "engine-run", "", 100)
			// A symbol never references itself; point the use at the only
			// other durable identity in the fixture.
			reference.SourceKey = "engine-run"
			reference.TargetKey = "provider-symbol"
			reference.TargetRepository = ""
			input.Analysis.References = append(input.Analysis.References, reference)

			set, _, err := NormalizeRust(context.Background(), input)
			if err != nil {
				t.Fatalf("NormalizeRust() error = %v", err)
			}
			edges := edgesOfKind(set, test.wantEdge)
			if len(edges) != 1 {
				t.Fatalf("%s edges = %#v", test.wantEdge, set.Edges)
			}
			if edges[0].Provenance != test.wantSource || edges[0].Confidence != ExactTypechecked {
				t.Fatalf("edge = %#v", edges[0])
			}
		})
	}
}

// TestNormalizeRustNamesTheProviderOfACrossRepositoryTarget keeps the
// registry-backed identity distinct from a locally resolved one, and proves
// the set only becomes valid once the provider is merged in.
func TestNormalizeRustNamesTheProviderOfACrossRepositoryTarget(t *testing.T) {
	input := rustFixtureInput()
	input.Analysis.References = append(input.Analysis.References,
		rustReference(rustloader.ReferenceCall, rustloader.UseNone, "provider-double", "acme/support", 100))
	input.Analysis.Dependencies = append(input.Analysis.Dependencies, rustloader.CrateDependency{
		SourceCrate:      rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
		TargetCrate:      rustloader.CrateRef{Name: "support", Version: "2.0.0"},
		TargetRepository: "acme/support",
		CrossWorkspace:   true,
		File:             "crates/engine/src/lib.rs",
		StartLine:        5, StartOffset: 100, EndOffset: 106, Text: "double",
	})

	set, _, err := NormalizeRust(context.Background(), input)
	if err != nil {
		t.Fatalf("NormalizeRust() error = %v", err)
	}
	calls := edgesOfKind(set, CallsDirect)
	if len(calls) != 1 || calls[0].Confidence != ExactPackageMapped || calls[0].Provenance != RustSyntaxCall {
		t.Fatalf("call edges = %#v", calls)
	}
	depends := edgesOfKind(set, PackageDependsOn)
	if len(depends) != 1 || depends[0].TargetKey != PackageKey(LanguageRust, "acme/support", "support") {
		t.Fatalf("dependency edges = %#v", depends)
	}
	if depends[0].Confidence != ExactPackageMapped || depends[0].Provenance != RustAnalyzerMoniker {
		t.Fatalf("dependency trust = %#v", depends[0])
	}
	if len(edgesOfKind(set, ModuleDependsOn)) != 1 {
		t.Fatalf("a dependency leaving the workspace must also be a module dependency: %#v", set.Edges)
	}

	err = set.Validate()
	if err == nil {
		t.Fatal("a cross-repository target must dangle until the provider is merged in")
	}
	if !errors.Is(err, ErrInvalidFacts) || !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("Validate() error = %v", err)
	}
}

// TestNormalizeRustKeepsEveryFailureAddressable is what
// get_unresolved_references depends on: a reason with no requested package is
// a fact nobody can act on, and Validate refuses it.
func TestNormalizeRustKeepsEveryFailureAddressable(t *testing.T) {
	input := rustFixtureInput()
	input.Analysis.Unresolved = []rustloader.UnresolvedReference{
		{
			Crate:           rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
			File:            "crates/engine/src/lib.rs",
			SourceKey:       "engine-run",
			RequestedCrate:  "core",
			RequestedSymbol: "ops::arith::mul",
			Reason:          rustloader.UnresolvedCrateProviderNotFound,
			StartLine:       9, StartOffset: 120,
		},
		{
			Reason: rustloader.UnresolvedMacroExpansionDisabled,
			Detail: "the index was built with procedural macro expansion disabled",
		},
	}

	set, report, err := NormalizeRust(context.Background(), input)
	if err != nil {
		t.Fatalf("NormalizeRust() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(set.Unresolved) != 2 || report.UnresolvedWithoutFile != 1 {
		t.Fatalf("unresolved = %#v report = %#v", set.Unresolved, report)
	}
	for _, entry := range set.Unresolved {
		if entry.Language != LanguageRust || entry.RequestedPackage == "" {
			t.Fatalf("unresolved entry = %#v", entry)
		}
	}
	withFile := set.Unresolved[0]
	if withFile.Reason == string(rustloader.UnresolvedCrateProviderNotFound) {
		if withFile.FileKey == "" || withFile.SourceSymbolKey != "engine-run" {
			t.Fatalf("unresolved entry lost its evidence: %#v", withFile)
		}
	}
}

// TestNormalizeRustRefusesToInventAPackage covers an index that names a crate
// no manifest declares: the symbol is dropped and counted, never published
// under a package this pass made up.
func TestNormalizeRustRefusesToInventAPackage(t *testing.T) {
	input := rustFixtureInput()
	input.Analysis.Definitions[0].Crate = rustloader.CrateRef{Name: "ghost", Version: "9.9.9"}

	set, report, err := NormalizeRust(context.Background(), input)
	if err != nil {
		t.Fatalf("NormalizeRust() error = %v", err)
	}
	if report.DefinitionsWithoutCrate != 1 || len(set.Symbols) != 0 {
		t.Fatalf("report = %#v symbols = %#v", report, set.Symbols)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

// TestNormalizeRustPublishesStructuralRelations covers the three relations the
// analyzer refuses to state: they carry the observation that proves them and
// the provenance that says the grammar decided their shape.
func TestNormalizeRustPublishesStructuralRelations(t *testing.T) {
	input := rustFixtureInput()
	input.Analysis.Definitions = append(input.Analysis.Definitions, rustloader.Definition{
		StableKey:         "engine-trait",
		CanonicalIdentity: "canonical:engine:Named",
		Symbol:            "rust-analyzer cargo engine 1.4.0 Named#",
		Crate:             rustloader.CrateRef{Name: "engine", Version: "1.4.0"},
		File:              "crates/engine/src/lib.rs",
		Name:              "Named", QualifiedName: "Named", Kind: "trait",
		Exported: true, Signature: "pub trait Named",
		StartLine: 1, StartOffset: 0, EndLine: 3, EndOffset: 30,
	})
	input.Analysis.Relations = []rustloader.Relation{
		{
			Kind: rustloader.RelationImplements, SourceKey: "engine-run", TargetKey: "engine-trait",
			File: "crates/engine/src/lib.rs", StartLine: 10, StartColumn: 5, StartOffset: 200, EndOffset: 205,
			Text: "Named",
		},
		{
			Kind: rustloader.RelationExtends, SourceKey: "engine-trait", TargetKey: "provider-trait",
			TargetRepository: "acme/support",
			File:             "crates/engine/src/lib.rs", StartLine: 11, StartOffset: 220, EndOffset: 226,
			Text: "Shared",
		},
		{
			Kind: rustloader.RelationOverrides, SourceKey: "engine-run", TargetKey: "engine-trait",
			File: "crates/engine/src/lib.rs", StartLine: 12, StartOffset: 240, EndOffset: 244,
			Text: "name",
		},
	}

	set, report, err := NormalizeRust(context.Background(), input)
	if err != nil {
		t.Fatalf("NormalizeRust() error = %v", err)
	}
	if report.EdgesWithoutSource != 0 || report.EdgesWithoutTarget != 0 {
		t.Fatalf("report = %#v", report)
	}
	for kind, want := range map[EdgeKind]Confidence{
		Implements: ExactTypechecked,
		Extends:    ExactPackageMapped,
		Overrides:  ExactTypechecked,
	} {
		edges := edgesOfKind(set, kind)
		if len(edges) != 1 {
			t.Fatalf("%s edges = %#v", kind, set.Edges)
		}
		if edges[0].Confidence != want || edges[0].Provenance != RustSyntaxImplementation {
			t.Fatalf("%s edge = %#v", kind, edges[0])
		}
		if edges[0].EvidenceKey == "" {
			t.Fatalf("%s edge carries no observation", kind)
		}
	}
}
