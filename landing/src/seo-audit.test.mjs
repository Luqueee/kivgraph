import assert from "node:assert/strict";
import test from "node:test";
import {
  auditBuiltSite,
  extractPageSignals,
  markdownPathForRoute,
  normalisePath,
} from "./seo-audit.mjs";

const description = "A".repeat(120);
const article = (
  path,
  links = ["/"],
  structuredTypes = ["BlogPosting", "BreadcrumbList"],
  markdown = markdownPathForRoute(path),
) => ({
  pathname: path,
  html: `<html><head><title>How to Build Better Code Intelligence for AI Agents</title><meta name="description" content="${description}"><link rel="canonical" href="https://kivgraph.dev${path}">${markdown ? `<link rel="alternate" type="text/markdown" href="${markdown}">` : ""}<script type="application/ld+json">${JSON.stringify({ "@graph": structuredTypes.map((type) => ({ "@type": type })) })}</script></head><body><h1>Article</h1><div class="blog-prose"><p>Direct answer.</p></div>${links.map((link) => `<a href="${link}">link</a>`).join("")}</body></html>`,
});

test("normalisePath preserves files and adds the site's trailing slash", () => {
  assert.equal(normalisePath("/blog/post"), "/blog/post/");
  assert.equal(normalisePath("/raw/blog/post.md"), "/raw/blog/post.md");
  assert.equal(normalisePath("/"), "/");
});

test("markdownPathForRoute pairs HTML routes with raw Markdown routes", () => {
  assert.equal(markdownPathForRoute("/blog/post/"), "/raw/blog/post.md");
  assert.equal(markdownPathForRoute("/docs/cli/"), "/raw/docs/cli.md");
  assert.equal(markdownPathForRoute("/"), undefined);
});

test("extractPageSignals reads metadata, JSON-LD and internal links", () => {
  const signals = extractPageSignals(
    article("/blog/post/", ["/", "/blog/"]).html,
  );
  assert.equal(signals.h1Count, 1);
  assert.equal(signals.description.length, 120);
  assert.equal(signals.canonical, "https://kivgraph.dev/blog/post/");
  assert.equal(signals.markdownAlternate, "/raw/blog/post.md");
  assert.equal(signals.structuredTypes.has("BlogPosting"), true);
  assert.deepEqual(signals.links, ["/", "/blog/"]);
});

test("HTML entity decoding is single-pass and title markup delimiters are removed", () => {
  const signals = extractPageSignals(
    '<title>&amp;lt;script&amp;gt;</title><meta name="description" content="ok">',
  );

  assert.equal(signals.title, "&lt;script&gt;");
});

test("the audit catches missing structural SEO signals and orphan routes", () => {
  const issues = auditBuiltSite({
    documents: [
      {
        pathname: "/",
        html: "<html><head></head><body><h1>Home</h1></body></html>",
      },
      article("/blog/linked/", ["/"], ["BlogPosting"]),
      article("/blog/orphan/", ["/"], ["BlogPosting"]),
    ],
    files: new Set(["robots.txt"]),
    runtimePaths: ["/raw/blog/linked.md", "/raw/blog/orphan.md"],
  });

  assert.equal(
    issues.some((item) => item.code === "missing-canonical"),
    true,
  );
  assert.equal(
    issues.some((item) => item.code === "missing-discovery-file"),
    true,
  );
  assert.equal(
    issues.some(
      (item) => item.code === "orphan-page" && item.path === "/blog/orphan/",
    ),
    true,
  );
  assert.equal(
    issues.some((item) => item.code === "missing-breadcrumblist"),
    true,
  );
});

test("the audit rejects foreign canonicals and missing local targets", () => {
  const post = article(
    "/blog/post/",
    ["/missing/"],
    ["BlogPosting"],
    "/raw/blog/post.md",
  );
  post.html = post.html.replace(
    'href="https://kivgraph.dev/blog/post/"',
    'href="https://other.example/blog/post/"',
  );
  const issues = auditBuiltSite({
    documents: [
      {
        pathname: "/",
        html: '<html><head><title>Home</title><meta name="description" content="Home"><link rel="canonical" href="https://kivgraph.dev/"></head><body><h1>Home</h1></body></html>',
      },
      post,
    ],
    files: new Set([
      "robots.txt",
      "sitemap-index.xml",
      "sitemap-0.xml",
      "llms.txt",
      "llms-full.txt",
      "llms-blog.txt",
      "rss.xml",
    ]),
    runtimePaths: [],
  });

  assert.equal(
    issues.some((item) => item.code === "canonical-origin"),
    true,
  );
  assert.equal(
    issues.some((item) => item.code === "missing-markdown-target"),
    true,
  );
  assert.equal(
    issues.some((item) => item.code === "broken-internal-link"),
    true,
  );

  const mismatch = auditBuiltSite({
    documents: [
      article("/blog/mismatch/", ["/"], ["BlogPosting"], "/raw/blog/other.md"),
    ],
    files: new Set([
      "robots.txt",
      "sitemap-index.xml",
      "sitemap-0.xml",
      "llms.txt",
      "llms-full.txt",
      "llms-blog.txt",
      "rss.xml",
    ]),
    runtimePaths: ["/raw/blog/mismatch.md"],
  });
  assert.equal(
    mismatch.some((item) => item.code === "markdown-alternate-mismatch"),
    true,
  );
});

test("runtime edge routes do not hide other broken local links", () => {
  const issues = auditBuiltSite({
    documents: [
      {
        pathname: "/",
        html: '<html><head><title>Home</title><meta name="description" content="Home"><link rel="canonical" href="https://kivgraph.dev/"></head><body><h1>Home</h1><a href="/github">GitHub</a><a href="/missing">Missing</a></body></html>',
      },
    ],
    files: new Set([
      "robots.txt",
      "sitemap-index.xml",
      "sitemap-0.xml",
      "llms.txt",
      "llms-full.txt",
      "llms-blog.txt",
      "rss.xml",
    ]),
    runtimePaths: ["/github"],
  });

  assert.equal(
    issues.some(
      (item) =>
        item.code === "broken-internal-link" &&
        item.message.includes("/github/"),
    ),
    false,
  );
  assert.equal(
    issues.some(
      (item) =>
        item.code === "broken-internal-link" &&
        item.message.includes("/missing/"),
    ),
    true,
  );
});

test("a linked blog article with the required discovery files passes", () => {
  const issues = auditBuiltSite({
    documents: [
      {
        pathname: "/",
        html: '<html><head><title>Home</title><meta name="description" content="Home"><link rel="canonical" href="https://kivgraph.dev/"></head><body><h1>Home</h1><a href="/blog/">Blog</a></body></html>',
      },
      {
        pathname: "/blog/",
        html: '<html><head><title>Blog</title><meta name="description" content="Blog"><link rel="canonical" href="https://kivgraph.dev/blog/"></head><body><h1>Blog</h1><a href="/blog/post/">Post</a></body></html>',
      },
      article("/blog/post/", ["/", "/blog/"]),
    ],
    files: new Set([
      "robots.txt",
      "sitemap-index.xml",
      "sitemap-0.xml",
      "llms.txt",
      "llms-full.txt",
      "llms-blog.txt",
      "rss.xml",
    ]),
    runtimePaths: ["/raw/blog/post.md"],
  });

  assert.deepEqual(issues, []);
});
