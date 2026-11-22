// Package checkout answers whether a directory is the root of another Git
// checkout. Every walk and loader that skips such a directory asks this package,
// so they cannot disagree about what a repository contains. Nothing here imports
// Kivgraph, which lets the configuration package use it without a cycle.
package checkout

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// pointerLimit bounds the read of a `.git` file: a gitdir pointer is one line,
// and a large file is not worth reading whole.
const pointerLimit = 4 << 10

// IsOtherCheckout reports whether directory is the root of another Git checkout:
// a clone, or a worktree linked from any repository. Git does not look inside
// one, and a walk that entered one would index the same files a second time.
//
// A submodule has the same two forms of `.git`, so its shape cannot tell it from
// a checkout. The repository that encloses it lists it in .gitmodules, and that
// list is the answer. Callers pass an absolute path, never the repository root.
func IsOtherCheckout(directory string) bool {
	marker := filepath.Join(directory, ".git")
	info, err := os.Lstat(marker)
	if err != nil {
		return false
	}
	candidate := info.IsDir() || (info.Mode().IsRegular() && isGitdirPointer(marker))
	return candidate && !enclosingRepositoryListsSubmodule(directory)
}

// isGitdirPointer reports whether the `.git` file at marker is a gitdir pointer.
// Any other file is not a checkout's marker, so its directory stays walked.
func isGitdirPointer(marker string) bool {
	file, err := os.Open(marker)
	if err != nil {
		return false
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, pointerLimit))
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(head)), "gitdir:")
}

// enclosingRepositoryListsSubmodule reports whether the nearest repository above
// directory lists it in its .gitmodules. Only the enclosing repository declares
// a submodule, whatever form the submodule's own `.git` takes.
func enclosingRepositoryListsSubmodule(directory string) bool {
	enclosing, found := enclosingRepository(directory)
	if !found {
		return false
	}
	relative, err := filepath.Rel(enclosing, directory)
	if err != nil {
		return false
	}
	return gitmodulesListsPath(filepath.Join(enclosing, ".gitmodules"), filepath.ToSlash(relative))
}

// enclosingRepository returns the nearest ancestor of directory that has a
// `.git` entry of any kind.
func enclosingRepository(directory string) (string, bool) {
	dir := filepath.Dir(directory)
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// gitmodulesListsPath reports whether the .gitmodules at path declares a
// submodule at relative, a slash-separated path. A link or a directory of that
// name declares nothing.
func gitmodulesListsPath(path, relative string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found && strings.ToLower(strings.TrimSpace(key)) == "path" &&
			filepath.ToSlash(filepath.Clean(strings.TrimSpace(value))) == relative {
			return true
		}
	}
	return false
}
