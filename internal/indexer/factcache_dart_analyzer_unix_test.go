//go:build !windows

package indexer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// The default command is `dart`, so the SDK is whichever one the PATH names, and an
// upgrade, or a PATH that puts another first, changes it without changing a
// character of the configuration. The facts carry what the SDK answers, so the
// fingerprint has to say which one it is. The stand-ins are shell scripts, which is
// why this does not build on Windows.

// installDart puts an executable called name in a directory of its own: it prints the
// version it finds in a file beside it, records what it was asked and where, and
// exits with status.
func installDart(t *testing.T, name, version string, status int) (directory string, asked func() []string) {
	t.Helper()
	directory = testsupport.TempDir(t)
	record := filepath.Join(directory, "asked")
	script := "#!/bin/sh\n" +
		"echo \"$* @ $(pwd -P)\" >> '" + record + "'\n" +
		"if [ " + string(rune('0'+status)) + " -ne 0 ]; then exit " + string(rune('0'+status)) + "; fi\n" +
		"read line < '" + filepath.Join(directory, "version") + "'\necho \"$line\"\n"
	writeFullFixture(t, filepath.Join(directory, name), script)
	if err := os.Chmod(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}
	setDartVersion(t, directory, version)
	return directory, func() []string {
		content, err := os.ReadFile(record)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(content)), "\n")
	}
}

func setDartVersion(t *testing.T, directory, version string) {
	t.Helper()
	writeFullFixture(t, filepath.Join(directory, "version"), version+"\n")
}

func dartPart(t *testing.T, options FullOptions) string {
	t.Helper()
	_, parts, err := observeAnalyzer(options)
	if err != nil {
		t.Fatalf("observeAnalyzer() error = %v", err)
	}
	return parts[analyzerPartDart]
}

func TestDartAnalyzerFingerprintFollowsTheSDKBehindTheCommand(t *testing.T) {
	directory, _ := installDart(t, "dart", "Dart SDK version: 3.12.2", 0)
	t.Setenv("PATH", directory)
	options := FullOptions{DartAnalyzer: "dart"}
	before := dartPart(t, options)
	if again := dartPart(t, options); again != before {
		t.Fatal("the Dart part of the fingerprint is not stable for the same SDK")
	}

	// Upgraded in place: the same path, the same configuration.
	setDartVersion(t, directory, "Dart SDK version: 3.13.1")
	if dartPart(t, options) == before {
		t.Fatal("an upgrade of the SDK behind the same command left the fingerprint as it was")
	}
}

func TestDartAnalyzerFingerprintFollowsThePATH(t *testing.T) {
	first, _ := installDart(t, "dart", "Dart SDK version: 3.12.2", 0)
	second, _ := installDart(t, "dart", "Dart SDK version: 3.13.1", 0)
	options := FullOptions{DartAnalyzer: "dart"}

	t.Setenv("PATH", first)
	fromFirst := dartPart(t, options)
	t.Setenv("PATH", second)
	fromSecond := dartPart(t, options)
	if fromFirst == fromSecond {
		t.Fatal("a PATH that puts another SDK first left the fingerprint as it was")
	}
	// Only that part: the rest of what the PATH names is the same.
	t.Setenv("PATH", first)
	baseline, baselineParts := observedParts(t, options)
	t.Setenv("PATH", second)
	moved, movedParts := observedParts(t, options)
	if got := changedAnalyzerParts(baselineParts, movedParts); !slices.Equal(got, []string{analyzerPartDart}) || baseline == moved {
		t.Fatalf("another SDK moved the parts %q, want only the Dart one", got)
	}
}

