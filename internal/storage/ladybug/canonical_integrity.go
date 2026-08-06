package ladybug

import (
	"errors"
	"sort"

	"github.com/Luqueee/ladygraph/internal/facts"
)

// ErrCanonicalIntegrity reports that VerifyCanonicalIntegrity could not run
// to completion. A violated invariant is never this error: it is a finding
// in the returned report. Only a failure of the verification engine itself
// (a query that could not execute, a rule with no evaluator) is.
var ErrCanonicalIntegrity = errors.New("canonical graph integrity failed")

// MaxIntegritySamples bounds the offending rows reported per rule: a broken
// graph must be diagnosable without dumping the whole graph.
const MaxIntegritySamples = 20

// IntegrityRule names one canonical graph invariant.
type IntegrityRule string

const (
	// RuleExactEdgeWithoutSource: an exact confidence semantic edge whose
	// source node carries no declaring structural edge of its own.
	RuleExactEdgeWithoutSource IntegrityRule = "exact_edge_without_source"
	// RuleExactEdgeWithoutTarget mirrors RuleExactEdgeWithoutSource for the
	// target side of the same semantic edges.
	RuleExactEdgeWithoutTarget IntegrityRule = "exact_edge_without_target"
	// RuleMissingEvidenceFile: a semantic edge names an evidence_key whose
	// Evidence row is missing, or is not observed in any File.
	RuleMissingEvidenceFile IntegrityRule = "missing_evidence_file"
	// RuleDuplicateStableKey: the same stable_key is used by two different
	// node tables, which would collide in the HotSnapshot keyspace.
	RuleDuplicateStableKey IntegrityRule = "duplicate_stable_key"
	// RuleUnknownConfidence: an edge's confidence or provenance is not one
	// of the values facts declares, or claims exactness from a provenance
	// that cannot support it.
	RuleUnknownConfidence IntegrityRule = "unknown_confidence"
	// RuleInvalidRepositoryOwner: a node's repository_key does not match
	// the repository reachable by walking its containment chain, or names a
	// Repository that does not exist.
	RuleInvalidRepositoryOwner IntegrityRule = "invalid_repository_ownership"
)

// CanonicalIntegrityRules returns every rule, in evaluation order.
func CanonicalIntegrityRules() []IntegrityRule {
	return []IntegrityRule{
		RuleExactEdgeWithoutSource,
		RuleExactEdgeWithoutTarget,
		RuleMissingEvidenceFile,
		RuleDuplicateStableKey,
		RuleUnknownConfidence,
		RuleInvalidRepositoryOwner,
	}
}

// IntegrityViolation identifies one offending row.
type IntegrityViolation struct {
	Rule   IntegrityRule
	Table  string
	Key    string
	Detail string
}

// IntegrityFinding is the outcome of one rule over the whole graph.
type IntegrityFinding struct {
	Rule       IntegrityRule
	Violations int64
	Samples    []IntegrityViolation
	Passed     bool
}

// CanonicalIntegrityReport is the verdict over every rule.
type CanonicalIntegrityReport struct {
	Findings []IntegrityFinding
	Passed   bool
}

// Violations returns the total across every rule.
func (report CanonicalIntegrityReport) Violations() int64 {
	var total int64
	for _, finding := range report.Findings {
		total += finding.Violations
	}
	return total
}

// Finding returns one rule's outcome.
func (report CanonicalIntegrityReport) Finding(rule IntegrityRule) (IntegrityFinding, bool) {
	for _, finding := range report.Findings {
		if finding.Rule == rule {
			return finding, true
		}
	}
	return IntegrityFinding{}, false
}

// canonicalConfidenceValues lists every facts.Confidence the canonical
// schema can legally store. facts exports no enumerator of its own constants
// (only the Exact method), so the catalog is written out once, here.
// TestConfidenceAndProvenanceCatalogsMatchFactsPackage in
// canonical_integrity_native_test.go parses facts.go directly and fails the
// moment facts gains or loses a Confidence constant this list was not
// updated for; losing a constant already fails the build, since every entry
// below is a reference to the real facts identifier.
var canonicalConfidenceValues = []facts.Confidence{
	facts.ExactTypechecked,
	facts.ExactDeclarationMapped,
	facts.ExactPackageMapped,
	facts.StructuralCertain,
	facts.Candidate,
	facts.Unresolved,
}

