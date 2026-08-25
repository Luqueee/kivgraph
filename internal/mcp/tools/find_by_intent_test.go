package tools

// Four statements of findByIntent are not covered, and none of them is
// reachable from any input this surface accepts:
//
//   - the HashQuery error, which needs a query struct that json.Marshal
//     rejects; this one is strings and ints.
//   - the offset clamp, which needs a cursor whose offset exceeds the total of
//     its own question; a cursor carries the query hash and the snapshot id, so
//     the total it is compared against is the total it was minted from.
//   - the NewCursor and Encode errors, which need an offset past
//     maxCursorOffset -- the candidate bound is four orders of magnitude below
//     it -- or a cursor that fails the validation of the constructor that just
//     built it.
//
// Covering them would mean handing the tool a value no encoder in this package
// produces. The guards stay because a future caller may not be this one.

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
// TestFindByIntentReportsOnlyTheTermsThatExplainABadAnswer is the negative
// first: a term that discriminated earns no line, because the rows are its line.
// Reporting every term cost a quarter of a payload to say `to 70` and `is 178`.
//
// What a caller can act on is reported: a word the code does not use at all, and
// a word so widely carried that it ordered nothing.
func TestFindByIntentReportsOnlyTheTermsThatExplainABadAnswer(t *testing.T) {
	store := intentStore(t, 96)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish the storage generation", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range response.Results.Terms {
		if term.Frequency == "" {
			t.Errorf("term %q was reported with nothing to say about it", term.Term)
		}
		if term.Symbols <= 0 {
			t.Errorf("term %q reported %d symbols", term.Term, term.Symbols)
		}
	}
	// `storage` is one file of this fixture and it discriminated, so it is not
	// reported; the rows carry it. `publish` is on most of the corpus here and
	// separated nothing, so it is.
	byTerm := map[string]string{}
	for _, term := range response.Results.Terms {
		byTerm[term.Term] = term.Frequency
	}
	if _, reported := byTerm["storage"]; reported {
		t.Errorf("terms = %#v, want the discriminating term left to the rows", response.Results.Terms)
	}
	if byTerm["publish"] == "" {
		t.Errorf("terms = %#v, want `publish` named as too widely carried", response.Results.Terms)
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

// TestFindByIntentAnswersThroughItsRegistration walks the path a host actually
// takes -- a JSON call over a session, not a direct handler call -- and does it
// with and without an observer, because the observed handler is a second closure
// wrapping the first and nothing else exercises it.
func TestFindByIntentAnswersThroughItsRegistration(t *testing.T) {
	store := intentStore(t, 120)
	observed := 0
	for name, register := range map[string]func(*sdkmcp.Server){
		"plain": func(server *sdkmcp.Server) { RegisterFindByIntentWithSnapshotStore(server, store) },
		"observed": func(server *sdkmcp.Server) {
			RegisterFindByIntentWithObserverAndSnapshotStore(server, func(string, time.Duration) { observed++ }, store)
		},
	} {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
		register(server)
		serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(context.Background(), serverTransport, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Cleanup(func() { _ = serverSession.Close() })
		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
		clientSession, err := client.Connect(context.Background(), clientTransport, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Cleanup(func() { _ = clientSession.Close() })

		result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: findByIntentToolName, Arguments: map[string]any{"intent": "publish generation"},
		})
		if err != nil {
			t.Fatalf("%s: CallTool() error = %v", name, err)
		}
		if result.IsError {
			t.Fatalf("%s: call returned an error result: %#v", name, result.Content)
		}
		text, ok := result.Content[0].(*sdkmcp.TextContent)
		if !ok || !strings.Contains(text.Text, "storage.PublishGeneration") {
			t.Fatalf("%s: payload does not name the production symbol: %#v", name, result.Content)
		}
	}
	if observed == 0 {
		t.Error("the observer was never called through the registered handler")
	}
}

// TestFindByIntentBoundsTheCandidateSet keeps one question from walking the
// whole graph. A term the corpus mostly carries would otherwise assemble a
// candidate per symbol before the ranking ever runs.
func TestFindByIntentBoundsTheCandidateSet(t *testing.T) {
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "repo", Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "go", Name: "pkg", ModulePath: "example.com/pkg"}},
		Files:        []hotsnapshot.FileRow{{Key: "file", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/pkg/wide.go", Language: "go"}},
	}
	const symbols = maximumIntentCandidates + 500
	for index := 0; index < symbols; index++ {
		rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{
			StableKey:         hotsnapshot.StableKey(fmt.Sprintf("sym-%06d", index)),
			CanonicalIdentity: fmt.Sprintf("go:pkg.Publish%06d", index),
			FileKey:           "file", Language: "go",
			Name:          fmt.Sprintf("Publish%06d", index),
			QualifiedName: fmt.Sprintf("pkg.Publish%06d", index),
			Kind:          "func", StartLine: uint32(index * 3), EndLine: uint32(index*3 + 1),
		})
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 121, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish", View: ViewFull,
	}, hotsnapshot.NewSnapshotStore(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != maximumIntentCandidates {
		t.Fatalf("total = %d over a corpus of %d, want the candidate bound of %d",
			response.Total, symbols, maximumIntentCandidates)
	}
}

