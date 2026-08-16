package ladybug

import (
	"errors"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// ErrInvalidCanonicalScan reports a definitive graph that cannot become a
// scan result. Today this is only an incompatible schema version: a rebuild
// stage must never try to derive a snapshot from a layout it does not
// understand, so ScanCanonical fails loudly before reading a single node or
// edge rather than misreading columns that have since moved.
var ErrInvalidCanonicalScan = errors.New("invalid canonical scan")

// CanonicalRepository is one Repository row of the definitive graph.
type CanonicalRepository struct {
	StableKey string
	Name      string
	RootPath  string
	Commit    string
	Branch    string
	Dirty     bool
	Languages string
}

// CanonicalPackage is one Package row of the definitive graph.
type CanonicalPackage struct {
	StableKey     string
	RepositoryKey string
	Language      string
	Name          string
	Version       string
	RootPath      string
	ManifestPath  string
	Container     string
}

// CanonicalFile is one File row of the definitive graph.
type CanonicalFile struct {
	StableKey     string
	RepositoryKey string
	PackageKey    string
	Path          string
	Language      string
	ContentHash   string
	Generated     bool
}

// CanonicalSymbol is one Symbol row of the definitive graph.
type CanonicalSymbol struct {
	StableKey         string
	CanonicalIdentity string
	RepositoryKey     string
	PackageKey        string
	FileKey           string
	Language          string
	Name              string
	QualifiedName     string
	Kind              string
	Exported          bool
	Signature         string
	StartLine         int64
	StartColumn       int64
	StartOffset       int64
	EndLine           int64
	EndOffset         int64
}

// CanonicalEvidence is one Evidence row of the definitive graph.
type CanonicalEvidence struct {
	StableKey     string
	RepositoryKey string
	FileKey       string
	StartLine     int64
	StartColumn   int64
	StartOffset   int64
	EndOffset     int64
	Text          string
}

// CanonicalUnresolvedReference is one unresolved import/reference finding.
// It is kept outside CanonicalEdge because it has no symbol target.
type CanonicalUnresolvedReference struct {
	StableKey        string
	RepositoryKey    string
	FileKey          string
	SourceSymbolKey  string
	Language         string
	RequestedPackage string
	RequestedSymbol  string
	Reason           string
	Detail           string
	StartLine        int64
	StartColumn      int64
	StartOffset      int64
}

// CanonicalEdge is one relationship row, with the table it came from.
//
// Confidence and Provenance are always populated: every edge table of the
// vocabulary carries both. EvidenceKey, SourceSnapshot and ResolverVersion
// hold the Go zero value for a structural table (CONTAINS_PACKAGE,
// CONTAINS_FILE, DEFINES), which has no such columns at all -- that is a
// schema fact declared by containmentProperties in canonical_schema.go, not
// a lossy read.
type CanonicalEdge struct {
	Table           string
	SourceKey       string
	TargetKey       string
	Confidence      string
	Provenance      string
	EvidenceKey     string
	SourceSnapshot  int64
	ResolverVersion string
}

// CanonicalGraph is the whole definitive graph, read out of the database.
type CanonicalGraph struct {
	SchemaVersion int
	Metadata      map[string]string
	Repositories  []CanonicalRepository
	Packages      []CanonicalPackage
	Files         []CanonicalFile
	Symbols       []CanonicalSymbol
	Evidence      []CanonicalEvidence
	Edges         []CanonicalEdge
	Unresolved    []CanonicalUnresolvedReference
}

// canonicalEdgeVocabularyTables returns the relationship tables that carry a
// facts.EdgeKind: the eighteen structural and semantic edges a scan must
// report as graph edges. CanonicalRelationshipTables also declares
// OBSERVED_IN and REPORTS_UNRESOLVED, which are relationship tables of the
// schema but not part of the facts.EdgeKind vocabulary -- Evidence and
// UnresolvedReference already carry their owning key directly (FileKey,
// RepositoryKey; see canonical_load.go's derivation of those two tables from
// Evidence/UnresolvedReference, never from a facts.Edge), so they are
// structural bookkeeping, never a scanned edge.
//
// facts.EdgeKind values are defined to equal their table name exactly (see
// internal/facts.EdgeKind and canonical_load.go's `table := string(edge.Kind)`),
// so membership is a single Valid() check against the list
// CanonicalRelationshipTables already declares, never a second hand written
// list that could drift from it.
func canonicalEdgeVocabularyTables() []SchemaRelationshipTable {
	tables := CanonicalRelationshipTables()
	vocabulary := make([]SchemaRelationshipTable, 0, len(tables))
	for _, table := range tables {
		if facts.EdgeKind(table.Name).Valid() {
			vocabulary = append(vocabulary, table)
		}
	}
	return vocabulary
}

// canonicalEdgeLess orders edges by (Table, SourceKey, TargetKey): the order
// two scans of the same graph must agree on, byte for byte, so a rebuild's
// snapshot digest depends only on what is stored, never on the order the
// database happened to hand rows back in.
func canonicalEdgeLess(left, right CanonicalEdge) bool {
	if left.Table != right.Table {
		return left.Table < right.Table
	}
	if left.SourceKey != right.SourceKey {
		return left.SourceKey < right.SourceKey
	}
	return left.TargetKey < right.TargetKey
}
