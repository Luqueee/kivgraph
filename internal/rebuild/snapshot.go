package rebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

// ErrSnapshotBuildFailed reports that the definitive graph read from a
// database could not become a HotSnapshot: an edge table outside the
// canonical vocabulary, an unknown confidence or provenance code, a source
// line that overflows uint32, or a dangling symbol or evidence reference.
// LUQUE-0904's invariants (no dangling exact edges, no missing evidence, no
// unknown confidence or provenance) already guarantee a valid canonical
// graph never trips any of these — every case here is a coherence
// assertion, not a value this package expects to actually see, so a broken
// graph fails loudly instead of silently producing a wrong snapshot.
var ErrSnapshotBuildFailed = errors.New("snapshot build failed")

// SnapshotRowFormatVersion versions the mapping this file implements from
// ladybug.CanonicalGraph to hotsnapshot.LadybugSnapshotRows. It is distinct
// from both ladybug.CanonicalSchemaVersion (the database layout) and
// facts.CodeFormatVersion (the kind/confidence/provenance numbering): this
// constant changes only when this adapter's own row shaping rules change in
// a way that would produce a different snapshot for the same canonical
// graph.
const SnapshotRowFormatVersion uint32 = 3

// SnapshotStats accounts for what a built snapshot contains.
type SnapshotStats struct {
	Repositories int
	Packages     int
	Files        int
	Symbols      int
	Evidence     int
	Edges        int
	PackageEdges int
	Unresolved   int
	SkippedEdges int
}

// SnapshotReport is the account of one snapshot build.
type SnapshotReport struct {
	SnapshotID uint64
	Version    uint32
	Digest     string
	Stats      SnapshotStats
	Passed     bool
}

// BuildSnapshotOptions configures one snapshot build.
type BuildSnapshotOptions struct {
	DatabasePath string
	SnapshotID   uint64
	Metrics      *metrics.Registry

	// Scan defaults to ladybug.ScanCanonical, exactly like Options.Load,
	// Options.Counts, Options.Probes and Options.Integrity already default
	// for Run: tests substitute it so this is exercised without cgo.
	Scan func(context.Context, string) (ladybug.CanonicalGraph, error)
}

// BuildSnapshot builds the in-memory HotSnapshot from the definitive graph
// stored at options.DatabasePath — never from a facts.Set. That is the
// point of LUQUE-0906: the snapshot is derived from what is actually
// stored in LadybugDB, the same definitive graph doctor graph and rollback
// already verify, not from the input the loader was handed. A graph that
// cannot become a snapshot is reported as an error wrapping
// ErrSnapshotBuildFailed; a *hotsnapshot.GraphSnapshot is only ever
// returned once hotsnapshot.BuildGraphSnapshot has itself validated it.
func BuildSnapshot(ctx context.Context, options BuildSnapshotOptions) (*hotsnapshot.GraphSnapshot, SnapshotReport, error) {
	buildStart := time.Now()
	report := SnapshotReport{SnapshotID: options.SnapshotID, Version: SnapshotRowFormatVersion}
	if err := ctx.Err(); err != nil {
		return nil, report, fmt.Errorf("%w: %w", ErrSnapshotBuildFailed, err)
	}

	scan := options.Scan
	if scan == nil {
		scan = ladybug.ScanCanonical
	}
	graph, err := scan(ctx, options.DatabasePath)
	if err != nil {
		return nil, report, fmt.Errorf("%w: scan canonical graph: %w", ErrSnapshotBuildFailed, err)
	}

	rows, skippedEdges, err := convertCanonicalGraph(graph)
	if err != nil {
		return nil, report, err
	}
	report.Digest = snapshotContentDigest(rows)

	// CreatedAt records when this snapshot was actually built; it must
	// never affect the digest above. snapshotContentDigest is computed
	// from rows alone, above, before CreatedAt even exists, so two builds
	// of the same underlying graph — published as different generations,
	// built at different wall clock times — compare equal by construction,
	// not by convention.
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, options.SnapshotID, time.Now().UTC(), SnapshotRowFormatVersion)
	if err != nil {
		return nil, report, fmt.Errorf("%w: build graph snapshot: %w", ErrSnapshotBuildFailed, err)
	}

	// Stats are read back from the snapshot's own metadata rather than
	// hand accumulated during conversion, so they can never drift from
	// what the snapshot actually holds; SkippedEdges is the one count
	counts := snapshot.Metadata().Counts
	report.Stats = SnapshotStats{
		Repositories: int(counts.Repositories),
		Packages:     int(counts.Packages),
		Files:        int(counts.Files),
		Symbols:      int(counts.Symbols),
		Evidence:     int(counts.Evidence),
		Edges:        int(counts.Edges),
		PackageEdges: int(counts.PackageEdges),
		Unresolved:   int(counts.Unresolved),
		SkippedEdges: skippedEdges,
	}
	report.Passed = true
	if options.Metrics != nil {
		databaseBytes := int64(0)
		if info, err := os.Stat(options.DatabasePath); err == nil {
			databaseBytes = info.Size()
		}
		metadata := snapshot.Metadata()
		options.Metrics.ObserveSnapshot(metrics.SnapshotObservation{
			ID:            metadata.ID,
			CreatedAt:     metadata.CreatedAt,
			BuildDuration: time.Since(buildStart),
			Bytes:         databaseBytes,
		})
	}
	return snapshot, report, nil
}

