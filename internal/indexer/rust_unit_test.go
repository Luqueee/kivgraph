package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

func rustFixtureRepository(t *testing.T, name string) workspace.Repository {
	t.Helper()
	return rustFixtureRepositoryFrom(t, "workspace", name)
}

func rustFixtureRepositoryFrom(t *testing.T, fixture, name string) workspace.Repository {
	t.Helper()
	root := filepath.Join(testsupport.TempDir(t), name)
	source := filepath.Join("..", "..", "testdata", "rust", fixture)
	if err := os.CopyFS(root, os.DirFS(source)); err != nil {
		t.Fatalf("copy Rust fixture %q: %v", fixture, err)
	}
	return workspace.Repository{Name: name, Path: root, RealPath: root, Languages: []string{"rust"}}
}

func rustFullOptions(t *testing.T, repositories ...workspace.Repository) FullOptions {
	t.Helper()
	return FullOptions{
		Repositories:        repositories,
		RustAnalyzer:        "rust-analyzer",
		RustTargetDirectory: filepath.Join(testsupport.TempDir(t), "rust-target"),
		RustBuildScripts:    true,
		RustProcMacros:      true,
		RustSysroot:         "discover",
		WorkingDirectory:    testsupport.TempDir(t),
	}
}

func requireRustAnalyzer(t *testing.T) {
	t.Helper()
	testsupport.RequireRustAnalyzer(t)
}

// TestFullIndexesARustRepository is the end to end contract of the Rust path:
// one registered repository, one published set that validates on its own.
func TestFullIndexesARustRepository(t *testing.T) {
	requireRustAnalyzer(t)
	repository := rustFixtureRepository(t, "fixture")

	set, report, err := Full(context.Background(), rustFullOptions(t, repository))
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.RustRepositories != 1 || report.RustWorkspaces != 1 || report.RustCrates != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.RustWorkspacesNotLoaded != 0 || report.RustSymbols == 0 || report.RustReferences == 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(set.Repositories) != 1 || set.Repositories[0].Languages[0] != facts.LanguageRust {
		t.Fatalf("repositories = %#v", set.Repositories)
	}
	crates := make(map[string]facts.Package, len(set.Packages))
	for _, entry := range set.Packages {
		crates[entry.Name] = entry
	}
	if len(crates) != 2 || crates["engine"].Version != "1.4.0" || crates["support"].Language != facts.LanguageRust {
		t.Fatalf("packages = %#v", set.Packages)
	}

	var calls, dependencies int
	for _, edge := range set.Edges {
		switch edge.Kind {
		case facts.CallsDirect:
			calls++
			if edge.Confidence != facts.ExactTypechecked || edge.EvidenceKey == "" {
				t.Fatalf("call edge = %#v", edge)
			}
		case facts.PackageDependsOn:
			dependencies++
		}
	}
	if calls == 0 || dependencies == 0 {
		t.Fatalf("edges = %d calls, %d dependencies", calls, dependencies)
	}
	// The standard library is not a registered repository, so its uses are
	// declared rather than turned into edges.
	declared := false
	for _, entry := range set.Unresolved {
		if entry.Reason == "CRATE_PROVIDER_NOT_FOUND" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("unresolved = %#v, want the sysroot crates declared", set.Unresolved)
	}
}

// TestFullIsolatesARustWorkspaceThatCannotLoad keeps one broken analyzer from
// costing every other repository its graph.
func TestFullIsolatesARustWorkspaceThatCannotLoad(t *testing.T) {
	repository := rustFixtureRepository(t, "fixture")
	options := rustFullOptions(t, repository)
	options.RustAnalyzer = "ladygraph-rust-analyzer-that-is-not-installed"

	set, report, err := Full(context.Background(), options)
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.RustWorkspacesNotLoaded != 1 || report.RustSymbols != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(set.Unresolved) != 1 || set.Unresolved[0].Reason != "ANALYZER_UNAVAILABLE" {
		t.Fatalf("unresolved = %#v", set.Unresolved)
	}
	if len(report.RustDiagnostics) == 0 || !strings.Contains(report.RustDiagnostics[0], "ANALYZER_UNAVAILABLE") {
		t.Fatalf("diagnostics = %#v", report.RustDiagnostics)
	}
}

// TestFullNamesARustRepositoryWithoutManifests keeps a registry entry that
// contributes nothing from looking like coverage.
func TestFullNamesARustRepositoryWithoutManifests(t *testing.T) {
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("no crate here\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	repository := workspace.Repository{
		Name: "empty", Path: root, RealPath: root, Languages: []string{"rs"},
	}

	set, report, err := Full(context.Background(), rustFullOptions(t, repository))
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if len(report.RustWithoutWorkspaces) != 1 || report.RustWithoutWorkspaces[0] != "empty" {
		t.Fatalf("report = %+v", report)
	}
	if len(set.Symbols) != 0 {
		t.Fatalf("symbols = %#v", set.Symbols)
	}
}

