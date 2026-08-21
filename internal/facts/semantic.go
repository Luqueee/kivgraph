package facts

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// SemanticPayload is the deliberately small boundary shared by external
// semantic loaders. The producer owns resolution; this package owns durable
// identities and the canonical graph model.
type SemanticPayload struct {
	Version int `json:"version"`
	// Authoritative is true only when the producer resolved references with a
	// language type analyzer. Syntax-only fallbacks may still provide useful
	// candidate edges, but they must never be published as exact knowledge.
	Authoritative   bool                 `json:"authoritative,omitempty"`
	Analyzer        string               `json:"analyzer,omitempty"`
	AnalyzerVersion string               `json:"analyzerVersion,omitempty"`
	Variant         string               `json:"variant,omitempty"`
	Repository      string               `json:"repository"`
	Language        Language             `json:"language"`
	Package         SemanticPackage      `json:"package"`
	Files           []SemanticFile       `json:"files"`
	Symbols         []SemanticSymbol     `json:"symbols"`
	References      []SemanticReference  `json:"references"`
	Imports         []SemanticImport     `json:"imports"`
	Parts           []SemanticPart       `json:"parts,omitempty"`
	Unresolved      []SemanticUnresolved `json:"unresolved"`
	Diagnostics     []SemanticDiagnostic `json:"diagnostics,omitempty"`
}

// SemanticDiagnostic records a limitation observed by an external producer.
// Diagnostics are facts about coverage, not graph edges, and are preserved in
// payload tooling even when the canonical graph only stores unresolved rows.
type SemanticDiagnostic struct {
	File        string `json:"file,omitempty"`
	Reason      string `json:"reason"`
	Detail      string `json:"detail,omitempty"`
	StartLine   int    `json:"startLine,omitempty"`
	StartColumn int    `json:"startColumn,omitempty"`
}

type SemanticPackage struct {
	Name         string `json:"name"`
	RootPath     string `json:"rootPath"`
	ManifestPath string `json:"manifestPath,omitempty"`
}

type SemanticFile struct {
	Path        string `json:"path"`
	Generated   bool   `json:"generated,omitempty"`
	LibraryName string `json:"libraryName,omitempty"`
}

type SemanticSymbol struct {
	ID            string `json:"id"`
	File          string `json:"file"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualifiedName"`
	Kind          string `json:"kind"`
	Exported      bool   `json:"exported"`
	Signature     string `json:"signature,omitempty"`
	StartLine     int    `json:"startLine"`
	StartColumn   int    `json:"startColumn"`
	Start         int    `json:"start"`
	EndLine       int    `json:"endLine"`
	EndColumn     int    `json:"endColumn"`
	End           int    `json:"end"`
}

// SemanticTarget is the provider identity of a declaration outside the
// payload's repository. The full index resolves this identity after all
// language units have been merged; a loader must never invent a local symbol
// for an external target it could not inspect.
type SemanticTarget struct {
	Repository    string `json:"repository"`
	Package       string `json:"package"`
	File          string `json:"file"`
	QualifiedName string `json:"qualifiedName"`
	Kind          string `json:"kind"`
	Signature     string `json:"signature"`
	Source        string `json:"source,omitempty"`
}

type SemanticReference struct {
	File        string          `json:"file"`
	SourceID    string          `json:"sourceId"`
	TargetID    string          `json:"targetId"`
	Kind        string          `json:"kind"`
	StartLine   int             `json:"startLine"`
	StartColumn int             `json:"startColumn"`
	Start       int             `json:"start"`
	EndLine     int             `json:"endLine"`
	EndColumn   int             `json:"endColumn"`
	End         int             `json:"end"`
	Text        string          `json:"text,omitempty"`
	Target      *SemanticTarget `json:"target,omitempty"`
}

type SemanticImport struct {
	File     string `json:"file"`
	SourceID string `json:"sourceId"`
	// Kind is an optional canonical edge kind. Empty preserves the historical
	// IMPORTS_SYMBOL meaning for payloads produced before Dart exports existed.
	Kind             string          `json:"kind,omitempty"`
	RequestedPackage string          `json:"requestedPackage"`
	RequestedSymbol  string          `json:"requestedSymbol"`
	Alternatives     []string        `json:"alternatives,omitempty"`
	Prefix           string          `json:"prefix,omitempty"`
	Deferred         bool            `json:"deferred,omitempty"`
	TargetID         string          `json:"targetId,omitempty"`
	StartLine        int             `json:"startLine"`
	StartColumn      int             `json:"startColumn"`
	Start            int             `json:"start"`
	Detail           string          `json:"detail,omitempty"`
	Target           *SemanticTarget `json:"target,omitempty"`
}

