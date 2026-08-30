package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFindSymbolSelectsANamedProfileAndScopesTheEnvelope(t *testing.T) {
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 11)),
		"other":   hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 29)),
	})
	if err != nil {
		t.Fatal(err)
	}
	session := newSymbolToolClient(t, aggregate)
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "find_symbol",
		Arguments: map[string]any{"name": "alpha", "profile": []any{"other"}},
	})
	if err != nil || result.IsError {
		t.Fatalf("CallTool() = %#v, %v", result, err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*sdkmcp.TextContent).Text), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["profile"] != "other" || envelope["snapshot_id"] != float64(29) {
		t.Fatalf("profile envelope = %#v", envelope)
	}
}

func TestOneProfileKeepsTheHistoricalResponseBytes(t *testing.T) {
	snapshot := buildSymbolSnapshot(t, 11)
	direct := newSymbolToolClient(t, hotsnapshot.NewSnapshotStore(snapshot))
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": hotsnapshot.NewSnapshotStore(snapshot),
	})
	if err != nil {
		t.Fatal(err)
	}
	scoped := newSymbolToolClient(t, aggregate)
	arguments := map[string]any{"name": "alpha"}
	directResult, err := direct.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "find_symbol", Arguments: arguments})
	if err != nil || directResult.IsError {
		t.Fatalf("direct CallTool() = %#v, %v", directResult, err)
	}
	scopedResult, err := scoped.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "find_symbol", Arguments: arguments})
	if err != nil || scopedResult.IsError {
		t.Fatalf("profile CallTool() = %#v, %v", scopedResult, err)
	}
	directText := directResult.Content[0].(*sdkmcp.TextContent).Text
	scopedText := scopedResult.Content[0].(*sdkmcp.TextContent).Text
	if directText != scopedText {
		t.Fatalf("single-profile response changed:\n direct: %s\nprofile: %s", directText, scopedText)
	}
}

