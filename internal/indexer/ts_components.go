package indexer

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// UnresolvedComponentNotRead is the reason of a component file the TypeScript
// engine does not read and that writes the name of an exported declaration.
const UnresolvedComponentNotRead = "COMPONENT_FILE_NOT_READ"

// componentExtensions are the files a TypeScript project is made of that the
// engine cannot read: a template with a script in it.
var componentExtensions = map[string]bool{".astro": true, ".vue": true, ".svelte": true}

// maximumComponentFile bounds a file read only to look for names.
const maximumComponentFile = 1 << 20

var componentIdentifier = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// declareUnreadComponents records the component files of a package that write
// the name of a declaration it exports.
//
// An `.astro` page imports from `src/lib` like any other module, and nothing
// reads it: its uses were in no edge and in no failure, so an answer about a
// function only pages call said COMPLETE with nobody calling it. What can be
// said without a compiler for the template is what ADR 0150 says of a Rust
// file the build left out: the file was not read, and it writes that name.
// That is said once for each name, at the first line that writes it, which
// bounds an answer about that name and no other.
//
// It reads words. A name in the markup counts, which errs towards saying an
// answer is bounded. A file that writes no exported name is not listed, and a
// directory that holds another package is that package's.
func declareUnreadComponents(set *facts.Set, repository workspace.Repository, packageRoot string) {
	if len(set.Packages) != 1 || len(set.Repositories) != 1 {
		return
	}
	root := repository.RealPath
	exported := make(map[string]bool)
	for _, symbol := range set.Symbols {
		if symbol.Exported && symbol.Name != "" && symbol.Name != "default" {
			exported[symbol.Name] = true
		}
	}
	if len(exported) == 0 || strings.TrimSpace(root) == "" {
		return
	}
	var components []string
	_ = filepath.WalkDir(packageRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path == packageRoot {
				return nil
			}
			if name := entry.Name(); name == "node_modules" || strings.HasPrefix(name, ".") || workspace.IsSkippedDirectory(path) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
				return filepath.SkipDir
			}
			if excluded, err := workspace.MatchesExclusion(root, path, repository.Exclusions); err != nil || excluded {
				return filepath.SkipDir
			}
			return nil
		}
		if !componentExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if excluded, err := workspace.MatchesExclusion(root, path, repository.Exclusions); err != nil || excluded {
			return nil
		}
		if relative, err := filepath.Rel(root, path); err == nil {
			components = append(components, filepath.ToSlash(relative))
		}
		return nil
	})
	sort.Strings(components)
	repositoryKey, packageKey := set.Repositories[0].Key, set.Packages[0].Key
	for _, relative := range components {
		names := componentNames(filepath.Join(root, filepath.FromSlash(relative)), exported)
		if len(names) == 0 {
			continue
		}
		fileKey := facts.FileKey(set.Repositories[0].Name, relative)
		set.Files = append(set.Files, facts.File{
			Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey,
			Path: relative, Language: facts.LanguageTypeScript,
		})
		set.Edges = append(set.Edges, facts.Edge{
			Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileKey,
			Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest,
		})
		written := make([]string, 0, len(names))
		for name := range names {
			written = append(written, name)
		}
		sort.Strings(written)
		for _, name := range written {
			set.Unresolved = append(set.Unresolved, facts.UnresolvedReference{
				RepositoryKey:    repositoryKey,
				FileKey:          fileKey,
				Language:         facts.LanguageTypeScript,
				RequestedPackage: set.Packages[0].Name,
				RequestedSymbol:  name,
				Reason:           UnresolvedComponentNotRead,
				Detail:           "the TypeScript engine does not read this file, and it writes this name",
				Start:            facts.Position{Line: names[name]},
			})
		}
	}
	if len(components) > 0 {
		set.Sort()
	}
}

// componentNames answers the names of `declared` a file writes, each with the
// first line that does.
func componentNames(filename string, declared map[string]bool) map[string]int {
	info, err := os.Stat(filename)
	if err != nil || info.Size() > maximumComponentFile {
		return nil
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil
	}
	names := make(map[string]int)
	for number, line := range strings.Split(string(data), "\n") {
		for _, word := range componentIdentifier.FindAllString(line, -1) {
			if declared[word] && names[word] == 0 {
				names[word] = number + 1
			}
		}
	}
	return names
}
