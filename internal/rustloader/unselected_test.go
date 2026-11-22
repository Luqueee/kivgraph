package rustloader

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// A name is found where it is written as a word, outside comments, and is
// reported at the first line that writes it.
func TestNamesWrittenReadsWordsOutsideComments(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "gated.rs")
	source := "// Display is only mentioned here\n" +
		"/* and Display\n   here */ use crate::Display;\n" +
		"fn capture(d: &Display) -> Displayed { render(d) }\n" +
		"fn again() { render(); let display = 1; }\n"
	if err := os.WriteFile(filename, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	declared := map[string]struct{}{"Display": {}, "render": {}, "absent": {}, "Displayed": {}}
	want := map[string]int{"Display": 3, "Displayed": 4, "render": 4}
	if got := namesWritten(filename, declared); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	for name, got := range map[string]map[string]int{
		"a file that is not there": namesWritten(filepath.Join(t.TempDir(), "missing.rs"), declared),
		"nothing declared":         namesWritten(filename, nil),
	} {
		if len(got) != 0 {
			t.Errorf("%s: names = %v, want none", name, got)
		}
	}
	big := filepath.Join(t.TempDir(), "big.rs")
	if err := os.WriteFile(big, append(make([]byte, maximumUnselectedFile), []byte("\nrender();\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := namesWritten(big, declared); len(got) != 0 {
		t.Errorf("a file above the bound was read: %v", got)
	}
}

// The case found on real code: a module behind a `cfg` that is false here has
// no document, so a use in it was in no edge and in no failure.
func TestCollectUnselectedFilesDeclaresWhatTheBuildLeftOut(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("crates/app/src/lib.rs", "pub struct Display;\n#[cfg(target_os = \"linux\")]\nmod linux;\n")
	write("crates/app/src/linux/mod.rs", "use crate::Display;\npub fn capture() -> Display { Display }\n")
	// Names nothing this workspace declares: not listed.
	write("crates/app/src/unrelated.rs", "pub fn other() -> u8 { 1 }\n")
	// Left out by what include_tests asks for, not by the platform.
	write("crates/app/src/tests/mod.rs", "use crate::Display;\n")
	// Outside `src`: an example is not a module of the crate.
	write("crates/app/examples/demo.rs", "use app::Display;\n")
	// A crate with no file selected at all is declared as a crate, elsewhere.
	write("crates/empty/src/lib.rs", "pub fn uses() { let _ = Display; }\n")

	crate := func(name string) workspace.CargoCrate {
		return workspace.CargoCrate{Name: name, Version: "0.1.0", RootPath: filepath.Join(root, "crates", name)}
	}
	options := AnalyzeOptions{
		Repository: workspace.Repository{Name: "repo", RealPath: root},
		Crates:     []workspace.CargoCrate{crate("app"), crate("empty")},
	}
	analysis := Analysis{
		Files:       []IndexedFile{{Path: "crates/app/src/lib.rs", Crate: CrateRef{Name: "app", Version: "0.1.0"}}},
		Definitions: []Definition{{Name: "Display", File: "crates/app/src/lib.rs"}},
	}
	unresolved := map[string]UnresolvedReference{}
	collectUnselectedFiles(options, &analysis, unresolved)

	var rows []UnresolvedReference
	for _, row := range unresolved {
		rows = append(rows, row)
	}
	want := []UnresolvedReference{{
		Crate: CrateRef{Name: "app", Version: "0.1.0"}, File: "crates/app/src/linux/mod.rs",
		RequestedCrate: "app", RequestedSymbol: "Display", Reason: UnresolvedTargetNotBuildable,
		Detail:    "the build configuration of this index did not select this file, and it writes this name",
		StartLine: 1,
	}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %+v, want %+v", rows, want)
	}
	var files []string
	for _, file := range analysis.Files {
		files = append(files, file.Path)
	}
	sort.Strings(files)
	// The file is published, or its rows would name no file and bound every
	// answer in the repository.
	if wantFiles := []string{"crates/app/src/lib.rs", "crates/app/src/linux/mod.rs"}; !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("files = %v, want %v", files, wantFiles)
	}

	// With no root to read from, nothing is claimed.
	collectUnselectedFiles(AnalyzeOptions{Crates: options.Crates}, &analysis, unresolved)
	if len(unresolved) != 1 {
		t.Errorf("a repository with no path produced rows: %d", len(unresolved))
	}
}

// The analyzer's log repeats one message with a different internal id each
// time. Twenty of those are not twenty diagnostics.
func TestClassifyDiagnosticsCountsARepeatedLogMessage(t *testing.T) {
	output := strings.Join([]string{
		"rust-analyzer: Loading metadata",
		"2026-11-22T16:18:11.301278+01:00 ERROR Encountered enclosing definition with no name def=Module(Module { id: Id(6e3ff) })",
		"2026-11-22T16:18:11.301468+01:00 ERROR Encountered enclosing definition with no name def=Module(Module { id: Id(6d0d6) })",
		"2026-11-22T16:18:12.000000+01:00  WARN failed to load a proc macro path=/a/b.dylib",
		"2026-11-22T16:18:12.175646+01:00 ERROR Encountered enclosing definition with no name def=Module(Module { id: Id(5ee66) })",
		"2026-11-22T16:18:13.000000+01:00 ERROR no fields here",
		"Duplicate symbol: rust-analyzer cargo a 0.1.0 b/C#",
		"Duplicate symbol: rust-analyzer cargo a 0.1.0 b/D#",
		"12: a::stack::frame WARN",
	}, "\n")
	want := []string{
		"2026-11-22T16:18:11.301278+01:00 ERROR Encountered enclosing definition with no name def=Module(Module { id: Id(6e3ff) }) (and 2 more with other fields)",
		"2026-11-22T16:18:12.000000+01:00  WARN failed to load a proc macro path=/a/b.dylib",
		"2026-11-22T16:18:13.000000+01:00 ERROR no fields here",
		// Not a log line: each names a different symbol, and both are kept.
		"Duplicate symbol: rust-analyzer cargo a 0.1.0 b/C#",
		"Duplicate symbol: rust-analyzer cargo a 0.1.0 b/D#",
	}
	if got := classifyDiagnostics(output); !reflect.DeepEqual(got, want) {
		t.Errorf("classifyDiagnostics =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, line := range []string{"", "ERROR", "word ERROR x", "20261122T1618 ERROR x", "abcd-ef-gh ERROR x"} {
		if message, logged := loggedMessage(line); logged {
			t.Errorf("loggedMessage(%q) = %q, want it not read as a log line", line, message)
		}
	}
}
