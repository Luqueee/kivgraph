package watcher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	gitMarkerName               = ".git"
	gitHeadFileName             = "HEAD"
	gitPackedReferencesFileName = "packed-refs"
	gitCommonDirectoryFileName  = "commondir"
	gitDirectoryPointerPrefix   = "gitdir:"
	gitSymbolicReferencePrefix  = "ref:"
	gitBranchReferencePrefix    = "refs/heads/"
	gitReferenceRootPrefix      = "refs/"
	gitSHA1HexLength            = 40
	gitSHA256HexLength          = 64
	// gitMaximumSymbolicHops bounds a chain of symbolic references so a cycle
	// on disk cannot hang the caller.
	gitMaximumSymbolicHops = 8
)

// GitHead is the resolved position of a repository's HEAD.
type GitHead struct {
	// Commit is the object id HEAD resolves to, lowercase hexadecimal, always
	// populated on success. It is 40 characters in a sha1 repository and 64 in
	// a sha256 one.
	Commit string
	// Branch is the short branch name, empty when HEAD is detached. A HEAD that
	// points outside refs/heads keeps its full reference name, because nothing
	// shorter identifies it unambiguously.
	Branch string
	// Detached reports that HEAD names an object id directly instead of a
	// branch.
	Detached bool
}

// gitLayout locates the two directories a repository resolves HEAD from. They
// differ only for a linked worktree, which keeps its own HEAD and shares refs
// and packed-refs with the repository that created it.
type gitLayout struct {
	gitDirectory    string
	commonDirectory string
}

// ReadGitHead resolves the HEAD of the repository rooted at repositoryPath.
//
// It reads the repository layout directly instead of running git: the watcher
// calls this once per filesystem event batch and a process per check is not
// affordable.
func ReadGitHead(repositoryPath string) (GitHead, error) {
	layout, err := gitLayoutFor(repositoryPath)
	if err != nil {
		return GitHead{}, err
	}
	head, err := readGitFile(filepath.Join(layout.gitDirectory, gitHeadFileName))
	if err != nil {
		return GitHead{}, fmt.Errorf("read HEAD of repository %q: %w", repositoryPath, err)
	}
	if commit, ok := gitObjectID(head); ok {
		return GitHead{Commit: commit, Detached: true}, nil
	}
	reference, ok := strings.CutPrefix(head, gitSymbolicReferencePrefix)
	if !ok {
		return GitHead{}, fmt.Errorf("HEAD of repository %q holds %q, which is neither an object id nor a symbolic reference", repositoryPath, head)
	}
	reference = strings.TrimSpace(reference)
	commit, err := layout.resolveReference(repositoryPath, reference)
	if err != nil {
		return GitHead{}, err
	}
	return GitHead{Commit: commit, Branch: gitShortBranchName(reference)}, nil
}

// GitWatchPaths returns the directories that must be watched for the repository
// rooted at repositoryPath so that a HEAD movement is observed: the git
// directory, which holds HEAD and packed-refs, and refs/heads, which holds the
// branch reference that a commit advances. A linked worktree resolves the last
// two through the common git directory, so that one appears as well.
//
// The result never contains a file, on purpose. Git publishes HEAD and every
// reference by writing a temporary file and renaming it over the target, so the
// path gets a new inode on every move; a watch installed on the file itself
// keeps pointing at the old, already unlinked inode and never reports again.
// Watching the containing directory observes the rename.
//
// Only existing directories are returned, as absolute cleaned paths.
func GitWatchPaths(repositoryPath string) ([]string, error) {
	layout, err := gitLayoutFor(repositoryPath)
	if err != nil {
		return nil, err
	}
	candidates := [...]string{
		layout.gitDirectory,
		layout.commonDirectory,
		filepath.Join(layout.commonDirectory, "refs", "heads"),
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if slices.Contains(paths, candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect git watch path %q of repository %q: %w", candidate, repositoryPath, err)
		}
		if !info.IsDir() {
			continue
		}
		paths = append(paths, candidate)
	}
	return paths, nil
}