// TestFindByIntentExcludesByPathAlone separates the two narrowings: a path
// prefix has to exclude on its own, not only alongside a kind.
func TestFindByIntentExcludesByPathAlone(t *testing.T) {
	store := intentStore(t, 122)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull, PathPrefix: "internal/storage/testdata",
		Limit: MaximumIntentLimit,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Total == 0 {
		t.Fatal("the fixture path excluded everything, including the fixture")
	}
	for _, row := range response.Results.Symbols {
		if !strings.HasPrefix(row.FilePath, "internal/storage/testdata") {
			t.Fatalf("row %#v survived a path prefix it does not satisfy", row)
		}
	}
}

// TestFindByIntentKeepsANameItsQualifiedNameDoesNotImply covers the one field a
// row withholds when it is redundant. An alias re-exported under another name is
// the real case: the qualified name does not end in the declaration's own name,
// so dropping it would lose the only spelling a reader recognises.
func TestFindByIntentKeepsANameItsQualifiedNameDoesNotImply(t *testing.T) {
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "repo", Languages: "ts"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "ts", Name: "web", ModulePath: "example.com/web"}},
		Files:        []hotsnapshot.FileRow{{Key: "file", RepositoryKey: "repo", PackageKey: "pkg", Path: "src/api/index.ts", Language: "ts"}},
		Symbols: []hotsnapshot.SymbolRow{{
			StableKey: "sym-alias", CanonicalIdentity: "ts:api.publishGenerationAlias", FileKey: "file",
			Language: "ts", Name: "publishGeneration", QualifiedName: "api.publishGenerationAlias",
			Kind: "func", Exported: true, StartLine: 5, EndLine: 9,
		}},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 123, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFull,
	}, hotsnapshot.NewSnapshotStore(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results.Symbols) != 1 {
		t.Fatalf("rows = %#v, want the one declaration", response.Results.Symbols)
	}
	if response.Results.Symbols[0].Name != "publishGeneration" {
		t.Fatalf("row = %#v, want the name kept because the qualified name does not end in it",
			response.Results.Symbols[0])
	}
}

// TestFindByIntentRefusesFiltersWithSurroundingWhitespace covers the three
// narrowing fields separately: each one is validated on its own, and a caller
// that pasted a padded value has to be told which field it was.
func TestFindByIntentRefusesFiltersWithSurroundingWhitespace(t *testing.T) {
	store := intentStore(t, 124)
	for field, arguments := range map[string]FindByIntentInput{
		"repo":        {Intent: "publish", Repo: " main"},
		"kind":        {Intent: "publish", Kind: "func "},
		"path_prefix": {Intent: "publish", PathPrefix: "\tinternal"},
	} {
		_, _, err := findByIntent(context.Background(), nil, arguments, store)
		if err == nil {
			t.Errorf("%s with surrounding whitespace was accepted", field)
			continue
		}
		if !strings.Contains(err.Error(), field) {
			t.Errorf("%s: message %q does not name the field", field, err.Error())
		}
	}
}

