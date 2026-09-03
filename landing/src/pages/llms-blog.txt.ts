import type { APIRoute } from "astro";
import {
  PROJECT_NAME,
  PROJECT_TAGLINE,
  absoluteUrl,
  blogRawPathname,
  blogPathname,
  loadBlogEntries,
} from "./_seo";

export const prerender = true;

/** A compact, agent-facing index of the published blog articles. */
export const GET: APIRoute = async ({ site }) => {
  const entries = await loadBlogEntries();
  const lines = [
    `# ${PROJECT_NAME} blog`,
    "",
    `> ${PROJECT_TAGLINE}`,
    "",
    "Practical articles about AI coding agents, MCP, code intelligence, semantic code search and software architecture.",
    "",
    "Each link below points to the Markdown version of a published article. The HTML page is the same path without /raw and with a trailing slash.",
    "",
  ];

  for (const entry of entries) {
    lines.push(
      `- [${entry.data.title}](${absoluteUrl(site, blogRawPathname(entry.id))}): ${entry.data.description} HTML: ${absoluteUrl(site, blogPathname(entry.id))}`,
    );
  }

  lines.push(
    "",
    `Documentation index: ${absoluteUrl(site, "/llms.txt")}`,
    `Repository: https://github.com/Luqueee/kivgraph`,
    "",
  );

  return new Response(lines.join("\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
