package facts

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidDelta reports a delta that cannot be applied coherently.
var ErrInvalidDelta = errors.New("invalid delta")

// Delta is a change to the canonical model, expressed in file units: an
// incremental index reacts to a file changing, so everything that file
// asserted is withdrawn and restated. See Diff for how one is computed and
// Delta.Validate for what makes one coherent.
type Delta struct {
	// ReplacedFiles are durable file keys whose facts are fully restated by
	// Upsert. Everything anchored on them is withdrawn first.
	ReplacedFiles []string
	// RemovedFiles are durable file keys that disappeared entirely.
	RemovedFiles []string
	// Upsert is the state that must exist once the delta is applied.
	Upsert Set
}

// Empty reports whether the delta would change nothing.
func (delta Delta) Empty() bool {
	return len(delta.ReplacedFiles) == 0 && len(delta.RemovedFiles) == 0 && upsertEmpty(delta.Upsert)
}

func upsertEmpty(set Set) bool {
	return len(set.Repositories) == 0 && len(set.Packages) == 0 && len(set.Files) == 0 &&
		len(set.Symbols) == 0 && len(set.Evidence) == 0 && len(set.Edges) == 0 && len(set.Unresolved) == 0
}

// Validate rejects a delta that cannot be applied coherently.
//
// Upsert is a fragment, not a closed graph, so it is deliberately NOT
// checked with Set.Validate. A delta that replaces or adds one file
// legitimately carries edges whose other endpoint — a caller, a callee, an
// implementer — lives in the part of the graph the delta does not touch.
// Set.Validate demands every edge endpoint resolve inside the very set it
// is checking; applying that rule to Upsert in isolation would reject
// exactly the deltas this type exists to describe, so a dangling-looking
// edge is not, by itself, a defect here.
//
// What must still hold for a fragment is narrower than for a closed graph:
//
//   - every record has the fields Set.Validate would call required, so a
//     malformed entry is never accepted;
//   - no collection holds two entries with the same durable identity —
//     Set.Merge silently keeps the first and drops the rest, which is the
//     wrong behaviour for a delta about to be applied transactionally: a
//     silent drop there would apply something other than what Diff computed;
//   - the references a fragment is never allowed to omit — a package's
//     repository, a file's repository and package, a symbol's file and
//     package, evidence's file, an edge's evidence, an unresolved
//     reference's repository/file/source symbol — resolve inside Upsert.
//     These, unlike an edge's source or target, are never external: Diff
//     always includes a file's repository and package alongside it (design
//     decision 4), and a symbol, evidence entry or unresolved reference
//     never travels without the file that anchors it (design decision 1).
//   - a file is never named in both ReplacedFiles and RemovedFiles — that
//     would ask an applier to both retire and restate the same file inside
//     one transaction.
func (delta Delta) Validate() error {
	replaced := make(map[string]struct{}, len(delta.ReplacedFiles))
	for _, key := range delta.ReplacedFiles {
		if key == "" {
			return fmt.Errorf("%w: replaced file key is empty", ErrInvalidDelta)
		}
		if _, duplicate := replaced[key]; duplicate {
			return fmt.Errorf("%w: duplicate replaced file %q", ErrInvalidDelta, key)
		}
		replaced[key] = struct{}{}
	}

	removed := make(map[string]struct{}, len(delta.RemovedFiles))
	for _, key := range delta.RemovedFiles {
		if key == "" {
			return fmt.Errorf("%w: removed file key is empty", ErrInvalidDelta)
		}
		if _, duplicate := removed[key]; duplicate {
			return fmt.Errorf("%w: duplicate removed file %q", ErrInvalidDelta, key)
		}
		if _, both := replaced[key]; both {
			return fmt.Errorf("%w: file %q is both replaced and removed", ErrInvalidDelta, key)
		}
		removed[key] = struct{}{}
	}

	return validateFragment(delta.Upsert)
}

