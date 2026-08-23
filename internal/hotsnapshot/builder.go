package hotsnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrInvalidSnapshotRows = errors.New("invalid snapshot rows")

// LadybugSnapshotRows is the ordered, canonical row set read from LadybugDB.
// Keys are durable database keys; the builder alone assigns dense snapshot IDs.
// SchemaVersion and ResolverVersion describe the graph the rows were read
// from, and are carried through to the snapshot's metadata unchanged.
type LadybugSnapshotRows struct {
	SchemaVersion       int
	ResolverVersion     string
	Repositories        []RepositoryRow
	Packages            []PackageRow
	Files               []FileRow
	Symbols             []SymbolRow
	Edges               []EdgeRow
	PackageDependencies []PackageDependencyRow
	Unresolved          []UnresolvedReferenceRow
}

type RepositoryRow struct {
	Key       string
	Name      string
	Path      string
	Languages string
	Commit    string
	Branch    string
	Dirty     bool
}

type PackageRow struct {
	Key           string
	RepositoryKey string
	Language      string
	Name          string
	ModulePath    string
}

type FileRow struct {
	Key           string
	RepositoryKey string
	PackageKey    string
	Path          string
	Language      string
	// ContentHash is the hex SHA-256 the canonical row carries. Anything that is
	// not a SHA-256 in hexadecimal lands as a zero digest, which reads as "no
	// comparable hash" and never as "fresh".
	ContentHash string
}

