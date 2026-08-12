package facts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/workspace"
)

var typeScriptGolden = filepath.Join("..", "..", "testdata", "protocol", "ts-facts-v4")

func loadPayload(t *testing.T, name string) TypeScriptPayload {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(typeScriptGolden, name))
	if err != nil {
		t.Fatalf("read golden payload: %v", err)
	}
	payload, err := DecodeTypeScriptPayload(data)
	if err != nil {
		t.Fatalf("DecodeTypeScriptPayload() error = %v", err)
	}
	return payload
}

// TestNormalizeTypeScriptConsumesRealWorkerOutput uses payloads produced by
// `pnpm facts` in ts-worker, so the wire contract is checked against the code
// that emits it and not against a hand written sample.
func TestNormalizeTypeScriptConsumesRealWorkerOutput(t *testing.T) {
	payload := loadPayload(t, "shared-library.json")
	root := filepath.Join("/repositories", "shared-library")

	set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: root})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if len(set.Repositories) != 1 || set.Repositories[0].Name != "shared-library" {
		t.Fatalf("repositories = %#v", set.Repositories)
	}
	if len(set.Packages) != 1 || set.Packages[0].Name != "@ladygraph-fixture/shared" {
		t.Fatalf("packages = %#v", set.Packages)
	}
	if set.Packages[0].RootPath != root {
		t.Fatalf("package root = %q, want the caller supplied root", set.Packages[0].RootPath)
	}

	byQualifiedName := make(map[string]Symbol, len(set.Symbols))
	byFileAndQualifiedName := make(map[string]string, len(set.Symbols))
	for _, symbol := range set.Symbols {
		if symbol.Key == "" || symbol.CanonicalIdentity == "" {
			t.Fatalf("symbol without identity: %#v", symbol)
		}
		if symbol.Language != LanguageTypeScript {
			t.Fatalf("symbol language = %q", symbol.Language)
		}
		byQualifiedName[symbol.QualifiedName] = symbol
		// Every (file, qualifiedName) pair must resolve to exactly one key:
		// an "export" symbol sharing its declaration's own name (the common
		// case for a direct, unrenamed export) must never collide with it.
		fileAndName := symbol.FileKey + "\x00" + symbol.QualifiedName
		if existing, seen := byFileAndQualifiedName[fileAndName]; seen && existing != symbol.Key {
			t.Fatalf("two different symbols share file+qualifiedName %q: %s and %s", fileAndName, existing, symbol.Key)
		}
		byFileAndQualifiedName[fileAndName] = symbol.Key
	}
	for _, name := range []string{"value", "compute", "helper", "Named", "NamedShape", "Widget"} {
		if _, declared := byQualifiedName[name]; !declared {
			t.Fatalf("symbol %q missing: %v", name, byQualifiedName)
		}
	}

	references, reexports := 0, 0
	for _, edge := range set.Edges {
		switch edge.Kind {
		case Defines:
			if edge.Provenance != TypeScriptChecker {
				t.Fatalf("DEFINES edge = %#v", edge)
			}
		case ContainsPackage, ContainsFile:
		default:
			references++
			if edge.Kind == Reexports || edge.Kind == Exports {
				reexports++
			}
			if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
				t.Fatalf("reference edge = %#v", edge)
			}
		}
	}
	if references == 0 {
		t.Fatalf("no local reference edge was produced: %#v", set.Edges)
	}
	// `src/index.ts` re-exports `aliasedHelper`, and star re-exports
	// `value`, `Shape`, `compute`, `Named`, `NamedShape` and `Widget`;
	// `src/helper.ts` exports its own declaration directly, `src/value.ts`
	// exports `value`, `Shape` and `compute` directly, and `src/inheritance.
	// ts` exports `Named`, `NamedShape` and `Widget` directly.
	if reexports != 14 {
		t.Fatalf("EXPORTS/REEXPORTS edges = %d, want 14: %#v", reexports, set.Edges)
	}
	if report.EdgesWithoutTarget != 0 {
		t.Fatalf("dropped targets = %d", report.EdgesWithoutTarget)
	}
	if report.ExportsWithoutTarget != 0 {
		t.Fatalf("dropped export targets = %d", report.ExportsWithoutTarget)
	}
	if report.ExtendsWithoutTarget != 0 {
		t.Fatalf("dropped extends targets = %d", report.ExtendsWithoutTarget)
	}
}

