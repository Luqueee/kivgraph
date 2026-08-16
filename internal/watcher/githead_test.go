package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

const (
	gitHeadTestCommit       = "0123456789abcdef0123456789abcdef01234567"
	gitHeadTestOtherCommit  = "89abcdef0123456789abcdef0123456789abcdef"
	gitHeadTestPackedCommit = "fedcba9876543210fedcba9876543210fedcba98"
)

func TestReadGitHeadResolvesAttachedHeadInGitDirectory(t *testing.T) {
	repository := testsupport.TempDir(t)
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "refs", "heads", "main"), gitHeadTestCommit+"\n")

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	if want := (GitHead{Commit: gitHeadTestCommit, Branch: "main"}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}
}

func TestReadGitHeadFollowsGitDirectoryPointerFile(t *testing.T) {
	root := testsupport.TempDir(t)
	repository := filepath.Join(root, "worktree")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatalf("create worktree directory: %v", err)
	}
	// A linked worktree and a submodule replace .git with a pointer file, and
	// the path it names may be relative to the repository.
	writeGitHeadTestFile(t, filepath.Join(repository, ".git"), "gitdir: ../store/worktrees/wt\n")
	gitDirectory := filepath.Join(root, "store", "worktrees", "wt")
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "HEAD"), "ref: refs/heads/main\n")
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "refs", "heads", "main"), gitHeadTestCommit+"\n")

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	if want := (GitHead{Commit: gitHeadTestCommit, Branch: "main"}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}
}

func TestReadGitHeadResolvesWorktreeReferencesThroughCommonDirectory(t *testing.T) {
	root := testsupport.TempDir(t)
	repository := filepath.Join(root, "worktree")
	commonDirectory := filepath.Join(root, "main", ".git")
	gitDirectory := filepath.Join(commonDirectory, "worktrees", "wt")
	writeGitHeadTestFile(t, filepath.Join(repository, ".git"), "gitdir: "+gitDirectory+"\n")
	// A linked worktree keeps its own HEAD and shares every branch reference
	// with the repository named by commondir.
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "HEAD"), "ref: refs/heads/topic\n")
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "commondir"), "../..\n")
	writeGitHeadTestFile(t, filepath.Join(commonDirectory, "refs", "heads", "topic"), gitHeadTestOtherCommit+"\n")

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	if want := (GitHead{Commit: gitHeadTestOtherCommit, Branch: "topic"}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}
}

func TestReadGitHeadFallsBackToPackedReferences(t *testing.T) {
	repository := testsupport.TempDir(t)
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/feature/x\n")
	// git gc removes the loose reference and leaves only this file, whose
	// header, peeled tag lines and near-miss names must not be mistaken for
	// the record being looked up.
	packed := strings.Join([]string{
		"# pack-refs with: peeled fully-peeled sorted",
		gitHeadTestOtherCommit + " refs/heads/feature/xy",
		gitHeadTestPackedCommit + " refs/heads/feature/x",
		gitHeadTestOtherCommit + " refs/tags/v1",
		"^" + gitHeadTestCommit,
		"",
	}, "\n")
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "packed-refs"), packed)

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	if want := (GitHead{Commit: gitHeadTestPackedCommit, Branch: "feature/x"}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}
}

func TestReadGitHeadReportsDetachedHead(t *testing.T) {
	repository := testsupport.TempDir(t)
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "HEAD"), gitHeadTestCommit+"\n")

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	if want := (GitHead{Commit: gitHeadTestCommit, Detached: true}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}
}

func TestReadGitHeadKeepsSlashesInBranchName(t *testing.T) {
	repository := testsupport.TempDir(t)
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/feature/nested/name\n")
	writeGitHeadTestFile(t, filepath.Join(repository, ".git", "refs", "heads", "feature", "nested", "name"), gitHeadTestCommit+"\n")

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	if want := (GitHead{Commit: gitHeadTestCommit, Branch: "feature/nested/name"}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}
}

func TestReadGitHeadRejectsNonRepositoryAndUnreadableLayouts(t *testing.T) {
	root := testsupport.TempDir(t)
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(filepath.Join(plain, "src"), 0o755); err != nil {
		t.Fatalf("create plain directory: %v", err)
	}
	missingReference := filepath.Join(root, "missing-ref")
	writeGitHeadTestFile(t, filepath.Join(missingReference, ".git", "HEAD"), "ref: refs/heads/gone\n")
	corruptHead := filepath.Join(root, "corrupt-head")
	writeGitHeadTestFile(t, filepath.Join(corruptHead, ".git", "HEAD"), "not a reference\n")
	escapingHead := filepath.Join(root, "escaping-head")
	writeGitHeadTestFile(t, filepath.Join(escapingHead, ".git", "HEAD"), "ref: ../../../etc/passwd\n")
	brokenPointer := filepath.Join(root, "broken-pointer")
	writeGitHeadTestFile(t, filepath.Join(brokenPointer, ".git"), "not a gitdir pointer\n")

	for name, repository := range map[string]string{
		"not a git checkout":     plain,
		"unresolvable reference": missingReference,
		"unparsable HEAD":        corruptHead,
		"reference outside refs": escapingHead,
		"broken gitdir pointer":  brokenPointer,
	} {
		head, err := ReadGitHead(repository)
		if err == nil {
			t.Fatalf("ReadGitHead(%s) = %#v, want an error", name, head)
		}
		if head != (GitHead{}) {
			t.Fatalf("ReadGitHead(%s) = %#v on error, want the zero value", name, head)
		}
		if !strings.Contains(err.Error(), repository) {
			t.Fatalf("ReadGitHead(%s) error = %v, want it to name %q", name, err, repository)
		}
	}
}

