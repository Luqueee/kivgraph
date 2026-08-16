//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// VerifyCanonicalIntegrity evaluates every canonical invariant against the
// graph at path. A violated invariant is a report, not an error: only an
// engine failure returns one.
func VerifyCanonicalIntegrity(ctx context.Context, path string) (CanonicalIntegrityReport, error) {
	if err := validatePath(path); err != nil {
		return CanonicalIntegrityReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalIntegrityReport{}, &Error{Op: "verify canonical integrity", Err: err}
	}

	db, err := openCanonicalDatabase(ctx, path, true)
	if err != nil {
		return CanonicalIntegrityReport{}, err
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		return CanonicalIntegrityReport{}, err
	}
	defer connection.Close()
	native := connection.native

	rules := CanonicalIntegrityRules()
	report := CanonicalIntegrityReport{Findings: make([]IntegrityFinding, 0, len(rules)), Passed: true}
	for _, rule := range rules {
		var finding IntegrityFinding
		var evalErr error
		switch rule {
		case RuleExactEdgeWithoutSource:
			finding, evalErr = evaluateExactEdgeWithoutSource(ctx, native)
		case RuleExactEdgeWithoutTarget:
			finding, evalErr = evaluateExactEdgeWithoutTarget(ctx, native)
		case RuleMissingEvidenceFile:
			finding, evalErr = evaluateMissingEvidenceFile(ctx, native)
		case RuleDuplicateStableKey:
			finding, evalErr = evaluateDuplicateStableKey(ctx, native)
		case RuleUnknownConfidence:
			finding, evalErr = evaluateUnknownConfidence(ctx, native)
		case RuleInvalidRepositoryOwner:
			finding, evalErr = evaluateInvalidRepositoryOwnership(ctx, native)
		default:
			evalErr = fmt.Errorf("no evaluator registered for rule %q", rule)
		}
		if evalErr != nil {
			return CanonicalIntegrityReport{}, &Error{Op: "verify canonical integrity", Err: fmt.Errorf("%w: rule %s: %v", ErrCanonicalIntegrity, rule, evalErr)}
		}
		finding.Rule = rule
		finding.Passed = finding.Violations == 0
		for index := range finding.Samples {
			finding.Samples[index].Rule = rule
		}
		report.Findings = append(report.Findings, finding)
		if !finding.Passed {
			report.Passed = false
		}
	}
	if err := ctx.Err(); err != nil {
		return CanonicalIntegrityReport{}, &Error{Op: "verify canonical integrity", Err: err}
	}
	return report, nil
}

