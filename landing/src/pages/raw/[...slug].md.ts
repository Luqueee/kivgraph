import type { APIRoute } from "astro";
import {
  LICENSE_NAME,
  LICENSE_URL,
  PROJECT_NAME,
  absoluteUrl,
  docPathname,
  loadDocsInOrder,
} from "../_seo";

/**
 * One markdown twin per published page, so `docs/tools/find-references` is
 * served at `/raw/docs/tools/find-references.md`. That is the URL the Starlight
 * `Head` override advertises with `rel="alternate" type="text/markdown"` and the
 * URL `llms.txt` links to; all three read `rawPathname` from `_seo.ts`, which is
 * what keeps them the same URL.
 *
 * Rendered on demand rather than prerendered, and that is the whole point: a
 * prerendered dynamic route under `output: "server"` still *matches* every
 * `/raw/**.md` the router is handed, but carries no component instance for the
 * paths `getStaticPaths` never emitted, so Astro throws inside its own pipeline
 * before any handler of ours can run. The namespace answered `500` to
 * everything unknown -- a server error where a crawler should read a `404`, and
 * `/raw/releases.md` was exactly that: a URL this site advertised for a page
 * that lives in `src/pages`, not in the `docs` collection, and never had a twin.
 * Handling it here costs one collection read and a string join per request.
 */
export const prerender = false;

export const GET: APIRoute = async ({ params, site }) => {
  const slug = params.slug;
  const entries = await loadDocsInOrder();
  const entry =
    slug === undefined ? undefined : entries.find((page) => page.id === slug);

  if (entry === undefined) {
    return new Response(
      `No markdown source is published at /raw/${slug}.md\n`,
      {
        status: 404,
        headers: { "Content-Type": "text/plain; charset=utf-8" },
      },
    );
  }

  // The header is plain markdown, not a comment: whatever reads this file reads
  // markdown, and an HTML comment would be noise in a plain-text pipeline. The
  // body goes out exactly as authored, fences and captured tool output included.
  const body = [
    `# ${entry.data.title}`,
    "",
    `Source: ${absoluteUrl(site, docPathname(entry.id))}`,
    `${PROJECT_NAME} documentation, licensed ${LICENSE_NAME} (${LICENSE_URL}).`,
    "",
    "---",
    "",
    (entry.body ?? "").trim(),
    "",
  ].join("\n");

  return new Response(body, {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
};