// TestNormalizeTypeScriptImportsSymbolTargetKeyMatchesProvider is the
// LUQUE-0907 acceptance test: the consumer must derive, for the destination
// of an IMPORTS_SYMBOL edge, exactly the stable key the provider assigns its
// own declaration when it normalises itself. It exercises the real
// cross-repository fixture of LUQUE-0707 through real `pnpm facts` output on
// both sides, not a hand written payload — the whole point is that two
// independently normalised repositories agree on one key, which a single
// hand written payload could never exercise.
func TestNormalizeTypeScriptImportsSymbolTargetKeyMatchesProvider(t *testing.T) {
	ctx := context.Background()

	providerPayload := loadPayload(t, "shared-library.json")
	providerSet, _, err := NormalizeTypeScript(ctx, providerPayload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(shared-library) error = %v", err)
	}

	consumerPayload := loadPayload(t, "consumer-a.json")
	consumerSet, consumerReport, err := NormalizeTypeScript(ctx, consumerPayload, workspace.Repository{RealPath: "/repositories/consumer-a"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-a) error = %v", err)
	}
	if consumerReport.ImportsWithoutTarget != 0 {
		t.Fatalf("consumer-a dropped %d imports without a target, want 0: every import in "+
			"this fixture resolves through a declaration map", consumerReport.ImportsWithoutTarget)
	}

	var importEdges []Edge
	for _, edge := range consumerSet.Edges {
		if edge.Kind == ImportsSymbol {
			importEdges = append(importEdges, edge)
		}
	}
	if len(importEdges) == 0 {
		t.Fatalf("no IMPORTS_SYMBOL edge was produced: %#v", consumerSet.Edges)
	}

	// A consumer Set alone can never validate: an IMPORTS_SYMBOL edge names a
	// symbol the provider owns, which this Set does not contain. That is the
	// documented, intentional shape of a single repository Set — the edge
	// only closes once the provider's Set is merged in below.
	if err := consumerSet.Validate(); err == nil {
		t.Fatalf("Validate() on the consumer alone unexpectedly passed: an IMPORTS_SYMBOL " +
			"edge should dangle until the provider is merged in")
	}

	// Looked up by (file, qualifiedName), not by qualifiedName alone:
	// shared-library's own "compute"/"value"/"Shape" exports (from
	// src/index.ts's star re-export) share these same bare names, so a
	// qualifiedName-only map would resolve ambiguously.
	for _, name := range []string{"compute", "value", "Shape"} {
		providerSymbol := findSymbol(t, providerSet.Symbols, "shared-library", "src/value.ts", name)
		matched := false
		for _, edge := range importEdges {
			if edge.TargetKey == providerSymbol.Key {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("no IMPORTS_SYMBOL edge targets the provider's own key for %q (%s); "+
				"consumer and provider derived different identities for the same declaration",
				name, providerSymbol.Key)
		}
	}

	merged := providerSet
	merged.Merge(consumerSet)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() on the merged graph = %v", err)
	}
}

// TestNormalizeTypeScriptImportsSymbolTargetKeyMatchesProviderThroughAlias
// repeats the acceptance test over consumer-b, whose "helper" binding
// renames the provider's exported "aliasedHelper", and whose "compute"
// binding is read off a namespace import (`shared.compute`), never spelled
// as a named import at all. The provider's stable key is derived from
// Target alone, never from the consumer's own naming or binding shape, so
// aliasing and reading through a namespace member must not change the
// outcome. `republished` — a re-export, not an import — is covered by
// TestNormalizeTypeScriptReexportsTargetKeyMatchesProvider instead.
func TestNormalizeTypeScriptImportsSymbolTargetKeyMatchesProviderThroughAlias(t *testing.T) {
	ctx := context.Background()

	providerPayload := loadPayload(t, "shared-library.json")
	providerSet, _, err := NormalizeTypeScript(ctx, providerPayload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(shared-library) error = %v", err)
	}

	consumerPayload := loadPayload(t, "consumer-b.json")
	consumerSet, consumerReport, err := NormalizeTypeScript(ctx, consumerPayload, workspace.Repository{RealPath: "/repositories/consumer-b"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-b) error = %v", err)
	}
	if consumerReport.ImportsWithoutTarget != 0 {
		t.Fatalf("consumer-b dropped %d imports without a target, want 0", consumerReport.ImportsWithoutTarget)
	}

	consumerKeyToLocalName := make(map[string]string, len(consumerSet.Symbols))
	for _, symbol := range consumerSet.Symbols {
		consumerKeyToLocalName[symbol.Key] = symbol.QualifiedName
	}
	targetKeyByLocalName := make(map[string]string, len(consumerSet.Edges))
	for _, edge := range consumerSet.Edges {
		if edge.Kind != ImportsSymbol {
			continue
		}
		if localName, declared := consumerKeyToLocalName[edge.SourceKey]; declared {
			targetKeyByLocalName[localName] = edge.TargetKey
		}
	}

	// "helper" is consumer-b's renamed binding of the provider's exported
	// "aliasedHelper" (backed by the src/helper.ts declaration named
	// "helper"); "compute" is read off `shared.compute`, a namespace member
	// access that never spells a named import at all. Neither binding
	// shape matches the provider's own qualified name or declaration
	// shape, which is exactly the case this ticket exists for. Looked up
	// by (file, qualifiedName): shared-library's own re-exports share
	// these same bare names in other files (src/index.ts), so a
	// qualifiedName-only map would resolve ambiguously.
	cases := []struct {
		localName             string
		providerFile          string
		providerQualifiedName string
	}{
		{"helper", "src/helper.ts", "helper"},
		{"compute", "src/value.ts", "compute"},
	}
	for _, testCase := range cases {
		targetKey, hasEdge := targetKeyByLocalName[testCase.localName]
		if !hasEdge {
			t.Fatalf("no IMPORTS_SYMBOL edge from consumer-b binding %q: %#v", testCase.localName, consumerSet.Edges)
		}
		providerSymbol := findSymbol(t, providerSet.Symbols, "shared-library", testCase.providerFile, testCase.providerQualifiedName)
		if targetKey != providerSymbol.Key {
			t.Fatalf("consumer-b binding %q targets %s, want the provider's key for %q (%s)",
				testCase.localName, targetKey, testCase.providerQualifiedName, providerSymbol.Key)
		}
	}

	merged := providerSet
	merged.Merge(consumerSet)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() on the merged graph = %v", err)
	}
}

// findSymbol looks up the one symbol declared at (repository, file,
// qualifiedName) — the same compound key NormalizeTypeScript itself
// resolves reference and import targets by, keyed on the file instead.
func findSymbol(t *testing.T, symbols []Symbol, repository, file, qualifiedName string) Symbol {
	t.Helper()
	fileKey := FileKey(repository, file)
	for _, symbol := range symbols {
		if symbol.FileKey == fileKey && symbol.QualifiedName == qualifiedName {
			return symbol
		}
	}
	t.Fatalf("no symbol %s#%s among %d symbols", file, qualifiedName, len(symbols))
	return Symbol{}
}

