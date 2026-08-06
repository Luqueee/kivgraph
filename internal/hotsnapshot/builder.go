package hotsnapshot

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrInvalidSnapshotRows = errors.New("invalid snapshot rows")

// LadybugSnapshotRows is the ordered, canonical row set read from LadybugDB.
// Keys are durable database keys; the builder alone assigns dense snapshot IDs.
type LadybugSnapshotRows struct {
	Repositories []RepositoryRow
	Packages     []PackageRow
	Files        []FileRow
	Symbols      []SymbolRow
	Edges        []EdgeRow
}

type RepositoryRow struct {
	Key    string
	Name   string
	Commit string
}

type PackageRow struct {
	Key           string
	RepositoryKey string
	Name          string
	ModulePath    string
}

type FileRow struct {
	Key           string
	RepositoryKey string
	PackageKey    string
	Path          string
}

type SymbolRow struct {
	StableKey         StableKey
	CanonicalIdentity string
	FileKey           string
	Name              string
	QualifiedName     string
	Kind              string
	Signature         string
	StartLine         uint32
	EndLine           uint32
}

// EdgeRow is one occurrence of a semantic relation. The canonical model is
// MANY_MANY on purpose: the same symbol may reach the same target from several
// places, and each occurrence carries its own evidence. EvidenceKey is what
// keeps two such occurrences distinguishable; without it they collapse into
// byte-identical rows and the duplicate check below cannot tell a real
// duplicate from a second, legitimate use.
type EdgeRow struct {
	SourceKey             StableKey
	TargetKey             StableKey
	Kind                  uint8
	Confidence            uint8
	Provenance            uint8
	Flags                 uint8
	EvidenceKey           string
	EvidenceKind          string
	EvidenceSourceFileKey string
	EvidenceTargetFileKey string
}

