import assert from "node:assert/strict";
import test from "node:test";
import {
  auditBuiltSite,
  extractPageSignals,
  normalisePath,
} from "./seo-audit.mjs";

const description = "A".repeat(120);
const article = (path, links = ["/"]) => ({
  pathname: path,
  html: `<html><head><title>How to Build Better Code Intelligence for AI Agents</title><meta name="description" content="${description}"><link rel="canonical" href="https://kivgraph.dev${path}"><link rel="alternate" type="text/markdown" href="/raw${path.slice(0, -1)}.md"><script type="application/ld+json">{"@type":"BlogPosting"}</script></head><body><h1>Article</h1><div class="blog-prose"><p>Direct answer.</p></div>${links.map((link) => `<a href="${link}">link</a>`).join("")}</body></html>`,
});

test("normalisePath preserves files and adds the site's trailing slash", () => {
  assert.equal(normalisePath("/blog/post"), "/blog/post/");
  assert.equal(normalisePath("/raw/blog/post.md"), "/raw/blog/post.md");
  assert.equal(normalisePath("/"), "/");
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

test("the audit catches missing structural SEO signals and orphan routes", () => {
  const issues = auditBuiltSite({
    documents: [
      {
        pathname: "/",
        html: "<html><head></head><body><h1>Home</h1></body></html>",
      },
      article("/blog/linked/", ["/"]),
      article("/blog/orphan/"),
    ],
    files: new Set(["robots.txt"]),
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
  });

  assert.deepEqual(issues, []);
});