// canonicalProvenanceValues lists every facts.Provenance the canonical
// schema can legally store; see canonicalConfidenceValues for why it is
// written out by hand instead of derived.
var canonicalProvenanceValues = []facts.Provenance{
	facts.TypeScriptChecker,
	facts.TypeScriptModuleResolution,
	facts.TypeScriptDeclarationMap,
	facts.TypeScriptProjectReference,
	facts.GoTypesDefinition,
	facts.GoTypesUse,
	facts.GoTypesSelection,
	facts.GoASTCall,
	facts.GoASTCallback,
	facts.GoObjectPath,
	facts.TreeSitterSyntax,
	facts.PackageManifest,
}

// exactConfidenceValues is the subset of canonicalConfidenceValues that
// Confidence.Exact reports true for, derived rather than repeated by hand.
func exactConfidenceValues() []facts.Confidence {
	var exact []facts.Confidence
	for _, value := range canonicalConfidenceValues {
		if value.Exact() {
			exact = append(exact, value)
		}
	}
	return exact
}

// nonExactProvenanceValues is the subset of canonicalProvenanceValues that
// Provenance.Exact reports false for: the provenances that can never back an
// exact confidence edge.
func nonExactProvenanceValues() []facts.Provenance {
	var nonExact []facts.Provenance
	for _, value := range canonicalProvenanceValues {
		if !value.Exact() {
			nonExact = append(nonExact, value)
		}
	}
	return nonExact
}

// isValidConfidence reports whether value is one of canonicalConfidenceValues.
func isValidConfidence(value string) bool {
	for _, candidate := range canonicalConfidenceValues {
		if string(candidate) == value {
			return true
		}
	}
	return false
}

// isValidProvenance reports whether value is one of canonicalProvenanceValues.
func isValidProvenance(value string) bool {
	for _, candidate := range canonicalProvenanceValues {
		if string(candidate) == value {
			return true
		}
	}
	return false
}

// hasRelationshipProperty reports whether table declares a property named name.
func hasRelationshipProperty(table SchemaRelationshipTable, name string) bool {
	for _, property := range table.Properties {
		if property.Name == name {
			return true
		}
	}
	return false
}

// semanticRelationshipTables returns the relationship tables that carry
// evidence_key: the semantic (Package-Package and Symbol-Symbol) edges of
// the plan, as opposed to the three structural containment edges and the
// two property-less derived relations (OBSERVED_IN, REPORTS_UNRESOLVED).
// Derived from the schema metadata so this can never drift from
// canonical_schema.go's declared 15 semantic tables.
func semanticRelationshipTables() []SchemaRelationshipTable {
	var tables []SchemaRelationshipTable
	for _, table := range CanonicalRelationshipTables() {
		if hasRelationshipProperty(table, "evidence_key") {
			tables = append(tables, table)
		}
	}
	return tables
}

// semanticRelationshipTableNames is semanticRelationshipTables projected to
// table names, in schema declaration order.
func semanticRelationshipTableNames() []string {
	tables := semanticRelationshipTables()
	names := make([]string, len(tables))
	for index, table := range tables {
		names[index] = table.Name
	}
	return names
}

// confidenceBearingRelationshipTableNames returns every relationship table
// that carries a confidence property: the 15 semantic edges plus the three
// structural containment edges (CONTAINS_PACKAGE, CONTAINS_FILE, DEFINES).
// OBSERVED_IN and REPORTS_UNRESOLVED carry no properties and are excluded.
func confidenceBearingRelationshipTableNames() []string {
	var names []string
	for _, table := range CanonicalRelationshipTables() {
		if hasRelationshipProperty(table, "confidence") {
			names = append(names, table.Name)
		}
	}
	return names
}

// relationshipGroup is a set of relationship tables that share the node type
// bound at the endpoint semanticRelationshipGroups was asked to group by.
type relationshipGroup struct {
	NodeType string
	Tables   []string
}

// semanticRelationshipGroups groups the semantic relationship tables by the
// node type endpoint extracts (From for a source side check, To for a
// target side check). For the current schema this always yields two groups,
// Symbol and Package, but the grouping is computed rather than hard coded so
// a future semantic edge with different endpoints is picked up automatically
// instead of silently mis-checked. Group and per-group table order are
// sorted, so callers get a deterministic iteration order regardless of
// CanonicalRelationshipTables' own declaration order.
func semanticRelationshipGroups(endpoint func(SchemaRelationshipTable) string) []relationshipGroup {
	index := make(map[string]int)
	var groups []relationshipGroup
	for _, table := range semanticRelationshipTables() {
		nodeType := endpoint(table)
		position, exists := index[nodeType]
		if !exists {
			position = len(groups)
			index[nodeType] = position
			groups = append(groups, relationshipGroup{NodeType: nodeType})
		}
		groups[position].Tables = append(groups[position].Tables, table.Name)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].NodeType < groups[j].NodeType })
	for index := range groups {
		sort.Strings(groups[index].Tables)
	}
	return groups
}