type SymbolRow struct {
	StableKey         StableKey
	CanonicalIdentity string
	FileKey           string
	Language          string
	Name              string
	QualifiedName     string
	Kind              string
	Signature         string
	Exported          bool
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

type PackageDependencyRow struct {
	SourceKey   string
	TargetKey   string
	Kind        uint8
	Confidence  uint8
	Provenance  uint8
	EvidenceKey string
}

type UnresolvedReferenceRow struct {
	Key              string
	RepositoryKey    string
	FileKey          string
	SourceKey        StableKey
	Language         string
	RequestedPackage string
	RequestedSymbol  string
	Reason           string
	Detail           string
	StartLine        uint32
	StartColumn      uint32
	StartOffset      uint32
}

// BuildGraphSnapshot converts canonical LadybugDB rows into one validated,
func BuildGraphSnapshot(rows LadybugSnapshotRows, snapshotID uint64, createdAt time.Time, version uint32) (*GraphSnapshot, error) {
	repositories := append([]RepositoryRow(nil), rows.Repositories...)
	packages := append([]PackageRow(nil), rows.Packages...)
	files := append([]FileRow(nil), rows.Files...)
	symbols := append([]SymbolRow(nil), rows.Symbols...)
	edges := append([]EdgeRow(nil), rows.Edges...)
	packageDependencies := append([]PackageDependencyRow(nil), rows.PackageDependencies...)
	unresolved := append([]UnresolvedReferenceRow(nil), rows.Unresolved...)
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Key < repositories[j].Key })
	sort.Slice(packages, func(i, j int) bool { return packages[i].Key < packages[j].Key })
	sort.Slice(files, func(i, j int) bool { return files[i].Key < files[j].Key })
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].StableKey < symbols[j].StableKey })
	sort.Slice(edges, func(i, j int) bool { return edgeLess(edges[i], edges[j]) })
	sort.Slice(packageDependencies, func(i, j int) bool {
		if packageDependencies[i].SourceKey != packageDependencies[j].SourceKey {
			return packageDependencies[i].SourceKey < packageDependencies[j].SourceKey
		}
		if packageDependencies[i].TargetKey != packageDependencies[j].TargetKey {
			return packageDependencies[i].TargetKey < packageDependencies[j].TargetKey
		}
		if packageDependencies[i].Kind != packageDependencies[j].Kind {
			return packageDependencies[i].Kind < packageDependencies[j].Kind
		}
		return packageDependencies[i].EvidenceKey < packageDependencies[j].EvidenceKey
	})
	sort.Slice(unresolved, func(i, j int) bool { return unresolved[i].Key < unresolved[j].Key })

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
		repositoryRecords[index] = RepositoryRecord{Name: name, Commit: commit, Dirty: row.Dirty}
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
	// A set, not an index: newFileIndex derives the index and refuses the same
	// duplicate. This one exists to refuse it here, where the row is still in
	// hand and the error can name the repository and the path a person wrote
	// rather than the two interned ids they became.
	filePaths := make(map[RepoPathKey]struct{}, len(files))
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
		repoPath := RepoPathKey{Repository: repositoryID, Path: path}
		if _, exists := filePaths[repoPath]; exists {
			return nil, fmt.Errorf("%w: repository %q holds two files at %q", ErrInvalidSnapshotRows, row.RepositoryKey, row.Path)
		}
		fileIDs[row.Key] = id
		filePaths[repoPath] = struct{}{}
		fileRecords[index] = FileRecord{Repository: repositoryID, Package: packageID, Path: path}
	}

	symbolRecords := make([]SymbolRecord, len(symbols))
	// The keys are collected in row order, which is key order: the sort above
	// put them there. NewStableKeyTable insists on that rather than sorting
	// again, because reordering here would move the IDs allocated below.
	stableKeys := make([]StableKey, 0, len(symbols))
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
		stableKeys = append(stableKeys, row.StableKey)
		symbolRecords[index] = SymbolRecord{StableKey: StableKeyID(id), CanonicalIdentity: canonical, File: fileID, Name: name, QualifiedName: qualifiedName, Kind: kind, Signature: signature, Exported: row.Exported, StartLine: row.StartLine, EndLine: row.EndLine}
	}
	stableKeyTable, err := NewStableKeyTable(stableKeys)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSnapshotRows, err)
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
	for index, row := range repositories {
		key, err := interner.Intern(row.Key)
		if err != nil {
			return nil, err
		}
		path, err := interner.Intern(row.Path)
		if err != nil {
			return nil, err
		}
		languages, err := interner.Intern(row.Languages)
		if err != nil {
			return nil, err
		}
		branch, err := interner.Intern(row.Branch)
		if err != nil {
			return nil, err
		}
		repositoryRecords[index].Key = key
		repositoryRecords[index].Path = path
		repositoryRecords[index].Languages = languages
		repositoryRecords[index].Branch = branch
	}
	for index, row := range packages {
		key, err := interner.Intern(row.Key)
		if err != nil {
			return nil, err
		}
		packageRecords[index].Key = key
	}
	for index, row := range files {
		key, err := interner.Intern(row.Key)
		if err != nil {
			return nil, err
		}
		language, err := interner.Intern(row.Language)
		if err != nil {
			return nil, err
		}
		fileRecords[index].Key = key
		fileRecords[index].Language = language
		fileRecords[index].ContentDigest = decodeContentDigest(row.ContentHash)
	}
	for index, row := range symbols {
		language, err := interner.Intern(row.Language)
		if err != nil {
			return nil, err
		}
		symbolRecords[index].Language = language
	}
	for index, row := range edges {
		key, err := interner.Intern(row.EvidenceKey)
		if err != nil {
			return nil, err
		}
		evidenceRecords[index].Key = key
	}

	for index, row := range packages {
		language, err := interner.Intern(row.Language)
		if err != nil {
			return nil, err
		}
		packageRecords[index].Language = language
	}
	packageDependencyRecords := make([]PackageDependencyRecord, len(packageDependencies))
	for index, row := range packageDependencies {
		sourceID, sourceExists := packageIDs[row.SourceKey]
		targetID, targetExists := packageIDs[row.TargetKey]
		if !sourceExists || !targetExists || row.Kind == 0 || row.Confidence == 0 || row.Provenance == 0 ||
			index > 0 && packageDependencyEqual(packageDependencies[index-1], row) {
			return nil, fmt.Errorf("%w: package dependency %d %q->%q", ErrInvalidSnapshotRows, index, row.SourceKey, row.TargetKey)
		}
		evidence, err := interner.Intern(row.EvidenceKey)
		if err != nil {
			return nil, err
		}
		packageDependencyRecords[index] = PackageDependencyRecord{
			Source: sourceID, Target: targetID, Kind: row.Kind, Confidence: row.Confidence,
			Provenance: row.Provenance, Evidence: evidence,
		}
	}

	unresolvedRecords := make([]UnresolvedReferenceRecord, len(unresolved))
	for index, row := range unresolved {
		repositoryID, repositoryExists := repositoryIDs[row.RepositoryKey]
		if row.Key == "" || !repositoryExists || row.RequestedPackage == "" || row.Reason == "" ||
			index > 0 && unresolved[index-1].Key == row.Key {
			return nil, fmt.Errorf("%w: unresolved reference %d %q", ErrInvalidSnapshotRows, index, row.Key)
		}
		fileID := InvalidFileID
		if row.FileKey != "" {
			var fileExists bool
			fileID, fileExists = fileIDs[row.FileKey]
			if !fileExists || fileRecords[fileID].Repository != repositoryID {
				return nil, fmt.Errorf("%w: unresolved reference %q file %q", ErrInvalidSnapshotRows, row.Key, row.FileKey)
			}
		}
		sourceID := InvalidSymbolID
		if row.SourceKey != "" {
			var sourceExists bool
			sourceID, sourceExists = symbolIDs[row.SourceKey]
			if !sourceExists {
				return nil, fmt.Errorf("%w: unresolved reference %q source %q", ErrInvalidSnapshotRows, row.Key, row.SourceKey)
			}
			if symbolRecords[sourceID].File != fileID && fileID != InvalidFileID {
				return nil, fmt.Errorf("%w: unresolved reference %q source/file mismatch", ErrInvalidSnapshotRows, row.Key)
			}
			if repositoryForSymbol := fileRecords[symbolRecords[sourceID].File].Repository; repositoryForSymbol != repositoryID {
				return nil, fmt.Errorf("%w: unresolved reference %q source repository mismatch", ErrInvalidSnapshotRows, row.Key)
			}
		}
		key, err := interner.Intern(row.Key)
		if err != nil {
			return nil, err
		}
		language, err := interner.Intern(row.Language)
		if err != nil {
			return nil, err
		}
		requestedPackage, err := interner.Intern(row.RequestedPackage)
		if err != nil {
			return nil, err
		}
		requestedSymbol, err := interner.Intern(row.RequestedSymbol)
		if err != nil {
			return nil, err
		}
		reason, err := interner.Intern(row.Reason)
		if err != nil {
			return nil, err
		}
		detail, err := interner.Intern(row.Detail)
		if err != nil {
			return nil, err
		}
		unresolvedRecords[index] = UnresolvedReferenceRecord{
			Key: key, Repository: repositoryID, File: fileID, Source: sourceID, Language: language,
			RequestedPackage: requestedPackage, RequestedSymbol: requestedSymbol, Reason: reason, Detail: detail,
			StartLine: row.StartLine, StartColumn: row.StartColumn, StartOffset: row.StartOffset,
		}
	}

	return NewGraphSnapshot(GraphSnapshotInput{
		ID: snapshotID, CreatedAt: createdAt, Version: version, Strings: interner.Freeze(),
		SchemaVersion: rows.SchemaVersion, ResolverVersion: rows.ResolverVersion,
		Repositories: repositoryRecords, Packages: packageRecords, Files: fileRecords, Symbols: symbolRecords, Evidence: evidenceRecords,
		PackageDependencies: packageDependencyRecords, Unresolved: unresolvedRecords,
		ForwardOffsets: forwardOffsets, ForwardEdges: forwardEdges, ReverseOffsets: reverseOffsets, ReverseEdges: reverseEdges,
		StableKeys: stableKeyTable,
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

func packageDependencyEqual(left, right PackageDependencyRow) bool {
	return left.SourceKey == right.SourceKey &&
		left.TargetKey == right.TargetKey &&
		left.Kind == right.Kind &&
		left.Confidence == right.Confidence &&
		left.Provenance == right.Provenance &&
		left.EvidenceKey == right.EvidenceKey
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

// decodeContentDigest turns the hex digest a canonical file row carries into the
// raw bytes the record stores.
//
// Anything that is not a SHA-256 in hexadecimal -- an empty value, a placeholder,
// a truncated digest -- becomes a zero digest, which reads as "the generation
// recorded no comparable hash". It does not fail the build: refusing to publish a
// whole generation because one row carries an odd hash trades a costlier answer
// for no answer at all.
//
// The direction of the degradation is what makes it safe. A zero digest can only
// make a file report as *not* fresh, never as fresh, so a reader is told to
// re-anchor and is never handed a range from a file that moved. See ADR 0040.
func decodeContentDigest(value string) [sha256.Size]byte {
	digest := [sha256.Size]byte{}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return digest
	}
	copy(digest[:], decoded)
	return digest
}
