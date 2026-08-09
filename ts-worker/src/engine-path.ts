import { readdirSync, statSync } from "node:fs";
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
const folding = new Map<string, boolean>();

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
  if (index === components.length) {
    resolved.set(candidate, current);
    return current;
  }
  // The walk stopped on a component that is not there yet. Nothing is
  // memoised: an indexing run writes files while it reads the tree, and a
  // cached negative would outlive the reason for it.
  return join(current, ...components.slice(index));
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
  const folded = entries.folded.get(component.toLowerCase());
  if (folded === undefined || !foldsCase(directory, folded)) {
    return undefined;
  }
  return folded;
}

/**
 * foldsCase reports whether this directory's filesystem treats two spellings
 * of a name as the same entry.
 *
 * Without this check the walk is destructive on a case-sensitive filesystem,
 * where `dist` and `Dist` are two different directories and answering with the
 * one that happens to exist rewrites the path to another file. The question is
 * asked of an entry that is really there: flip its case and see whether the
 * filesystem hands back the same inode.
 */
function foldsCase(directory: string, entry: string): boolean {
  const known = folding.get(directory);
  if (known !== undefined) {
    return known;
  }
  let answer = false;
  const flipped = entry
    .split("")
    .map((character) => {
      const upper = character.toUpperCase();
      return character === upper ? character.toLowerCase() : upper;
    })
    .join("");
  if (flipped !== entry) {
    try {
      const original = statSync(join(directory, entry));
      const other = statSync(join(directory, flipped));
      answer = original.ino === other.ino && original.dev === other.dev;
    } catch {
      answer = false;
    }
  }
  folding.set(directory, answer);
  return answer;
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
  folding.clear();
}
