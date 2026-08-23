package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, status, response.Body.String())
	}
	var apiErr apiError
	if err := json.Unmarshal(response.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if apiErr.Code != code {
		t.Fatalf("error code = %q, want %q", apiErr.Code, code)
	}
}

func testSnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
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
	strings := interner.Freeze()

	snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID:              7,
		CreatedAt:       time.Unix(1_700_000_000, 0).UTC(),
		Version:         1,
		SchemaVersion:   2,
		ResolverVersion: "resolver-test",
		Strings:         strings,
		Repositories: []hotsnapshot.RepositoryRecord{{
			Key: repoKey, Name: interned(strings, "repo"), Path: interned(strings, "/workspace/repo"), Languages: interned(strings, "go"),
		}},
		Packages: []hotsnapshot.PackageRecord{{
			Key: packageKey, Repository: 0, Language: language, Name: interned(strings, "example"), ModulePath: interned(strings, "example"),
		}},
		Files: []hotsnapshot.FileRecord{{
			Key: fileKey, Repository: 0, Package: 0, Path: interned(strings, "src/index.go"), Language: language,
		}},
		Symbols: []hotsnapshot.SymbolRecord{
			{StableKey: 0, CanonicalIdentity: interned(strings, "identity-a"), File: 0, Language: language, Name: nameLoad, QualifiedName: qnameA, Kind: kindFunction, Signature: interned(strings, "func Load()"), StartLine: 1, EndLine: 2},
			{StableKey: 1, CanonicalIdentity: interned(strings, "identity-b"), File: 0, Language: language, Name: nameLoad, QualifiedName: qnameB, Kind: kindFunction, Signature: interned(strings, "func Loader()"), StartLine: 4, EndLine: 5},
			{StableKey: 2, CanonicalIdentity: interned(strings, "identity-c"), File: 0, Language: language, Name: nameOther, QualifiedName: qnameC, Kind: kindFunction, Signature: interned(strings, "func Other()"), StartLine: 7, EndLine: 8},
		},
		Evidence:       []hotsnapshot.EvidenceRecord{{Key: evidenceKey, SourceFile: 0, TargetFile: 0, Kind: interned(strings, "call"), Provenance: interned(strings, "GoTypesUse")}},
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