// BuildGraphSnapshot converts canonical LadybugDB rows into one validated,
// immutable snapshot. Every input collection is copied and sorted by its
// durable key before IDs, strings, CSR, and exact indexes are assigned.
func BuildGraphSnapshot(rows LadybugSnapshotRows, snapshotID uint64, createdAt time.Time, version uint32) (*GraphSnapshot, error) {
	repositories := append([]RepositoryRow(nil), rows.Repositories...)
	packages := append([]PackageRow(nil), rows.Packages...)
	files := append([]FileRow(nil), rows.Files...)
	symbols := append([]SymbolRow(nil), rows.Symbols...)
	edges := append([]EdgeRow(nil), rows.Edges...)
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Key < repositories[j].Key })
	sort.Slice(packages, func(i, j int) bool { return packages[i].Key < packages[j].Key })
	sort.Slice(files, func(i, j int) bool { return files[i].Key < files[j].Key })
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].StableKey < symbols[j].StableKey })
	sort.Slice(edges, func(i, j int) bool { return edgeLess(edges[i], edges[j]) })

	interner := NewStringInterner()
	allocator := new(IDAllocator)
	repositoryIDs := make(map[string]RepositoryID, len(repositories))
	packageIDs := make(map[string]PackageID, len(packages))
	fileIDs := make(map[string]FileID, len(files))
	symbolIDs := make(map[StableKey]SymbolID, len(symbols))

	repositoryRecords := make([]RepositoryRecord, len(repositories))
	// Validation covers identity and referential integrity only. Commit,
	// module path and signature are descriptive: a checkout without git
	// metadata has no commit, an npm package has no Go module path, and a
	// constant or a field has no signature. Requiring them would reject real
	// graphs to satisfy a fixture.
	for index, row := range repositories {
		if row.Key == "" || row.Name == "" || index > 0 && repositories[index-1].Key == row.Key {
			return nil, fmt.Errorf("%w: repository %d %q", ErrInvalidSnapshotRows, index, row.Key)
		}
		id, err := allocator.Repository()
		if err != nil {
			return nil, err
		}
		name, err := interner.Intern(row.Name)
		if err != nil {
			return nil, err
		}
		commit, err := interner.Intern(row.Commit)
		if err != nil {
			return nil, err
		}
		repositoryIDs[row.Key] = id
		repositoryRecords[index] = RepositoryRecord{Name: name, Commit: commit}
	}

	packageRecords := make([]PackageRecord, len(packages))
	for index, row := range packages {
		repositoryID, exists := repositoryIDs[row.RepositoryKey]
		if row.Key == "" || row.Name == "" || !exists || index > 0 && packages[index-1].Key == row.Key {
			return nil, fmt.Errorf("%w: package %d %q of repository %q", ErrInvalidSnapshotRows, index, row.Key, row.RepositoryKey)
		}
		id, err := allocator.Package()
		if err != nil {
			return nil, err
		}
		name, err := interner.Intern(row.Name)
		if err != nil {
			return nil, err
		}
		modulePath, err := interner.Intern(row.ModulePath)
		if err != nil {
			return nil, err
		}
		packageIDs[row.Key] = id
		packageRecords[index] = PackageRecord{Repository: repositoryID, Name: name, ModulePath: modulePath}
	}

	fileRecords := make([]FileRecord, len(files))
	fileIndex := make(map[RepoPathKey]FileID, len(files))
	for index, row := range files {
		repositoryID, repositoryExists := repositoryIDs[row.RepositoryKey]
		packageID, packageExists := packageIDs[row.PackageKey]
		if row.Key == "" || row.Path == "" || !repositoryExists || !packageExists || index > 0 && files[index-1].Key == row.Key {
			return nil, fmt.Errorf("%w: file %d %q of repository %q package %q", ErrInvalidSnapshotRows, index, row.Key, row.RepositoryKey, row.PackageKey)
		}
		if packageRecords[packageID].Repository != repositoryID {
			return nil, fmt.Errorf("%w: file %q claims repository %q but its package belongs to another", ErrInvalidSnapshotRows, row.Key, row.RepositoryKey)
		}
		id, err := allocator.File()
		if err != nil {
			return nil, err
		}
		path, err := interner.Intern(row.Path)
		if err != nil {
			return nil, err
		}
		key := RepoPathKey{Repository: repositoryID, Path: path}
		if _, exists := fileIndex[key]; exists {
			return nil, fmt.Errorf("%w: repository %q holds two files at %q", ErrInvalidSnapshotRows, row.RepositoryKey, row.Path)
		}
		fileIDs[row.Key] = id
		fileIndex[key] = id
		fileRecords[index] = FileRecord{Repository: repositoryID, Package: packageID, Path: path}
	}

	symbolRecords := make([]SymbolRecord, len(symbols))
	symbolByStableKey := make(map[StableKey]SymbolID, len(symbols))
	symbolsByName := make(map[InternedString][]SymbolID)
	symbolsByQName := make(map[InternedString][]SymbolID)
	for index, row := range symbols {
		fileID, fileExists := fileIDs[row.FileKey]
		if row.StableKey == "" || row.CanonicalIdentity == "" || row.Name == "" || row.QualifiedName == "" || row.Kind == "" || !fileExists || index > 0 && symbols[index-1].StableKey == row.StableKey {
			return nil, fmt.Errorf("%w: symbol %d %q in file %q", ErrInvalidSnapshotRows, index, string(row.StableKey), row.FileKey)
		}
		id, err := allocator.Symbol()
		if err != nil {
			return nil, err
		}
		canonical, err := interner.Intern(row.CanonicalIdentity)
		if err != nil {
			return nil, err
		}
		name, err := interner.Intern(row.Name)
		if err != nil {
			return nil, err
		}
		qualifiedName, err := interner.Intern(row.QualifiedName)
		if err != nil {
			return nil, err
		}
		kind, err := interner.Intern(row.Kind)
		if err != nil {
			return nil, err
		}
		signature, err := interner.Intern(row.Signature)
		if err != nil {
			return nil, err
		}
		symbolIDs[row.StableKey] = id
		symbolByStableKey[row.StableKey] = id
		symbolsByName[name] = append(symbolsByName[name], id)
		symbolsByQName[qualifiedName] = append(symbolsByQName[qualifiedName], id)
		symbolRecords[index] = SymbolRecord{StableKey: row.StableKey, CanonicalIdentity: canonical, File: fileID, Name: name, QualifiedName: qualifiedName, Kind: kind, Signature: signature, StartLine: row.StartLine, EndLine: row.EndLine}
	}

	sourcedEdges := make([]SourcedEdge, len(edges))
	evidenceRecords := make([]EvidenceRecord, len(edges))
	for index, row := range edges {
		sourceID, sourceExists := symbolIDs[row.SourceKey]
		targetID, targetExists := symbolIDs[row.TargetKey]
		sourceFileID, sourceFileExists := fileIDs[row.EvidenceSourceFileKey]
		targetFileID, targetFileExists := fileIDs[row.EvidenceTargetFileKey]
		if !sourceExists || !targetExists || !sourceFileExists || !targetFileExists || row.EvidenceKind == "" || index > 0 && edgeEqual(edges[index-1], row) {
			return nil, fmt.Errorf("%w: edge %d %q->%q kind %d evidence %q files %q/%q", ErrInvalidSnapshotRows, index, string(row.SourceKey), string(row.TargetKey), row.Kind, row.EvidenceKind, row.EvidenceSourceFileKey, row.EvidenceTargetFileKey)
		}
		evidenceKind, err := interner.Intern(row.EvidenceKind)
		if err != nil {
			return nil, err
		}
		provenance, err := interner.Intern("LADYBUGDB")
		if err != nil {
			return nil, err
		}
		evidenceID, err := allocator.Evidence()
		if err != nil {
			return nil, err
		}
		evidenceRecords[index] = EvidenceRecord{SourceFile: sourceFileID, TargetFile: targetFileID, Kind: evidenceKind, Provenance: provenance}
		sourcedEdges[index] = SourcedEdge{Source: sourceID, Edge: PackedEdge{Target: targetID, Evidence: evidenceID, Kind: row.Kind, Confidence: row.Confidence, Provenance: row.Provenance, Flags: row.Flags}}
	}

	forwardOffsets, forwardEdges, err := BuildForwardCSR(uint32(len(symbolRecords)), sourcedEdges)
	if err != nil {
		return nil, err
	}
	reverseOffsets, reverseEdges, err := BuildReverseCSR(uint32(len(symbolRecords)), forwardOffsets, forwardEdges)
	if err != nil {
		return nil, err
	}
	return NewGraphSnapshot(GraphSnapshotInput{
		ID: snapshotID, CreatedAt: createdAt, Version: version, Strings: interner.Freeze(),
		Repositories: repositoryRecords, Packages: packageRecords, Files: fileRecords, Symbols: symbolRecords, Evidence: evidenceRecords,
		ForwardOffsets: forwardOffsets, ForwardEdges: forwardEdges, ReverseOffsets: reverseOffsets, ReverseEdges: reverseEdges,
		SymbolByStableKey: symbolByStableKey, SymbolsByName: symbolsByName, SymbolsByQName: symbolsByQName, FileByRepoPath: fileIndex,
	})
}

