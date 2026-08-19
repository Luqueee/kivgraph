package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestFindSymbolModesAndPagination(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))

	exact := callFindSymbolFull(t, client, map[string]any{"name": "alpha", "mode": FindSymbolModeExact})
	if exact.Total != 1 || exact.Returned != 1 || exact.Truncated || exact.NextCursor != nil {
		t.Fatalf("exact page metadata = %#v", exact)
	}
	if exact.Results[0].StableKey != "symbol-alpha" || exact.Results[0].QualifiedName != "pkg.Alpha" {
		t.Fatalf("exact result = %#v", exact.Results[0])
	}

	qualified := callFindSymbolFull(t, client, map[string]any{"name": "pkg.Alpha", "mode": FindSymbolModeQualifiedExact})
	if qualified.Total != 1 || qualified.Results[0].StableKey != "symbol-alpha" {
		t.Fatalf("qualified_exact result = %#v", qualified)
	}

	firstPrefix := callFindSymbolFull(t, client, map[string]any{"name": "alp", "mode": FindSymbolModePrefix, "limit": 1})
	if firstPrefix.Total != 2 || firstPrefix.Returned != 1 || !firstPrefix.Truncated || firstPrefix.NextCursor == nil {
		t.Fatalf("first prefix page = %#v", firstPrefix)
	}
	if firstPrefix.Results[0].Name != "alpha" {
		t.Fatalf("first prefix result = %#v", firstPrefix.Results[0])
	}
	secondPrefix := callFindSymbolFull(t, client, map[string]any{
		"name": "alp", "mode": FindSymbolModePrefix, "limit": 1, "cursor": *firstPrefix.NextCursor,
	})
	if secondPrefix.Total != 2 || secondPrefix.Returned != 1 || secondPrefix.Truncated || secondPrefix.NextCursor != nil {
		t.Fatalf("second prefix page = %#v", secondPrefix)
	}
	if secondPrefix.Results[0].Name != "alphabet" {
		t.Fatalf("second prefix result = %#v", secondPrefix.Results[0])
	}

	ambiguous := callFindSymbolFull(t, client, map[string]any{"name": "shared"})
	if ambiguous.Total != 2 || ambiguous.Returned != 2 || ambiguous.Truncated {
		t.Fatalf("ambiguous exact page = %#v", ambiguous)
	}
	if ambiguous.Results[0].StableKey != "symbol-shared-a" || ambiguous.Results[1].StableKey != "symbol-shared-b" {
		t.Fatalf("ambiguous exact ordering = %#v", ambiguous.Results)
	}

	missing := callFindSymbolFull(t, client, map[string]any{"name": "missing"})
	if missing.Total != 0 || missing.Returned != 0 || missing.Results == nil || missing.Truncated {
		t.Fatalf("missing exact page = %#v", missing)
	}
}

// TestFindSymbolFullViewKeepsTheFieldPerRowShape pins what `view: "full"`
// promises: the payload a client written before ADR 0046 parses, down to the
// envelope nulls and to `results` being the bare array of rows rather than an
// object wrapping them.
func TestFindSymbolFullViewKeepsTheFieldPerRowShape(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))
	payload := callFindSymbolJSON(t, client, map[string]any{"name": "shared", "view": ViewFull})

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("full envelope decode error = %v", err)
	}
	const wantEnvelope = "coverage,next_cursor,results,returned,snapshot_age_ms,snapshot_id,total,truncated"
	if got := jsonKeys(envelope); got != wantEnvelope {
		t.Fatalf("full envelope keys = %q, want %q", got, wantEnvelope)
	}

	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["results"], &rows); err != nil {
		t.Fatalf("full results is not an array of rows: %v (%s)", err, envelope["results"])
	}
	if len(rows) != 2 {
		t.Fatalf("full rows = %d, want 2", len(rows))
	}
	const wantRow = "end_line,exported,file_path,kind,name,qualified_name,repository,signature,stable_key,start_line"
	if got := jsonKeys(rows[0]); got != wantRow {
		t.Fatalf("full row keys = %q, want %q", got, wantRow)
	}

	// The same page decoded through the public type: the field names are the
	// contract, not just their number.
	typed := callFindSymbolFull(t, client, map[string]any{"name": "shared"})
	want := SymbolSummary{
		StableKey: "symbol-shared-a", Name: "shared", QualifiedName: "pkg.SharedA",
		Kind: "function", Signature: "func SharedA()", Exported: true,
		Repository: "repo-a", FilePath: "alpha.go", StartLine: 20, EndLine: 20,
	}
	if typed.Results[0] != want {
		t.Fatalf("full row = %#v, want %#v", typed.Results[0], want)
	}
}

