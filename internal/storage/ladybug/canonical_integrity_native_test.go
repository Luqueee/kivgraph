//go:build ladybug && cgo

package ladybug

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
)

// TestConfidenceAndProvenanceCatalogsMatchFactsPackage parses facts.go
// directly and counts its Confidence and Provenance constant declarations.
// facts exports no enumerator of its own constants, so
// canonical_integrity.go writes the catalogs out by hand; losing a constant
// there already fails the build (every catalog entry names a real facts
// identifier), but gaining one in facts.go without updating the catalog
// would otherwise go unnoticed. This test is what catches that.
func TestConfidenceAndProvenanceCatalogsMatchFactsPackage(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filepath.Join("..", "..", "facts", "facts.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse facts.go: %v", err)
	}

	var confidenceCount, provenanceCount int
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || valueSpec.Type == nil {
				continue
			}
			ident, ok := valueSpec.Type.(*ast.Ident)
			if !ok {
				continue
			}
			switch ident.Name {
			case "Confidence":
				confidenceCount += len(valueSpec.Names)
			case "Provenance":
				provenanceCount += len(valueSpec.Names)
			}
		}
	}

	if confidenceCount != len(canonicalConfidenceValues) {
		t.Fatalf("facts.go declares %d Confidence constants, canonicalConfidenceValues in canonical_integrity.go has %d: update the catalog",
			confidenceCount, len(canonicalConfidenceValues))
	}
	if provenanceCount != len(canonicalProvenanceValues) {
		t.Fatalf("facts.go declares %d Provenance constants, canonicalProvenanceValues in canonical_integrity.go has %d: update the catalog",
			provenanceCount, len(canonicalProvenanceValues))
	}
}

// TestVerifyCanonicalIntegrityPassesOnACleanGraph proves the six rules do
// not false-positive on a graph LoadCanonical actually built: the fixture
// covers several repositories, packages, files, symbols, evidence, an
// unresolved reference and three semantic edge classes (REFERENCES,
// CALLS_DIRECT, TYPE_USES).
func TestVerifyCanonicalIntegrityPassesOnACleanGraph(t *testing.T) {
	path := buildCleanCanonicalGraph(t)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("report.Passed = false, findings = %#v", report.Findings)
	}
	if report.Violations() != 0 {
		t.Fatalf("report.Violations() = %d, want 0", report.Violations())
	}

	rules := CanonicalIntegrityRules()
	if len(report.Findings) != len(rules) {
		t.Fatalf("report has %d findings, want %d", len(report.Findings), len(rules))
	}
	for index, rule := range rules {
		finding := report.Findings[index]
		if finding.Rule != rule {
			t.Fatalf("Findings[%d].Rule = %s, want %s (CanonicalIntegrityRules order)", index, finding.Rule, rule)
		}
		if !finding.Passed || finding.Violations != 0 || len(finding.Samples) != 0 {
			t.Fatalf("rule %s = %#v, want a clean pass", rule, finding)
		}
	}
}

// TestVerifyCanonicalIntegrityDetectsExactEdgeWithoutSource injects a new
// orphan Symbol (no incoming DEFINES) and an exact confidence REFERENCES
// edge sourced from it, towards an already declared Symbol.
func TestVerifyCanonicalIntegrityDetectsExactEdgeWithoutSource(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	keys := newFixtureKeySet()
	const orphanKey = "symbol:repoA:orphan1.go:Orphan1"
	injectRawCypher(t, path,
		createOrphanSymbolCypher(orphanKey, keys.RepoA, keys.PackageA, keys.FileHelper, "Orphan1"),
		fmt.Sprintf(`MATCH (source:Symbol {stable_key: '%s'}), (target:Symbol {stable_key: '%s'})
CREATE (source)-[:REFERENCES {confidence: 'EXACT_TYPECHECKED', provenance: 'GO_TYPES_USE', evidence_key: '', source_snapshot: 1, resolver_version: 'inject-v1'}]->(target)`,
			orphanKey, fixtureSymbolHelperKey),
	)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	if report.Passed {
		t.Fatalf("report.Passed = true, want false")
	}

	source := mustFinding(t, report, RuleExactEdgeWithoutSource)
	if source.Passed || source.Violations != 1 {
		t.Fatalf("RuleExactEdgeWithoutSource = %#v, want exactly 1 violation", source)
	}
	if len(source.Samples) != 1 || source.Samples[0].Table != "REFERENCES" || source.Samples[0].Key != orphanKey {
		t.Fatalf("RuleExactEdgeWithoutSource samples = %#v, want one REFERENCES sample keyed %s", source.Samples, orphanKey)
	}

	// The orphan Symbol has no incoming DEFINES at all, so it also breaks
	// its own containment chain: RuleInvalidRepositoryOwner necessarily
	// fires for the same node, because a Symbol's ownership chain in this
	// schema ends with the very same DEFINES edge RuleExactEdgeWithoutSource
	// checks for declaration. There is no way to construct a
	// source-undeclared Symbol that still has a valid ownership chain — the
	// plan defines both rules against that one edge by design.
	ownership := mustFinding(t, report, RuleInvalidRepositoryOwner)
	if ownership.Passed || ownership.Violations != 1 {
		t.Fatalf("RuleInvalidRepositoryOwner = %#v, want exactly 1 violation (the same orphan Symbol)", ownership)
	}
	if len(ownership.Samples) != 1 || ownership.Samples[0].Table != "Symbol" || ownership.Samples[0].Key != orphanKey {
		t.Fatalf("RuleInvalidRepositoryOwner samples = %#v, want one Symbol sample keyed %s", ownership.Samples, orphanKey)
	}

	assertRulesPassed(t, report, RuleExactEdgeWithoutTarget, RuleMissingEvidenceFile, RuleDuplicateStableKey, RuleUnknownConfidence)
}

