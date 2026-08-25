// The identity of the project, in plain ESM so `astro.config.mjs` can import it.
//
// This file exists because of what `astro.config.mjs` is: not a module Vite
// transforms, so it cannot import `_seo.ts` -- that file pulls in
// `astro:content`, which only exists inside the build. The config therefore
// carried its own copy of the name and the tagline, and the copy had already
// drifted into a second, different sentence while `_seo.ts` documented that the
// two matched. Nothing here may import from `astro:*`.
//
// `_seo.ts` re-exports every constant below, so it stays the module every
// component and endpoint imports from and none of them had to change.
//
// One exception, and it has to be hand-synced: `landing/public/site.webmanifest`
// is static JSON copied verbatim into `dist/`, so it cannot import this. Its
// `description` must be edited to match PROJECT_TAGLINE whenever that changes.

export const PROJECT_NAME = "Kivgraph";

/**
 * One sentence, reused as the `description` of the site, of the software, of the
 * Starlight integration, of the web app manifest and of any page that carries no
 * `description` of its own.
 */
export const PROJECT_TAGLINE =
  "A local cross-repository code intelligence MCP server for AI coding agents.";

/**
 * The paragraph an agent needs before it reads anything else: what the thing is,
 * and the one property that separates it from a grep.
 */
export const PROJECT_SUMMARY =
  "Kivgraph builds a canonical semantic code graph across registered Go, TypeScript, Rust, Python and Dart repositories. It answers questions about symbols, repository relationships, callers, dependencies and change impact locally through MCP. Results carry analyzer evidence or remain CANDIDATE/UNRESOLVED; they are never invented from matching names.";

export const REPOSITORY_URL = "https://github.com/Luqueee/kivgraph";

export const LICENSE_NAME = "Apache-2.0";

/** The SPDX page for the license, which is what `schema.org/license` wants. */
export const LICENSE_URL = "https://spdx.org/licenses/Apache-2.0.html";

/**
 * What `landing/public/og.png` shows, for the reader whose client renders the
 * alt text instead of the image. It describes the picture rather than restating
 * the tagline, which the `og:description` beside it already carries. Both heads
 * read it from here; the card itself is rendered from
 * `landing/scripts/social-card.html`.
 */
export const PREVIEW_ALT =
  "A dark card headed by the Kivgraph mark and name, reading: Know what breaks before your agent changes it — semantic code intelligence for coding agents.";