// TestFindSymbolCompactPageHoistsWhatEveryRowShares is the default view: the
// name, the kind, the visibility and the repository are one header instead of
// two copies each, `stable_key` is gone, and a declaration that starts and ends
// on the same line spells that line once.
func TestFindSymbolCompactPageHoistsWhatEveryRowShares(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))
	payload := callFindSymbolJSON(t, client, map[string]any{"name": "shared"})

	const want = `{"snapshot_id":11,"total":2,"returned":2,"coverage":{"exact":2},` +
		`"results":{"name":"shared","kind":"function","exported":true,"repository":"repo-a","symbols":[` +
		`{"at":"alpha.go:20","qn":"pkg.SharedA","sig":"func SharedA()"},` +
		`{"at":"alpha.go:30","end":34,"qn":"pkg.SharedB","sig":"func SharedB()"}]}}`
	if payload != want {
		t.Fatalf("compact payload =\n%s\nwant\n%s", payload, want)
	}
	// The facts of the full view are all still readable: two declarations, both
	// `shared`, both exported functions of `repo-a`, at alpha.go:20 and
	// alpha.go:30-34, qualified pkg.SharedA and pkg.SharedB.
	if strings.Contains(payload, "stable_key") || strings.Contains(payload, "snapshot_age_ms") {
		t.Fatalf("compact payload carries a key the caller never asked for: %s", payload)
	}
}

// TestFindSymbolCompactRowsCarryWhatTheHeaderCannot is the other half of the
// rule: a column with two values stays on the row. Nothing is hoisted away that
// the rows disagree about, `exported: false` included.
func TestFindSymbolCompactRowsCarryWhatTheHeaderCannot(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))
	payload := callFindSymbolJSON(t, client, map[string]any{"name": "delta"})

	const want = `{"snapshot_id":11,"total":2,"returned":2,"coverage":{"exact":2},` +
		`"results":{"name":"delta","repository":"repo-a","symbols":[` +
		`{"at":"alpha.go:50","kind":"const","exported":true},` +
		`{"at":"alpha.go:60","qn":"pkg.Config.delta","kind":"field","exported":false}]}}`
	if payload != want {
		t.Fatalf("mixed compact payload =\n%s\nwant\n%s", payload, want)
	}
}

// TestFindSymbolCompactRowsAddressAcrossRepositories is the other branch of the
// row label: with no repository to hoist, every row spells the whole
// `repository:path:line` triple rather than leaving the caller to guess it.
func TestFindSymbolCompactRowsAddressAcrossRepositories(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))
	payload := callFindSymbolJSON(t, client, map[string]any{"name": "omega"})

	const want = `{"snapshot_id":11,"total":2,"returned":2,"coverage":{"exact":2},` +
		`"results":{"name":"omega","kind":"function","exported":true,"symbols":[` +
		`{"at":"repo-a:alpha.go:70","qn":"pkg.Omega","sig":"func Omega()"},` +
		`{"at":"repo-b:omega.go:5","qn":"pkgb.Omega","sig":"func Omega() error"}]}}`
	if payload != want {
		t.Fatalf("cross-repository compact payload =\n%s\nwant\n%s", payload, want)
	}
}

