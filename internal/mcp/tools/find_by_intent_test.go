package tools

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// intentStore is a corpus with the three shapes a retrieval has to tell apart:
// production code, a fixture that looks exactly like it, and an import row that
// names a symbol without being it.
func intentStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-main", Name: "main", Languages: "go"},
			{Key: "repo-other", Name: "other", Languages: "ts"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-main", RepositoryKey: "repo-main", Language: "go", Name: "storage", ModulePath: "example.com/main"},
			{Key: "pkg-other", RepositoryKey: "repo-other", Language: "ts", Name: "web", ModulePath: "example.com/other"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-prod", RepositoryKey: "repo-main", PackageKey: "pkg-main", Path: "internal/storage/publish.go", Language: "go"},
			{Key: "file-fixture", RepositoryKey: "repo-main", PackageKey: "pkg-main", Path: "internal/storage/testdata/publish.go", Language: "go"},
			{Key: "file-web", RepositoryKey: "repo-other", PackageKey: "pkg-other", Path: "src/api/client.ts", Language: "ts"},
		},
		Symbols: append([]hotsnapshot.SymbolRow{
			{StableKey: "sym-publish", CanonicalIdentity: "go:storage.PublishGeneration", FileKey: "file-prod",
				Language: "go", Name: "PublishGeneration", QualifiedName: "storage.PublishGeneration",
				Kind: "func", Exported: true, StartLine: 40, EndLine: 80},
			{StableKey: "sym-fixture", CanonicalIdentity: "go:testdata.PublishGeneration", FileKey: "file-fixture",
				Language: "go", Name: "PublishGeneration", QualifiedName: "testdata.PublishGeneration",
				Kind: "func", Exported: true, StartLine: 10, EndLine: 20},
			{StableKey: "sym-import", CanonicalIdentity: "ts:client.publishGeneration", FileKey: "file-web",
				Language: "ts", Name: "publishGeneration", QualifiedName: "client.publishGeneration",
				Kind: "import", Exported: false, StartLine: 3, EndLine: 3},
			{StableKey: "sym-reader", CanonicalIdentity: "go:storage.readGeneration", FileKey: "file-prod",
				Language: "go", Name: "readGeneration", QualifiedName: "storage.readGeneration",
				Kind: "func", StartLine: 90, EndLine: 110},
		}, ballast()...),
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// --- negatives first, per AGENTS.md ---

// TestFindByIntentRefusesAnAskItCannotAnswer covers the inputs that describe no
// question. A rejection is checked by its message as well as its error: a
// caller told only "invalid" cannot fix the call.
func TestFindByIntentRefusesAnAskItCannotAnswer(t *testing.T) {
	store := intentStore(t, 91)
	for name, arguments := range map[string]FindByIntentInput{
		"no intent":         {},
		"blank intent":      {Intent: "   \t\n"},
		"intent too long":   {Intent: strings.Repeat("publish ", 60)},
		"empty keyword":     {Intent: "publish", Keywords: []string{"generation", ""}},
		"too many keywords": {Intent: "publish", Keywords: make([]string, MaximumIntentKeywords+1)},
		"limit below one":   {Intent: "publish", Limit: -1},
		"limit past max":    {Intent: "publish", Limit: MaximumIntentLimit + 1},
		"unknown view":      {Intent: "publish", View: "graph"},
		"unknown format":    {Intent: "publish", ResponseFormat: "yaml"},
	} {
		_, _, err := findByIntent(context.Background(), nil, arguments, store)
		if err == nil {
			t.Errorf("%s was accepted, want a refusal", name)
			continue
		}
		if message := err.Error(); message == "" || !strings.Contains(strings.ToLower(message), strings.ToLower(firstWord(name))) &&
			!strings.Contains(message, "intent") && !strings.Contains(message, "keyword") &&
			!strings.Contains(message, "limit") && !strings.Contains(message, "view") &&
			!strings.Contains(message, "response_format") {
			t.Errorf("%s: message %q names nothing the caller can fix", name, message)
		}
	}
}

// TestFindByIntentSeparatesTheThreeEmpties is the honesty contract. Nothing
// matched, nothing matched within the narrowing, and nothing was asked are three
// different answers, and a caller that reads them as one concludes an absence
// this tool never established.
func TestFindByIntentSeparatesTheThreeEmpties(t *testing.T) {
	store := intentStore(t, 92)

	// Nothing in the graph carries these words.
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "quantum tunnelling entropy",
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 0 || len(response.Results.Unmatched) == 0 {
		t.Fatalf("unknown vocabulary = %#v, want no rows and the words listed as unmatched", response.Results)
	}
	if !strings.Contains(response.Guidance, "no prose") {
		t.Errorf("guidance = %q, want it to say the index holds no prose", response.Guidance)
	}

	// The terms match, but the narrowing excludes every candidate. That is not
	// the same as the graph not holding them.
	_, response, err = findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", Repo: "nonexistent-repo",
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 0 || len(response.Results.Terms) == 0 {
		t.Fatalf("excluded by narrowing = %#v, want no rows but the terms reported as matching", response.Results)
	}
	if !strings.Contains(response.Guidance, "narrowing") {
		t.Errorf("guidance = %q, want it to blame the narrowing rather than the graph", response.Guidance)
	}

	// A question of single characters folds to no term at all, which is a third
	// thing: nothing was actually asked.
	_, response, err = findByIntent(context.Background(), nil, FindByIntentInput{Intent: "a b c"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 0 || len(response.Results.Terms) != 0 || len(response.Results.Unmatched) != 0 {
		t.Fatalf("no terms = %#v, want nothing matched and nothing unmatched", response.Results)
	}
	if !strings.Contains(response.Guidance, "ask with words") {
		t.Errorf("guidance = %q, want it to say the question folded to nothing", response.Guidance)
	}
}

// TestFindByIntentRefusesAStaleCursor keeps a page from being served against a
// graph or a question it was not cut from.
func TestFindByIntentRefusesAStaleCursor(t *testing.T) {
	store := intentStore(t, 93)
	base := FindByIntentInput{Intent: "publish generation", Limit: 1}
	_, response, err := findByIntent(context.Background(), nil, base, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.NextCursor == nil {
		t.Fatal("a limited page produced no cursor")
	}
	cursor := *response.NextCursor

	if _, _, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "read generation", Limit: 1, Cursor: cursor,
	}, store); err == nil {
		t.Error("a cursor from another question was accepted")
	}
	if _, _, err := findByIntent(context.Background(), nil, base, intentStore(t, 94)); err == nil {
		_, _, err = findByIntent(context.Background(), nil, FindByIntentInput{
			Intent: base.Intent, Limit: 1, Cursor: cursor,
		}, intentStore(t, 94))
		if err == nil {
			t.Error("a cursor from another snapshot was accepted")
		}
	}
	if _, _, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: base.Intent, Cursor: "not-a-cursor",
	}, store); err == nil {
		t.Error("a malformed cursor was accepted")
	}
}

