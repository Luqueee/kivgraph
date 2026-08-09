import { readdirSync } from "node:fs";
import { isAbsolute, join, parse, sep } from "node:path";

/**
 * Recovers the on-disk casing of a path reported by the TypeScript engine.
 *
 * On a filesystem that folds case - the macOS default - the engine
 * canonicalises the paths it resolves through module resolution to lower case.
 * Those paths reach stable keys and evidence, and they also stop matching the
 * declaration-map index the worker builds from real paths, which silently
 * drops declaration-to-source mappings. Both symptoms have one cause, so the
 * correction belongs at the boundary where an engine path enters worker data.
 *
 * The walk is per component and never resolves symlinks: `realpath` would
 * rewrite a pnpm `node_modules` entry into its `.pnpm` store location and
 * change the facts. A component that does not exist ends the walk and the
 * remainder is kept verbatim, so a virtual or deleted path is returned
 * unchanged rather than guessed.
 *
 * Directory listings and whole paths are memoised: the cost is one `readdir`
 * per directory the engine mentions, and on a case-sensitive filesystem every
 * component matches exactly on the first try.
 */
const resolved = new Map<string, string>();
const listings = new Map<string, DirectoryIndex | undefined>();

interface DirectoryIndex {
  /** Entry names exactly as the filesystem spells them. */
  readonly names: ReadonlySet<string>;
  /** Lower-cased name to the spelling on disk. */
  readonly folded: ReadonlyMap<string, string>;
}

export function enginePath(candidate: string): string {
  if (!isAbsolute(candidate)) {
    return candidate;
  }
  const cached = resolved.get(candidate);
  if (cached !== undefined) {
    return cached;
  }
  const { root } = parse(candidate);
  const components = candidate
    .slice(root.length)
    .split(sep)
    .filter((part) => part !== "");
  let current = root;
  let index = 0;
  for (; index < components.length; index += 1) {
    const component = components[index];
    if (component === undefined) {
      continue;
    }
    const actual = lookup(current, component, false);
    if (actual === undefined) {
      // A memoised listing predates the entry: a run indexes a tree while
      // fixtures and build outputs appear. A miss is only authoritative after
      // a fresh read.
      const refreshed = lookup(current, component, true);
      if (refreshed === undefined) {
        break;
      }
      current = join(current, refreshed);
      continue;
    }
    current = join(current, actual);
  }
  const remainder = components.slice(index);
  const answer = remainder.length === 0 ? current : join(current, ...remainder);
  resolved.set(candidate, answer);
  return answer;
}

/** lookup answers with the spelling on disk of one component, if it is there. */
function lookup(
  directory: string,
  component: string,
  refresh: boolean,
): string | undefined {
  if (refresh) {
    listings.delete(directory);
  }
  const entries = listing(directory);
  if (entries === undefined) {
    return undefined;
  }
  if (entries.names.has(component)) {
    return component;
  }
  return entries.folded.get(component.toLowerCase());
}

/**
 * listing indexes a directory twice: by the exact spelling, so an already
 * correct component costs one lookup, and by the folded spelling, which is the
 * only way back from a canonicalised path.
 */
function listing(directory: string): DirectoryIndex | undefined {
  if (listings.has(directory)) {
    return listings.get(directory);
  }
  let index: DirectoryIndex | undefined;
  try {
    const names = new Set<string>();
    const folded = new Map<string, string>();
    for (const name of readdirSync(directory)) {
      names.add(name);
      const key = name.toLowerCase();
      if (!folded.has(key)) {
        folded.set(key, name);
      }
    }
    index = { names, folded };
  } catch {
    index = undefined;
  }
  listings.set(directory, index);
  return index;
}

/** Clears the memoised listings. Indexing runs read a tree that does not change. */
export function forgetEnginePaths(): void {
  resolved.clear();
  listings.clear();
}