// TestNormalizeTypeScriptReexportsTargetKeyMatchesProvider is the REEXPORTS
// half of the LUQUE-0907 acceptance test: consumer-b's
// `export { value as republished } from "@ladygraph-fixture/shared"` crosses
// into another repository through a `from` clause, so it must derive the
// exact same provider-source key an IMPORTS_SYMBOL edge would — REEXPORTS
// is a different edge kind, never a weaker proof.
func TestNormalizeTypeScriptReexportsTargetKeyMatchesProvider(t *testing.T) {
	ctx := context.Background()

	providerPayload := loadPayload(t, "shared-library.json")
	providerSet, _, err := NormalizeTypeScript(ctx, providerPayload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(shared-library) error = %v", err)
	}

	consumerPayload := loadPayload(t, "consumer-b.json")
	consumerSet, _, err := NormalizeTypeScript(ctx, consumerPayload, workspace.Repository{RealPath: "/repositories/consumer-b"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-b) error = %v", err)
	}

	republished := findSymbol(t, consumerSet.Symbols, "consumer-b", "src/barrel.ts", "republished")
	value := findSymbol(t, providerSet.Symbols, "shared-library", "src/value.ts", "value")

	var reexportEdges []Edge
	for _, edge := range consumerSet.Edges {
		if edge.Kind == Reexports && edge.SourceKey == republished.Key {
			reexportEdges = append(reexportEdges, edge)
		}
	}
	if len(reexportEdges) != 1 {
		t.Fatalf("REEXPORTS edges from %q = %d, want 1: %#v", "republished", len(reexportEdges), consumerSet.Edges)
	}
	if reexportEdges[0].TargetKey != value.Key {
		t.Fatalf("republished targets %s, want the provider's key for value (%s)",
			reexportEdges[0].TargetKey, value.Key)
	}
	if !reexportEdges[0].Confidence.Exact() || reexportEdges[0].EvidenceKey == "" {
		t.Fatalf("REEXPORTS edge = %#v", reexportEdges[0])
	}

	// A consumer Set alone can never validate: the REEXPORTS edge names a
	// symbol the provider owns. It only closes once the provider is merged.
	if err := consumerSet.Validate(); err == nil {
		t.Fatalf("Validate() on consumer-b alone unexpectedly passed: a REEXPORTS " +
			"edge should dangle until the provider is merged in")
	}
	merged := providerSet
	merged.Merge(consumerSet)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() on the merged graph = %v", err)
	}
}

// TestNormalizeTypeScriptLocalReexportsResolveWithinOneRepository covers
// shared-library's own src/index.ts, whose `export { helper as
// aliasedHelper } from "./helper.js"` and `export * from "./value.js"` both
// re-export declarations of this same repository. Unlike a cross-repository
// REEXPORTS edge, these resolve entirely within shared-library's own Set —
// no merge is needed, because the target was never a foreign package.
func TestNormalizeTypeScriptLocalReexportsResolveWithinOneRepository(t *testing.T) {
	payload := loadPayload(t, "shared-library.json")
	set, _, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	cases := []struct {
		exportedName        string
		targetFile          string
		targetQualifiedName string
	}{
		{"aliasedHelper", "src/helper.ts", "helper"},
		{"value", "src/value.ts", "value"},
		{"Shape", "src/value.ts", "Shape"},
		{"compute", "src/value.ts", "compute"},
	}
	for _, testCase := range cases {
		source := findSymbol(t, set.Symbols, "shared-library", "src/index.ts", testCase.exportedName)
		target := findSymbol(t, set.Symbols, "shared-library", testCase.targetFile, testCase.targetQualifiedName)
		matched := false
		for _, edge := range set.Edges {
			if edge.Kind == Reexports && edge.SourceKey == source.Key && edge.TargetKey == target.Key {
				matched = true
				if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
					t.Fatalf("REEXPORTS edge for %q = %#v", testCase.exportedName, edge)
				}
				break
			}
		}
		if !matched {
			t.Fatalf("no REEXPORTS edge from src/index.ts#%s (%s) to %s#%s (%s)",
				testCase.exportedName, source.Key, testCase.targetFile, testCase.targetQualifiedName, target.Key)
		}
	}
}

// TestNormalizeTypeScriptDirectExportProducesExportsEdge covers
// consumer-a's `export function total(...)`: a declaration exported
// directly, with no `from` clause, must produce EXPORTS — never REEXPORTS —
// from its own public name to itself.
func TestNormalizeTypeScriptDirectExportProducesExportsEdge(t *testing.T) {
	payload := loadPayload(t, "consumer-a.json")
	set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-a"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if report.ExportsWithoutTarget != 0 {
		t.Fatalf("dropped export targets = %d", report.ExportsWithoutTarget)
	}

	exportSymbol := findSymbol(t, set.Symbols, "consumer-a", "src/direct.ts", "total#2")
	if exportSymbol.Kind != "export" {
		t.Fatalf("total's public name symbol kind = %q, want %q", exportSymbol.Kind, "export")
	}
	declaration := findSymbol(t, set.Symbols, "consumer-a", "src/direct.ts", "total")
	if declaration.Kind != "function" {
		t.Fatalf("total's declaration kind = %q, want %q", declaration.Kind, "function")
	}

	matched := false
	for _, edge := range set.Edges {
		if edge.Kind == Exports && edge.SourceKey == exportSymbol.Key {
			matched = true
			if edge.TargetKey != declaration.Key {
				t.Fatalf("EXPORTS edge targets %s, want total's own key %s", edge.TargetKey, declaration.Key)
			}
			if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
				t.Fatalf("EXPORTS edge = %#v", edge)
			}
		}
		if edge.Kind == Reexports && edge.SourceKey == exportSymbol.Key {
			t.Fatalf("a direct export must never produce REEXPORTS: %#v", edge)
		}
	}
	if !matched {
		t.Fatalf("no EXPORTS edge from total's public name: %#v", set.Edges)
	}
}

