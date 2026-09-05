package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/Luqueee/kivgraph/internal/storage/generation"
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
	const defaultRepositoryLanguages = " go, typescript,go,, "
	const otherRepositoryLanguages = "rust, typescript"
	defaultStore := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyIDAndLanguages(t, 7, defaultRepositoryLanguages))
	otherStore := hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyIDAndLanguages(t, 8, otherRepositoryLanguages))
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
		t.Fatalf("%s: topology status = %d, want %d; body=%s", request.URL.String(), response.Code, http.StatusOK, response.Body.String())
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
	if len(value.Repositories) != 1 || !reflect.DeepEqual(value.Repositories[0].Languages, []string{"go", "rust", "typescript"}) {
		t.Fatalf("%s: repository language facets for inputs %q and %q = %#v, want [go rust typescript]", multiProfileRequest, defaultRepositoryLanguages, otherRepositoryLanguages, value.Repositories)
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

func TestTopologyRelationshipsPreserveCrossRepositoryEndpoints(t *testing.T) {
	assembler := newTopologyAssembler()
	err := assembler.addSnapshotRelationships(context.Background(), topologyProfileData{
		Name:         "default",
		GenerationID: "000007",
		Snapshot:     testSnapshotWithCrossRepositoryTopology(t, 7),
	})
	if err != nil {
		t.Fatalf("addSnapshotRelationships() error = %v", err)
	}
	for _, relationship := range assembler.relationships {
		if relationship.Type == "code_dependency" && relationship.Source.ID == "repo" &&
			relationship.Target != nil && relationship.Target.ID == "provider" {
			return
		}
	}
	t.Fatalf("cross-repository dependency = missing from %#v", assembler.relationships)
}

func TestTopologyResponseUsesEmptyLanguageArrayWhenMetadataIsUnavailable(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	const repositoryLanguages = ""
	handler := NewHandlerWithTopology(
		hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyIDAndLanguages(t, 7, repositoryLanguages)),
		TopologyOptions{ConfigPath: configPath, Profile: "default"},
	)
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
	if len(value.Repositories) != 1 || value.Repositories[0].Languages == nil || len(value.Repositories[0].Languages) != 0 {
		t.Fatalf("topology repository languages for metadata %q = %#v, want a non-nil empty array", repositoryLanguages, value.Repositories)
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

	otherWithoutTopology := otherData
	otherWithoutTopology.Snapshot = testSnapshotData(t, 8, false)
	residentOther := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), residentOther, otherWithoutTopology); err != nil {
		t.Fatalf("reusing addSnapshotRelationships(%q) error = %v", otherWithoutTopology.Name, err)
	}
	if !reflect.DeepEqual(residentOther.relationships, otherRelationships) {
		t.Fatalf("reused relationships = %#v, want %#v", residentOther.relationships, otherRelationships)
	}

	defaultWithoutTopology := defaultData
	defaultWithoutTopology.Snapshot = testSnapshotData(t, 7, false)
	evictedDefault := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), evictedDefault, defaultWithoutTopology); err != nil {
		t.Fatalf("reloading addSnapshotRelationships(%q) error = %v", defaultWithoutTopology.Name, err)
	}
	uncachedDefault := newTopologyAssembler()
	if err := uncachedDefault.addSnapshotRelationships(context.Background(), defaultWithoutTopology); err != nil {
		t.Fatalf("uncached addSnapshotRelationships(%q) error = %v", defaultWithoutTopology.Name, err)
	}
	if !reflect.DeepEqual(evictedDefault.relationships, uncachedDefault.relationships) ||
		reflect.DeepEqual(evictedDefault.relationships, defaultRelationships) {
		t.Fatalf("evicted relationships = %#v, want uncached result %#v", evictedDefault.relationships, uncachedDefault.relationships)
	}

	reloadedOther := newTopologyAssembler()
	if err := handler.addSnapshotRelationships(context.Background(), reloadedOther, otherData); err != nil {
		t.Fatalf("reloading addSnapshotRelationships(%q) error = %v", otherData.Name, err)
	}
	if !reflect.DeepEqual(reloadedOther.relationships, otherRelationships) {
		t.Fatalf("reloaded relationships = %#v, want %#v", reloadedOther.relationships, otherRelationships)
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

func TestHandlerTopologyKeepsPublishedCompositionAfterConfigurationChanges(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default", "other")
	value, present, err := config.LoadProfileTopology(configPath, "other")
	if err != nil {
		t.Fatalf("LoadProfileTopology(other) error = %v", err)
	}
	if !present || len(value.Repositories) != 1 || len(value.Worktrees) != 1 {
		t.Fatalf("profile other topology = %#v, want one repository and worktree", value)
	}
	value.Repositories[0].Name = "Live configuration only"
	value.Worktrees[0].ID = "live-worktree"
	value.Profiles[0].Worktrees[0].Worktree = "live-worktree"
	if err := config.SaveProfileTopology(configPath, "other", value); err != nil {
		t.Fatalf("SaveProfileTopology(other) error = %v", err)
	}

	store, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7)),
		"other":   hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 8)),
	})
	if err != nil {
		t.Fatalf("NewProfileSnapshotStore() error = %v", err)
	}
	handler := NewHandlerWithTopology(store, TopologyOptions{ConfigPath: configPath})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?profile=default&profile=other&generation=default:000007&generation=other:000008", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("%s: topology status = %d, want %d; body=%s", request.URL.String(), response.Code, http.StatusOK, response.Body.String())
	}
	var got topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode topology for %s: %v; body=%s", request.URL.String(), err, response.Body.String())
	}
	if len(got.Worktrees) != 1 || got.Worktrees[0].ID != "shared-worktree" ||
		len(got.Repositories) != 1 || got.Repositories[0].Name != "Repository" {
		t.Fatalf("topology after a live configuration edit = %#v / %#v, want the published composition", got.Worktrees, got.Repositories)
	}
	profiles := append([]topologyProfileView(nil), got.Profiles...)
	sort.Slice(profiles, func(left, right int) bool { return profiles[left].ID < profiles[right].ID })
	if len(profiles) != 2 ||
		profiles[0].ID != "default" || profiles[0].GenerationID != "000007" ||
		profiles[1].ID != "other" || profiles[1].GenerationID != "000008" {
		t.Fatalf("published profiles for %s = %#v, want default:000007 and other:000008", request.URL.String(), profiles)
	}
	for _, profile := range profiles {
		if profile.Status == "partial" || !profile.CompositionComplete {
			t.Fatalf("profile %q status/composition completeness = %q/%t reason = %q, want the published composition", profile.ID, profile.Status, profile.CompositionComplete, profile.Reason)
		}
	}
}