// GenerationSnapshotOptions resolves a generation inside a generation store
// and builds its HotSnapshot in one call — the same root+generation
// resolution RollbackOptions already does for Rollback, applied here to
// BuildSnapshot instead of generation.Store.Restore.
type GenerationSnapshotOptions struct {
	Root    string
	Store   generation.Config
	Metrics *metrics.Registry

	// GenerationID selects which published generation to snapshot; empty
	// means graph.active — the generation actually serving right now,
	// unlike RollbackOptions.GenerationID, whose empty value means the
	// registered graph.backup.
	GenerationID string
	SnapshotID   uint64
	Scan         func(context.Context, string) (ladybug.CanonicalGraph, error)
}

// SnapshotGeneration resolves options.GenerationID (or, when empty, the
// active generation) inside the generation store rooted at options.Root,
// then builds its HotSnapshot exactly like BuildSnapshot does. It is the
// entry point the snapshot CLI command uses.
func SnapshotGeneration(ctx context.Context, options GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, SnapshotReport, error) {
	report := SnapshotReport{SnapshotID: options.SnapshotID, Version: SnapshotRowFormatVersion}
	store, err := openGenerationStore(options.Root, options.Store)
	if err != nil {
		return nil, report, fmt.Errorf("%w: open generation store: %w", ErrSnapshotBuildFailed, err)
	}
	target, err := resolveGeneration(ctx, store, options.GenerationID)
	if err != nil {
		return nil, report, fmt.Errorf("%w: %w", ErrSnapshotBuildFailed, err)
	}
	return BuildSnapshot(ctx, BuildSnapshotOptions{
		DatabasePath: target.DatabasePath,
		SnapshotID:   options.SnapshotID,
		Metrics:      options.Metrics,
		Scan:         options.Scan,
	})
}

// resolveGeneration returns the generation named by id, or graph.active
// when id is empty.
func resolveGeneration(ctx context.Context, store *generation.Store, id string) (generation.Generation, error) {
	if id == "" {
		current, err := store.Current(ctx)
		if err != nil {
			return generation.Generation{}, fmt.Errorf("read active generation: %w", err)
		}
		return current, nil
	}
	generations, err := store.List(ctx)
	if err != nil {
		return generation.Generation{}, fmt.Errorf("list generations: %w", err)
	}
	for _, candidate := range generations {
		if candidate.ID == id {
			return candidate, nil
		}
	}
	return generation.Generation{}, fmt.Errorf("generation %s not found", id)
}

// symbolToSymbolTables classifies every canonical relationship table by
// whether it connects Symbol to Symbol — the only shape hotsnapshot's CSR
// can hold, since GraphSnapshot indexes exclusively by Symbol.StableKey.
// Built from ladybug.CanonicalRelationshipTables itself, not a hand written
// second enumeration, so a schema change can never drift out of sync with
// what this adapter accepts.
func symbolToSymbolTables() map[string]bool {
	tables := ladybug.CanonicalRelationshipTables()
	classified := make(map[string]bool, len(tables))
	for _, table := range tables {
		classified[table.Name] = table.From == "Symbol" && table.To == "Symbol"
	}
	return classified
}

