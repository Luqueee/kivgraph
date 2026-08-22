package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// TypeScriptWireVersion is the version of the `ts-facts-v4` payload.
const TypeScriptWireVersion = 4

// TypeScriptPayload is the fact payload the worker emits for one repository.
//
// The worker reports identity components and positions; it never computes a
// key. Deriving keys on a single side is what keeps one symbol from getting
// two identities when a consumer and its provider are indexed separately.
type TypeScriptPayload struct {
	Version      int                    `json:"version"`
	Repository   TypeScriptRepository   `json:"repository"`
	Package      *TypeScriptPackage     `json:"package"`
	Files        []string               `json:"files"`
	Symbols      []TypeScriptSymbol     `json:"symbols"`
	References   []TypeScriptReference  `json:"references"`
	Imports      []TypeScriptImport     `json:"imports"`
	Exports      []TypeScriptExport     `json:"exports"`
	Extends      []TypeScriptExtends    `json:"extends"`
	Dependencies []TypeScriptDependency `json:"dependencies"`
	Unresolved   []TypeScriptUnresolved `json:"unresolved"`
}

// TypeScriptRepository names the repository the payload belongs to.
type TypeScriptRepository struct {
	Name string `json:"name"`
}

// TypeScriptPackage is the npm package of the repository, with repository
// relative paths so a recorded payload stays portable.
type TypeScriptPackage struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	RootPath     string `json:"rootPath"`
	ManifestPath string `json:"manifestPath"`
}

