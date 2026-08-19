package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestFindCrossRepoConsumersReturnsAllConfidenceCategories(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(crossRepoConsumerRows(), 17, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	if response.Total != 4 || response.Returned != 4 || response.Truncated {
		t.Fatalf("response pagination = %#v, want four untruncated results", response)
	}
	// The package dependency is evidence about the package, never a use of
	// the symbol: it is counted apart from the one exact consumer.
	if response.Coverage != (Coverage{Exact: 1, Candidate: 1, UnresolvedRelated: 1, PackageLevel: 1}) {
		t.Fatalf("response coverage = %#v, want exact=1 candidate=1 unresolved=1 package=1", response.Coverage)
	}
	categories := []string{
		response.Results.Consumers[0].Category,
		response.Results.Consumers[1].Category,
		response.Results.Consumers[2].Category,
		response.Results.Consumers[3].Category,
	}
	wantCategories := []string{CrossRepoConsumerExactSymbol, CrossRepoConsumerPackage, CrossRepoConsumerCandidate, CrossRepoConsumerUnresolved}
	for index := range categories {
		if categories[index] != wantCategories[index] {
			t.Fatalf("result categories = %v, want %v", categories, wantCategories)
		}
	}
	if response.Results.Consumers[0].QualifiedName != "consumer.Exact" || response.Results.Consumers[0].EdgeKind != string(facts.References) {
		t.Fatalf("exact result = %#v", response.Results.Consumers[0])
	}
	if response.Results.Consumers[1].QualifiedName != "" || response.Results.Consumers[1].PackageName == "" {
		t.Fatalf("package result = %#v, want package-only consumer", response.Results.Consumers[1])
	}
	if response.Results.Consumers[2].Confidence != string(facts.Candidate) {
		t.Fatalf("candidate result = %#v", response.Results.Consumers[2])
	}
	unresolved := response.Results.Consumers[3]
	if unresolved.Reason != "package_not_indexed" || response.Results.Subject.QualifiedName == "" {
		t.Fatalf("unresolved result = %#v", unresolved)
	}
}

func TestFindCrossRepoConsumersIgnoresFailuresThatNamedNoSymbol(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(crossRepoConsumerRows(), 19, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	// The fixture holds a failure for the whole `example.com/target` import
	// with no requested symbol. It belongs to the package, not to every
	// symbol the package exports, so a query about one symbol never sees it.
	for _, result := range response.Results.Consumers {
		if result.UnresolvedKey == "unresolved-module" {
			t.Fatalf("symbol query returned a package-level failure: %#v", result)
		}
	}
	if response.Coverage.UnresolvedRelated != 1 {
		t.Fatalf("unresolved_related = %d, want only the failure that named this symbol", response.Coverage.UnresolvedRelated)
	}
}

func TestFindCrossRepoConsumersFiltersAndPaginatesWithSnapshotCursor(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(crossRepoConsumerRows(), 18, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	_, first, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", Language: "go", Limit: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Returned != 2 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first page = %#v, want two results and a cursor", first)
	}
	_, second, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", Language: "go", Limit: 2, Cursor: *first.NextCursor}, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Returned != 2 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second page = %#v, want final two results", second)
	}
	_, filtered, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", Repo: "consumer"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 4 {
		t.Fatalf("repo filter total = %d, want four consumer facts", filtered.Total)
	}
}

func TestFindCrossRepoConsumersIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindCrossRepoConsumers(server)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == findCrossRepoConsumersToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("find_cross_repo_consumers annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("find_cross_repo_consumers is not registered")
}