// validateFragment checks Upsert's internal coherence. See Delta.Validate
// for why this deliberately differs from Set.Validate: every check below
// mirrors one Set.Validate makes, except an edge's SourceKey/TargetKey are
// only checked for being non-empty, never for resolving inside the
// fragment, and every collection additionally gets a duplicate-identity
// check Set.Validate does not bother with (Set.Merge tolerates duplicates
// by silently deduplicating; a delta about to be applied once must not
// contain them to begin with).
func validateFragment(set Set) error {
	repositories := make(map[string]struct{}, len(set.Repositories))
	for _, repository := range set.Repositories {
		if repository.Key == "" || repository.Name == "" || repository.RootPath == "" {
			return fmt.Errorf("%w: repository %q is incomplete", ErrInvalidDelta, repository.Key)
		}
		if _, duplicate := repositories[repository.Key]; duplicate {
			return fmt.Errorf("%w: duplicate repository %q", ErrInvalidDelta, repository.Key)
		}
		repositories[repository.Key] = struct{}{}
	}

	packages := make(map[string]struct{}, len(set.Packages))
	for _, entry := range set.Packages {
		if entry.Key == "" || entry.Name == "" || entry.RootPath == "" {
			return fmt.Errorf("%w: package %q is incomplete", ErrInvalidDelta, entry.Key)
		}
		if !known(entry.RepositoryKey, repositories) {
			return fmt.Errorf("%w: package %q needs repository %q, which the fragment does not carry", ErrInvalidDelta, entry.Key, entry.RepositoryKey)
		}
		if _, duplicate := packages[entry.Key]; duplicate {
			return fmt.Errorf("%w: duplicate package %q", ErrInvalidDelta, entry.Key)
		}
		packages[entry.Key] = struct{}{}
	}

	files := make(map[string]struct{}, len(set.Files))
	for _, file := range set.Files {
		if file.Key == "" || file.Path == "" {
			return fmt.Errorf("%w: file %q is incomplete", ErrInvalidDelta, file.Key)
		}
		if !known(file.RepositoryKey, repositories) {
			return fmt.Errorf("%w: file %q needs repository %q, which the fragment does not carry", ErrInvalidDelta, file.Key, file.RepositoryKey)
		}
		if file.PackageKey != "" && !known(file.PackageKey, packages) {
			return fmt.Errorf("%w: file %q needs package %q, which the fragment does not carry", ErrInvalidDelta, file.Key, file.PackageKey)
		}
		if _, duplicate := files[file.Key]; duplicate {
			return fmt.Errorf("%w: duplicate file %q", ErrInvalidDelta, file.Key)
		}
		files[file.Key] = struct{}{}
	}

	symbols := make(map[string]struct{}, len(set.Symbols))
	for _, symbol := range set.Symbols {
		if symbol.Key == "" || symbol.CanonicalIdentity == "" || symbol.QualifiedName == "" {
			return fmt.Errorf("%w: symbol %q is incomplete", ErrInvalidDelta, symbol.QualifiedName)
		}
		if !known(symbol.FileKey, files) {
			return fmt.Errorf("%w: symbol %q needs file %q, which the fragment does not carry", ErrInvalidDelta, symbol.Key, symbol.FileKey)
		}
		if !known(symbol.PackageKey, packages) {
			return fmt.Errorf("%w: symbol %q needs package %q, which the fragment does not carry", ErrInvalidDelta, symbol.Key, symbol.PackageKey)
		}
		if _, duplicate := symbols[symbol.Key]; duplicate {
			return fmt.Errorf("%w: duplicate symbol %q", ErrInvalidDelta, symbol.Key)
		}
		symbols[symbol.Key] = struct{}{}
	}

	evidence := make(map[string]struct{}, len(set.Evidence))
	for _, entry := range set.Evidence {
		if entry.Key == "" {
			return fmt.Errorf("%w: evidence without key", ErrInvalidDelta)
		}
		if !known(entry.FileKey, files) {
			return fmt.Errorf("%w: evidence %q needs file %q, which the fragment does not carry", ErrInvalidDelta, entry.Key, entry.FileKey)
		}
		if _, duplicate := evidence[entry.Key]; duplicate {
			return fmt.Errorf("%w: duplicate evidence %q", ErrInvalidDelta, entry.Key)
		}
		evidence[entry.Key] = struct{}{}
	}

	edges := make(map[string]struct{}, len(set.Edges))
	for _, edge := range set.Edges {
		if !edge.Kind.Valid() {
			return fmt.Errorf("%w: unknown edge kind %q", ErrInvalidDelta, edge.Kind)
		}
		if edge.SourceKey == "" || edge.TargetKey == "" {
			// Well formed, not resolved: an edge's endpoints may legitimately
			// live outside the fragment (see Delta.Validate above), but an
			// empty key can never be a real one.
			return fmt.Errorf("%w: edge %s has an empty endpoint", ErrInvalidDelta, edge.Kind)
		}
		if edge.Confidence.Exact() && !edge.Provenance.Exact() {
			return fmt.Errorf("%w: edge %s claims %s from %s", ErrInvalidDelta, edge.Kind, edge.Confidence, edge.Provenance)
		}
		if edge.EvidenceKey != "" && !known(edge.EvidenceKey, evidence) {
			return fmt.Errorf("%w: edge %s needs evidence %q, which the fragment does not carry", ErrInvalidDelta, edge.Kind, edge.EvidenceKey)
		}
		identity := edgeIdentity(edge)
		if _, duplicate := edges[identity]; duplicate {
			return fmt.Errorf("%w: duplicate edge %s from %s to %s", ErrInvalidDelta, edge.Kind, edge.SourceKey, edge.TargetKey)
		}
		edges[identity] = struct{}{}
	}

	unresolved := make(map[string]struct{}, len(set.Unresolved))
	for _, entry := range set.Unresolved {
		if entry.Reason == "" {
			return fmt.Errorf("%w: unresolved reference without reason", ErrInvalidDelta)
		}
		if !known(entry.RepositoryKey, repositories) {
			return fmt.Errorf("%w: unresolved reference needs repository %q, which the fragment does not carry", ErrInvalidDelta, entry.RepositoryKey)
		}
		if entry.FileKey != "" && !known(entry.FileKey, files) {
			return fmt.Errorf("%w: unresolved reference needs file %q, which the fragment does not carry", ErrInvalidDelta, entry.FileKey)
		}
		if entry.SourceSymbolKey != "" && !known(entry.SourceSymbolKey, symbols) {
			return fmt.Errorf("%w: unresolved reference needs symbol %q, which the fragment does not carry", ErrInvalidDelta, entry.SourceSymbolKey)
		}
		identity := UnresolvedKey(entry)
		if _, duplicate := unresolved[identity]; duplicate {
			return fmt.Errorf("%w: duplicate unresolved reference %q", ErrInvalidDelta, identity)
		}
		unresolved[identity] = struct{}{}
	}

	return nil
}

