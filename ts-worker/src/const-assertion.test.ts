import { mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { afterEach, expect, it } from "vitest";
import { collectFacts } from "./facts-cli.js";
import { createPackageProviderRegistry } from "./package-import-resolver.js";
import { temporaryRoot } from "./temporary-root.js";

const roots: string[] = [];
afterEach(async () => {
  await Promise.all(
    roots.splice(0).map((root) => rm(root, { recursive: true, force: true })),
  );
});

// `[] as const` is one line of ordinary TypeScript. The engine panics when it
// is asked for the type of that `const` -- a type reference it reads as a
// tuple -- and the worker died with it: on real code one empty tuple left a
// program of several thousand files, and the six packages that share it, out
// of the graph.
it("indexes a file that holds an empty tuple asserted as const", async () => {
  const root = await temporaryRoot("kivgraph-const-assertion-");
  roots.push(root);
  await mkdir(path.join(root, "src"));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({
      name: "const-assertion",
      version: "1.0.0",
      type: "module",
    }),
  );
  await writeFile(
    path.join(root, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: { strict: true, target: "ES2022", noEmit: true },
      include: ["src/*.ts"],
    }),
  );
  await writeFile(
    path.join(root, "src/index.ts"),
    `export const none = [] as const;
export const some = [1, 2] as const;
export class Box {}
export function open(box: Box): Box {
  return box;
}
export const opened = open(new Box());
`,
  );
  const payload = await collectFacts(
    "const-assertion",
    root,
    createPackageProviderRegistry([]),
  );
  const uses = payload.references
    .map((reference) => `${reference.kind} ${reference.text}`)
    .sort();
  expect(uses).toEqual([
    "CALLS_DIRECT Box",
    "CALLS_DIRECT open",
    "REFERENCES box",
    "TYPE_USES Box",
    "TYPE_USES Box",
  ]);
  expect(payload.symbols.map((symbol) => symbol.name)).toContain("none");
});
