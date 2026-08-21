package indexer

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/dartloader"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/pythonloader"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func semanticUnits(repositories []workspace.Repository, language facts.Language) []analysisUnit {
	units := make([]analysisUnit, 0, len(repositories))
	for _, repository := range repositories {
		units = append(units, analysisUnit{repository: repository, language: language, isPython: language == facts.LanguagePython, isDart: language == facts.LanguageDart})
	}
	return units
}

func countSemanticFiles(repository workspace.Repository, language facts.Language) int {
	root := repository.RealPath
	if root == "" {
		root = repository.Path
	}
	total := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".dart_tool", "build", ".venv", "venv", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if (language == facts.LanguagePython && (ext == ".py" || ext == ".pyi")) || (language == facts.LanguageDart && ext == ".dart") {
			total++
		}
		return nil
	})
	return total
}

func indexSemantic(ctx context.Context, options FullOptions, unit analysisUnit) (analysisResult, error) {
	var payload facts.SemanticPayload
	var err error
	switch unit.language {
	case facts.LanguagePython:
		payload, err = pythonloader.RunWithOptions(ctx, pythonloader.Options{
			IndexerCommand:   options.PythonIndexer,
			AnalyzerCommand:  options.PythonAnalyzer,
			AnalyzerMode:     options.PythonAnalyzerMode,
			PythonPath:       options.PythonPath,
			IncludeTests:     options.PythonIncludeTests,
			IncludeGenerated: options.PythonIncludeGenerated,
			IncludeExternal:  options.PythonIncludeExternal,
		}, unit.repository, options.WorkingDirectory)
	case facts.LanguageDart:
		payload, err = dartloader.RunWithOptions(ctx, dartloader.Options{
			Command:             options.DartAnalyzer,
			SDKPath:             options.DartSDKPath,
			Repository:          unit.repository,
			IncludeGenerated:    options.DartIncludeGenerated,
			IncludeTests:        options.DartIncludeTests,
			IncludeExternal:     options.DartIncludeExternal,
			IncludeSDK:          options.DartIncludeSDK,
			PackageConfig:       options.DartPackageConfig,
			WaitForAnalysis:     options.DartWaitForAnalysis,
			MaximumAnalysisTime: options.DartMaximumAnalysisTime,
			Providers:           options.Repositories,
		})
	default:
		return analysisResult{}, fmt.Errorf("unsupported semantic language %q", unit.language)
	}
	if err != nil {
		return analysisResult{}, err
	}
	set, err := facts.NormalizeSemantic(ctx, unit.repository, payload)
	if err != nil {
		return analysisResult{}, err
	}
	composed := make(map[string]composedTarget)
	for _, target := range semanticExternalTargets(payload, unit.repository.Name) {
		key, keyErr := facts.SemanticTargetKey(unit.language, target)
		if keyErr != nil {
			return analysisResult{}, fmt.Errorf("semantic target identity: %w", keyErr)
		}
		composed[key] = composedTarget{Repository: target.Repository, Package: target.Package, Symbol: target.QualifiedName}
	}
	return analysisResult{set: set, symbols: len(set.Symbols), definitions: len(set.Symbols), references: countSemanticReferences(set), unresolved: len(set.Unresolved), composed: composed, detail: fmt.Sprintf("%s files=%d symbols=%d", unit.language, countSemanticFiles(unit.repository, unit.language), len(set.Symbols))}, nil
}

func semanticExternalTargets(payload facts.SemanticPayload, repositoryName string) []facts.SemanticTarget {
	seen := make(map[string]struct{})
	result := make([]facts.SemanticTarget, 0)
	add := func(target *facts.SemanticTarget) {
		if target == nil || target.Repository == repositoryName {
			return
		}
		key := target.Repository + "\x00" + target.Package + "\x00" + target.QualifiedName + "\x00" + target.Kind + "\x00" + target.Signature
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, *target)
	}
	for _, reference := range payload.References {
		add(reference.Target)
	}
	for _, importFact := range payload.Imports {
		add(importFact.Target)
	}
	return result
}

func countSemanticReferences(set facts.Set) int {
	total := 0
	for _, edge := range set.Edges {
		switch edge.Kind {
		case facts.References, facts.CallsDirect, facts.ImportsSymbol, facts.TypeUses, facts.Implements, facts.Extends, facts.Embeds, facts.Overrides, facts.PartOf:
			total++
		}
	}
	return total
}

// resolveSemanticPackageDependencies turns a declared import into a package
// dependency when a uniquely named provider is present in the same merged
// corpus. It deliberately does not create a symbol edge: a package dependency
// proves only that the consumer depends on the provider package. Ambiguous or
// absent providers remain UNRESOLVED and are therefore visible to MCP callers.
func resolveSemanticPackageDependencies(set facts.Set) facts.Set {
	files := make(map[string]facts.File, len(set.Files))
	for _, file := range set.Files {
		files[file.Key] = file
	}
	packages := make(map[string]facts.Package, len(set.Packages))
	byName := make(map[string][]facts.Package)
	for _, pkg := range set.Packages {
		packages[pkg.Key] = pkg
		key := string(pkg.Language) + "\x00" + pkg.Name
		byName[key] = append(byName[key], pkg)
	}
	for key := range byName {
		sort.Slice(byName[key], func(left, right int) bool { return byName[key][left].Key < byName[key][right].Key })
	}
	seen := make(map[string]struct{})
	for _, edge := range set.Edges {
		seen[edgeIdentity(edge)] = struct{}{}
	}
	for _, unresolved := range set.Unresolved {
		if unresolved.Reason != "IMPORT_NOT_RESOLVED" || unresolved.FileKey == "" {
			continue
		}
		file, exists := files[unresolved.FileKey]
		if !exists {
			continue
		}
		sourcePackage, exists := packages[file.PackageKey]
		if !exists {
			continue
		}
		requested := semanticRequestedPackage(file.Language, unresolved.RequestedPackage)
		providers := byName[string(file.Language)+"\x00"+requested]
		if requested == "" || len(providers) != 1 || providers[0].Key == sourcePackage.Key {
			continue
		}
		candidate := facts.Edge{Kind: facts.PackageDependsOn, SourceKey: sourcePackage.Key, TargetKey: providers[0].Key, Confidence: facts.ExactPackageMapped, Provenance: facts.PackageManifest}
		identity := edgeIdentity(candidate)
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		set.Edges = append(set.Edges, candidate)
	}
	set.Sort()
	return set
}

func semanticRequestedPackage(language facts.Language, requested string) string {
	requested = strings.TrimSpace(requested)
	switch language {
	case facts.LanguageDart:
		requested = strings.TrimPrefix(requested, "package:")
		if slash := strings.IndexByte(requested, '/'); slash >= 0 {
			requested = requested[:slash]
		}
	case facts.LanguagePython:
		if dot := strings.IndexByte(requested, '.'); dot >= 0 {
			requested = requested[:dot]
		}
	}
	return requested
}

func edgeIdentity(edge facts.Edge) string {
	return string(edge.Kind) + "\x00" + edge.SourceKey + "\x00" + edge.TargetKey + "\x00" + string(edge.Confidence) + "\x00" + string(edge.Provenance) + "\x00" + edge.EvidenceKey
}
