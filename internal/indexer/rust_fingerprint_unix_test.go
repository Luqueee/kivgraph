//go:build !windows

package indexer

import (
	"fmt"
	"os"
	"path/filepath"
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
	script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *sysroot*) echo '%s';; *) echo 'rustc 1.96.1 (31fca3adb 2026-06-26)';; esac\n", sysroot)
	if err := os.WriteFile(filepath.Join(bin, "rustc"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARGO_HOME", filepath.Join(root, "cargo-home"))
	t.Setenv("RUSTC", "")
	t.Setenv("PATH", testsupport.TempDir(t))

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
}