// evaluateExactEdgeWithoutEndpoint implements RuleExactEdgeWithoutSource and
// RuleExactEdgeWithoutTarget: an exact confidence semantic edge whose
// checked endpoint carries no declaring structural edge of its own — a
// Symbol with no incoming DEFINES, or a Package with no incoming
// CONTAINS_PACKAGE. The two rules differ only in which side of the pattern
// is bound to the checked node, so one function drives both.
//
// NOT EXISTS { ... } and IN list literals were verified directly against
// the pinned v0.13.1 engine before this shape was chosen; both are accepted.
func evaluateExactEdgeWithoutEndpoint(ctx context.Context, native *lbug.Connection, checkSource bool) (IntegrityFinding, error) {
	endpoint := func(table SchemaRelationshipTable) string { return table.To }
	side := "target"
	if checkSource {
		endpoint = func(table SchemaRelationshipTable) string { return table.From }
		side = "source"
	}

	var total int64
	var sampleGroups [][]IntegrityViolation
	for _, group := range semanticRelationshipGroups(endpoint) {
		declaring, known := declaringEdges[group.NodeType]
		if !known {
			return IntegrityFinding{}, fmt.Errorf("canonical integrity: no declaring edge known for node type %q", group.NodeType)
		}

		// The un-checked side of the pattern is left unlabelled: the
		// relationship type itself already fixes both endpoint tables (a
		// REFERENCES edge can only ever connect two Symbol rows), so
		// repeating that label would be redundant, not protective, and
		// would depend on every semantic table being endpoint-symmetric.
		var pattern string
		if checkSource {
			pattern = fmt.Sprintf("(violation_node:%s)-[violation_edge:%s]->()", group.NodeType, cypherTypeUnion(group.Tables))
		} else {
			pattern = fmt.Sprintf("()-[violation_edge:%s]->(violation_node:%s)", cypherTypeUnion(group.Tables), group.NodeType)
		}
		predicate := fmt.Sprintf(
			"violation_edge.confidence IN %s AND NOT EXISTS { MATCH (:%s)-[:%s]->(violation_node) }",
			exactConfidenceLiteral(), declaring.From, declaring.Relationship,
		)

		count, err := queryCount(ctx, native, fmt.Sprintf("MATCH %s WHERE %s RETURN count(*)", pattern, predicate))
		if err != nil {
			return IntegrityFinding{}, fmt.Errorf("count %s group %s: %w", side, group.NodeType, err)
		}
		total += count

		sampleQuery := fmt.Sprintf(`MATCH %s
WHERE %s
RETURN label(violation_edge) AS violation_table, violation_node.stable_key AS violation_key, violation_edge.confidence AS confidence
ORDER BY violation_table, violation_key
LIMIT %d`, pattern, predicate, MaxIntegritySamples)
		samples, err := runIntegritySampleQuery(ctx, native, sampleQuery, func(tuple *lbug.FlatTuple) (IntegrityViolation, error) {
			table, err1 := tupleString(tuple, 0)
			key, err2 := tupleString(tuple, 1)
			confidence, err3 := tupleString(tuple, 2)
			if err := errors.Join(err1, err2, err3); err != nil {
				return IntegrityViolation{}, err
			}
			return IntegrityViolation{
				Table:  table,
				Key:    key,
				Detail: fmt.Sprintf("%s confidence %s has no declaring %s from %s", side, confidence, declaring.Relationship, declaring.From),
			}, nil
		})
		if err != nil {
			return IntegrityFinding{}, fmt.Errorf("sample %s group %s: %w", side, group.NodeType, err)
		}
		sampleGroups = append(sampleGroups, samples)
	}

	return IntegrityFinding{Violations: total, Samples: mergeViolationSamples(sampleGroups...)}, nil
}

func evaluateExactEdgeWithoutSource(ctx context.Context, native *lbug.Connection) (IntegrityFinding, error) {
	return evaluateExactEdgeWithoutEndpoint(ctx, native, true)
}

func evaluateExactEdgeWithoutTarget(ctx context.Context, native *lbug.Connection) (IntegrityFinding, error) {
	return evaluateExactEdgeWithoutEndpoint(ctx, native, false)
}

// evaluateMissingEvidenceFile implements RuleMissingEvidenceFile: a semantic
// edge whose evidence_key is set but names an Evidence row that either does
// not exist or is not linked to a File by OBSERVED_IN. The two correlated
// NOT EXISTS subqueries reference violation_edge.evidence_key directly, a
// combination verified against the engine before use.
func evaluateMissingEvidenceFile(ctx context.Context, native *lbug.Connection) (IntegrityFinding, error) {
	pattern := fmt.Sprintf("(edge_source)-[violation_edge:%s]->(edge_target)", cypherTypeUnion(semanticRelationshipTableNames()))
	predicate := `violation_edge.evidence_key <> ''
  AND (
    NOT EXISTS { MATCH (evidence:Evidence) WHERE evidence.stable_key = violation_edge.evidence_key }
    OR NOT EXISTS { MATCH (evidence:Evidence)-[:OBSERVED_IN]->(:File) WHERE evidence.stable_key = violation_edge.evidence_key }
  )`

	count, err := queryCount(ctx, native, fmt.Sprintf("MATCH %s WHERE %s RETURN count(*)", pattern, predicate))
	if err != nil {
		return IntegrityFinding{}, fmt.Errorf("count: %w", err)
	}

	sampleQuery := fmt.Sprintf(`MATCH %s
WHERE %s
RETURN label(violation_edge) AS violation_table, edge_source.stable_key AS source_key, edge_target.stable_key AS target_key, violation_edge.evidence_key AS evidence_key
ORDER BY violation_table, source_key, target_key
LIMIT %d`, pattern, predicate, MaxIntegritySamples)
	samples, err := runIntegritySampleQuery(ctx, native, sampleQuery, func(tuple *lbug.FlatTuple) (IntegrityViolation, error) {
		table, err1 := tupleString(tuple, 0)
		sourceKey, err2 := tupleString(tuple, 1)
		targetKey, err3 := tupleString(tuple, 2)
		evidenceKey, err4 := tupleString(tuple, 3)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return IntegrityViolation{}, err
		}
		return IntegrityViolation{
			Table:  table,
			Key:    edgeViolationKey(sourceKey, targetKey),
			Detail: fmt.Sprintf("evidence_key %s has no Evidence observed in a File", evidenceKey),
		}, nil
	})
	if err != nil {
		return IntegrityFinding{}, fmt.Errorf("sample: %w", err)
	}
	return IntegrityFinding{Violations: count, Samples: samples}, nil
}

