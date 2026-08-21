import { realpathSync } from "node:fs";
import { pathToFileURL } from "node:url";

// isEntryPoint decides whether a module was started as a program or imported
// as a library. Comparing pathToFileURL(process.argv[1]) alone is not enough:
// Node resolves the main module through realpath, so any invocation path with
// a symlinked component - every path under /tmp or /var on macOS, and any
// symlinked install root - produced a silent exit instead of the protocol.
// Both forms are accepted because --preserve-symlinks-main keeps the logical
// path in import.meta.url.
export function isEntryPoint(
  entry: string | undefined,
  moduleURL: string,
): boolean {
  if (!entry) {
    return false;
  }
  if (pathToFileURL(entry).href === moduleURL) {
    return true;
  }
  try {
    return pathToFileURL(realpathSync(entry)).href === moduleURL;
  } catch {
    return false;
  }
}
