import assert from "node:assert/strict";
import test from "node:test";
import {
  blogPathname,
  blogRawPathname,
  isPublishedBlog,
  sortBlogEntries,
} from "./blog.mjs";

test("draft entries are not published", () => {
  assert.equal(isPublishedBlog({ data: { draft: true } }), false);
});

test("entries without a draft flag remain publishable", () => {
  assert.equal(isPublishedBlog({ data: {} }), true);
});

test("canonical paths keep HTML and Markdown namespaces distinct", () => {
  assert.equal(
    blogPathname("semantic-code-search"),
    "/blog/semantic-code-search/",
  );
  assert.equal(
    blogRawPathname("semantic-code-search"),
    "/raw/blog/semantic-code-search.md",
  );
});

test("sorting returns newest first without changing the input", () => {
  const older = { data: { pubDate: new Date("2026-01-01") } };
  const newer = { data: { pubDate: new Date("2026-02-01") } };
  const entries = [older, newer];

  assert.deepEqual(sortBlogEntries(entries), [newer, older]);
  assert.deepEqual(entries, [older, newer]);
});
