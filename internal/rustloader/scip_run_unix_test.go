//go:build !windows

package rustloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// toolchainWithoutRustSrc puts a `rustc` on the PATH whose sysroot is the real
// one minus `lib/rustlib/src`, and a `rustup` that records being called. It
// answers the file that `rustup` creates.
//
// The stand-ins are shell scripts, which is why this file does not build on
// Windows. `CARGO_HOME` moves with them because the analyzer looks for `rustup`
// in `$CARGO_HOME/bin` before it looks on the PATH.
func toolchainWithoutRustSrc(t *testing.T) string {
	t.Helper()
	rustc, err := exec.LookPath("rustc")
	if err != nil {
		t.Skip("no rustc on the PATH, so there is no sysroot to hide the sources of")
	}
	output, err := exec.Command(rustc, "--print", "sysroot").Output()
	if err != nil {
		t.Fatalf("rustc --print sysroot: %v", err)
	}
	real := strings.TrimSpace(string(output))

	root := testsupport.TempDir(t)
	sysroot := filepath.Join(root, "sysroot")
	mirrorWithout(t, real, sysroot, []string{"lib", "rustlib", "src"})

	home := filepath.Join(root, "cargo-home")
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	called := filepath.Join(root, "rustup-was-called")
	scripts := map[string]string{
		"rustc":  fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *\"--print sysroot\"*) echo '%s'; exit 0;; esac\nexec '%s' \"$@\"\n", sysroot, rustc),
		"rustup": fmt.Sprintf("#!/bin/sh\necho \"$*\" >> '%s'\nexit 1\n", called),
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CARGO_HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return called
}

// mirrorWithout links every entry of source into destination except the one
// the skip path names, descending only along that path.
func mirrorWithout(t *testing.T, source, destination string, skip []string) {
	t.Helper()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		from, to := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
		switch {
		case entry.Name() != skip[0]:
			if err := os.Symlink(from, to); err != nil {
				t.Fatal(err)
			}
		case len(skip) > 1:
			mirrorWithout(t, from, to, skip[1:])
		}
	}
}

// TestRunDoesNotInstallTheStandardLibrarySources: asked to discover a sysroot
// that has no `rust-src`, the analyzer runs `rustup component add rust-src` on
// its own -- a download and a write into the toolchain, from a pass that is
// offline unless `rust.allow_network` says otherwise. Two passes starting
// together raced that install, and the loser indexed without a standard
// library that was there a second later.
func TestRunDoesNotInstallTheStandardLibrarySources(t *testing.T) {
	requireAnalyzer(t)
	called := toolchainWithoutRustSrc(t)
	workspace, output := analyzerFixture(t)

	if _, err := Run(context.Background(), defaultRunOptions(workspace, output)); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// The analyzer also asks `rustup which cargo`, which reads and installs
	// nothing; only the install is forbidden.
	calls, err := os.ReadFile(called)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "component add") {
		t.Fatalf("the run asked rustup to install: %q", calls)
	}
}

// TestRunLeavesDiscoveryToTheAnalyzerWhenTheNetworkIsAllowed is the declared
// way out: with `rust.allow_network` the analyzer may fetch what is missing.
func TestRunLeavesDiscoveryToTheAnalyzerWhenTheNetworkIsAllowed(t *testing.T) {
	requireAnalyzer(t)
	called := toolchainWithoutRustSrc(t)
	workspace, output := analyzerFixture(t)
	options := defaultRunOptions(workspace, output)
	options.AllowNetwork = true

	if _, err := Run(context.Background(), options); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	calls, err := os.ReadFile(called)
	if err != nil || !strings.Contains(string(calls), "component add rust-src") {
		t.Fatalf("rustup was called with %q (%v), want `component add rust-src`", calls, err)
	}
}