// TestVerifyCanonicalIntegrityDetectsExactEdgeWithoutTarget mirrors the
// source side test: an already declared Symbol gains an exact confidence
// CALLS_DIRECT edge towards a new orphan Symbol.
func TestVerifyCanonicalIntegrityDetectsExactEdgeWithoutTarget(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	keys := newFixtureKeySet()
	const orphanKey = "symbol:repoA:orphan2.go:Orphan2"
	injectRawCypher(t, path,
		createOrphanSymbolCypher(orphanKey, keys.RepoA, keys.PackageA, keys.FileHelper, "Orphan2"),
		fmt.Sprintf(`MATCH (source:Symbol {stable_key: '%s'}), (target:Symbol {stable_key: '%s'})
CREATE (source)-[:CALLS_DIRECT {confidence: 'EXACT_DECLARATION_MAPPED', provenance: 'GO_AST_CALL', evidence_key: '', source_snapshot: 1, resolver_version: 'inject-v1'}]->(target)`,
			fixtureSymbolHelperKey, orphanKey),
	)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	target := mustFinding(t, report, RuleExactEdgeWithoutTarget)
	if target.Passed || target.Violations != 1 {
		t.Fatalf("RuleExactEdgeWithoutTarget = %#v, want exactly 1 violation", target)
	}
	if len(target.Samples) != 1 || target.Samples[0].Table != "CALLS_DIRECT" || target.Samples[0].Key != orphanKey {
		t.Fatalf("RuleExactEdgeWithoutTarget samples = %#v, want one CALLS_DIRECT sample keyed %s", target.Samples, orphanKey)
	}

	// Same unavoidable overlap as the source side test: an undeclared
	// Symbol also breaks its own RuleInvalidRepositoryOwner chain, for the
	// same reason (see the comment in the source side test).
	ownership := mustFinding(t, report, RuleInvalidRepositoryOwner)
	if ownership.Passed || ownership.Violations != 1 {
		t.Fatalf("RuleInvalidRepositoryOwner = %#v, want exactly 1 violation (the same orphan Symbol)", ownership)
	}
	if len(ownership.Samples) != 1 || ownership.Samples[0].Table != "Symbol" || ownership.Samples[0].Key != orphanKey {
		t.Fatalf("RuleInvalidRepositoryOwner samples = %#v, want one Symbol sample keyed %s", ownership.Samples, orphanKey)
	}

	assertRulesPassed(t, report, RuleExactEdgeWithoutSource, RuleMissingEvidenceFile, RuleDuplicateStableKey, RuleUnknownConfidence)
}

