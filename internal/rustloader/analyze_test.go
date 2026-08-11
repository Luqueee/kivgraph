package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/syntax"
	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// analyzeFixture runs the whole Rust path over the recorded workspace: the
// external analyzer, the decoder, the grammar and the crate registry.
func analyzeFixture(t *testing.T) Analysis {
	t.Helper()
	requireAnalyzer(t)
	root, output := analyzerFixture(t)
	repository := workspace.Repository{Name: "fixture", Path: root, RealPath: root}

	discovery, err := workspace.DiscoverCargo(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	if len(discovery.Workspaces) != 1 {
		t.Fatalf("workspaces = %#v", discovery.Workspaces)
	}
	result, err := Run(context.Background(), defaultRunOptions(root, output))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	registry, err := NewCrateRegistry(context.Background(), []workspace.Repository{repository})
	if err != nil {
		t.Fatalf("NewCrateRegistry() error = %v", err)
	}
	parsers, err := syntax.NewParserManager(2)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	t.Cleanup(func() { parsers.Close() })

	analysis, err := Analyze(context.Background(), AnalyzeOptions{
		Repository:   repository,
		Workspace:    discovery.Workspaces[0],
		Crates:       discovery.Crates,
		Index:        result.Index,
		Registry:     registry,
		Parsers:      parsers,
		ProcMacros:   true,
		BuildScripts: true,
		Diagnostics:  result.Diagnostics,
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return analysis
}

func definitionNamed(t *testing.T, analysis Analysis, qualified string) Definition {
	t.Helper()
	for _, definition := range analysis.Definitions {
		if definition.QualifiedName == qualified {
			return definition
		}
	}
	t.Fatalf("analysis has no definition %q", qualified)
	return Definition{}
}

// TestAnalyzeBuildsDurableDefinitions checks the identity, visibility and span
// of what the analyzer indexed: the three things every Rust symbol carries
// into the graph.
func TestAnalyzeBuildsDurableDefinitions(t *testing.T) {
	analysis := analyzeFixture(t)

	double := definitionNamed(t, analysis, "double")
	if double.Crate.Name != "support" || double.Crate.Version != "1.4.0" {
		t.Fatalf("double crate = %#v", double.Crate)
	}
	// The published kind is the fine grained one: every `#` suffix would
	// otherwise read as "type", and every `()` as "method".
	if double.Kind != "function" || !double.Exported {
		t.Fatalf("double = %#v, want an exported function", double)
	}
	if double.Signature != "pub fn double(value: i32) -> i32" {
		t.Fatalf("double signature = %q", double.Signature)
	}
	if double.File != "crates/support/src/lib.rs" || double.StartLine != 11 {
		t.Fatalf("double location = %s:%d", double.File, double.StartLine)
	}
	if double.EndOffset <= double.StartOffset {
		t.Fatalf("double span = %d..%d, want the declaration body", double.StartOffset, double.EndOffset)
	}
	if double.StableKey == "" || double.CanonicalIdentity == "" {
		t.Fatalf("double identity = %#v", double)
	}

	// Visibility is read from the grammar: SCIP carries none, and a private
	// item that looked exported would be a wrong public API.
	private := definitionNamed(t, analysis, "private_helper")
	if private.Exported {
		t.Fatalf("private_helper = %#v, want unexported", private)
	}

	// Two crates declare no symbol with the same key.
	keys := make(map[string]string, len(analysis.Definitions))
	for _, definition := range analysis.Definitions {
		if previous, exists := keys[string(definition.StableKey)]; exists {
			t.Fatalf("definitions %q and %q share a stable key", previous, definition.Symbol)
		}
		keys[string(definition.StableKey)] = definition.Symbol
	}
}

// TestAnalyzeAttributesUsesToTheirDeclaration is the contract the analyzer
// does not state: SCIP says which symbol a use resolves to, never which
// declaration contains it.
func TestAnalyzeAttributesUsesToTheirDeclaration(t *testing.T) {
	analysis := analyzeFixture(t)

	run := definitionNamed(t, analysis, "run")
	helperUser := definitionNamed(t, analysis, "helper_user")
	double := definitionNamed(t, analysis, "double")
	private := definitionNamed(t, analysis, "private_helper")
	value := definitionNamed(t, analysis, "Value")

	var call, typeUse, importBinding, localCall bool
	for _, reference := range analysis.References {
		switch {
		case reference.SourceKey == string(run.StableKey) &&
			reference.TargetKey == string(double.StableKey) &&
			reference.Kind == ReferenceCall:
			call = true
		case reference.SourceKey == string(run.StableKey) &&
			reference.TargetKey == string(value.StableKey) &&
			reference.Kind == ReferenceType:
			typeUse = true
		case reference.Use == UseImport && reference.TargetKey == string(double.StableKey):
			importBinding = true
		case reference.SourceKey == string(helperUser.StableKey) &&
			reference.TargetKey == string(private.StableKey) &&
			reference.Kind == ReferenceCall:
			localCall = true
		}
	}
	if !call {
		t.Fatalf("no call from run to double in %#v", analysis.References)
	}
	if !typeUse {
		t.Fatal("the return type of run was not classified as a type use")
	}
	if !importBinding {
		t.Fatal("the `use support::double` binding was not classified as an import")
	}
	if !localCall {
		t.Fatal("the call from helper_user to private_helper was not attributed")
	}
}

// TestAnalyzeRecordsCrateDependenciesFromRealUses keeps a dependency edge tied
// to an observed occurrence rather than to a manifest entry nobody uses.
func TestAnalyzeRecordsCrateDependenciesFromRealUses(t *testing.T) {
	analysis := analyzeFixture(t)

	if len(analysis.Dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want engine -> support only", analysis.Dependencies)
	}
	dependency := analysis.Dependencies[0]
	if dependency.SourceCrate.Name != "engine" || dependency.TargetCrate.Name != "support" {
		t.Fatalf("dependency = %#v", dependency)
	}
	if dependency.TargetRepository != "" || dependency.CrossWorkspace {
		t.Fatalf("dependency = %#v, want it inside this workspace", dependency)
	}
	if dependency.File == "" || dependency.EndOffset <= dependency.StartOffset {
		t.Fatalf("dependency evidence = %#v", dependency)
	}
}

// TestAnalyzeDeclaresWhatItCouldNotResolve covers the standard library, which
// no registered repository provides: its uses must be declared, never dropped
// and never turned into an edge.
func TestAnalyzeDeclaresWhatItCouldNotResolve(t *testing.T) {
	analysis := analyzeFixture(t)

	found := false
	for _, entry := range analysis.Unresolved {
		if entry.Reason != UnresolvedCrateProviderNotFound {
			continue
		}
		if entry.RequestedCrate == "core" || entry.RequestedCrate == "std" {
			found = true
			if entry.RequestedSymbol == "" || entry.File == "" {
				t.Fatalf("unresolved entry = %#v, want the occurrence it was seen at", entry)
			}
		}
	}
	if !found {
		t.Fatalf("unresolved = %#v, want the sysroot crate declared", analysis.Unresolved)
	}
	for _, entry := range analysis.Unresolved {
		if entry.Reason == UnresolvedTargetNotBuildable {
			t.Fatalf("both crates are indexed, so %#v must not be reported", entry)
		}
	}
}

// TestAnalyzeIgnoresFilesOutsideTheRepository keeps sysroot sources out of the
// graph: they are read to resolve a symbol, not indexed as repository files.
func TestAnalyzeIgnoresFilesOutsideTheRepository(t *testing.T) {
	analysis := analyzeFixture(t)

	for _, file := range analysis.Files {
		if filepath.IsAbs(file.Path) || strings.HasPrefix(file.Path, "..") {
			t.Fatalf("file %q is not repository relative", file.Path)
		}
		if file.Crate.Name == "" {
			t.Fatalf("file %q belongs to no crate", file.Path)
		}
	}
	if len(analysis.Files) != 3 {
		t.Fatalf("files = %#v, want the three crate sources", analysis.Files)
	}
}

// TestAnalyzeDerivesStructuralRelations covers what SCIP refuses to say:
// `relationships` travels empty, so an implementation, a supertrait and an
// override are read from the shape of the header with ends the analyzer
// resolved.
func TestAnalyzeDerivesStructuralRelations(t *testing.T) {
	analysis := analyzeFixture(t)

	circle := definitionNamed(t, analysis, "shapes::Circle")
	named := definitionNamed(t, analysis, "shapes::Named")
	drawable := definitionNamed(t, analysis, "shapes::Drawable")
	traitMethod := definitionNamed(t, analysis, "shapes::Named::name")

	var implementsNamed, implementsDrawable, extendsNamed, overridesName bool
	for _, relation := range analysis.Relations {
		switch {
		case relation.Kind == RelationImplements &&
			relation.SourceKey == string(circle.StableKey) &&
			relation.TargetKey == string(named.StableKey):
			implementsNamed = true
		case relation.Kind == RelationImplements &&
			relation.SourceKey == string(circle.StableKey) &&
			relation.TargetKey == string(drawable.StableKey):
			implementsDrawable = true
		case relation.Kind == RelationExtends &&
			relation.SourceKey == string(drawable.StableKey) &&
			relation.TargetKey == string(named.StableKey):
			extendsNamed = true
		case relation.Kind == RelationOverrides &&
			relation.TargetKey == string(traitMethod.StableKey):
			overridesName = true
		}
	}
	if !implementsNamed || !implementsDrawable {
		t.Fatalf("implementations missing in %#v", analysis.Relations)
	}
	if !extendsNamed {
		t.Fatal("the supertrait of Drawable was not derived")
	}
	if !overridesName {
		t.Fatal("the trait method a implementation answers for was not derived")
	}
	for _, relation := range analysis.Relations {
		if relation.File == "" || relation.EndOffset <= relation.StartOffset {
			t.Fatalf("relation without evidence: %#v", relation)
		}
	}

	// An inherent implementation relates a type to no trait, so `new` must
	// not override anything.
	inherent := definitionNamed(t, analysis, "shapes::impl::Circle::new")
	for _, relation := range analysis.Relations {
		if relation.Kind == RelationOverrides && relation.SourceKey == string(inherent.StableKey) {
			t.Fatalf("an inherent method overrides nothing: %#v", relation)
		}
	}
}

// TestAnalyzePublishesFineGrainedKinds is what makes a query answerable: the
// descriptor suffix calls a struct, an enum and a trait the same thing.
func TestAnalyzePublishesFineGrainedKinds(t *testing.T) {
	analysis := analyzeFixture(t)

	want := map[string]string{
		"Value":                     "struct",
		"Value::inner":              "field",
		"double":                    "function",
		"shapes::Named":             "trait",
		"shapes::Named::name":       "trait_method",
		"shapes::Drawable":          "trait",
		"crate":                     "module",
		"shapes::impl::Circle::new": "static_method",
	}
	for qualified, kind := range want {
		definition := definitionNamed(t, analysis, qualified)
		if definition.Kind != kind {
			t.Fatalf("%s kind = %q, want %q", qualified, definition.Kind, kind)
		}
	}
}

// TestAnalyzeDeclaresTheImplementationBlockItCannotDefine is a regression
// test for the shape every Rust crate uses: `-> Self` names the `impl` block,
// which SCIP mentions in occurrences and never defines. Naming a key nobody
// publishes made the whole pass fail validation with a dangling edge.
func TestAnalyzeDeclaresTheImplementationBlockItCannotDefine(t *testing.T) {
	analysis := analyzeFixture(t)

	published := make(map[string]struct{}, len(analysis.Definitions))
	for _, definition := range analysis.Definitions {
		published[string(definition.StableKey)] = struct{}{}
	}
	for _, reference := range analysis.References {
		if reference.TargetRepository != "" {
			continue
		}
		if _, exists := published[reference.TargetKey]; !exists {
			t.Fatalf("reference names a key this pass does not publish: %#v", reference)
		}
	}
	for _, relation := range analysis.Relations {
		if relation.TargetRepository != "" {
			continue
		}
		if _, exists := published[relation.TargetKey]; !exists {
			t.Fatalf("relation names a key this pass does not publish: %#v", relation)
		}
	}

	declared := false
	for _, entry := range analysis.Unresolved {
		if entry.Reason == UnresolvedDefinitionNotIndexed && entry.RequestedSymbol == "shapes::impl::Circle" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("unresolved = %#v, want the implementation block declared", analysis.Unresolved)
	}
}

// TestAnalyzeReadsFunctionValues is the end to end half of the value-position
// contract: the grammar decides the class, the analyzer resolves the target,
// and a target that is not callable keeps the plain relation.
func TestAnalyzeReadsFunctionValues(t *testing.T) {
	requireAnalyzer(t)
	root := testsupport.TempDir(t)
	source := filepath.Join(root, "workspace")
	output := filepath.Join(root, "out")
	if err := os.CopyFS(source, os.DirFS(filepath.Join("..", "..", "testdata", "rust", "function-values"))); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", output, err)
	}
	repository := workspace.Repository{Name: "values", Path: source, RealPath: source}
	discovery, err := workspace.DiscoverCargo(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	result, err := Run(context.Background(), defaultRunOptions(source, output))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	registry, err := NewCrateRegistry(context.Background(), []workspace.Repository{repository})
	if err != nil {
		t.Fatalf("NewCrateRegistry() error = %v", err)
	}
	parsers, err := syntax.NewParserManager(2)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	t.Cleanup(func() { parsers.Close() })
	analysis, err := Analyze(context.Background(), AnalyzeOptions{
		Repository: repository, Workspace: discovery.Workspaces[0], Crates: discovery.Crates,
		Index: result.Index, Registry: registry, Parsers: parsers,
		ProcMacros: true, BuildScripts: true, Diagnostics: result.Diagnostics,
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	names := map[string]string{}
	for _, definition := range analysis.Definitions {
		names[string(definition.StableKey)] = definition.QualifiedName
	}
	observed := map[string]ReferenceKind{}
	for _, reference := range analysis.References {
		observed[names[reference.SourceKey]+" -> "+names[reference.TargetKey]] = reference.Kind
	}

	for edge, want := range map[string]ReferenceKind{
		"passes_double -> double":      ReferenceCallback,
		"passes_double -> apply":       ReferenceCall,
		"binds_double -> double":       ReferenceAssign,
		"picks_double -> double":       ReferenceReturn,
		"returns_explicitly -> double": ReferenceReturn,
		"passes_limit -> takes_limit":  ReferenceCall,
		// A constant in the same shapes is not a function travelling.
		"passes_limit -> LIMIT": ReferenceUse,
		"binds_limit -> LIMIT":  ReferenceUse,
	} {
		if got, exists := observed[edge]; !exists {
			t.Errorf("analysis has no reference %q; observed %v", edge, observed)
		} else if got != want {
			t.Errorf("reference %q = %q, want %q", edge, got, want)
		}
	}
}
