package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/gitdiff"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/testpath"
)

// This file is the engine of `kivgraph impact`, a command and not a tool. It
// lives here because it answers with the same traversal, the same default edge
// vocabulary and the same withheld kinds as `get_blast_radius`, and those are
// unexported on purpose: a second copy in another package would drift. Nothing
// here is registered with the server, and no tool input or description changes.

// ErrRepositoryNotIndexed is returned when no repository of the snapshot lives at
// the checkout that was asked about.
var ErrRepositoryNotIndexed = errors.New("the checkout is not an indexed repository")

// ImpactOptions says what to compute the impact of.
type ImpactOptions struct {
	// RepositoryRoot is the checkout the changes were measured in; the
	// repository of the snapshot that lives there is the one they are mapped
	// onto.
	RepositoryRoot string
	Changes        []gitdiff.Change
	// Depth and MaxNodes bound the traversal exactly as they do for
	// `get_blast_radius`; zero takes its defaults.
	Depth, MaxNodes int
	// Limit caps the production rows. The count is always the whole one.
	Limit int
}

// ImpactSymbol is a symbol a change touches.
type ImpactSymbol struct {
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	FilePath      string `json:"file_path"`
	StartLine     uint32 `json:"start_line"`
	EndLine       uint32 `json:"end_line"`
	Test          bool   `json:"test"`
}

// ImpactAffected is a symbol that depends on a touched one.
type ImpactAffected struct {
	ImpactSymbol
	Repository string `json:"repository"`
	Depth      int    `json:"depth"`
}

// ImpactFile is a changed file the graph cannot place.
type ImpactFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ImpactGap is a run of changed lines that no symbol of an indexed file holds: a
// comment, an import, a blank line. EndLine 0 means the whole file.
type ImpactGap struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// ImpactTestFile is a test file with the affected symbols it declares.
type ImpactTestFile struct {
	FilePath string           `json:"file_path"`
	Symbols  []ImpactAffected `json:"symbols"`
}

// ImpactRepositoryCount is how many affected symbols one repository holds.
type ImpactRepositoryCount struct {
	Repository string `json:"repository"`
	Affected   int    `json:"affected"`
}

// ImpactReport is what a diff reaches.
type ImpactReport struct {
	Repository string `json:"repository"`
	SnapshotID uint64 `json:"snapshot_id"`
	Depth      int    `json:"depth"`
	MaxNodes   int    `json:"max_nodes"`

	Touched    []ImpactSymbol `json:"touched"`
	NotIndexed []ImpactFile   `json:"not_indexed"`
	NoSymbol   []ImpactGap    `json:"no_symbol"`

	// Affected counts what depends on a touched symbol, the touched ones
	// excluded: a symbol is not affected by its own change. Production and
	// ByTestFile partition it.
	Affected            int                     `json:"affected"`
	TraversalTruncated  bool                    `json:"traversal_truncated"`
	ProductionTotal     int                     `json:"production_total"`
	Production          []ImpactAffected        `json:"production"`
	ByTestFile          []ImpactTestFile        `json:"by_test_file"`
	ByRepository        []ImpactRepositoryCount `json:"by_repository"`
	DefaultExcludedKind []string                `json:"default_excluded_kinds"`
}

