package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
)

func TestHandlerMetaAndReadOnlyMethod(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("meta status = %d, want %d", response.Code, http.StatusOK)
	}
	var meta metaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.APIVersion != APIVersion || meta.Status != "ready" || meta.SnapshotID == nil || *meta.SnapshotID != 7 {
		t.Fatalf("meta = %#v", meta)
	}
	if meta.Counts.Symbols != 3 || meta.Counts.Edges != 2 {
		t.Fatalf("meta counts = %#v", meta.Counts)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/meta", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
}

func TestHandlerWebBundleFallback(t *testing.T) {
	handler := NewHandler(nil)

	for _, path := range []string{"/", "/assets/app.js"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want %d", path, response.Code, http.StatusServiceUnavailable)
		}
		if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("%s content type = %q, want text/html", path, contentType)
		}
		if !strings.Contains(response.Body.String(), "Web bundle unavailable") {
			t.Fatalf("%s body does not explain missing web bundle: %q", path, response.Body.String())
		}
	}
}

func TestHandlerSearchSymbolAndNeighborhood(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?name=Load&mode=prefix", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, want %d", response.Code, http.StatusOK)
	}
	var search searchResponse
	if err := json.Unmarshal(response.Body.Bytes(), &search); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if search.Total != 2 || search.Returned != 2 || len(search.Results) != 2 {
		t.Fatalf("search = %#v", search)
	}
	if search.Results[0].StableKey != "symbol-a" || search.Results[1].StableKey != "symbol-b" {
		t.Fatalf("search results = %#v", search.Results)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/symbol?stable_key=symbol-b", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("symbol status = %d, want %d", response.Code, http.StatusOK)
	}
	var symbol symbolResponse
	if err := json.Unmarshal(response.Body.Bytes(), &symbol); err != nil {
		t.Fatalf("decode symbol: %v", err)
	}
	if symbol.Symbol.Name != "Load" || symbol.Symbol.Repository != "repo" || symbol.Symbol.File != "src/index.go" {
		t.Fatalf("symbol = %#v", symbol.Symbol)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/neighborhood?stable_key=symbol-b&depth=1&direction=both", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("neighborhood status = %d, want %d", response.Code, http.StatusOK)
	}
	var neighborhood neighborhoodResponse
	if err := json.Unmarshal(response.Body.Bytes(), &neighborhood); err != nil {
		t.Fatalf("decode neighborhood: %v", err)
	}
	if len(neighborhood.Nodes) != 3 || len(neighborhood.Edges) != 2 || neighborhood.Truncated {
		t.Fatalf("neighborhood = %#v", neighborhood)
	}
	foundCall := false
	for _, edge := range neighborhood.Edges {
		if edge.Source == "symbol-a" && edge.Target == "symbol-b" &&
			edge.Kind == string(facts.CallsDirect) && edge.Confidence == string(facts.ExactTypechecked) {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatalf("neighborhood edges = %#v", neighborhood.Edges)
	}
}

func TestHandlerRejectsMissingSnapshotAndUnknownSymbol(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(nil))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusServiceUnavailable, "INDEX_NOT_READY")

	handler = NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))
	request = httptest.NewRequest(http.MethodGet, "/api/v1/symbol?stable_key=missing", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusNotFound, "SYMBOL_NOT_FOUND")
}

func TestHandlerTopologyRequiresPublishedGeneration(t *testing.T) {
	handler := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(nil), TopologyOptions{Profile: "default"})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusServiceUnavailable, "TOPOLOGY_UNAVAILABLE")
	if !strings.Contains(response.Body.String(), "no published generation") {
		t.Fatalf("topology error = %q, want the missing-generation reason", response.Body.String())
	}
}

func TestHandlerTopologyIsOptIn(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusNotFound, "NOT_FOUND")
}

func TestHandlerTopologyRejectsMalformedGenerationPin(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	handler := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(testSnapshot(t)), TopologyOptions{
		ConfigPath: configPath,
		Profile:    "default",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?generation_id=not-a-generation", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT")
	if !strings.Contains(response.Body.String(), "generation_id") {
		t.Fatalf("topology error = %q, want the invalid pin field", response.Body.String())
	}
}

