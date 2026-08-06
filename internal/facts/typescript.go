package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Luqueee/luque/internal/hotsnapshot"
)

// TypeScriptWireVersion is the version of the `ts-facts-v3` payload.
const TypeScriptWireVersion = 3

// TypeScriptPayload is the fact payload the worker emits for one repository.
//
// The worker reports identity components and positions; it never computes a
// key. Deriving keys on a single side is what keeps one symbol from getting
// two identities when a consumer and its provider are indexed separately.
type TypeScriptPayload struct {
	Version    int                    `json:"version"`
	Repository TypeScriptRepository   `json:"repository"`
	Package    *TypeScriptPackage     `json:"package"`
	Files      []string               `json:"files"`
	Symbols    []TypeScriptSymbol     `json:"symbols"`
	References []TypeScriptReference  `json:"references"`
	Imports    []TypeScriptImport     `json:"imports"`
	Exports    []TypeScriptExport     `json:"exports"`
	Unresolved []TypeScriptUnresolved `json:"unresolved"`
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
// itself: computed from the provider's own source, at the position
// LUQUE-0703's declaration map bridge resolved. Never derived from the
// consumer's `.d.ts` text, which is not guaranteed to restate the source.
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
	// EdgesWithoutSource counts uses at file scope, with no enclosing symbol.
	EdgesWithoutSource int
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
}

// DecodeTypeScriptPayload parses a `ts-facts-v3` document.
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
// rootPath is the absolute repository root, which the caller already knows:
// the payload carries only repository relative paths.
func NormalizeTypeScript(
	ctx context.Context,
	payload TypeScriptPayload,
	rootPath string,
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

	files := make(map[string]struct{}, len(payload.Files))
	for _, file := range payload.Files {
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
		fileKey := FileKey(name, symbol.File)
		if _, exists := files[fileKey]; !exists {
			return Set{}, TypeScriptReport{}, fmt.Errorf("%w: symbol %q lives in unreported file %q",
				ErrInvalidFacts, symbol.QualifiedName, symbol.File)
		}
		identity := typeScriptSymbolIdentity(name, payload.Package.Name, symbol.QualifiedName, symbol.Kind, symbol.Signature)
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

	report := TypeScriptReport{}
	for _, reference := range payload.References {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
		}
		sourceKey, hasSource := symbolKeys[reference.File+"\x00"+reference.SourceQualifiedName]
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
		if target == nil || strings.TrimSpace(target.Kind) == "" || strings.TrimSpace(target.Signature) == "" {
			// No declaration map reached an exact provider declaration, or it
			// reached one whose class or signature is unclassified. Either
			// way the provider's stable key cannot be derived, so this is a
			// dropped edge recorded as an unresolved reference, never a
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
		targetIdentity := typeScriptSymbolIdentity(target.Repository, target.Package, target.QualifiedName, target.Kind, target.Signature)
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
		set.Edges = append(set.Edges, Edge{
			Kind:        ImportsSymbol,
			SourceKey:   sourceKey,
			TargetKey:   string(targetKey),
			Confidence:  ExactTypechecked,
			Provenance:  TypeScriptChecker,
			EvidenceKey: evidence.Key,
		})
	}

	for _, exp := range payload.Exports {
		if err := ctx.Err(); err != nil {
			return Set{}, TypeScriptReport{}, err
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
		case exp.Target != nil && strings.TrimSpace(exp.Target.Kind) != "" && strings.TrimSpace(exp.Target.Signature) != "":
			// A cross-repository target, proven the exact same way an
			// IMPORTS_SYMBOL target is: the provider's own source, read at
			// the position LUQUE-0703's declaration map bridge resolved.
			targetIdentity := typeScriptSymbolIdentity(exp.Target.Repository, exp.Target.Package, exp.Target.QualifiedName, exp.Target.Kind, exp.Target.Signature)
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
				RequestedPackage: exp.RequestedPackage,
				RequestedSymbol:  exp.RequestedSymbol,
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
			Confidence:  ExactTypechecked,
			Provenance:  TypeScriptChecker,
			EvidenceKey: evidence.Key,
		})
	}

	for _, entry := range payload.Unresolved {
		unresolved := UnresolvedReference{
			RepositoryKey:    repositoryKey,
			Language:         LanguageTypeScript,
			RequestedPackage: entry.RequestedPackage,
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

// typeScriptSymbolIdentity builds the stable key identity of a TypeScript
// declaration from its identity components alone, regardless of who computes
// it: a provider uses it over its own source when it normalises itself, and
// NormalizeTypeScript's imports loop uses it again over the provider source
// reached through a declaration map. The two call sites can never diverge
// because they are the same code.
func typeScriptSymbolIdentity(repository, packageName, qualifiedName, kind, signature string) hotsnapshot.StableKeyIdentity {
	return hotsnapshot.StableKeyIdentity{
		FormatVersion: hotsnapshot.StableKeyFormatVersion,
		Language:      string(LanguageTypeScript),
		Repository:    repository,
		Package:       packageName,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Discriminator: discriminatorOf(signature),
	}
}