// TestIntentFrequencyLabelSaysNothingWithoutACorpus is a direct unit test of the
// label, for the one input the tool cannot produce: a published graph always has
// symbols, so a corpus of zero only arrives if this function is reused.
func TestIntentFrequencyLabelSaysNothingWithoutACorpus(t *testing.T) {
	for name, corpus := range map[string]int{"zero": 0, "negative": -5} {
		if label := intentFrequencyLabel(10, corpus); label != "" {
			t.Errorf("%s corpus produced the label %q, want none", name, label)
		}
	}
	if label := intentFrequencyLabel(1, 1_000); label != "" {
		t.Errorf("a rare term produced %q, want no label", label)
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

// corruptIntentSnapshot builds a snapshot by hand -- the constructor's own input,
// not the loader's rows -- so a field can carry a value no writer would produce.
// A snapshot arrives from a file, and a file can disagree with its string table.
func corruptIntentSnapshot(t *testing.T, kind, file hotsnapshot.InternedString, fileIndex uint32) *hotsnapshot.GraphSnapshot {
	t.Helper()
	interner := hotsnapshot.NewStringInterner()
	intern := func(value string) hotsnapshot.InternedString {
		id, err := interner.Intern(value)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	repositoryName, path := intern("repo"), intern("internal/pkg/publish.go")
	language, name := intern("go"), intern("publishGeneration")
	qualifiedName, validKind := intern("pkg.publishGeneration"), intern("func")
	repositoryKey, packageKey, fileKey := intern("repo"), intern("pkg"), intern("file")
	table := interner.Freeze()
	stableKeys, err := hotsnapshot.NewStableKeyTable([]hotsnapshot.StableKey{"sym-corrupt"})
	if err != nil {
		t.Fatal(err)
	}
	if kind == hotsnapshot.InternedString(0) {
		kind = validKind
	}
	if file == hotsnapshot.InternedString(0) {
		file = path
	}
	snapshot, buildErr := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID: 1, CreatedAt: time.Unix(1, 0).UTC(), Version: 1, SchemaVersion: 4, ResolverVersion: "test",
		Strings:      table,
		Repositories: []hotsnapshot.RepositoryRecord{{Key: repositoryKey, Name: repositoryName, Path: path, Languages: language}},
		Packages:     []hotsnapshot.PackageRecord{{Key: packageKey, Repository: 0, Language: language, Name: repositoryName, ModulePath: repositoryName}},
		Files:        []hotsnapshot.FileRecord{{Key: fileKey, Repository: 0, Package: 0, Path: file, Language: language}},
		Symbols: []hotsnapshot.SymbolRecord{{
			StableKey: 0, CanonicalIdentity: qualifiedName, File: hotsnapshot.FileID(fileIndex), Language: language,
			Name: name, QualifiedName: qualifiedName, Kind: kind, StartLine: 1, EndLine: 2,
		}},
		ForwardOffsets: []uint32{0, 0}, ReverseOffsets: []uint32{0, 0},
		StableKeys: stableKeys,
	})
	if buildErr != nil {
		t.Skipf("the constructor rejects this input, so the tool cannot receive it: %v", buildErr)
	}
	return snapshot
}

// TestFindByIntentRefusesASnapshotItCannotSpell covers the guard between the
// index and the answer: a symbol the term index reached, whose display strings
// the string table cannot resolve. A row half-spelled is worse than a refusal,
// because the caller would open a file the graph never named.
func TestFindByIntentRefusesASnapshotItCannotSpell(t *testing.T) {
	const pastTheTable = hotsnapshot.InternedString(9_999)
	for name, snapshot := range map[string]*hotsnapshot.GraphSnapshot{
		"invalid kind string": corruptIntentSnapshot(t, pastTheTable, 0, 0),
		"invalid file string": corruptIntentSnapshot(t, 0, pastTheTable, 0),
	} {
		_, _, err := findByIntent(context.Background(), nil, FindByIntentInput{
			Intent: "publish generation", View: ViewFull,
		}, hotsnapshot.NewSnapshotStore(snapshot))
		if err == nil {
			t.Errorf("%s: a snapshot it cannot spell was answered", name)
			continue
		}
		if !strings.Contains(err.Error(), "invalid") {
			t.Errorf("%s: message %q does not say the snapshot is at fault", name, err.Error())
		}
	}
}

// neighbourStore is a question with two plausible answers and one edge between
// them: `caller` carries "register" and calls three symbols carrying "handler",
// while `bystander` carries "register" and calls nothing. Nothing in the text
// separates them, which is the case the graph exists for.
func neighbourStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "main", Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "go", Name: "server", ModulePath: "example.com/server"}},
		Files: []hotsnapshot.FileRow{
			{Key: "file-caller", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/server/wiring.go", Language: "go"},
			{Key: "file-bystander", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/server/other.go", Language: "go"},
			{Key: "file-handlers", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/server/handlers.go", Language: "go"},
			// The orphan needs a path that carries no term of the question: a
			// file called handlers.go would hand it "handler" through its own
			// route, which is a lexical hit and not a graph one.
			{Key: "file-orphan", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/server/misc.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-a-caller", CanonicalIdentity: "go:server.registerEverything", FileKey: "file-caller",
				Language: "go", Name: "registerEverything", QualifiedName: "server.registerEverything",
				Kind: "func", StartLine: 10, EndLine: 40},
			{StableKey: "sym-b-bystander", CanonicalIdentity: "go:server.registerNothing", FileKey: "file-bystander",
				Language: "go", Name: "registerNothing", QualifiedName: "server.registerNothing",
				Kind: "func", StartLine: 10, EndLine: 40},
			{StableKey: "sym-c-handler", CanonicalIdentity: "go:server.handlerOne", FileKey: "file-handlers",
				Language: "go", Name: "handlerOne", QualifiedName: "server.handlerOne",
				Kind: "func", StartLine: 5, EndLine: 9},
			{StableKey: "sym-d-orphan", CanonicalIdentity: "go:server.callsBoth", FileKey: "file-orphan",
				Language: "go", Name: "callsBoth", QualifiedName: "server.callsBoth",
				Kind: "func", StartLine: 50, EndLine: 60},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-a-caller", TargetKey: "sym-c-handler", Kind: 1, Confidence: 8, Provenance: 3,
				EvidenceKind: "checker", EvidenceSourceFileKey: "file-caller", EvidenceTargetFileKey: "file-handlers"},
			// The orphan calls both candidates. It carries no term of the
			// question, and the graph must not turn it into an answer.
			{SourceKey: "sym-d-orphan", TargetKey: "sym-a-caller", Kind: 1, Confidence: 8, Provenance: 3,
				EvidenceKind: "checker", EvidenceSourceFileKey: "file-orphan", EvidenceTargetFileKey: "file-caller"},
			{SourceKey: "sym-d-orphan", TargetKey: "sym-b-bystander", Kind: 1, Confidence: 8, Provenance: 3,
				EvidenceKind: "checker", EvidenceSourceFileKey: "file-orphan", EvidenceTargetFileKey: "file-bystander"},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestFindByIntentNeverInventsACandidateFromTheGraph is the negative first. A
// symbol that carries no term of the question is not an answer, however many
// answers it calls: the credit is a weight on lexical evidence and never a
// substitute for it.
func TestFindByIntentNeverInventsACandidateFromTheGraph(t *testing.T) {
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "register handler", View: ViewFull, Limit: MaximumIntentLimit,
	}, neighbourStore(t, 130))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range response.Results.Symbols {
		if row.QualifiedName == "server.callsBoth" {
			t.Fatalf("a symbol carrying no term was answered because it calls two that do: %#v", row)
		}
	}
	if response.Total == 0 {
		t.Fatal("the question answered nothing at all")
	}
}

// TestFindByIntentPrefersTheSymbolThatReachesTheRest is the contract the graph
// buys. Two symbols carry the same term and nothing in their text separates
// them; the one whose resolved calls reach the other term of the question is
// the answer, and the row says so rather than calling itself lexical.
func TestFindByIntentPrefersTheSymbolThatReachesTheRest(t *testing.T) {
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "register handler", View: ViewFull, Limit: MaximumIntentLimit,
	}, neighbourStore(t, 131))
	if err != nil {
		t.Fatal(err)
	}
	caller, bystander := -1, -1
	for index, row := range response.Results.Symbols {
		switch row.QualifiedName {
		case "server.registerEverything":
			caller = index
		case "server.registerNothing":
			bystander = index
		}
	}
	if caller < 0 || bystander < 0 {
		t.Fatalf("both candidates should be answered, got %#v", response.Results.Symbols)
	}
	if caller > bystander {
		t.Fatalf("the symbol that reaches the question's other term ranked %d, behind the one that reaches nothing at %d",
			caller+1, bystander+1)
	}
	if match := response.Results.Symbols[caller].Match; match != "lexical+calls" {
		t.Errorf("the credited row says match = %q, want it to admit the edge it used", match)
	}
	if match := response.Results.Symbols[bystander].Match; match != "lexical" {
		t.Errorf("an uncredited row says match = %q, want plain lexical", match)
	}
}