func TestHandlerTopologyRejectsUnknownProfileSelection(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	handler := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(testSnapshot(t)), TopologyOptions{
		ConfigPath: configPath,
		Profile:    "default",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?profile=missing", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT", request.URL.String())
}

func TestHandlerTopologyReturnsPinnedProfilesAndRelationships(t *testing.T) {
	configPath, stateRoot := topologyTestConfiguration(t, "default", "other")
	defaultStore := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7))
	otherStore := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 8))
	store, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": defaultStore,
		"other":   otherStore,
	})
	if err != nil {
		t.Fatalf("NewProfileSnapshotStore() error = %v", err)
	}
	handler := NewHandlerWithTopology(store, TopologyOptions{
		ConfigPath:       configPath,
		InvalidationRoot: stateRoot,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?profile=default&profile=other", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("topology status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var value topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	const multiProfileRequest = "/api/v1/topology?profile=default&profile=other"
	if value.TopologyVersion != topology.CurrentSchemaVersion {
		t.Fatalf("%s: topology_version = %d, want %d", multiProfileRequest, value.TopologyVersion, topology.CurrentSchemaVersion)
	}
	if value.GenerationID != "" {
		t.Fatalf("%s: generation_id = %q, want it omitted for a multi-profile response", multiProfileRequest, value.GenerationID)
	}
	if len(value.Profiles) != 2 {
		t.Fatalf("%s: profiles = %#v, want 2 profiles", multiProfileRequest, value.Profiles)
	}
	profiles := append([]topologyProfileView(nil), value.Profiles...)
	sort.Slice(profiles, func(left, right int) bool { return profiles[left].ID < profiles[right].ID })
	if profiles[0].ID != "default" || profiles[0].GenerationID != "000007" ||
		profiles[1].ID != "other" || profiles[1].GenerationID != "000008" {
		t.Fatalf("%s: topology profiles = %#v", multiProfileRequest, profiles)
	}
	if value.Completeness.Truncated || len(value.SharedInputs) != 1 ||
		len(value.SharedInputs[0].Owners) != 2 {
		t.Fatalf("%s: topology completeness/shared inputs = %#v / %#v", multiProfileRequest, value.Completeness, value.SharedInputs)
	}
	for _, expected := range []struct{ typ, status string }{
		{typ: "membership", status: "structural"},
		{typ: "code_dependency", status: "exact"},
		{typ: "code_dependency", status: "candidate"},
		{typ: "unresolved_reference", status: "conflict"},
		{typ: "unresolved_reference", status: "unresolved"},
	} {
		if !hasTopologyRelationship(value.Relationships, expected.typ, expected.status) {
			t.Fatalf("missing topology relationship type=%q status=%q; relationships = %#v", expected.typ, expected.status, value.Relationships)
		}
	}
	for _, relationship := range value.Relationships {
		if relationship.Type == "code_dependency" && relationship.Status == "candidate" && relationship.Evidence != "" {
			t.Fatalf("%s: candidate dependency evidence = %q, want unset zero evidence", multiProfileRequest, relationship.Evidence)
		}
	}

	const singleProfileRequest = "/api/v1/topology?generation_id=000007"
	request = httptest.NewRequest(http.MethodGet, singleProfileRequest, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, want %d; body=%s", singleProfileRequest, response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("%s: topology content type = %q, want JSON", singleProfileRequest, got)
	}
	var single topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &single); err != nil {
		t.Fatalf("%s: decode single topology: %v", singleProfileRequest, err)
	}
	if single.GenerationID != "000007" || len(single.Profiles) != 1 || single.Profiles[0].ID != "default" {
		t.Fatalf("%s: single topology = %#v", singleProfileRequest, single)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/topology?profile=*&generation_id=000007", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT", request.URL.String())

	for _, rejected := range []string{
		"/api/v1/topology?profile=default&profile=other&generation=default:000007",
		"/api/v1/topology?profile=default&generation=other:000008",
		"/api/v1/topology?profile=default&generation=default:not-a-generation",
		"/api/v1/topology?profile=default&generation=default:000007&generation=default:000008",
	} {
		request = httptest.NewRequest(http.MethodGet, rejected, nil)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT", request.URL.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/topology?profile=default&profile=other&generation=default:000007&generation=other:000008", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("pinned multi-profile topology status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var pinned topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &pinned); err != nil {
		t.Fatalf("decode pinned topology: %v", err)
	}
	sort.Slice(pinned.Profiles, func(left, right int) bool { return pinned.Profiles[left].ID < pinned.Profiles[right].ID })
	if len(pinned.Profiles) != 2 ||
		pinned.Profiles[0].ID != "default" || pinned.Profiles[0].GenerationID != "000007" ||
		pinned.Profiles[1].ID != "other" || pinned.Profiles[1].GenerationID != "000008" {
		t.Fatalf("pinned profiles for generation pins = %#v", pinned.Profiles)
	}
	for _, expected := range []struct{ typ, status string }{
		{typ: "membership", status: "structural"},
		{typ: "code_dependency", status: "exact"},
		{typ: "code_dependency", status: "candidate"},
		{typ: "unresolved_reference", status: "conflict"},
		{typ: "unresolved_reference", status: "unresolved"},
	} {
		if !hasTopologyRelationship(pinned.Relationships, expected.typ, expected.status) {
			t.Fatalf("pinned topology is missing relationship type=%q status=%q; relationships = %#v", expected.typ, expected.status, pinned.Relationships)
		}
	}
}

func TestTopologyRelationshipCacheRespectsMaximumOpenProfiles(t *testing.T) {
	defaultStore := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7))
	otherStore := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 8))
	store, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": defaultStore,
		"other":   otherStore,
	})
	if err != nil {
		t.Fatalf("NewProfileSnapshotStore() error = %v", err)
	}
	if err := store.SetMaxOpenProfiles(1); err != nil {
		t.Fatalf("SetMaxOpenProfiles(1) error = %v", err)
	}
	handler := &Handler{
		store:                 store,
		topologyRelationships: make(map[string]topologyRelationshipCacheEntry),
	}
	defaultData := topologyProfileData{Name: "default", GenerationID: "000007", Snapshot: testSnapshotWithTopologyID(t, 7)}
	otherData := topologyProfileData{Name: "other", GenerationID: "000008", Snapshot: testSnapshotWithTopologyID(t, 8)}
	defaultAssembler := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), defaultAssembler, defaultData); err != nil {
		t.Fatalf("addSnapshotRelationships(%q) error = %v", defaultData.Name, err)
	}
	defaultRelationships := append([]topologyRelationshipView(nil), defaultAssembler.relationships...)
	otherAssembler := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), otherAssembler, otherData); err != nil {
		t.Fatalf("addSnapshotRelationships(%q) error = %v", otherData.Name, err)
	}
	otherRelationships := append([]topologyRelationshipView(nil), otherAssembler.relationships...)

	residentOther := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), residentOther, otherData); err != nil {
		t.Fatalf("reusing addSnapshotRelationships(%q) error = %v", otherData.Name, err)
	}
	if !reflect.DeepEqual(residentOther.relationships, otherRelationships) {
		t.Fatalf("reused relationships = %#v, want %#v", residentOther.relationships, otherRelationships)
	}
	evictedDefault := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), evictedDefault, defaultData); err != nil {
		t.Fatalf("reloading addSnapshotRelationships(%q) error = %v", defaultData.Name, err)
	}
	if !reflect.DeepEqual(evictedDefault.relationships, defaultRelationships) {
		t.Fatalf("reloaded relationships = %#v, want %#v", evictedDefault.relationships, defaultRelationships)
	}
	if len(handler.topologyRelationships) != 1 {
		t.Fatalf("relationship cache size = %d, want 1", len(handler.topologyRelationships))
	}
	if _, found := handler.topologyRelationships["default"]; !found {
		t.Fatalf("relationship cache = %#v, want most recently used profile", handler.topologyRelationships)
	}
}

