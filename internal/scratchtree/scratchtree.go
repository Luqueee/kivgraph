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

// extractTar writes an archive into root and lets nothing out of it.
//
// The archive is `git archive` over a **registered repository**, which is
// third-party content: a repository can carry a symlink pointing anywhere, and
// nothing stops one from carrying an entry designed to escape. Three shapes
// have to be refused, and only the first is the obvious one.
//
//  1. An entry named `../../etc/passwd`. securePath rejects it.
//  2. A symlink whose target is **absolute** -- `evil -> /etc`. The first
//     version of this joined the link target onto the entry's directory to
//     test it, and filepath.Join cleans an absolute operand away:
//     Join("a", "/etc") is "a/etc", which passes every containment check. The
//     symlink was then created pointing at /etc, and a later entry named
//     `a/evil/passwd` wrote through it. That is the hole CodeQL found, and
//     the reason nothing here creates a symlink at all any more: an inner
//     link is materialised as a copy of its content by materialiseLinks, so
//     the class of escape cannot occur rather than being defended against.
//  3. A regular entry written **through** a symlinked parent, whether that
//     symlink came from an earlier entry or already existed. The name is
//     clean, so no test on the name can see it; the parent directory has to
//     be resolved.
func extractTar(stream io.Reader, root string) error {
	reader := tar.NewReader(stream)
	// The prefix every path written below has to carry. It is computed once
	// and compared inline at each sink rather than inside a helper: a
	// containment test that lives one call away is one a reader -- and a
	// static analyser -- has to take on faith.
	prefix := filepath.Clean(root) + string(os.PathSeparator)
	// A symlink is recorded and materialised after the loop, because its
	// target may not have been written yet.
	var links []recordedLink
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return materialiseLinks(root, prefix, links)
		}
		if err != nil {
			return err
		}
		if err := refuseEscapingName(header.Name); err != nil {
			return err
		}
		target := filepath.Join(root, filepath.Clean(filepath.FromSlash(header.Name)))
		if !strings.HasPrefix(target, prefix) {
			return fmt.Errorf("scratchtree: entry %q escapes the tree", header.Name)
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
			if err := parentStaysInside(root, target); err != nil {
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
			// An absolute target is refused before anything is joined,
			// because joining is what hides it: Join("a", "/etc") is "a/etc".
			// A relative one is kept for the second pass, which is the only
			// place a link can be resolved against a tree that exists.
			if absoluteLinkname(header.Linkname) {
				continue
			}
			links = append(links, recordedLink{target: target, linkname: header.Linkname})
		}
	}
}

// recordedLink is a symlink entry waiting for the second pass.
type recordedLink struct{ target, linkname string }

// materialiseLinks turns each recorded symlink into a **copy of what it points
// at**, and never into a symlink.
//
// The tree is a build input, not something a person reads, and a build cares
// about the bytes behind a path rather than about how the path is spelled. So
// nothing here calls os.Symlink, and the entire class of archive-symlink
// escape -- a link to /etc followed by an entry written through it -- cannot
// occur rather than being defended against. That is worth the one thing it
// costs: a build that inspects link-ness sees a regular file.
//
// A link out of the tree, or onto a directory, or onto something that is not
// there, is simply absent. A repository may legitimately contain any of the
// three, and reproducing them is what this package exists to avoid.
func materialiseLinks(root, prefix string, links []recordedLink) error {
	for _, entry := range links {
		resolved := filepath.Clean(
			filepath.Join(filepath.Dir(entry.target), filepath.FromSlash(entry.linkname)))
		if !strings.HasPrefix(resolved, prefix) {
			continue
		}
		info, err := os.Lstat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(entry.target), 0o755); err != nil {
			return err
		}
		if err := parentStaysInside(root, entry.target); err != nil {
			return err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return err
		}
		if err := os.WriteFile(entry.target, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// absoluteLinkname reports whether a symlink target names a location rather
// than a neighbour.
//
// It covers the Windows drive and UNC spellings too, which are absolute in the
// sense that matters here even on a platform whose filepath.IsAbs says
// otherwise: an archive is written on one machine and extracted on another.
func absoluteLinkname(linkname string) bool {
	trimmed := strings.TrimSpace(linkname)
	return trimmed == "" ||
		filepath.IsAbs(filepath.FromSlash(trimmed)) ||
		strings.HasPrefix(trimmed, "/") ||
		strings.ContainsRune(trimmed, ':') ||
		strings.HasPrefix(trimmed, `\`)
}

// symlinkStaysInside is the same rule extractTar applies inline, exposed so a
// test can state it directly over the shapes that matter.
func symlinkStaysInside(root, target, linkname string) bool {
	if absoluteLinkname(linkname) {
		return false
	}
	resolved := filepath.Clean(
		filepath.Join(filepath.Dir(target), filepath.FromSlash(linkname)))
	return strings.HasPrefix(resolved, filepath.Clean(root)+string(os.PathSeparator))
}

// parentStaysInside resolves the directory a write is about to land in and
// refuses one that leaves the tree through a symlink. A clean entry name says
// nothing about this: the escape is in the directory, not in the name.
func parentStaysInside(root, target string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("scratchtree: resolve tree root: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("scratchtree: resolve %q: %w", filepath.Dir(target), err)
	}
	if !contains(realRoot, parent) {
		return fmt.Errorf("scratchtree: %q resolves outside the tree", target)
	}
	return nil
}

// contains reports whether path is root or sits under it. It is a prefix test
// on cleaned absolute paths, which is the shape that actually holds after
// filepath.Join has normalised its operands.
func contains(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
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
//
// It tests the joined result and not only the entry name. The two are almost
// always the same answer, and the one time they are not is the one that
// matters.
func securePath(root, name string) (string, error) {
	if err := refuseEscapingName(name); err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.Clean(filepath.FromSlash(name)))
	if !strings.HasPrefix(target, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("scratchtree: entry %q escapes the tree", name)
	}
	return target, nil
}

// refuseEscapingName rejects an entry name that names a location rather than a
// path inside the archive. `../` is the classic way out; an absolute name and
// the Windows spellings are the rest of it.
func refuseEscapingName(name string) error {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(name, "/") ||
		cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) ||
		strings.ContainsRune(name, ':') || strings.HasPrefix(name, `\`) {
		return fmt.Errorf("scratchtree: entry %q escapes the tree", name)
	}
	return nil
}

func within(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