// Impact maps the changed lines onto the symbols that hold them and follows
// who depends on those, as one traversal from all of them at once.
func Impact(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, options ImpactOptions) (ImpactReport, error) {
	if snapshot == nil {
		return ImpactReport{}, ErrIndexNotReady()
	}
	if options.Depth <= 0 {
		options.Depth = DefaultBlastRadiusDepth
	}
	if options.MaxNodes <= 0 {
		options.MaxNodes = DefaultBlastRadiusMaxNodes
	}
	if options.Limit <= 0 {
		options.Limit = DefaultBlastRadiusLimit
	}
	repository, name, err := impactRepository(snapshot, options.RepositoryRoot)
	if err != nil {
		return ImpactReport{}, err
	}
	report := ImpactReport{
		Repository: name, SnapshotID: snapshot.Metadata().ID,
		Depth: options.Depth, MaxNodes: options.MaxNodes,
		Touched: []ImpactSymbol{}, NotIndexed: []ImpactFile{}, NoSymbol: []ImpactGap{},
		Production: []ImpactAffected{}, ByTestFile: []ImpactTestFile{}, ByRepository: []ImpactRepositoryCount{},
		DefaultExcludedKind: blastRadiusDefaultExcludedKinds,
	}

	filesByPath := make(map[string]hotsnapshot.FileID, len(options.Changes))
	var indexed []hotsnapshot.FileID
	table := snapshot.Strings()
	for _, change := range options.Changes {
		if err := ctx.Err(); err != nil {
			return ImpactReport{}, err
		}
		if id, found := impactFile(snapshot, repository, change.Path); found {
			filesByPath[change.Path] = id
			indexed = append(indexed, id)
			continue
		}
		if change.Untracked {
			continue
		}
		reason := "not in the index"
		if change.Deleted {
			reason = "deleted: the symbols it declared are no longer in the graph"
		}
		report.NotIndexed = append(report.NotIndexed, ImpactFile{Path: change.Path, Reason: reason})
	}

	symbolsByFile, err := impactSymbols(snapshot, indexed)
	if err != nil {
		return ImpactReport{}, err
	}
	seen := make(map[hotsnapshot.SymbolID]struct{})
	var seeds []hotsnapshot.SymbolID
	for _, change := range options.Changes {
		fileID, found := filesByPath[change.Path]
		if !found {
			continue
		}
		for _, changed := range change.Ranges {
			touched := innermostTouched(snapshot, symbolsByFile[fileID], changed)
			if len(touched) == 0 {
				gap := ImpactGap{Path: change.Path, StartLine: changed.Start, EndLine: changed.End}
				if changed.End == gitdiff.WholeFile {
					gap.StartLine, gap.EndLine = 1, 0
				}
				report.NoSymbol = append(report.NoSymbol, gap)
				continue
			}
			for _, id := range touched {
				if _, duplicate := seen[id]; duplicate {
					continue
				}
				seen[id] = struct{}{}
				seeds = append(seeds, id)
			}
		}
	}
	sort.Slice(seeds, func(left, right int) bool { return seeds[left] < seeds[right] })
	for _, id := range seeds {
		row, err := impactRow(snapshot, table, id)
		if err != nil {
			return ImpactReport{}, err
		}
		report.Touched = append(report.Touched, row)
	}
	// In the order a reader walks the files, not the order the snapshot stores them.
	sort.Slice(report.Touched, func(left, right int) bool {
		a, b := report.Touched[left], report.Touched[right]
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.QualifiedName < b.QualifiedName
	})
	if len(seeds) == 0 {
		return report, nil
	}

	traversalOptions, err := blastRadiusTraversalOptions(ctx, blastRadiusOptions{Depth: options.Depth, MaxNodes: options.MaxNodes})
	if err != nil {
		return ImpactReport{}, err
	}
	traversal, err := snapshot.TraverseFrom(seeds, traversalOptions)
	if err != nil {
		return ImpactReport{}, classifyTraversalError(err)
	}
	report.TraversalTruncated = traversal.Truncated

	var affected []ImpactAffected
	for _, visit := range traversal.Visits {
		if visit.Source == hotsnapshot.InvalidSymbolID {
			continue
		}
		row, err := impactRow(snapshot, table, visit.ID)
		if err != nil {
			return ImpactReport{}, err
		}
		if !blastRadiusKindReported(row.Kind) {
			continue
		}
		_, _, repo, _, err := symbolReferenceLocation(snapshot, visit.ID)
		if err != nil {
			return ImpactReport{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid impact metadata", err)
		}
		// A derived provider is how the graph reaches a dependency, not code
		// the repository owns; `get_blast_radius` withholds it by default.
		if facts.IsSyntheticRepository(repo.name) {
			continue
		}
		affected = append(affected, ImpactAffected{ImpactSymbol: row, Repository: repo.name, Depth: int(visit.Depth)})
	}
	sort.Slice(affected, func(left, right int) bool {
		a, b := affected[left], affected[right]
		switch {
		case a.Depth != b.Depth:
			return a.Depth < b.Depth
		case a.Repository != b.Repository:
			return a.Repository < b.Repository
		case a.FilePath != b.FilePath:
			return a.FilePath < b.FilePath
		case a.StartLine != b.StartLine:
			return a.StartLine < b.StartLine
		}
		return a.QualifiedName < b.QualifiedName
	})

	report.Affected = len(affected)
	byRepository := make(map[string]int)
	tests := make(map[string]*ImpactTestFile)
	var testOrder []string
	for _, row := range affected {
		byRepository[row.Repository]++
		if row.Test {
			key := row.Repository + "\x00" + row.FilePath
			if tests[key] == nil {
				tests[key] = &ImpactTestFile{FilePath: row.FilePath}
				testOrder = append(testOrder, key)
			}
			tests[key].Symbols = append(tests[key].Symbols, row)
			continue
		}
		report.ProductionTotal++
		if len(report.Production) < options.Limit {
			report.Production = append(report.Production, row)
		}
	}
	for _, key := range testOrder {
		report.ByTestFile = append(report.ByTestFile, *tests[key])
	}
	sort.Slice(report.ByTestFile, func(left, right int) bool {
		return report.ByTestFile[left].FilePath < report.ByTestFile[right].FilePath
	})
	for repo, count := range byRepository {
		report.ByRepository = append(report.ByRepository, ImpactRepositoryCount{Repository: repo, Affected: count})
	}
	sort.Slice(report.ByRepository, func(left, right int) bool {
		return report.ByRepository[left].Repository < report.ByRepository[right].Repository
	})
	return report, nil
}

// blastRadiusKindReported answers the default kind filter of `get_blast_radius`.
func blastRadiusKindReported(kind string) bool {
	return blastRadiusKindFilter{}.keeps(kind)
}

// impactRepository finds the repository of the snapshot that lives at root, by the
// resolved path: a checkout reached through a symbolic link is the same one.
func impactRepository(snapshot *hotsnapshot.GraphSnapshot, root string) (hotsnapshot.RepositoryID, string, error) {
	want := resolvedPath(root)
	table := snapshot.Strings()
	for index := range snapshot.Metadata().Counts.Repositories {
		id := hotsnapshot.RepositoryID(index)
		record, found := snapshot.Repository(id)
		if !found {
			continue
		}
		recorded, pathOK := table.String(record.Path)
		name, nameOK := table.String(record.Name)
		if pathOK && nameOK && resolvedPath(recorded) == want {
			return id, name, nil
		}
	}
	return 0, "", fmt.Errorf("%w: %s", ErrRepositoryNotIndexed, root)
}

func resolvedPath(value string) string {
	if resolved, err := filepath.EvalSymlinks(value); err == nil {
		value = resolved
	}
	return filepath.Clean(value)
}

// impactFile finds the file of the repository recorded at exactly this path.
func impactFile(snapshot *hotsnapshot.GraphSnapshot, repository hotsnapshot.RepositoryID, filePath string) (hotsnapshot.FileID, bool) {
	table := snapshot.Strings()
	for _, id := range snapshot.FilesUnder(repository, filePath) {
		record, found := snapshot.File(id)
		if !found {
			continue
		}
		if recorded, ok := table.String(record.Path); ok && recorded == filePath {
			return id, true
		}
	}
	return 0, false
}

// impactSymbols reads every symbol of the files in one walk, grouped by file.
func impactSymbols(snapshot *hotsnapshot.GraphSnapshot, files []hotsnapshot.FileID) (map[hotsnapshot.FileID][]hotsnapshot.SymbolID, error) {
	grouped := make(map[hotsnapshot.FileID][]hotsnapshot.SymbolID, len(files))
	if len(files) == 0 {
		return grouped, nil
	}
	for offset := 0; ; {
		page, err := snapshot.SearchSymbolsInFiles(files, offset, hotsnapshot.MaxExactResults, nil)
		if err != nil {
			return nil, WrapToolError(CodeSnapshotUnavailable, "active snapshot could not list the symbols of a file", err)
		}
		for _, id := range page.IDs {
			symbol, found := snapshot.Symbol(id)
			if !found {
				return nil, WrapToolError(CodeSnapshotUnavailable, "active snapshot lists a symbol it does not hold", nil)
			}
			grouped[symbol.File] = append(grouped[symbol.File], id)
		}
		offset += len(page.IDs)
		if len(page.IDs) == 0 || offset >= page.Total {
			return grouped, nil
		}
	}
}

// innermostTouched answers the symbols one run of changed lines belongs to: those
// that overlap it, without the ones that merely contain another. A hunk inside a
// method touches the method, not the type around it; a hunk that spans two
// methods touches both.
func innermostTouched(snapshot *hotsnapshot.GraphSnapshot, symbols []hotsnapshot.SymbolID, changed gitdiff.Range) []hotsnapshot.SymbolID {
	type candidate struct {
		id          hotsnapshot.SymbolID
		first, last int
	}
	var overlapping []candidate
	for _, id := range symbols {
		symbol, found := snapshot.Symbol(id)
		if !found || symbol.StartLine == 0 {
			continue
		}
		if changed.Overlaps(int(symbol.StartLine), int(symbol.EndLine)) {
			overlapping = append(overlapping, candidate{id, int(symbol.StartLine), int(symbol.EndLine)})
		}
	}
	var kept []hotsnapshot.SymbolID
	for _, outer := range overlapping {
		containsAnother := false
		for _, inner := range overlapping {
			if inner.id == outer.id {
				continue
			}
			nested := inner.first >= outer.first && inner.last <= outer.last
			smaller := inner.last-inner.first < outer.last-outer.first
			if nested && smaller {
				containsAnother = true
				break
			}
		}
		if !containsAnother {
			kept = append(kept, outer.id)
		}
	}
	return kept
}

func impactRow(snapshot *hotsnapshot.GraphSnapshot, table hotsnapshot.StringTable, id hotsnapshot.SymbolID) (ImpactSymbol, error) {
	symbol, file, _, _, err := symbolReferenceLocation(snapshot, id)
	if err != nil {
		return ImpactSymbol{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid impact metadata", err)
	}
	qualified, qualifiedOK := table.String(symbol.QualifiedName)
	kind, kindOK := table.String(symbol.Kind)
	if !qualifiedOK || !kindOK {
		return ImpactSymbol{}, WrapToolError(CodeSnapshotUnavailable, "active snapshot contains invalid display strings", nil)
	}
	return ImpactSymbol{
		QualifiedName: qualified, Kind: kind, FilePath: file.path,
		StartLine: symbol.StartLine, EndLine: symbol.EndLine, Test: testpath.Is(file.path),
	}, nil
}