func TestHandlerTopologyReportsUnavailableCurrentSource(t *testing.T) {
	configPath, stateRoot := topologyTestConfiguration(t, "default")
	manager, err := invalidation.Open(stateRoot)
	if err != nil {
		t.Fatalf("invalidation.Open() error = %v", err)
	}
	observation, err := topology.NewSourceObservation("shared-worktree", "commit-a", "main", false, strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("NewSourceObservation() error = %v", err)
	}
	manifest := sourceobservation.Manifest{
		Version:             sourceobservation.CurrentVersion,
		Profile:             "default",
		ResolverVersion:     "resolver-test",
		AnalyzerFingerprint: "analyzer-test",
		Sources: []sourceobservation.Source{{
			Repository:  "repo",
			Observation: observation,
		}},
	}
	if err := manager.RecordPublished(context.Background(), invalidation.ProfileRecord{
		Profile: "default", Generation: "000007", Manifest: manifest,
	}); err != nil {
		t.Fatalf("RecordPublished() error = %v", err)
	}
	if err := manager.MarkStale(context.Background(), observation.Worktree, "repo", invalidation.ReasonSourceUnavailable, "worktree disappeared"); err != nil {
		t.Fatalf("MarkStale() error = %v", err)
	}

	handler := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7)), TopologyOptions{
		ConfigPath:       configPath,
		Profile:          "default",
		InvalidationRoot: stateRoot,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("topology status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var value topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	if len(value.Profiles) != 1 || len(value.Sources) != 1 || value.Sources[0].Status != "unavailable" ||
		value.Sources[0].Current != nil || value.Sources[0].Indexed == nil ||
		!strings.Contains(value.Sources[0].Reason, "worktree disappeared") || value.Profiles[0].Status != "stale" {
		t.Fatalf("topology source/profile status = %#v / %#v", value.Sources, value.Profiles)
	}
}

func TestHandlerTopologyRejectsAStaleContinuation(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	store := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7))
	handler := NewHandlerWithTopology(store, TopologyOptions{ConfigPath: configPath, Profile: "default"})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("initial topology status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if err := store.Publish(testSnapshotWithTopologyID(t, 8)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/topology?generation_id=000007", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusConflict, "GENERATION_CHANGED")
	if !strings.Contains(response.Body.String(), "refresh") {
		t.Fatalf("stale continuation error = %q, want refresh guidance", response.Body.String())
	}
}

