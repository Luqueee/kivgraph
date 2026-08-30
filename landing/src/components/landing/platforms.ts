/** The published platforms and the command each one uses to install them. */
export type Platform = {
  id: string;
  label: string;
  prompt: string;
  arch: string;
  command: string;
};

export const POSIX_COMMAND =
  "curl -fsSL https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh | bash";

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