// TestFindByIntentWithoutAGraphSaysSo keeps the fail-closed contract: no
// snapshot is not an empty answer.
func TestFindByIntentWithoutAGraphSaysSo(t *testing.T) {
	if _, _, err := findByIntent(context.Background(), nil, FindByIntentInput{Intent: "publish"}, nil); err == nil {
		t.Error("a call without a snapshot store was answered")
	}
	if _, _, err := findByIntent(context.Background(), nil, FindByIntentInput{Intent: "publish"},
		hotsnapshot.NewSnapshotStore(nil)); err == nil {
		t.Error("a call against an empty store was answered")
	}
}

// --- and then the answer itself ---

// TestFindByIntentRanksProductionOverFixtureAndImport is the ordering the whole
// ranker exists to produce, over rows that are otherwise identical: the same
// name, in three places, one of which is the answer.
func TestFindByIntentRanksProductionOverFixtureAndImport(t *testing.T) {
	store := intentStore(t, 95)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Total < 3 {
		t.Fatalf("total = %d, want the three declarations that carry the term", response.Total)
	}
	order := make([]string, 0, len(response.Results.Symbols))
	for _, symbol := range response.Results.Symbols {
		order = append(order, symbol.QualifiedName)
	}
	if order[0] != "storage.PublishGeneration" {
		t.Fatalf("order = %v, want production code first", order)
	}
	last := order[len(order)-1]
	if last != "client.publishGeneration" {
		t.Fatalf("order = %v, want the import row last", order)
	}
	// Every row says how it was matched, because none of them is an edge.
	for _, symbol := range response.Results.Symbols {
		if symbol.Match != "lexical" {
			t.Fatalf("row %q carries match %q, want lexical", symbol.QualifiedName, symbol.Match)
		}
	}
	// And no row carries a score: it orders candidates and means nothing alone.
	encoded, err := json.Marshal(response.Results)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "score") {
		t.Fatalf("payload carries a score: %s", encoded)
	}
}

