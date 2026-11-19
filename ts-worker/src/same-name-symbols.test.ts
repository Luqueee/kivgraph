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

// A qualified name is not unique in a file: the parameter of an arrow function
// assigned to a `const` is just its own name, and a method of an object literal
// can share it. `ky`'s `body.ts` has five symbols called `value`, and the Go side
// resolved every reference to whichever came last. The worker says where each
// declaration starts so the name does not have to.
it("says which declaration a use belongs to when names are shared", async () => {
  const root = await temporaryRoot("kivgraph-same-name-");
  roots.push(root);
  await mkdir(path.join(root, "src"));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({ name: "same-name", version: "1.0.0", type: "module" }),
  );
  await writeFile(
    path.join(root, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: { strict: true, target: "ES2022", noEmit: true },
      include: ["src/*.ts"],
    }),
  );
  await writeFile(
    path.join(root, "src/body.ts"),
    `export const cancel = (value: string): number => value.length;
export const make = {
  value() {
    return 1;
  },
};
export const total = (value: number): number => value + 1;
`,
  );

  const payload = await collectFacts(
    "same-name",
    root,
    createPackageProviderRegistry([]),
  );
  const named = payload.symbols.filter(
    (symbol) => symbol.qualifiedName === "value",
  );
  expect(named.map((symbol) => symbol.kind).sort()).toEqual([
    "method",
    "parameter",
    "parameter",
  ]);
  expect(new Set(named.map((symbol) => symbol.start)).size).toBe(3);

  const startOf = new Map(named.map((symbol) => [symbol.start, symbol]));
  const uses = payload.references.filter(
    (reference) => reference.text === "value",
  );
  expect(uses).toHaveLength(2);
  for (const use of uses) {
    // Each use reaches the parameter of the function it sits in, never the
    // method that happens to share the name.
    const target = startOf.get(use.targetStart);
    expect(target?.kind).toBe("parameter");
    expect(use.targetStart).toBeLessThan(use.start);
  }
  expect(new Set(uses.map((use) => use.targetStart)).size).toBe(2);
});
