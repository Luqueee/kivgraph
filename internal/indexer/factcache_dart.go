package indexer

import (
	"context"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/dartloader"
	"github.com/Luqueee/kivgraph/internal/executable"
)

// dartProviderRoots are the roots of the registered repositories whose files a Dart
// unit can read: the ones that hold a package its package config names, which is all
// the analysis server reads besides the repository itself, and the ones its facts
// name a target in.
//
// The loader is handed every registered repository as a provider, whatever language
// it was registered under, and the facts of a consumer name symbols of the one a
// reference lands in. The semantic registry digests the trees of the repositories
// that declare a semantic language, so a provider registered under another one could
// change, or be registered after the consumer was analysed, without the consumer's
// entry noticing: measured, the consumer was served with the old symbol.
//
// The unit's address takes the first of them, which are known before it is analysed,
// so a provider that appears is another address; the entry records the second as
// inputs to check when it is served.
func dartProviderRoots(unit analysisUnit, options FullOptions, composed map[string]composedTarget) []string {
	own := unit.repository.RealPath
	if own == "" {
		own = unit.repository.Path
	}
	packages := dartloader.PackageRoots(own, options.DartPackageConfig)
	for index, root := range packages {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			packages[index] = resolved
		}
	}
	named := make(map[string]struct{}, len(composed))
	for _, target := range composed {
		named[target.Repository] = struct{}{}
	}
	roots := make([]string, 0)
	for _, repository := range options.Repositories {
		if repository.Name == unit.repository.Name {
			continue
		}
		root := repository.RealPath
		if root == "" {
			root = repository.Path
		}
		root = filepath.Clean(root)
		_, composedInto := named[repository.Name]
		if composedInto || holdsAnyOf(root, packages) {
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots
}

// holdsAnyOf reports whether any of the paths is the root or inside it. A directory
// that merely starts with the same characters is not inside it.
func holdsAnyOf(root string, paths []string) bool {
	for _, path := range paths {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// dartVersionTimeout bounds the question put to the Dart SDK. A `dart` that is a
// wrapper script may have to fetch its SDK before it answers.
const dartVersionTimeout = 30 * time.Second

// dartAnalyzerFingerprint identifies the Dart analyzer this pass would start, which
// the options alone cannot: the default command is `dart`, so the SDK is whichever
// one the PATH names, and an upgrade, or a PATH that puts another first, changes
// it without changing a character of the configuration. The facts carry what the
// SDK answers -- the unresolved ones name the file of its core library they were
// looked up in -- so an entry written under one SDK is not the answer under another.
//
// It resolves the command the way the loader does. A `dart` is identified by the
// path it resolves to and what `--version` says, asked from a directory that pins
// nothing: the file of the command is not enough, because Flutter's is a script
// that starts an SDK it keeps elsewhere. Any other command is a program of the
// user's, and its content is its identity. A command that is not there is `absent`,
// and one that does not answer `failed`: determinate facts that repeat, since a
// timestamp here would turn the whole fingerprint into one that never repeats and
// switch the fact cache off for every language.
func dartAnalyzerFingerprint(options FullOptions) string {
	fields := strings.Fields(strings.TrimSpace(options.DartAnalyzer))
	if len(fields) == 0 {
		fields = strings.Fields(strings.TrimSpace(options.DartSDKPath))
	}
	if len(fields) == 0 {
		return "unconfigured"
	}
	resolved, err := exec.LookPath(fields[0])
	if err != nil {
		return "absent"
	}
	location := resolved
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		location = real
	}
	if executable.BaseName(resolved) != "dart" {
		return "program\x00" + fileFingerprint(location)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dartVersionTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, resolved, "--version")
	command.Dir = probeDirectory()
	output, err := command.CombinedOutput()
	if err != nil {
		return location + "\x00failed"
	}
	return location + "\x00" + strings.Join(strings.Fields(string(output)), " ")
}