// TestVerifyCanonicalIntegrityDetectsMissingEvidenceFile injects a new edge
// between two already declared symbols, naming an evidence_key that has no
// Evidence row at all. Both endpoints stay declared and no node is created,
// so this isolates the violation to RuleMissingEvidenceFile alone.
func TestVerifyCanonicalIntegrityDetectsMissingEvidenceFile(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	injectRawCypher(t, path,
		fmt.Sprintf(`MATCH (source:Symbol {stable_key: '%s'}), (target:Symbol {stable_key: '%s'})
CREATE (source)-[:REFERENCES {confidence: 'EXACT_TYPECHECKED', provenance: 'GO_TYPES_USE', evidence_key: 'evidence:does-not-exist', source_snapshot: 1, resolver_version: 'inject-v1'}]->(target)`,
			fixtureSymbolHelperKey, fixtureSymbolProcessKey),
	)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	finding := mustFinding(t, report, RuleMissingEvidenceFile)
	if finding.Passed || finding.Violations != 1 {
		t.Fatalf("RuleMissingEvidenceFile = %#v, want exactly 1 violation", finding)
	}
	wantKey := edgeViolationKey(fixtureSymbolHelperKey, fixtureSymbolProcessKey)
	if len(finding.Samples) != 1 || finding.Samples[0].Table != "REFERENCES" || finding.Samples[0].Key != wantKey {
		t.Fatalf("RuleMissingEvidenceFile samples = %#v, want one REFERENCES sample keyed %s", finding.Samples, wantKey)
	}

	assertRulesPassed(t, report, RuleExactEdgeWithoutSource, RuleExactEdgeWithoutTarget, RuleDuplicateStableKey, RuleUnknownConfidence, RuleInvalidRepositoryOwner)
}

// TestVerifyCanonicalIntegrityDetectsDuplicateStableKey injects a new File
// row that reuses the existing Main Symbol's stable_key, but wires it up
// with a real CONTAINS_FILE edge and a matching repository_key so its own
// containment chain is otherwise perfectly valid. That isolates the
// violation to RuleDuplicateStableKey: an orphaned new node would otherwise
// also fail RuleInvalidRepositoryOwner, the same overlap the source/target
// tests above hit deliberately.
func TestVerifyCanonicalIntegrityDetectsDuplicateStableKey(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	keys := newFixtureKeySet()
	injectRawCypher(t, path,
		fmt.Sprintf(`CREATE (:File {stable_key: '%s', repository_key: '%s', package_key: '%s', path: 'duplicate.go', language: 'go', content_hash: 'dup-hash', generated: false})`,
			fixtureSymbolMainKey, keys.RepoA, keys.PackageA),
		fmt.Sprintf(`MATCH (pkg:Package {stable_key: '%s'}), (file:File {stable_key: '%s'})
CREATE (pkg)-[:CONTAINS_FILE {confidence: 'STRUCTURAL_CERTAIN', provenance: 'PACKAGE_MANIFEST'}]->(file)`,
			keys.PackageA, fixtureSymbolMainKey),
	)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	finding := mustFinding(t, report, RuleDuplicateStableKey)
	if finding.Passed || finding.Violations != 2 {
		t.Fatalf("RuleDuplicateStableKey = %#v, want exactly 2 violations (one per colliding row)", finding)
	}
	seenTables := map[string]bool{}
	for _, sample := range finding.Samples {
		if sample.Key != fixtureSymbolMainKey {
			t.Fatalf("sample %#v has unexpected key, want %s", sample, fixtureSymbolMainKey)
		}
		seenTables[sample.Table] = true
	}
	if !seenTables["File"] || !seenTables["Symbol"] {
		t.Fatalf("RuleDuplicateStableKey samples = %#v, want one File row and one Symbol row", finding.Samples)
	}

	assertRulesPassed(t, report, RuleExactEdgeWithoutSource, RuleExactEdgeWithoutTarget, RuleMissingEvidenceFile, RuleUnknownConfidence, RuleInvalidRepositoryOwner)
}

// TestVerifyCanonicalIntegrityDetectsUnknownConfidence covers both halves of
// RuleUnknownConfidence: an edge whose confidence is not a facts.Confidence
// value at all, and an edge that claims an exact confidence backed by
// TREE_SITTER_SYNTAX provenance, which facts.Provenance.Exact reports false
// for. Both edges connect already declared symbols and create no node, so
// neither sub-case touches any other rule.
func TestVerifyCanonicalIntegrityDetectsUnknownConfidence(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	injectRawCypher(t, path,
		fmt.Sprintf(`MATCH (source:Symbol {stable_key: '%s'}), (target:Symbol {stable_key: '%s'})
CREATE (source)-[:REFERENCES {confidence: 'BOGUS_CONFIDENCE', provenance: 'GO_TYPES_USE', evidence_key: '', source_snapshot: 1, resolver_version: 'inject-v1'}]->(target)`,
			fixtureSymbolProcessKey, fixtureSymbolHelperKey),
		fmt.Sprintf(`MATCH (source:Symbol {stable_key: '%s'}), (target:Symbol {stable_key: '%s'})
CREATE (source)-[:TYPE_USES {confidence: 'STRUCTURAL_CERTAIN', provenance: 'TREE_SITTER_SYNTAX', evidence_key: '', source_snapshot: 1, resolver_version: 'inject-v1'}]->(target)`,
			fixtureSymbolProcessKey, fixtureSymbolAppKey),
	)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	finding := mustFinding(t, report, RuleUnknownConfidence)
	if finding.Passed || finding.Violations != 2 {
		t.Fatalf("RuleUnknownConfidence = %#v, want exactly 2 violations", finding)
	}
	wantTables := map[string]bool{"REFERENCES": true, "TYPE_USES": true}
	for _, sample := range finding.Samples {
		if !wantTables[sample.Table] {
			t.Fatalf("unexpected sample table %q: %#v", sample.Table, sample)
		}
		delete(wantTables, sample.Table)
	}
	if len(wantTables) != 0 {
		t.Fatalf("RuleUnknownConfidence samples = %#v, missing tables %v", finding.Samples, wantTables)
	}

	assertRulesPassed(t, report, RuleExactEdgeWithoutSource, RuleExactEdgeWithoutTarget, RuleMissingEvidenceFile, RuleDuplicateStableKey, RuleInvalidRepositoryOwner)
}