// gitLayoutFor resolves the git directory of a repository and the common
// directory its references live in.
func gitLayoutFor(repositoryPath string) (gitLayout, error) {
	if strings.TrimSpace(repositoryPath) == "" {
		return gitLayout{}, errors.New("resolve git layout: the repository path is empty")
	}
	root, err := filepath.Abs(repositoryPath)
	if err != nil {
		return gitLayout{}, fmt.Errorf("resolve repository path %q: %w", repositoryPath, err)
	}
	root = filepath.Clean(root)
	marker := filepath.Join(root, gitMarkerName)
	info, err := os.Stat(marker)
	if err != nil {
		return gitLayout{}, fmt.Errorf("repository %q is not a git checkout: %w", repositoryPath, err)
	}
	gitDirectory := marker
	if !info.IsDir() {
		// A linked worktree and a submodule replace .git with a file holding
		// "gitdir: <path>", where the path may be relative to the repository.
		gitDirectory, err = readGitDirectoryPointer(repositoryPath, marker, root)
		if err != nil {
			return gitLayout{}, err
		}
	}
	layout := gitLayout{gitDirectory: gitDirectory, commonDirectory: gitDirectory}
	common, err := readGitFile(filepath.Join(gitDirectory, gitCommonDirectoryFileName))
	switch {
	case err == nil:
		layout.commonDirectory = resolveGitPath(gitDirectory, common)
	case errors.Is(err, fs.ErrNotExist):
		// A repository that is not a linked worktree resolves its own
		// references and writes no commondir.
	default:
		return gitLayout{}, fmt.Errorf("read commondir of repository %q: %w", repositoryPath, err)
	}
	return layout, nil
}

