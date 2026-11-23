package indexing

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// ErrGenerationLostFiles reports a pass nobody asked for that would have
// replaced a generation with one that reads less of the same code.
var ErrGenerationLostFiles = errors.New("the pass lost files the active generation reads")

// lostFiles names, as `repository:path`, the files that held declarations in
// the active generation, are still on disk, and hold none in the candidate --
// in a repository and language where the candidate says something was not
// loaded.
//
// A pass isolates what it cannot read and publishes the rest (ADR 0147). That
// is right for a pass somebody ran: they read its report. A pass the watcher
// started has no reader, and on a machine whose `go` moved ahead of the build
// it replaced a whole graph with one that held no Go, generation after
// generation, while every answer went on saying COMPLETE about what was left.
//
// Each clause keeps out something that is not a loss. A file that is gone was
// deleted. A repository or a language the registry no longer names was
// withdrawn. And without a `*_NOT_LOADED` or `ANALYZER_UNAVAILABLE` failure the pass read everything it
// was asked to: the file was excluded by a configuration, or holds no
// declaration any more.
func lostFiles(active *hotsnapshot.GraphSnapshot, candidate facts.Set, exists func(string) bool) []string {
	registered := make(map[string]map[string]bool, len(candidate.Repositories))
	names := make(map[string]string, len(candidate.Repositories))
	for _, repository := range candidate.Repositories {
		languages := make(map[string]bool, len(repository.Languages))
		for _, language := range repository.Languages {
			languages[string(language)] = true
		}
		registered[repository.Name] = languages
		names[repository.Key] = repository.Name
	}
	notLoaded := make(map[string]bool)
	for _, unresolved := range candidate.Unresolved {
		if isLoadFailure(unresolved.Reason) {
			notLoaded[names[unresolved.RepositoryKey]+"\x00"+string(unresolved.Language)] = true
		}
	}
	if len(notLoaded) == 0 {
		return nil
	}
	paths := make(map[string]string, len(candidate.Files))
	for _, file := range candidate.Files {
		paths[file.Key] = names[file.RepositoryKey] + ":" + file.Path
	}
	read := make(map[string]bool, len(candidate.Files))
	for _, symbol := range candidate.Symbols {
		read[paths[symbol.FileKey]] = true
	}

	lost := make([]string, 0)
	text := active.Strings()
	for _, id := range active.FilesHoldingSymbols() {
		file, _ := active.File(id)
		repository, found := active.Repository(file.Repository)
		if !found {
			continue
		}
		name, _ := text.String(repository.Name)
		root, _ := text.String(repository.Path)
		path, _ := text.String(file.Path)
		language, _ := text.String(file.Language)
		if !registered[name][language] || !notLoaded[name+"\x00"+language] || read[name+":"+path] {
			continue
		}
		if exists(filepath.Join(root, filepath.FromSlash(path))) {
			lost = append(lost, name+":"+path)
		}
	}
	sort.Strings(lost)
	return lost
}

// isLoadFailure says whether a reason means the pass could not read code it
// was asked to: a module or workspace that did not load, or an analyzer that
// did not run at all. The second is the Rust loader's way of saying the whole
// workspace is missing (ADR 0035), and an unattended pass that ran on a PATH
// without rust-analyzer replaced a graph of 4,595 symbols with one of none.
func isLoadFailure(reason string) bool {
	return strings.HasSuffix(reason, "_NOT_LOADED") || reason == "ANALYZER_UNAVAILABLE"
}

func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
