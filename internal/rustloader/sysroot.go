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
	provider, reason := sysrootFrom(askToolchain(ctx, workingDirectory))
	return provider, reason, nil
}

// ToolchainIdentity is what one `rustc` says about itself, asked once: the
// version line and whether its library sources are installed.
type ToolchainIdentity struct {
	// Version is the line `rustc --version` printed, empty when it did not
	// answer.
	Version string
	// Answered is whether `rustc --version` answered at all, so that an empty
	// line is not mistaken for no answer.
	Answered bool
	// Library is why the standard library is not available, empty when its
	// sources are installed. It is the answer DiscoverSysroot gives.
	Library SysrootUnavailableReason
}

// ProbeToolchainIdentity asks `rustc` for its version and its sysroot with one
// `--version` and one `--print sysroot`. A caller that needs both -- the
// fingerprint of the analyzer does -- would otherwise start `rustc --version`
// twice, once for the line and once inside DiscoverSysroot.
func ProbeToolchainIdentity(ctx context.Context, workingDirectory string) (ToolchainIdentity, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ToolchainIdentity{}, err
	}
	probe := askToolchain(ctx, workingDirectory)
	_, reason := sysrootFrom(probe)
	return ToolchainIdentity{Version: probe.version, Answered: probe.answered, Library: reason}, nil
}

// sysrootFrom turns what `rustc` answered into the standard library, or says why
// there is none.
func sysrootFrom(probe toolchainProbe) (SysrootProvider, SysrootUnavailableReason) {
	if probe.reason != "" {
		return SysrootProvider{}, probe.reason
	}
	toolchain, root := probe.release, probe.root
	library := filepath.Join(root, "lib", "rustlib", "src", "rust", "library")
	repository, err := workspace.NewSyntheticRepository(SyntheticRepositoryName(toolchain), library, []string{RustLanguage})
	if err != nil {
		// The library directory of a toolchain is not something a user
		// chose, so a path this policy rejects is a machine this build
		// cannot index hermetically, not a mistake to correct.
		return SysrootProvider{}, SysrootSourceMissing
	}
	if _, err := os.Stat(filepath.Join(repository.RealPath, "Cargo.toml")); err != nil {
		// A toolchain without library sources carries only artifacts.
		// `rustup component add rust-src` installs them.
		return SysrootProvider{}, SysrootSourceMissing
	}
	return SysrootProvider{Repository: repository, Toolchain: toolchain, LibraryPath: repository.RealPath}, ""
}

// LookToolchain answers the executable of the Rust toolchain that the analyzer
// itself would run, which is not the one the PATH names.
//
// rust-analyzer reads `$RUSTC` (the variable named after the tool), then
// `$CARGO_HOME/bin`, which defaults to `~/.cargo/bin`, and only then the PATH.
// A probe that read the PATH alone found no toolchain in a process started
// with a minimal one -- a service, a launchd job -- while the analyzer it then
// started found `~/.cargo/bin/rustc` and discovered a sysroot on its own.
func LookToolchain(name string) (string, error) {
	if chosen := strings.TrimSpace(os.Getenv(strings.ToUpper(name))); chosen != "" {
		return exec.LookPath(chosen)
	}
	home := strings.TrimSpace(os.Getenv("CARGO_HOME"))
	if home == "" {
		if user, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(user, ".cargo")
		}
	}
	if home != "" {
		if resolved, err := exec.LookPath(filepath.Join(home, "bin", name)); err == nil {
			return resolved, nil
		}
	}
	return exec.LookPath(name)
}

// ToolchainVersion answers the version line of one toolchain executable,
// asked the way every probe here asks: without letting rustup install.
//
// The executable is usually rustup's proxy, which answers with the toolchain
// the working directory selects, so the directory is part of the question: an
// empty one is the process's own.
func ToolchainVersion(ctx context.Context, binary, workingDirectory string) (string, error) {
	return runToolchainProbe(ctx, binary, workingDirectory, "--version")
}

// toolchainProbe is everything one pair of questions to `rustc` answered. The
// version line is kept when the release in it could not be read, because a
// toolchain that answers is not the same fact as one that does not.
type toolchainProbe struct {
	version  string
	answered bool
	release  string
	root     string
	reason   SysrootUnavailableReason
}

// askToolchain asks `rustc` for its version and then, if the release can be read
// from it, for its sysroot.
func askToolchain(ctx context.Context, workingDirectory string) toolchainProbe {
	notFound := toolchainProbe{reason: SysrootToolchainNotFound}
	binary, err := LookToolchain("rustc")
	if err != nil {
		return notFound
	}
	version, err := runToolchainProbe(ctx, binary, workingDirectory, "--version")
	if err != nil {
		return notFound
	}
	probe := toolchainProbe{version: version, answered: true, reason: SysrootToolchainNotFound}
	probe.release = toolchainRelease(version)
	if probe.release == "" {
		return probe
	}
	root, err := runToolchainProbe(ctx, binary, workingDirectory, "--print", "sysroot")
	if err != nil || root == "" {
		return probe
	}
	probe.root, probe.reason = root, ""
	return probe
}

// probeToolchain reads the release and the sysroot root from `rustc`.
func probeToolchain(ctx context.Context, workingDirectory string) (string, string, SysrootUnavailableReason) {
	probe := askToolchain(ctx, workingDirectory)
	if probe.reason != "" {
		return "", "", probe.reason
	}
	return probe.release, probe.root, ""
}

// ProbeEnvironment is the environment of a process that asks the toolchain a
// question: this one, with rustup forbidden to answer by installing.
func ProbeEnvironment() []string {
	return append(os.Environ(), "RUSTUP_AUTO_INSTALL=0")
}

// runToolchainProbe asks `rustc` a question and never lets the answer be an
// installation.
//
// The `rustc` on a PATH is usually rustup's proxy, and a proxy that finds its
// active toolchain missing downloads it before answering. That is what a
// process with a home of its own looks like -- a test with a temporary HOME,
// a service -- and what a `rust-toolchain.toml` naming a release nobody
// installed looks like. Measured on the macOS runner: `rustc --version` under
// an empty HOME took 15 to 19 s and left a 1.4 GB toolchain behind, every
// time. `RUSTUP_AUTO_INSTALL=0` is rustup's own switch for it; the probe then
// fails, which every caller already reads as "no toolchain here".
func runToolchainProbe(ctx context.Context, binary, workingDirectory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	if strings.TrimSpace(workingDirectory) != "" {
		command.Dir = workingDirectory
	}
	command.Env = ProbeEnvironment()
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