func TestFindCrossRepoConsumersCompactHoistsWhatEveryConsumerShares(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoCompactRows(), 21))
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	if response.View != ViewCompact {
		t.Fatalf("default view = %q, want %q", response.View, ViewCompact)
	}
	envelope := marshalCrossRepoResponse(t, response)
	// The two exact consumers agree on category, edge kind, confidence and
	// provenance, so those four are stated once instead of eight times.
	results := envelope["results"].(map[string]any)
	for field, want := range map[string]string{
		"category":      CrossRepoConsumerExactSymbol,
		"edge_kind":     string(facts.References),
		"confidence":    string(facts.ExactTypechecked),
		"provenance":    string(facts.GoTypesUse),
		"evidence_kind": "types",
	} {
		if results[field] != want {
			t.Fatalf("hoisted %s = %v, want %q", field, results[field], want)
		}
	}
	consumers := results["consumers"].([]any)
	if len(consumers) != 2 {
		t.Fatalf("consumers = %d, want the two exact consumers", len(consumers))
	}
	first := consumers[0].(map[string]any)
	if first["repo"] != "alpha" || first["pkg"] != "alpha" || first["at"] != "alpha.go:7" {
		t.Fatalf("first consumer = %#v, want alpha:alpha.go:7", first)
	}
	if first["qualified_name"] != "alpha.Use" {
		t.Fatalf("first consumer qualified_name = %v, want alpha.Use", first["qualified_name"])
	}
	// `name` is the last segment of `qualified_name`, `language` is the path
	// extension, and the hoisted columns are gone from the row.
	for _, field := range []string{"name", "language", "file_path", "start_line", "confidence", "provenance", "edge_kind", "category", "stable_key", "consumer_symbol_key"} {
		if _, present := first[field]; present {
			t.Fatalf("compact consumer still carries %q: %#v", field, first)
		}
	}
	coverage := envelope["coverage"].(map[string]any)
	if coverage["exact"] != float64(2) {
		t.Fatalf("coverage exact = %v, want 2", coverage["exact"])
	}
	if _, present := coverage["package_level"]; present {
		t.Fatalf("coverage names a category that counted nothing: %#v", coverage)
	}
	for _, field := range []string{"snapshot_age_ms", "truncated", "next_cursor"} {
		if _, present := envelope[field]; present {
			t.Fatalf("compact envelope still carries %q: %#v", field, envelope)
		}
	}
}

func TestFindCrossRepoConsumersCompactGroupsPackageDependenciesByRepository(t *testing.T) {
	rows := crossRepoCompactRows()
	// A second package of `alpha` depends on the provider. It is another
	// package-level fact and the same answer to the question asked: `alpha`
	// depends on `target`.
	rows.Packages = append(rows.Packages, hotsnapshot.PackageRow{
		Key: "pkg-alpha-cli", RepositoryKey: "repo-alpha", Language: "go", Name: "alpha-cli", ModulePath: "example.com/alpha/cli",
	})
	rows.PackageDependencies = append(rows.PackageDependencies,
		hotsnapshot.PackageDependencyRow{SourceKey: "pkg-alpha", TargetKey: "pkg-target", Kind: facts.CodePackageDependsOn, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodePackageManifest},
		hotsnapshot.PackageDependencyRow{SourceKey: "pkg-alpha-cli", TargetKey: "pkg-target", Kind: facts.CodePackageDependsOn, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodePackageManifest},
	)
	store := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, rows, 22))
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	// Grouping is how the page is written, never what it counts: both
	// dependencies are still facts, and they stay out of `exact`.
	if response.Coverage != (Coverage{Exact: 2, PackageLevel: 2}) {
		t.Fatalf("coverage = %#v, want exact=2 package_level=2", response.Coverage)
	}
	if response.Total != 4 {
		t.Fatalf("total = %d, want the four consumer facts", response.Total)
	}
	envelope := marshalCrossRepoResponse(t, response)
	coverage := envelope["coverage"].(map[string]any)
	if coverage["exact"] != float64(2) || coverage["package_level"] != float64(2) {
		t.Fatalf("compact coverage = %#v, want exact and package_level apart", coverage)
	}
	// Two categories, two tuples: exact_symbol and package group separately,
	// so the merge-by-repository lives inside the package group's own
	// consumers rather than a flat top-level list.
	results := envelope["results"].(map[string]any)
	if _, flat := results["consumers"]; flat {
		t.Fatalf("page stayed flat instead of grouping category exact_symbol apart from package: %#v", results)
	}
	groups, ok := results["groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("groups = %#v, want exactly two: exact_symbol and package", results["groups"])
	}
	var packageEntries int
	for _, rawGroup := range groups {
		group := rawGroup.(map[string]any)
		if group["category"] != CrossRepoConsumerPackage {
			continue
		}
		consumers := group["consumers"].([]any)
		if len(consumers) != 1 {
			t.Fatalf("package group consumers = %d, want one entry for alpha's packages", len(consumers))
		}
		consumer := consumers[0].(map[string]any)
		packageEntries++
		if consumer["repo"] != "alpha" {
			t.Fatalf("package entry repo = %v, want alpha", consumer["repo"])
		}
		names, ok := consumer["pkg"].([]any)
		if !ok || len(names) != 2 || names[0] != "alpha" || names[1] != "alpha-cli" {
			t.Fatalf("package entry pkg = %#v, want both package names", consumer["pkg"])
		}
		// A package dependency proves the repository depends on the provider,
		// never that a file uses the symbol: it has no position to open.
		if _, present := consumer["at"]; present {
			t.Fatalf("package entry claims a position: %#v", consumer)
		}
		// category is entirely accounted for by the group.
		if _, present := consumer["category"]; present {
			t.Fatalf("package entry repeats the group's category: %#v", consumer)
		}
	}
	if packageEntries != 1 {
		t.Fatalf("package entries = %d, want one per repository", packageEntries)
	}
}

