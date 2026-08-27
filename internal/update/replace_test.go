package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/executable"
)

// holdVariable makes this test binary act as an installed kivgraph that is
// still running: it blocks until it is killed. The point is not what it does
// but that the operating system has an executing image open on a file inside
// the bundle about to be replaced.
//
// Windows keeps such an image open and refuses to unlink it. What it does
// about renaming the *directory* that contains it is the question replaceBundle
// turns on, and it is not a question worth reasoning about -- an update that
// cannot move the old bundle aside cannot install anything, and an update that
// can but then fails to delete the backup would report a failure after having
// succeeded. So it is measured here instead.
// The variable carries the path of a file the child creates once it is
// running, because "has the image been mapped yet" is the thing being waited
// for and a process that has been started is not yet a process that has been
// loaded.
const holdVariable = "KIVGRAPH_TEST_HOLD_UNTIL_KILLED"

// bundleVersionVariable makes this test binary answer `version` the way an
// installed kivgraph does, so that it can stand in for the binary inside a
// fixture bundle. validateBundle runs that binary and compares its output
// against the release, which a shell script cannot satisfy on Windows.
const bundleVersionVariable = "KIVGRAPH_TEST_BUNDLE_VERSION"

func TestMain(m *testing.M) {
	if version := os.Getenv(bundleVersionVariable); version != "" {
		fmt.Println(version)
		os.Exit(0)
	}
	if ready := os.Getenv(holdVariable); ready != "" {
		if err := os.WriteFile(ready, []byte("running"), 0o600); err != nil {
			os.Exit(2)
		}
		select {}
	}
	os.Exit(m.Run())
}

func copyProgram(t *testing.T, destination string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary: %v", err)
	}
	source, err := os.Open(self)
	if err != nil {
		t.Fatalf("open the test binary: %v", err)
	}
	defer source.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatalf("create %q: %v", destination, err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		t.Fatalf("copy the test binary: %v", copyErr)
	}
	if closeErr != nil {
		t.Fatalf("close %q: %v", destination, closeErr)
	}
}

func TestReplaceBundleMovesABundleWhoseBinaryIsStillRunning(t *testing.T) {
	parent := t.TempDir()
	current := filepath.Join(parent, "kivgraph")
	staged := filepath.Join(parent, "staged", "kivgraph")

	program := filepath.Join(current, "bin", executable.Name("kivgraph"))
	copyProgram(t, program)
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatalf("create staged bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staged, "manifest.json"), []byte(`{"product":"kivgraph"}`), 0o644); err != nil {
		t.Fatalf("write staged manifest: %v", err)
	}

	ready := filepath.Join(parent, "running")
	command := exec.Command(program)
	command.Env = append(os.Environ(), holdVariable+"="+ready)
	if err := command.Start(); err != nil {
		t.Fatalf("start the installed binary: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	// The image has to be mapped before the rename or the test measures
	// nothing, and a started process is not yet a loaded one.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the installed binary never reported itself running")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := replaceBundle(current, staged); err != nil {
		t.Fatalf("replaceBundle() error = %v: an update cannot install while the binary it replaces runs", err)
	}

	if _, err := os.Stat(filepath.Join(current, "manifest.json")); err != nil {
		t.Fatalf("staged bundle is not in place: %v", err)
	}
	if _, err := os.Stat(program); err == nil {
		t.Fatal("the old binary is still at the installed path, so nothing was replaced")
	}
}

// The backup is what the caller sees afterwards, and the two platforms differ:
// a Unix unlink detaches the name from a running image and the directory goes,
// while Windows refuses and the backup stays for the next update to clear.
// Both are successful updates; only one leaves a directory behind.
func TestReplaceBundleLeavesNoBackupExceptWhereItCannot(t *testing.T) {
	parent := t.TempDir()
	current := filepath.Join(parent, "kivgraph")
	staged := filepath.Join(parent, "staged", "kivgraph")
	for _, directory := range []string{current, staged} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %q: %v", directory, err)
		}
	}

	if err := replaceBundle(current, staged); err != nil {
		t.Fatalf("replaceBundle() error = %v", err)
	}
	// Nothing was running out of this bundle, so the backup goes on every
	// platform. A leftover here would mean removeReplacedBundle is swallowing
	// a failure that has nothing to do with a locked image.
	if _, err := os.Stat(current + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("stat backup = %v, want it removed when nothing holds it", err)
	}
}

// A backup an earlier update could not delete must not block the next one.
// On Windows that is the ordinary state of an installation that has updated
// once, so refusing on it would cap every machine at a single update.
func TestReplaceBundleClearsABackupAnEarlierUpdateLeft(t *testing.T) {
	parent := t.TempDir()
	current := filepath.Join(parent, "kivgraph")
	staged := filepath.Join(parent, "staged", "kivgraph")
	for _, directory := range []string{current, staged, current + ".previous"} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %q: %v", directory, err)
		}
	}
	if err := os.WriteFile(filepath.Join(current+".previous", "stale"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write stale backup: %v", err)
	}

	if err := replaceBundle(current, staged); err != nil {
		t.Fatalf("replaceBundle() error = %v, want the stale backup cleared rather than refused", err)
	}
}