// TestNormalizeTypeScriptReferenceTargetsImportBinding covers consumer-b's
// `export const used = helper;`: since LUQUE-0907 already turns the "helper"
// import binding into its own emitted symbol, a use that never resolves to
// a genuine local declaration — because the alias leads to another
// repository — must still produce a REFERENCES edge to that binding.
func TestNormalizeTypeScriptReferenceTargetsImportBinding(t *testing.T) {
	payload := loadPayload(t, "consumer-b.json")
	set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-b"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if report.EdgesWithoutTarget != 0 || report.EdgesWithoutSource != 0 {
		t.Fatalf("dropped reference edges: withoutTarget=%d withoutSource=%d", report.EdgesWithoutTarget, report.EdgesWithoutSource)
	}

	used := findSymbol(t, set.Symbols, "consumer-b", "src/barrel.ts", "used")
	helper := findSymbol(t, set.Symbols, "consumer-b", "src/barrel.ts", "helper")
	if helper.Kind != "import" {
		t.Fatalf("helper's kind = %q, want %q", helper.Kind, "import")
	}

	matched := false
	for _, edge := range set.Edges {
		if edge.Kind == References && edge.SourceKey == used.Key && edge.TargetKey == helper.Key {
			matched = true
			if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
				t.Fatalf("REFERENCES edge = %#v", edge)
			}
		}
	}
	if !matched {
		t.Fatalf("no REFERENCES edge from %q to the import binding %q: %#v", "used", "helper", set.Edges)
	}
}

// TestNormalizeTypeScriptMergedRepositoriesValidate merges the whole
// three-repository fixture — shared-library, consumer-a and consumer-b — and
// requires the combined graph to validate with no dangling edge: every
// IMPORTS_SYMBOL, REEXPORTS, EXTENDS and PACKAGE_DEPENDS_ON edge produced by
// any of the three must close against a symbol or package one of the other
// two declares.
func TestNormalizeTypeScriptMergedRepositoriesValidate(t *testing.T) {
	ctx := context.Background()

	sharedSet, _, err := NormalizeTypeScript(ctx, loadPayload(t, "shared-library.json"), workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(shared-library) error = %v", err)
	}
	consumerASet, _, err := NormalizeTypeScript(ctx, loadPayload(t, "consumer-a.json"), workspace.Repository{RealPath: "/repositories/consumer-a"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-a) error = %v", err)
	}
	consumerBSet, _, err := NormalizeTypeScript(ctx, loadPayload(t, "consumer-b.json"), workspace.Repository{RealPath: "/repositories/consumer-b"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-b) error = %v", err)
	}

	merged := sharedSet
	merged.Merge(consumerASet)
	merged.Merge(consumerBSet)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() on the merged three-repository graph = %v", err)
	}

	kinds := make(map[EdgeKind]int)
	for _, edge := range merged.Edges {
		kinds[edge.Kind]++
	}
	for _, kind := range []EdgeKind{ImportsSymbol, Exports, Reexports, References, Extends, PackageDependsOn} {
		if kinds[kind] == 0 {
			t.Fatalf("edge kind %q missing from the merged graph: %#v", kind, kinds)
		}
	}
	// TypeScript has no module concept distinct from its package: it must
	// never emit MODULE_DEPENDS_ON, only Go's package/module split does.
	if kinds[ModuleDependsOn] != 0 {
		t.Fatalf("TypeScript must never emit MODULE_DEPENDS_ON: %#v", kinds)
	}
}

// TestNormalizeTypeScriptImportWithoutTargetIsUnresolved covers an import
// binding whose declaration map produced no exact provider target: it must
// become an UnresolvedReference, never an edge, since the provider's class
// and signature — and therefore its stable key — are not knowable. It also
// keeps payload.Unresolved's own pass-through working: a package-level
// failure (no provider registered at all, so no binding symbol ever exists)
// and a binding-level one must coexist as two distinct facts.
func TestNormalizeTypeScriptImportWithoutTargetIsUnresolved(t *testing.T) {
	payload := TypeScriptPayload{
		Version:    TypeScriptWireVersion,
		Repository: TypeScriptRepository{Name: "consumer-x"},
		Package: &TypeScriptPackage{
			Name: "@ladygraph-fixture/consumer-x", Version: "1.0.0",
			RootPath: ".", ManifestPath: "package.json",
		},
		Files: []string{"src/index.ts"},
		Symbols: []TypeScriptSymbol{{
			File: "src/index.ts", Name: "helper", QualifiedName: "helper", Kind: "import",
			Signature: `import { helper } from "@ladygraph-fixture/shared"`,
			StartLine: 1, EndLine: 1, Start: 0, End: 47,
		}},
		Imports: []TypeScriptImport{{
			File: "src/index.ts", QualifiedName: "helper",
			Start: 0, End: 47, StartLine: 1,
			Text:             `import { helper } from "@ladygraph-fixture/shared"`,
			RequestedPackage: "@ladygraph-fixture/shared",
			RequestedSymbol:  "helper",
			Target:           nil,
			Reason:           "NO_DECLARATION_MAP",
			Detail:           "provider has no declaration map for dist/helper.d.ts",
		}},
		Unresolved: []TypeScriptUnresolved{{
			File:             "src/index.ts",
			Reason:           "PACKAGE_PROVIDER_NOT_FOUND",
			RequestedPackage: "@ladygraph-fixture/other",
			Start:            60,
		}},
	}

	set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-x"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.ImportsWithoutTarget != 1 {
		t.Fatalf("ImportsWithoutTarget = %d, want 1", report.ImportsWithoutTarget)
	}
	for _, edge := range set.Edges {
		if edge.Kind == ImportsSymbol {
			t.Fatalf("an import without a target must not produce an edge: %#v", edge)
		}
	}
	if len(set.Unresolved) != 2 {
		t.Fatalf("unresolved = %#v, want one entry from the import and one passed through from payload.Unresolved", set.Unresolved)
	}

	var fromImport, fromPayload UnresolvedReference
	for _, entry := range set.Unresolved {
		switch entry.Reason {
		case "NO_DECLARATION_MAP":
			fromImport = entry
		case "PACKAGE_PROVIDER_NOT_FOUND":
			fromPayload = entry
		default:
			t.Fatalf("unexpected unresolved reason %q: %#v", entry.Reason, entry)
		}
	}

	if fromImport.Detail != "provider has no declaration map for dist/helper.d.ts" ||
		fromImport.RequestedPackage != "@ladygraph-fixture/shared" ||
		fromImport.RequestedSymbol != "helper" ||
		fromImport.Language != LanguageTypeScript ||
		fromImport.FileKey != FileKey("consumer-x", "src/index.ts") {
		t.Fatalf("unresolved entry from the import = %#v", fromImport)
	}
	if fromImport.SourceSymbolKey == "" {
		t.Fatalf("the import's unresolved entry should carry the consumer's own binding as its source symbol")
	}

	if fromPayload.RequestedPackage != "@ladygraph-fixture/other" ||
		fromPayload.Language != LanguageTypeScript ||
		fromPayload.FileKey != FileKey("consumer-x", "src/index.ts") {
		t.Fatalf("unresolved entry passed through from payload.Unresolved = %#v", fromPayload)
	}
	if fromPayload.SourceSymbolKey != "" {
		t.Fatalf("payload.Unresolved carries no source symbol, NormalizeTypeScript must not invent one: %#v", fromPayload)
	}
}

