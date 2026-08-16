package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestBlastRadiusVerdictTurnsOnOneRecordedFailure is the guard the whole
// contract rests on. A verdict that never moves is decoration: the same query
// against the same graph must answer COMPLETE with nothing recorded, and
// LOWER_BOUND once a single failure that could belong to it exists.
func TestBlastRadiusVerdictTurnsOnOneRecordedFailure(t *testing.T) {
	clean := completenessSnapshot(t, 51)
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, clean)
	if err != nil {
		t.Fatalf("get_blast_radius error = %v", err)
	}
	if response.Completeness == nil || response.Completeness.Verdict != VerdictComplete {
		t.Fatalf("clean graph verdict = %#v, want %s", response.Completeness, VerdictComplete)
	}
	if response.Coverage.UnresolvedRelated != 0 {
		t.Fatalf("clean graph unresolved_related = %d, want 0", response.Coverage.UnresolvedRelated)
	}

	// One failed reference that asked for a member of the same name.
	blinded := completenessSnapshot(t, 52, hotsnapshot.UnresolvedReferenceRow{
		Key: "unresolved-core", RepositoryKey: "repo-app", FileKey: "file-app",
		Language: "go", RequestedPackage: "example.com/vendor", RequestedSymbol: "Client.Core",
		Reason: "MODULE_PROVIDER_NOT_FOUND", Detail: "no registered repository provides this module",
		StartLine: 328, StartColumn: 13,
	})
	_, response, err = getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, blinded)
	if err != nil {
		t.Fatalf("get_blast_radius error = %v", err)
	}
	if response.Completeness == nil || response.Completeness.Verdict != VerdictLowerBound {
		t.Fatalf("blinded verdict = %#v, want %s", response.Completeness, VerdictLowerBound)
	}
	if response.Coverage.UnresolvedRelated != 1 {
		t.Fatalf("blinded unresolved_related = %d, want 1", response.Coverage.UnresolvedRelated)
	}
	spots := response.Completeness.BlindSpots
	if len(spots) != 1 {
		t.Fatalf("blind spots = %#v, want exactly the recorded one", spots)
	}
	// The coordinates are the point: an agent must be able to go and look.
	if spots[0].FilePath != "app.go" || spots[0].StartLine != 328 {
		t.Fatalf("blind spot location = %#v, want app.go:328", spots[0])
	}
	if spots[0].Reason != "MODULE_PROVIDER_NOT_FOUND" || spots[0].RequestedSymbol != "Client.Core" {
		t.Fatalf("blind spot evidence = %#v", spots[0])
	}
	if response.Completeness.Fallback == nil || response.Completeness.Fallback.Pattern != `\bCore\b` {
		t.Fatalf("fallback = %#v, want the bounded search that closes the gap", response.Completeness.Fallback)
	}
}

// TestCompletenessSeparatesAFailedReferenceFromAnUnreadableScope keeps the two
// apart: one bounds an answer about a symbol, the other bounds every answer
// about the repository. The snapshot tells them apart by whether the failure
// names a file, not by matching reason strings.
func TestCompletenessSeparatesAFailedReferenceFromAnUnreadableScope(t *testing.T) {
	store := completenessSnapshot(t, 53, hotsnapshot.UnresolvedReferenceRow{
		Key: "unresolved-scope", RepositoryKey: "repo-core", Language: "go",
		RequestedPackage: "example.com/core/benchmarks", Reason: "PACKAGE_NOT_BUILDABLE",
		Detail: "build constraints exclude all Go files in /repo-core/benchmarks",
	})
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatalf("get_blast_radius error = %v", err)
	}
	if response.Completeness.Verdict != VerdictLowerBound {
		t.Fatalf("verdict = %q, want %s", response.Completeness.Verdict, VerdictLowerBound)
	}
	if len(response.Completeness.BlindSpots) != 0 {
		t.Fatalf("a scope failure must not be reported as a failed reference: %#v", response.Completeness.BlindSpots)
	}
	scopes := response.Completeness.InvisibleScopes
	if len(scopes) != 1 || scopes[0].Reason != "PACKAGE_NOT_BUILDABLE" || scopes[0].FilePath != "" {
		t.Fatalf("invisible scopes = %#v", scopes)
	}
	// The directory the loader named is what the fallback has to search.
	if response.Completeness.Fallback == nil ||
		len(response.Completeness.Fallback.Paths) != 1 ||
		response.Completeness.Fallback.Paths[0] != "/repo-core/benchmarks" {
		t.Fatalf("fallback paths = %#v, want the excluded directory", response.Completeness.Fallback)
	}
}

