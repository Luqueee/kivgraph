package rustloader

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// SysrootUnavailableReason says why the standard library is not in the graph
// when the configuration asked for it.
//
// None of these is a failure of the pass: the sysroot is opt-in and its absence
// leaves the graph exactly as it is without it, saying so.
type SysrootUnavailableReason string

const (
	// SysrootNotRequested means the configuration did not ask for it.
	SysrootNotRequested SysrootUnavailableReason = "SYSROOT_NOT_REQUESTED"
	// SysrootToolchainNotFound means no `rustc` answered, so nothing names
	// which standard library this corpus compiles against.
	SysrootToolchainNotFound SysrootUnavailableReason = "SYSROOT_TOOLCHAIN_NOT_FOUND"
	// SysrootSourceMissing means the toolchain carries no library source.
	// `rustup component add rust-src` installs it; without the sources there
	// is nothing to index, only artifacts.
	SysrootSourceMissing SysrootUnavailableReason = "SYSROOT_SOURCE_MISSING"
)

// LangCrateOrigin is the version field rust-analyzer writes into the moniker of
// a standard library symbol seen from a crate that uses it.
//
// It is a URL, not a release: `core` referenced from any crate arrives as
// `rust-analyzer cargo core https://github.com/rust-lang/rust/library/core`,
// while the same crate indexed as a workspace of its own publishes version
// `0.0.0`. The two sides therefore never agree on the version field, which is
// why attribution of a lang crate cannot compare it. They do agree on the
// crate name and on the descriptor path, and the stable key is built from those.
const LangCrateOrigin = "https://github.com/rust-lang/rust/library/"

// SysrootProvider is the standard library shaped like a repository, which is
// what the rest of the pass already knows how to index.
//
// The repository is synthetic: it has a name and a path but no commit, no
// branch and no registry entry. Its name carries the toolchain version because
// the stable key of every symbol it publishes carries the repository, and two
// toolchains do not declare the same code.
type SysrootProvider struct {
	Repository workspace.Repository
	// Toolchain is the release `rustc` reported, such as `1.96.1`.
	Toolchain string
	// LibraryPath is the Cargo workspace of the standard library.
	LibraryPath string
}

// SyntheticRepositoryName is the name a sysroot of one toolchain is registered
// under. The colon is deliberate: a repository name is an identifier compared
// exactly, never a path component, so no registered repository can collide with
// this one by being called `rust`.
func SyntheticRepositoryName(toolchain string) string {
	return "rust:" + strings.TrimSpace(toolchain)
}

// DiscoverSysroot answers the standard library of the toolchain on this
// machine, or why it is not available.
//
// It asks `rustc` rather than reading configuration: the sysroot that resolves
// a reference is the one the analyzer loaded, and that is decided by the
// toolchain, not by us. A `rust-toolchain.toml` inside a repository is honoured
// for free, because rustup answers per working directory.
func DiscoverSysroot(ctx context.Context, workingDirectory string) (SysrootProvider, SysrootUnavailableReason, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SysrootProvider{}, "", err
	}
	toolchain, root, reason := probeToolchain(ctx, workingDirectory)
	if reason != "" {
		return SysrootProvider{}, reason, nil
	}
	library := filepath.Join(root, "lib", "rustlib", "src", "rust", "library")
	repository, err := workspace.NewSyntheticRepository(SyntheticRepositoryName(toolchain), library, []string{RustLanguage})
	if err != nil {
		// The library directory of a toolchain is not something a user
		// chose, so a path this policy rejects is a machine this build
		// cannot index hermetically, not a mistake to correct.
		return SysrootProvider{}, SysrootSourceMissing, nil
	}
	if _, err := os.Stat(filepath.Join(repository.RealPath, "Cargo.toml")); err != nil {
		// A toolchain without library sources carries only artifacts.
		// `rustup component add rust-src` installs them.
		return SysrootProvider{}, SysrootSourceMissing, nil
	}
	return SysrootProvider{Repository: repository, Toolchain: toolchain, LibraryPath: repository.RealPath}, "", nil
}

// probeToolchain reads the release and the sysroot root from `rustc`.
func probeToolchain(ctx context.Context, workingDirectory string) (string, string, SysrootUnavailableReason) {
	binary, err := exec.LookPath("rustc")
	if err != nil {
		return "", "", SysrootToolchainNotFound
	}
	version, err := runToolchainProbe(ctx, binary, workingDirectory, "--version")
	if err != nil {
		return "", "", SysrootToolchainNotFound
	}
	release := toolchainRelease(version)
	if release == "" {
		return "", "", SysrootToolchainNotFound
	}
	root, err := runToolchainProbe(ctx, binary, workingDirectory, "--print", "sysroot")
	if err != nil || root == "" {
		return "", "", SysrootToolchainNotFound
	}
	return release, root, ""
}

func runToolchainProbe(ctx context.Context, binary, workingDirectory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	if strings.TrimSpace(workingDirectory) != "" {
		command.Dir = workingDirectory
	}
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("run %s %s: %w", binary, strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// toolchainRelease reads the release out of `rustc 1.96.1 (31fca3adb 2026-06-26)`.
// A line that does not have that shape names no toolchain, and a repository
// name derived from it would identify nothing.
func toolchainRelease(version string) string {
	fields := strings.Fields(version)
	if len(fields) < 2 || fields[0] != "rustc" {
		return ""
	}
	release := fields[1]
	for _, character := range release {
		if character != '.' && character != '-' && character != '+' &&
			(character < '0' || character > '9') &&
			(character < 'a' || character > 'z') {
			return ""
		}
	}
	return release
}