func TestHandlerTopologyMarksLegacyGenerationCompositionIncomplete(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	legacy := topologyManifest(t, "default", "legacy-worktree", nil)
	legacy.Version = sourceobservation.LegacyVersion
	if err := sourceobservation.Write(topologyGenerationPath(t, configPath, "default", 7), legacy); err != nil {
		t.Fatal(err)
	}

	handler := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7)), TopologyOptions{
		ConfigPath: configPath,
		Profile:    "default",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?generation_id=000007", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s: topology status = %d, want %d; body=%s", request.URL.String(), response.Code, http.StatusOK, response.Body.String())
	}
	var got topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode topology for %s: %v; body=%s", request.URL.String(), err, response.Body.String())
	}
	if got.Status != "partial" || len(got.Profiles) != 1 || got.Profiles[0].Status != "partial" ||
		!strings.Contains(got.Profiles[0].Reason, "does not record topology composition") ||
		!strings.Contains(got.Completeness.Reason, "does not record topology composition") || len(got.Worktrees) != 0 {
		t.Fatalf("legacy topology = %#v, want explicit incomplete composition without live worktrees", got)
	}
}

func TestHandlerTopologyMarksInvalidManifestIncomplete(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	value, present, err := config.LoadProfileTopology(configPath, "default")
	if err != nil || !present {
		t.Fatalf("LoadProfileTopology(default) = %#v, %t, %v", value, present, err)
	}
	manifest := topologyManifest(t, "default", "shared-worktree", &value)
	if manifest.Composition == nil {
		t.Fatal("topology manifest has no persisted composition")
	}
	manifest.Composition.Profile = "other"
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(topologyGenerationPath(t, configPath, "default", 7), sourceobservation.FileName)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	handler, ok := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7)), TopologyOptions{
		ConfigPath: configPath,
		Profile:    "default",
	}).(*Handler)
	if !ok {
		t.Fatal("NewHandlerWithTopology() did not return *Handler")
	}
	var logs bytes.Buffer
	handler.logger = slog.New(slog.NewTextHandler(&logs, nil))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?generation_id=000007", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s: topology status = %d, want %d; body=%s", request.URL.String(), response.Code, http.StatusOK, response.Body.String())
	}
	var got topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode topology for %s: %v; body=%s", request.URL.String(), err, response.Body.String())
	}
	if got.Status != "partial" || len(got.Profiles) != 1 || got.Profiles[0].Status != "partial" ||
		got.Profiles[0].CompositionComplete || got.Profiles[0].Reason != "indexed source observations are invalid" ||
		!strings.Contains(got.Completeness.Reason, "indexed source observations are invalid") || len(got.Worktrees) != 0 {
		t.Fatalf("invalid-manifest topology = %#v, want an explicit incomplete composition", got)
	}
	if logOutput := logs.String(); !strings.Contains(logOutput, "published source observations are invalid") ||
		!strings.Contains(logOutput, "does not match source observation profile") {
		t.Fatalf("invalid-manifest logs = %q, want the original source observation diagnostic", logOutput)
	}
}