func TestFindCrossRepoConsumersCompactKeepsUnresolvedProse(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoConsumerRows(), 23))
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	results := marshalCrossRepoResponse(t, response)["results"].(map[string]any)
	// The request is the call's, identical on every failure that reached this
	// page, so it is stated once instead of on each unresolved row.
	if results["requested_package"] != "example.com/target" || results["requested_symbol"] != "Target" {
		t.Fatalf("hoisted request = %#v, want example.com/target Target", results)
	}
	consumers := results["consumers"].([]any)
	if len(consumers) != 4 {
		t.Fatalf("compact consumers = %d, want the four categories", len(consumers))
	}
	unresolved := consumers[3].(map[string]any)
	// The four categories differ, so `category` stays on the row, and the
	// prose of an unresolved consumer is what the agent actually reads.
	if unresolved["category"] != CrossRepoConsumerUnresolved {
		t.Fatalf("unresolved entry category = %v", unresolved["category"])
	}
	if unresolved["reason"] != "package_not_indexed" {
		t.Fatalf("unresolved entry reason = %v", unresolved["reason"])
	}
	if unresolved["detail"] != "target package was not available during resolution" {
		t.Fatalf("unresolved entry detail = %v", unresolved["detail"])
	}
	for _, field := range []string{"requested_package", "requested_symbol"} {
		if _, present := unresolved[field]; present {
			t.Fatalf("unresolved entry repeats the hoisted %q: %#v", field, unresolved)
		}
	}
	if unresolved["at"] != "consumer.go:12" || unresolved["col"] != float64(4) {
		t.Fatalf("unresolved entry position = %#v, want consumer.go:12 col 4", unresolved)
	}
	if unresolved["name"] != "Unresolved" {
		t.Fatalf("unresolved entry name = %v, want the symbol that holds the failure", unresolved["name"])
	}
}

func TestFindCrossRepoConsumersFullViewIsUnchanged(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoConsumerRows(), 24))
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", View: ViewFull}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	envelope := marshalCrossRepoResponse(t, response)
	for _, field := range []string{"snapshot_id", "snapshot_age_ms", "total", "returned", "truncated", "next_cursor", "coverage", "results"} {
		if _, present := envelope[field]; !present {
			t.Fatalf("full envelope dropped %q: %#v", field, envelope)
		}
	}
	coverage := envelope["coverage"].(map[string]any)
	for _, field := range []string{"exact", "candidate", "unresolved_related", "package_level"} {
		if coverage[field] != float64(1) {
			t.Fatalf("full coverage %s = %v, want 1", field, coverage[field])
		}
	}
	results := envelope["results"].(map[string]any)
	if len(results) != 2 {
		t.Fatalf("full results keys = %#v, want subject and consumers only", results)
	}
	subject := results["subject"].(map[string]any)
	for _, field := range []string{"qualified_name", "repository", "package_name", "module_path", "file_path", "start_line", "end_line"} {
		if _, present := subject[field]; !present {
			t.Fatalf("full subject dropped %q: %#v", field, subject)
		}
	}
	exact := results["consumers"].([]any)[0].(map[string]any)
	for _, field := range []string{"category", "name", "qualified_name", "kind", "edge_kind", "repository", "package_name", "file_path", "confidence", "provenance", "evidence_kind"} {
		if _, present := exact[field]; !present {
			t.Fatalf("full row dropped %q: %#v", field, exact)
		}
	}
	// A row whose declaration has a range still spells both ends of it, where
	// the compact view states the start inside `at`.
	ranged := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoCompactRows(), 26))
	_, response, err = findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", View: ViewFull}, ranged)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	rows := marshalCrossRepoResponse(t, response)["results"].(map[string]any)["consumers"].([]any)
	first := rows[0].(map[string]any)
	if first["start_line"] != float64(7) || first["end_line"] != float64(7) {
		t.Fatalf("full row range = %#v, want start and end line 7", first)
	}
	second := rows[1].(map[string]any)
	if second["start_line"] != float64(11) || second["end_line"] != float64(14) {
		t.Fatalf("full row range = %#v, want lines 11 to 14", second)
	}
}

