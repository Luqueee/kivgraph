package gitdiff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// checkout makes a git repository with one commit holding files, and answers
// its root. The environment is pinned so the machine's git configuration cannot
// change what is committed or how a diff is printed.
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, root, "init", "-q", "-b", "main")
	write(t, root, files)
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "base")
	return root
}

func run(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{
		"-C", root, "-c", "user.name=t", "-c", "user.email=t@t",
		"-c", "commit.gpgsign=false",
	}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func lines(count int) string {
	var builder strings.Builder
	for index := 1; index <= count; index++ {
		builder.WriteString("line ")
		builder.WriteString(string(rune('a' + index%26)))
		builder.WriteString("\n")
	}
	return builder.String()
}

func compare(t *testing.T, root, ref string) Diff {
	t.Helper()
	diff, err := Compare(context.Background(), root, ref)
	if err != nil {
		t.Fatalf("Compare(%q) error = %v", ref, err)
	}
	return diff
}

func changeFor(t *testing.T, diff Diff, path string) Change {
	t.Helper()
	for _, change := range diff.Changes {
		if change.Path == path {
			return change
		}
	}
	t.Fatalf("no change for %q in %#v", path, diff.Changes)
	return Change{}
}

func TestCompareRefusesWhatIsNotACommit(t *testing.T) {
	root := checkout(t, map[string]string{"a.go": lines(3)})
	for _, ref := range []string{"", "  ", "no-such-branch", "--output=/tmp/x", "-p", "main:a.go"} {
		_, err := Compare(context.Background(), root, ref)
		if !errors.Is(err, ErrUnknownRef) {
			t.Errorf("Compare(%q) error = %v, want ErrUnknownRef", ref, err)
		}
	}
	// The message names the ref: a typo has to be findable.
	if _, err := Compare(context.Background(), root, "origin/mian"); err == nil || !strings.Contains(err.Error(), "origin/mian") {
		t.Errorf("error %v does not name the ref", err)
	}
}

func TestRootOfADirectoryOutsideAnyCheckoutIsAnError(t *testing.T) {
	if _, err := Root(context.Background(), t.TempDir()); err == nil {
		t.Fatal("a directory outside a checkout has a root")
	}
	root := checkout(t, map[string]string{"sub/a.go": lines(1)})
	got, err := Root(context.Background(), filepath.Join(root, "sub"))
	if err != nil || got != root {
		t.Fatalf("Root(sub) = %q, %v, want %q", got, err, root)
	}
}

func TestComparingACleanCheckoutFindsNothing(t *testing.T) {
	root := checkout(t, map[string]string{"a.go": lines(3)})
	diff := compare(t, root, "HEAD")
	if len(diff.Changes) != 0 || len(diff.BaseCommit) != 40 {
		t.Fatalf("diff = %#v, want no change and a full commit id", diff)
	}
}

// The ranges are the lines of the working tree, which is what the graph indexed
// when it is fresh, so the new side of each hunk is what is read.
func TestCompareReadsTheNewSideOfEachHunk(t *testing.T) {
	base := lines(20)
	root := checkout(t, map[string]string{"a.go": base})
	edited := strings.Split(strings.TrimSuffix(base, "\n"), "\n")
	edited[4] = "CHANGED five"                                                                // line 5, one line replaced
	edited = append(edited[:9], append([]string{"ADDED one", "ADDED two"}, edited[9:]...)...) // lines 10-11 inserted
	write(t, root, map[string]string{"a.go": strings.Join(edited, "\n") + "\n"})

	got := changeFor(t, compare(t, root, "HEAD"), "a.go")
	want := []Range{{Start: 5, End: 5}, {Start: 10, End: 11}}
	if !reflect.DeepEqual(got.Ranges, want) || got.Deleted {
		t.Fatalf("change = %#v, want ranges %v", got, want)
	}
}

func TestARemovalBelongsToTheSymbolsThatHoldBothSidesOfIt(t *testing.T) {
	root := checkout(t, map[string]string{"a.go": lines(10), "top.go": lines(5)})
	body := strings.Split(strings.TrimSuffix(lines(10), "\n"), "\n")
	write(t, root, map[string]string{
		"a.go":   strings.Join(append(append([]string{}, body[:4]...), body[5:]...), "\n") + "\n",        // line 5 removed
		"top.go": strings.Join(strings.Split(strings.TrimSuffix(lines(5), "\n"), "\n")[1:], "\n") + "\n", // line 1 removed
	})
	diff := compare(t, root, "HEAD")
	if got := changeFor(t, diff, "a.go").Ranges; !reflect.DeepEqual(got, []Range{{Start: 4, End: 5, Between: true}}) {
		t.Fatalf("a.go ranges = %v, want the gap between lines 4 and 5", got)
	}
	// Nothing precedes the first line, so the gap is anchored on it.
	if got := changeFor(t, diff, "top.go").Ranges; !reflect.DeepEqual(got, []Range{{Start: 1, End: 2, Between: true}}) {
		t.Fatalf("top.go ranges = %v, want the gap at the top", got)
	}
}

func TestARangeOverlapsExactlyTheSymbolsItTouches(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		changed     Range
		first, last int
		want        bool
	}{
		{"inside", Range{Start: 5, End: 6}, 1, 10, true},
		{"the first line", Range{Start: 1, End: 1}, 1, 10, true},
		{"the last line", Range{Start: 10, End: 10}, 1, 10, true},
		{"one line before", Range{Start: 4, End: 4}, 5, 10, false},
		{"one line after", Range{Start: 11, End: 11}, 5, 10, false},
		{"spanning the symbol", Range{Start: 1, End: 99}, 5, 10, true},
		{"a whole file", Range{Start: 1, End: WholeFile}, 5, 10, true},
		{"a gap inside", Range{Start: 6, End: 7, Between: true}, 5, 10, true},
		{"a gap between two symbols", Range{Start: 10, End: 11, Between: true}, 5, 10, false},
		{"a gap at the end of a symbol", Range{Start: 10, End: 11, Between: true}, 5, 11, true},
	} {
		if got := testCase.changed.Overlaps(testCase.first, testCase.last); got != testCase.want {
			t.Errorf("%s: Overlaps(%d, %d) = %v, want %v", testCase.name, testCase.first, testCase.last, got, testCase.want)
		}
	}
}