// TestRustWorkspaceLimitStaysBelowTheProcessorCount defends the memory ceiling:
// every analyzer process holds a whole workspace and its sysroot.
func TestRustWorkspaceLimitStaysBelowTheProcessorCount(t *testing.T) {
	if limit := rustWorkspaceLimit(FullOptions{}); limit < 1 || limit > defaultRustWorkspaceLimit {
		t.Fatalf("default limit = %d", limit)
	}
	if limit := rustWorkspaceLimit(FullOptions{RustMaximumWorkspaces: 7}); limit != 7 {
		t.Fatalf("configured limit = %d", limit)
	}
	if threads := rustAnalyzerThreads(FullOptions{RustMaximumWorkspaces: 1024}); threads != 1 {
		t.Fatalf("threads = %d, want at least one per worker", threads)
	}
}

// crossRepositoryFixture copies the two repositories side by side, because the
// consumer reaches its provider through a path dependency and the layout is
// part of what is being tested.
func crossRepositoryFixture(t *testing.T) (provider, consumer workspace.Repository) {
	t.Helper()
	root := testsupport.TempDir(t)
	source := filepath.Join("..", "..", "testdata", "rust", "cross-repository")
	if err := os.CopyFS(root, os.DirFS(source)); err != nil {
		t.Fatalf("copy cross-repository fixture: %v", err)
	}
	providerRoot := filepath.Join(root, "provider")
	consumerRoot := filepath.Join(root, "consumer")
	return workspace.Repository{
			Name: "acme/provider", Path: providerRoot, RealPath: providerRoot,
			Languages: []string{"rust"},
		}, workspace.Repository{
			Name: "acme/consumer", Path: consumerRoot, RealPath: consumerRoot,
			Languages: []string{"rust"},
		}
}

// TestFullResolvesARustTargetInAnotherRepository is the cross-repository
// contract: consumer and provider are indexed separately, and the edge only
// exists because both sides derive the same identity from the same analyzer
// symbol -- never from a name that happens to match.
func TestFullResolvesARustTargetInAnotherRepository(t *testing.T) {
	requireRustAnalyzer(t)
	provider, consumer := crossRepositoryFixture(t)

	set, report, err := Full(context.Background(), rustFullOptions(t, provider, consumer))
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	// A dangling edge would fail here: the target of the cross-repository
	// call is a symbol the provider's own unit published.
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.RustRepositories != 2 || report.RustWorkspaces != 2 {
		t.Fatalf("report = %+v", report)
	}

	symbols := make(map[string]facts.Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		symbols[symbol.Key] = symbol
	}
	crossCall := false
	crossDependency := false
	for _, edge := range set.Edges {
		switch edge.Kind {
		case facts.CallsDirect:
			source, hasSource := symbols[edge.SourceKey]
			target, hasTarget := symbols[edge.TargetKey]
			if !hasSource || !hasTarget {
				continue
			}
			if source.RepositoryKey == target.RepositoryKey {
				continue
			}
			crossCall = true
			if edge.Confidence != facts.ExactPackageMapped || edge.Provenance != facts.RustSyntaxCall {
				t.Fatalf("cross-repository call = %#v", edge)
			}
			if target.Name != "double" || source.Name != "run" {
				t.Fatalf("cross-repository call joins %q and %q", source.Name, target.Name)
			}
			if edge.EvidenceKey == "" {
				t.Fatal("a cross-repository edge must carry the occurrence that proves it")
			}
		case facts.PackageDependsOn:
			if strings.Contains(edge.SourceKey, "consumer") && strings.Contains(edge.TargetKey, "support") {
				crossDependency = true
				if edge.Provenance != facts.RustAnalyzerMoniker || edge.Confidence != facts.ExactPackageMapped {
					t.Fatalf("cross-repository dependency = %#v", edge)
				}
			}
		}
	}
	if !crossCall {
		t.Fatal("the consumer's call into the provider repository did not become an edge")
	}
	if !crossDependency {
		t.Fatal("the crate dependency across repositories was not recorded")
	}
}

// TestFullDeclaresARustProviderNobodyRegistered is the other half: without the
// provider in the registry the same call is a declared failure, not an edge.
func TestFullDeclaresARustProviderNobodyRegistered(t *testing.T) {
	requireRustAnalyzer(t)
	_, consumer := crossRepositoryFixture(t)

	set, _, err := Full(context.Background(), rustFullOptions(t, consumer))
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, edge := range set.Edges {
		if edge.Kind == facts.PackageDependsOn {
			t.Fatalf("an unregistered provider must not produce %#v", edge)
		}
	}
	declared := false
	for _, entry := range set.Unresolved {
		if entry.Reason == "CRATE_PROVIDER_NOT_FOUND" && entry.RequestedPackage == "support" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("unresolved = %#v, want the unregistered provider declared", set.Unresolved)
	}
}

