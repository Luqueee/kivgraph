import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(
  new URL("./components/landing/TopBar.astro", import.meta.url),
  "utf8",
);

test("desktop topbar keeps the logo and navigation on one row", () => {
  assert.match(source, /class="[^"]*flex-wrap[^"]*lg:flex-nowrap/);
  assert.match(source, /class="hidden text-gray-400 xl:inline"/);
  assert.match(source, /class="ml-auto[^"]*lg:hidden/);
  assert.match(
    source,
    /class="hidden w-full basis-full flex-col[^"]*lg:flex[^"]*lg:gap-x-4/,
  );
});
