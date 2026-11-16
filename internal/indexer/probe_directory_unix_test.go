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

// The go command and rustup's proxies answer with the toolchain the working
// directory selects. A fingerprint asked from wherever the process happened to
// start described that directory's pin: `go env` in a directory whose go.mod
// wants a newer Go downloaded it, and two processes started in different places
// gave one machine two identities. The stand-ins answer as the real ones do, by
// what is in the directory they are run from, and record where that was.
//
// They are shell scripts, which is why this does not build on Windows.
func installToolchainStandIns(t *testing.T) (record string) {
	t.Helper()
	root := testsupport.TempDir(t)
	bin := filepath.Join(root, "bin")
	record = filepath.Join(root, "directories")
	sysroot := filepath.Join(root, "sysroot")
	for name, script := range map[string]string{
		"go": "echo \"$(pwd -P)\" >> '" + record + "'\n" +
			"if [ -f go.mod ] || [ -f go.work ]; then echo go1.99.0-pinned; else echo go1.26.0-default; fi\n",
		"rustc": "echo \"$(pwd -P)\" >> '" + record + "'\n" +
			"case \"$*\" in *sysroot*) echo '" + sysroot + "';; *)\n" +
			"if [ -f rust-toolchain.toml ]; then echo 'rustc 1.80.0 (pinned)'; else echo 'rustc 1.96.1 (default)'; fi;; esac\n",
		"cargo": "echo \"$(pwd -P)\" >> '" + record + "'\n" +
			"if [ -f rust-toolchain.toml ]; then echo 'cargo 1.80.0 (pinned)'; else echo 'cargo 1.96.1 (default)'; fi\n",
	} {
		writeFullFixture(t, filepath.Join(bin, name), "#!/bin/sh\n"+script)
		if err := os.Chmod(filepath.Join(bin, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("RUSTC", filepath.Join(bin, "rustc"))
	t.Setenv("CARGO_HOME", filepath.Join(root, "cargo-home"))
	return record
}

// pinnedAndPlain are two working directories that select different toolchains.
func pinnedAndPlain(t *testing.T) (pinned, plain string) {
	t.Helper()
	pinned, plain = testsupport.TempDir(t), testsupport.TempDir(t)
	writeFullFixture(t, filepath.Join(pinned, "go.mod"), "module pinned\n\ngo 1.99\n")
	writeFullFixture(t, filepath.Join(pinned, "rust-toolchain.toml"), "[toolchain]\nchannel = \"1.80.0\"\n")
	return pinned, plain
}

// recordedDirectories reads where the stand-ins ran, each once.
func recordedDirectories(t *testing.T, record string) []string {
	t.Helper()
	content, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("no toolchain was asked anything: %v", err)
	}
	directories := strings.Fields(string(content))
	slices.Sort(directories)
	return slices.Compact(directories)
}

func TestGoEnvironmentIsAskedFromADirectoryThatPinsNothing(t *testing.T) {
	record := installToolchainStandIns(t)
	pinned, plain := pinnedAndPlain(t)

	t.Chdir(plain)
	fromPlain, err := goEnvironmentFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(pinned)
	fromPinned, err := goEnvironmentFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fromPlain != fromPinned {
		t.Fatalf("the Go environment depends on where the process started:\nplain  %q\npinned %q", fromPlain, fromPinned)
	}
	// The answer is the default toolchain's and not the pinned directory's, or the
	// two would agree because neither moved.
	if !strings.Contains(fromPinned, "go1.26.0-default") {
		t.Fatalf("Go environment = %q, want the answer of a directory that pins nothing", fromPinned)
	}
	neutral, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := recordedDirectories(t, record); !slices.Equal(got, []string{neutral}) {
		t.Fatalf("go ran in %v, want only %s", got, neutral)
	}
}

func TestRustToolchainIsAskedFromADirectoryThatPinsNothing(t *testing.T) {
	record := installToolchainStandIns(t)
	pinned, plain := pinnedAndPlain(t)
	options := FullOptions{RustAnalyzer: "rust-analyzer", RustSysroot: "discover"}

	t.Chdir(plain)
	fromPlain := rustAnalysisFingerprint(options)
	t.Chdir(pinned)
	fromPinned := rustAnalysisFingerprint(options)
	if fromPlain != fromPinned {
		t.Fatal("the Rust fingerprint depends on where the process started")
	}
	neutral, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Every probe counts: rustc, the sysroot behind it and cargo.
	if got := recordedDirectories(t, record); !slices.Equal(got, []string{neutral}) {
		t.Fatalf("the toolchain was asked from %v, want only %s", got, neutral)
	}
}

// A temporary directory that is not usable must not make a pass fail: the
// fingerprint falls back to the process's own directory, which is what it always
// did, and says what it can.
func TestProbesFallBackToTheProcessDirectoryWhenTheTemporaryOneIsUnusable(t *testing.T) {
	installToolchainStandIns(t)
	pinned, _ := pinnedAndPlain(t)
	for name, value := range map[string]string{
		"missing":  filepath.Join(testsupport.TempDir(t), "gone"),
		"relative": "relative-temporary-directory",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TMPDIR", value)
			t.Chdir(pinned)
			if directory := probeDirectory(); directory != "" {
				t.Fatalf("probeDirectory() = %q with TMPDIR=%q, want the process's own", directory, value)
			}
			got, err := goEnvironmentFingerprint()
			if err != nil {
				t.Fatalf("goEnvironmentFingerprint() error = %v", err)
			}
			if !strings.Contains(got, "go1.99.0-pinned") {
				t.Fatalf("Go environment = %q, want the process directory's answer", got)
			}
		})
	}
}

func TestProbeDirectoryIsTheTemporaryDirectory(t *testing.T) {
	directory := testsupport.TempDir(t)
	t.Setenv("TMPDIR", directory)
	if got := probeDirectory(); got != directory {
		t.Fatalf("probeDirectory() = %q, want %q", got, directory)
	}
}