func TestTopologyDeclaredRepositoryWinsSynthesizedRecord(t *testing.T) {
	assembler := newTopologyAssembler()
	if err := assembler.addSnapshotRepositories(context.Background(), testSnapshot(t)); err != nil {
		t.Fatalf("addSnapshotRepositories() error = %v", err)
	}
	data := topologyProfileData{
		Name:     "default",
		Snapshot: testSnapshot(t),
		Composition: topology.ProfileComposition{
			Profile:      topology.Profile{ID: "default"},
			Repositories: []topology.LogicalRepository{{ID: "repo", Name: "Repository"}},
		},
		ManifestOK: true,
	}
	if err := assembler.addComposition(context.Background(), data); err != nil {
		t.Fatalf("declared repository after synthesized record: %v", err)
	}
	repositories := assembler.response().Repositories
	if len(repositories) != 1 || repositories[0].ID != "repo" || repositories[0].Name != "Repository" {
		t.Fatalf("repositories for declared id %q = %#v, want the declared name", "repo", repositories)
	}

	conflicting := data
	conflicting.Composition.Repositories = []topology.LogicalRepository{{ID: "repo", Name: "Conflicting repository"}}
	if err := assembler.addComposition(context.Background(), conflicting); !errors.Is(err, errTopologyAmbiguous) {
		t.Fatalf("conflicting declared repository error = %v, want %v", err, errTopologyAmbiguous)
	}
}