// edgeIdentity mirrors the tuple Set.Merge deduplicates edges on.
func edgeIdentity(edge Edge) string {
	return strings.Join([]string{string(edge.Kind), edge.SourceKey, edge.TargetKey, edge.EvidenceKey}, "\x00")
}

// Diff computes the delta that turns previous into next.
//
// Both sets must already be valid (Set.Validate passes). Upsert's contents
// come straight from next, so a next that is not itself a coherent graph —
// a dangling edge, an unknown package — would otherwise propagate silently
// into the delta instead of failing where the actual problem is.
//
// Repository and Package records are monotonic membership metadata in this
// model: a Delta only ever adds or refreshes one (always alongside the
// file(s) that need it, per design decision 4), never withdraws one. That
// mirrors CanonicalMutationResult, which accounts for removed files,
// symbols, evidence and edges but has no notion of a removed package or
// repository. PACKAGE_DEPENDS_ON and MODULE_DEPENDS_ON — genuine package to
// package relationships, not containment — are outside what a file-grained
// Delta tracks at all; CONTAINS_PACKAGE is not (see addNeededContainers and
// edgeAnchor for the two halves of why).
func Diff(previous, next Set) (Delta, error) {
	if err := previous.Validate(); err != nil {
		return Delta{}, fmt.Errorf("%w: previous set: %w", ErrInvalidDelta, err)
	}
	if err := next.Validate(); err != nil {
		return Delta{}, fmt.Errorf("%w: next set: %w", ErrInvalidDelta, err)
	}

	before := cloneSet(previous)
	before.Sort()
	after := cloneSet(next)
	after.Sort()

	beforeIndex := buildSnapshotIndex(before)
	afterIndex := buildSnapshotIndex(after)

	var removedFiles, replacedFiles []string
	upsert := Set{}

	// Walk both file lists in lockstep, like a two way merge: both are
	// sorted by Key (Sort, just above) and Key is unique within a valid Set
	// (Validate rejects duplicates), so this visits every file exactly
	// once, in ascending key order — which is also the order Delta must
	// return ReplacedFiles/RemovedFiles in, so no separate sort is needed
	// afterwards.
	i, j := 0, 0
	for i < len(before.Files) || j < len(after.Files) {
		switch {
		case j >= len(after.Files) || (i < len(before.Files) && before.Files[i].Key < after.Files[j].Key):
			// Present only in before: it disappeared.
			removedFiles = append(removedFiles, before.Files[i].Key)
			i++
		case i >= len(before.Files) || (j < len(after.Files) && after.Files[j].Key < before.Files[i].Key):
			// Present only in after: brand new, nothing to withdraw first.
			fragment, _ := afterIndex.fragment(after.Files[j].Key)
			upsert.Merge(fragment)
			j++
		default:
			// Same key on both sides: compare what each anchors.
			key := before.Files[i].Key
			beforeFragment, _ := beforeIndex.fragment(key)
			afterFragment, _ := afterIndex.fragment(key)
			// Comparing the derived fragment — not File.ContentHash, which
			// is optional (omitempty) and may be absent from either
			// snapshot — is deliberate, and it is what makes the "source
			// unchanged, callee vanished" case resolve without a special
			// case: next already had to pass Validate above, so it cannot
			// contain an edge whose target does not exist. If this file's
			// code refers to a symbol that just disappeared, next simply
			// omits that edge, which means the file's own anchored edge set
			// shrank between before and after even though its bytes never
			// changed. That shrink is exactly what this comparison catches,
			// so the file is correctly replaced with a restatement that no
			// longer carries the dead edge. Diff does not additionally special
			// case a vanished target: retiring that target's own file (via
			// RemovedFiles/ReplacedFiles, computed the same way) is what
			// makes the applier cascade the edge away on both ends; this
			// comparison is what guarantees the source file's restated facts
			// agree with that outcome instead of a stale edge lingering
			// until the target's retirement happens to clean it up too.
			if !fragmentsEqual(beforeFragment, afterFragment) {
				replacedFiles = append(replacedFiles, key)
				upsert.Merge(afterFragment)
			}
			i++
			j++
		}
	}

	addNeededContainers(&upsert, afterIndex)
	upsert.Sort()

	delta := Delta{ReplacedFiles: replacedFiles, RemovedFiles: removedFiles, Upsert: upsert}
	if err := delta.Validate(); err != nil {
		return Delta{}, err
	}
	return delta, nil
}