// evaluateDuplicateStableKey implements RuleDuplicateStableKey: a stable_key
// used by rows of two different node tables. There is no single-query form
// spanning all six node tables at once (a stable_key collision needs a join
// between two specific tables, and Cypher joins tables in a fixed pattern,
// not a variable one), so this walks every unordered pair once. In the
// healthy case — the keys the facts package builds are prefixed per table
// and can never collide — every pair query returns zero rows and nothing
// beyond the count(*) style cost of a hash join is paid; only an actually
// corrupt graph pays for transferring the offending rows.
func evaluateDuplicateStableKey(ctx context.Context, native *lbug.Connection) (IntegrityFinding, error) {
	tables := stableKeyNodeTables()
	// Keyed by (table, key) so a stable_key spanning three or more tables —
	// unreachable through facts.Set.Validate, only through direct writes —
	// is still reported once per offending row, not once per pair that
	// independently noticed the collision.
	violations := make(map[[2]string]IntegrityViolation)
	for i := range tables {
		for j := i + 1; j < len(tables); j++ {
			left, right := tables[i], tables[j]
			query := fmt.Sprintf(`MATCH (left:%s), (right:%s)
WHERE left.stable_key = right.stable_key
RETURN left.stable_key AS key
ORDER BY key`, left, right)
			hits, err := runIntegritySampleQuery(ctx, native, query, func(tuple *lbug.FlatTuple) (IntegrityViolation, error) {
				key, err := tupleString(tuple, 0)
				if err != nil {
					return IntegrityViolation{}, err
				}
				return IntegrityViolation{Key: key}, nil
			})
			if err != nil {
				return IntegrityFinding{}, fmt.Errorf("pair %s/%s: %w", left, right, err)
			}
			for _, hit := range hits {
				violations[[2]string{left, hit.Key}] = IntegrityViolation{
					Table: left, Key: hit.Key,
					Detail: fmt.Sprintf("stable_key also used by %s", right),
				}
				violations[[2]string{right, hit.Key}] = IntegrityViolation{
					Table: right, Key: hit.Key,
					Detail: fmt.Sprintf("stable_key also used by %s", left),
				}
			}
		}
	}

	all := make([]IntegrityViolation, 0, len(violations))
	for _, violation := range violations {
		all = append(all, violation)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Table != all[j].Table {
			return all[i].Table < all[j].Table
		}
		return all[i].Key < all[j].Key
	})
	samples := all
	if len(samples) > MaxIntegritySamples {
		samples = samples[:MaxIntegritySamples]
	}
	return IntegrityFinding{Violations: int64(len(all)), Samples: samples}, nil
}

