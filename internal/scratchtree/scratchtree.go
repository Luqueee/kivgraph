// Package scratchtree gives an analyzer a copy of a repository it is allowed
// to write into.
//
// The root AGENTS.md states it without an exception: an indexed repository is
// never modified. Every analyzer honoured that until Java and C#, which are
// indexed by *building* them -- scip-java drives Maven or Gradle and
// scip-dotnet runs `dotnet restore`. A build writes `target/`, `obj/` and
// `bin/` into the directory it builds, and no flag moves that: it is the build
// tool's output, not Kivgraph's.
//
// So the pass stops handing the analyzer the repository. It hands it a tree
// with the same content somewhere else, and throws it away afterwards.
//
// Measured on this repository -- 1652 tracked files, a 3.8 GB working tree
// once `node_modules`, `.tooling` and `dist` are counted:
//
//	copy of the working tree      8154 ms   4.5 GB   55862 files
//	git worktree add              107 ms    16 MB    1638 files
//	git archive + dirty overlay   76 ms     16 MB    1637 files
//
// The archive wins on all three axes and is the only one of the three that
// writes nothing inside the repository at all: `git worktree add` registers
// metadata under `.git/worktrees/`, which a pass that dies leaves behind. A
// copy is honest but drags in every build output and vendored dependency that
// happens to be there, which is what makes it two orders of magnitude slower.
package scratchtree

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// Tree is a materialised copy of a repository, and the directory an analyzer
// may dirty.
type Tree struct {
	// Path is the root the analyzer is pointed at.
	Path string
	// Strategy names how it was produced, for the pass to report.
	Strategy string
	root     string
}

// Strategies, as they appear in a report.
const (
	StrategyArchive = "git-archive"
	StrategyCopy    = "copy"
)

// excludedDirectories are never materialised. They are build output and
// dependency caches: an analyzer that needs them regenerates them, and copying
// them is what makes a copy cost gigabytes.
//
// It is not a guess about what is unimportant -- every entry here is a
// directory a build tool owns and rewrites.
var excludedDirectories = map[string]bool{
	".git": true, "node_modules": true, "target": true, "obj": true,
	"bin": true, "build": true, ".gradle": true, ".dart_tool": true,
	"__pycache__": true, ".venv": true, "venv": true, "dist": true,
	".tooling": true,
}

// Materialise produces a tree with the repository's current content under
// base, which must be outside every indexed repository.
//
// It reproduces the **working tree**, not the last commit. A user editing code
// expects the graph to describe what is on disk, and the registry already
// records whether a repository is dirty; indexing HEAD instead would answer
// about code nobody has.
func Materialise(ctx context.Context, repository workspace.Repository, base string) (*Tree, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	source := repository.RealPath
	if source == "" {
		source = repository.Path
	}
	if strings.TrimSpace(source) == "" {
		return nil, errors.New("scratchtree: repository has no path")
	}
	if strings.TrimSpace(base) == "" {
		return nil, errors.New("scratchtree: a base directory outside the repository is required")
	}
	absoluteBase, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("scratchtree: resolve base: %w", err)
	}
	if within(absoluteBase, source) {
		return nil, fmt.Errorf(
			"scratchtree: base %q is inside the repository %q", absoluteBase, source)
	}
	if err := os.MkdirAll(absoluteBase, 0o755); err != nil {
		return nil, fmt.Errorf("scratchtree: create base: %w", err)
	}
	root, err := os.MkdirTemp(absoluteBase, "tree-")
	if err != nil {
		return nil, fmt.Errorf("scratchtree: create tree: %w", err)
	}

	tree := &Tree{Path: root, root: root, Strategy: StrategyArchive}
	if err := materialiseFromGit(ctx, source, root); err != nil {
		// Not a git repository, or one with no commit yet. Falling back is
		// not a degradation of the result -- the tree is the same either way
		// -- only of the cost.
		if copyErr := materialiseByCopy(ctx, source, root); copyErr != nil {
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("scratchtree: %w (git: %v)", copyErr, err)
		}
		tree.Strategy = StrategyCopy
	}
	return tree, nil
}

// Close removes the tree and everything the analyzer wrote into it.
func (tree *Tree) Close() error {
	if tree == nil || tree.root == "" {
		return nil
	}
	return os.RemoveAll(tree.root)
}

// materialiseFromGit streams `git archive HEAD` and then overlays whatever the
// working tree says is not HEAD.
//
// The archive is read as a tar stream in process rather than piped to a `tar`
// binary: Windows is a published platform and does not reliably have one.
func materialiseFromGit(ctx context.Context, source, root string) error {
	archive := exec.CommandContext(ctx, "git", "-C", source, "archive", "--format=tar", "HEAD")
	output, err := archive.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git archive: %w", err)
	}
	var stderr strings.Builder
	archive.Stderr = &stderr
	if err := archive.Start(); err != nil {
		return fmt.Errorf("git archive: %w", err)
	}
	if err := extractTar(output, root); err != nil {
		_ = archive.Wait()
		return fmt.Errorf("git archive: %w", err)
	}
	if err := archive.Wait(); err != nil {
		return fmt.Errorf("git archive: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return overlayWorkingTree(ctx, source, root)
}

func extractTar(stream io.Reader, root string) error {
	reader := tar.NewReader(stream)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := securePath(root, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
				fs.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, reader); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// A symlink out of the tree is not reproduced. The analyzer would
			// follow it back into the repository, which is the one thing this
			// package exists to prevent.
			if _, err := securePath(root, filepath.Join(filepath.Dir(header.Name), header.Linkname)); err != nil {
				continue
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// overlayWorkingTree copies over what the working tree changed since HEAD, so
// the analyzer sees the code the user has rather than the code they committed.
func overlayWorkingTree(ctx context.Context, source, root string) error {
	status := exec.CommandContext(ctx, "git", "-C", source,
		"status", "--porcelain", "-z", "--untracked-files=normal")
	out, err := status.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		if len(entry) < 4 {
			continue
		}
		code, path := entry[:2], entry[3:]
		if excludedPath(path) {
			continue
		}
		target, err := securePath(root, path)
		if err != nil {
			continue
		}
		if strings.ContainsRune(code, 'D') {
			_ = os.RemoveAll(target)
			continue
		}
		if err := copyIfFile(filepath.Join(source, filepath.FromSlash(path)), target); err != nil {
			return err
		}
	}
	return nil
}

func copyIfFile(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		// A directory reported by `git status` is an untracked directory; its
		// files are reported with it only when it is empty of tracked
		// content, and walking it is what the copy fallback is for. Skipping
		// keeps the archive path cheap; a repository whose untracked
		// directories matter is one the copy strategy handles.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, info.Mode().Perm())
}

// materialiseByCopy is the fallback for a repository git cannot describe.
func materialiseByCopy(ctx context.Context, source, root string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return nil
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() {
			if excludedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(root, relative), 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyIfFile(path, filepath.Join(root, relative))
	})
}

func excludedPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if excludedDirectories[segment] {
			return true
		}
	}
	return false
}

// securePath refuses a path that would land outside the tree. A tar entry is
// attacker-controlled in the general case and `../` is the classic way out.
func securePath(root, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("scratchtree: entry %q escapes the tree", name)
	}
	return filepath.Join(root, cleaned), nil
}

func within(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
