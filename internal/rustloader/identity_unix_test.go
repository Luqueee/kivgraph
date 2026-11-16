//go:build !windows

package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// installRustc puts a `rustc` stand-in where the analyzer looks first. It prints
// versionLine for `--version`, the sysroot for `--print sysroot`, and records
// every question it was asked. It is a shell script, which is why this does not
// build on Windows.
func installRustc(t *testing.T, versionLine string, withLibrary bool, exitStatus int) (asked func() []string) {
	t.Helper()
	root := testsupport.TempDir(t)
	sysroot := filepath.Join(root, "sysroot")
	if withLibrary {
		if err := os.MkdirAll(filepath.Join(sysroot, "lib", "rustlib", "src", "rust", "library"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sysroot, "lib", "rustlib", "src", "rust", "library", "Cargo.toml"), []byte("[workspace]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.MkdirAll(sysroot, 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(root, "asked")
	script := "#!/bin/sh\necho \"$*\" >> '" + record + "'\n" +
		"if [ " + strconv.Itoa(exitStatus) + " -ne 0 ]; then exit " + strconv.Itoa(exitStatus) + "; fi\n" +
		"case \"$*\" in *sysroot*) echo '" + sysroot + "';; *) echo '" + versionLine + "';; esac\n"
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "rustc"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("RUSTC", filepath.Join(bin, "rustc"))
	t.Setenv("CARGO_HOME", filepath.Join(root, "cargo-home"))
	return func() []string {
		content, err := os.ReadFile(record)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(content)), "\n")
	}
}

// ProbeToolchainIdentity exists so that a caller that wants the version line and
// the answer of DiscoverSysroot starts `rustc --version` once. It must say the
// same thing as the two calls it replaces, in every state a machine can be in.
func TestProbeToolchainIdentityAnswersWhatDiscoverSysrootDoes(t *testing.T) {
	const line = "rustc 1.96.1 (31fca3adb 2026-06-26)"
	for name, test := range map[string]struct {
		line        string
		withLibrary bool
		exit        int
		want        ToolchainIdentity
		wantAsked   []string
	}{
		"the toolchain answers and carries its library": {
			line: line, withLibrary: true,
			want:      ToolchainIdentity{Version: line, Answered: true},
			wantAsked: []string{"--version", "--print sysroot"},
		},
		"the toolchain answers and carries no library": {
			line:      line,
			want:      ToolchainIdentity{Version: line, Answered: true, Library: SysrootSourceMissing},
			wantAsked: []string{"--version", "--print sysroot"},
		},
		// An answer that is not a version line is still an answer, and the
		// fingerprint records it; it names no toolchain, so nothing else is asked.
		"the toolchain answers with something that is not a version": {
			line: "garbage", withLibrary: true,
			want:      ToolchainIdentity{Version: "garbage", Answered: true, Library: SysrootToolchainNotFound},
			wantAsked: []string{"--version"},
		},
		"the toolchain refuses to answer": {
			line: line, exit: 1,
			want:      ToolchainIdentity{Library: SysrootToolchainNotFound},
			wantAsked: []string{"--version"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			asked := installRustc(t, test.line, test.withLibrary, test.exit)
			got, err := ProbeToolchainIdentity(context.Background(), "")
			if err != nil {
				t.Fatalf("ProbeToolchainIdentity() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ProbeToolchainIdentity() = %+v, want %+v", got, test.want)
			}
			if calls := asked(); !slices.Equal(calls, test.wantAsked) {
				t.Fatalf("rustc was asked %q, want %q", calls, test.wantAsked)
			}
			_, reason, err := DiscoverSysroot(context.Background(), "")
			if err != nil {
				t.Fatalf("DiscoverSysroot() error = %v", err)
			}
			if reason != got.Library {
				t.Fatalf("DiscoverSysroot() reason = %q, ProbeToolchainIdentity() library = %q, want the same", reason, got.Library)
			}
		})
	}
}

func TestProbeToolchainIdentityWithoutAToolchainAnswersNothing(t *testing.T) {
	withoutToolchain(t)
	got, err := ProbeToolchainIdentity(context.Background(), "")
	if err != nil {
		t.Fatalf("ProbeToolchainIdentity() error = %v", err)
	}
	if want := (ToolchainIdentity{Library: SysrootToolchainNotFound}); got != want {
		t.Fatalf("ProbeToolchainIdentity() = %+v, want %+v", got, want)
	}
}

func TestProbeToolchainIdentityStopsWhenTheContextIsCancelled(t *testing.T) {
	asked := installRustc(t, "rustc 1.96.1 (x)", true, 0)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProbeToolchainIdentity(cancelled, ""); err == nil {
		t.Fatal("ProbeToolchainIdentity() on a cancelled context returned no error")
	}
	if calls := asked(); len(calls) != 0 {
		t.Fatalf("rustc was asked %q after the context was cancelled", calls)
	}
}
