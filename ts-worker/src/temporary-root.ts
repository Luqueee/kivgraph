import { mkdtemp, realpath } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

/**
 * Creates a temporary directory and returns the path with symlinks resolved.
 *
 * The TypeScript engine reports the files it resolves by their real path. On
 * macOS the system temporary directory lives under `/var`, a symlink to
 * `/private/var`, so a fixture built from `tmpdir()` and the paths the engine
 * answers with would never compare equal. Production never hits this: the
 * workspace layer rejects a repository path with a symlinked component and
 * hands the worker a resolved root.
 */
export async function temporaryRoot(prefix: string): Promise<string> {
  return await realpath(await mkdtemp(join(tmpdir(), prefix)));
}