// SemanticPart records Dart's library/part relationship. A part contributes
// declarations to its library, so it is deliberately kept separate from an
// import/export symbol edge; the canonical graph still exposes both files and
// their definitions independently.
type SemanticPart struct {
	LibraryFile string `json:"libraryFile"`
	PartFile    string `json:"partFile"`
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	Start       int    `json:"start"`
	Detail      string `json:"detail,omitempty"`
}

type SemanticUnresolved struct {
	File             string `json:"file"`
	SourceID         string `json:"sourceId,omitempty"`
	RequestedPackage string `json:"requestedPackage,omitempty"`
	RequestedSymbol  string `json:"requestedSymbol,omitempty"`
	Reason           string `json:"reason"`
	Detail           string `json:"detail,omitempty"`
	StartLine        int    `json:"startLine"`
	StartColumn      int    `json:"startColumn"`
	Start            int    `json:"start"`
}

// SemanticTargetKey derives the same durable key a provider declaration will
// receive when its own payload is normalised. It is shared by the consumer
// normaliser and the merge bridge so cross-repository edges cannot drift.
func SemanticTargetKey(language Language, target SemanticTarget) (string, error) {
	module := ""
	if language == LanguageDart {
		module = filepath.ToSlash(filepath.Clean(strings.TrimSpace(target.File)))
	}
	identity := hotsnapshot.StableKeyIdentity{
		FormatVersion: hotsnapshot.StableKeyFormatVersion,
		Language:      string(language),
		Repository:    strings.TrimSpace(target.Repository),
		Package:       strings.TrimSpace(target.Package),
		Module:        module,
		QualifiedName: strings.TrimSpace(target.QualifiedName),
		Kind:          strings.TrimSpace(target.Kind),
		Discriminator: target.Signature,
	}
	if identity.Repository == "" || identity.Package == "" || identity.QualifiedName == "" || identity.Kind == "" || identity.Discriminator == "" {
		return "", fmt.Errorf("semantic target identity is incomplete")
	}
	key, err := identity.Key()
	if err != nil {
		return "", err
	}
	return string(key), nil
}