func TestFindCrossRepoConsumersRejectsUnsupportedView(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoConsumerRows(), 25))
	// The answer is a set of repositories, so `files` is refused instead of
	// silently answering something else.
	for _, view := range []string{ViewFiles, "summary"} {
		_, _, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", View: view}, store)
		if ErrorCode(err) != CodeInvalidArgument {
			t.Fatalf("view %q error code = %q, want %q", view, ErrorCode(err), CodeInvalidArgument)
		}
	}
}

func buildCrossRepoSnapshot(t *testing.T, rows hotsnapshot.LadybugSnapshotRows, id uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

func marshalCrossRepoResponse(t *testing.T, response Response[CrossRepoConsumers]) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response error = %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal response error = %v", err)
	}
	return decoded
}

// crossRepoCompactRows holds two exact consumers in two repositories, which is
// what lets a whole page share its category, edge kind, confidence and
// provenance.
func crossRepoCompactRows() hotsnapshot.LadybugSnapshotRows {
	return hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-target", Name: "target", Languages: "go"},
			{Key: "repo-alpha", Name: "alpha", Languages: "go"},
			{Key: "repo-beta", Name: "beta", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-target", RepositoryKey: "repo-target", Language: "go", Name: "target", ModulePath: "example.com/target"},
			{Key: "pkg-alpha", RepositoryKey: "repo-alpha", Language: "go", Name: "alpha", ModulePath: "example.com/alpha"},
			{Key: "pkg-beta", RepositoryKey: "repo-beta", Language: "go", Name: "beta", ModulePath: "example.com/beta"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-target", RepositoryKey: "repo-target", PackageKey: "pkg-target", Path: "target.go", Language: "go"},
			{Key: "file-alpha", RepositoryKey: "repo-alpha", PackageKey: "pkg-alpha", Path: "alpha.go", Language: "go"},
			{Key: "file-beta", RepositoryKey: "repo-beta", PackageKey: "pkg-beta", Path: "beta.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-target", CanonicalIdentity: "go:target.Target", FileKey: "file-target", Language: "go", Name: "Target", QualifiedName: "target.Target", Kind: "func", StartLine: 3, EndLine: 9},
			{StableKey: "sym-alpha", CanonicalIdentity: "go:alpha.Use", FileKey: "file-alpha", Language: "go", Name: "Use", QualifiedName: "alpha.Use", Kind: "func", StartLine: 7, EndLine: 7},
			{StableKey: "sym-beta", CanonicalIdentity: "go:beta.Use", FileKey: "file-beta", Language: "go", Name: "Use", QualifiedName: "beta.Use", Kind: "func", StartLine: 11, EndLine: 14},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-alpha", TargetKey: "sym-target", Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-alpha", EvidenceTargetFileKey: "file-target"},
			{SourceKey: "sym-beta", TargetKey: "sym-target", Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-beta", EvidenceTargetFileKey: "file-target"},
		},
	}
}

