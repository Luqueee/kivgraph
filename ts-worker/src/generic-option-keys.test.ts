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

async function factsFor(source: string) {
  const root = await temporaryRoot("kivgraph-generic-keys-");
  roots.push(root);
  await mkdir(path.join(root, "src"));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({ name: "generic-keys", version: "1.0.0", type: "module" }),
  );
  await writeFile(
    path.join(root, "tsconfig.json"),
    JSON.stringify({
      compilerOptions: { strict: true, target: "ES2022", noEmit: true },
      include: ["src/*.ts"],
    }),
  );
  await writeFile(path.join(root, "src/options.ts"), source);
  return collectFacts("generic-keys", root, createPackageProviderRegistry([]));
}

// A key of an object literal names the property of the type the literal is
// checked against. When the call is generic over its argument the checker
// answers with the type it inferred from the literal itself, so the key found
// there is the literal's own and points at nothing declared. The declared type
// is the constraint of the parameter.
it("resolves the keys of a literal passed to a generic call to the declared properties", async () => {
  const payload =
    await factsFor(`export type Https = { rejectUnauthorized?: boolean };
export type Options = { https?: Https; retries?: number };
export type Other = { retries?: number; unrelated?: string };
export declare function extend<T extends Array<Options>>(...options: T): T;
export declare function configure<T extends Options>(options: T): T;
extend({ https: { rejectUnauthorized: false }, retries: 1 });
configure({ https: { rejectUnauthorized: true }, unrelated: "x" });
`);
  const declaration = (qualifiedName: string): number | undefined =>
    payload.symbols.find((symbol) => symbol.qualifiedName === qualifiedName)
      ?.start;
  const keys = payload.references
    .filter((reference) => reference.kind === "REFERENCES")
    .map(
      (reference) =>
        `${reference.text}@${reference.startLine}->${reference.targetQualifiedName}`,
    )
    .filter((use) =>
      /^(https|rejectUnauthorized|retries|unrelated)@/.test(use),
    );
  expect(keys.sort()).toEqual([
    "https@6->Options.https",
    "https@7->Options.https",
    "rejectUnauthorized@6->Https.rejectUnauthorized",
    "rejectUnauthorized@7->Https.rejectUnauthorized",
    "retries@6->Options.retries",
  ]);
  // `unrelated` is declared on a type the constraint does not mention, so the
  // key at line 7 reaches nothing, and `retries` never reaches `Other`.
  expect(declaration("Other.unrelated")).toBeDefined();
});