// TestNormalizeTypeScriptImportWithIncompleteTargetIsUnresolved covers a
// target that is present but missing its module, its class or its signature:
// without all three, the provider's stable key cannot be derived, so it must
// be treated exactly like a nil target, never guessed.
func TestNormalizeTypeScriptImportWithIncompleteTargetIsUnresolved(t *testing.T) {
	base := TypeScriptImportTarget{
		Repository:    "shared-library",
		Package:       "@ladygraph-fixture/shared",
		QualifiedName: "helper",
		Kind:          "function",
		Signature:     "export function helper(shape: Shape): number",
		File:          "src/helper.ts",
		StartLine:     3,
	}
	withEmptyKind := base
	withEmptyKind.Kind = ""
	withEmptySignature := base
	withEmptySignature.Signature = ""
	withEmptyFile := base
	withEmptyFile.File = ""

	cases := map[string]TypeScriptImportTarget{
		"empty kind":      withEmptyKind,
		"empty signature": withEmptySignature,
		"empty module":    withEmptyFile,
	}

	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			payload := TypeScriptPayload{
				Version:    TypeScriptWireVersion,
				Repository: TypeScriptRepository{Name: "consumer-x"},
				Package: &TypeScriptPackage{
					Name: "@ladygraph-fixture/consumer-x", Version: "1.0.0",
					RootPath: ".", ManifestPath: "package.json",
				},
				Files: []string{"src/index.ts"},
				Symbols: []TypeScriptSymbol{{
					File: "src/index.ts", Name: "helper", QualifiedName: "helper", Kind: "import",
					Signature: `import { helper } from "@ladygraph-fixture/shared"`,
					StartLine: 1, EndLine: 1, Start: 0, End: 47,
				}},
				Imports: []TypeScriptImport{{
					File: "src/index.ts", QualifiedName: "helper",
					Start: 0, End: 47, StartLine: 1,
					Text:             `import { helper } from "@ladygraph-fixture/shared"`,
					RequestedPackage: "@ladygraph-fixture/shared",
					RequestedSymbol:  "helper",
					Target:           &target,
					Reason:           "PROVIDER_DECLARATION_UNCLASSIFIED",
					Detail:           "declaration map resolved a position the provider source could not classify",
				}},
			}

			set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-x"})
			if err != nil {
				t.Fatalf("NormalizeTypeScript() error = %v", err)
			}
			if err := set.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if report.ImportsWithoutTarget != 1 {
				t.Fatalf("ImportsWithoutTarget = %d, want 1", report.ImportsWithoutTarget)
			}
			for _, edge := range set.Edges {
				if edge.Kind == ImportsSymbol {
					t.Fatalf("an import with an incomplete target must not produce an edge: %#v", edge)
				}
			}
			if len(set.Unresolved) != 1 || set.Unresolved[0].Reason != "PROVIDER_DECLARATION_UNCLASSIFIED" {
				t.Fatalf("unresolved = %#v", set.Unresolved)
			}
		})
	}
}

