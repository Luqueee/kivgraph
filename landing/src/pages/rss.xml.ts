import type { APIRoute } from "astro";
import { absoluteUrl, blogPathname, loadBlogEntries } from "./_seo";
import { escapeXml } from "../rss.mjs";

export const prerender = true;

export const GET: APIRoute = async ({ site }) => {
  const entries = await loadBlogEntries();
  const origin = absoluteUrl(site, "/");
  const self = absoluteUrl(site, "/rss.xml");
  const items = entries
    .map((entry) => {
      const url = absoluteUrl(site, blogPathname(entry.id));
      return [
        "    <item>",
        `      <title>${escapeXml(entry.data.title)}</title>`,
        `      <link>${escapeXml(url)}</link>`,
        `      <guid isPermaLink="true">${escapeXml(url)}</guid>`,
        `      <description>${escapeXml(entry.data.description)}</description>`,
        `      <pubDate>${entry.data.pubDate.toUTCString()}</pubDate>`,
        `      <category>${escapeXml(entry.data.category)}</category>`,
        "    </item>",
      ].join("\n");
    })
    .join("\n");
  const latestChange = entries.reduce(
    (latest, entry) =>
      Math.max(
        latest,
        (entry.data.updatedDate ?? entry.data.pubDate).valueOf(),
      ),
    0,
  );
  const lastBuildDate = latestChange
    ? new Date(latestChange).toUTCString()
    : new Date().toUTCString();
  const xml = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">',
    "  <channel>",
    "    <title>Kivgraph Blog</title>",
    "    <description>Practical writing about AI coding agents, MCP and code intelligence.</description>",
    `    <link>${escapeXml(origin)}</link>`,
    `    <atom:link href="${escapeXml(self)}" rel="self" type="application/rss+xml" />`,
    `    <lastBuildDate>${lastBuildDate}</lastBuildDate>`,
    items,
    "  </channel>",
    "</rss>",
    "",
  ].join("\n");

  return new Response(xml, {
    headers: { "Content-Type": "application/rss+xml; charset=utf-8" },
  });
};