// readGitDirectoryPointer follows the "gitdir: <path>" file that stands in for
// the git directory of a linked worktree or a submodule.
func readGitDirectoryPointer(repositoryPath, marker, root string) (string, error) {
	contents, err := readGitFile(marker)
	if err != nil {
		return "", fmt.Errorf("read git directory pointer of repository %q: %w", repositoryPath, err)
	}
	target, ok := strings.CutPrefix(contents, gitDirectoryPointerPrefix)
	if !ok {
		return "", fmt.Errorf("git directory pointer of repository %q holds %q, which does not start with %q", repositoryPath, contents, gitDirectoryPointerPrefix)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("git directory pointer of repository %q names no path", repositoryPath)
	}
	gitDirectory := resolveGitPath(root, target)
	info, err := os.Stat(gitDirectory)
	if err != nil {
		return "", fmt.Errorf("read git directory %q of repository %q: %w", gitDirectory, repositoryPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("git directory %q of repository %q is not a directory", gitDirectory, repositoryPath)
	}
	return gitDirectory, nil
}

// resolveReference walks a reference to the object id it names, following a
// bounded chain of symbolic references.
func (layout gitLayout) resolveReference(repositoryPath, reference string) (string, error) {
	current := reference
	for range gitMaximumSymbolicHops {
		if err := validateGitReferenceName(current); err != nil {
			return "", fmt.Errorf("resolve HEAD of repository %q: %w", repositoryPath, err)
		}
		contents, err := readGitFile(filepath.Join(layout.commonDirectory, filepath.FromSlash(current)))
		switch {
		case err == nil:
			if next, ok := strings.CutPrefix(contents, gitSymbolicReferencePrefix); ok {
				current = strings.TrimSpace(next)
				continue
			}
			commit, ok := gitObjectID(contents)
			if !ok {
				return "", fmt.Errorf("reference %q of repository %q holds %q, which is not an object id", current, repositoryPath, contents)
			}
			return commit, nil
		case errors.Is(err, fs.ErrNotExist):
			// git gc packs loose references away, so their absence is normal
			// and packed-refs is the remaining place to look.
			commit, found, packedErr := layout.lookupPackedReference(current)
			if packedErr != nil {
				return "", fmt.Errorf("read packed references of repository %q: %w", repositoryPath, packedErr)
			}
			if !found {
				return "", fmt.Errorf("reference %q of repository %q exists neither as a loose reference nor in packed-refs", current, repositoryPath)
			}
			return commit, nil
		default:
			return "", fmt.Errorf("read reference %q of repository %q: %w", current, repositoryPath, err)
		}
	}
	return "", fmt.Errorf("reference %q of repository %q follows more than %d symbolic references", reference, repositoryPath, gitMaximumSymbolicHops)
}

// lookupPackedReference searches packed-refs for an exact reference name. A
// missing packed-refs file is reported as a miss, not as an error.
func (layout gitLayout) lookupPackedReference(reference string) (string, bool, error) {
	file, err := os.Open(filepath.Join(layout.commonDirectory, gitPackedReferencesFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// "#" opens the header or a comment, and "^" carries the peeled target
		// of the tag on the previous line. Neither names a reference.
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		objectID, name, separated := strings.Cut(line, " ")
		if !separated || strings.TrimSpace(name) != reference {
			continue
		}
		commit, ok := gitObjectID(objectID)
		if !ok {
			return "", false, fmt.Errorf("packed reference %q holds %q, which is not an object id", reference, objectID)
		}
		return commit, true, nil
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

// validateGitReferenceName rejects a name that does not address a reference, so
// a corrupted HEAD cannot make the resolver read outside the git directory.
func validateGitReferenceName(reference string) error {
	if !strings.HasPrefix(reference, gitReferenceRootPrefix) {
		return fmt.Errorf("reference %q does not live under %s", reference, gitReferenceRootPrefix)
	}
	if strings.ContainsAny(reference, "\\\x00") {
		return fmt.Errorf("reference %q contains a character that is not valid in a reference name", reference)
	}
	for _, segment := range strings.Split(reference, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("reference %q has an empty or relative path segment", reference)
		}
	}
	return nil
}

// gitShortBranchName drops the refs/heads/ prefix while preserving the slashes
// inside the branch name itself.
func gitShortBranchName(reference string) string {
	if name, ok := strings.CutPrefix(reference, gitBranchReferencePrefix); ok {
		return name
	}
	return reference
}

// gitObjectID reports whether value is a hexadecimal object id and returns it
// in lowercase, without copying when it already is.
func gitObjectID(value string) (string, bool) {
	if len(value) != gitSHA1HexLength && len(value) != gitSHA256HexLength {
		return "", false
	}
	uppercase := false
	for index := range len(value) {
		switch character := value[index]; {
		case character >= '0' && character <= '9', character >= 'a' && character <= 'f':
		case character >= 'A' && character <= 'F':
			uppercase = true
		default:
			return "", false
		}
	}
	if uppercase {
		return strings.ToLower(value), true
	}
	return value, true
}

// resolveGitPath resolves a path read from a git control file, which git writes
// either absolute or relative to the directory that holds the file.
func resolveGitPath(base, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(base, filepath.FromSlash(value))
}

// readGitFile reads a git control file and trims the trailing newline and any
// surrounding whitespace that git tolerates in it.
func readGitFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(contents)), nil
}

// GitOperationInProgress reports whether git currently holds the index lock of
// the repository.
//
// A checkout is not atomic seen from outside: HEAD moves early and the working
// tree is rewritten after. `.git/index.lock` exists for the duration of the
// operation, which makes it the cheapest honest answer to "is git still
// inside?" -- one stat, no subprocess. A repository whose layout cannot be
// resolved is reported as not busy: the caller already has to handle a HEAD it
// could not read, and inventing a lock would stall it forever.
func GitOperationInProgress(repositoryPath string) bool {
	layout, err := gitLayoutFor(repositoryPath)
	if err != nil {
		return false
	}
	_, statErr := os.Stat(filepath.Join(layout.gitDirectory, "index.lock"))
	return statErr == nil
}

// CommitsHaveIdenticalTrees reports whether two commits of a repository record
// exactly the same files with exactly the same contents.
//
// It is what tells a commit apart from a checkout. Both move HEAD; only one
// changes the code, and rebuilding a corpus to discover that nothing changed
// costs seconds of every commit for a graph identical to the one already
// published.
//
// Unlike ReadGitHead this does run git, and it may: a HEAD movement is rare by
// construction, so the cost is paid once per checkout rather than on every
// poll. Anything it cannot answer -- a commit the repository no longer has,
// git missing from the PATH -- is reported as "not identical", because the
// honest answer to "may I skip the rebuild?" when the question cannot be
// settled is no.
func CommitsHaveIdenticalTrees(ctx context.Context, repositoryPath, from, to string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := gitObjectID(from); !ok {
		return false
	}
	if _, ok := gitObjectID(to); !ok {
		return false
	}
	if from == to {
		return true
	}
	// --quiet exits 0 when the trees agree and 1 when they differ, and
	// writes nothing either way.
	command := exec.CommandContext(ctx, "git", "-C", repositoryPath, "diff", "--quiet", from, to)
	command.Stdout = nil
	command.Stderr = nil
	return command.Run() == nil
}