func TestCompareSeesDeletedNewStagedAndUntrackedFiles(t *testing.T) {
	root := checkout(t, map[string]string{"gone.go": lines(3), "kept.go": lines(3), ".gitignore": "ignored.go\n"})
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	write(t, root, map[string]string{"staged.go": lines(4), "untracked.go": lines(2), "ignored.go": lines(2)})
	run(t, root, "add", "staged.go")

	diff := compare(t, root, "HEAD")
	if got := changeFor(t, diff, "gone.go"); !got.Deleted || len(got.Ranges) != 0 {
		t.Errorf("gone.go = %#v, want a deleted file with no lines to map", got)
	}
	if got := changeFor(t, diff, "staged.go").Ranges; !reflect.DeepEqual(got, []Range{{Start: 1, End: 4}}) {
		t.Errorf("staged.go ranges = %v, want its four lines", got)
	}
	if got := changeFor(t, diff, "untracked.go"); !got.Untracked || !reflect.DeepEqual(got.Ranges, []Range{{Start: 1, End: WholeFile}}) {
		t.Errorf("untracked.go = %#v, want the whole file, marked untracked", got)
	}
	if changeFor(t, diff, "staged.go").Untracked || changeFor(t, diff, "gone.go").Untracked {
		t.Error("a file git knows about is marked untracked")
	}
	for _, change := range diff.Changes {
		if change.Path == "ignored.go" || change.Path == "kept.go" {
			t.Errorf("%s is reported, and neither is a change", change.Path)
		}
	}
}

// A moved file is read as a deletion and an addition, so each side lands on the
// path the graph knows it by.
func TestARenamedFileIsADeletionAndAnAddition(t *testing.T) {
	root := checkout(t, map[string]string{"old.go": lines(6)})
	run(t, root, "mv", "old.go", "new.go")
	diff := compare(t, root, "HEAD")
	if !changeFor(t, diff, "old.go").Deleted {
		t.Errorf("old.go = %#v, want a deleted file", changeFor(t, diff, "old.go"))
	}
	if got := changeFor(t, diff, "new.go").Ranges; !reflect.DeepEqual(got, []Range{{Start: 1, End: 6}}) {
		t.Errorf("new.go ranges = %v, want all six lines", got)
	}
}

func TestPathsKeepTheirSpacesAndNonASCIICharacters(t *testing.T) {
	root := checkout(t, map[string]string{"dir with space/ñandú.go": lines(2)})
	write(t, root, map[string]string{"dir with space/ñandú.go": lines(3)})
	got := changeFor(t, compare(t, root, "HEAD"), "dir with space/ñandú.go")
	if !reflect.DeepEqual(got.Ranges, []Range{{Start: 3, End: 3}}) {
		t.Fatalf("ranges = %v, want the added third line", got.Ranges)
	}
}

// A removed line that begins with "-- " is printed "--- ...", and an added one
// that begins with "++ " is printed "+++ ...": both look like the file headers
// of a diff. Reading them as headers invented a file that was never changed and
// dropped the lines that were.
func TestALineThatLooksLikeAFileHeaderIsContent(t *testing.T) {
	root := checkout(t, map[string]string{"query.sql": "-- the comment\nselect 1;\n", "other.go": lines(3)})
	write(t, root, map[string]string{"query.sql": "++ new\nselect 1;\n"})
	diff := compare(t, root, "HEAD")
	if len(diff.Changes) != 1 {
		t.Fatalf("changes = %#v, want only query.sql", diff.Changes)
	}
	if got := changeFor(t, diff, "query.sql"); got.Deleted || !reflect.DeepEqual(got.Ranges, []Range{{Start: 1, End: 1}}) {
		t.Fatalf("query.sql = %#v, want line 1 changed", got)
	}
}

func TestCompareAgainstAnEarlierCommitSeesWhatLandedSince(t *testing.T) {
	root := checkout(t, map[string]string{"a.go": lines(3)})
	write(t, root, map[string]string{"a.go": lines(5)})
	run(t, root, "commit", "-q", "-a", "-m", "second")
	if got := compare(t, root, "HEAD").Changes; len(got) != 0 {
		t.Fatalf("against HEAD: %#v, want nothing", got)
	}
	if got := changeFor(t, compare(t, root, "HEAD~1"), "a.go").Ranges; !reflect.DeepEqual(got, []Range{{Start: 4, End: 5}}) {
		t.Fatalf("against HEAD~1: ranges = %v, want lines 4-5", got)
	}
}