func TestFindSymbolUnionDeduplicatesAndPinsTheProfileSet(t *testing.T) {
	defaultStore := hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 11))
	otherStore := hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 29))
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": defaultStore,
		"other":   otherStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := newSymbolToolClient(t, aggregate)
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "find_symbol",
		Arguments: map[string]any{
			"name": "shared", "profile": []any{"other", "default"}, "limit": 1, "view": "full",
		},
	})
	if err != nil || result.IsError {
		t.Fatalf("union CallTool() = %#v, %v", result, err)
	}
	var envelope struct {
		Total             int               `json:"total"`
		Returned          int               `json:"returned"`
		NextCursor        string            `json:"next_cursor"`
		CrossProfileEdges string            `json:"cross_profile_edges"`
		Profiles          []ProfileSnapshot `json:"profiles"`
		Results           []struct {
			Profiles []string `json:"profile"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*sdkmcp.TextContent).Text), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Total != 2 || envelope.Returned != 1 || envelope.NextCursor == "" || envelope.CrossProfileEdges != "not_resolved" || len(envelope.Profiles) != 2 || len(envelope.Results) != 1 {
		t.Fatalf("union envelope = %#v", envelope)
	}
	if got := envelope.Results[0].Profiles; len(got) != 2 || got[0] != "default" || got[1] != "other" {
		t.Fatalf("deduplicated row profiles = %v, want [default other]", got)
	}
	if err := otherStore.Publish(buildSymbolSnapshot(t, 30)); err != nil {
		t.Fatalf("Publish(other) error = %v", err)
	}
	refusedAfterPublish, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "find_symbol",
		Arguments: map[string]any{
			"name": "shared", "profile": []any{"default", "other"}, "limit": 1,
			"cursor": envelope.NextCursor, "view": "full",
		},
	})
	if err != nil || refusedAfterPublish == nil || !refusedAfterPublish.IsError {
		t.Fatalf("cursor reused after one profile published = %#v, %v", refusedAfterPublish, err)
	}
	refused, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "find_symbol",
		Arguments: map[string]any{
			"name": "shared", "profile": []any{"other"}, "limit": 1, "cursor": envelope.NextCursor, "view": "full",
		},
	})
	if err != nil || refused == nil || !refused.IsError {
		t.Fatalf("cursor reused with another profile set = %#v, %v", refused, err)
	}
}

func TestStableKeyNeedsAnExplicitProfileWhenSeveralExist(t *testing.T) {
	if err := RequireStableKeyProfile(2, "stable-key", nil); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("RequireStableKeyProfile() error = %v, want INVALID_ARGUMENT", err)
	}
	if err := RequireStableKeyProfile(1, "stable-key", nil); err != nil {
		t.Fatalf("single-profile stable key error = %v", err)
	}
	if err := RequireStableKeyProfile(2, "stable-key", []string{"default"}); err != nil {
		t.Fatalf("explicit-profile stable key error = %v", err)
	}
	if err := RequireStableKeyProfile(2, "stable-key", []string{"default", "other"}); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("multi-profile stable key error = %v, want INVALID_ARGUMENT", err)
	}
	if err := RequireStableKeyProfile(2, "stable-key", []string{"*"}); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("all-profile stable key error = %v, want INVALID_ARGUMENT", err)
	}
}

func TestStableKeyRoutingChangesOnlyAfterASecondProfileExists(t *testing.T) {
	one, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": getSymbolSnapshot(t, 61),
	})
	if err != nil {
		t.Fatal(err)
	}
	oneClient := newGetSymbolToolClient(t, one)
	accepted, err := oneClient.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "get_symbol", Arguments: map[string]any{"stable_key": "symbol-alpha"},
	})
	if err != nil || accepted.IsError {
		t.Fatalf("one-profile stable key = %#v, %v", accepted, err)
	}

	many, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": getSymbolSnapshot(t, 62),
		"other":   getSymbolSnapshot(t, 63),
	})
	if err != nil {
		t.Fatal(err)
	}
	manyClient := newGetSymbolToolClient(t, many)
	refused, err := manyClient.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "get_symbol", Arguments: map[string]any{"stable_key": "symbol-alpha"},
	})
	if err != nil || refused == nil || !refused.IsError {
		t.Fatalf("unscoped multi-profile stable key = %#v, %v", refused, err)
	}
	accepted, err = manyClient.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "get_symbol", Arguments: map[string]any{
			"stable_key": "symbol-alpha", "profile": []any{"other"},
		},
	})
	if err != nil || accepted.IsError {
		t.Fatalf("scoped multi-profile stable key = %#v, %v", accepted, err)
	}
}

func TestEmptyReferencesNameAnotherProfileContainingTheRepository(t *testing.T) {
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": referenceSnapshot(t, 41),
		"other":   referenceSnapshot(t, 42),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := newFindReferencesToolClient(t, aggregate)
	response := callFindReferences(t, client, map[string]any{
		"profile": []any{"default"}, "name": "target", "repo": "missing",
	})
	if response.Total != 0 {
		t.Fatalf("Total = %d, want empty filtered answer", response.Total)
	}
	for _, want := range []string{"repo-a", "other", "scoped to profile default"} {
		if !strings.Contains(response.Guidance, want) {
			t.Fatalf("guidance = %q, want %q", response.Guidance, want)
		}
	}
}

func TestMultiProfileCompletenessUsesTheWeakestProfile(t *testing.T) {
	blinded := completenessSnapshot(t, 52, hotsnapshot.UnresolvedReferenceRow{
		Key: "unresolved-absent", RepositoryKey: "repo-core", Language: "go",
		RequestedPackage: "example.com/core/hidden", RequestedSymbol: "Absent",
		Reason: "PACKAGE_NOT_BUILDABLE", Detail: "build constraints excluded the package",
	})
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("clean", map[string]*hotsnapshot.SnapshotStore{
		"clean":   completenessSnapshot(t, 51),
		"blinded": blinded,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := newSymbolToolClient(t, aggregate)
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "find_symbol", Arguments: map[string]any{"name": "Absent", "profile": []any{"*"}, "view": "full"},
	})
	if err != nil || result.IsError {
		t.Fatalf("CallTool() = %#v, %v", result, err)
	}
	var envelope struct {
		Completeness Completeness      `json:"completeness"`
		Profiles     []ProfileSnapshot `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*sdkmcp.TextContent).Text), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Completeness.Verdict != VerdictLowerBound {
		t.Fatalf("merged verdict = %q, want %s", envelope.Completeness.Verdict, VerdictLowerBound)
	}
	if len(envelope.Profiles) != 2 || envelope.Profiles[0].Completeness == nil || envelope.Profiles[1].Completeness == nil {
		t.Fatalf("profile completeness = %#v", envelope.Profiles)
	}
	if envelope.Profiles[0].Completeness.Verdict != VerdictLowerBound || envelope.Profiles[1].Completeness.Verdict != VerdictComplete {
		t.Fatalf("canonical profile verdicts = %#v", envelope.Profiles)
	}
}

func TestProfileSetSnapshotIDPinsEveryGenerationAndCanonicalisesOrder(t *testing.T) {
	first := []ProfileSnapshot{{Name: "a", SnapshotID: 7}, {Name: "b", SnapshotID: 9}}
	reversed := []ProfileSnapshot{{Name: "b", SnapshotID: 9}, {Name: "a", SnapshotID: 7}}
	if ProfileSetSnapshotID(first) != ProfileSetSnapshotID(reversed) {
		t.Fatal("equivalent profile sets produced different cursor identities")
	}
	changed := []ProfileSnapshot{{Name: "a", SnapshotID: 8}, {Name: "b", SnapshotID: 9}}
	if ProfileSetSnapshotID(first) == ProfileSetSnapshotID(changed) {
		t.Fatal("publishing one profile did not invalidate the cursor identity")
	}
	removed := []ProfileSnapshot{{Name: "a", SnapshotID: 7}}
	if ProfileSetSnapshotID(first) == ProfileSetSnapshotID(removed) {
		t.Fatal("changing the profile set did not invalidate the cursor identity")
	}
}