// The same SDK reached by two names is the same SDK: what identifies it is where it
// is, resolved.
func TestDartAnalyzerFingerprintResolvesLinks(t *testing.T) {
	directory, _ := installDart(t, "dart", "Dart SDK version: 3.12.2", 0)
	links := testsupport.TempDir(t)
	if err := os.Symlink(filepath.Join(directory, "dart"), filepath.Join(links, "dart")); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	options := FullOptions{DartAnalyzer: "dart"}
	t.Setenv("PATH", directory)
	direct := dartPart(t, options)
	t.Setenv("PATH", links)
	if viaLink := dartPart(t, options); viaLink != direct {
		t.Fatal("the same SDK reached through a link has another identity")
	}
}

// Absent and failing are not the same answer, and neither is a timestamp.
func TestDartAnalyzerFingerprintSaysWhatItCannotFind(t *testing.T) {
	present, _ := installDart(t, "dart", "Dart SDK version: 3.12.2", 0)
	failing, _ := installDart(t, "dart", "Dart SDK version: 3.12.2", 1)
	empty := testsupport.TempDir(t)
	options := FullOptions{DartAnalyzer: "dart"}

	values := make(map[string]string)
	for name, path := range map[string]string{"present": present, "failing": failing, "absent": empty} {
		t.Setenv("PATH", path)
		first := dartPart(t, options)
		if second := dartPart(t, options); second != first {
			t.Fatalf("%s: the fingerprint is not stable across calls", name)
		}
		values[name] = first
	}
	if values["present"] == values["failing"] || values["present"] == values["absent"] || values["failing"] == values["absent"] {
		t.Fatalf("present, failing and absent are not three different facts: %v", values)
	}
	// A configuration that names nothing is not a machine without Dart.
	if none, noneAgain := dartPart(t, FullOptions{}), dartPart(t, FullOptions{}); none != noneAgain || none == values["absent"] {
		t.Fatalf("an unconfigured analyzer fingerprints as %q, want a fact of its own that repeats", none)
	}
}

// One question per observation, from a directory that pins nothing, and nothing
// else asked.
func TestDartAnalyzerFingerprintAsksTheSDKOnceFromANeutralDirectory(t *testing.T) {
	directory, asked := installDart(t, "dart", "Dart SDK version: 3.12.2", 0)
	t.Setenv("PATH", directory)
	if _, _, err := observeAnalyzer(FullOptions{DartAnalyzer: "dart --flag value"}); err != nil {
		t.Fatal(err)
	}
	neutral, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := asked(), []string{"--version @ " + neutral}; !slices.Equal(got, want) {
		t.Fatalf("the SDK was asked %q, want %q: the arguments of the command are not part of the question", got, want)
	}
}

// A command that is not a Dart SDK is a program of the user's: its content is its
// identity, and it is not asked what version it is.
func TestDartAnalyzerFingerprintIsTheContentOfACommandThatIsNotDart(t *testing.T) {
	directory, asked := installDart(t, "my-analyzer", "ignored", 0)
	t.Setenv("PATH", directory)
	options := FullOptions{DartAnalyzer: "my-analyzer"}
	before := dartPart(t, options)

	writeFullFixture(t, filepath.Join(directory, "my-analyzer"), "#!/bin/sh\necho changed\n")
	if err := os.Chmod(filepath.Join(directory, "my-analyzer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if dartPart(t, options) == before {
		t.Fatal("editing the analyzer left the fingerprint as it was")
	}
	if calls := asked(); len(calls) != 0 {
		t.Fatalf("a command that is not Dart was asked %q", calls)
	}
}

// The loader falls back on the SDK path when it is given no command, and so does
// the fingerprint.
func TestDartAnalyzerFingerprintFallsBackOnTheSDKPathLikeTheLoader(t *testing.T) {
	directory, _ := installDart(t, "dart", "Dart SDK version: 3.12.2", 0)
	t.Setenv("PATH", directory)
	options := FullOptions{DartSDKPath: "dart"}
	before := dartPart(t, options)
	setDartVersion(t, directory, "Dart SDK version: 3.13.1")
	if dartPart(t, options) == before {
		t.Fatal("an upgrade of the SDK named only by its path left the fingerprint as it was")
	}
}