// convertCanonicalGraph maps one definitive graph onto the row shape
// hotsnapshot.BuildGraphSnapshot requires, and reports how many edges could
// not be represented. It never receives, and never needs, a facts.Set: the
// graph already stored in LadybugDB is what gets snapshotted.
func convertCanonicalGraph(graph ladybug.CanonicalGraph) (hotsnapshot.LadybugSnapshotRows, int, error) {
	var rows hotsnapshot.LadybugSnapshotRows
	// Provenance of the definitive graph, carried so a status query can name
	// the schema and resolver behind the published snapshot. A graph written
	// by an older loader may not record a resolver version; that gap travels
	// as an empty string rather than as an invented default.
	rows.SchemaVersion = graph.SchemaVersion
	rows.ResolverVersion = graph.Metadata["resolver_version"]

	rows.Repositories = make([]hotsnapshot.RepositoryRow, len(graph.Repositories))
	for index, repository := range graph.Repositories {
		rows.Repositories[index] = hotsnapshot.RepositoryRow{
			Key: repository.StableKey, Name: repository.Name, Path: repository.RootPath,
			Languages: repository.Languages, Commit: repository.Commit,
			Branch: repository.Branch, Dirty: repository.Dirty,
		}
	}

	rows.Packages = make([]hotsnapshot.PackageRow, len(graph.Packages))
	for index, pkg := range graph.Packages {
		rows.Packages[index] = hotsnapshot.PackageRow{
			Key: pkg.StableKey, RepositoryKey: pkg.RepositoryKey, Language: pkg.Language, Name: pkg.Name,
			// canonical_schema.go documents Package.container as "holds the
			// Go module" — it is the schema's own name for what hotsnapshot
			// calls a module path, and the only column a Go module path is
			// actually stored in on a canonical Package row.
			ModulePath: pkg.Container,
		}
	}

	rows.Files = make([]hotsnapshot.FileRow, len(graph.Files))
	for index, file := range graph.Files {
		rows.Files[index] = hotsnapshot.FileRow{
			Key: file.StableKey, RepositoryKey: file.RepositoryKey, PackageKey: file.PackageKey,
			Path: file.Path, Language: file.Language, ContentHash: file.ContentHash,
		}
	}

	// symbolFileKeys backs EvidenceSourceFileKey/EvidenceTargetFileKey
	// below: the canonical Evidence node carries a single file_key (where
	// its excerpt was captured), which cannot supply both ends an edge
	// needs, so both come from the edge's own source and target symbols
	// instead.
	symbolFileKeys := make(map[string]string, len(graph.Symbols))
	rows.Symbols = make([]hotsnapshot.SymbolRow, len(graph.Symbols))
	for index, symbol := range graph.Symbols {
		startLine, err := lineToUint32(symbol.StartLine)
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: symbol %s: start_line %d: %w", ErrSnapshotBuildFailed, symbol.StableKey, symbol.StartLine, err)
		}
		endLine, err := lineToUint32(symbol.EndLine)
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: symbol %s: end_line %d: %w", ErrSnapshotBuildFailed, symbol.StableKey, symbol.EndLine, err)
		}
		rows.Symbols[index] = hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(symbol.StableKey), CanonicalIdentity: symbol.CanonicalIdentity,
			FileKey: symbol.FileKey, Language: symbol.Language, Name: symbol.Name, QualifiedName: symbol.QualifiedName,
			Kind: symbol.Kind, Signature: symbol.Signature, Exported: symbol.Exported,
			StartLine: startLine, EndLine: endLine,
		}
		symbolFileKeys[symbol.StableKey] = symbol.FileKey
	}

	evidenceExists := make(map[string]bool, len(graph.Evidence))
	for _, evidence := range graph.Evidence {
		evidenceExists[evidence.StableKey] = true
	}
	symbolTables := symbolToSymbolTables()
	packageKeys := make(map[string]bool, len(graph.Packages))
	for _, pkg := range graph.Packages {
		packageKeys[pkg.StableKey] = true
	}
	skippedEdges := 0
	rows.Edges = make([]hotsnapshot.EdgeRow, 0, len(graph.Edges))
	rows.PackageDependencies = make([]hotsnapshot.PackageDependencyRow, 0)
	for _, edge := range graph.Edges {
		isPackageDependency := edge.Table == string(facts.PackageDependsOn) || edge.Table == string(facts.ModuleDependsOn)
		if isPackageDependency {
			kindCode, err := facts.EdgeKind(edge.Table).Code()
			if err != nil {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: package edge %s->%s: %w", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, err)
			}
			confidenceCode, err := facts.Confidence(edge.Confidence).Code()
			if err != nil {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: package edge %s->%s: %w", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, err)
			}
			provenanceCode, err := facts.Provenance(edge.Provenance).Code()
			if err != nil {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: package edge %s->%s: %w", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, err)
			}
			if !packageKeys[edge.SourceKey] || !packageKeys[edge.TargetKey] {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: package edge %s->%s has a dangling package", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey)
			}
			if edge.EvidenceKey != "" && !evidenceExists[edge.EvidenceKey] {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: package edge %s->%s: evidence_key %q does not resolve", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, edge.EvidenceKey)
			}
			rows.PackageDependencies = append(rows.PackageDependencies, hotsnapshot.PackageDependencyRow{
				SourceKey: edge.SourceKey, TargetKey: edge.TargetKey, Kind: kindCode,
				Confidence: confidenceCode, Provenance: provenanceCode, EvidenceKey: edge.EvidenceKey,
			})
			continue
		}

		isSymbolToSymbol, known := symbolTables[edge.Table]
		if !known {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge table %q is outside the canonical vocabulary", ErrSnapshotBuildFailed, edge.Table)
		}
		if !isSymbolToSymbol {
			// Structural edges have already been represented by node ownership
			// fields, so they are intentionally not copied into symbol CSR.
			skippedEdges++
			continue
		}

		kindCode, err := facts.EdgeKind(edge.Table).Code()
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge %s->%s: %w", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, err)
		}
		confidenceCode, err := facts.Confidence(edge.Confidence).Code()
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge %s->%s: %w", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, err)
		}
		provenanceCode, err := facts.Provenance(edge.Provenance).Code()
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge %s->%s: %w", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, err)
		}

		sourceFileKey, sourceFound := symbolFileKeys[edge.SourceKey]
		if !sourceFound {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge %s->%s: source symbol %q not found", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, edge.SourceKey)
		}
		targetFileKey, targetFound := symbolFileKeys[edge.TargetKey]
		if !targetFound {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge %s->%s: target symbol %q not found", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, edge.TargetKey)
		}

		// A present evidence_key is a durable reference: LUQUE-0904's
		// invariants guarantee it always resolves in a valid graph, so a
		// miss here is the coherence assertion the plan calls for. An
		// absent evidence_key is not an error — some canonical rows
		// legitimately carry none — and EvidenceKind/Source/TargetFileKey
		// below never actually depend on the Evidence row resolving.
		if edge.EvidenceKey != "" && !evidenceExists[edge.EvidenceKey] {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: edge %s->%s: evidence_key %q does not resolve", ErrSnapshotBuildFailed, edge.SourceKey, edge.TargetKey, edge.EvidenceKey)
		}

		rows.Edges = append(rows.Edges, hotsnapshot.EdgeRow{
			SourceKey: hotsnapshot.StableKey(edge.SourceKey), TargetKey: hotsnapshot.StableKey(edge.TargetKey),
			Kind: kindCode, Confidence: confidenceCode, Provenance: provenanceCode, Flags: 0,
			// EvidenceKind carries the edge's own provenance string — the
			// human readable mechanism that produced it (e.g.
			// "GO_TYPES_DEF") — as the audit trail companion to the packed
			// numeric Provenance code above, the same role CanonicalIdentity
			// plays for StableKey elsewhere in this codebase. It is always
			// non-empty because Provenance was already validated above.
			// EvidenceKey is what keeps two occurrences of the same relation
			// distinguishable: the canonical model is MANY_MANY precisely so
			// the same symbol can reach the same target from several places.
			EvidenceKey:  edge.EvidenceKey,
			EvidenceKind: edge.Provenance, EvidenceSourceFileKey: sourceFileKey, EvidenceTargetFileKey: targetFileKey,
		})
	}

	rows.Unresolved = make([]hotsnapshot.UnresolvedReferenceRow, 0, len(graph.Unresolved))
	repositoryKeys := make(map[string]bool, len(graph.Repositories))
	for _, repository := range graph.Repositories {
		repositoryKeys[repository.StableKey] = true
	}
	fileKeys := make(map[string]string, len(graph.Files))
	for _, file := range graph.Files {
		fileKeys[file.StableKey] = file.RepositoryKey
	}
	for _, unresolved := range graph.Unresolved {
		if unresolved.StableKey == "" || !repositoryKeys[unresolved.RepositoryKey] ||
			unresolved.RequestedPackage == "" || unresolved.Reason == "" {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: invalid unresolved reference %q", ErrSnapshotBuildFailed, unresolved.StableKey)
		}
		if unresolved.FileKey != "" && fileKeys[unresolved.FileKey] != unresolved.RepositoryKey {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved reference %q file %q does not belong to repository %q", ErrSnapshotBuildFailed, unresolved.StableKey, unresolved.FileKey, unresolved.RepositoryKey)
		}
		if unresolved.SourceSymbolKey != "" {
			sourceFileKey, sourceFound := symbolFileKeys[unresolved.SourceSymbolKey]
			if !sourceFound {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved reference %q source symbol %q not found", ErrSnapshotBuildFailed, unresolved.StableKey, unresolved.SourceSymbolKey)
			}
			if unresolved.FileKey != "" && sourceFileKey != unresolved.FileKey {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved reference %q source/file mismatch", ErrSnapshotBuildFailed, unresolved.StableKey)
			}
			if fileKeys[sourceFileKey] != unresolved.RepositoryKey {
				return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved reference %q source repository mismatch", ErrSnapshotBuildFailed, unresolved.StableKey)
			}
		}
		startLine, err := lineToUint32(unresolved.StartLine)
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved %s start_line: %w", ErrSnapshotBuildFailed, unresolved.StableKey, err)
		}
		startColumn, err := lineToUint32(unresolved.StartColumn)
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved %s start_column: %w", ErrSnapshotBuildFailed, unresolved.StableKey, err)
		}
		startOffset, err := lineToUint32(unresolved.StartOffset)
		if err != nil {
			return hotsnapshot.LadybugSnapshotRows{}, 0, fmt.Errorf("%w: unresolved %s start_offset: %w", ErrSnapshotBuildFailed, unresolved.StableKey, err)
		}
		rows.Unresolved = append(rows.Unresolved, hotsnapshot.UnresolvedReferenceRow{
			Key: unresolved.StableKey, RepositoryKey: unresolved.RepositoryKey, FileKey: unresolved.FileKey,
			SourceKey: hotsnapshot.StableKey(unresolved.SourceSymbolKey), Language: unresolved.Language,
			RequestedPackage: unresolved.RequestedPackage, RequestedSymbol: unresolved.RequestedSymbol,
			Reason: unresolved.Reason, Detail: unresolved.Detail,
			StartLine: startLine, StartColumn: startColumn, StartOffset: startOffset,
		})
	}

	return rows, skippedEdges, nil
}

