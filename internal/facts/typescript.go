package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Luqueee/luque/internal/hotsnapshot"
)

// TypeScriptWireVersion is the version of the `ts-facts-v1` payload.
const TypeScriptWireVersion = 1

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
}

// DecodeTypeScriptPayload parses a `ts-facts-v1` document.
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
		identity := hotsnapshot.StableKeyIdentity{
			FormatVersion: hotsnapshot.StableKeyFormatVersion,
			Language:      string(LanguageTypeScript),
			Repository:    name,
			Package:       payload.Package.Name,
			QualifiedName: symbol.QualifiedName,
			Kind:          symbol.Kind,
			Discriminator: discriminatorOf(symbol.Signature),
		}
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