// TypeScriptSymbol is one local declaration reported by the worker.
type TypeScriptSymbol struct {
	File          string `json:"file"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualifiedName"`
	Kind          string `json:"kind"`
	Exported      bool   `json:"exported"`
	Signature     string `json:"signature"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	Start         int    `json:"start"`
	End           int    `json:"end"`
}

// TypeScriptReference is one classified local use.
type TypeScriptReference struct {
	File                string `json:"file"`
	Kind                string `json:"kind"`
	SourceQualifiedName string `json:"sourceQualifiedName"`
	TargetQualifiedName string `json:"targetQualifiedName"`
	TargetFile          string `json:"targetFile"`
	StartLine           int    `json:"startLine"`
	Start               int    `json:"start"`
	End                 int    `json:"end"`
	Text                string `json:"text"`
}

// TypeScriptImportTarget is the provider declaration an import binding
// reaches, described exactly as the provider indexes it when it normalises
// itself: computed from the provider's own source, at the position the
// provider-source bridge resolved. Never derived from the consumer's `.d.ts`
// text, which is not guaranteed to restate the source.
//
// A `TypeScriptExport` that crosses into another repository reuses this
// exact shape: a re-exported symbol needs the same provider-source proof an
// imported one does.
type TypeScriptImportTarget struct {
	Repository    string `json:"repository"`
	Package       string `json:"package"`
	QualifiedName string `json:"qualifiedName"`
	Kind          string `json:"kind"`
	Signature     string `json:"signature"`
	File          string `json:"file"`
	StartLine     int    `json:"startLine"`
	// Source names how the provider source position was reached. An empty
	// value means a declaration map, which is what every payload recorded
	// before the provider-export path existed carries.
	Source string `json:"source,omitempty"`
}

const (
	// TypeScriptIdentityDeclarationMap marks a target the artifact's own
	// `.d.ts.map` placed inside the provider's source.
	TypeScriptIdentityDeclarationMap = "DECLARATION_MAP"
	// TypeScriptIdentityProviderExport marks a target the provider's own
	// checker named, as the export of a source file the provider's project
	// roots mapped its declaration artifact to. The position is exact and
	// comes from the compiler that owns the code, but the artifact-to-source
	// step rests on the provider's build configuration rather than on a map
	// it emitted, so the edge is graded ExactPackageMapped. See ADR 0038.
	TypeScriptIdentityProviderExport = "PROVIDER_EXPORT"
)

// crossRepositoryGrade grades a cross-repository target by the evidence that
// placed it in the provider's source. Both grades are exact; they differ in
// what a consumer of the graph has to trust, and the graph says which.
func crossRepositoryGrade(target *TypeScriptImportTarget) (Confidence, Provenance) {
	if target.Source == TypeScriptIdentityProviderExport {
		return ExactPackageMapped, TypeScriptProjectReference
	}
	return ExactTypechecked, TypeScriptChecker
}

// TypeScriptImport is one import binding of the consumer. The worker already
// turns the binding itself into an "import" kind TypeScriptSymbol, so
// QualifiedName here names an entry already present in Symbols; Target is
// the provider declaration it reaches, or nil when that declaration is not
// exactly known.
type TypeScriptImport struct {
	File             string                  `json:"file"`
	QualifiedName    string                  `json:"qualifiedName"`
	Start            int                     `json:"start"`
	End              int                     `json:"end"`
	StartLine        int                     `json:"startLine"`
	Text             string                  `json:"text"`
	RequestedPackage string                  `json:"requestedPackage"`
	RequestedSymbol  string                  `json:"requestedSymbol"`
	Target           *TypeScriptImportTarget `json:"target"`
	Reason           string                  `json:"reason"`
	Detail           string                  `json:"detail"`
}

// TypeScriptExport is one export or re-export binding of the file: the
// worker turns the public name itself into an "export" kind TypeScriptSymbol,
// so QualifiedName here names an entry already present in Symbols. It
// resolves exactly one of two ways:
//
//   - TargetQualifiedName/TargetFile name a declaration already present in
//     this same payload's Symbols — a direct export, or a re-export whose
//     `from` clause stays inside this repository.
//   - Target carries the provider-source identity of a declaration in
//     another repository — a re-export whose `from` clause names a package.
//     Target is nil when that identity is not exactly known, exactly like an
//     import without one: the binding still becomes an UnresolvedReference,
//     never a guessed edge.
type TypeScriptExport struct {
	File                string                  `json:"file"`
	Kind                string                  `json:"kind"`
	QualifiedName       string                  `json:"qualifiedName"`
	Start               int                     `json:"start"`
	End                 int                     `json:"end"`
	StartLine           int                     `json:"startLine"`
	Text                string                  `json:"text"`
	TargetQualifiedName string                  `json:"targetQualifiedName"`
	TargetFile          string                  `json:"targetFile"`
	Target              *TypeScriptImportTarget `json:"target"`
	RequestedPackage    string                  `json:"requestedPackage"`
	RequestedSymbol     string                  `json:"requestedSymbol"`
	Reason              string                  `json:"reason"`
	Detail              string                  `json:"detail"`
}

// TypeScriptExtends is one base of a `class ... extends` or `interface ...
// extends` clause: QualifiedName names the class or interface declaring the
// clause, already present in Symbols — a heritage clause introduces no
// binding of its own, unlike an import or an export. It resolves exactly
// one of two ways, exactly like TypeScriptExport:
//
//   - TargetQualifiedName/TargetFile name a declaration already present in
//     this same payload's Symbols — a base declared in this repository.
//   - Target carries the provider-source identity of a declaration in
//     another repository — a base introduced by an import whose module
//     specifier names a package. Target is nil when that identity is not
//     exactly known, exactly like an import without one: the base still
//     becomes an UnresolvedReference, never a guessed edge.
//
// TypeScript's `implements` never produces one of these: see the worker's
// extends-resolver.ts for why.
type TypeScriptExtends struct {
	File                string                  `json:"file"`
	QualifiedName       string                  `json:"qualifiedName"`
	Start               int                     `json:"start"`
	End                 int                     `json:"end"`
	StartLine           int                     `json:"startLine"`
	Text                string                  `json:"text"`
	TargetQualifiedName string                  `json:"targetQualifiedName"`
	TargetFile          string                  `json:"targetFile"`
	Target              *TypeScriptImportTarget `json:"target"`
	RequestedPackage    string                  `json:"requestedPackage"`
	RequestedSymbol     string                  `json:"requestedSymbol"`
	Reason              string                  `json:"reason"`
	Detail              string                  `json:"detail"`
}

// TypeScriptDependency is one real dependency from this repository's own
// package to another package the checker proved this repository imports
// from — never a nominal `package.json` entry, which may list a package
// nothing actually imports. TypeScript has no module concept distinct from
// its package, so this alone never becomes MODULE_DEPENDS_ON; only a Go
// package crossing a module boundary does.
type TypeScriptDependency struct {
	// Repository and Package name the provider exactly as PackageKey derives
	// it: the repository Kivgraph indexes it under, and its own package name.
	Repository string `json:"repository"`
	Package    string `json:"package"`
	// File, Specifier, Start, End and StartLine are one deterministic import
	// occurrence proving the dependency — never every occurrence, since a
	// single edge needs only one witness.
	File      string `json:"file"`
	Specifier string `json:"specifier"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	StartLine int    `json:"startLine"`
}

// TypeScriptUnresolved is one classified failure of the worker.
type TypeScriptUnresolved struct {
	File             string `json:"file"`
	Reason           string `json:"reason"`
	RequestedPackage string `json:"requestedPackage"`
	RequestedSymbol  string `json:"requestedSymbol"`
	Detail           string `json:"detail"`
	Start            int    `json:"start"`
}

// TypeScriptReport records what normalisation could not keep.
type TypeScriptReport struct {
	// EdgesWithoutSource counts uses whose enclosing symbol is named but not
	// in this payload. A use with no enclosing declaration at all is owned by
	// its module instead, and counted below.
	EdgesWithoutSource int
	// EdgesOwnedByModule counts uses whose owner is the file's own scope: a
	// top level statement, or a call inside an anonymous function passed as an
	// argument. Before the module symbol existed these were dropped, which
	// erased every call a test file makes.
	EdgesOwnedByModule int
	// EdgesWithoutTarget counts uses whose target is not in this payload.
	EdgesWithoutTarget int
	// ImportsWithoutTarget counts import bindings whose provider declaration
	// is not exactly known: no declaration map reached it, or the reached
	// declaration carries no class or no signature.
	ImportsWithoutTarget int
	// ExportsWithoutTarget counts export/re-export bindings whose declaration
	// is not exactly known: neither a local declaration in this payload nor a
	// provider declaration reached with a class and a signature.
	ExportsWithoutTarget int
	// ExtendsWithoutTarget counts extends bases whose declaration is not
	// exactly known: neither a local declaration in this payload nor a
	// provider declaration reached with a class and a signature.
	ExtendsWithoutTarget int
	// FactsOutsideRepository counts facts dropped because the file holding
	// them is not inside the repository being indexed. A consumer resolves an
	// import to a provider's built declaration, so the worker reports a path
	// that leaves the consumer's tree -- `../../libraries/library-shared/dist`
	// and the like. Keying such a file under the consumer produced a row
	// whose path escapes its own repository and which therefore cannot be
	// handed back to any tool, which is LUQUE-2011.
	//
	// The fact is retired rather than re-attributed. Naming the owning
	// repository is not enough to build a complete row: a File belongs to a
	// Package, and this payload never says which package of the provider
	// holds the file. An incomplete row would also be dangerous, because
	// MergeAll keeps the first row for a key and drops later ones without
	// comparing, so a package-less row could silently beat a complete one.
	FactsOutsideRepository int
}

// UnresolvedFileOutsideRepository is retained when a payload reports a file
// whose repository-relative path climbs out of its own repository. A consumer
// resolving an import into a workspace sibling's built declarations gets
// `../../libraries/library-shared/dist/...`, and that names a file the sibling
// owns.
//
// The relation between the two repositories does not depend on this file and is
// not affected: an import binding's target identity is built from the
// provider's own repository and package, so the edge is byte identical to the
// key the provider assigns its own declaration. What is retired here are the
// uses whose **source** file is the provider's output -- facts about the
// provider, which the provider's own pass is the one to report.
const UnresolvedFileOutsideRepository = "FILE_OUTSIDE_REPOSITORY"

// escapesRepository reports whether a repository-relative path leaves its own
// repository. Cleaning first is what makes `src/../../x` and `../x` the same
// answer, and an absolute path is outside by definition: neither can be joined
// onto a repository root to give a path a reader could open.
func escapesRepository(file string) bool {
	if path.IsAbs(file) {
		return true
	}
	cleaned := path.Clean(file)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

// DecodeTypeScriptPayload parses a `ts-facts-v4` document.
func DecodeTypeScriptPayload(data []byte) (TypeScriptPayload, error) {
	var payload TypeScriptPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return TypeScriptPayload{}, fmt.Errorf("decode typescript facts: %w", err)
	}
	if payload.Version != TypeScriptWireVersion {
		return TypeScriptPayload{}, fmt.Errorf("%w: unsupported typescript facts version %d",
			ErrInvalidFacts, payload.Version)
	}
	return payload, nil
}

// NormalizeTypeScript converts a worker payload into the canonical model.
//
// repository is the registered repository the payload was produced from. The
// payload carries only repository-relative paths, and the worker cannot
// observe the git state of the tree it read: both come from the caller, the
// same way NormalizeGo and NormalizeRust take them.
func NormalizeTypeScript(
	ctx context.Context,
	payload TypeScriptPayload,
	repository workspace.Repository,
) (Set, TypeScriptReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Set{}, TypeScriptReport{}, err
	}
	name := strings.TrimSpace(payload.Repository.Name)
	if name == "" {
		return Set{}, TypeScriptReport{}, fmt.Errorf("%w: payload has no repository name", ErrInvalidFacts)
	}
	rootPath := repository.RealPath
	if strings.TrimSpace(rootPath) == "" {
		return Set{}, TypeScriptReport{}, fmt.Errorf("%w: repository %q has no root path", ErrInvalidFacts, name)
	}
	if payload.Package == nil {
		return Set{}, TypeScriptReport{}, fmt.Errorf("%w: repository %q declares no package", ErrInvalidFacts, name)
	}

	repositoryKey := RepositoryKey(name)
	packageKey := PackageKey(LanguageTypeScript, name, payload.Package.Name)
	set := Set{
		Repositories: []Repository{{
			Key:       repositoryKey,
			Name:      name,
			RootPath:  rootPath,
			Languages: []Language{LanguageTypeScript},
		}},
		Packages: []Package{{
			Key:           packageKey,
			RepositoryKey: repositoryKey,
			Language:      LanguageTypeScript,
			Name:          payload.Package.Name,
			Version:       payload.Package.Version,
			RootPath:      path.Join(rootPath, payload.Package.RootPath),
			ManifestPath:  path.Join(rootPath, payload.Package.ManifestPath),
		}},
		Edges: []Edge{{
			Kind:       ContainsPackage,
			SourceKey:  repositoryKey,
			TargetKey:  packageKey,
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		}},
	}

	// outside holds the raw paths of files this pass refuses to claim. A
	// repository-relative path that climbs out of its own tree names a file
	// another repository owns, and keying it here would publish a row nobody
	// can address. The loss is counted and retained with its reason, the way
	// the Go loader already does it -- never hidden, and never re-attributed
	// to a repository whose package this payload cannot name.
	report := TypeScriptReport{}
	files := make(map[string]struct{}, len(payload.Files))
	outside := make(map[string]struct{})
	for _, file := range payload.Files {
		if escapesRepository(file) {
			if _, seen := outside[file]; seen {
				continue
			}
			outside[file] = struct{}{}
			// The file is one retained gap; the facts it held are counted
			// one by one where each is dropped, the way Go does it.
			//
			// RequestedPackage is the package that was being indexed when the
			// path appeared, which is the only package this payload can name
			// truthfully: the provider's own package is exactly what a
			// consumer payload never says, and inventing it is what this
			// retirement exists to avoid.
			set.Unresolved = append(set.Unresolved, UnresolvedReference{
				RepositoryKey:    repositoryKey,
				Language:         LanguageTypeScript,
				RequestedPackage: payload.Package.Name,
				RequestedSymbol:  path.Base(file),
				Reason:           string(UnresolvedFileOutsideRepository),
				Detail:           file,
			})
			continue
		}
		fileKey := FileKey(name, file)
		if _, exists := files[fileKey]; exists {
			continue
		}
		files[fileKey] = struct{}{}
		set.Files = append(set.Files, File{
			Key:           fileKey,
			RepositoryKey: repositoryKey,
			PackageKey:    packageKey,
			Path:          file,
			Language:      LanguageTypeScript,
		})
		set.Edges = append(set.Edges, Edge{
			Kind:       ContainsFile,
			SourceKey:  packageKey,
			TargetKey:  fileKey,
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		})
	}

	symbolKeys := make(map[string]string, len(payload.Symbols))
	for _, symbol := range payload.Symbols {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		if _, retired := outside[symbol.File]; retired {
			// Its file belongs to another repository's tree and was refused
			// above, so this declaration is the provider's to publish.
			report.FactsOutsideRepository++
			continue
		}
		fileKey := FileKey(name, symbol.File)
		if _, exists := files[fileKey]; !exists {
			return Set{}, TypeScriptReport{}, fmt.Errorf("%w: symbol %q lives in unreported file %q",
				ErrInvalidFacts, symbol.QualifiedName, symbol.File)
		}
		identity := typeScriptSymbolIdentity(name, payload.Package.Name, symbol.File, symbol.QualifiedName, symbol.Kind, symbol.Signature)
		canonical, err := identity.Canonical()
		if err != nil {
			return Set{}, TypeScriptReport{}, fmt.Errorf("symbol %q identity: %w", symbol.QualifiedName, err)
		}
		key, err := identity.Key()
		if err != nil {
			return Set{}, TypeScriptReport{}, fmt.Errorf("symbol %q key: %w", symbol.QualifiedName, err)
		}
		set.Symbols = append(set.Symbols, Symbol{
			Key:               string(key),
			CanonicalIdentity: canonical,
			RepositoryKey:     repositoryKey,
			PackageKey:        packageKey,
			FileKey:           fileKey,
			Language:          LanguageTypeScript,
			Name:              symbol.Name,
			QualifiedName:     symbol.QualifiedName,
			Kind:              symbol.Kind,
			Exported:          symbol.Exported,
			Signature:         symbol.Signature,
			Start:             Position{Line: symbol.StartLine, Offset: symbol.Start},
			End:               Position{Line: symbol.EndLine, Offset: symbol.End},
		})
		symbolKeys[symbol.File+"\x00"+symbol.QualifiedName] = string(key)
		set.Edges = append(set.Edges, Edge{
			Kind:       Defines,
			SourceKey:  fileKey,
			TargetKey:  string(key),
			Confidence: StructuralCertain,
			Provenance: TypeScriptChecker,
		})
	}

	// moduleSymbolKeys holds the synthetic module symbol of a file, created the
	// first time a use needs it. Creating one per file up front would add a
	// symbol to every file in the corpus to serve the few that have a use with
	// no narrower owner; the payload's order is fixed, so creating them lazily
	// is just as deterministic.
	moduleSymbolKeys := make(map[string]string, 0)
	moduleSymbolFor := func(file string) (string, error) {
		if existing, ok := moduleSymbolKeys[file]; ok {
			return existing, nil
		}
		fileKey := FileKey(name, file)
		if _, known := files[fileKey]; !known {
			return "", nil
		}
		qualifiedName := moduleQualifiedName(file)
		identity := typeScriptSymbolIdentity(name, payload.Package.Name, file,
			qualifiedName, ModuleSymbolKind, "")
		canonical, err := identity.Canonical()
		if err != nil {
			return "", fmt.Errorf("module symbol %q identity: %w", file, err)
		}
		key, err := identity.Key()
		if err != nil {
			return "", fmt.Errorf("module symbol %q key: %w", file, err)
		}
		set.Symbols = append(set.Symbols, Symbol{
			Key:               string(key),
			CanonicalIdentity: canonical,
			RepositoryKey:     repositoryKey,
			PackageKey:        packageKey,
			FileKey:           fileKey,
			Language:          LanguageTypeScript,
			Name:              moduleSymbolName(file),
			QualifiedName:     qualifiedName,
			Kind:              ModuleSymbolKind,
			Exported:          false,
			Start:             Position{Line: 1},
		})
		set.Edges = append(set.Edges, Edge{
			Kind:      Defines,
			SourceKey: fileKey,
			TargetKey: string(key),
			// The scope exists because the file does; the checker declared
			// nothing here, so it is not credited.
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		})
		moduleSymbolKeys[file] = string(key)
		return string(key), nil
	}

	for _, reference := range payload.References {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		if _, retired := outside[reference.File]; retired {
			report.FactsOutsideRepository++
			continue
		}
		sourceKey, hasSource := symbolKeys[reference.File+"\x00"+reference.SourceQualifiedName]
		if !hasSource && reference.SourceQualifiedName == "" {
			// The use has no enclosing declaration, not an unknown one: the
			// worker reports an empty source exactly then. Its owner is the
			// file's own scope.
			moduleKey, err := moduleSymbolFor(reference.File)
			if err != nil {
				return Set{}, TypeScriptReport{}, err
			}
			if moduleKey != "" {
				sourceKey, hasSource = moduleKey, true
				report.EdgesOwnedByModule++
			}
		}
		if !hasSource {
			report.EdgesWithoutSource++
			continue
		}
		targetKey, hasTarget := symbolKeys[reference.TargetFile+"\x00"+reference.TargetQualifiedName]
		if !hasTarget {
			report.EdgesWithoutTarget++
			continue
		}
		fileKey := FileKey(name, reference.File)
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, reference.Start, reference.End),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start:         Position{Line: reference.StartLine, Offset: reference.Start},
			End:           Position{Line: reference.StartLine, Offset: reference.End},
			Text:          reference.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, Edge{
			Kind:        EdgeKind(reference.Kind),
			SourceKey:   sourceKey,
			TargetKey:   targetKey,
			Confidence:  ExactTypechecked,
			Provenance:  TypeScriptChecker,
			EvidenceKey: evidence.Key,
		})
	}

	for _, imp := range payload.Imports {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		if _, retired := outside[imp.File]; retired {
			report.FactsOutsideRepository++
			continue
		}
		sourceKey, hasSource := symbolKeys[imp.File+"\x00"+imp.QualifiedName]
		if !hasSource {
			// The worker always turns an import binding into an "import"
			// kind symbol before this payload is built, so a miss here is
			// the payload contradicting itself, not an ordinary unresolved
			// import. Mirrors the source handling of the References loop.
			report.EdgesWithoutSource++
			continue
		}
		fileKey := FileKey(name, imp.File)
		target := imp.Target
		if target == nil || strings.TrimSpace(target.Kind) == "" || strings.TrimSpace(target.Signature) == "" || strings.TrimSpace(target.File) == "" {
			// No declaration map reached an exact provider declaration, or it
			// reached one whose module, class or signature is unclassified.
			// Either way the provider's stable key cannot be derived, so this
			// is a dropped edge recorded as an unresolved reference, never a
			// guess: LUQUE-0907 forbids inferring kind or signature.
			set.Unresolved = append(set.Unresolved, UnresolvedReference{
				RepositoryKey:    repositoryKey,
				FileKey:          fileKey,
				Language:         LanguageTypeScript,
				SourceSymbolKey:  sourceKey,
				RequestedPackage: imp.RequestedPackage,
				RequestedSymbol:  imp.RequestedSymbol,
				Reason:           imp.Reason,
				Detail:           imp.Detail,
				Start:            Position{Line: imp.StartLine, Offset: imp.Start},
			})
			report.ImportsWithoutTarget++
			continue
		}

		// Build the target identity with the exact function that computes a
		// provider's own symbol identity when it normalises itself, over the
		// class and signature read from the provider's source at the
		// declaration map position. Same code over the same bytes yields the
		// same key by construction, so this consumer-derived key is byte
		// identical to the key the provider assigns its own declaration.
		targetIdentity := typeScriptSymbolIdentity(target.Repository, target.Package, target.File, target.QualifiedName, target.Kind, target.Signature)
		targetKey, err := targetIdentity.Key()
		if err != nil {
			return Set{}, TypeScriptReport{}, fmt.Errorf("import %q target identity: %w", imp.QualifiedName, err)
		}

		evidence := Evidence{
			Key:           EvidenceKey(fileKey, imp.Start, imp.End),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start:         Position{Line: imp.StartLine, Offset: imp.Start},
			End:           Position{Line: imp.StartLine, Offset: imp.End},
			Text:          imp.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		// TargetKey names a symbol the PROVIDER repository normalises, not
		// one in this payload's own Symbols: this Set alone will fail
		// Validate() with a dangling edge, by design. The edge only becomes
		// valid once the caller merges the provider's Set in with
		// Set.Merge, exactly like Go's cross-package edges already do.
		confidence, provenance := crossRepositoryGrade(target)
		set.Edges = append(set.Edges, Edge{
			Kind:        ImportsSymbol,
			SourceKey:   sourceKey,
			TargetKey:   string(targetKey),
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
	}

	for _, exp := range payload.Exports {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		if _, retired := outside[exp.File]; retired {
			report.FactsOutsideRepository++
			continue
		}
		kind := EdgeKind(exp.Kind)
		if kind != Exports && kind != Reexports {
			return Set{}, TypeScriptReport{}, fmt.Errorf("%w: export %q has unknown kind %q",
				ErrInvalidFacts, exp.QualifiedName, exp.Kind)
		}
		sourceKey, hasSource := symbolKeys[exp.File+"\x00"+exp.QualifiedName]
		if !hasSource {
			// Every export/re-export binding is already an "export" kind
			// symbol before this payload is built; a miss here is the
			// payload contradicting itself, exactly like the Imports loop.
			report.EdgesWithoutSource++
			continue
		}
		fileKey := FileKey(name, exp.File)

		var targetKey string
		// A local target is the checker's own resolution inside this
		// payload; only a cross-repository one is graded by how the
		// provider's source was reached.
		confidence, provenance := ExactTypechecked, TypeScriptChecker
		switch {
		case exp.TargetQualifiedName != "" && exp.TargetFile != "":
			// A local target: the declaration already lives in this same
			// payload's Symbols, exactly like a References target.
			key, hasTarget := symbolKeys[exp.TargetFile+"\x00"+exp.TargetQualifiedName]
			if !hasTarget {
				report.EdgesWithoutTarget++
				continue
			}
			targetKey = key
		case exp.Target != nil && strings.TrimSpace(exp.Target.Kind) != "" && strings.TrimSpace(exp.Target.Signature) != "" && strings.TrimSpace(exp.Target.File) != "":
			// A cross-repository target, proven the exact same way an
			// IMPORTS_SYMBOL target is: the provider's own source, read at
			// the position the provider-source bridge resolved.
			confidence, provenance = crossRepositoryGrade(exp.Target)
			targetIdentity := typeScriptSymbolIdentity(exp.Target.Repository, exp.Target.Package, exp.Target.File, exp.Target.QualifiedName, exp.Target.Kind, exp.Target.Signature)
			key, err := targetIdentity.Key()
			if err != nil {
				return Set{}, TypeScriptReport{}, fmt.Errorf("export %q target identity: %w", exp.QualifiedName, err)
			}
			targetKey = string(key)
		default:
			// Neither a local declaration nor a provable provider identity:
			// dropped as an unresolved reference, never guessed.
			set.Unresolved = append(set.Unresolved, UnresolvedReference{
				RepositoryKey:    repositoryKey,
				FileKey:          fileKey,
				Language:         LanguageTypeScript,
				SourceSymbolKey:  sourceKey,
				RequestedPackage: requestedPackageOr(payload.Package.Name, exp.RequestedPackage),
				RequestedSymbol:  requestedSymbolOr(exp.QualifiedName, exp.RequestedSymbol),
				Reason:           exp.Reason,
				Detail:           exp.Detail,
				Start:            Position{Line: exp.StartLine, Offset: exp.Start},
			})
			report.ExportsWithoutTarget++
			continue
		}

		evidence := Evidence{
			Key:           EvidenceKey(fileKey, exp.Start, exp.End),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start:         Position{Line: exp.StartLine, Offset: exp.Start},
			End:           Position{Line: exp.StartLine, Offset: exp.End},
			Text:          exp.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		// TargetKey names a symbol the PROVIDER repository normalises when
		// it crosses repositories: this Set alone will fail Validate() with
		// a dangling edge until the caller merges the provider's Set in,
		// exactly like an IMPORTS_SYMBOL edge.
		set.Edges = append(set.Edges, Edge{
			Kind:        kind,
			SourceKey:   sourceKey,
			TargetKey:   targetKey,
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
	}

	for _, ext := range payload.Extends {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		if _, retired := outside[ext.File]; retired {
			report.FactsOutsideRepository++
			continue
		}
		sourceKey, hasSource := symbolKeys[ext.File+"\x00"+ext.QualifiedName]
		if !hasSource {
			// The class or interface declaring every extends clause is
			// already a symbol in this same payload; a miss here is the
			// payload contradicting itself, exactly like the Imports and
			// Exports loops.
			report.EdgesWithoutSource++
			continue
		}
		fileKey := FileKey(name, ext.File)

		var targetKey string
		// A local base is the checker's own resolution inside this payload;
		// only a cross-repository one is graded by how the provider's
		// source was reached.
		confidence, provenance := ExactTypechecked, TypeScriptChecker
		switch {
		case ext.TargetQualifiedName != "" && ext.TargetFile != "":
			// A local base: the declaration already lives in this same
			// payload's Symbols, exactly like a References target.
			key, hasTarget := symbolKeys[ext.TargetFile+"\x00"+ext.TargetQualifiedName]
			if !hasTarget {
				report.EdgesWithoutTarget++
				continue
			}
			targetKey = key
		case ext.Target != nil && strings.TrimSpace(ext.Target.Kind) != "" && strings.TrimSpace(ext.Target.Signature) != "" && strings.TrimSpace(ext.Target.File) != "":
			// A cross-repository base, proven the exact same way an
			// IMPORTS_SYMBOL target is: the provider's own source, read at
			// the position the provider-source bridge resolved.
			confidence, provenance = crossRepositoryGrade(ext.Target)
			targetIdentity := typeScriptSymbolIdentity(ext.Target.Repository, ext.Target.Package, ext.Target.File, ext.Target.QualifiedName, ext.Target.Kind, ext.Target.Signature)
			key, err := targetIdentity.Key()
			if err != nil {
				return Set{}, TypeScriptReport{}, fmt.Errorf("extends %q target identity: %w", ext.QualifiedName, err)
			}
			targetKey = string(key)
		default:
			// Neither a local declaration nor a provable provider identity:
			// dropped as an unresolved reference, never guessed.
			set.Unresolved = append(set.Unresolved, UnresolvedReference{
				RepositoryKey:    repositoryKey,
				FileKey:          fileKey,
				Language:         LanguageTypeScript,
				SourceSymbolKey:  sourceKey,
				RequestedPackage: requestedPackageOr(payload.Package.Name, ext.RequestedPackage),
				RequestedSymbol:  requestedSymbolOr(ext.Text, ext.RequestedSymbol),
				Reason:           ext.Reason,
				Detail:           ext.Detail,
				Start:            Position{Line: ext.StartLine, Offset: ext.Start},
			})
			report.ExtendsWithoutTarget++
			continue
		}

		evidence := Evidence{
			Key:           EvidenceKey(fileKey, ext.Start, ext.End),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start:         Position{Line: ext.StartLine, Offset: ext.Start},
			End:           Position{Line: ext.StartLine, Offset: ext.End},
			Text:          ext.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		// TargetKey names a symbol the PROVIDER repository normalises when
		// the base crosses repositories: this Set alone will fail
		// Validate() with a dangling edge until the caller merges the
		// provider's Set in, exactly like an IMPORTS_SYMBOL edge.
		set.Edges = append(set.Edges, Edge{
			Kind:        Extends,
			SourceKey:   sourceKey,
			TargetKey:   targetKey,
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
	}

	// PACKAGE_DEPENDS_ON needs no symbol lookup at all: both ends are the
	// package keys the payload and the provider registry already name.
	// TypeScript has no module concept distinct from its package, so this
	// worker never emits MODULE_DEPENDS_ON — only Go's package/module split
	// does.
	for _, dependency := range payload.Dependencies {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		targetPackageKey := PackageKey(LanguageTypeScript, dependency.Repository, dependency.Package)
		if _, retired := outside[dependency.File]; retired {
			report.FactsOutsideRepository++
			continue
		}
		fileKey := FileKey(name, dependency.File)
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, dependency.Start, dependency.End),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start:         Position{Line: dependency.StartLine, Offset: dependency.Start},
			End:           Position{Line: dependency.StartLine, Offset: dependency.End},
			Text:          dependency.Specifier,
		}
		set.Evidence = append(set.Evidence, evidence)
		// TargetKey names a package the PROVIDER repository normalises, not
		// one in this payload's own Packages: this Set alone will fail
		// Validate() with a dangling edge until the caller merges the
		// provider's Set in, exactly like an IMPORTS_SYMBOL edge.
		set.Edges = append(set.Edges, Edge{
			Kind:        PackageDependsOn,
			SourceKey:   packageKey,
			TargetKey:   targetPackageKey,
			Confidence:  ExactTypechecked,
			Provenance:  TypeScriptChecker,
			EvidenceKey: evidence.Key,
		})
	}

	for _, entry := range payload.Unresolved {
		unresolved := UnresolvedReference{
			RepositoryKey:    repositoryKey,
			Language:         LanguageTypeScript,
			RequestedPackage: requestedPackageOr(payload.Package.Name, entry.RequestedPackage),
			RequestedSymbol:  entry.RequestedSymbol,
			Reason:           entry.Reason,
			Detail:           entry.Detail,
			Start:            Position{Offset: entry.Start},
		}
		fileKey := FileKey(name, entry.File)
		if _, exists := files[fileKey]; exists {
			unresolved.FileKey = fileKey
		}
		set.Unresolved = append(set.Unresolved, unresolved)
	}

	set.Evidence = deduplicateEvidence(set.Evidence)
	set.Sort()
	return set, report, nil
}

func discriminatorOf(signature string) string {
	trimmed := strings.TrimSpace(signature)
	if trimmed == "" {
		return "none"
	}
	return trimmed
}

// requestedPackageOr names the package an unresolved reference was requested
// from. A base type or an export that resolved to nothing local and to no
// package import was still looked up from the module's own package: that is
// the fact, and an unresolved reference with no subject is not usable by any
// consumer of the graph.
func requestedPackageOr(localPackage, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return requested
	}
	return localPackage
}

// requestedSymbolOr falls back to the name spelled at the reference site when
// the worker could not attribute the request to an exported name.
func requestedSymbolOr(spelled, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return requested
	}
	return spelled
}

// typeScriptSymbolIdentity builds the stable key identity of a TypeScript
// declaration from its identity components alone, regardless of who computes
// it: a provider uses it over its own source when it normalises itself, and
// NormalizeTypeScript's imports loop uses it again over the provider source
// reached through a declaration map. The two call sites can never diverge
// because they are the same code.
func typeScriptSymbolIdentity(repository, packageName, module, qualifiedName, kind, signature string) hotsnapshot.StableKeyIdentity {
	return hotsnapshot.StableKeyIdentity{
		FormatVersion: hotsnapshot.StableKeyFormatVersion,
		Language:      string(LanguageTypeScript),
		Repository:    repository,
		Package:       packageName,
		Module:        module,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Discriminator: discriminatorOf(signature),
	}
}