// TestFindSymbolCompactRestoresTheKeysForTheDetailedFormat covers the escape
// hatch: the keys the compact rows drop are not gone from the tool, they are
// gone from the default answer.
func TestFindSymbolCompactRestoresTheKeysForTheDetailedFormat(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))
	payload := callFindSymbolJSON(t, client, map[string]any{
		"name": "alpha", "response_format": ResponseFormatDetailed,
	})

	const want = `{"snapshot_id":11,"total":1,"returned":1,"coverage":{"exact":1},` +
		`"results":{"name":"alpha","kind":"function","exported":true,"repository":"repo-a","symbols":[` +
		`{"at":"alpha.go:10","qn":"pkg.Alpha","sig":"func Alpha()",` +
		`"stable_key":"symbol-alpha","canonical_identity":"go:alpha"}]}}`
	if payload != want {
		t.Fatalf("detailed compact payload =\n%s\nwant\n%s", payload, want)
	}
}

func TestFindSymbolRejectsUnsupportedModeAndInvalidName(t *testing.T) {
	store := symbolSnapshot(t, 11)
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha", Mode: "fuzzy"}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("unsupported mode error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: " alpha "}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("invalid name error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha", Limit: MaximumSymbolLimit + 1}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("invalid limit error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	// A declaration is not a file: the files view has to fail rather than
	// answer a different question.
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha", View: ViewFiles}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("files view error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha", View: "brief"}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("unsupported view error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha"}, hotsnapshot.NewSnapshotStore(nil)); ErrorCode(err) != CodeIndexNotReady {
		t.Fatalf("unpublished snapshot error code = %q, want %q", ErrorCode(err), CodeIndexNotReady)
	}
}

func TestFindSymbolCursorExpiresAfterSnapshotPublication(t *testing.T) {
	store := symbolSnapshot(t, 11)
	client := newSymbolToolClient(t, store)
	first := callFindSymbolFull(t, client, map[string]any{"name": "alp", "mode": FindSymbolModePrefix, "limit": 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no cursor")
	}
	secondSnapshot := buildSymbolSnapshot(t, 12)
	if err := store.Publish(secondSnapshot); err != nil {
		t.Fatalf("SnapshotStore.Publish() error = %v", err)
	}
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "find_symbol", Arguments: map[string]any{"name": "alp", "mode": FindSymbolModePrefix, "limit": 1, "cursor": *first.NextCursor},
	})
	if err != nil {
		t.Fatalf("expired cursor CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("expired cursor result = %#v, want an error", result)
	}
}

// callFindSymbolFull decodes the full view through the public row type. The view
// is spelled out because the default is the compact one.
func callFindSymbolFull(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[[]SymbolSummary] {
	t.Helper()
	full := make(map[string]any, len(arguments)+1)
	for key, value := range arguments {
		full[key] = value
	}
	full["view"] = ViewFull
	return decodeResponse[[]SymbolSummary](t, callFindSymbol(t, client, full))
}

// callFindSymbolJSON returns the payload verbatim. The compact shape is about
// which bytes travel, so the assertions are on the bytes.
func callFindSymbolJSON(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) string {
	t.Helper()
	return contentText(callFindSymbol(t, client, arguments))
}

func callFindSymbol(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "find_symbol", Arguments: arguments})
	if err != nil {
		t.Fatalf("find_symbol CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("find_symbol CallTool() returned an error: %#v", result.Content)
	}
	return result
}

// jsonKeys is the key set of one JSON object, sorted, so a shape assertion
// fails on a field that appeared as well as on one that vanished.
func jsonKeys(object map[string]json.RawMessage) string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func newSymbolToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindSymbolWithSnapshotStore(server, store)
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
	return clientSession
}

func symbolSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	return hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, id))
}