func TestHandlerTopologyReportsAmbiguousRepositoryDeclarations(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default", "other")
	for _, declaration := range []struct {
		profile string
		name    string
	}{
		{profile: "default", name: "Default repository"},
		{profile: "other", name: "Other repository"},
	} {
		value, present, err := config.LoadProfileTopology(configPath, declaration.profile)
		if err != nil {
			t.Fatalf("LoadProfileTopology(%q) error = %v", declaration.profile, err)
		}
		if !present || len(value.Repositories) != 1 {
			t.Fatalf("profile %q topology = %#v, want one repository", declaration.profile, value)
		}
		repository := value.Repositories[0]
		repository.Name = declaration.name
		value.Repositories = []topology.LogicalRepository{repository}
		if err := config.SaveProfileTopology(configPath, declaration.profile, value); err != nil {
			t.Fatalf("SaveProfileTopology(%q) error = %v", declaration.profile, err)
		}
	}

	store, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7)),
		"other":   hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 8)),
	})
	if err != nil {
		t.Fatalf("NewProfileSnapshotStore() error = %v", err)
	}
	handler := NewHandlerWithTopology(store, TopologyOptions{ConfigPath: configPath})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?profile=default&profile=other", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusConflict, "TOPOLOGY_AMBIGUOUS", request.URL.String())
}

func TestTopologyAssemblerHidesUnobservedStaleCurrent(t *testing.T) {
	observation, err := topology.NewSourceObservation("shared-worktree", "commit-a", "main", false, strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("NewSourceObservation() error = %v", err)
	}
	assembler := newTopologyAssembler()
	assembler.addSources(topologyProfileData{
		Name: "default", ManifestOK: true,
		Manifest: sourceobservation.Manifest{Sources: []sourceobservation.Source{{
			Repository: "repo", Observation: observation,
		}}},
		Composition: topology.ProfileComposition{Worktrees: []topology.Worktree{{
			ID: observation.Worktree, Repository: "repo",
		}}},
		State: &invalidation.ProfileState{Changes: []invalidation.SourceChange{{
			Worktree: observation.Worktree, Repository: "repo",
			Reason: invalidation.ReasonCommitChanged, Detail: "commit changed",
		}}},
	})

	sources := assembler.response().Sources
	if len(sources) != 1 || sources[0].Status != "stale" || sources[0].Current != nil {
		t.Fatalf("stale source view for worktree %q = %#v, want stale without an observed current", observation.Worktree, sources)
	}
}

func TestTopologyResponseReportsIncompleteAndTruncatedReasons(t *testing.T) {
	assembler := newTopologyAssembler()
	assembler.sources = []topologySourceView{{Status: "missing"}}
	assembler.truncated = true
	assembler.truncatedReason = "relationship limit reached"

	response := assembler.response()
	if response.Completeness.Complete || !response.Completeness.Truncated {
		t.Fatalf("completeness = %#v, want incomplete and truncated", response.Completeness)
	}
	want := "one or more source observations or indexed manifests are missing or unavailable; relationship limit reached"
	if response.Completeness.Reason != want {
		t.Fatalf("completeness reason = %q, want %q", response.Completeness.Reason, want)
	}
}

func TestTopologyClientErrorUsesInternalMessageForUnknownCode(t *testing.T) {
	if got := topologyClientError("INTERNAL", errors.New("snapshot indexes are inconsistent")); got != "topology request failed" {
		t.Fatalf("topologyClientError(INTERNAL) = %q, want the generic internal message", got)
	}
}

func hasTopologyRelationship(relationships []topologyRelationshipView, typ, status string) bool {
	for _, relationship := range relationships {
		if relationship.Type == typ && relationship.Status == status {
			return true
		}
	}
	return false
}