// stableKeyNodeTables returns the node tables keyed by stable_key: every
// entity table except GraphMetadata, whose key column is named key and
// holds fixed identity rows (schema version, resolver identity), not
// durable entity keys. GraphMetadata never collides with an entity and is
// not part of the stable_key keyspace RuleDuplicateStableKey protects.
func stableKeyNodeTables() []string {
	var names []string
	for _, table := range CanonicalNodeTables() {
		if table.PrimaryKey.Name == "stable_key" {
			names = append(names, table.Name)
		}
	}
	return names
}

// containmentEdge names the single structural edge that must declare a node
// before any exact semantic edge may anchor on it.
type containmentEdge struct {
	// Relationship is the declaring edge's table name.
	Relationship string
	// From is the node type the declaring edge must arrive from.
	From string
}

// declaringEdges maps a node type that can anchor a semantic edge to the
// structural containment edge that must supply it: a Symbol needs an
// incoming DEFINES from a File, a Package needs an incoming CONTAINS_PACKAGE
// from a Repository. This is decided domain knowledge (the plan states it in
// prose), not something the schema metadata alone determines, since nothing
// marks DEFINES and CONTAINS_PACKAGE as "the" declaring edge for their
// target type versus any other OneToMany relation that might be added later.
var declaringEdges = map[string]containmentEdge{
	"Symbol":  {Relationship: "DEFINES", From: "File"},
	"Package": {Relationship: "CONTAINS_PACKAGE", From: "Repository"},
}

// containmentHop is one edge of a containment chain walked from Repository
// down to the node type RuleInvalidRepositoryOwner is checking. Reversed
// marks a hop stored in the opposite direction of the walk, as OBSERVED_IN
// is: it points from Evidence to File, not from File to Evidence.
type containmentHop struct {
	Relationship string
	Reversed     bool
}

// ownershipChains names, for each node type RuleInvalidRepositoryOwner
// governs, the containment path from Repository down to it: Package by
// CONTAINS_PACKAGE, File by CONTAINS_FILE after Package's own chain, Symbol
// by DEFINES after File's own chain, Evidence by OBSERVED_IN after File's
// own chain. Like declaringEdges, this is decided domain knowledge, not
// something derivable from the schema metadata alone.
var ownershipChains = map[string][]containmentHop{
	"Package":  {{Relationship: "CONTAINS_PACKAGE"}},
	"File":     {{Relationship: "CONTAINS_PACKAGE"}, {Relationship: "CONTAINS_FILE"}},
	"Symbol":   {{Relationship: "CONTAINS_PACKAGE"}, {Relationship: "CONTAINS_FILE"}, {Relationship: "DEFINES"}},
	"Evidence": {{Relationship: "CONTAINS_PACKAGE"}, {Relationship: "CONTAINS_FILE"}, {Relationship: "OBSERVED_IN", Reversed: true}},
}

// ownershipCheckedNodeTables lists the node types RuleInvalidRepositoryOwner
// governs, in a fixed order so findings stay deterministic regardless of map
// iteration. Repository is excluded: it owns itself. UnresolvedReference is
// excluded: the plan's decided semantics name only these four.
func ownershipCheckedNodeTables() []string {
	return []string{"Package", "File", "Symbol", "Evidence"}
}

// mergeViolationSamples pools per-group sample rows into the single
// deterministic top MaxIntegritySamples for a whole rule. Each group passed
// in is expected to already be that group's own top MaxIntegritySamples,
// sorted by table then key: because every group already contributes its own
// smallest MaxIntegritySamples entries, the true global smallest
// MaxIntegritySamples is always inside the pool. An element ranked below its
// own group's cutoff cannot rank inside the global cutoff either, since
// fewer than MaxIntegritySamples elements of its own group would then be
// smaller than it, let alone globally.
func mergeViolationSamples(groups ...[]IntegrityViolation) []IntegrityViolation {
	var pooled []IntegrityViolation
	for _, group := range groups {
		pooled = append(pooled, group...)
	}
	sort.Slice(pooled, func(i, j int) bool {
		if pooled[i].Table != pooled[j].Table {
			return pooled[i].Table < pooled[j].Table
		}
		return pooled[i].Key < pooled[j].Key
	})
	if len(pooled) > MaxIntegritySamples {
		pooled = pooled[:MaxIntegritySamples]
	}
	return pooled
}
