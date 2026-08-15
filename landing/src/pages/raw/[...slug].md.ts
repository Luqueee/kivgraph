import type { APIRoute, GetStaticPaths } from "astro";
import {
  type DocEntry,
  LICENSE_NAME,
  LICENSE_URL,
  PROJECT_NAME,
  absoluteUrl,
  docPathname,
  loadDocsInOrder,
} from "../_seo";

export const prerender = true;

interface RawPageProps {
  entry: DocEntry;
}

/**
 * One route per published page, so `reference/tools/find-references` is served
 * at `/raw/reference/tools/find-references.md`. That is the URL the Starlight
 * `Head` override advertises with `rel="alternate" type="text/markdown"` and the
 * URL `llms.txt` links to; all three read `rawPathname` from `_seo.ts`, which is
 * what keeps them the same URL.
 */
export const getStaticPaths: GetStaticPaths = async () => {
  const entries = await loadDocsInOrder();
  return entries.map((entry) => ({
    params: { slug: entry.id },
    props: { entry } satisfies RawPageProps,
  }));
};

export const GET: APIRoute<RawPageProps> = ({ props, site }) => {
  const { entry } = props;

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
