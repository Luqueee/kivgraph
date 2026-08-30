/**
 * The published platforms and the one command each of them installs with.
 *
 * There are three archives on a release -- `kivgraph-linux-amd64.tar.gz`,
 * `kivgraph-darwin-arm64.tar.gz` and `kivgraph-windows-amd64.zip` -- and two
 * installers beside them, because `install.sh` cannot run where there is no
 * POSIX shell. That is the whole reason this list has a Windows row and why
 * that row is the only one whose command is different.
 *
 * macOS and Linux carry the *same* line on purpose, and showing it twice is
 * the point rather than a duplication: `install.sh` reads `uname` and picks
 * the archive itself, so the answer to "what do I run on a Mac" is the answer
 * to "what do I run on Linux", and a visitor who has to infer that from a
 * single unlabelled box is a visitor who wonders whether the page forgot them.
 *
 * The `arch` line is not decoration. Only one architecture is published per
 * platform, and the installer refuses the others by name -- an Intel Mac is
 * the case people actually hit.
 *
 * Nothing is detected: these pages are prerendered, so the first tab is a
 * default and not a guess about the reader.
 */
export type Platform = {
  /** Stable id. It is the radio value, the panel key and the analytics label. */
  id: string;
  /** What the tab says. */
  label: string;
  /** The prompt sigil the snippet shows, which is the shell it runs in. */
  prompt: string;
  /** The published architecture, and what the installer refuses beside it. */
  arch: string;
  command: string;
};

/**
 * Byte-identical to `README.md`, to `landing/src/content/docs/install.md` and
 * to the tail of `scripts/install.sh`'s own usage. `install.sh` has to be run
 * by `bash` and not `sh`: it uses arrays and `BASH_REMATCH`.
 */
export const POSIX_COMMAND =
  "curl -fsSL https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh | bash";

/**
 * `irm | iex` is the PowerShell shape of `curl | bash`, and it is symmetric
 * with it down to what it gives up: the script's `#Requires -Version 5.1`
 * becomes a comment when the text is piped into `Invoke-Expression` rather
 * than run as a file, so the version guard is not enforced by the one-liner.
 * `install.ps1` needs 5.1, which is what Windows 10 and Server 2016 onward
 * ship, so the guard is a courtesy rather than the only thing standing between
 * a reader and a broken install. Download it to a file and run it if you want
 * the guard back.
 */
const WINDOWS_COMMAND =
  "irm https://github.com/Luqueee/kivgraph/releases/latest/download/install.ps1 | iex";

export const PLATFORMS: readonly Platform[] = [
  {
    id: "macos",
    label: "macOS",
    prompt: "$",
    arch: "Apple Silicon. darwin/amd64 is not published.",
    command: POSIX_COMMAND,
  },
  {
    id: "linux",
    label: "Linux",
    prompt: "$",
    arch: "x86_64.",
    command: POSIX_COMMAND,
  },
  {
    id: "windows",
    label: "Windows",
    prompt: "PS>",
    arch: "x86_64. The installer adds the Visual C++ redistributable.",
    command: WINDOWS_COMMAND,
  },
];