func topologyTestConfiguration(t *testing.T, profiles ...string) (string, string) {
	t.Helper()
	root := t.TempDir()
	testsupport.SetHome(t, filepath.Join(root, "home"))
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath, RepositoriesPath: repositoriesPath}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	for _, profile := range profiles[1:] {
		if err := config.CreateProfile(configPath, profile); err != nil {
			t.Fatalf("config.CreateProfile(%q) error = %v", profile, err)
		}
	}
	sourcePath := filepath.Join(root, "source")
	if err := os.MkdirAll(sourcePath, 0o700); err != nil {
		t.Fatalf("create source path: %v", err)
	}
	for _, profile := range profiles {
		value := topology.Topology{
			Version:      topology.CurrentSchemaVersion,
			Repositories: []topology.LogicalRepository{{ID: "repo", Name: "Repository"}},
			Worktrees:    []topology.Worktree{{ID: "shared-worktree", Repository: "repo", Path: sourcePath}},
			Profiles:     []topology.Profile{{ID: topology.ProfileID(profile), Worktrees: []topology.WorktreeSelection{{Repository: "repo", Worktree: "shared-worktree"}}}},
		}
		if err := config.SaveProfileTopology(configPath, profile, value); err != nil {
			t.Fatalf("config.SaveProfileTopology(%q) error = %v", profile, err)
		}
	}
	loaded, err := config.LoadProfile(configPath, profiles[0])
	if err != nil {
		t.Fatalf("config.LoadProfile() error = %v", err)
	}
	databaseDirectory := filepath.Dir(loaded.Config.Storage.DatabasePath)
	profilesDirectory := filepath.Dir(databaseDirectory)
	if filepath.Base(profilesDirectory) != "profiles" {
		t.Fatalf("storage layout for profile %q = %q, want a profiles/<name> parent", profiles[0], loaded.Config.Storage.DatabasePath)
	}
	stateRoot := filepath.Dir(profilesDirectory)
	return configPath, stateRoot
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string, request ...string) {
	t.Helper()
	requestContext := ""
	if len(request) > 0 {
		requestContext = "request=" + request[0] + "; "
	}
	if response.Code != status {
		t.Fatalf("%sstatus = %d, want %d; body=%s", requestContext, response.Code, status, response.Body.String())
	}
	var apiErr apiError
	if err := json.Unmarshal(response.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("%sdecode error: %v", requestContext, err)
	}
	if apiErr.Code != code {
		t.Fatalf("%serror code = %q, want %q", requestContext, apiErr.Code, code)
	}
}

func testSnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	return testSnapshotData(t, 7, false)
}

func testSnapshotWithTopologyID(t *testing.T, snapshotID uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	return testSnapshotData(t, snapshotID, true)
}

