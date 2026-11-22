import { expect, it } from "vitest";
import { findOwner } from "./reference-extractor.js";
import type { LocalSymbol } from "./symbol-extractor.js";

// The owner used to be found by scanning every symbol of the program for every
// reference. It is found through an index by file now, and an index that is
// wrong does not fail: it answers. So it is held to the scan, position by
// position, over ranges generated to nest, overlap and tie.
function scan(
  symbols: readonly LocalSymbol[],
  fileName: string,
  position: number,
): LocalSymbol | undefined {
  let owner: LocalSymbol | undefined;
  for (const candidate of symbols) {
    if (
      candidate.fileName !== fileName ||
      position < candidate.start ||
      position >= candidate.end
    ) {
      continue;
    }
    if (
      owner === undefined ||
      candidate.end - candidate.start < owner.end - owner.start ||
      (candidate.end - candidate.start === owner.end - owner.start &&
        candidate.start < owner.start)
    ) {
      owner = candidate;
    }
  }
  return owner;
}

it("finds the same owner as a scan of every symbol", () => {
  let seed = 7;
  const random = (bound: number): number => {
    seed = (seed * 1103515245 + 12345) % 2147483648;
    return seed % bound;
  };
  const files = ["a.ts", "b.ts", "c.ts"];
  const symbols: LocalSymbol[] = [];
  for (let index = 0; index < 400; index++) {
    const start = random(200);
    // Widths of 0 to 40, so that some ranges are empty and many tie.
    symbols.push({
      fileName: files[random(files.length)],
      start,
      end: start + random(41),
    } as LocalSymbol);
  }
  let owned = 0;
  for (const fileName of [...files, "absent.ts"]) {
    for (let position = -1; position <= 245; position++) {
      const expected = scan(symbols, fileName, position);
      expect(
        findOwner(symbols, fileName, position),
        `${fileName} @${position}`,
      ).toBe(expected);
      if (expected !== undefined) owned++;
    }
  }
  // Both sides of the question were asked.
  expect(owned).toBeGreaterThan(300);
  expect(findOwner([], "a.ts", 3)).toBeUndefined();
});