func crossRepoConsumerRows() hotsnapshot.LadybugSnapshotRows {
	return hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-target", Name: "target", Languages: "go"},
			{Key: "repo-consumer", Name: "consumer", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-target", RepositoryKey: "repo-target", Language: "go", Name: "target", ModulePath: "example.com/target"},
			{Key: "pkg-consumer", RepositoryKey: "repo-consumer", Language: "go", Name: "consumer", ModulePath: "example.com/consumer"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-target", RepositoryKey: "repo-target", PackageKey: "pkg-target", Path: "target.go", Language: "go"},
			{Key: "file-consumer", RepositoryKey: "repo-consumer", PackageKey: "pkg-consumer", Path: "consumer.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-target", CanonicalIdentity: "go:target.Target", FileKey: "file-target", Language: "go", Name: "Target", QualifiedName: "target.Target", Kind: "func"},
			{StableKey: "sym-exact", CanonicalIdentity: "go:consumer.Exact", FileKey: "file-consumer", Language: "go", Name: "Exact", QualifiedName: "consumer.Exact", Kind: "func"},
			{StableKey: "sym-candidate", CanonicalIdentity: "go:consumer.Candidate", FileKey: "file-consumer", Language: "go", Name: "Candidate", QualifiedName: "consumer.Candidate", Kind: "func"},
			{StableKey: "sym-unresolved", CanonicalIdentity: "go:consumer.Unresolved", FileKey: "file-consumer", Language: "go", Name: "Unresolved", QualifiedName: "consumer.Unresolved", Kind: "func"},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-exact", TargetKey: "sym-target", Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-consumer", EvidenceTargetFileKey: "file-target"},
			{SourceKey: "sym-candidate", TargetKey: "sym-target", Kind: facts.CodeReferences, Confidence: facts.CodeCandidate, Provenance: facts.CodeTreeSitterSyntax, EvidenceKind: "syntax", EvidenceSourceFileKey: "file-consumer", EvidenceTargetFileKey: "file-target"},
		},
		PackageDependencies: []hotsnapshot.PackageDependencyRow{
			{SourceKey: "pkg-consumer", TargetKey: "pkg-target", Kind: facts.CodePackageDependsOn, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodePackageManifest},
		},
		Unresolved: []hotsnapshot.UnresolvedReferenceRow{
			{Key: "unresolved-consumer", RepositoryKey: "repo-consumer", FileKey: "file-consumer", SourceKey: "sym-unresolved", Language: "go", RequestedPackage: "example.com/target", RequestedSymbol: "Target", Reason: "package_not_indexed", Detail: "target package was not available during resolution", StartLine: 12, StartColumn: 4, StartOffset: 180},
			// A failure that names the package and no symbol: the whole
			// import broke, so no symbol was ever requested.
			{Key: "unresolved-module", RepositoryKey: "repo-consumer", FileKey: "file-consumer", Language: "go", RequestedPackage: "example.com/target", Reason: "module_not_resolved", Detail: "example.com/target is not installed", StartLine: 3, StartColumn: 1, StartOffset: 20},
		},
	}
}

// crossRepoFanInRows is the shape that motivated the second tier of hoisting:
// four repositories depend on the provider package, and three files across
// two other repositories fail to resolve the same import for the same
// reason, with the same `detail` sentence -- a template, not each row's own
// prose.
func crossRepoFanInRows() hotsnapshot.LadybugSnapshotRows {
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-target", Name: "target", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-target", RepositoryKey: "repo-target", Language: "go", Name: "target", ModulePath: "example.com/target"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-target", RepositoryKey: "repo-target", PackageKey: "pkg-target", Path: "target.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-target", CanonicalIdentity: "go:target.Target", FileKey: "file-target", Language: "go", Name: "Target", QualifiedName: "target.Target", Kind: "func", StartLine: 3, EndLine: 9},
		},
	}
	for index := 1; index <= 4; index++ {
		suffix := string(rune('a' - 1 + index))
		repoKey, pkgKey := "repo-dep-"+suffix, "pkg-dep-"+suffix
		rows.Repositories = append(rows.Repositories, hotsnapshot.RepositoryRow{Key: repoKey, Name: "dep-" + suffix, Languages: "go"})
		rows.Packages = append(rows.Packages, hotsnapshot.PackageRow{Key: pkgKey, RepositoryKey: repoKey, Language: "go", Name: "dep-" + suffix, ModulePath: "example.com/dep-" + suffix})
		rows.PackageDependencies = append(rows.PackageDependencies,
			hotsnapshot.PackageDependencyRow{SourceKey: pkgKey, TargetKey: "pkg-target", Kind: facts.CodePackageDependsOn, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodePackageManifest})
	}
	for index := 1; index <= 3; index++ {
		suffix := string(rune('a' - 1 + index))
		repoKey, pkgKey, fileKey := "repo-fail-"+suffix, "pkg-fail-"+suffix, "file-fail-"+suffix
		rows.Repositories = append(rows.Repositories, hotsnapshot.RepositoryRow{Key: repoKey, Name: "fail-" + suffix, Languages: "typescript"})
		rows.Packages = append(rows.Packages, hotsnapshot.PackageRow{Key: pkgKey, RepositoryKey: repoKey, Language: "typescript", Name: "fail-" + suffix, ModulePath: "@fail/" + suffix})
		rows.Files = append(rows.Files, hotsnapshot.FileRow{Key: fileKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "src/index.ts", Language: "typescript"})
		rows.Unresolved = append(rows.Unresolved, hotsnapshot.UnresolvedReferenceRow{
			Key: "unresolved-" + suffix, RepositoryKey: repoKey, FileKey: fileKey, Language: "typescript",
			RequestedPackage: "example.com/target", RequestedSymbol: "Target",
			Reason: "DECLARATION_SOURCE_NOT_MAPPED", Detail: "dist/target.d.ts is not mapped to a source file",
			StartLine: uint32(10 * index), StartColumn: 1, StartOffset: uint32(100 * index),
		})
	}
	return rows
}