// NormalizeSemantic converts one external payload into canonical facts. A
// target outside this payload is allowed to dangle until the full pass merges
// the provider set; an unproven target is still published as unresolved.
func NormalizeSemantic(ctx context.Context, repository workspace.Repository, payload SemanticPayload) (Set, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Set{}, err
	}
	name := strings.TrimSpace(repository.Name)
	if name == "" || payload.Language == "" {
		return Set{}, fmt.Errorf("%w: semantic repository and language are required", ErrInvalidFacts)
	}
	root := repository.RealPath
	if root == "" {
		root = repository.Path
	}
	root = filepath.Clean(root)
	repositoryKey := RepositoryKey(name)
	packageName := strings.TrimSpace(payload.Package.Name)
	if packageName == "" {
		packageName = name
	}
	packageRoot := payload.Package.RootPath
	if packageRoot == "" {
		packageRoot = root
	}
	packageKey := PackageKey(payload.Language, name, packageName)
	set := Set{Repositories: []Repository{{Key: repositoryKey, Name: name, RootPath: root, Languages: []Language{payload.Language}}}}
	set.Packages = append(set.Packages, Package{Key: packageKey, RepositoryKey: repositoryKey, Language: payload.Language, Name: packageName, RootPath: filepath.Clean(packageRoot), ManifestPath: payload.Package.ManifestPath})
	set.Edges = append(set.Edges, Edge{Kind: ContainsPackage, SourceKey: repositoryKey, TargetKey: packageKey, Confidence: StructuralCertain, Provenance: PackageManifest})

	fileKeys := make(map[string]string, len(payload.Files))
	for _, file := range payload.Files {
		if err := ctx.Err(); err != nil {
			return Set{}, err
		}
		path := filepath.Clean(filepath.ToSlash(file.Path))
		if path == "." || path == "" {
			return Set{}, fmt.Errorf("%w: semantic file has empty path", ErrInvalidFacts)
		}
		key := FileKey(name, path)
		fileKeys[path] = key
		set.Files = append(set.Files, File{Key: key, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: path, Language: payload.Language, Generated: file.Generated})
		set.Edges = append(set.Edges, Edge{Kind: ContainsFile, SourceKey: packageKey, TargetKey: key, Confidence: StructuralCertain, Provenance: PackageManifest})
	}
	symbolKeys := make(map[string]string, len(payload.Symbols))
	moduleKeys := make(map[string]string)
	dartIdentityCounts := make(map[string]int)
	if payload.Language == LanguageDart {
		for _, symbol := range payload.Symbols {
			identity := strings.Join([]string{symbol.QualifiedName, symbol.Kind, symbol.Signature, filepath.ToSlash(filepath.Clean(symbol.File))}, "\x00")
			dartIdentityCounts[identity]++
		}
	}
	for _, symbol := range payload.Symbols {
		fileKey, ok := fileKeys[filepath.ToSlash(filepath.Clean(symbol.File))]
		if !ok {
			return Set{}, fmt.Errorf("%w: symbol %q references unreported file %q", ErrInvalidFacts, symbol.Name, symbol.File)
		}
		identity := hotsnapshot.StableKeyIdentity{FormatVersion: hotsnapshot.StableKeyFormatVersion, Language: string(payload.Language), Repository: name, Package: packageName, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind, Discriminator: symbol.Signature}
		if payload.Language == LanguageDart {
			module := filepath.ToSlash(filepath.Clean(symbol.File))
			collision := strings.Join([]string{symbol.QualifiedName, symbol.Kind, symbol.Signature, module}, "\x00")
			if dartIdentityCounts[collision] > 1 {
				module += "#" + strconv.Itoa(symbol.Start)
			}
			identity.Module = module
		}
		canonical, err := identity.Canonical()
		if err != nil {
			return Set{}, fmt.Errorf("semantic symbol %q identity: %w", symbol.QualifiedName, err)
		}
		key, err := identity.Key()
		if err != nil {
			return Set{}, fmt.Errorf("semantic symbol %q key: %w", symbol.QualifiedName, err)
		}
		keyString := string(key)
		lookup := symbol.ID
		if lookup == "" {
			lookup = symbol.File + "\x00" + symbol.QualifiedName
		}
		symbolKeys[lookup] = keyString
		if symbol.Kind == "module" {
			moduleKeys[filepath.ToSlash(filepath.Clean(symbol.File))] = keyString
		}
		set.Symbols = append(set.Symbols, Symbol{Key: keyString, CanonicalIdentity: canonical, RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey, Language: payload.Language, Name: symbol.Name, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind, Exported: symbol.Exported, Signature: symbol.Signature, Start: Position{Line: symbol.StartLine, Column: symbol.StartColumn, Offset: symbol.Start}, End: Position{Line: symbol.EndLine, Column: symbol.EndColumn, Offset: symbol.End}})
		set.Edges = append(set.Edges, Edge{Kind: Defines, SourceKey: fileKey, TargetKey: keyString, Confidence: StructuralCertain, Provenance: definitionProvenance(payload.Language)})
	}
	partEdges := make(map[string]struct{}, len(payload.Parts))
	for _, part := range payload.Parts {
		libraryFile := filepath.ToSlash(filepath.Clean(part.LibraryFile))
		partFile := filepath.ToSlash(filepath.Clean(part.PartFile))
		libraryKey := moduleKeys[libraryFile]
		partKey := moduleKeys[partFile]
		if libraryKey == "" || partKey == "" || libraryKey == partKey {
			continue
		}
		identity := partKey + "\x00" + libraryKey
		if _, exists := partEdges[identity]; exists {
			continue
		}
		partEdges[identity] = struct{}{}
		fileKey := fileKeys[libraryFile]
		if fileKey == "" {
			fileKey = fileKeys[partFile]
		}
		if fileKey == "" {
			continue
		}
		set.Edges = append(set.Edges, Edge{Kind: PartOf, SourceKey: partKey, TargetKey: libraryKey, Confidence: StructuralCertain, Provenance: PackageManifest})
	}
	hasExternalTarget := false
	for _, reference := range payload.References {
		sourceKey := symbolKeys[reference.SourceID]
		targetKey := symbolKeys[reference.TargetID]
		confidence := Candidate
		if payload.Authoritative {
			confidence = ExactTypechecked
		}
		if targetKey == "" && reference.Target != nil {
			var err error
			targetKey, err = SemanticTargetKey(payload.Language, *reference.Target)
			if err != nil {
				return Set{}, fmt.Errorf("semantic reference target: %w", err)
			}
			if reference.Target.Repository != name {
				hasExternalTarget = true
				if reference.Target.Source == "PROVIDER_SOURCE" {
					confidence = ExactPackageMapped
				}
			}
		}
		if sourceKey == "" || targetKey == "" {
			continue
		}
		fileKey := fileKeys[filepath.ToSlash(filepath.Clean(reference.File))]
		if fileKey == "" {
			return Set{}, fmt.Errorf("%w: reference reports unreported file %q", ErrInvalidFacts, reference.File)
		}
		evidence := Evidence{Key: EvidenceKey(fileKey, reference.Start, reference.End), RepositoryKey: repositoryKey, FileKey: fileKey, Start: Position{Line: reference.StartLine, Column: reference.StartColumn, Offset: reference.Start}, End: Position{Line: reference.EndLine, Column: reference.EndColumn, Offset: reference.End}, Text: reference.Text}
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, Edge{Kind: EdgeKind(reference.Kind), SourceKey: sourceKey, TargetKey: targetKey, Confidence: confidence, Provenance: useProvenance(payload.Language, reference.Kind), EvidenceKey: evidence.Key})
	}
	for _, importFact := range payload.Imports {
		fileKey := fileKeys[filepath.ToSlash(filepath.Clean(importFact.File))]
		if fileKey == "" {
			return Set{}, fmt.Errorf("%w: import references unreported file %q", ErrInvalidFacts, importFact.File)
		}
		sourceKey := symbolKeys[importFact.SourceID]
		sourceSymbolKey := sourceKey
		if sourceKey == "" {
			sourceKey = moduleKeys[filepath.ToSlash(filepath.Clean(importFact.File))]
		}
		if sourceKey == "" {
			sourceKey = fileKey
		}
		targetKey := symbolKeys[importFact.TargetID]
		confidence := Candidate
		if payload.Authoritative {
			confidence = ExactTypechecked
		}
		if targetKey == "" && importFact.Target != nil {
			var err error
			targetKey, err = SemanticTargetKey(payload.Language, *importFact.Target)
			if err != nil {
				return Set{}, fmt.Errorf("semantic import target: %w", err)
			}
			if importFact.Target.Repository != name {
				hasExternalTarget = true
				if importFact.Target.Source == "PROVIDER_SOURCE" {
					confidence = ExactPackageMapped
				}
			}
		}
		kind := ImportsSymbol
		if strings.TrimSpace(importFact.Kind) != "" {
			kind = EdgeKind(importFact.Kind)
			if !kind.Valid() || kind == ContainsPackage || kind == ContainsFile || kind == Defines || kind == PackageDependsOn || kind == ModuleDependsOn {
				return Set{}, fmt.Errorf("%w: semantic import kind %q is not a symbol relation", ErrInvalidFacts, importFact.Kind)
			}
		}
		if targetKey != "" {
			set.Edges = append(set.Edges, Edge{Kind: kind, SourceKey: sourceKey, TargetKey: targetKey, Confidence: confidence, Provenance: useProvenance(payload.Language, kind)})
			continue
		}
		set.Unresolved = append(set.Unresolved, UnresolvedReference{RepositoryKey: repositoryKey, FileKey: fileKey, Language: payload.Language, SourceSymbolKey: sourceSymbolKey, RequestedPackage: firstNonEmptySemantic(importFact.RequestedPackage, packageName), RequestedSymbol: importFact.RequestedSymbol, Reason: "IMPORT_NOT_RESOLVED", Detail: importFact.Detail, Start: Position{Line: importFact.StartLine, Column: importFact.StartColumn, Offset: importFact.Start}})
	}
	for _, unresolved := range payload.Unresolved {
		fileKey := fileKeys[filepath.ToSlash(filepath.Clean(unresolved.File))]
		if fileKey == "" {
			return Set{}, fmt.Errorf("%w: unresolved reference reports unreported file %q", ErrInvalidFacts, unresolved.File)
		}
		set.Unresolved = append(set.Unresolved, UnresolvedReference{RepositoryKey: repositoryKey, FileKey: fileKey, Language: payload.Language, SourceSymbolKey: symbolKeys[unresolved.SourceID], RequestedPackage: firstNonEmptySemantic(unresolved.RequestedPackage, packageName), RequestedSymbol: unresolved.RequestedSymbol, Reason: unresolved.Reason, Detail: unresolved.Detail, Start: Position{Line: unresolved.StartLine, Column: unresolved.StartColumn, Offset: unresolved.Start}})
	}
	set.Sort()
	if err := set.Validate(); err != nil && !hasExternalTarget {
		return Set{}, err
	}
	return set, nil
}

func definitionProvenance(language Language) Provenance {
	if language == LanguageDart {
		return DartAnalyzerDefinition
	}
	return PythonIndexerDefinition
}

func useProvenance(language Language, kind any) Provenance {
	if language == LanguageDart {
		return DartAnalyzerUse
	}
	return PythonIndexerUse
}

func firstNonEmptySemantic(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}