// TestFullKeepsARustGraphInsideItsOwnRepository defends the boundary of a Rust
// repository. Both halves of it were a pass that could not publish at all.
//
// A crate vendored into the repository is code the analyzer indexes and no
// manifest of this repository declares: keeping the uses of it while dropping
// its declarations left every one of those edges without a target, and the
// fact set never validated again. And one package compiles into several
// crates -- a library, a binary, a build script, one per integration test --
// whose root modules all arrive under the moniker of the package: crediting a
// use to whichever of them the analyzer happened to emit first put the use in
// a file its own source symbol does not live in, which no snapshot accepts.
func TestFullKeepsARustGraphInsideItsOwnRepository(t *testing.T) {
	requireRustAnalyzer(t)
	repository := rustFixtureRepositoryFrom(t, "targets", "app")

	set, report, err := Full(context.Background(), rustFullOptions(t, repository))
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.RustWorkspaces != 1 || report.RustCrates != 1 || report.RustWorkspacesNotLoaded != 0 {
		t.Fatalf("report = %+v, want the vendored crate outside this repository", report)
	}

	files := make(map[string]facts.File, len(set.Files))
	for _, file := range set.Files {
		if strings.HasPrefix(file.Path, "vendor/") {
			t.Fatalf("file %q belongs to a vendored crate this repository does not provide", file.Path)
		}
		files[file.Key] = file
	}
	for _, entry := range set.Packages {
		if entry.Name != "app" {
			t.Fatalf("package %q is not declared by any manifest of this repository", entry.Name)
		}
	}
	// The vendored crate is out of the graph and said so: silence would read
	// as a repository that uses nothing.
	declared := false
	for _, entry := range set.Unresolved {
		if entry.Reason == "CRATE_PROVIDER_NOT_FOUND" && entry.RequestedPackage == "vendored" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("unresolved = %#v, want the vendored crate declared", set.Unresolved)
	}

	symbols := make(map[string]facts.Symbol, len(set.Symbols))
	roots, mains := make([]facts.Symbol, 0, 1), make([]facts.Symbol, 0, 1)
	for _, symbol := range set.Symbols {
		symbols[symbol.Key] = symbol
		switch symbol.Name {
		case "crate":
			roots = append(roots, symbol)
		case "main":
			mains = append(mains, symbol)
		}
	}
	// One node per moniker, placed in the target another repository can name:
	// the library over the binary, the binary over the build script.
	if len(roots) != 1 || files[roots[0].FileKey].Path != "src/lib.rs" {
		t.Fatalf("crate root modules = %#v, want the library's", roots)
	}
	if len(mains) != 1 || files[mains[0].FileKey].Path != "src/main.rs" {
		t.Fatalf("main declarations = %#v, want the binary's", mains)
	}
	// The binary really does call the library, and the edge proves the choice
	// above: had the build script's `main` won the moniker, this call would
	// have no source to hang on.
	call := false
	for _, edge := range set.Edges {
		if edge.Kind != facts.CallsDirect || edge.SourceKey != mains[0].Key {
			continue
		}
		target, published := symbols[edge.TargetKey]
		if !published || target.Name != "run" {
			continue
		}
		call = true
		if files[target.FileKey].Path != "src/lib.rs" {
			t.Fatalf("the call reaches %q", files[target.FileKey].Path)
		}
	}
	if !call {
		t.Fatal("the binary's call into the library did not become an edge")
	}

	// Every unresolved reference is an observation of one file: a record whose
	// source declaration lives in another one is what stopped the snapshot.
	for _, entry := range set.Unresolved {
		if entry.SourceSymbolKey == "" || entry.FileKey == "" {
			continue
		}
		source, published := symbols[entry.SourceSymbolKey]
		if !published {
			t.Fatalf("unresolved %q names a source symbol nobody published", entry.RequestedSymbol)
		}
		if source.FileKey != entry.FileKey {
			t.Fatalf("unresolved %q observed in %q is credited to a declaration in %q",
				entry.RequestedSymbol, files[entry.FileKey].Path, files[source.FileKey].Path)
		}
	}

	// A declaration the graph could not keep is named, not counted.
	duplicate := false
	for _, diagnostic := range report.RustDiagnostics {
		if strings.Contains(diagnostic, "in more than one document") {
			duplicate = true
		}
	}
	if !duplicate {
		t.Fatalf("diagnostics = %#v, want the duplicated monikers named", report.RustDiagnostics)
	}
}