// TestFindCrossRepoConsumersCompactGroupsTheMajorityTupleOnce is the
// regression guard for the real page that motivated this: a 35-consumer page
// over `kena` had 22 package dependencies sharing one tuple and 13 unresolved
// rows collapsing to two (reason, detail) pairs -- `detail` was assumed to be
// each row's own prose and was in fact a template repeated verbatim.
// Grouping cut that page from 2.202 to 518 tokens.
func TestFindCrossRepoConsumersCompactGroupsTheMajorityTupleOnce(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoFanInRows(), 24))
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	if response.Coverage != (Coverage{UnresolvedRelated: 3, PackageLevel: 4}) {
		t.Fatalf("coverage = %#v, want unresolved_related=3 package_level=4", response.Coverage)
	}

	results := marshalCrossRepoResponse(t, response)["results"].(map[string]any)
	if _, flat := results["consumers"]; flat {
		t.Fatalf("page stayed flat instead of grouping package apart from unresolved: %#v", results)
	}
	groups, ok := results["groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("groups = %#v, want exactly two: package and unresolved", results["groups"])
	}
	for _, rawGroup := range groups {
		group := rawGroup.(map[string]any)
		consumers := group["consumers"].([]any)
		switch group["category"] {
		case CrossRepoConsumerPackage:
			// Four repositories, four independent package dependencies, none
			// of them sharing a repository, so no further per-repository
			// merge applies -- four consumer rows, each a bare repo and pkg.
			if len(consumers) != 4 {
				t.Fatalf("package group consumers = %d, want 4", len(consumers))
			}
			for _, rawConsumer := range consumers {
				consumer := rawConsumer.(map[string]any)
				if _, present := consumer["category"]; present {
					t.Fatalf("package row repeats the group's category: %#v", consumer)
				}
			}
		case CrossRepoConsumerUnresolved:
			if group["reason"] != "DECLARATION_SOURCE_NOT_MAPPED" {
				t.Fatalf("unresolved group reason = %v", group["reason"])
			}
			// The three rows share one detail sentence, so it hoists to the
			// group and none of them repeats it.
			if group["detail"] != "dist/target.d.ts is not mapped to a source file" {
				t.Fatalf("unresolved group detail = %v, want the shared sentence hoisted", group["detail"])
			}
			if len(consumers) != 3 {
				t.Fatalf("unresolved group consumers = %d, want 3", len(consumers))
			}
			for _, rawConsumer := range consumers {
				consumer := rawConsumer.(map[string]any)
				if _, present := consumer["detail"]; present {
					t.Fatalf("unresolved row repeats the group's detail: %#v", consumer)
				}
				if _, present := consumer["reason"]; present {
					t.Fatalf("unresolved row repeats the group's reason: %#v", consumer)
				}
				// What still differs per row -- which file and offset failed
				// -- has to survive: three real occurrences, not one.
				if consumer["at"] == nil {
					t.Fatalf("unresolved row lost its file: %#v", consumer)
				}
			}
		default:
			t.Fatalf("unexpected group category %v", group["category"])
		}
	}
}