// TestFindSymbolReportsWhatItCouldNotResolve is the empty-result case: zero
// hits and zero doubt is a claim that the name does not exist.
func TestFindSymbolReportsWhatItCouldNotResolve(t *testing.T) {
	store := completenessSnapshot(t, 54, hotsnapshot.UnresolvedReferenceRow{
		Key: "unresolved-connection", RepositoryKey: "repo-app", FileKey: "file-app",
		Language: "go", RequestedPackage: "example.com/vendor", RequestedSymbol: "Connection.Close",
		Reason: "MODULE_PROVIDER_NOT_FOUND", Detail: "no registered repository provides this module",
		StartLine: 12,
	})
	_, response, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "Close"}, store)
	if err != nil {
		t.Fatalf("find_symbol error = %v", err)
	}
	if response.Total != 0 {
		t.Fatalf("total = %d, want no exact match", response.Total)
	}
	if response.Coverage.UnresolvedRelated != 1 {
		t.Fatalf("unresolved_related = %d, want the recorded failure counted", response.Coverage.UnresolvedRelated)
	}

	// A near miss is not a match: Closer is a different name.
	_, other, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "Closer"}, store)
	if err != nil {
		t.Fatalf("find_symbol error = %v", err)
	}
	if other.Coverage.UnresolvedRelated != 0 {
		t.Fatalf("unresolved_related for a different name = %d, want 0", other.Coverage.UnresolvedRelated)
	}
}

func TestRegexpQuoteWordEscapesWhatANameMayContain(t *testing.T) {
	if got := regexpQuoteWord("Set.Merge"); got != `Set\.Merge` {
		t.Fatalf("regexpQuoteWord() = %q", got)
	}
	if got := regexpQuoteWord("Plain"); got != "Plain" {
		t.Fatalf("regexpQuoteWord() = %q", got)
	}
}

func TestScopeDirectoryReadsTheLoaderDetail(t *testing.T) {
	detail := "build constraints exclude all Go files in /repo/benchmarks/x"
	if got := scopeDirectory(detail); got != "/repo/benchmarks/x" {
		t.Fatalf("scopeDirectory() = %q", got)
	}
	if got := scopeDirectory("nothing to read here"); got != "" {
		t.Fatalf("scopeDirectory() = %q, want empty", got)
	}
	if got := scopeDirectory(""); got != "" {
		t.Fatalf("scopeDirectory() = %q, want empty", got)
	}
	if strings.TrimSpace(scopeDirectory("in ")) != "" {
		t.Fatalf("scopeDirectory() must not invent a directory")
	}
}

// TestRootSelectorAcceptsAQualifiedName lets an agent that read a diff or a
// stack trace ask directly, instead of running a search to turn a name it
// already has into a key.
func TestRootSelectorAcceptsAQualifiedName(t *testing.T) {
	store := completenessSnapshot(t, 55)
	_, byName, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{QualifiedName: "core.Core"}, store)
	if err != nil {
		t.Fatalf("get_blast_radius by qualified name error = %v", err)
	}
	_, byKey, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatalf("get_blast_radius by stable key error = %v", err)
	}
	if byName.Results.RootKey != byKey.Results.RootKey || byName.Total != byKey.Total {
		t.Fatalf("qualified name answered differently: %#v vs %#v", byName.Results, byKey.Results)
	}

	_, trace, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{QualifiedName: "app.Caller"}, store)
	if err != nil {
		t.Fatalf("trace_dependencies by qualified name error = %v", err)
	}
	if trace.Results.RootKey != "sym-caller" {
		t.Fatalf("trace root = %q, want sym-caller", trace.Results.RootKey)
	}

	// Neither selector, and both at once, are both mistakes worth naming.
	if _, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("no selector error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", QualifiedName: "core.Core",
	}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("both selectors error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		QualifiedName: "core.Ghost",
	}, store); ErrorCode(err) != CodeSymbolNotFound {
		t.Fatalf("unknown qualified name error code = %q, want %q", ErrorCode(err), CodeSymbolNotFound)
	}
}

// completenessSnapshot is one caller in repo-app reaching one symbol in
// repo-core, plus whatever failures the case needs.
func completenessSnapshot(t *testing.T, id uint64, unresolved ...hotsnapshot.UnresolvedReferenceRow) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-app", Name: "app", Languages: "go"},
				{Key: "repo-core", Name: "core", Languages: "go"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "pkg-app", RepositoryKey: "repo-app", Language: "go", Name: "app", ModulePath: "example.com/app"},
				{Key: "pkg-core", RepositoryKey: "repo-core", Language: "go", Name: "core", ModulePath: "example.com/core"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-app", RepositoryKey: "repo-app", PackageKey: "pkg-app", Path: "app.go", Language: "go"},
				{Key: "file-core", RepositoryKey: "repo-core", PackageKey: "pkg-core", Path: "core.go", Language: "go"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{
					StableKey: "sym-caller", CanonicalIdentity: "go:app.Caller", FileKey: "file-app",
					Language: "go", Name: "Caller", QualifiedName: "app.Caller", Kind: "func",
					Exported: true, StartLine: 1, EndLine: 5,
				},
				{
					StableKey: "sym-core", CanonicalIdentity: "go:core.Core", FileKey: "file-core",
					Language: "go", Name: "Core", QualifiedName: "core.Core", Kind: "func",
					Exported: true, StartLine: 1, EndLine: 9,
				},
			},
			Edges: []hotsnapshot.EdgeRow{{
				SourceKey: "sym-caller", TargetKey: "sym-core",
				Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
				EvidenceKey: "evidence-call", EvidenceKind: "types",
				EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-core",
			}},
			Unresolved: unresolved,
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}
