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

// The checker has one symbol for a `get`/`set` pair and both declarations are
// published. Every use went to the first, so the setter had no reference and
// an answer about it said nothing assigns the property.
it("sends a write to the setter and a read to the getter", async () => {
  const root = await temporaryRoot("kivgraph-accessor-pair-");
  roots.push(root);
  await mkdir(path.join(root, "src"));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({ name: "accessor-pair", version: "1.0.0", type: "module" }),
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
    `export class Box {
  #size = 0;
  get size(): number {
    return this.#size;
  }
  set size(value: number) {
    this.#size = value;
  }
  get only(): number {
    return 1;
  }
}
export function read(box: Box): number {
  return box.size + box.only;
}
export function write(box: Box): void {
  box.size = 2;
}
export function grow(box: Box): void {
  box.size += 1;
}
export function step(box: Box): void {
  box.size++;
}
export function parenWrite(box: Box): void {
  (box.size) = 2;
}
export function parenStep(box: Box): void {
  (box.size)++;
}
`,
  );
  const payload = await collectFacts(
    "accessor-pair",
    root,
    createPackageProviderRegistry([]),
  );
  const accessor = new Map(
    payload.symbols
      .filter((symbol) => symbol.name === "size" || symbol.name === "only")
      .map((symbol) => [
        symbol.start,
        `${symbol.signature.startsWith("set") ? "set" : "get"} ${symbol.name}`,
      ]),
  );
  expect([...accessor.values()].sort()).toEqual([
    "get only",
    "get size",
    "set size",
  ]);
  const uses = payload.references
    .filter((reference) => accessor.has(reference.targetStart))
    .map(
      (reference) =>
        `${reference.sourceQualifiedName} -> ${accessor.get(reference.targetStart)}`,
    )
    .sort();
  expect(uses).toEqual([
    "grow -> get size",
    "grow -> set size",
    "parenStep -> get size",
    "parenStep -> set size",
    "parenWrite -> set size",
    "read -> get only",
    "read -> get size",
    "step -> get size",
    "step -> set size",
    "write -> set size",
  ]);
});