// TestVerifyCanonicalIntegrityDetectsInvalidRepositoryOwnership sets an
// existing, properly contained File's repository_key to a different, real
// repository — exactly the example the plan gives: "un File cuyo
// repository_key contradice su cadena de contención". The mutation touches
// no edge, no confidence/provenance and no stable_key, so it is fully
// isolated to RuleInvalidRepositoryOwner.
func TestVerifyCanonicalIntegrityDetectsInvalidRepositoryOwnership(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	keys := newFixtureKeySet()
	injectRawCypher(t, path,
		fmt.Sprintf(`MATCH (file:File {stable_key: '%s'}) SET file.repository_key = '%s'`, keys.FileHelper, keys.RepoB),
	)

	report, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}

	finding := mustFinding(t, report, RuleInvalidRepositoryOwner)
	if finding.Passed || finding.Violations != 1 {
		t.Fatalf("RuleInvalidRepositoryOwner = %#v, want exactly 1 violation", finding)
	}
	if len(finding.Samples) != 1 || finding.Samples[0].Table != "File" || finding.Samples[0].Key != keys.FileHelper {
		t.Fatalf("RuleInvalidRepositoryOwner samples = %#v, want one File sample keyed %s", finding.Samples, keys.FileHelper)
	}

	assertRulesPassed(t, report, RuleExactEdgeWithoutSource, RuleExactEdgeWithoutTarget, RuleMissingEvidenceFile, RuleDuplicateStableKey, RuleUnknownConfidence)
}

// TestVerifyCanonicalIntegritySamplesAreCappedAndDeterministic injects 26
// RuleUnknownConfidence violations (13 Symbol-Symbol semantic tables times
// 2 fresh symbol pairs, well over MaxIntegritySamples) and checks that
// Violations still counts all of them while Samples is capped to exactly
// MaxIntegritySamples, and that two independent runs return the identical,
// (table, key) sorted list.
func TestVerifyCanonicalIntegritySamplesAreCappedAndDeterministic(t *testing.T) {
	path := buildCleanCanonicalGraph(t)

	var symbolTables []string
	for _, group := range semanticRelationshipGroups(func(table SchemaRelationshipTable) string { return table.From }) {
		if group.NodeType == "Symbol" {
			symbolTables = group.Tables
		}
	}
	if len(symbolTables) == 0 {
		t.Fatalf("no Symbol-Symbol semantic relationship tables found")
	}

	pairs := [][2]string{
		{fixtureSymbolHelperKey, fixtureSymbolProcessKey},
		{fixtureSymbolProcessKey, fixtureSymbolHelperKey},
	}
	var statements []string
	for _, pair := range pairs {
		for _, table := range symbolTables {
			statements = append(statements, fmt.Sprintf(`MATCH (source:Symbol {stable_key: '%s'}), (target:Symbol {stable_key: '%s'})
CREATE (source)-[:%s {confidence: 'BOGUS_CONFIDENCE', provenance: 'GO_TYPES_USE', evidence_key: '', source_snapshot: 1, resolver_version: 'inject-v1'}]->(target)`,
				pair[0], pair[1], table))
		}
	}
	injected := len(statements)
	if injected <= MaxIntegritySamples {
		t.Fatalf("test setup only injected %d violations, want more than MaxIntegritySamples=%d", injected, MaxIntegritySamples)
	}
	injectRawCypher(t, path, statements...)

	first, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() first run error = %v", err)
	}
	second, err := VerifyCanonicalIntegrity(context.Background(), path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() second run error = %v", err)
	}

	firstFinding := mustFinding(t, first, RuleUnknownConfidence)
	secondFinding := mustFinding(t, second, RuleUnknownConfidence)

	if firstFinding.Violations != int64(injected) {
		t.Fatalf("Violations = %d, want %d (uncapped)", firstFinding.Violations, injected)
	}
	if len(firstFinding.Samples) != MaxIntegritySamples {
		t.Fatalf("len(Samples) = %d, want exactly MaxIntegritySamples=%d", len(firstFinding.Samples), MaxIntegritySamples)
	}
	if !reflect.DeepEqual(firstFinding.Samples, secondFinding.Samples) {
		t.Fatalf("two runs returned different samples:\nfirst:  %#v\nsecond: %#v", firstFinding.Samples, secondFinding.Samples)
	}
	for index := 1; index < len(firstFinding.Samples); index++ {
		previous, current := firstFinding.Samples[index-1], firstFinding.Samples[index]
		if previous.Table > current.Table || (previous.Table == current.Table && previous.Key > current.Key) {
			t.Fatalf("samples not sorted by (table, key) at index %d: %#v then %#v", index, previous, current)
		}
	}
}