func testSnapshotData(t *testing.T, snapshotID uint64, includeTopologyRecords bool) *hotsnapshot.GraphSnapshot {
	t.Helper()
	interner := hotsnapshot.NewStringInterner()
	intern := func(value string) hotsnapshot.InternedString {
		id, err := interner.Intern(value)
		if err != nil {
			t.Fatalf("intern %q: %v", value, err)
		}
		return id
	}
	stableKeys, err := hotsnapshot.NewStableKeyTable([]hotsnapshot.StableKey{"symbol-a", "symbol-b", "symbol-c"})
	if err != nil {
		t.Fatalf("NewStableKeyTable: %v", err)
	}
	repoKey := intern("repository:repo")
	packageKey := intern("package:go:repo:example")
	fileKey := intern("file:repo:src/index.go")
	language := intern("go")
	nameLoad := intern("Load")
	nameOther := intern("Other")
	qnameA := intern("example.Load")
	qnameB := intern("example.Loader")
	qnameC := intern("example.Other")
	kindFunction := intern("function")
	evidenceKey := intern("evidence:file:repo:src/index.go:1:2")
	intern("repo")
	intern("/workspace/repo")
	intern("example")
	intern("src/index.go")
	intern("identity-a")
	intern("identity-b")
	intern("identity-c")
	intern("func Load()")
	intern("func Loader()")
	intern("func Other()")
	intern("call")
	intern("GoTypesUse")
	intern("AMBIGUOUS_SYMBOL")
	intern("candidate dependency")
	intern("unresolved symbol")
	intern("not found")
	intern("Missing")
	stringTable := interner.Freeze()
	var packageDependencies []hotsnapshot.PackageDependencyRecord
	var unresolved []hotsnapshot.UnresolvedReferenceRecord
	if includeTopologyRecords {
		packageDependencies = []hotsnapshot.PackageDependencyRecord{{
			Source: 0, Target: 0, Kind: facts.CodePackageDependsOn, Confidence: facts.CodeCandidate,
			Provenance: facts.CodePackageManifest,
		}}
		unresolved = []hotsnapshot.UnresolvedReferenceRecord{{
			Key: evidenceKey, Repository: 0, File: 0, Source: 0, Language: language,
			RequestedPackage: interned(stringTable, "example"), RequestedSymbol: interned(stringTable, "Missing"),
			Reason: interned(stringTable, "AMBIGUOUS_SYMBOL"), Detail: interned(stringTable, "candidate dependency"),
		}, {
			Key: evidenceKey, Repository: 0, File: 0, Source: 0, Language: language,
			RequestedPackage: interned(stringTable, "example"), RequestedSymbol: interned(stringTable, "Missing"),
			Reason: interned(stringTable, "unresolved symbol"), Detail: interned(stringTable, "not found"),
		}}
	}

	snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID:              snapshotID,
		CreatedAt:       time.Unix(1_700_000_000, 0).UTC(),
		Version:         1,
		SchemaVersion:   2,
		ResolverVersion: "resolver-test",
		Strings:         stringTable,
		Repositories: []hotsnapshot.RepositoryRecord{{
			Key: repoKey, Name: interned(stringTable, "repo"), Path: interned(stringTable, "/workspace/repo"), Languages: interned(stringTable, "go"),
		}},
		Packages: []hotsnapshot.PackageRecord{{
			Key: packageKey, Repository: 0, Language: language, Name: interned(stringTable, "example"), ModulePath: interned(stringTable, "example"),
		}},
		Files: []hotsnapshot.FileRecord{{
			Key: fileKey, Repository: 0, Package: 0, Path: interned(stringTable, "src/index.go"), Language: language,
		}},
		PackageDependencies: packageDependencies,
		Symbols: []hotsnapshot.SymbolRecord{
			{StableKey: 0, CanonicalIdentity: interned(stringTable, "identity-a"), File: 0, Language: language, Name: nameLoad, QualifiedName: qnameA, Kind: kindFunction, Signature: interned(stringTable, "func Load()"), StartLine: 1, EndLine: 2},
			{StableKey: 1, CanonicalIdentity: interned(stringTable, "identity-b"), File: 0, Language: language, Name: nameLoad, QualifiedName: qnameB, Kind: kindFunction, Signature: interned(stringTable, "func Loader()"), StartLine: 4, EndLine: 5},
			{StableKey: 2, CanonicalIdentity: interned(stringTable, "identity-c"), File: 0, Language: language, Name: nameOther, QualifiedName: qnameC, Kind: kindFunction, Signature: interned(stringTable, "func Other()"), StartLine: 7, EndLine: 8},
		},
		Evidence:       []hotsnapshot.EvidenceRecord{{Key: evidenceKey, SourceFile: 0, TargetFile: 0, Kind: interned(stringTable, "call"), Provenance: interned(stringTable, "GoTypesUse")}},
		Unresolved:     unresolved,
		ForwardOffsets: []uint32{0, 1, 2, 2},
		ForwardEdges: []hotsnapshot.PackedEdge{
			{Target: 1, Evidence: 0, Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse},
			{Target: 2, Evidence: 0, Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse},
		},
		ReverseOffsets: []uint32{0, 0, 1, 2},
		ReverseEdges: []hotsnapshot.PackedEdge{
			{Target: 0, Evidence: 0, Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse},
			{Target: 1, Evidence: 0, Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse},
		},
		StableKeys: stableKeys,
	})
	if err != nil {
		t.Fatalf("NewGraphSnapshot: %v", err)
	}
	return snapshot
}

func interned(table hotsnapshot.StringTable, value string) hotsnapshot.InternedString {
	id, ok := table.Lookup(value)
	if !ok {
		panic("missing fixture string: " + value)
	}
	return id
}
