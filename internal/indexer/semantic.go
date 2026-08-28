package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/csharploader"
	"github.com/Luqueee/kivgraph/internal/dartloader"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/javaloader"
	"github.com/Luqueee/kivgraph/internal/pythonloader"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func semanticUnits(repositories []workspace.Repository, language facts.Language) []analysisUnit {
	units := make([]analysisUnit, 0, len(repositories))
	for _, repository := range repositories {
		units = append(units, analysisUnit{
			repository: repository, language: language, kind: unitSemantic,
		})
	}
	return units
}

// semanticSourceExtensions is what each semantic language is written in, for
// the file count that estimates a unit's weight.
//
// It is deliberately not config.SourceExtensions: that table is keyed by the
// spellings a configuration may declare, and this one is keyed by the language
// of a fact. A language absent here weighs zero and is scheduled last, which
// is wrong but not incorrect; a language absent from the table above publishes
// nothing at all.
var semanticSourceExtensions = map[facts.Language]map[string]bool{
	facts.LanguagePython: {".py": true, ".pyi": true},
	facts.LanguageDart:   {".dart": true},
	facts.LanguageJava:   {".java": true},
	facts.LanguageCSharp: {".cs": true},
}

// semanticSkippedDirectories are the analyzer and build outputs a source count
// must not walk into. They are named per language rather than pooled so a
// directory one language treats as source is not skipped for it.
var semanticSkippedDirectories = map[string]bool{
	".git": true, ".dart_tool": true, "build": true, ".venv": true,
	"venv": true, "__pycache__": true, "target": true, ".gradle": true,
	"bin": true, "obj": true,
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
			if semanticSkippedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if semanticSourceExtensions[language][strings.ToLower(filepath.Ext(path))] {
			total++
		}
		return nil
	})
	return total
}

// analyzerNotInstalled records a repository whose analyzer this machine does
// not have.
//
// It is the same judgement indexGoModule already makes about a module that
// will not load, applied to the reason one language cannot be read at all: a
// toolchain nobody installed is a fact about this machine, not about the code,
// and it must not decide whether every other repository gets a graph. A Dart
// repository registered on a laptop without Dart used to fail the entire
// index -- five repositories, four of which had nothing to do with Dart.
//
// Nothing is published for it. Unlike a Go module that failed to load, there
// is no diagnostic worth putting in the graph: the answer is "install the
// analyzer", which belongs in the run's own output and in `doctor`, both of
// which already say it.
func analyzerNotInstalled(unit analysisUnit, err error) analysisResult {
	return analysisResult{
		notLoaded: true,
		detail:    fmt.Sprintf("%s analyzer not installed: %v", unit.language, err),
	}
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
	case facts.LanguageJava:
		payload, err = javaloader.Run(ctx, javaloader.Options{
			Command:          options.JavaIndexerCommand,
			BuildTool:        options.JavaBuildTool,
			TargetDirectory:  options.JavaTargetDirectory,
			Repository:       unit.repository,
			IncludeTests:     options.JavaIncludeTests,
			IncludeGenerated: options.JavaIncludeGenerated,
			MaximumIndexTime: options.JavaMaximumIndexTime,
		})
	case facts.LanguageCSharp:
		payload, err = csharploader.Run(ctx, csharploader.Options{
			Command:          options.CSharpIndexerCommand,
			Project:          options.CSharpProject,
			TargetDirectory:  options.CSharpTargetDirectory,
			Repository:       unit.repository,
			IncludeTests:     options.CSharpIncludeTests,
			IncludeGenerated: options.CSharpIncludeGenerated,
			MaximumIndexTime: options.CSharpMaximumIndexTime,
			SkipRestore:      options.CSharpSkipRestore,
		})
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
		if errors.Is(err, exec.ErrNotFound) {
			return analyzerNotInstalled(unit, err), nil
		}
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
	case facts.LanguageCSharp:
		// scip-dotnet writes `.` for a package the project declares and a
		// real assembly name for one it consumed, so the last segment is the
		// assembly a registered repository would be named after.
		if slash := strings.LastIndexByte(requested, '/'); slash >= 0 {
			requested = requested[slash+1:]
		}
	case facts.LanguageJava:
		// A SCIP package identity is `<manager>/<group>/<artifact>`, and the
		// artifact is what a registered repository is named after. Matching
		// on the whole identity would never find a provider, because it
		// carries the group and the version of the consumer's view.
		if slash := strings.LastIndexByte(requested, '/'); slash >= 0 {
			requested = requested[slash+1:]
		}
	}
	return requested
}

func edgeIdentity(edge facts.Edge) string {
	return string(edge.Kind) + "\x00" + edge.SourceKey + "\x00" + edge.TargetKey + "\x00" + string(edge.Confidence) + "\x00" + string(edge.Provenance) + "\x00" + edge.EvidenceKey
}