// TestNormalizeTypeScriptGradesCrossRepositoryTargetsByEvidence pins the two
// exact grades apart. Both are exact edges to the same provider declaration;
// they differ in what placed that declaration in the provider's source, and
// a consumer of the graph has to be able to tell which.
func TestNormalizeTypeScriptGradesCrossRepositoryTargetsByEvidence(t *testing.T) {
	cases := map[string]struct {
		source     string
		confidence Confidence
		provenance Provenance
	}{
		"declaration map": {TypeScriptIdentityDeclarationMap, ExactTypechecked, TypeScriptChecker},
		// A payload recorded before the provider-export path existed carries
		// no source at all, and means the same as a declaration map.
		"unset":           {"", ExactTypechecked, TypeScriptChecker},
		"provider export": {TypeScriptIdentityProviderExport, ExactPackageMapped, TypeScriptProjectReference},
	}

	for name, expected := range cases {
		t.Run(name, func(t *testing.T) {
			target := TypeScriptImportTarget{
				Repository:    "shared-library",
				Package:       "@ladygraph-fixture/shared",
				QualifiedName: "helper",
				Kind:          "function",
				Signature:     "export function helper(shape: Shape): number",
				File:          "src/helper.ts",
				StartLine:     3,
				Source:        expected.source,
			}
			payload := TypeScriptPayload{
				Version:    TypeScriptWireVersion,
				Repository: TypeScriptRepository{Name: "consumer-x"},
				Package: &TypeScriptPackage{
					Name: "@ladygraph-fixture/consumer-x", Version: "1.0.0",
					RootPath: ".", ManifestPath: "package.json",
				},
				Files: []string{"src/index.ts"},
				Symbols: []TypeScriptSymbol{{
					File: "src/index.ts", Name: "helper", QualifiedName: "helper", Kind: "import",
					Signature: `import { helper } from "@ladygraph-fixture/shared"`,
					StartLine: 1, EndLine: 1, Start: 0, End: 47,
				}},
				Imports: []TypeScriptImport{{
					File: "src/index.ts", QualifiedName: "helper",
					Start: 0, End: 47, StartLine: 1,
					Text:             `import { helper } from "@ladygraph-fixture/shared"`,
					RequestedPackage: "@ladygraph-fixture/shared",
					RequestedSymbol:  "helper",
					Target:           &target,
				}},
			}

			set, _, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-x"})
			if err != nil {
				t.Fatalf("NormalizeTypeScript() error = %v", err)
			}
			var edges []Edge
			for _, edge := range set.Edges {
				if edge.Kind == ImportsSymbol {
					edges = append(edges, edge)
				}
			}
			if len(edges) != 1 {
				t.Fatalf("IMPORTS_SYMBOL edges = %#v, want exactly one", edges)
			}
			if edges[0].Confidence != expected.confidence || edges[0].Provenance != expected.provenance {
				t.Fatalf("edge graded %s/%s, want %s/%s",
					edges[0].Confidence, edges[0].Provenance, expected.confidence, expected.provenance)
			}
			// Both grades are exact: the target key is the provider's own,
			// so the edge resolves once the provider's Set is merged in.
			if !edges[0].Confidence.Exact() {
				t.Fatalf("edge confidence %s is not exact", edges[0].Confidence)
			}
		})
	}
}

func TestNormalizeTypeScriptIsDeterministicAndPortable(t *testing.T) {
	payload := loadPayload(t, "shared-library.json")
	first, _, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	second, _, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	for index := range first.Edges {
		if first.Edges[index] != second.Edges[index] {
			t.Fatalf("edge %d differs between runs", index)
		}
	}

	// Keys must not depend on where the repository is checked out.
	moved, _, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/elsewhere/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	for index := range first.Symbols {
		if first.Symbols[index].Key != moved.Symbols[index].Key {
			t.Fatalf("symbol key changed with the checkout path")
		}
	}
	for index := range first.Files {
		if first.Files[index].Key != moved.Files[index].Key {
			t.Fatalf("file key changed with the checkout path")
		}
	}
}

func TestDecodeTypeScriptPayloadRejectsForeignVersions(t *testing.T) {
	if _, err := DecodeTypeScriptPayload([]byte(`{"version":99}`)); !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("DecodeTypeScriptPayload() error = %v, want ErrInvalidFacts", err)
	}
	if _, err := DecodeTypeScriptPayload([]byte("not json")); err == nil {
		t.Fatalf("DecodeTypeScriptPayload() must reject malformed input")
	}
	if _, _, err := NormalizeTypeScript(context.Background(), TypeScriptPayload{
		Version: TypeScriptWireVersion,
	}, workspace.Repository{RealPath: "/repositories/x"}); !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("NormalizeTypeScript() must reject a payload without repository")
	}
}

// TestNormalizeTypeScriptLocalExtendsResolveWithinOneRepository covers
// shared-library's own `interface NamedShape extends Shape, Named`: both
// bases are declared in this same repository — Shape in src/value.ts, Named
// in src/inheritance.ts — so each becomes its own EXTENDS edge entirely
// within shared-library's own Set, no merge required.
func TestNormalizeTypeScriptLocalExtendsResolveWithinOneRepository(t *testing.T) {
	payload := loadPayload(t, "shared-library.json")
	set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.ExtendsWithoutTarget != 0 {
		t.Fatalf("dropped extends targets = %d", report.ExtendsWithoutTarget)
	}

	source := findSymbol(t, set.Symbols, "shared-library", "src/inheritance.ts", "NamedShape")
	cases := []struct {
		targetFile          string
		targetQualifiedName string
	}{
		{"src/value.ts", "Shape"},
		{"src/inheritance.ts", "Named"},
	}
	for _, testCase := range cases {
		target := findSymbol(t, set.Symbols, "shared-library", testCase.targetFile, testCase.targetQualifiedName)
		matched := false
		for _, edge := range set.Edges {
			if edge.Kind == Extends && edge.SourceKey == source.Key && edge.TargetKey == target.Key {
				matched = true
				if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
					t.Fatalf("EXTENDS edge to %q = %#v", testCase.targetQualifiedName, edge)
				}
				break
			}
		}
		if !matched {
			t.Fatalf("no EXTENDS edge from NamedShape (%s) to %s#%s (%s)",
				source.Key, testCase.targetFile, testCase.targetQualifiedName, target.Key)
		}
	}
}

