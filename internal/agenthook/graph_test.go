package agenthook

import (
	"testing"
)

// The two fixtures below were captured from a running daemon over this
// repository's own graph, not written from the response types. That is the
// difference that matters here: the `files` view type in
// internal/mcp/tools/find_references.go declares `files` at its root, but the
// server nests it under `results` and puts the count the gate weighs in `total`
// beside it. A fixture built from the type decodes cleanly and reports zero
// references for every symbol in the graph, and the gate's crowded branch would
// then never fire.
const (
	// referencesFixture is `find_references(name="locationLabel", view="files")`.
	// The completeness block is trimmed; every key the parser reads is verbatim.
	referencesFixture = `{"snapshot_id":90,"total":5,"returned":5,` +
		`"coverage":{"exact":5},"completeness":{"verdict":"LOWER_BOUND"},` +
		`"results":{"subject":"kivgraph:internal/mcp/tools/view.go:153",` +
		`"qn":"locationLabel","direction":"incoming",` +
		`"edge_kinds_default_excluded":["EXPORTS","REEXPORTS"],"files":[` +
		`{"file":"kivgraph/internal/mcp/tools/find_references.go","count":1},` +
		`{"file":"kivgraph/internal/mcp/tools/find_symbol.go","count":1},` +
		`{"file":"kivgraph/internal/mcp/tools/find_cross_repo_consumers.go","count":1},` +
		`{"file":"kivgraph/internal/mcp/tools/blast_radius.go","count":1},` +
		`{"file":"kivgraph/internal/mcp/tools/root_symbol.go","count":1}]}}`

	// unreferencedFixture is a subject that resolved and that nothing
	// reaches: `find_references(name="NewServer", view="files")`.
	unreferencedFixture = `{"snapshot_id":90,"total":0,"returned":0,` +
		`"coverage":{"unresolved_related":4},"completeness":{"verdict":"LOWER_BOUND"},` +
		`"results":{"subject":"kivgraph:internal/mcp/server.go:20","qn":"NewServer",` +
		`"direction":"incoming","files":[]}}`

	// ambiguousFixture is the error the same call returns for a name five
	// symbols carry, verbatim.
	ambiguousFixture = `AMBIGUOUS_SYMBOL: name "Load" declares 5 symbols; ` +
		`repeat with the repository and path of the one you mean: ` +
		`kivgraph:internal/goloader/loader.go:174, kivgraph:internal/rebuild/rebuild.go:82, ` +
		`kivgraph:internal/config/config.go:771, kivgraph:internal/hotsnapshot/publication.go:75, ` +
		`kivgraph:internal/rebuild/rebuild.go:112`
)

// TestReferenceFactsReadTheCountTheServerSends is the regression the fixture
// exists for: the count lives in `total` at the envelope's root, and the rows
// under `results`.
func TestReferenceFactsReadTheCountTheServerSends(t *testing.T) {
	facts, err := referenceFacts(referencesFixture)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := Facts{Declarations: 1, Repositories: 1, References: 5, Sample: []string{
		"kivgraph/internal/mcp/tools/find_references.go",
		"kivgraph/internal/mcp/tools/find_symbol.go",
		"kivgraph/internal/mcp/tools/find_cross_repo_consumers.go",
	}}
	if !factsEqual(facts, want) {
		t.Fatalf("got %#v, want %#v", facts, want)
	}
}

// TestAResolvedSubjectNobodyReferencesIsStillOneDeclaration keeps the empty
// page from reading as "the graph does not know this name". It does know it;
// nothing reaches it, which is a fact and not a failure, and the gate's answer
// to it is to allow the grep.
func TestAResolvedSubjectNobodyReferencesIsStillOneDeclaration(t *testing.T) {
	facts, err := referenceFacts(unreferencedFixture)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := Facts{Declarations: 1, Repositories: 1, References: 0}
	if !factsEqual(facts, want) {
		t.Fatalf("got %#v, want %#v", facts, want)
	}
}

// TestAmbiguityIsReadFromTheCodeAndQuotedFromTheMessage holds the split the
// error contract asks for: the decision comes from the stable code, and the
// message is read only for the rows the refusal quotes back.
func TestAmbiguityIsReadFromTheCodeAndQuotedFromTheMessage(t *testing.T) {
	facts := ambiguityFacts(ambiguousFixture)
	want := Facts{Declarations: 5, Repositories: 1, Sample: []string{
		"kivgraph:internal/goloader/loader.go:174",
		"kivgraph:internal/rebuild/rebuild.go:82",
		"kivgraph:internal/config/config.go:771",
	}}
	if !factsEqual(facts, want) {
		t.Fatalf("got %#v, want %#v", facts, want)
	}
}

// TestOnlyAmbiguityJustifiesARefusal is the negative: every other classified
// failure -- a name the graph never saw, a graph that is not published yet --
// leaves the gate with nothing to refuse on.
func TestOnlyAmbiguityJustifiesARefusal(t *testing.T) {
	for _, text := range []string{
		`SYMBOL_NOT_FOUND: no symbol named "Zzz"`,
		`INDEX_NOT_READY: no graph is published yet`,
		`INVALID_ARGUMENT: view "files" is unsupported here`,
		``,
	} {
		if facts := ambiguityFacts(text); facts.Known() {
			t.Fatalf("refused on %q: got %#v", text, facts)
		}
	}
}

// TestAMalformedAnswerIsAnErrorAndNotZeroFacts keeps a broken response from
// reading as "one declaration, no references", which is an allow the gate would
// have reached for the wrong reason.
func TestAMalformedAnswerIsAnErrorAndNotZeroFacts(t *testing.T) {
	if _, err := referenceFacts("not json at all"); err == nil {
		t.Fatal("decoded a malformed answer without complaining")
	}
}

func factsEqual(got, want Facts) bool {
	if got.Declarations != want.Declarations ||
		got.Repositories != want.Repositories ||
		got.References != want.References ||
		len(got.Sample) != len(want.Sample) {
		return false
	}
	for index := range got.Sample {
		if got.Sample[index] != want.Sample[index] {
			return false
		}
	}
	return true
}
