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

// toolchainWithoutRustSrc installs a `rustc` whose sysroot is the real one
// minus `lib/rustlib/src`, and a `rustup` that records being called. It
// answers the file that `rustup` appends its arguments to.
//
// Both live in `$CARGO_HOME/bin`, which is where the analyzer looks first.
// With onPath that directory is also put on the PATH; without it the PATH is
// rebuilt to hold the analyzer, cargo and the system tools and no `rustc` at
// all, which is the toolchain a rustup install leaves for a process started
// with a minimal PATH -- a service, a launchd job.
//
// The stand-ins are shell scripts, which is why this file does not build on
// Windows.
func toolchainWithoutRustSrc(t *testing.T, onPath bool) string {
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
	// The analyzer reads `$RUSTC` before anything else, and an inherited one
	// would point it past the stand-in.
	t.Setenv("RUSTC", "")
	if err := os.Unsetenv("RUSTC"); err != nil {
		t.Fatal(err)
	}
	if onPath {
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		return called
	}
	tools := filepath.Join(root, "tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rust-analyzer", "cargo"} {
		resolved, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s is not on the PATH: %v", name, err)
		}
		if err := os.Symlink(resolved, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", strings.Join([]string{tools, "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	if found, err := exec.LookPath("rustc"); err == nil {
		t.Skipf("the system directories carry %s, so no PATH here is without a rustc", found)
	}
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

// TestLookToolchainReadsWhereTheAnalyzerReads pins the order: the variable
// named after the tool, then `$CARGO_HOME/bin`, then the PATH.
func TestLookToolchainReadsWhereTheAnalyzerReads(t *testing.T) {
	root := testsupport.TempDir(t)
	place := func(directory string) string {
		path := filepath.Join(root, directory, "rustc")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	onPath, inHome, chosen := place("path"), place(filepath.Join("home", "bin")), place("chosen")
	withoutToolchain(t)
	if found, err := LookToolchain("rustc"); err == nil {
		t.Fatalf("LookToolchain() = %q with no toolchain anywhere", found)
	}

	t.Setenv("PATH", filepath.Dir(onPath))
	if found, err := LookToolchain("rustc"); err != nil || found != onPath {
		t.Fatalf("LookToolchain() = %q, %v, want the PATH's %q", found, err, onPath)
	}
	t.Setenv("CARGO_HOME", filepath.Join(root, "home"))
	if found, err := LookToolchain("rustc"); err != nil || found != inHome {
		t.Fatalf("LookToolchain() = %q, %v, want the cargo home's %q", found, err, inHome)
	}
	t.Setenv("RUSTC", chosen)
	if found, err := LookToolchain("rustc"); err != nil || found != chosen {
		t.Fatalf("LookToolchain() = %q, %v, want $RUSTC %q", found, err, chosen)
	}
	// A variable that names nothing runnable is an answer, not a reason to
	// look elsewhere: the analyzer would run it and fail.
	t.Setenv("RUSTC", filepath.Join(root, "absent"))
	if found, err := LookToolchain("rustc"); err == nil {
		t.Fatalf("LookToolchain() = %q for a $RUSTC that does not exist", found)
	}
}

// TestRunDoesNotInstallForAToolchainThatIsOffThePath: the analyzer finds
// `rustc` in `$CARGO_HOME/bin` whether or not the PATH names it, so a probe
// that only read the PATH found no toolchain, left discovery to the analyzer,
// and the analyzer installed.
func TestRunDoesNotInstallForAToolchainThatIsOffThePath(t *testing.T) {
	requireAnalyzer(t)
	called := toolchainWithoutRustSrc(t, false)
	workspace, output := analyzerFixture(t)

	_, runErr := Run(context.Background(), defaultRunOptions(workspace, output))
	calls, err := os.ReadFile(called)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "component add") {
		t.Fatalf("the run asked rustup to install: %q", calls)
	}
	if runErr != nil {
		t.Fatalf("Run() error = %v", runErr)
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
	called := toolchainWithoutRustSrc(t, true)
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
	called := toolchainWithoutRustSrc(t, true)
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

// recordingStandIn writes an executable that appends the RUSTUP_AUTO_INSTALL
// it was started with to a file, and answers the file. The variable is
// removed from this process first, so "unset" means nobody set it.
func recordingStandIn(t *testing.T, directory, name string, exit int) string {
	t.Helper()
	saw := filepath.Join(directory, name+"-saw")
	script := fmt.Sprintf("#!/bin/sh\necho \"${RUSTUP_AUTO_INSTALL-unset}\" >> '%s'\nexit %d\n", saw, exit)
	if err := os.WriteFile(filepath.Join(directory, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSTUP_AUTO_INSTALL", "")
	if err := os.Unsetenv("RUSTUP_AUTO_INSTALL"); err != nil {
		t.Fatal(err)
	}
	return saw
}

// sawOnce reads what the stand-in recorded and forgets it.
func sawOnce(t *testing.T, saw string) string {
	t.Helper()
	data, err := os.ReadFile(saw)
	if err != nil {
		t.Fatalf("the stand-in was never started: %v", err)
	}
	if err := os.Remove(saw); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// TestMetadataForbidsAToolchainInstallUnlessTheNetworkIsAllowed: `cargo` is
// rustup's proxy on most machines, and `cargo metadata` in a workspace whose
// toolchain is missing downloads it before it reads a manifest.
func TestMetadataForbidsAToolchainInstallUnlessTheNetworkIsAllowed(t *testing.T) {
	root := testsupport.TempDir(t)
	saw := recordingStandIn(t, root, "cargo", 0)
	t.Setenv("PATH", root)

	for network, want := range map[bool]string{false: "0", true: "unset"} {
		if _, err := Metadata(context.Background(), MetadataOptions{Workspace: root, AllowNetwork: network}); err != nil {
			t.Fatalf("Metadata() error = %v", err)
		}
		if got := sawOnce(t, saw); got != want {
			t.Fatalf("network=%t: cargo saw RUSTUP_AUTO_INSTALL=%q, want %q", network, got, want)
		}
	}
}

// TestRunForbidsAToolchainInstallUnlessTheNetworkIsAllowed: the pinned
// analyzer forbids the install on every command it spawns, but the analyzer
// is whatever `rust.analyzer_command` names, and an offline pass cannot rest
// on a version it did not choose.
func TestRunForbidsAToolchainInstallUnlessTheNetworkIsAllowed(t *testing.T) {
	workspace, output := analyzerFixture(t)
	directory := testsupport.TempDir(t)
	saw := recordingStandIn(t, directory, "analyzer", 1)

	for network, want := range map[bool]string{false: "0", true: "unset"} {
		options := defaultRunOptions(workspace, output)
		options.AnalyzerCommand = filepath.Join(directory, "analyzer")
		options.AllowNetwork = network
		var runErr *RunError
		if _, err := Run(context.Background(), options); !errors.As(err, &runErr) || runErr.Kind != RunErrorWorkspaceNotLoaded {
			t.Fatalf("Run() error = %v, want the stand-in's exit declared as %s", err, RunErrorWorkspaceNotLoaded)
		}
		if got := sawOnce(t, saw); got != want {
			t.Fatalf("network=%t: the analyzer saw RUSTUP_AUTO_INSTALL=%q, want %q", network, got, want)
		}
	}
}
