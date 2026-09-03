import assert from "node:assert/strict";
import test from "node:test";
import { escapeXml } from "./rss.mjs";

test("XML escaping removes markup from feed content", () => {
  assert.equal(
    escapeXml('<title data-x="1">A & B</title>'),
    "&lt;title data-x=&quot;1&quot;&gt;A &amp; B&lt;/title&gt;",
  );
});

test("XML escaping handles apostrophes", () => {
  assert.equal(escapeXml("agent's code"), "agent&apos;s code");
});