// TestNormalizeTypeScriptExtendsTargetKeyMatchesProvider is the EXTENDS
// half of the parity proof LUQUE-0907 already established for
// IMPORTS_SYMBOL: consumer-a's `class LabeledWidget extends Widget` crosses
// into shared-library through the same import that also backs
// LabeledWidget's IMPORTS_SYMBOL and PACKAGE_DEPENDS_ON edges, so the
// EXTENDS target must derive the exact same provider-source key the
// provider assigns its own declaration — the same proof, for a different
// edge kind.
func TestNormalizeTypeScriptExtendsTargetKeyMatchesProvider(t *testing.T) {
	ctx := context.Background()

	providerPayload := loadPayload(t, "shared-library.json")
	providerSet, _, err := NormalizeTypeScript(ctx, providerPayload, workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(shared-library) error = %v", err)
	}

	consumerPayload := loadPayload(t, "consumer-a.json")
	consumerSet, report, err := NormalizeTypeScript(ctx, consumerPayload, workspace.Repository{RealPath: "/repositories/consumer-a"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-a) error = %v", err)
	}
	if report.ExtendsWithoutTarget != 0 {
		t.Fatalf("consumer-a dropped %d extends bases without a target, want 0", report.ExtendsWithoutTarget)
	}

	source := findSymbol(t, consumerSet.Symbols, "consumer-a", "src/derived.ts", "LabeledWidget")
	var extendsEdges []Edge
	for _, edge := range consumerSet.Edges {
		if edge.Kind == Extends && edge.SourceKey == source.Key {
			extendsEdges = append(extendsEdges, edge)
		}
	}
	if len(extendsEdges) != 1 {
		t.Fatalf("EXTENDS edges from LabeledWidget = %d, want 1: %#v", len(extendsEdges), consumerSet.Edges)
	}

	// A consumer Set alone can never validate: the EXTENDS edge names a
	// symbol the provider owns. It only closes once the provider is merged.
	if err := consumerSet.Validate(); err == nil {
		t.Fatalf("Validate() on consumer-a alone unexpectedly passed: an EXTENDS " +
			"edge should dangle until the provider is merged in")
	}

	provided := findSymbol(t, providerSet.Symbols, "shared-library", "src/inheritance.ts", "Widget")
	if extendsEdges[0].TargetKey != provided.Key {
		t.Fatalf("LabeledWidget's EXTENDS edge targets %s, want the provider's key for Widget (%s)",
			extendsEdges[0].TargetKey, provided.Key)
	}
	if !extendsEdges[0].Confidence.Exact() || extendsEdges[0].EvidenceKey == "" {
		t.Fatalf("EXTENDS edge = %#v", extendsEdges[0])
	}

	merged := providerSet
	merged.Merge(consumerSet)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() on the merged graph = %v", err)
	}
}

// TestNormalizeTypeScriptPackageDependsOnTargetKeyMatchesProvider covers
// consumer-a's real dependency on @ladygraph-fixture/shared: the edge connects
// consumer-a's own package key to shared-library's package key, exactly as
// PackageKey derives them independently on each side — the package-level
// analogue of the symbol-level parity tests above.
func TestNormalizeTypeScriptPackageDependsOnTargetKeyMatchesProvider(t *testing.T) {
	ctx := context.Background()

	providerSet, _, err := NormalizeTypeScript(ctx, loadPayload(t, "shared-library.json"), workspace.Repository{RealPath: "/repositories/shared-library"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(shared-library) error = %v", err)
	}
	consumerSet, _, err := NormalizeTypeScript(ctx, loadPayload(t, "consumer-a.json"), workspace.Repository{RealPath: "/repositories/consumer-a"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-a) error = %v", err)
	}

	consumerPackageKey := PackageKey(LanguageTypeScript, "consumer-a", "@ladygraph-fixture/consumer-a")
	providerPackageKey := PackageKey(LanguageTypeScript, "shared-library", "@ladygraph-fixture/shared")

	var dependsOnEdges []Edge
	for _, edge := range consumerSet.Edges {
		if edge.Kind == PackageDependsOn {
			dependsOnEdges = append(dependsOnEdges, edge)
		}
	}
	if len(dependsOnEdges) != 1 {
		t.Fatalf("PACKAGE_DEPENDS_ON edges from consumer-a = %d, want 1: %#v", len(dependsOnEdges), consumerSet.Edges)
	}
	edge := dependsOnEdges[0]
	if edge.SourceKey != consumerPackageKey {
		t.Fatalf("PACKAGE_DEPENDS_ON source = %s, want consumer-a's own package key %s", edge.SourceKey, consumerPackageKey)
	}
	if edge.TargetKey != providerPackageKey {
		t.Fatalf("PACKAGE_DEPENDS_ON target = %s, want shared-library's package key %s", edge.TargetKey, providerPackageKey)
	}
	if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
		t.Fatalf("PACKAGE_DEPENDS_ON edge = %#v", edge)
	}

	if err := providerSet.Validate(); err != nil {
		t.Fatalf("Validate(shared-library) error = %v", err)
	}
	merged := providerSet
	merged.Merge(consumerSet)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() on the merged graph = %v", err)
	}
}

