package version

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// documentedVersion matches a pinned install command as the documentation
// writes it: `KIVGRAPH_VERSION=v0.5.0 ./scripts/install.sh`. The `v` prefix is
// outside the group because `Value` does not carry it.
var documentedVersion = regexp.MustCompile(`KIVGRAPH_VERSION=v([0-9][^\s"'` + "`" + `]*)`)

// skippedTrees are generated or vendored and never a source of truth.
var skippedTrees = map[string]struct{}{
	".git": {}, "node_modules": {}, "dist": {}, ".tooling": {},
	".astro": {}, ".pnpm-store": {}, "target": {},
}

// isSeparateCheckout reports whether a directory is a checkout of its own.
//
// A git worktree carries `.git` as a **file** naming its gitdir, so the entry
// above -- which only matches directories -- walks straight into one. This
// repository keeps its worktrees under `.claude/worktrees/`, and each of them
// is another branch: the four on this machine pinned `v0.9.1` while `main`
// built `v0.9.2`, and the failure named nine files that are not in this tree
// at all.
//
// That is not the defect this test exists for. A gate that goes red because a
// developer has a second branch checked out is a gate that gets ignored, which
// is how the stale `install.md` it was written for got there.
func isSeparateCheckout(directory string) bool {
	_, err := os.Lstat(filepath.Join(directory, ".git"))
	return err == nil
}

// documentedExtensions are the files a reader or an agent copies a command
// from. A `.go` file that names the variable is code, not an instruction.
var documentedExtensions = map[string]struct{}{
	".md": {}, ".mdx": {}, ".astro": {}, ".sh": {}, ".txt": {},
}

// ledgers record what happened, including a defect quoted with the wrong
// version in it. They instruct nobody to install anything, so holding them to
// the current version would make it impossible to describe a version defect at
// all -- the entry for this very test tripped it.
var ledgers = map[string]struct{}{
	"TASKS.md": {}, "CHANGELOG.md": {},
}

// TestDocumentedInstallVersionMatchesTheBinary keeps every pinned install
// command equal to the version this binary reports.
//
// It exists because the release procedure could not catch a stale one. The
// version lives in more places than the procedure names: `README.md` and
// `docs/installation.md` were current at `v0.5.0` while
// `landing/src/content/docs/install.md` still told a reader to pin `v0.3.0` --
// two minors behind, and a command that installs something else than the page
// around it describes.
//
// So the files are discovered instead of listed. A list would go stale exactly
// the way the procedure did: the next page carrying the command would not be
// checked, and nothing would say so.
func TestDocumentedInstallVersionMatchesTheBinary(t *testing.T) {
	root := filepath.Join("..", "..")
	want := "v" + Value
	type occurrence struct {
		path  string
		line  int
		found string
	}
	occurrences := make([]occurrence, 0, 4)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if _, skipped := skippedTrees[entry.Name()]; skipped {
				return fs.SkipDir
			}
			if path != root && isSeparateCheckout(path) {
				return fs.SkipDir
			}
			return nil
		}
		if _, ledger := ledgers[entry.Name()]; ledger {
			return nil
		}
		if _, documented := documentedExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; !documented {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %q: %w", path, readErr)
		}
		for index, line := range strings.Split(string(data), "\n") {
			for _, match := range documentedVersion.FindAllStringSubmatch(line, -1) {
				occurrences = append(occurrences, occurrence{
					path: filepath.ToSlash(path), line: index + 1, found: "v" + match[1],
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}
	if len(occurrences) == 0 {
		t.Fatalf("no documented install command pins a version; this test guards %q and now guards nothing", want)
	}
	sort.Slice(occurrences, func(left, right int) bool {
		if occurrences[left].path != occurrences[right].path {
			return occurrences[left].path < occurrences[right].path
		}
		return occurrences[left].line < occurrences[right].line
	})
	stale := make([]string, 0)
	for _, found := range occurrences {
		if found.found != want {
			stale = append(stale, fmt.Sprintf("%s:%d pins %s", found.path, found.line, found.found))
		}
	}
	if len(stale) != 0 {
		t.Fatalf("documented install version = %s; %s", want, strings.Join(stale, "; "))
	}
}

func TestReleaseNotes(t *testing.T) {
	want := "v" + Value
	notesDir := filepath.Join("..", "..", "landing", "src", "content", "releases")

	entries, err := os.ReadDir(notesDir)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("release notes directory does not exist: %s", notesDir)
		}
		t.Fatalf("read release notes directory: %v", err)
	}

	var hasCurrent bool
	var notes []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		versionName := strings.TrimSuffix(entry.Name(), ".md")
		notes = append(notes, versionName)

		if versionName == want {
			hasCurrent = true
		}
	}

	if !hasCurrent {
		t.Errorf("missing release note for current version %q", want)
	}

	// A tag list this test cannot read is not evidence that a tag does not
	// exist, and this check has cost three release attempts by assuming it was.
	//
	// It had never run: with one release note, and that note naming the current
	// version, the `tags[want] = true` below answered the question before the
	// tag list was consulted. The second note is what exercised it.
	//
	// A plain `actions/checkout` fetches no tags. A checkout on a tag ref
	// fetches exactly one -- the version being built -- which is a partial list
	// that looks complete. Neither can be fixed with `fetch-tags`: combined
	// with a tag `ref` it makes git fetch the commit and the tag to the same
	// `refs/tags/<tag>` and the checkout itself fails.
	//
	// So this check runs where the tag list is real, which is a developer's
	// clone, and skips in CI rather than failing there. The two guards below
	// are what make that a skip and not a false negative.
	cmd := exec.Command("git", "tag")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Logf("git tag failed, skipping version existence check: %v", err)
		return
	}

	tags := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			tags[line] = true
		}
	}
	switch {
	case len(tags) == 0:
		t.Log("this checkout carries no tags, skipping version existence check")
		return
	case len(tags) == 1 && tags[want]:
		t.Logf("this checkout carries only %s, the version being built, so its tag list is partial: skipping version existence check", want)
		return
	}
	tags[want] = true

	for _, note := range notes {
		if !tags[note] {
			t.Errorf("release note %q names a version that does not exist as a git tag", note)
		}
	}
}
