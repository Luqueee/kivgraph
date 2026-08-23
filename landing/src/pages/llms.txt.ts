import type { APIRoute } from "astro";
import {
  LICENSE_NAME,
  LICENSE_URL,
  MCP_TOOLS,
  PROJECT_NAME,
  PROJECT_SUMMARY,
  PROJECT_TAGLINE,
  REPOSITORY_URL,
  absoluteUrl,
  docDescription,
  loadDocGroups,
  rawPathname,
} from "./_seo";

export const prerender = true;

/**
 * The llms.txt index, in the order llmstxt.org specifies: an H1, a blockquote
 * summary, non-heading prose, then H2 "file list" sections.
 *
 * Two things follow the spec rather than the obvious reading. The links point at
 * the markdown source of each page, not at the HTML, because the spec says the
 * links "should point to LLM-friendly content"; the HTML URL of any of them is
 * the same path without the `/raw/` prefix and the `.md` suffix, and that rule
 * is stated in the file so an agent can navigate either way. And the closing
 * section is called "Optional", which the spec reserves by convention for links
 * an agent may skip when its context is short.
 *
 * Sections come from `getCollection`, so a page added to the collection appears
 * here without anyone editing this file.
 */
export const GET: APIRoute = async ({ site }) => {
  const groups = await loadDocGroups();

  const lines = [
    `# ${PROJECT_NAME}`,
    "",
    `> ${PROJECT_TAGLINE}`,
    "",
    PROJECT_SUMMARY,
    "",
    "How to read this file:",
    "",
    "- Every link below is the markdown source of one documentation page. Its HTML page is the same path without the `/raw/` prefix and with `.md` replaced by a trailing slash, so `/raw/install.md` is served as HTML at `/install/`.",
    `- The whole documentation in a single fetch: ${absoluteUrl(site, "/llms-full.txt")}`,
    `- Licensed ${LICENSE_NAME} (${LICENSE_URL}). Source: ${REPOSITORY_URL}`,
    `- The server registers ${MCP_TOOLS.length} tools over stdio: ${MCP_TOOLS.join(", ")}.`,
    "",
  ];

  for (const group of groups) {
    lines.push(`## ${group.title}`, "");
    for (const entry of group.entries) {
      const url = absoluteUrl(site, rawPathname(entry.id));
      lines.push(`- [${entry.data.title}](${url}): ${docDescription(entry)}`);
    }
    lines.push("");
  }

  lines.push(
    "## Optional",
    `- [Releases](${absoluteUrl(site, "/releases/")}): release notes and upgrade instructions.`,
    `- [Repository](${REPOSITORY_URL}): source and issue tracker.`,
    `- [License](${LICENSE_URL}): ${LICENSE_NAME}.`,
    "",
  );

  return new Response(lines.join("\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
