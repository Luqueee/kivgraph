package rustsysroot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/rustloader"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// stdlibFixture copies the recorded crate into a private directory, because
// indexing must never touch the repository the fixture lives in.
func stdlibFixture(t *testing.T) workspace.Repository {
	t.Helper()
	root := filepath.Join(testsupport.TempDir(t), "user")
	source := filepath.Join("..", "..", "..", "testdata", "rust", "stdlib")
	if err := os.CopyFS(root, os.DirFS(source)); err != nil {
		t.Fatalf("copy Rust fixture: %v", err)
	}
	return workspace.Repository{Name: "user", Path: root, RealPath: root, Languages: []string{"rust"}}
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
// test of the Rust path by an order of magnitude. That cost is the feature,
// and it is why the test has a package to itself; see doc.go.
func TestFullIndexesTheStandardLibraryAsASyntheticProvider(t *testing.T) {
	testsupport.RequireRustAnalyzer(t)
	sysroot := requireSysrootSources(t)
	options := indexer.FullOptions{
		Repositories:        []workspace.Repository{stdlibFixture(t)},
		RustAnalyzer:        "rust-analyzer",
		RustTargetDirectory: filepath.Join(testsupport.TempDir(t), "rust-target"),
		RustBuildScripts:    true,
		RustProcMacros:      true,
		RustSysroot:         "discover",
		RustIndexSysroot:    true,
		WorkingDirectory:    testsupport.TempDir(t),
	}

	set, report, err := indexer.Full(context.Background(), options)
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
		if entry.Reason == indexer.ProviderDefinitionNotIndexed &&
			entry.RequestedSymbol == "ops::arith::impl::u32::Add<Self>::add" {
			declared = true
		}
	}
	if !declared || report.EdgesWithoutProvider == 0 {
		t.Fatalf("unresolved = %d entries, dropped = %d: want the macro-generated impl declared",
			len(set.Unresolved), report.EdgesWithoutProvider)
	}
}

// reachedEdge names one relation the fixture must publish into the standard
// library.
type reachedEdge struct {
	source string
	kind   facts.EdgeKind
	target string
}
