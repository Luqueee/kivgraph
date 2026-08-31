import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const route = readFileSync(
  new URL("./pages/blog/[...slug].astro", import.meta.url),
  "utf8",
);
const globalStyles = readFileSync(
  new URL("./styles/global.css", import.meta.url),
  "utf8",
);

test("blog prose does not override Expressive Code frame internals", () => {
  assert.match(route, /:global\(pre:not\(:where\(\.expressive-code \*\)\)\)/);
});

test("landing Expressive Code skin owns controls and mobile sizing", () => {
  assert.match(globalStyles, /--ec-frm-copyIcon:/);
  assert.match(globalStyles, /--ec-frm-trmIcon:/);
  assert.match(globalStyles, /--ec-frm-trmTtbDotsFg:/);
  assert.match(globalStyles, /--ec-frm-tooltipSuccessBg:/);
  assert.match(globalStyles, /\.expressive-code \.frame \{/);
  assert.match(globalStyles, /\.expressive-code \.frame pre \{/);
  assert.match(globalStyles, /max-width: 100%/);
});
