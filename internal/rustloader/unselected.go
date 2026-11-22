package rustloader

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/testpath"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// maximumUnselectedFile bounds a file read only to look for names.
const maximumUnselectedFile = 1 << 20

var (
	// Rust identifiers are Unicode: `[A-Za-z_]` read `größe` as `gr` and `e`.
	rustIdentifier   = regexp.MustCompile(`[\p{L}_][\p{L}\p{N}_]*`)
	rustBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	rustLineComment  = regexp.MustCompile(`//[^\n]*`)
)

// collectUnselectedFiles declares the source files of an indexed crate that
// the build configuration left out: a module behind a `cfg` that is false on
// this machine. The analyzer writes no document for one, so its uses were in
// no edge and in no failure, and an answer about a declaration it uses said
// COMPLETE.
//
// Nothing here knows what a name in such a file resolves to. What can be said
// is that the file was not read and that it writes the name of a declaration
// this workspace publishes, and that is said once per name, at the first line
// that writes it -- which is what bounds an answer about that name and no
// other. A file that names nothing this workspace declares is not listed.
//
// A test file is not one of them: it is left out by what `include_tests` asks
// for, and the files a configuration excludes do not bound an answer.
func collectUnselectedFiles(options AnalyzeOptions, analysis *Analysis, unresolved map[string]UnresolvedReference) {
	root := options.Repository.RealPath
	if strings.TrimSpace(root) == "" {
		return
	}
	indexed := make(map[string]struct{}, len(analysis.Files))
	crates := make(map[string]struct{})
	for _, file := range analysis.Files {
		indexed[file.Path] = struct{}{}
		crates[file.Crate.Name] = struct{}{}
	}
	declared := make(map[string]struct{}, len(analysis.Definitions))
	for _, definition := range analysis.Definitions {
		declared[definition.Name] = struct{}{}
	}
	// A file the repository excludes was left out by the registry, not by the
	// build: it does not bound an answer. An unreadable list excludes nothing,
	// which errs towards declaring.
	excluded, err := workspace.ExclusionMatcher(root, options.Repository.Exclusions)
	if err != nil {
		excluded = func(string) bool { return false }
	}
	for _, crate := range options.Crates {
		if _, selected := crates[crate.Name]; !selected {
			// No file of it at all: collectEmptyCrates declares the crate.
			continue
		}
		// What could not be read is declared for the crate, with no file: any
		// name may be written there, so no answer about the crate's
		// repository can say it looked everywhere. Skipping it in silence said
		// COMPLETE over a file nobody opened.
		unread := func(path string, err error) {
			unresolved[string(UnresolvedTargetNotBuildable)+"\x00unread\x00"+path] = UnresolvedReference{
				Crate:          CrateRef{Name: crate.Name, Version: crate.Version},
				RequestedCrate: crate.Name,
				Reason:         UnresolvedTargetNotBuildable,
				Detail:         fmt.Sprintf("a source the build configuration may have left out could not be read, so the names it writes are unknown: %s: %v", path, err),
			}
		}
		source := filepath.Join(crate.RootPath, "src")
		var unselected []string
		_ = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// A crate with no `src` is not one that could not be read.
				if path != source || !errors.Is(err, fs.ErrNotExist) {
					unread(path, err)
				}
				return nil
			}
			if entry.IsDir() || filepath.Ext(path) != ".rs" {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relative = filepath.ToSlash(relative)
			if _, read := indexed[relative]; !read && !testpath.Is(relative) && !excluded(relative) {
				unselected = append(unselected, relative)
			}
			return nil
		})
		sort.Strings(unselected)
		for _, relative := range unselected {
			filename := filepath.Join(root, filepath.FromSlash(relative))
			names, err := namesWritten(filename, declared)
			if err != nil {
				// Removed since it was listed: it writes nothing now.
				if !errors.Is(err, fs.ErrNotExist) {
					unread(filename, err)
				}
				continue
			}
			if len(names) == 0 {
				continue
			}
			analysis.Files = append(analysis.Files, IndexedFile{Path: relative, Crate: CrateRef{Name: crate.Name, Version: crate.Version}})
			for name, line := range names {
				unresolved[string(UnresolvedTargetNotBuildable)+"\x00"+relative+"\x00"+name] = UnresolvedReference{
					Crate:           CrateRef{Name: crate.Name, Version: crate.Version},
					File:            relative,
					RequestedCrate:  crate.Name,
					RequestedSymbol: name,
					Reason:          UnresolvedTargetNotBuildable,
					Detail:          "the build configuration of this index did not select this file, and it writes this name",
					StartLine:       line,
				}
			}
		}
	}
}

// namesWritten answers the names of `declared` a file writes outside its
// comments, each with the first line that does. It reads words, not Rust: a
// name inside a string counts, which errs towards saying an answer is bounded.
//
// A file above the bound is not read, by decision, and answers no names. A
// file that cannot be read is an error: what it writes is not known.
func namesWritten(filename string, declared map[string]struct{}) (map[string]int, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if info.Size() > maximumUnselectedFile {
		return nil, nil
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	// A comment is blanked and not removed, so that lines keep their number.
	blank := func(comment string) string {
		return strings.Map(func(character rune) rune {
			if character == '\n' {
				return '\n'
			}
			return ' '
		}, comment)
	}
	text := rustLineComment.ReplaceAllStringFunc(rustBlockComment.ReplaceAllStringFunc(string(data), blank), blank)
	names := make(map[string]int)
	for number, line := range strings.Split(text, "\n") {
		for _, word := range rustIdentifier.FindAllString(line, -1) {
			if _, known := declared[word]; !known {
				continue
			}
			if _, seen := names[word]; !seen {
				names[word] = number + 1
			}
		}
	}
	return names, nil
}