// evaluateUnknownConfidence implements RuleUnknownConfidence: an edge whose
// confidence is not one of the six facts.Confidence values, whose
// provenance is not one of the twelve facts.Provenance values, or which
// claims exactness (an exact confidence) from a provenance that cannot back
// it — the same rule facts.Set.Validate applies to facts before they are
// written, reverified here against what is actually stored. Spans every
// confidence-bearing table, structural and semantic, in one query: the
// mixed-type, untyped-endpoint pattern below mirrors the scanEdgesQuery
// shape already used elsewhere in this package.
func evaluateUnknownConfidence(ctx context.Context, native *lbug.Connection) (IntegrityFinding, error) {
	pattern := fmt.Sprintf("(edge_source)-[violation_edge:%s]->(edge_target)", cypherTypeUnion(confidenceBearingRelationshipTableNames()))
	predicate := fmt.Sprintf(`NOT violation_edge.confidence IN %s
   OR NOT violation_edge.provenance IN %s
   OR (violation_edge.confidence IN %s AND violation_edge.provenance IN %s)`,
		cypherStringSet(confidenceStrings(canonicalConfidenceValues)),
		cypherStringSet(provenanceStrings(canonicalProvenanceValues)),
		exactConfidenceLiteral(),
		cypherStringSet(provenanceStrings(nonExactProvenanceValues())),
	)

	count, err := queryCount(ctx, native, fmt.Sprintf("MATCH %s WHERE %s RETURN count(*)", pattern, predicate))
	if err != nil {
		return IntegrityFinding{}, fmt.Errorf("count: %w", err)
	}

	sampleQuery := fmt.Sprintf(`MATCH %s
WHERE %s
RETURN label(violation_edge) AS violation_table, edge_source.stable_key AS source_key, edge_target.stable_key AS target_key, violation_edge.confidence AS confidence, violation_edge.provenance AS provenance
ORDER BY violation_table, source_key, target_key
LIMIT %d`, pattern, predicate, MaxIntegritySamples)
	samples, err := runIntegritySampleQuery(ctx, native, sampleQuery, func(tuple *lbug.FlatTuple) (IntegrityViolation, error) {
		table, err1 := tupleString(tuple, 0)
		sourceKey, err2 := tupleString(tuple, 1)
		targetKey, err3 := tupleString(tuple, 2)
		confidence, err4 := tupleString(tuple, 3)
		provenance, err5 := tupleString(tuple, 4)
		if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
			return IntegrityViolation{}, err
		}
		var detail string
		switch {
		case !isValidConfidence(confidence):
			detail = fmt.Sprintf("confidence %q is not a facts.Confidence value", confidence)
		case !isValidProvenance(provenance):
			detail = fmt.Sprintf("provenance %q is not a facts.Provenance value", provenance)
		default:
			detail = fmt.Sprintf("confidence %q claims exactness but provenance %q is not exact", confidence, provenance)
		}
		return IntegrityViolation{Table: table, Key: edgeViolationKey(sourceKey, targetKey), Detail: detail}, nil
	})
	if err != nil {
		return IntegrityFinding{}, fmt.Errorf("sample: %w", err)
	}
	return IntegrityFinding{Violations: count, Samples: samples}, nil
}

// evaluateInvalidRepositoryOwnership implements RuleInvalidRepositoryOwner:
// a node whose repository_key does not match the Repository reachable by
// walking its containment chain, including the case where no repository is
// reachable at all — the chain itself is broken, or repository_key names a
// Repository that does not exist. OPTIONAL MATCH ... WITH ... WHERE x IS
// NULL OR ... was verified directly against the engine before this shape
// was chosen: a repository that never matched degrades to null rather than
// filtering the row, which is exactly what lets the WHERE tell "reachable
// but wrong" and "unreachable" apart from "correct" in one pass.
func evaluateInvalidRepositoryOwnership(ctx context.Context, native *lbug.Connection) (IntegrityFinding, error) {
	var total int64
	var sampleGroups [][]IntegrityViolation
	for _, nodeType := range ownershipCheckedNodeTables() {
		chain, known := ownershipChains[nodeType]
		if !known {
			return IntegrityFinding{}, fmt.Errorf("canonical integrity: no ownership chain known for node type %q", nodeType)
		}
		chainPattern, err := renderOwnershipChainPattern(chain)
		if err != nil {
			return IntegrityFinding{}, err
		}
		predicate := "repository IS NULL OR repository.stable_key <> node.repository_key"

		countQuery := fmt.Sprintf(`MATCH (node:%s)
OPTIONAL MATCH %s
WITH node, repository
WHERE %s
RETURN count(*)`, nodeType, chainPattern, predicate)
		count, err := queryCount(ctx, native, countQuery)
		if err != nil {
			return IntegrityFinding{}, fmt.Errorf("count %s: %w", nodeType, err)
		}
		total += count

		sampleQuery := fmt.Sprintf(`MATCH (node:%s)
OPTIONAL MATCH %s
WITH node, repository
WHERE %s
RETURN node.stable_key AS violation_key, node.repository_key AS repository_key
ORDER BY violation_key
LIMIT %d`, nodeType, chainPattern, predicate, MaxIntegritySamples)
		samples, err := runIntegritySampleQuery(ctx, native, sampleQuery, func(tuple *lbug.FlatTuple) (IntegrityViolation, error) {
			key, err1 := tupleString(tuple, 0)
			repositoryKey, err2 := tupleString(tuple, 1)
			if err := errors.Join(err1, err2); err != nil {
				return IntegrityViolation{}, err
			}
			return IntegrityViolation{
				Table:  nodeType,
				Key:    key,
				Detail: fmt.Sprintf("repository_key %s is not confirmed by the containment chain from Repository", repositoryKey),
			}, nil
		})
		if err != nil {
			return IntegrityFinding{}, fmt.Errorf("sample %s: %w", nodeType, err)
		}
		sampleGroups = append(sampleGroups, samples)
	}
	return IntegrityFinding{Violations: total, Samples: mergeViolationSamples(sampleGroups...)}, nil
}

