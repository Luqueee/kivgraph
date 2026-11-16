package workspace

import (
	"io"
	"os"
	"path/filepath"
)

// cacheDirectoryTag and cacheDirectorySignature are the Cache Directory Tagging
// Specification (https://bford.info/cachedir/): a tool that fills a directory
// with reproducible cache says so by writing this file, whose first bytes are
// the signature. Cargo writes one in every target/ it creates.
const (
	cacheDirectoryTag       = "CACHEDIR.TAG"
	cacheDirectorySignature = "Signature: 8a477f597d28d172789f06886806bc55"
)

// IsCacheDirectory reports whether directory holds a valid cache directory tag
// (ADR 0122). Every walk that decides what a repository's sources are asks it
// before entering a directory, so they cannot disagree about what is skipped.
//
// The tag must be a regular file -- a symlink is not one, so a tagged-looking
// link cannot hide a tree -- directly inside directory, and start with the
// signature. Anything else, including a tag that cannot be read, is not a tag
// and the directory is walked. Callers never ask about the repository root.
func IsCacheDirectory(directory string) bool {
	tag := filepath.Join(directory, cacheDirectoryTag)
	info, err := os.Lstat(tag)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(tag)
	if err != nil {
		return false
	}
	defer file.Close()
	prefix := make([]byte, len(cacheDirectorySignature))
	if _, err := io.ReadFull(file, prefix); err != nil {
		return false
	}
	return string(prefix) == cacheDirectorySignature
}