// buildSymbolSnapshot keeps most declarations in one file so a page shares its
// repository, its kind and its visibility, while the `delta` pair disagrees on
// kind and on visibility and the `omega` pair spans two repositories: those
// disagreements are the only way to tell a hoisted column from a dropped one.
func buildSymbolSnapshot(t *testing.T, id uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"},
				{Key: "repo-b", Name: "repo-b", Path: "/repo-b", Languages: "go"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "package-a", RepositoryKey: "repo-a", Name: "pkg", ModulePath: "example.com/pkg"},
				{Key: "package-b", RepositoryKey: "repo-b", Name: "pkgb", ModulePath: "example.com/pkgb"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "alpha.go"},
				{Key: "file-b", RepositoryKey: "repo-b", PackageKey: "package-b", Path: "omega.go"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{StableKey: "symbol-shared-b", CanonicalIdentity: "go:shared:b", FileKey: "file-a", Name: "shared", QualifiedName: "pkg.SharedB", Kind: "function", Signature: "func SharedB()", Exported: true, StartLine: 30, EndLine: 34},
				{StableKey: "symbol-alpha", CanonicalIdentity: "go:alpha", FileKey: "file-a", Name: "alpha", QualifiedName: "pkg.Alpha", Kind: "function", Signature: "func Alpha()", Exported: true, StartLine: 10, EndLine: 10},
				{StableKey: "symbol-shared-a", CanonicalIdentity: "go:shared:a", FileKey: "file-a", Name: "shared", QualifiedName: "pkg.SharedA", Kind: "function", Signature: "func SharedA()", Exported: true, StartLine: 20, EndLine: 20},
				{StableKey: "symbol-alphabet", CanonicalIdentity: "go:alphabet", FileKey: "file-a", Name: "alphabet", QualifiedName: "pkg.Alphabet", Kind: "function", Signature: "func Alphabet()", Exported: true, StartLine: 40, EndLine: 44},
				{StableKey: "symbol-delta-const", CanonicalIdentity: "go:delta:const", FileKey: "file-a", Name: "delta", QualifiedName: "delta", Kind: "const", Signature: "untyped string", Exported: true, StartLine: 50, EndLine: 50},
				{StableKey: "symbol-delta-field", CanonicalIdentity: "go:delta:field", FileKey: "file-a", Name: "delta", QualifiedName: "pkg.Config.delta", Kind: "field", Signature: "string", Exported: false, StartLine: 60, EndLine: 60},
				{StableKey: "symbol-omega-a", CanonicalIdentity: "go:omega:a", FileKey: "file-a", Name: "omega", QualifiedName: "pkg.Omega", Kind: "function", Signature: "func Omega()", Exported: true, StartLine: 70, EndLine: 70},
				{StableKey: "symbol-omega-b", CanonicalIdentity: "go:omega:b", FileKey: "file-b", Name: "omega", QualifiedName: "pkgb.Omega", Kind: "function", Signature: "func Omega() error", Exported: true, StartLine: 5, EndLine: 5},
			},
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

// manySymbolsSnapshot has six declarations named with the shared prefix
// `handle`: four exported functions across two files, and two unexported
// variables in a third. Every declaration shares `repository` and `name`
// disagrees only in its suffix, so kind and exported are the only columns
// left for compact to hoist or group.
func manySymbolsSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{
				{Key: "repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"},
			},
			Packages: []hotsnapshot.PackageRow{
				{Key: "package-a", RepositoryKey: "repo-a", Name: "pkg", ModulePath: "example.com/pkg"},
			},
			Files: []hotsnapshot.FileRow{
				{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "a.go"},
				{Key: "file-b", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "b.go"},
				{Key: "file-c", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "c.go"},
			},
			Symbols: []hotsnapshot.SymbolRow{
				{StableKey: "symbol-1", CanonicalIdentity: "go:1", FileKey: "file-a", Name: "handleOne", QualifiedName: "pkg.handleOne", Kind: "function", Signature: "func handleOne()", Exported: true, StartLine: 10, EndLine: 10},
				{StableKey: "symbol-2", CanonicalIdentity: "go:2", FileKey: "file-a", Name: "handleTwo", QualifiedName: "pkg.handleTwo", Kind: "function", Signature: "func handleTwo()", Exported: true, StartLine: 20, EndLine: 20},
				{StableKey: "symbol-3", CanonicalIdentity: "go:3", FileKey: "file-b", Name: "handleThree", QualifiedName: "pkg.handleThree", Kind: "function", Signature: "func handleThree()", Exported: true, StartLine: 30, EndLine: 30},
				{StableKey: "symbol-4", CanonicalIdentity: "go:4", FileKey: "file-b", Name: "handleFour", QualifiedName: "pkg.handleFour", Kind: "function", Signature: "func handleFour()", Exported: true, StartLine: 40, EndLine: 40},
				{StableKey: "symbol-5", CanonicalIdentity: "go:5", FileKey: "file-c", Name: "handleFive", QualifiedName: "pkg.handleFive", Kind: "variable", Signature: "func()", Exported: false, StartLine: 50, EndLine: 50},
				{StableKey: "symbol-6", CanonicalIdentity: "go:6", FileKey: "file-c", Name: "handleSix", QualifiedName: "pkg.handleSix", Kind: "variable", Signature: "func()", Exported: false, StartLine: 60, EndLine: 60},
			},
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

// TestFindSymbolCompactGroupsTheMajorityKindOnce is the regression guard for
// the real page that motivated this: a search of `453` rows over `kena` had 7
// distinct (kind, exported) pairs, one covering 222 of them, and repeating
// both columns on every row cost more than the search itself. Here four
// declarations share one pair across two files and two share another in a
// third, which is enough to force grouping over the flat fallback.
func TestFindSymbolCompactGroupsTheMajorityKindOnce(t *testing.T) {
	client := newSymbolToolClient(t, manySymbolsSnapshot(t, 41))

	wire := callFindSymbolJSON(t, client, map[string]any{"name": "handle", "mode": FindSymbolModePrefix, "limit": 500})
	var payload struct {
		Results struct {
			Repository string `json:"repository"`
			Symbols    []any  `json:"symbols"`
			Groups     []struct {
				Kind     string `json:"kind"`
				Exported *bool  `json:"exported"`
				Symbols  []struct {
					At  string `json:"at"`
					QN  string `json:"qn"`
					Sig string `json:"sig"`
				} `json:"symbols"`
			} `json:"groups"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(wire), &payload); err != nil {
		t.Fatalf("unmarshal compact page: %v (%s)", err, wire)
	}
	if payload.Results.Repository != "repo-a" {
		t.Fatalf("repository = %q, want it hoisted to the page", payload.Results.Repository)
	}
	if payload.Results.Symbols != nil {
		t.Fatalf("page stayed flat instead of grouping two real kinds: %#v", payload.Results.Symbols)
	}
	if len(payload.Results.Groups) != 2 {
		t.Fatalf("groups = %#v, want exactly two: function/exported and variable/unexported", payload.Results.Groups)
	}
	var functions, variables []struct {
		At  string `json:"at"`
		QN  string `json:"qn"`
		Sig string `json:"sig"`
	}
	for _, group := range payload.Results.Groups {
		switch group.Kind {
		case "function":
			if group.Exported == nil || !*group.Exported {
				t.Fatalf("function group exported = %v, want true", group.Exported)
			}
			functions = group.Symbols
		case "variable":
			if group.Exported == nil || *group.Exported {
				t.Fatalf("variable group exported = %v, want false", group.Exported)
			}
			variables = group.Symbols
		default:
			t.Fatalf("unexpected group kind %q", group.Kind)
		}
	}
	if len(functions) != 4 || len(variables) != 2 {
		t.Fatalf("function rows = %d, variable rows = %d, want 4 and 2", len(functions), len(variables))
	}
	// Every row inside either group carries only its own location and name:
	// kind and exported are entirely accounted for by the group. A function
	// row also keeps its signature -- it answers how to call it -- and a
	// variable row does not, exactly as the full view already decides per
	// kind; grouping changes where that column lives, never whether it does.
	for _, row := range functions {
		if row.At == "" || row.QN == "" || row.Sig == "" {
			t.Fatalf("function row = %#v, want at, qn and sig all present", row)
		}
	}
	for _, row := range variables {
		if row.At == "" || row.QN == "" || row.Sig != "" {
			t.Fatalf("variable row = %#v, want at and qn present, sig absent", row)
		}
	}
}
