package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/rustloader"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
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
	//
	// That declaration only exists when the analyzer could resolve a use of the
	// standard library at all, and resolving one needs its sources on the
	// machine: without `rust-src` those tokens carry no moniker and the index
	// discards them, so nothing names a crate whose provider is missing. What is
	// absent then is the machine's standard library, not the pass's declaration,
	// and the difference is named rather than failed -- exactly as the analyzer's
	// own absence is. A guard that ignored it turned a runner without `rust-src`
	// into a broken release: the v0.1.3 release failed here on a commit whose CI
	// had just passed on another runner.
	_, reason, err := rustloader.DiscoverSysroot(context.Background(), repository.Path)
	switch {
	case err != nil:
		t.Logf("the standard library could not be discovered (%v), so its uses are not declared here", err)
	case reason != "":
		t.Logf("this machine has no indexable standard library (%s), so its uses are not declared here", reason)
	default:
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
	// The absence is declared, not implied: a reader cannot tell a machine
	// without a toolchain from a configuration that asked for nothing.
	if report.RustSysroot != "" || report.RustSysrootReason != string(rustloader.SysrootNotRequested) {
		t.Fatalf("report = %+v, want the standard library declared as not requested", report)
	}
}

// TestFullIsolatesARustWorkspaceThatCannotLoad keeps one broken analyzer from
// costing every other repository its graph.
func TestFullIsolatesARustWorkspaceThatCannotLoad(t *testing.T) {
	repository := rustFixtureRepository(t, "fixture")
	options := rustFullOptions(t, repository)
	options.RustAnalyzer = "kivgraph-rust-analyzer-that-is-not-installed"

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

// requireSysrootSources skips when the toolchain carries no library sources.
// `rustup component add rust-src` installs them; a machine without them can
// index its repositories and cannot index the standard library, which is
// exactly the contract this feature has.
func requireSysrootSources(t *testing.T) rustloader.SysrootProvider {
	t.Helper()
	provider, reason, err := rustloader.DiscoverSysroot(context.Background(), "")
	if err != nil {
		t.Fatalf("DiscoverSysroot() error = %v", err)
	}
	if reason != "" {
		t.Skipf("the standard library is not available: %s", reason)
	}
	return provider
}

// TestFullIndexesTheStandardLibraryAsASyntheticProvider is the acceptance
// criterion of LUQUE-1826: a derive, an overloaded operator, a `?` and a call
// into the standard library all reach a symbol of `core`, `alloc` or `std` with
// an exact confidence and observed evidence.
//
// It runs the analyzer over the whole library workspace, so it is the slowest
// test in this package by an order of magnitude. That cost is the feature.
func TestFullIndexesTheStandardLibraryAsASyntheticProvider(t *testing.T) {
	requireRustAnalyzer(t)
	sysroot := requireSysrootSources(t)
	repository := rustFixtureRepositoryFrom(t, "stdlib", "user")
	options := rustFullOptions(t, repository)
	options.RustIndexSysroot = true

	set, report, err := Full(context.Background(), options)
	if err != nil {
		t.Fatalf("Full() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if report.RustSysroot != sysroot.Repository.Name || report.RustSysrootReason != "" {
		t.Fatalf("report = %+v, want the synthetic repository named", report)
	}

	// The synthetic repository is in the set, with no version control
	// metadata: nothing read a commit for it, so nothing claims one.
	var synthetic *facts.Repository
	for index, entry := range set.Repositories {
		if entry.Name == sysroot.Repository.Name {
			synthetic = &set.Repositories[index]
		}
	}
	if synthetic == nil {
		t.Fatalf("repositories = %#v, want %q", set.Repositories, sysroot.Repository.Name)
	}
	if synthetic.Commit != "" || synthetic.Branch != "" {
		t.Fatalf("synthetic repository = %#v, want no version control metadata", *synthetic)
	}

	symbols := make(map[string]facts.Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		symbols[string(symbol.Key)] = symbol
	}
	// Every edge that leaves the fixture and lands in the standard library
	// has to carry the same guarantees as any other exact edge.
	byKind := make(map[facts.EdgeKind]int)
	reached := make(map[reachedEdge]bool)
	for _, edge := range set.Edges {
		source, sourceKnown := symbols[string(edge.SourceKey)]
		target, targetKnown := symbols[string(edge.TargetKey)]
		if !sourceKnown || !targetKnown {
			continue
		}
		if source.RepositoryKey == target.RepositoryKey {
			continue
		}
		if target.RepositoryKey != facts.RepositoryKey(sysroot.Repository.Name) {
			continue
		}
		byKind[edge.Kind]++
		reached[reachedEdge{source: source.Name, kind: edge.Kind, target: target.QualifiedName}] = true
		if !edge.Confidence.Exact() {
			t.Fatalf("edge into the standard library = %#v, want an exact confidence", edge)
		}
		if edge.EvidenceKey == "" || edge.Provenance == "" {
			t.Fatalf("edge into the standard library = %#v, want provenance and evidence", edge)
		}
	}
	if byKind[facts.CallsDirect] == 0 {
		t.Fatalf("edges by kind = %#v, want calls into the standard library", byKind)
	}
	if byKind[facts.Implements] == 0 {
		t.Fatalf("edges by kind = %#v, want the operator impl to reach its trait", byKind)
	}

	// The four silences this feature exists to close, each named. A counter
	// would pass while any one of them was still missing.
	for _, want := range []struct {
		source string
		kind   facts.EdgeKind
		target string
	}{
		// `#[derive(Clone, Debug, Default, PartialEq)]` leaves no relation at
		// all without the standard library in the graph.
		{"Offset", facts.References, "clone::Clone"},
		{"Offset", facts.References, "cmp::PartialEq"},
		{"Offset", facts.References, "default::Default"},
		{"Offset", facts.References, "fmt::macros::Debug"},
		// An overloaded operator reaches the trait it implements.
		{"Offset", facts.Implements, "ops::arith::Add"},
		// `?` desugars into a call the source never spells out.
		{"parse_line", facts.References, "result::impl::Result<T, E>::Try::branch"},
		// And the common case: a plain call into the standard library.
		{"parse_line", facts.CallsDirect, "str::impl::str::parse"},
		{"render", facts.CallsDirect, "string::impl::String::push_str"},
		{"render", facts.CallsDirect, "string::impl::T::ToString::to_string"},
	} {
		if !reached[reachedEdge{source: want.source, kind: want.kind, target: want.target}] {
			t.Fatalf("%s %s %s is missing: the graph still cannot see it",
				want.source, want.kind, want.target)
		}
	}

	// `u32 + u32` resolves to an impl `add_impl!` generates, which exists in no
	// source range and therefore in no index. It is declared, never guessed,
	// and never published as an edge to a symbol nobody has.
	declared := false
	for _, entry := range set.Unresolved {
		if entry.Reason == ProviderDefinitionNotIndexed &&
			entry.RequestedSymbol == "ops::arith::impl::u32::Add<Self>::add" {
			declared = true
		}
	}
	if !declared || report.EdgesWithoutProvider == 0 {
		t.Fatalf("unresolved = %d entries, dropped = %d: want the macro-generated impl declared",
			len(set.Unresolved), report.EdgesWithoutProvider)
	}
}

// TestWorkspaceNotLoadedFactsRecordsAScopeAndNotAReference is the guard on the
// only shape that lets a Rust repository survive a workspace it cannot read.
//
// The row must carry no file. That is not cosmetic: `UnresolvedScopes` selects
// exactly the failures whose file is unset, so a row that gained one would
// stop bounding the answers about its repository and every question about that
// repository would report COMPLETE over a workspace nobody could read. Neither
// reason had a test naming it before this.
func TestWorkspaceNotLoadedFactsRecordsAScopeAndNotAReference(t *testing.T) {
	repository := workspace.Repository{
		Name: "app", Path: "/repo/app", RealPath: "/repo/app", Languages: []string{"rust"},
	}
	unit := rustWorkspaceUnit{
		repository: repository,
		workspace:  workspace.CargoWorkspace{RootPath: "/repo/app"},
		crates:     []workspace.CargoCrate{{Name: "app_core", Version: "0.1.0"}},
	}

	for _, testCase := range []struct {
		name       string
		failure    rustloader.RunError
		wantReason rustloader.UnresolvedReason
	}{
		{
			name:       "the analyzer refused the workspace",
			failure:    rustloader.RunError{Kind: rustloader.RunErrorWorkspaceNotLoaded, Detail: "cargo metadata failed"},
			wantReason: rustloader.UnresolvedWorkspaceNotLoaded,
		},
		{
			// A missing analyzer is a different fact from a workspace the
			// analyzer read and rejected, and an agent that has to decide
			// whether to install something or fix a manifest needs to know
			// which one happened.
			name:       "the analyzer was not there at all",
			failure:    rustloader.RunError{Kind: rustloader.RunErrorAnalyzerUnavailable, Detail: "command is not executable"},
			wantReason: rustloader.UnresolvedAnalyzerUnavailable,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := workspaceNotLoadedFacts(unit, &testCase.failure)
			if !result.notLoaded {
				t.Fatal("a workspace that did not load must be marked as such, or the fact cache stores the hole")
			}
			if len(result.set.Unresolved) != 1 {
				t.Fatalf("unresolved = %#v, want exactly the workspace failure", result.set.Unresolved)
			}
			row := result.set.Unresolved[0]
			if row.Reason != string(testCase.wantReason) {
				t.Fatalf("reason = %q, want %q", row.Reason, testCase.wantReason)
			}
			if row.FileKey != "" {
				t.Fatalf("file = %q, want none: a row with a file is one reference and not the scope this is", row.FileKey)
			}
			if row.RequestedPackage != "app_core" {
				t.Fatalf("requested package = %q, want the crate the workspace resolves", row.RequestedPackage)
			}
			if row.Detail != testCase.failure.Detail {
				t.Fatalf("detail = %q, want the classified failure's own", row.Detail)
			}
			// A failure attributed to a repository the set does not declare is
			// not a valid fact, so the repository record travels with it.
			if len(result.set.Repositories) != 1 || result.set.Repositories[0].Name != repository.Name {
				t.Fatalf("repositories = %#v, want the one this failure belongs to", result.set.Repositories)
			}
			if row.RepositoryKey != result.set.Repositories[0].Key {
				t.Fatalf("repository key = %q, want %q", row.RepositoryKey, result.set.Repositories[0].Key)
			}
		})
	}
}

// reachedEdge names one relation the fixture must publish into the standard
// library.
type reachedEdge struct {
	source string
	kind   facts.EdgeKind
	target string
}
