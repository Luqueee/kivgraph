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

// `await import()` is how a route or a command loads its code, and what it
// reaches through the namespace is a use of the declaration.
it("records what is used through a dynamic import", async () => {
  const root = await temporaryRoot("kivgraph-dynamic-import-");
  roots.push(root);
  await mkdir(path.join(root, "src"));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({ name: "dynamic", version: "1.0.0", type: "module" }),
  );
  await writeFile(
    path.join(root, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: {
        strict: true,
        target: "ES2022",
        module: "ESNext",
        moduleResolution: "Bundler",
        noEmit: true,
      },
      include: ["src/*.ts"],
    }),
  );
  await writeFile(
    path.join(root, "src/lib.ts"),
    `export function destructured(): number {
  return 1;
}
export function accessed(): number {
  return 2;
}
export function viaVariable(): number {
  return 3;
}
export default function fallback(): number {
  return 4;
}
function internal(): number {
  return 5;
}
export { internal as load };
`,
  );
  await writeFile(
    path.join(root, "src/index.ts"),
    `export async function one(): Promise<number> {
  const { destructured } = await import("./lib");
  return destructured();
}
export async function two(): Promise<number> {
  return (await import("./lib")).accessed();
}
export async function three(): Promise<number> {
  const lib = await import("./lib");
  return lib.viaVariable() + lib.default();
}
export async function four(): Promise<number> {
  const { load } = await import("./lib");
  return load();
}
`,
  );
  const payload = await collectFacts(
    "dynamic",
    root,
    createPackageProviderRegistry([]),
  );
  const uses = payload.references
    .filter((reference) => reference.targetFile === "src/lib.ts")
    .map(
      (reference) =>
        `${reference.sourceQualifiedName} -> ${reference.targetQualifiedName}`,
    );
  // The destructured name is a local of `one`, and that local is what reaches
  // the export: the binding is where the module's name is written.
  expect([...new Set(uses)].sort()).toEqual([
    "four.load -> internal",
    "one.destructured -> destructured",
    "three -> fallback",
    "three -> viaVariable",
    "two -> accessed",
  ]);
});
