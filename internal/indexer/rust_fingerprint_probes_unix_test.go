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

// The fingerprint asks the analyzer, `rustc` and `cargo` what they are. Each
// question is asked once -- `rustc --version` used to be asked twice, once for
// the line and once inside the sysroot probe -- and the three are asked
// together, because each starts processes and none needs another's answer.
//
// Each stand-in leaves a marker when it starts and waits for the other two. One
// that runs after another has finished waits for a marker that never comes and
// gives up, so a fingerprint that asks them in sequence leaves a stand-in that
// never met the rest. The stand-ins are shell scripts, which is why this does not
// build on Windows, and they wait with /bin/sleep because their PATH holds only
// themselves.
func TestRustFingerprintAsksEachToolchainOnceAndTogether(t *testing.T) {
	root := testsupport.TempDir(t)
	bin := filepath.Join(root, "bin")
	state := filepath.Join(root, "state")
	sysroot := filepath.Join(root, "sysroot")
	for _, directory := range []string{bin, state, sysroot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nname=${0##*/}\n" +
		"echo \"$name $*\" >> '" + filepath.Join(state, "asked") + "'\n" +
		": > '" + state + "'/started-$name\n" +
		"i=0\nwhile [ $i -lt 100 ]; do\n" +
		"  if [ -f '" + state + "'/started-rustc ] && [ -f '" + state + "'/started-cargo ] && [ -f '" + state + "'/started-rust-analyzer ]; then\n" +
		"    echo \"$name\" >> '" + filepath.Join(state, "met") + "'; break\n  fi\n  /bin/sleep 0.05; i=$((i+1))\ndone\n" +
		"case \"$*\" in *sysroot*) echo '" + sysroot + "';; *) echo 'rustc 1.96.1 (31fca3adb 2026-06-26)';; esac\n"
	for _, name := range []string{"rustc", "cargo", "rust-analyzer"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("RUSTC", filepath.Join(bin, "rustc"))
	t.Setenv("CARGO_HOME", filepath.Join(root, "cargo-home"))

	rustAnalysisFingerprint(FullOptions{RustAnalyzer: "rust-analyzer", RustSysroot: "discover"})

	read := func(name string) []string {
		content, err := os.ReadFile(filepath.Join(state, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		slices.Sort(lines)
		return lines
	}
	if got, want := read("asked"), []string{
		"cargo --version", "rust-analyzer --version", "rustc --print sysroot", "rustc --version",
	}; !slices.Equal(got, want) {
		t.Fatalf("the toolchain was asked %q, want each question once: %q", got, want)
	}
	// The probe of `rustc` asks two questions in a row, so it meets the others
	// when it starts; what has to be met is that the three were running together.
	met := read("met")
	for _, name := range []string{"cargo", "rust-analyzer", "rustc"} {
		if !slices.Contains(met, name) {
			t.Fatalf("%s never saw the other two running; met = %q", name, met)
		}
	}
}