// TestNormalizeTypeScriptUnusedManifestDependencyProducesNoEdge covers
// consumer-a's package.json, which lists a dependency on
// @ladygraph-fixture/unused that nothing in src/ actually imports: decision 1
// forbids an edge from a nominal package.json string, so the worker itself
// never reports one for it — facts-cli.ts derives PACKAGE_DEPENDS_ON purely
// from checker-resolved imports, never from package.json — and the
// normalised graph carries exactly the one PACKAGE_DEPENDS_ON edge a real
// import backs: consumer-a to @ladygraph-fixture/shared.
func TestNormalizeTypeScriptUnusedManifestDependencyProducesNoEdge(t *testing.T) {
	consumerSet, _, err := NormalizeTypeScript(context.Background(), loadPayload(t, "consumer-a.json"), workspace.Repository{RealPath: "/repositories/consumer-a"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript(consumer-a) error = %v", err)
	}

	sharedPackageKey := PackageKey(LanguageTypeScript, "shared-library", "@ladygraph-fixture/shared")
	var dependsOnEdges []Edge
	for _, edge := range consumerSet.Edges {
		if edge.Kind == PackageDependsOn {
			dependsOnEdges = append(dependsOnEdges, edge)
		}
	}
	if len(dependsOnEdges) != 1 || dependsOnEdges[0].TargetKey != sharedPackageKey {
		t.Fatalf("PACKAGE_DEPENDS_ON edges from consumer-a = %#v, want exactly one targeting %s "+
			"(@ladygraph-fixture/unused is declared in package.json but never imported, so it must "+
			"produce none)", dependsOnEdges, sharedPackageKey)
	}
}

// TestNormalizeTypeScriptExtendsWithoutTargetIsUnresolved covers an extends
// base whose provider identity could not be proven — no declaration map
// reached it, or the base resolved to neither a local declaration nor an
// import at all: it must become an UnresolvedReference, never a guessed
// edge, exactly like an import or an export without a target.
func TestNormalizeTypeScriptExtendsWithoutTargetIsUnresolved(t *testing.T) {
	payload := TypeScriptPayload{
		Version:    TypeScriptWireVersion,
		Repository: TypeScriptRepository{Name: "consumer-x"},
		Package: &TypeScriptPackage{
			Name: "@ladygraph-fixture/consumer-x", Version: "1.0.0",
			RootPath: ".", ManifestPath: "package.json",
		},
		Files: []string{"src/index.ts"},
		Symbols: []TypeScriptSymbol{{
			File: "src/index.ts", Name: "Derived", QualifiedName: "Derived", Kind: "class",
			Signature: "export class Derived extends Base",
			StartLine: 1, EndLine: 3, Start: 0, End: 60,
		}},
		Extends: []TypeScriptExtends{{
			File: "src/index.ts", QualifiedName: "Derived",
			Start: 30, End: 34, StartLine: 1,
			Text:                "Base",
			TargetQualifiedName: "",
			TargetFile:          "",
			Target:              nil,
			RequestedPackage:    "@ladygraph-fixture/shared",
			RequestedSymbol:     "Base",
			Reason:              "PROVIDER_SOURCE_UNAVAILABLE",
			Detail:              "no declaration map places this symbol in the provider's source",
		}},
	}

	set, report, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-x"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.ExtendsWithoutTarget != 1 {
		t.Fatalf("ExtendsWithoutTarget = %d, want 1", report.ExtendsWithoutTarget)
	}
	for _, edge := range set.Edges {
		if edge.Kind == Extends {
			t.Fatalf("an extends base without a target must not produce an edge: %#v", edge)
		}
	}
	if len(set.Unresolved) != 1 {
		t.Fatalf("unresolved = %#v, want 1 entry", set.Unresolved)
	}
	entry := set.Unresolved[0]
	if entry.Reason != "PROVIDER_SOURCE_UNAVAILABLE" ||
		entry.RequestedPackage != "@ladygraph-fixture/shared" ||
		entry.RequestedSymbol != "Base" ||
		entry.Language != LanguageTypeScript ||
		entry.FileKey != FileKey("consumer-x", "src/index.ts") {
		t.Fatalf("unresolved entry = %#v", entry)
	}
	if entry.SourceSymbolKey == "" {
		t.Fatalf("the extends unresolved entry should carry the declaring class as its source symbol")
	}
}

// Two files of one package may each declare a local with the same name, kind
// and signature. They are different symbols: their stable keys must differ,
// and each must be defined by exactly one file. A shared key would make the
// canonical DEFINES relationship claim one symbol is declared twice.
func TestNormalizeTypeScriptSeparatesHomonymsDeclaredInDifferentModules(t *testing.T) {
	payload := TypeScriptPayload{
		Version:    TypeScriptWireVersion,
		Repository: TypeScriptRepository{Name: "consumer-x"},
		Package: &TypeScriptPackage{
			Name: "@ladygraph-fixture/consumer-x", Version: "1.0.0",
			RootPath: ".", ManifestPath: "package.json",
		},
		Files: []string{"src/first.ts", "src/second.ts"},
		Symbols: []TypeScriptSymbol{
			{
				File: "src/first.ts", Name: "s", QualifiedName: "s", Kind: "variable",
				Signature: "s", StartLine: 4, EndLine: 4, Start: 40, End: 41,
			},
			{
				File: "src/second.ts", Name: "s", QualifiedName: "s", Kind: "variable",
				Signature: "s", StartLine: 9, EndLine: 9, Start: 90, End: 91,
			},
		},
	}

	set, _, err := NormalizeTypeScript(context.Background(), payload, workspace.Repository{RealPath: "/repositories/consumer-x"})
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(set.Symbols) != 2 {
		t.Fatalf("symbols = %d, want 2", len(set.Symbols))
	}
	if set.Symbols[0].Key == set.Symbols[1].Key {
		t.Fatalf("homonyms in different modules share the stable key %q", set.Symbols[0].Key)
	}
	definers := make(map[string][]string)
	for _, edge := range set.Edges {
		if edge.Kind == Defines {
			definers[edge.TargetKey] = append(definers[edge.TargetKey], edge.SourceKey)
		}
	}
	if len(definers) != 2 {
		t.Fatalf("DEFINES targets = %d, want 2 (%#v)", len(definers), definers)
	}
	for target, files := range definers {
		if len(files) != 1 {
			t.Fatalf("symbol %s is defined by %d files, want exactly 1: %v", target, len(files), files)
		}
	}
}