// buildCleanCanonicalGraph loads canonicalFixtureSet (defined in
// canonical_load_native_test.go) into a fresh database and returns its path.
func buildCleanCanonicalGraph(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	set := canonicalFixtureSet(t)
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "integrity-test-v1"}
	path := filepath.Join(t.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, set, options); err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}
	return path
}

// injectRawCypher writes directly to the database at path, bypassing
// facts.Set.Validate entirely: it is the only way any of these tests can
// introduce a violation, since LoadCanonical refuses invalid facts.
func injectRawCypher(t *testing.T, path string, statements ...string) {
	t.Helper()
	ctx := context.Background()
	db, err := openCanonicalDatabase(ctx, path, false)
	if err != nil {
		t.Fatalf("open for injection: %v", err)
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		t.Fatalf("connect for injection: %v", err)
	}
	defer connection.Close()
	for _, statement := range statements {
		if err := queryWithDeadline(ctx, connection.native, statement); err != nil {
			t.Fatalf("inject %q: %v", statement, err)
		}
	}
}

// mustFinding fetches one rule's finding or fails the test immediately: a
// missing finding means VerifyCanonicalIntegrity skipped a rule, which is
// itself a bug worth failing loudly on rather than a nil-checked assertion.
func mustFinding(t *testing.T, report CanonicalIntegrityReport, rule IntegrityRule) IntegrityFinding {
	t.Helper()
	finding, ok := report.Finding(rule)
	if !ok {
		t.Fatalf("report has no finding for rule %s", rule)
	}
	return finding
}

// assertRulesPassed fails the test if any of rules did not pass cleanly in
// report: the negative-case tests use this to prove their injection did not
// drag unrelated rules down as false positives.
func assertRulesPassed(t *testing.T, report CanonicalIntegrityReport, rules ...IntegrityRule) {
	t.Helper()
	for _, rule := range rules {
		if finding := mustFinding(t, report, rule); !finding.Passed {
			t.Fatalf("rule %s unexpectedly failed: %#v", rule, finding)
		}
	}
}

// createOrphanSymbolCypher renders a CREATE for a Symbol with no incoming
// DEFINES: every column the canonical schema declares is set explicitly, so
// the row is well formed in every respect except the one the test cares
// about (it is not declared by any File).
func createOrphanSymbolCypher(stableKey, repositoryKey, packageKey, fileKey, name string) string {
	return fmt.Sprintf(`CREATE (:Symbol {stable_key: '%s', canonical_identity: 'go:%s', repository_key: '%s', package_key: '%s', file_key: '%s', language: 'go', name: '%s', qualified_name: '%s', kind: 'function', exported: true, signature: 'func %s()', start_line: 1, start_column: 0, start_offset: 0, end_line: 2, end_offset: 10})`,
		stableKey, name, repositoryKey, packageKey, fileKey, name, name, name)
}

// fixtureKeySet recomputes the durable keys canonicalFixtureSet built its
// graph from, using the same facts key builders, so injection statements
// never hand-copy a string canonicalFixtureSet itself derives.
type fixtureKeySet struct {
	RepoA, RepoB string
	PackageA     string
	FileHelper   string
}

func newFixtureKeySet() fixtureKeySet {
	return fixtureKeySet{
		RepoA:      facts.RepositoryKey("repoA"),
		RepoB:      facts.RepositoryKey("repoB"),
		PackageA:   facts.PackageKey(facts.LanguageGo, "repoA", "main"),
		FileHelper: facts.FileKey("repoA", "helper.go"),
	}
}