func edgeLess(left, right EdgeRow) bool {
	if left.SourceKey != right.SourceKey {
		return left.SourceKey < right.SourceKey
	}
	if left.TargetKey != right.TargetKey {
		return left.TargetKey < right.TargetKey
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.Confidence != right.Confidence {
		return left.Confidence < right.Confidence
	}
	if left.Provenance != right.Provenance {
		return left.Provenance < right.Provenance
	}
	if left.Flags != right.Flags {
		return left.Flags < right.Flags
	}
	if left.EvidenceKey != right.EvidenceKey {
		return left.EvidenceKey < right.EvidenceKey
	}
	if left.EvidenceKind != right.EvidenceKind {
		return left.EvidenceKind < right.EvidenceKind
	}
	if left.EvidenceSourceFileKey != right.EvidenceSourceFileKey {
		return left.EvidenceSourceFileKey < right.EvidenceSourceFileKey
	}
	return left.EvidenceTargetFileKey < right.EvidenceTargetFileKey
}

func edgeEqual(left, right EdgeRow) bool {
	return left.SourceKey == right.SourceKey &&
		left.TargetKey == right.TargetKey &&
		left.Kind == right.Kind &&
		left.Confidence == right.Confidence &&
		left.Provenance == right.Provenance &&
		left.Flags == right.Flags &&
		left.EvidenceKey == right.EvidenceKey &&
		left.EvidenceKind == right.EvidenceKind &&
		left.EvidenceSourceFileKey == right.EvidenceSourceFileKey &&
		left.EvidenceTargetFileKey == right.EvidenceTargetFileKey
}
