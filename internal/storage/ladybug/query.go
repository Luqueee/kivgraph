package ladybug

import "context"

const (
	// MaxReferenceResults matches the maximum page size in the public query contract.
	MaxReferenceResults = 500
	// MaxTraversalDepth is the maximum supported semantic traversal depth.
	MaxTraversalDepth = 5
	// MaxTraversalResults bounds materialized traversal nodes.
	MaxTraversalResults = 25_000
)

// Reader owns one LadybugDB connection and its prepared read statements.
// A Reader is safe for concurrent use; calls on one Reader execute serially.
type Reader interface {
	Close() error
	GetSymbol(context.Context, string) (Symbol, bool, error)
	IncomingReferences(context.Context, string, int) ([]Reference, error)
	OutgoingReferences(context.Context, string, int) ([]Reference, error)
	TraverseOutgoing(context.Context, string, int, int) ([]TraversalNode, error)
	ShortestPath(context.Context, string, string, int) (Path, bool, error)
	IncomingReferencesByRepository(context.Context, string) ([]RepositoryReferenceCount, error)
	ScanAll(context.Context) (ScanRows, error)
}

// ScanRows contains every row from the synthetic graph schema in deterministic
// key order. Relationships retain their exact kind and evidence fields.
type ScanRows struct {
	Repositories []RepositoryRecord
	Files        []FileRecord
	Symbols      []Symbol
	Edges        []ScanEdge
}

type ScanEdge struct {
	SourceKey     string
	TargetKey     string
	Kind          string
	EvidenceKind  string
	SourceFileKey string
	TargetFileKey string
}

type RepositoryRecord struct {
	StableKey string
	Name      string
	Path      string
	Language  string
}

type FileRecord struct {
	StableKey     string
	RepositoryKey string
	Path          string
	ContentHash   string
	Language      string
}

// Symbol is the persisted synthetic symbol shape returned by stable-key lookup.
type Symbol struct {
	StableKey     string
	RepositoryKey string
	FileKey       string
	Name          string
	QualifiedName string
	Kind          string
	Signature     string
	StartLine     int64
	EndLine       int64
}

// Reference is one exact persisted REFERENCES or CALLS_DIRECT relationship.
type Reference struct {
	SourceKey     string
	TargetKey     string
	Kind          string
	EvidenceKind  string
	SourceFileKey string
	TargetFileKey string
}

// TraversalNode is one distinct semantic successor at its shortest depth.
type TraversalNode struct {
	StableKey string
	Depth     int
}

// Path contains both endpoints and every intermediate symbol in order.
type Path struct {
	StableKeys []string
	Length     int
}

// RepositoryReferenceCount groups incoming relationships by source repository.
type RepositoryReferenceCount struct {
	RepositoryKey string
	Count         int64
}

func validateStableKey(stableKey string) error {
	if stableKey == "" {
		return ErrInvalidStableKey
	}
	return nil
}

func validateReferenceLimit(limit int) error {
	if limit < 1 || limit > MaxReferenceResults {
		return ErrInvalidLimit
	}
	return nil
}

func validateTraversal(depth, limit int) error {
	if depth < 1 || depth > MaxTraversalDepth {
		return ErrInvalidDepth
	}
	if limit < 1 || limit > MaxTraversalResults {
		return ErrInvalidLimit
	}
	return nil
}

func validatePathQuery(sourceKey, targetKey string, maxDepth int) error {
	if err := validateStableKey(sourceKey); err != nil {
		return err
	}
	if err := validateStableKey(targetKey); err != nil {
		return err
	}
	if maxDepth < 1 || maxDepth > MaxTraversalDepth {
		return ErrInvalidDepth
	}
	return nil
}