// fragmentsEqual compares two per-file fragments built by snapshotIndex.fragment.
// Every field that can appear in one (File, Symbol, Evidence, Edge,
// UnresolvedReference) is a plain value type, so a direct element-wise
// comparison is both correct and cheaper than reflection.
func fragmentsEqual(before, after Set) bool {
	return equalSlice(before.Files, after.Files) &&
		equalSlice(before.Symbols, after.Symbols) &&
		equalSlice(before.Evidence, after.Evidence) &&
		equalSlice(before.Edges, after.Edges) &&
		equalSlice(before.Unresolved, after.Unresolved)
}

func equalSlice[T comparable](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// addNeededContainers pulls in, from after, the Repository and Package
// records that every file, symbol, evidence entry and unresolved reference
// already in upsert names — plus, transitively, the repository each pulled
// package itself belongs to — so Upsert never claims a foreign key it does
// not also carry the row for. It always adds them, whether or not the
// repository/package is itself new: an unchanged Repository/Package
// travelling alongside a genuinely new or replaced file is a harmless
// create-or-update for the applier, and Upsert must be self contained
// regardless (see Delta.Validate).
//
// It also pulls in the CONTAINS_PACKAGE edge of every package it adds, if
// after has one. That edge is not anchored on any file (see edgeAnchor), so
// nothing else would ever carry it into Upsert — but skipping it would
// leave a newly introduced package floating, unreachable from its own
// repository, which defeats the point of decision 4 bringing the package
// in "para existir" in the first place. PACKAGE_DEPENDS_ON and
// MODULE_DEPENDS_ON get no such treatment: unlike a containment edge, they
// assert a relationship between two independently existing packages, not
// one package's membership in another — genuinely outside what a
// file-grained Delta tracks, same as Diff's doc comment explains.
func addNeededContainers(upsert *Set, after snapshotIndex) {
	repositoryKeys := make(map[string]struct{})
	packageKeys := make(map[string]struct{})

	addKeys := func(repositoryKey, packageKey string) {
		if repositoryKey != "" {
			repositoryKeys[repositoryKey] = struct{}{}
		}
		if packageKey != "" {
			packageKeys[packageKey] = struct{}{}
		}
	}
	for _, file := range upsert.Files {
		addKeys(file.RepositoryKey, file.PackageKey)
	}
	for _, symbol := range upsert.Symbols {
		addKeys(symbol.RepositoryKey, symbol.PackageKey)
	}
	for _, entry := range upsert.Evidence {
		addKeys(entry.RepositoryKey, "")
	}
	for _, entry := range upsert.Unresolved {
		addKeys(entry.RepositoryKey, "")
	}
	for packageKey := range packageKeys {
		if entry, ok := after.packagesByKey[packageKey]; ok {
			addKeys(entry.RepositoryKey, "")
		}
	}

	for key := range repositoryKeys {
		if repository, ok := after.repositoriesByKey[key]; ok {
			upsert.Repositories = append(upsert.Repositories, repository)
		}
	}
	for key := range packageKeys {
		if entry, ok := after.packagesByKey[key]; ok {
			upsert.Packages = append(upsert.Packages, entry)
		}
		if edge, ok := after.containsPackageEdge[key]; ok {
			upsert.Edges = append(upsert.Edges, edge)
		}
	}
}

// cloneSet copies every collection so Sort, which permutes slices in place,
// never mutates a caller's Set.
func cloneSet(set Set) Set {
	return Set{
		Repositories: append([]Repository(nil), set.Repositories...),
		Packages:     append([]Package(nil), set.Packages...),
		Files:        append([]File(nil), set.Files...),
		Symbols:      append([]Symbol(nil), set.Symbols...),
		Evidence:     append([]Evidence(nil), set.Evidence...),
		Edges:        append([]Edge(nil), set.Edges...),
		Unresolved:   append([]UnresolvedReference(nil), set.Unresolved...),
	}
}

// snapshotIndex groups one already-sorted Set by the file each fact anchors
// on, so Diff can compare and assemble fragments file by file. It also
// keeps a package-granularity grouping of exactly one kind — the
// CONTAINS_PACKAGE edge naming each package — for addNeededContainers; see
// there for why.
type snapshotIndex struct {
	filesByKey          map[string]File
	symbolFile          map[string]string // Symbol.Key -> its FileKey
	symbolsByFile       map[string][]Symbol
	evidenceByFile      map[string][]Evidence
	edgesByFile         map[string][]Edge
	unresolvedByFile    map[string][]UnresolvedReference
	repositoriesByKey   map[string]Repository
	packagesByKey       map[string]Package
	containsPackageEdge map[string]Edge   // Package.Key -> its CONTAINS_PACKAGE edge
	evidenceFile        map[string]string // Evidence.Key -> its FileKey
}

// buildSnapshotIndex indexes set, which must already be sorted (Set.Sort):
// grouping only filters, so each group inherits the parent's deterministic
// order, and fragment comparisons stay order stable without re-sorting.
func buildSnapshotIndex(set Set) snapshotIndex {
	index := snapshotIndex{
		filesByKey:          make(map[string]File, len(set.Files)),
		symbolFile:          make(map[string]string, len(set.Symbols)),
		symbolsByFile:       make(map[string][]Symbol, len(set.Files)),
		evidenceByFile:      make(map[string][]Evidence, len(set.Files)),
		edgesByFile:         make(map[string][]Edge, len(set.Files)),
		unresolvedByFile:    make(map[string][]UnresolvedReference, len(set.Files)),
		repositoriesByKey:   make(map[string]Repository, len(set.Repositories)),
		packagesByKey:       make(map[string]Package, len(set.Packages)),
		containsPackageEdge: make(map[string]Edge, len(set.Packages)),
		evidenceFile:        make(map[string]string, len(set.Evidence)),
	}
	for _, repository := range set.Repositories {
		index.repositoriesByKey[repository.Key] = repository
	}
	for _, entry := range set.Packages {
		index.packagesByKey[entry.Key] = entry
	}
	for _, file := range set.Files {
		index.filesByKey[file.Key] = file
	}
	for _, symbol := range set.Symbols {
		index.symbolFile[symbol.Key] = symbol.FileKey
		index.symbolsByFile[symbol.FileKey] = append(index.symbolsByFile[symbol.FileKey], symbol)
	}
	for _, entry := range set.Evidence {
		index.evidenceFile[entry.Key] = entry.FileKey
		index.evidenceByFile[entry.FileKey] = append(index.evidenceByFile[entry.FileKey], entry)
	}
	for _, edge := range set.Edges {
		if edge.Kind == ContainsPackage {
			index.containsPackageEdge[edge.TargetKey] = edge
		}
		if anchor, ok := edgeAnchor(edge, index.filesByKey, index.symbolFile, index.evidenceFile); ok {
			index.edgesByFile[anchor] = append(index.edgesByFile[anchor], edge)
		}
	}
	for _, entry := range set.Unresolved {
		if entry.FileKey != "" {
			index.unresolvedByFile[entry.FileKey] = append(index.unresolvedByFile[entry.FileKey], entry)
		}
	}
	return index
}

// fragment returns everything anchored on fileKey: the File record itself
// plus every symbol, evidence entry, anchored edge and file scoped
// unresolved reference that names it. See edgeAnchor for which single file
// an edge anchors on.
func (index snapshotIndex) fragment(fileKey string) (Set, bool) {
	file, present := index.filesByKey[fileKey]
	if !present {
		return Set{}, false
	}
	return Set{
		Files:      []File{file},
		Symbols:    index.symbolsByFile[fileKey],
		Evidence:   index.evidenceByFile[fileKey],
		Edges:      index.edgesByFile[fileKey],
		Unresolved: index.unresolvedByFile[fileKey],
	}, true
}

// edgeAnchor names the one file an edge is asserted by: the file whose
// indexing pass produced it. That is the file owning the edge's source —
// DEFINES' source is the file itself; every symbol-to-symbol relation's
// source is a symbol, anchored through that symbol's own file (see
// internal/rebuild's symbolRelationKinds for the full list: REFERENCES,
// CALLS_DIRECT, PASSES_AS_CALLBACK, ASSIGNS_FUNCTION, RETURNS_FUNCTION,
// TYPE_USES, IMPLEMENTS, EXTENDS, EMBEDS, OVERRIDES, IMPORTS_SYMBOL,
// EXPORTS, REEXPORTS). CONTAINS_FILE inverts the pattern — its source is
// the containing package, not a file — so a file-typed target anchors too:
// the file's own existence is what brings that edge into being. Only one of
// the two ever applies for a given edge, matching a single anchoring file
// rather than both of an edge's endpoints.
//
// PACKAGE_DEPENDS_ON and MODULE_DEPENDS_ON own no file on either end, but
// they are still asserted by one file: the one where the import was
// observed, which is exactly what their evidence names. Anchoring them
// there is not a special case, it is the same rule reached through the
// evidence instead of through an endpoint — and it is load bearing. Without
// it a package edge survives the retirement of the very file that produced
// it, keeping a reference to evidence that no longer exists: a ghost edge,
// which the incremental gate forbids and `missing_evidence_file` catches.
//
// CONTAINS_PACKAGE is the one edge with neither a file endpoint nor
// evidence; it is grouped by the package it names instead
// (buildSnapshotIndex's containsPackageEdge, consumed by
// addNeededContainers).
func edgeAnchor(edge Edge, filesByKey map[string]File, symbolFile, evidenceFile map[string]string) (string, bool) {
	if _, isFile := filesByKey[edge.SourceKey]; isFile {
		return edge.SourceKey, true
	}
	if fileKey, isSymbol := symbolFile[edge.SourceKey]; isSymbol {
		return fileKey, true
	}
	if _, isFile := filesByKey[edge.TargetKey]; isFile {
		return edge.TargetKey, true
	}
	if edge.EvidenceKey != "" {
		if fileKey, observed := evidenceFile[edge.EvidenceKey]; observed {
			return fileKey, true
		}
	}
	return "", false
}