// TestFindByIntentReportsWhatEachTermReached is the section this tool adds over
// the tool it was modelled on: a caller can see which of its words did the work
// and which merely matched everything.
func TestFindByIntentReportsWhatEachTermReached(t *testing.T) {
	store := intentStore(t, 96)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish the storage generation", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results.Terms) == 0 {
		t.Fatal("no terms were reported")
	}
	byTerm := map[string]int{}
	for _, term := range response.Results.Terms {
		if term.Symbols <= 0 {
			t.Errorf("term %q reported %d symbols", term.Term, term.Symbols)
		}
		byTerm[term.Term] = term.Symbols
	}
	if byTerm["publish"] == 0 {
		t.Errorf("terms = %#v, want `publish` among them", response.Results.Terms)
	}
	// `the` folds to a term and reaches nothing here, so it must be reported as
	// unmatched rather than dropped in silence: that is the difference between a
	// word the code does not use and a word the tool ignored.
	if !containsString(response.Results.Unmatched, "the") {
		t.Errorf("unmatched = %v, want `the` listed", response.Results.Unmatched)
	}
}

// TestFindByIntentAnswersWithFilesFirst covers the granularity the question is
// usually asked at: which files to open, not which symbols exist.
func TestFindByIntentAnswersWithFilesFirst(t *testing.T) {
	store := intentStore(t, 97)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFiles,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.Results)
	if err != nil {
		t.Fatal(err)
	}
	var files struct {
		Files []struct {
			File    string `json:"file"`
			Repo    string `json:"repo"`
			Symbols int    `json:"symbols"`
		} `json:"files"`
		Symbols []json.RawMessage `json:"symbols"`
	}
	if err := json.Unmarshal(encoded, &files); err != nil {
		t.Fatal(err)
	}
	if len(files.Symbols) != 0 {
		t.Fatalf("the files view carried %d symbol rows, want none", len(files.Symbols))
	}
	if len(files.Files) == 0 {
		t.Fatalf("the files view carried no files: %s", encoded)
	}
	if files.Files[0].File != "internal/storage/publish.go" || files.Files[0].Repo != "main" {
		t.Fatalf("first file = %#v, want the production file of the main repository", files.Files[0])
	}
	// Two candidates share the production file, so its count says two.
	if files.Files[0].Symbols != 2 {
		t.Fatalf("first file holds %d symbols, want the two declarations of that file", files.Files[0].Symbols)
	}
}

// TestFindByIntentPagesDeterministically is what the cursor depends on: two
// calls of one question must produce one sequence, or a second page skips and
// repeats rows.
func TestFindByIntentPagesDeterministically(t *testing.T) {
	store := intentStore(t, 98)
	whole := FindByIntentInput{Intent: "publish generation", View: ViewFull, Limit: MaximumIntentLimit}
	_, full, err := findByIntent(context.Background(), nil, whole, store)
	if err != nil {
		t.Fatal(err)
	}

	seen := make([]string, 0, full.Total)
	arguments := FindByIntentInput{Intent: whole.Intent, View: ViewFull, Limit: 1}
	for page := 0; page < full.Total; page++ {
		_, response, err := findByIntent(context.Background(), nil, arguments, store)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(response.Results.Symbols) != 1 {
			t.Fatalf("page %d returned %d rows, want one", page, len(response.Results.Symbols))
		}
		seen = append(seen, response.Results.Symbols[0].QualifiedName)
		if response.NextCursor == nil {
			break
		}
		arguments.Cursor = *response.NextCursor
	}
	if len(seen) != full.Total {
		t.Fatalf("paging saw %d rows, the whole answer holds %d", len(seen), full.Total)
	}
	for index, symbol := range full.Results.Symbols {
		if seen[index] != symbol.QualifiedName {
			t.Fatalf("row %d paged as %q, whole answer has %q", index, seen[index], symbol.QualifiedName)
		}
	}
}

