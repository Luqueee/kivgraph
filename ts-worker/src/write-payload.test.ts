import { readFile, rm } from "node:fs/promises";
import path from "node:path";
import { afterEach, expect, it } from "vitest";
import { writePayload } from "./facts-cli.js";
import { temporaryRoot } from "./temporary-root.js";

const roots: string[] = [];
afterEach(async () => {
  await Promise.all(
    roots.splice(0).map((root) => rm(root, { recursive: true, force: true })),
  );
});

// The payload is written a slice at a time so that it is never one string.
// What is read back has to be the value that was given, at every size around
// a slice.
it("writes a payload that reads back as the value it was given", async () => {
  const root = await temporaryRoot("kivgraph-write-payload-");
  roots.push(root);
  const row = (index: number) => ({
    file: `src/${index}.ts`,
    text: 'quote " comma , bracket ] brace } newline \n',
    start: index,
    nested: [index, [index]],
    none: null,
  });
  for (const length of [0, 1, 4095, 4096, 4097, 9000]) {
    const payload = {
      version: 5,
      repository: { name: "r" },
      package: null,
      missing: undefined,
      files: Array.from({ length }, (_, index) => `src/${index}.ts`),
      references: Array.from({ length }, (_, index) => row(index)),
      empty: [],
    };
    const filename = path.join(root, `${length}.json`);
    await writePayload(filename, payload);
    const text = await readFile(filename, "utf8");
    expect(text.endsWith("}\n")).toBe(true);
    expect(JSON.parse(text)).toEqual(JSON.parse(JSON.stringify(payload)));
  }
  await writePayload(path.join(root, "bare.json"), {});
  expect(await readFile(path.join(root, "bare.json"), "utf8")).toBe("{}\n");
  await expect(
    writePayload(path.join(root, "no", "such", "directory.json"), { a: 1 }),
  ).rejects.toThrow();
});
