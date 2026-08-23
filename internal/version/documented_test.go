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
	tags[want] = true

	for _, note := range notes {
		if !tags[note] {
			t.Errorf("release note %q names a version that does not exist as a git tag", note)
		}
	}
}