// TestFindByIntentNarrowsWithoutReordering keeps the filters honest: they choose
// which candidates are considered, and the survivors keep the order they had.
func TestFindByIntentNarrowsWithoutReordering(t *testing.T) {
	store := intentStore(t, 99)
	_, wide, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull, Limit: MaximumIntentLimit,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	_, narrow, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull, Limit: MaximumIntentLimit,
		Kind: "func", PathPrefix: "internal/storage",
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if narrow.Total >= wide.Total {
		t.Fatalf("narrowing kept %d of %d rows, want fewer", narrow.Total, wide.Total)
	}
	position := 0
	for _, row := range wide.Results.Symbols {
		if position < len(narrow.Results.Symbols) && narrow.Results.Symbols[position].QualifiedName == row.QualifiedName {
			position++
		}
	}
	if position != len(narrow.Results.Symbols) {
		t.Fatalf("the narrowed page is not a subsequence of the wide one: %v against %v",
			narrow.Results.Symbols, wide.Results.Symbols)
	}
	for _, row := range narrow.Results.Symbols {
		if row.Kind != "func" || !strings.HasPrefix(row.FilePath, "internal/storage") {
			t.Fatalf("row %#v survived a narrowing it does not satisfy", row)
		}
	}
}

// TestFindByIntentWithholdsKeysUntilAsked keeps the concise row concise: a
// repository, a path and a qualified name already address the next call.
func TestFindByIntentWithholdsKeysUntilAsked(t *testing.T) {
	store := intentStore(t, 100)
	_, concise, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range concise.Results.Symbols {
		if row.StableKey != "" {
			t.Fatalf("concise row %q carries a stable key", row.QualifiedName)
		}
	}
	_, detailed, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull, ResponseFormat: ResponseFormatDetailed,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range detailed.Results.Symbols {
		if row.StableKey == "" {
			t.Fatalf("detailed row %q withheld its stable key", row.QualifiedName)
		}
	}
}

// ballast makes the corpus wide enough for the terms of a question to be rare.
//
// Without it the fixture wins, and correctly: in a corpus of four symbols every
// term is carried by every symbol, so the frequency discount zeroes the text
// entirely and the order falls to the structural signals alone. That is real
// behaviour worth knowing -- it is why Score has a floor -- but it is not the
// behaviour these tests are about, and a fixture that makes every term
// corpus-wide proves nothing about ranking a term that is not.
// TestFindByIntentIsRegisteredReadOnly walks the registration overloads a host
// actually calls, and pins the annotation: a retrieval reads and must never be
// offered as something that writes.
func TestFindByIntentIsRegisteredReadOnly(t *testing.T) {
	store := intentStore(t, 110)
	for name, register := range map[string]func(*sdkmcp.Server){
		"bare":     RegisterFindByIntent,
		"observer": func(server *sdkmcp.Server) { RegisterFindByIntentWithObserver(server, nil) },
		"store":    func(server *sdkmcp.Server) { RegisterFindByIntentWithSnapshotStore(server, store) },
		"observed": func(server *sdkmcp.Server) {
			RegisterFindByIntentWithObserverAndSnapshotStore(server, func(string, time.Duration) {}, store)
		},
	} {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
		register(server)
		serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(context.Background(), serverTransport, nil)
		if err != nil {
			t.Fatalf("%s: server.Connect() error = %v", name, err)
		}
		t.Cleanup(func() { _ = serverSession.Close() })
		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
		clientSession, err := client.Connect(context.Background(), clientTransport, nil)
		if err != nil {
			t.Fatalf("%s: client.Connect() error = %v", name, err)
		}
		t.Cleanup(func() { _ = clientSession.Close() })

		listed, err := clientSession.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("%s: ListTools() error = %v", name, err)
		}
		found := false
		for _, tool := range listed.Tools {
			if tool.Name != findByIntentToolName {
				continue
			}
			found = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s: annotations = %#v, want read-only", name, tool.Annotations)
			}
			if tool.Description == "" {
				t.Fatalf("%s: no description, which is the only routing a deferred surface carries", name)
			}
		}
		if !found {
			t.Fatalf("%s: find_by_intent is not registered", name)
		}
	}
}

