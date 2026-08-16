import type { APIRoute } from "astro";
import { absoluteUrl } from "./_seo";

export const prerender = true;

/**
 * The date the crawler tokens below were last checked against their operator's
 * own documentation. It is printed into the file so whoever audits it knows how
 * old the list is without reading this source.
 */
const TOKENS_VERIFIED_ON = "2026-08-15";

interface CrawlerOperator {
  readonly name: string;
  /** The page the tokens were read from. */
  readonly source: string;
  readonly tokens: readonly string[];
}

/**
 * AI crawlers and answer engines, every token read from the operator's own
 * published documentation rather than from a list someone copied around. A
 * token no operator publishes does not belong in this file.
 *
 * Inclusion follows purpose: a token is here if its operator says it reads site
 * content for model training, for an AI search index, or for a user-directed
 * fetch. Three of the user-directed ones — `ChatGPT-User`, `Perplexity-User`,
 * `Meta-ExternalFetcher` — are documented as possibly ignoring robots.txt. An
 * allow they ignore costs nothing, and an auditor reading this file expects
 * every published token to be accounted for rather than silently dropped.
 *
 * Left out: `OAI-AdsBot` and `Meta-ExternalAds`, which only fetch pages
 * submitted as ads, and `facebookexternalhit`, which builds link previews and
 * is not an answer engine.
 */
const AI_CRAWLERS: readonly CrawlerOperator[] = [
  {
    name: "OpenAI",
    source: "https://developers.openai.com/api/docs/bots",
    tokens: ["GPTBot", "OAI-SearchBot", "ChatGPT-User"],
  },
  {
    name: "Anthropic",
    source:
      "https://support.claude.com/en/articles/8896518-does-anthropic-crawl-data-from-the-web-and-how-can-site-owners-block-the-crawler",
    tokens: ["ClaudeBot", "Claude-SearchBot", "Claude-User"],
  },
  {
    name: "Perplexity",
    source: "https://docs.perplexity.ai/docs/resources/perplexity-crawlers",
    tokens: ["PerplexityBot", "Perplexity-User"],
  },
  {
    name: "Google",
    source:
      "https://developers.google.com/crawling/docs/crawlers-fetchers/google-common-crawlers",
    tokens: ["Google-Extended"],
  },
  {
    name: "Apple",
    source: "https://support.apple.com/en-us/119829",
    tokens: ["Applebot", "Applebot-Extended"],
  },
  {
    name: "Common Crawl",
    source: "https://commoncrawl.org/ccbot",
    tokens: ["CCBot"],
  },
  {
    name: "Meta",
    source:
      "https://developers.facebook.com/documentation/sharing/webmasters/web-crawlers",
    tokens: ["meta-externalagent", "Meta-WebIndexer", "Meta-ExternalFetcher"],
  },
];

export const GET: APIRoute = ({ site }) => {
  // Consecutive `User-agent` lines form one group that shares the rules below
  // them, so every token named here gets the same `Allow: /`. The group is
  // redundant against `User-agent: *` and exists to be read: an explicit allow
  // is the signal an operator looks for, and `Google-Extended` and
  // `Applebot-Extended` have no meaning anywhere but in this file.
  const lines = [
    "# Kivgraph documentation.",
    "#",
    `# The agent-facing index of this site is ${absoluteUrl(site, "/llms.txt")}`,
    `# and the whole documentation in one fetch is ${absoluteUrl(site, "/llms-full.txt")}`,
    "# Every documentation page also has a markdown source at /raw/<path>.md",
    "",
    "User-agent: *",
    "Allow: /",
    "",
    "# AI crawlers and answer engines, allowed explicitly. Every token below was",
    `# read from its operator's own documentation on ${TOKENS_VERIFIED_ON}:`,
  ];

  for (const operator of AI_CRAWLERS) {
    lines.push(`#   ${operator.name}: ${operator.source}`);
  }

  lines.push("");
  for (const operator of AI_CRAWLERS) {
    for (const token of operator.tokens) {
      lines.push(`User-agent: ${token}`);
    }
  }
  lines.push("Allow: /");

  // `@astrojs/sitemap`, which Starlight adds, writes its index as
  // `<filenameBase>-index.xml` with `filenameBase` defaulting to `sitemap`.
  lines.push("", `Sitemap: ${absoluteUrl(site, "/sitemap-index.xml")}`, "");

  return new Response(lines.join("\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