func TestRemainingQueryToolsAcceptAMultiProfileUnion(t *testing.T) {
	symbolProfiles := func() []hotsnapshot.ProfileStore {
		aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
			"default": hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 71)),
			"other":   hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 72)),
		})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := aggregate.ResolveProfiles([]string{"*"})
		if err != nil {
			t.Fatal(err)
		}
		return selected
	}
	assertUnion := func(t *testing.T, profiles []ProfileSnapshot, cross string, total int) {
		t.Helper()
		if len(profiles) != 2 || cross != "not_resolved" || total == 0 {
			t.Fatalf("union = profiles:%#v cross:%q total:%d", profiles, cross, total)
		}
	}

	t.Run("find_by_intent", func(t *testing.T) {
		_, response, err := findByIntentAcrossProfiles(context.Background(), nil, FindByIntentInput{
			Intent: "alpha", Limit: 10, View: ViewFull,
		}, symbolProfiles())
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
	t.Run("get_symbol", func(t *testing.T) {
		profiles := []hotsnapshot.ProfileStore{
			{Name: "default", Store: getSymbolSnapshot(t, 73)},
			{Name: "other", Store: getSymbolSnapshot(t, 74)},
		}
		_, response, err := getSymbolAcrossProfiles(context.Background(), nil, GetSymbolInput{
			QualifiedName: "pkg.Alpha", Repository: "alpha-repo", Path: "src/alpha.go",
		}, profiles)
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
		if len(response.Results.Variants) != 1 || string(response.Results.Variants[0].Profiles) != "default\x00other" {
			t.Fatalf("symbol variants = %#v", response.Results.Variants)
		}
	})
	t.Run("get_source", func(t *testing.T) {
		_, response, err := getSourceAcrossProfiles(context.Background(), nil, GetSourceInput{
			Symbols: []SourceRequest{{QualifiedName: "pkg.Alpha", Repository: "repo-a", Path: "alpha.go"}},
		}, symbolProfiles())
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
	t.Run("get_file_outline", func(t *testing.T) {
		_, response, err := getFileOutlineAcrossProfiles(context.Background(), nil, GetFileOutlineInput{
			Repository: "repo-a", Path: "alpha.go", Limit: 20, View: ViewFull,
		}, symbolProfiles())
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
	t.Run("find_references", func(t *testing.T) {
		profiles := []hotsnapshot.ProfileStore{{Name: "default", Store: referenceSnapshot(t, 75)}, {Name: "other", Store: referenceSnapshot(t, 76)}}
		_, response, err := findReferencesAcrossProfiles(context.Background(), nil, FindReferencesInput{Name: "target", View: ViewFull}, profiles)
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
	t.Run("find_cross_repo_consumers", func(t *testing.T) {
		profiles := []hotsnapshot.ProfileStore{
			{Name: "default", Store: hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoConsumerRows(), 77))},
			{Name: "other", Store: hotsnapshot.NewSnapshotStore(buildCrossRepoSnapshot(t, crossRepoConsumerRows(), 78))},
		}
		_, response, err := findCrossRepoConsumersAcrossProfiles(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", View: ViewFull}, profiles)
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
	t.Run("trace_dependencies", func(t *testing.T) {
		profiles := []hotsnapshot.ProfileStore{{Name: "default", Store: referenceSnapshot(t, 79)}, {Name: "other", Store: referenceSnapshot(t, 80)}}
		_, response, err := traceDependenciesAcrossProfiles(context.Background(), nil, TraceDependenciesInput{
			QualifiedName: "pkg.Target", Repository: "repo-a", Path: "src/caller.go", Depth: 2, View: ViewFull,
		}, profiles)
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
	t.Run("get_blast_radius", func(t *testing.T) {
		profiles := []hotsnapshot.ProfileStore{{Name: "default", Store: referenceSnapshot(t, 81)}, {Name: "other", Store: referenceSnapshot(t, 82)}}
		_, response, err := getBlastRadiusAcrossProfiles(context.Background(), nil, GetBlastRadiusInput{
			QualifiedName: "pkg.Target", Repository: "repo-a", Path: "src/caller.go", Depth: 2, Kinds: []string{"*"}, View: ViewFull,
		}, profiles)
		if err != nil {
			t.Fatal(err)
		}
		assertUnion(t, response.Profiles, response.CrossProfileEdges, response.Total)
	})
}

func TestDiscoveryToolsEnumerateEveryProfile(t *testing.T) {
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 91)),
		"other":   hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, 92)),
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := aggregate.ResolveProfiles([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	_, repositories, err := listRepositoriesAcrossProfiles(context.Background(), nil, ListRepositoriesInput{Limit: 20}, selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories.Profiles) != 2 || repositories.Total != 4 {
		t.Fatalf("repositories = %#v", repositories)
	}
	for _, row := range repositories.Results {
		if row.Profile == "" {
			t.Fatalf("repository has no profile: %#v", row)
		}
	}
	_, status, err := graphStatusAcrossProfiles(context.Background(), nil, selected, "default", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status.Total != 2 || len(status.Results.Profiles) != 2 || !status.Results.Profiles[0].Default {
		t.Fatalf("status = %#v", status)
	}
}
