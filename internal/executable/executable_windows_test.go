//go:build windows

package executable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
)

// PATHEXT is read rather than assumed, so what happens when it is missing,
// empty or ragged is part of the contract and not an implementation detail: a
// machine that has narrowed it is describing itself, and guessing over it
// would disagree with whatever actually runs the file.

func TestAnAbsentPathExtFallsBackToTheDefaultList(t *testing.T) {
	t.Setenv("PATHEXT", "")
	if got := executable.Name("kivgraph.exe"); got != "kivgraph.exe" {
		t.Fatalf("Name(kivgraph.exe) = %q with no PATHEXT, want the extension recognised from the default list", got)
	}
	if got := executable.BaseName(filepath.Join("a", "kivgraph.exe")); got != "kivgraph" {
		t.Fatalf("BaseName() = %q with no PATHEXT, want the default list to strip it", got)
	}
}

func TestAPathExtOfBlanksIsTreatedAsAbsent(t *testing.T) {
	t.Setenv("PATHEXT", "   ")
	if got := executable.Name("kivgraph.exe"); got != "kivgraph.exe" {
		t.Fatalf("Name() = %q with a blank PATHEXT, want the default list", got)
	}
}

// A ragged list is what a machine that has been edited by hand actually has.
// The empty entries have to be skipped rather than matched, or every file
// would look like a program.
func TestRaggedPathExtEntriesAreSkipped(t *testing.T) {
	t.Setenv("PATHEXT", ";.EXE;; ;.CMD;")
	path := filepath.Join(t.TempDir(), "runnable.exe")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !executable.IsProgram(info) {
		t.Fatal("IsProgram(runnable.exe) = false, want the ragged list still to name .EXE")
	}

	plain := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	plainInfo, err := os.Stat(plain)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if executable.IsProgram(plainInfo) {
		t.Fatal("IsProgram(notes.txt) = true, want an empty entry not to match everything")
	}
}

// The list is matched without regard to case, which is what the shell does.
func TestPathExtIsMatchedWithoutRegardToCase(t *testing.T) {
	t.Setenv("PATHEXT", ".exe")
	if got := executable.Name("kivgraph.EXE"); got != "kivgraph.EXE" {
		t.Fatalf("Name(kivgraph.EXE) = %q, want no second extension added", got)
	}
}