// TestFindByIntentAnswersAQuestionWiderThanItsMask covers the bound on the term
// mask: a question with more terms than the mask holds still answers, and the
// terms past the bound simply earn no graph credit.
func TestFindByIntentAnswersAQuestionWiderThanItsMask(t *testing.T) {
	words := make([]string, 0, maximumNeighbourTerms+8)
	for index := 0; index < maximumNeighbourTerms+8; index++ {
		words = append(words, fmt.Sprintf("word%02d", index))
	}
	intent := "register handler " + strings.Join(words, " ")
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: intent, View: ViewFull, Limit: MaximumIntentLimit,
	}, neighbourStore(t, 132))
	if err != nil {
		t.Fatal(err)
	}
	if response.Total == 0 {
		t.Fatal("a question wider than the mask answered nothing")
	}
	if len(response.Results.Unmatched) == 0 {
		t.Error("none of the invented words was reported as unmatched")
	}
}

// TestFindByIntentCompactViewKeepsAMatchTheRowsDisagreeOn is the negative of the
// hoisting: a page where one row was credited for the calls it reaches does not
// share a match, so the header cannot state one. Hoisting `lexical` there would
// put a claim in the envelope that the row it describes does not make.
func TestFindByIntentCompactViewKeepsAMatchTheRowsDisagreeOn(t *testing.T) {
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "register handler", View: ViewCompact, Limit: MaximumIntentLimit,
	}, neighbourStore(t, 140))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.Results)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Match   string `json:"match"`
		Symbols []struct {
			QualifiedName string `json:"qualified_name"`
			Match         string `json:"match"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Match != "" {
		t.Errorf("the header states match = %q over rows that disagree", payload.Match)
	}
	byName := map[string]string{}
	for _, row := range payload.Symbols {
		byName[row.QualifiedName] = row.Match
	}
	if byName["server.registerEverything"] != "lexical+calls" {
		t.Errorf("the credited row reads %q in compact view", byName["server.registerEverything"])
	}
	if byName["server.registerNothing"] != "lexical" {
		t.Errorf("the uncredited row reads %q in compact view", byName["server.registerNothing"])
	}
}

// TestFindByIntentCountsFilesWhenFilesAreTheRows pins what a limit means. A row
// of a files view is a file, so asking for two rows returns two files -- not the
// files that happen to fall out of the first two symbols, which on this fixture
// is one file twice.
func TestFindByIntentCountsFilesWhenFilesAreTheRows(t *testing.T) {
	store := intentStore(t, 141)
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish generation", View: ViewFiles, Limit: 2,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.Results)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Files []struct {
			File    string `json:"file"`
			Symbols int    `json:"symbols"`
		} `json:"files"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Files) != 2 {
		t.Fatalf("files = %#v, want the two rows that were asked for", payload.Files)
	}
	if response.Returned != 2 {
		t.Errorf("returned = %d, want it counted in the unit the rows are in", response.Returned)
	}
	if response.Total < 2 {
		t.Errorf("total = %d, want the files the whole ranking holds", response.Total)
	}
	// A file that holds two of the ranked symbols still spends one row, and says
	// so: that count is why the row is worth reading.
	carried := 0
	for _, file := range payload.Files {
		carried += file.Symbols
	}
	if carried < 2 {
		t.Errorf("files = %#v, want the symbol counts kept", payload.Files)
	}
}