// lineToUint32 converts a canonical INT64 source line into the uint32
// hotsnapshot.SymbolRow requires. It rejects anything that would silently
// truncate: a line number this large means a file with billions of lines,
// which is itself evidence of a bad read, not something to paper over.
func lineToUint32(value int64) (uint32, error) {
	if value < 0 || value > math.MaxUint32 {
		return 0, fmt.Errorf("%d does not fit in a uint32", value)
	}
	return uint32(value), nil
}

// snapshotContentDigest hashes exactly what ends up inside the built
// snapshot — repositories, packages, files, symbols, package dependencies,
// unresolved references, and the semantic edges hotsnapshot actually keeps —
// sorted by durable key so the row order ScanCanonical happens to return never
// changes the digest.
//
// It deliberately excludes SnapshotID, CreatedAt and the skipped edge
// count: two builds of the same underlying graph, published as different
// generations at different times, must compare equal here, and a skipped
// edge is accounting about what did not enter the snapshot, not content the
// snapshot holds.
//
// canonicalSnapshotDigest does not fit reuse here: it hashes per table row
// counts read from LadybugDB, not row content, so two structurally
// different graphs that happen to share matching counts would collide.
// This hashes the sorted rows themselves, using the same SHA-256/hex
func snapshotContentDigest(rows hotsnapshot.LadybugSnapshotRows) string {
	repositories := append([]hotsnapshot.RepositoryRow(nil), rows.Repositories...)
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Key < repositories[j].Key })
	packages := append([]hotsnapshot.PackageRow(nil), rows.Packages...)
	sort.Slice(packages, func(i, j int) bool { return packages[i].Key < packages[j].Key })
	files := append([]hotsnapshot.FileRow(nil), rows.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Key < files[j].Key })
	symbols := append([]hotsnapshot.SymbolRow(nil), rows.Symbols...)
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].StableKey < symbols[j].StableKey })
	packageDependencies := append([]hotsnapshot.PackageDependencyRow(nil), rows.PackageDependencies...)
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
	unresolved := append([]hotsnapshot.UnresolvedReferenceRow(nil), rows.Unresolved...)
	sort.Slice(unresolved, func(i, j int) bool { return unresolved[i].Key < unresolved[j].Key })
	edges := append([]hotsnapshot.EdgeRow(nil), rows.Edges...)
	sort.Slice(edges, func(i, j int) bool { return edgeRowLess(edges[i], edges[j]) })

	hash := sha256.New()
	fmt.Fprintf(hash, "snapshot_row_format=%d\n", SnapshotRowFormatVersion)
	for _, repository := range repositories {
		fmt.Fprintf(hash, "repository:%s name=%s path=%s languages=%s commit=%s\n",
			repository.Key, repository.Name, repository.Path, repository.Languages, repository.Commit)
	}
	for _, pkg := range packages {
		fmt.Fprintf(hash, "package:%s repository=%s language=%s name=%s module_path=%s\n",
			pkg.Key, pkg.RepositoryKey, pkg.Language, pkg.Name, pkg.ModulePath)
	}
	for _, file := range files {
		fmt.Fprintf(hash, "file:%s repository=%s package=%s path=%s language=%s\n",
			file.Key, file.RepositoryKey, file.PackageKey, file.Path, file.Language)
	}
	for _, symbol := range symbols {
		fmt.Fprintf(hash, "symbol:%s identity=%s file=%s language=%s name=%s qname=%s kind=%s signature=%s start=%d end=%d\n",
			symbol.StableKey, symbol.CanonicalIdentity, symbol.FileKey, symbol.Language, symbol.Name, symbol.QualifiedName, symbol.Kind, symbol.Signature, symbol.StartLine, symbol.EndLine)
	}
	for _, dependency := range packageDependencies {
		fmt.Fprintf(hash, "package_edge:%s->%s kind=%d confidence=%d provenance=%d evidence_key=%s\n",
			dependency.SourceKey, dependency.TargetKey, dependency.Kind, dependency.Confidence, dependency.Provenance, dependency.EvidenceKey)
	}
	for _, reference := range unresolved {
		fmt.Fprintf(hash, "unresolved:%s repository=%s file=%s source=%s language=%s requested_package=%s requested_symbol=%s reason=%s detail=%s start=%d:%d:%d\n",
			reference.Key, reference.RepositoryKey, reference.FileKey, reference.SourceKey, reference.Language,
			reference.RequestedPackage, reference.RequestedSymbol, reference.Reason, reference.Detail,
			reference.StartLine, reference.StartColumn, reference.StartOffset)
	}
	for _, edge := range edges {
		fmt.Fprintf(hash, "edge:%s->%s kind=%d confidence=%d provenance=%d flags=%d evidence_key=%s evidence_kind=%s evidence_source=%s evidence_target=%s\n",
			edge.SourceKey, edge.TargetKey, edge.Kind, edge.Confidence, edge.Provenance, edge.Flags, edge.EvidenceKey, edge.EvidenceKind, edge.EvidenceSourceFileKey, edge.EvidenceTargetFileKey)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// edgeRowLess orders edge rows deterministically for digesting: the same
// key hotsnapshot.BuildGraphSnapshot itself sorts and deduplicates edges
// by, reimplemented here because that comparator is unexported.
func edgeRowLess(left, right hotsnapshot.EdgeRow) bool {
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
