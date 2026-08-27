//go:build unix

package executable

import (
	"io/fs"
	"path/filepath"
)

// Name returns the file name a program is stored under, which here is the name
// it is called by.
func Name(base string) string { return base }

// IsProgram reports whether info describes a file this platform would run: a
// regular file that somebody has permission to execute.
func IsProgram(info fs.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// BaseName returns the name a program is called by, given the path it is
// stored at. Here the two are the same.
func BaseName(path string) string { return filepath.Base(path) }
