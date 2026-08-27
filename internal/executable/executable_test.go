package executable_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
)

// The two functions are inverses, and the tests are written around what
// happens when they are given something that is not a program: an empty name,
// a name that already carries the extension, and a name whose extension means
// something else. Each of those was a real defect -- Name("") answered ".exe",
// and BaseName was written because `stop` compared "kivgraph.exe" against
// "kivgraph" and quietly stopped nothing.

func TestNameRefusesToInventAProgramFromNothing(t *testing.T) {
	if got := executable.Name(""); got != "" {
		t.Fatalf("Name(\"\") = %q, want the empty name back: a program with no name is not one", got)
	}
}

func TestNameIsIdempotent(t *testing.T) {
	once := executable.Name("kivgraph")
	if twice := executable.Name(once); twice != once {
		t.Fatalf("Name(Name(%q)) = %q, want %q: applying it twice must not double an extension", "kivgraph", twice, once)
	}
}

// A suffix that is not a program extension is part of the name, not a claim
// about what the file is. Getting this wrong would turn "graph.db" into a
// program on one platform and leave it a database on the other.
func TestNameKeepsASuffixThatIsNotAProgram(t *testing.T) {
	got := executable.Name("graph.db")
	if runtime.GOOS == "windows" {
		if got != "graph.db.exe" {
			t.Fatalf("Name(graph.db) = %q, want the program extension added to the whole name", got)
		}
		return
	}
	if got != "graph.db" {
		t.Fatalf("Name(graph.db) = %q, want it unchanged", got)
	}
}

func TestBaseNameRefusesNothingAndKeepsWhatIsNotAnExtension(t *testing.T) {
	for name, want := range map[string]string{
		"":                      "",
		"graph.db":              "graph.db",
		filepath.Join("a", "b"): "b",
	} {
		if got := executable.BaseName(name); got != want {
			t.Fatalf("BaseName(%q) = %q, want %q", name, got, want)
		}
	}
}

// This is the pair's contract and the reason both exist: whatever a platform
// stores a program under, the name it is called by comes back.
func TestBaseNameInvertsName(t *testing.T) {
	for _, called := range []string{"kivgraph", "node", "rust-analyzer"} {
		stored := filepath.Join("some", "where", executable.Name(called))
		if got := executable.BaseName(stored); got != called {
			t.Fatalf("BaseName(Name(%q)) = %q, want %q", called, got, called)
		}
	}
}

func TestIsProgramRefusesWhatCannotBeRun(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "plain")
	if err := os.WriteFile(plain, []byte("not a program"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for name, path := range map[string]string{
		"a directory":             root,
		"a file without the mark": plain,
	} {
		t.Run(name, func(t *testing.T) {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if executable.IsProgram(info) {
				t.Fatalf("IsProgram(%s) = true, want false", name)
			}
		})
	}
}

func TestIsProgramAcceptsOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), executable.Name("runnable"))
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !executable.IsProgram(info) {
		t.Fatalf("IsProgram(%q) = false, want true", path)
	}
}

func TestCandidatesRefusesToLookForNothing(t *testing.T) {
	if got := executable.Candidates(""); len(got) != 0 {
		t.Fatalf("Candidates(\"\") = %v, want nothing to look for", got)
	}
}

// A base that already names a program is a name, not a stem: appending
// extensions to it would look for "kivgraph.exe.cmd".
func TestCandidatesDoNotExtendANameThatIsAlreadyOne(t *testing.T) {
	for _, base := range []string{executable.Name("kivgraph"), "kivgraph"} {
		got := executable.Candidates(executable.Name(base))
		if len(got) != 1 || got[0] != executable.Name(base) {
			t.Fatalf("Candidates(%q) = %v, want the name alone", executable.Name(base), got)
		}
	}
}

// Whatever a platform would try, Name's answer has to be among it, or a
// program this project writes is one it cannot then find.
func TestWhatNameWritesIsAmongWhatCandidatesLooksFor(t *testing.T) {
	for _, base := range []string{"kivgraph", "rust-analyzer"} {
		written := executable.Name(base)
		found := false
		for _, candidate := range executable.Candidates(base) {
			if candidate == written {
				found = true
			}
		}
		if !found {
			t.Fatalf("Candidates(%q) = %v, none of which is Name(%q) = %q",
				base, executable.Candidates(base), base, written)
		}
	}
}