// runIntegritySampleQuery executes query — already carrying its own ORDER BY
// and LIMIT, or none at all when the caller needs the exact match set — and
// decodes each row with decode. Mirrors the deadline handling every other
// looped query in this package uses (queryCount, queryWithDeadline).
func runIntegritySampleQuery(ctx context.Context, native *lbug.Connection, query string, decode func(*lbug.FlatTuple) (IntegrityViolation, error)) ([]IntegrityViolation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := setQueryDeadline(native, ctx); err != nil {
		return nil, err
	}
	defer native.SetTimeout(0)
	result, err := native.Query(query)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return nil, err
	}
	defer result.Close()
	var violations []IntegrityViolation
	for result.HasNext() {
		tuple, err := nextTuple(result)
		if err != nil {
			return nil, err
		}
		violation, err := decode(tuple)
		tuple.Close()
		if err != nil {
			return nil, err
		}
		violations = append(violations, violation)
	}
	return violations, ctx.Err()
}

// renderOwnershipChainPattern renders the containment path from Repository
// down to the node type chain describes, ending at the already bound `node`
// variable so the caller's OPTIONAL MATCH can degrade the whole chain to
// null in one step when any hop is missing. Intermediate labels are read
// from CanonicalRelationshipTables, not repeated by hand, so the rendered
// pattern can never name an endpoint canonical_schema.go disagrees with.
func renderOwnershipChainPattern(chain []containmentHop) (string, error) {
	relationships := make(map[string]SchemaRelationshipTable, len(CanonicalRelationshipTables()))
	for _, table := range CanonicalRelationshipTables() {
		relationships[table.Name] = table
	}

	var builder strings.Builder
	builder.WriteString("(repository:Repository)")
	for index, hop := range chain {
		table, known := relationships[hop.Relationship]
		if !known {
			return "", fmt.Errorf("canonical integrity: unknown containment relationship %q", hop.Relationship)
		}
		last := index == len(chain)-1
		nextLabel := table.To
		if hop.Reversed {
			nextLabel = table.From
		}
		next := ":" + nextLabel
		if last {
			next = "node"
		}
		if hop.Reversed {
			fmt.Fprintf(&builder, "<-[:%s]-(%s)", table.Name, next)
		} else {
			fmt.Fprintf(&builder, "-[:%s]->(%s)", table.Name, next)
		}
	}
	return builder.String(), nil
}

// edgeViolationKey identifies an offending edge by its endpoints: the
// canonical schema gives relationship rows no key of their own.
func edgeViolationKey(sourceKey, targetKey string) string {
	return sourceKey + "->" + targetKey
}

// cypherStringSet renders values as a Cypher list literal of single quoted
// strings. Every value passed to it is a compile time constant from the
// facts package or a canonical table name, never external input, so this is
// a literal renderer, not an escaper — untrusted data has no path into
// these queries.
func cypherStringSet(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = "'" + value + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// cypherTypeUnion renders relationship type names as a Cypher multi-type
// pattern fragment (A|:B|:C), the form scanEdgesQuery and the other proven
// queries in this package already use for "any of these edge types".
func cypherTypeUnion(names []string) string {
	return strings.Join(names, "|:")
}

// exactConfidenceLiteral is the Cypher IN list of every facts.Confidence
// value Confidence.Exact reports true for.
func exactConfidenceLiteral() string {
	return cypherStringSet(confidenceStrings(exactConfidenceValues()))
}

func confidenceStrings(values []facts.Confidence) []string {
	rendered := make([]string, len(values))
	for index, value := range values {
		rendered[index] = string(value)
	}
	return rendered
}

func provenanceStrings(values []facts.Provenance) []string {
	rendered := make([]string, len(values))
	for index, value := range values {
		rendered[index] = string(value)
	}
	return rendered
}