// TestFindByIntentCompactViewStatesTheMatchOnce is the granularity contract:
// every row of a retrieval matched text, so the page says so once in its header
// instead of on each row.
func TestFindByIntentCompactViewStatesTheMatchOnce(t *testing.T) {
	store := intentStore(t, 111)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation",
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Results.View != ViewCompact {
		t.Fatalf("view = %q, want compact by default", response.Results.View)
	}
	encoded, err := json.Marshal(response.Results)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Match   string `json:"match"`
		Symbols []struct {
			QualifiedName string `json:"qualified_name"`
			Match         string `json:"match"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(encoded, &page); err != nil {
		t.Fatal(err)
	}
	if page.Match != "lexical" {
		t.Fatalf("header match = %q, want lexical stated once", page.Match)
	}
	if len(page.Symbols) == 0 {
		t.Fatalf("compact page carried no rows: %s", encoded)
	}
	for _, row := range page.Symbols {
		if row.Match != "" {
			t.Fatalf("row %q repeats the match the header states", row.QualifiedName)
		}
	}
}

// TestFindByIntentSaysWhenATermMatchedTooMuch covers the label that warns a
// caller its own word did the damage. The words are not numbers on purpose: a
// count derived from the graph would change every reindex, inside text a client
// may cache.
func TestFindByIntentSaysWhenATermMatchedTooMuch(t *testing.T) {
	store := intentStore(t, 112)
	// `unrelated` names most of the ballast, and `func` is the kind of nearly
	// everything, so between them the two labels appear.
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "unrelated func publish storage", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, term := range response.Results.Terms {
		labels[term.Term] = term.Frequency
	}
	if !strings.Contains(labels["unrelated"], "separated little") {
		t.Errorf("labels = %#v, want the corpus-wide term marked", labels)
	}
	// `publish` names a handful of symbols out of sixty-odd, which is over the
	// threshold for `common` and nowhere near corpus-wide: three labels, three
	// bands, and the rarest of the three carries none.
	if labels["publish"] != "common" {
		t.Errorf("labels = %#v, want `publish` marked common", labels)
	}
	if labels["storage"] != "" {
		t.Errorf("the rarest term carried the label %q, want none", labels["storage"])
	}
	for _, label := range labels {
		for _, digit := range "0123456789" {
			if strings.ContainsRune(label, digit) {
				t.Fatalf("label %q carries a digit derived from the graph", label)
			}
		}
	}
}

func ballast() []hotsnapshot.SymbolRow {
	rows := make([]hotsnapshot.SymbolRow, 0, 60)
	for index := 0; index < 60; index++ {
		name := fmt.Sprintf("Unrelated%02d", index)
		rows = append(rows, hotsnapshot.SymbolRow{
			StableKey:         hotsnapshot.StableKey(fmt.Sprintf("sym-ballast-%02d", index)),
			CanonicalIdentity: fmt.Sprintf("go:web.%s", name),
			FileKey:           "file-web",
			Language:          "ts", Name: name, QualifiedName: "web." + name,
			Kind: "func", StartLine: uint32(100 + index*3), EndLine: uint32(101 + index*3),
		})
	}
	return rows
}

func firstWord(text string) string {
	if index := strings.IndexByte(text, ' '); index > 0 {
		return text[:index]
	}
	return text
}
