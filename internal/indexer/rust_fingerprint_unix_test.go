//go:build !windows

package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestRustFingerprintChangesWhenTheLibrarySourcesArrive: a pass never installs
// `rust-src`, so a machine without it publishes facts with no standard library
// resolved. Installing the component changes neither the release nor a single
// file of any repository, and a fingerprint that did not see it would go on
// serving those facts.
//
// The toolchain is a shell stand-in, which is why this does not build on
// Windows.
func TestRustFingerprintChangesWhenTheLibrarySourcesArrive(t *testing.T) {
	root := testsupport.TempDir(t)
	sysroot := filepath.Join(root, "sysroot")
	bin := filepath.Join(root, "cargo-home", "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	asked := filepath.Join(root, "auto-install")
	script := fmt.Sprintf("#!/bin/sh\necho \"${0##*/}=${RUSTUP_AUTO_INSTALL-unset}\" >> '%s'\ncase \"$*\" in *sysroot*) echo '%s';; *) echo 'rustc 1.96.1 (31fca3adb 2026-06-26)';; esac\n", asked, sysroot)
	// `cargo` is found through the PATH and `rustc` through the cargo home;
	// the same stand-in answers for both and records how it was asked.
	path := testsupport.TempDir(t)
	for _, executable := range []string{filepath.Join(bin, "rustc"), filepath.Join(path, "cargo")} {
		if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CARGO_HOME", filepath.Join(root, "cargo-home"))
	t.Setenv("RUSTC", "")
	t.Setenv("PATH", path)

	options := FullOptions{RustAnalyzer: "rust-analyzer", RustSysroot: "discover"}
	without := rustAnalysisFingerprint(options)
	if again := rustAnalysisFingerprint(options); again != without {
		t.Fatal("the fingerprint is not stable on an unchanged machine")
	}

	library := filepath.Join(sysroot, "lib", "rustlib", "src", "rust", "library")
	if err := os.MkdirAll(library, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "Cargo.toml"), []byte("[workspace]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rustAnalysisFingerprint(options) == without {
		t.Fatal("installing the library sources left the fingerprint unchanged")
	}

	// Every question the fingerprint asked the toolchain forbade rustup to
	// answer it by installing one: under a HOME of its own the proxy
	// downloads 1.4 GB before printing a version.
	recorded, err := os.ReadFile(asked)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, call := range strings.Fields(string(recorded)) {
		seen[call] = true
	}
	if len(seen) != 2 || !seen["rustc=0"] || !seen["cargo=0"] {
		t.Fatalf("calls and the RUSTUP_AUTO_INSTALL they saw = %v, want rustc=0 and cargo=0 only", seen)
	}
}
