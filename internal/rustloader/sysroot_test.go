package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestDiscoverSysrootNamesTheToolchainItFound is the contract the stable key of
// every standard library symbol rests on: the synthetic repository carries the
// release, because two toolchains do not declare the same code and a key that
// omitted it would make them one node.
func TestDiscoverSysrootNamesTheToolchainItFound(t *testing.T) {
	testsupport.RequireRustAnalyzer(t)
	if _, err := os.Stat(filepath.Join(t.TempDir())); err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	provider, reason, err := DiscoverSysroot(context.Background(), "")
	if err != nil {
		t.Fatalf("DiscoverSysroot() error = %v", err)
	}
	if reason == SysrootSourceMissing {
		t.Skip("this toolchain carries no library sources; `rustup component add rust-src` installs them")
	}
	if reason != "" {
		t.Fatalf("DiscoverSysroot() reason = %q, want the sysroot of the installed toolchain", reason)
	}
	if provider.Toolchain == "" || !strings.HasPrefix(provider.Repository.Name, "rust:") {
		t.Fatalf("provider = %#v, want a release and a synthetic name", provider)
	}
	if provider.Repository.Name != "rust:"+provider.Toolchain {
		t.Fatalf("name = %q, want it to carry the release %q", provider.Repository.Name, provider.Toolchain)
	}
	// The synthetic repository is not under version control and nothing must
	// pretend otherwise: a commit nobody read is a freshness claim nobody can
	// check.
	if provider.Repository.Commit != "" || provider.Repository.Branch != "" || provider.Repository.Dirty {
		t.Fatalf("repository = %#v, want no version control metadata", provider.Repository)
	}
	if _, err := os.Stat(filepath.Join(provider.LibraryPath, "core", "Cargo.toml")); err != nil {
		t.Fatalf("library path %q does not hold core: %v", provider.LibraryPath, err)
	}
}

// TestDiscoverSysrootDeclaresWhatItCannotFind covers the acceptance criterion
// that the absence of a sysroot is not a failure. A machine without a toolchain
// still indexes its repositories.
func TestDiscoverSysrootDeclaresWhatItCannotFind(t *testing.T) {
	directory := testsupport.TempDir(t)
	t.Setenv("PATH", directory)
	provider, reason, err := DiscoverSysroot(context.Background(), "")
	if err != nil {
		t.Fatalf("DiscoverSysroot() error = %v", err)
	}
	if reason != SysrootToolchainNotFound {
		t.Fatalf("reason = %q, want %q", reason, SysrootToolchainNotFound)
	}
	if provider.Repository.Name != "" {
		t.Fatalf("provider = %#v, want nothing named", provider)
	}
}

func TestToolchainReleaseReadsOnlyAVersionLine(t *testing.T) {
	tests := map[string]string{
		"rustc 1.96.1 (31fca3adb 2026-06-26)":   "1.96.1",
		"rustc 1.97.0-nightly (abc 2026-07-01)": "1.97.0-nightly",
		"rustc 1.90.0":                          "1.90.0",
		// Anything that is not a version line names no toolchain, and a
		// synthetic repository named after it would identify nothing.
		"error: no default toolchain": "",
		"1.96.1":                      "",
		"rustc":                       "",
		"rustc /etc/passwd":           "",
		"":                            "",
	}
	for version, want := range tests {
		if got := toolchainRelease(version); got != want {
			t.Fatalf("toolchainRelease(%q) = %q, want %q", version, got, want)
		}
	}
}