func TestGitWatchPathsReturnsExistingDirectoriesOnly(t *testing.T) {
	repository := testsupport.TempDir(t)
	gitDirectory := filepath.Join(repository, ".git")
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "HEAD"), "ref: refs/heads/main\n")
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "refs", "heads", "main"), gitHeadTestCommit+"\n")

	paths, err := GitWatchPaths(repository)
	if err != nil {
		t.Fatalf("GitWatchPaths() error = %v", err)
	}
	want := []string{gitDirectory, filepath.Join(gitDirectory, "refs", "heads")}
	if len(paths) != len(want) {
		t.Fatalf("GitWatchPaths() = %v, want %v", paths, want)
	}
	for index, path := range paths {
		if path != want[index] {
			t.Fatalf("GitWatchPaths()[%d] = %q, want %q", index, path, want[index])
		}
		if !filepath.IsAbs(path) || path != filepath.Clean(path) {
			t.Fatalf("GitWatchPaths()[%d] = %q, want an absolute cleaned path", index, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %q: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("GitWatchPaths()[%d] = %q, want a directory", index, path)
		}
	}
}

func TestGitWatchPathsOmitsMissingBranchDirectory(t *testing.T) {
	repository := testsupport.TempDir(t)
	gitDirectory := filepath.Join(repository, ".git")
	// A repository whose references are fully packed has no refs/heads at all.
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "HEAD"), "ref: refs/heads/main\n")
	writeGitHeadTestFile(t, filepath.Join(gitDirectory, "packed-refs"), gitHeadTestCommit+" refs/heads/main\n")

	paths, err := GitWatchPaths(repository)
	if err != nil {
		t.Fatalf("GitWatchPaths() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != gitDirectory {
		t.Fatalf("GitWatchPaths() = %v, want [%q]", paths, gitDirectory)
	}
}

func TestGitWatchPathsReportsNonRepository(t *testing.T) {
	repository := testsupport.TempDir(t)

	paths, err := GitWatchPaths(repository)
	if err == nil {
		t.Fatalf("GitWatchPaths() = %v, want an error", paths)
	}
	if paths != nil {
		t.Fatalf("GitWatchPaths() = %v on error, want nil", paths)
	}
}

func TestReadGitHeadAgreesWithGitBinaryAfterBranchCheckout(t *testing.T) {
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("skipping the cross-check against the real repository layout: git is not on PATH (%v)", err)
	}
	repository := testsupport.TempDir(t)
	run := func(arguments ...string) string {
		t.Helper()
		command := exec.Command(binary, arguments...)
		command.Dir = repository
		command.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Kivgraph Test",
			"GIT_AUTHOR_EMAIL=test@kivgraph.invalid",
			"GIT_COMMITTER_NAME=Kivgraph Test",
			"GIT_COMMITTER_EMAIL=test@kivgraph.invalid",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", ".")
	run("commit", "--allow-empty", "--message", "initial commit")
	run("checkout", "-b", "feature/branch-with-slash")
	run("commit", "--allow-empty", "--message", "second commit")

	head, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead() error = %v", err)
	}
	wantCommit := run("rev-parse", "HEAD")
	if want := (GitHead{Commit: wantCommit, Branch: "feature/branch-with-slash"}); head != want {
		t.Fatalf("ReadGitHead() = %#v, want %#v", head, want)
	}

	run("checkout", "--detach")
	detached, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead(detached) error = %v", err)
	}
	if want := (GitHead{Commit: wantCommit, Detached: true}); detached != want {
		t.Fatalf("ReadGitHead(detached) = %#v, want %#v", detached, want)
	}

	run("pack-refs", "--all")
	packed, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead(packed) error = %v", err)
	}
	if packed != detached {
		t.Fatalf("ReadGitHead(packed) = %#v, want %#v", packed, detached)
	}
	run("checkout", "feature/branch-with-slash")
	attached, err := ReadGitHead(repository)
	if err != nil {
		t.Fatalf("ReadGitHead(attached) error = %v", err)
	}
	if want := (GitHead{Commit: wantCommit, Branch: "feature/branch-with-slash"}); attached != want {
		t.Fatalf("ReadGitHead(attached) = %#v, want %#v", attached, want)
	}
}

func writeGitHeadTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
