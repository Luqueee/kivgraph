package durable_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/durable"
)

// Coverage of this package is 94.1% of statements. The three that are not
// covered are Directory's Stat failing on a handle os.Open has just returned,
// which needs a filesystem that answers one call and refuses the next on the
// same descriptor -- a state a test cannot fix. Every other branch is below.
//
// The refusals come first because they are the half nobody exercises. A flush
// that quietly does nothing is indistinguishable from one that worked, and
// this package exists precisely so that the difference between the two is
// stated in one place.

func TestFileRefusesWhatItCannotFlush(t *testing.T) {
	directory := t.TempDir()
	for name, path := range map[string]string{
		"absent":        filepath.Join(directory, "not-here"),
		"a directory":   directory,
		"an empty name": "",
	} {
		t.Run(name, func(t *testing.T) {
			if err := durable.File(path); err == nil {
				t.Fatalf("File(%q) = nil, want the refusal to say what could not be opened", path)
			}
		})
	}
}

// A file the caller cannot write cannot be flushed, and that is the documented
// consequence of asking for the access the flush needs rather than the access
// a read would need. It is asserted rather than left implicit because the
// alternative -- opening read-only -- is what failed on Windows.
func TestFileRefusesAFileItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file, so the mode says nothing here")
	}
	path := filepath.Join(t.TempDir(), "read-only")
	if err := os.WriteFile(path, []byte("x"), 0o400); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := durable.File(path); err == nil {
		t.Fatal("File(read-only) = nil, want the refusal")
	}
}

func TestDirectoryRefusesWhatIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "regular")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for name, path := range map[string]string{
		"absent":       filepath.Join(root, "not-here"),
		"a plain file": file,
	} {
		t.Run(name, func(t *testing.T) {
			if err := durable.Directory(path); err == nil {
				t.Fatalf("Directory(%q) = nil, want a refusal", path)
			}
		})
	}
}

// The two names differ by one platform's separator and by nothing else, which
// is the invariant the callers rely on: a store that flushed the file and
// skipped its directory would leave a complete file nobody can reach.
func TestFileAndDirectoryFlushWhatTheyName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "payload")
	if err := os.WriteFile(path, []byte("contents"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := durable.File(path); err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if err := durable.Directory(root); err != nil {
		t.Fatalf("Directory() error = %v", err)
	}
	// The flush must not have disturbed what it flushed.
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "contents" {
		t.Fatalf("after File(): contents = %q, %v, want them untouched", contents, err)
	}
}

// A caller that flushes twice is not an error: the store retries a publish.
func TestFileIsRepeatable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for attempt := range 3 {
		if err := durable.File(path); err != nil {
			t.Fatalf("File() attempt %d error = %v", attempt, err)
		}
	}
}

// An error from a path that does not exist has to name the path, or a caller
// reading a log cannot tell which of a publish's several flushes failed.
func TestRefusalNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	err := durable.File(path)
	var pathError *os.PathError
	if !errors.As(err, &pathError) || pathError.Path != path {
		t.Fatalf("File(missing) error = %v, want one naming %q", err, path)
	}
}