func TestHandlerTopologyMarksUnavailableManifestIncomplete(t *testing.T) {
	configPath, _ := topologyTestConfiguration(t, "default")
	path := filepath.Join(topologyGenerationPath(t, configPath, "default", 7), sourceobservation.FileName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithTopology(hotsnapshot.NewSnapshotStore(testSnapshotWithTopologyID(t, 7)), TopologyOptions{
		ConfigPath: configPath,
		Profile:    "default",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/topology?generation_id=000007", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s: topology status = %d, want %d; body=%s", request.URL.String(), response.Code, http.StatusOK, response.Body.String())
	}
	var got topologyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode topology for %s: %v; body=%s", request.URL.String(), err, response.Body.String())
	}
	if got.Status != "partial" || len(got.Profiles) != 1 || got.Profiles[0].Status != "partial" ||
		got.Profiles[0].CompositionComplete || got.Profiles[0].Reason != "indexed source observations are unavailable" {
		t.Fatalf("topology for unavailable manifest = %#v, want explicit incomplete manifest", got)
	}
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

func TestTopologyAssemblerEmitsOverlayAndSharedInputInvalidation(t *testing.T) {
	sharedObservation, err := topology.NewSourceObservation("shared-main", "0123456789abcdef", "main", false, strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("NewSourceObservation(shared-main) error = %v", err)
	}
	featureObservation, err := topology.NewSourceObservation("feature-worktree", "fedcba9876543210", "feature", false, strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("NewSourceObservation(feature-worktree) error = %v", err)
	}
	repository := topology.LogicalRepository{ID: "repo", Name: "Repository"}
	sharedWorktree := topology.Worktree{ID: "shared-main", Repository: "repo", Path: "/workspace/shared"}
	featureWorktree := topology.Worktree{ID: "feature-worktree", Repository: "repo", Path: "/workspace/feature"}
	change := invalidation.SourceChange{
		Worktree: "shared-main", Repository: "repo", Reason: invalidation.ReasonContentChanged,
		Detail: "shared content changed after indexing",
	}
	data := []topologyProfileData{
		{
			Name: "default", GenerationID: "000007", Snapshot: testSnapshot(t), CompositionOK: true,
			Composition: topology.ProfileComposition{
				Profile:      topology.Profile{ID: "default", Worktrees: []topology.WorktreeSelection{{Repository: "repo", Worktree: "shared-main"}}},
				Repositories: []topology.LogicalRepository{repository}, Worktrees: []topology.Worktree{sharedWorktree},
			},
			ManifestOK: true, Manifest: sourceobservation.Manifest{Sources: []sourceobservation.Source{{Repository: "repo", Observation: sharedObservation}}},
			State: &invalidation.ProfileState{Profile: "default", Generation: "000007", Stale: true, Reason: change.Detail, Changes: []invalidation.SourceChange{change}},
		},
		{
			Name: "maintenance", GenerationID: "000008", Snapshot: testSnapshot(t), CompositionOK: true,
			Composition: topology.ProfileComposition{
				Profile:      topology.Profile{ID: "maintenance", Worktrees: []topology.WorktreeSelection{{Repository: "repo", Worktree: "shared-main"}}},
				Repositories: []topology.LogicalRepository{repository}, Worktrees: []topology.Worktree{sharedWorktree},
			},
			ManifestOK: true, Manifest: sourceobservation.Manifest{Sources: []sourceobservation.Source{{Repository: "repo", Observation: sharedObservation}}},
			State: &invalidation.ProfileState{Profile: "maintenance", Generation: "000008", Stale: true, Reason: change.Detail, Changes: []invalidation.SourceChange{change}},
		},
		{
			Name: "feature", GenerationID: "000009", Snapshot: testSnapshot(t), CompositionOK: true,
			Composition: topology.ProfileComposition{
				Profile:      topology.Profile{ID: "feature", Worktrees: []topology.WorktreeSelection{{Repository: "repo", Worktree: "feature-worktree", Overlays: "shared-main"}}},
				Repositories: []topology.LogicalRepository{repository}, Worktrees: []topology.Worktree{featureWorktree}, OverlayWorktrees: []topology.Worktree{sharedWorktree},
			},
			ManifestOK: true, Manifest: sourceobservation.Manifest{Sources: []sourceobservation.Source{{Repository: "repo", Observation: featureObservation}}},
			State: &invalidation.ProfileState{Profile: "feature", Generation: "000009"},
		},
	}
	assembler := newTopologyAssembler()
	for _, profile := range data {
		if err := assembler.addComposition(context.Background(), profile); err != nil {
			t.Fatalf("addComposition(%q) error = %v", profile.Name, err)
		}
	}
	response := assembler.response()
	if len(response.SharedInputs) != 1 || response.SharedInputs[0].ID != "shared-main" ||
		response.SharedInputs[0].Status != "stale" || response.SharedInputs[0].Reason != change.Detail ||
		!reflect.DeepEqual(response.SharedInputs[0].Owners, []string{"default", "maintenance"}) {
		t.Fatalf("shared input = %#v, want the stale shared-main input and its owners", response.SharedInputs)
	}
	for _, expected := range []struct {
		typ, profile, generation, sourceType, sourceID, targetType, targetID, kind, provenance string
	}{
		{"shared_input_usage", "default", "000007", "profile", "default", "shared_input", "worktree:shared-main", "uses", "TOPOLOGY_DECLARATION"},
		{"shared_input_usage", "maintenance", "000008", "profile", "maintenance", "shared_input", "worktree:shared-main", "uses", "TOPOLOGY_DECLARATION"},
		{"worktree_overlay", "feature", "000009", "worktree", "feature-worktree", "shared_input", "worktree:shared-main", "overlays", "TOPOLOGY_DECLARATION"},
		{"shared_input_invalidation", "default", "000007", "shared_input", "worktree:shared-main", "profile", "default", "invalidates", "SOURCE_INVALIDATION"},
		{"shared_input_invalidation", "maintenance", "000008", "shared_input", "worktree:shared-main", "profile", "maintenance", "invalidates", "SOURCE_INVALIDATION"},
	} {
		found := false
		for _, relationship := range response.Relationships {
			if relationship.Type == expected.typ && relationship.Profile == expected.profile && relationship.GenerationID == expected.generation &&
				relationship.Source.Type == expected.sourceType && relationship.Source.ID == expected.sourceID &&
				relationship.Target != nil && relationship.Target.Type == expected.targetType && relationship.Target.ID == expected.targetID &&
				relationship.Kind == expected.kind && relationship.Status == "structural" && relationship.Confidence == string(facts.StructuralCertain) && relationship.Provenance == expected.provenance {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing typed structural relationship %#v in %#v", expected, response.Relationships)
		}
	}
}

func TestTopologyAssemblerRejectsConflictingOverlayRepositoryIdentity(t *testing.T) {
	profile := func(name, generation string, repository topology.LogicalRepositoryID, overlayPath string) topologyProfileData {
		worktree := topology.WorktreeID(name + "-worktree")
		return topologyProfileData{
			Name: name, GenerationID: generation, Snapshot: testSnapshot(t), CompositionOK: true,
			Composition: topology.ProfileComposition{
				Profile: topology.Profile{ID: topology.ProfileID(name), Worktrees: []topology.WorktreeSelection{{
					Repository: repository, Worktree: worktree, Overlays: "shared-main",
				}}},
				Repositories: []topology.LogicalRepository{{ID: repository, Name: "Repository"}},
				Worktrees:    []topology.Worktree{{ID: worktree, Repository: repository, Path: "/workspace/" + name}},
				OverlayWorktrees: []topology.Worktree{{
					ID: "shared-main", Repository: repository, Path: overlayPath,
				}},
			},
		}
	}

	assembler := newTopologyAssembler()
	if err := assembler.addComposition(context.Background(), profile("first", "000007", "repo-a", "/workspace/shared-a")); err != nil {
		t.Fatalf("add first composition: %v", err)
	}
	if err := assembler.addComposition(context.Background(), profile("second", "000008", "repo-a", "/workspace/shared-b")); err != nil {
		t.Fatalf("add same-repository overlay with another path: %v", err)
	}
	err := assembler.addComposition(context.Background(), profile("third", "000009", "repo-b", "/workspace/shared-c"))
	if !errors.Is(err, errTopologyAmbiguous) || !strings.Contains(err.Error(), "maps to logical repositories") {
		t.Fatalf("conflicting overlay repository error = %v, want an ambiguous repository identity", err)
	}
}

func TestTopologyAssemblerKeepsStaleAndIncompleteCompositionSeparate(t *testing.T) {
	assembler := newTopologyAssembler()
	if err := assembler.addComposition(context.Background(), topologyProfileData{
		Name: "default", GenerationID: "000007", Snapshot: testSnapshot(t),
		Composition:       topology.ProfileComposition{Profile: topology.Profile{ID: "default"}},
		CompositionReason: "generation does not record topology composition",
		State:             &invalidation.ProfileState{Stale: true, Reason: "commit changed"},
	}); err != nil {
		t.Fatalf("addComposition() error = %v", err)
	}
	response := assembler.response()
	if response.Status != "stale" || response.Completeness.Complete || len(response.Profiles) != 1 ||
		response.Profiles[0].Status != "stale" || response.Profiles[0].CompositionComplete ||
		!strings.Contains(response.Profiles[0].Reason, "commit changed") ||
		!strings.Contains(response.Completeness.Reason, "does not record topology composition") {
		t.Fatalf("stale incomplete topology = %#v, want distinct stale and composition states", response)
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
		for _, snapshotID := range []uint64{7, 8} {
			manifest := topologyManifest(t, profile, "shared-worktree", &value)
			if err := sourceobservation.Write(topologyGenerationPath(t, configPath, profile, snapshotID), manifest); err != nil {
				t.Fatalf("write published topology composition for profile %q generation %d: %v", profile, snapshotID, err)
			}
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

func topologyGenerationPath(t *testing.T, configPath, profile string, snapshotID uint64) string {
	t.Helper()
	loaded, err := config.LoadProfile(configPath, profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(
		generation.GenerationsDir(filepath.Dir(loaded.Config.Storage.DatabasePath)),
		fmt.Sprintf("%06d", snapshotID),
	)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func topologyManifest(
	t *testing.T,
	profile string,
	worktree topology.WorktreeID,
	value *topology.Topology,
) sourceobservation.Manifest {
	t.Helper()
	observation, err := topology.NewSourceObservation(worktree, "0123456789abcdef", "main", false, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest := sourceobservation.Manifest{
		Version:             sourceobservation.CurrentVersion,
		Profile:             profile,
		ResolverVersion:     "resolver-test",
		AnalyzerFingerprint: "analyzer-test",
		Sources:             []sourceobservation.Source{{Repository: "repo", Observation: observation}},
	}
	if value == nil {
		return manifest
	}
	composition, err := value.Compose(topology.ProfileID(profile))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := sourceobservation.NewTopologyComposition(composition)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Composition = &persisted
	return manifest
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

func testSnapshotWithTopologyIDAndLanguages(t *testing.T, snapshotID uint64, languages string) *hotsnapshot.GraphSnapshot {
	t.Helper()
	return testSnapshotDataWithLanguages(t, snapshotID, true, languages)
}

func testSnapshotWithCrossRepositoryTopology(t *testing.T, snapshotID uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	return testSnapshotDataWithLanguagesAndCrossRepository(t, snapshotID, true, "go", true)
}

func testSnapshotData(t *testing.T, snapshotID uint64, includeTopologyRecords bool) *hotsnapshot.GraphSnapshot {
	t.Helper()
	return testSnapshotDataWithLanguages(t, snapshotID, includeTopologyRecords, "go")
}

func testSnapshotDataWithLanguages(t *testing.T, snapshotID uint64, includeTopologyRecords bool, repositoryLanguages string) *hotsnapshot.GraphSnapshot {
	return testSnapshotDataWithLanguagesAndCrossRepository(t, snapshotID, includeTopologyRecords, repositoryLanguages, false)
}

func testSnapshotDataWithLanguagesAndCrossRepository(
	t *testing.T,
	snapshotID uint64,
	includeTopologyRecords bool,
	repositoryLanguages string,
	includeCrossRepository bool,
) *hotsnapshot.GraphSnapshot {
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
	repositoryLanguageKey := intern(repositoryLanguages)
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
	if includeCrossRepository {
		intern("repository:provider")
		intern("package:go:provider:example")
		intern("provider")
		intern("/workspace/provider")
		intern("provider.example")
	}
	stringTable := interner.Freeze()
	repositories := []hotsnapshot.RepositoryRecord{{
		Key: repoKey, Name: interned(stringTable, "repo"), Path: interned(stringTable, "/workspace/repo"), Languages: repositoryLanguageKey,
	}}
	packages := []hotsnapshot.PackageRecord{{
		Key: packageKey, Repository: 0, Language: language, Name: interned(stringTable, "example"), ModulePath: interned(stringTable, "example"),
	}}
	packageDependencyTarget := hotsnapshot.PackageID(0)
	if includeCrossRepository {
		repositories = append(repositories, hotsnapshot.RepositoryRecord{
			Key: interned(stringTable, "repository:provider"), Name: interned(stringTable, "provider"),
			Path: interned(stringTable, "/workspace/provider"), Languages: repositoryLanguageKey,
		})
		packages = append(packages, hotsnapshot.PackageRecord{
			Key: interned(stringTable, "package:go:provider:example"), Repository: 1, Language: language,
			Name: interned(stringTable, "provider.example"), ModulePath: interned(stringTable, "provider.example"),
		})
		packageDependencyTarget = 1
	}
	var packageDependencies []hotsnapshot.PackageDependencyRecord
	var unresolved []hotsnapshot.UnresolvedReferenceRecord
	if includeTopologyRecords {
		packageDependencies = []hotsnapshot.PackageDependencyRecord{{
			Source: 0, Target: packageDependencyTarget, Kind: facts.CodePackageDependsOn, Confidence: facts.CodeCandidate,
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
		Repositories:    repositories,
		Packages:        packages,
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
