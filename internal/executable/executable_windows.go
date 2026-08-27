//go:build windows

package executable

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Name returns the file name a program is stored under, which here carries the
// extension that makes it one. A caller that already spelled the extension
// gets its name back rather than "kivgraph.exe.exe".
func Name(base string) string {
	if base == "" {
		return ""
	}
	if extension := filepath.Ext(base); isProgramExtension(extension) {
		return base
	}
	return base + ".exe"
}

// IsProgram reports whether info describes a file this platform would run.
//
// The question is answered from the name, because there is nothing else to
// answer it from: the mode bits Go reports on Windows describe the read-only
// attribute and nothing about execution, so a permission test here would
// refuse every program there is.
func IsProgram(info fs.FileInfo) bool {
	return info.Mode().IsRegular() && isProgramExtension(filepath.Ext(info.Name()))
}

// isProgramExtension reads PATHEXT, which is what the shell and CreateProcess
// consult, rather than assuming the list is ".exe". An environment that has
// narrowed or widened it is describing this machine, and guessing over it
// would disagree with whatever actually runs the file.
func isProgramExtension(extension string) bool {
	if extension == "" {
		return false
	}
	list := os.Getenv("PATHEXT")
	if strings.TrimSpace(list) == "" {
		list = ".COM;.EXE;.BAT;.CMD"
	}
	for _, candidate := range strings.Split(list, ";") {
		if candidate = strings.TrimSpace(candidate); candidate == "" {
			continue
		}
		if strings.EqualFold(candidate, extension) {
			return true
		}
	}
	return false
}

// BaseName returns the name a program is called by, given the path it is
// stored at, which here means without the extension that made it runnable.
//
// It is the inverse of Name, and it matters wherever code compares an observed
// program against one it expects: a process table reports "kivgraph.exe", and
// every caller that knows the program as "kivgraph" would otherwise have to
// remember which of the two it was holding. `stop` did not, and quietly
// stopped nothing.
//
// Only a program extension is removed. A file called "graph.db" keeps its
// suffix, because that suffix is part of what it is called.
func BaseName(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if extension := filepath.Ext(base); isProgramExtension(extension) {
		return strings.TrimSuffix(base, extension)
	}
	return base
}
