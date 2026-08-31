// The `lastmod` of every sitemap entry, derived from git rather than from the
// filesystem. Plain ESM for the same reason `site.mjs` is: `astro.config.mjs`
// is not a module Vite transforms, so it cannot import anything that touches
// `astro:content`.
//
// Google uses `lastmod` to schedule recrawls and **ignores the field across the
// whole sitemap when it does not trust it**, so the only two acceptable answers
// are an accurate date and no date at all. That is why every failure path here
// returns nothing instead of a guess:
//
//   - `git` missing, or the build running outside a checkout;
//   - a shallow clone, where every file collapses onto the tip commit. Not
//     hypothetical: `.github/workflows/ci.yml` checks out at the default
//     depth of 1, while `release.yml` passes `fetch-depth: 0`;
//   - a URL whose source file this module cannot name.
//
// A build `mtime` is never the answer: it is the date of the build, not of the
// content, and it would mark all 37 URLs as changed on every deploy.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SRC_DIR = path.dirname(fileURLToPath(import.meta.url));
const LANDING_DIR = path.dirname(SRC_DIR);
const REPO_DIR = path.dirname(LANDING_DIR);

/** Repository-relative, because that is how `git log --name-only` prints. */
const SRC_PREFIX = `${path.basename(LANDING_DIR)}/src`;

function git(args) {
  return execFileSync("git", ["-C", REPO_DIR, ...args], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
}

const ISO_DATE = /^\d{4}-\d\d-\d\dT/;

/**
 * Every tracked file under `landing/src`, mapped to the committer date of the
 * most recent commit that touched it. One `git log` pass rather than one call
 * per file: 37 pages reach far more than 37 files.
 *
 * `--no-merges` because a merge commit reports its whole diff, which would
 * stamp every file in the branch with the date of the merge.
 */
function readCommitDates() {
  try {
    if (git(["rev-parse", "--is-shallow-repository"]).trim() !== "false") {
      return null;
    }
    const log = git([
      "log",
      "--format=%cI",
      "--name-only",
      "--no-merges",
      "--",
      SRC_PREFIX,
    ]);
    const dates = new Map();
    let commitDate = null;
    // Newest first, so the first mention of a path is its latest change.
    for (const line of log.split("\n")) {
      if (line === "") continue;
      if (ISO_DATE.test(line)) {
        commitDate = line;
      } else if (commitDate !== null && !dates.has(line)) {
        dates.set(line, commitDate);
      }
    }
    return dates.size === 0 ? null : dates;
  } catch {
    return null;
  }
}

const COMMIT_DATES = readCommitDates();

/** `landing/src/…` for a path relative to `src`, matching the git keys. */
function key(relativeToSrc) {
  return `${SRC_PREFIX}/${relativeToSrc}`;
}

function newest(relativePaths) {
  if (COMMIT_DATES === null) return undefined;
  let latest;
  for (const relative of relativePaths) {
    const date = COMMIT_DATES.get(key(relative));
    if (date !== undefined && (latest === undefined || date > latest)) {
      latest = date;
    }
  }
  return latest;
}

/**
 * The landing page renders no content of its own: it composes the components in
 * `components/landing`, and its date is the newest of the ones it imports plus
 * its own template. Reading the imports rather than the whole directory keeps a
 * component that the page dropped from still dating it.
 *
 * Stylesheets and the motion layer are deliberately out. They change how the
 * page looks, not what it says, and `lastmod` answers the second question.
 */
function homepageSources() {
  const page = "pages/index.astro";
  const sources = [page];
  const template = path.join(SRC_DIR, page);
  if (existsSync(template)) {
    const body = readFileSync(template, "utf8");
    for (const match of body.matchAll(
      /from\s+"\.\.\/components\/landing\/([A-Za-z]+\.astro)"/g,
    )) {
      sources.push(`components/landing/${match[1]}`);
    }
  }
  return sources;
}

/** `/docs/tools/find-references/` -> `content/docs/docs/tools/find-references`. */
function docsEntrySources(slug) {
  const base = `content/docs/${slug}`;
  return [`${base}.md`, `${base}.mdx`].filter((relative) =>
    existsSync(path.join(SRC_DIR, relative)),
  );
}

/** `/blog/semantic-code-search-vs-grep/` -> `content/blog/<slug>.md`. */
function blogEntrySources(slug) {
  const base = `content/blog/${slug}`;
  return [`${base}.md`, `${base}.mdx`].filter((relative) =>
    existsSync(path.join(SRC_DIR, relative)),
  );
}

/** The blog index changes when its route or one of its published entries does. */
function blogIndexSources() {
  const entries = [];
  if (COMMIT_DATES !== null) {
    for (const tracked of COMMIT_DATES.keys()) {
      if (tracked.startsWith(`${SRC_PREFIX}/content/blog/`)) {
        const relative = tracked.slice(SRC_PREFIX.length + 1);
        if (!isBlogDraft(relative)) entries.push(relative);
      }
    }
  }
  return ["pages/blog/index.astro", ...entries];
}

/** A draft does not change the public blog index until its flag is removed. */
function isBlogDraft(relative) {
  try {
    return /^draft:\s*true\s*$/m.test(
      readFileSync(path.join(SRC_DIR, relative), "utf8"),
    );
  } catch {
    return false;
  }
}

/**
 * The date to publish for one sitemap URL, or `undefined` to publish none.
 *
 * @param {string} url Absolute URL, as `@astrojs/sitemap` hands it over.
 * @returns {string | undefined} An ISO 8601 timestamp.
 */
export function lastmodFor(url) {
  if (COMMIT_DATES === null) return undefined;

  const slug = new URL(url).pathname.replace(/^\/|\/$/g, "");

  if (slug === "") return newest(homepageSources());

  // `/releases/` is a page that renders a collection, so both date it.
  if (slug === "releases") {
    const notes = [];
    if (COMMIT_DATES !== null) {
      for (const tracked of COMMIT_DATES.keys()) {
        if (tracked.startsWith(`${SRC_PREFIX}/content/releases/`)) {
          notes.push(tracked.slice(SRC_PREFIX.length + 1));
        }
      }
    }
    return newest(["pages/releases.astro", ...notes]);
  }

  if (slug === "blog") return newest(blogIndexSources());

  if (slug.startsWith("blog/")) {
    return newest(blogEntrySources(slug.slice("blog/".length)));
  }

  const entry = docsEntrySources(slug);
  return entry.length === 0 ? undefined : newest(entry);
}
