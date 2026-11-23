import { mkdir, rm, symlink, writeFile } from "node:fs/promises";
import path from "node:path";
import { afterEach, expect, it } from "vitest";
import { collectFacts, siblingProviders } from "./facts-cli.js";
import { createPackageProviderRegistry } from "./package-import-resolver.js";
import { temporaryRoot } from "./temporary-root.js";

const roots: string[] = [];
afterEach(async () => {
  await Promise.all(
    roots.splice(0).map((root) => rm(root, { recursive: true, force: true })),
  );
});

const options = {
  strict: true,
  target: "ES2022",
  module: "ESNext",
  moduleResolution: "Bundler",
  noEmit: true,
};

async function put(root: string, name: string, contents: string) {
  await mkdir(path.dirname(path.join(root, name)), { recursive: true });
  await writeFile(path.join(root, name), contents);
}

// A workspace of three packages: `app` imports `lib` and `tokens` through the
// links a package manager leaves, and both publish their source. `tokens`
// declares no project.
async function workspace(): Promise<string> {
  const root = await temporaryRoot("kivgraph-sibling-");
  roots.push(root);
  await put(
    root,
    "packages/lib/package.json",
    JSON.stringify({ name: "@w/lib", version: "1.0.0", main: "src/index.ts" }),
  );
  await put(
    root,
    "packages/lib/tsconfig.json",
    JSON.stringify({ compilerOptions: options, include: ["src"] }),
  );
  await put(
    root,
    "packages/lib/src/index.ts",
    "export function fromLib(): number {\n  return 1;\n}\n",
  );
  await put(
    root,
    "packages/tokens/package.json",
    JSON.stringify({
      name: "@w/tokens",
      version: "1.0.0",
      main: "src/index.ts",
    }),
  );
  await put(
    root,
    "packages/tokens/src/index.ts",
    "export const fromTokens = 2;\n",
  );
  await put(
    root,
    "apps/app/package.json",
    JSON.stringify({ name: "@w/app", version: "1.0.0" }),
  );
  await put(
    root,
    "apps/app/tsconfig.json",
    JSON.stringify({ compilerOptions: options, include: ["src"] }),
  );
  await put(
    root,
    "apps/app/src/index.ts",
    `import { fromLib } from "@w/lib";
import { fromTokens } from "@w/tokens";
import { shipped } from "shipped";
export const sum = fromLib() + fromTokens + shipped;
`,
  );
  await mkdir(path.join(root, "apps/app/node_modules/@w"), { recursive: true });
  for (const name of ["lib", "tokens"]) {
    await symlink(
      path.join(root, "packages", name),
      path.join(root, "apps/app/node_modules/@w", name),
      "junction",
    );
  }
  // A dependency that ships its source, installed and not linked: nobody in
  // this repository declares it.
  await put(
    root,
    "apps/app/node_modules/shipped/package.json",
    JSON.stringify({ name: "shipped", version: "1.0.0", main: "index.ts" }),
  );
  await put(
    root,
    "apps/app/node_modules/shipped/index.ts",
    "export const shipped = 3;\n",
  );
  return root;
}

// An import of the package next door reaches its source, so the edge names
// the declaration that package publishes -- under the package that declares
// the file, though every sibling shares the repository root.
it("resolves an import of a sibling package to the source it publishes", async () => {
  const root = await workspace();
  const siblings = await siblingProviders("w", root, [
    path.join(root, "packages/lib"),
    path.join(root, "packages/tokens"),
  ]);
  const payload = await collectFacts(
    "w",
    root,
    createPackageProviderRegistry(siblings),
    "apps/app/tsconfig.json",
  );
  expect(
    payload.imports.map((entry) => [entry.requestedSymbol, entry.target]),
  ).toEqual([
    [
      "fromLib",
      {
        repository: "w",
        package: "@w/lib",
        qualifiedName: "fromLib",
        kind: "function",
        signature: "export function fromLib(): number",
        file: "packages/lib/src/index.ts",
        startLine: 1,
        source: "SOURCE",
      },
    ],
    [
      "fromTokens",
      {
        repository: "w",
        package: "@w/tokens",
        qualifiedName: "fromTokens",
        kind: "variable",
        signature: "fromTokens",
        file: "packages/tokens/src/index.ts",
        startLine: 1,
        source: "SOURCE",
      },
    ],
  ]);
  // The identity is the one the sibling gives its own declaration, or the
  // edge would dangle.
  const own = await collectFacts(
    "w",
    root,
    createPackageProviderRegistry([]),
    "packages/lib/tsconfig.json",
  );
  const target = payload.imports[0]?.target;
  expect(
    own.symbols
      .filter((symbol) => symbol.kind === "function")
      .map((symbol) => ({
        file: symbol.file,
        qualifiedName: symbol.qualifiedName,
        kind: symbol.kind,
        signature: symbol.signature,
        startLine: symbol.startLine,
      })),
  ).toEqual([
    {
      file: target?.file,
      qualifiedName: target?.qualifiedName,
      kind: target?.kind,
      signature: target?.signature,
      startLine: target?.startLine,
    },
  ]);
  // The installed dependency stays what it was: a package nobody provides.
  expect(
    payload.unresolved.map((entry) => [entry.requestedPackage, entry.reason]),
  ).toEqual([["shipped", "PACKAGE_PROVIDER_NOT_FOUND"]]);
});

// Without the sibling named, the same import is a package nobody provides:
// the edge is never made from the path alone.
it("makes no edge to a sibling nobody registered", async () => {
  const root = await workspace();
  const payload = await collectFacts(
    "w",
    root,
    createPackageProviderRegistry([]),
    "apps/app/tsconfig.json",
  );
  expect(payload.imports).toEqual([]);
  expect(
    payload.unresolved.map((entry) => entry.requestedPackage).sort(),
  ).toEqual(["@w/lib", "@w/tokens", "shipped"]);
});

it("refuses a sibling with no manifest", async () => {
  const root = await workspace();
  await expect(
    siblingProviders("w", root, [path.join(root, "packages")]),
  ).rejects.toThrow(/no valid package\.json/);
  await expect(
    siblingProviders("w", root, [path.dirname(root)]),
  ).rejects.toThrow(/escapes repository root/);
});

// A package that declares no project is read through one written outside
// the repository, and is still that package: its files, its manifest.
it("reads a package through a project written outside the repository", async () => {
  const root = await workspace();
  const outside = await temporaryRoot("kivgraph-sibling-project-");
  roots.push(outside);
  const project = path.join(outside, "tsconfig.json");
  await writeFile(
    project,
    JSON.stringify({
      compilerOptions: options,
      files: [path.join(root, "packages/tokens/src/index.ts")],
    }),
  );
  const packageRoot = path.join(root, "packages/tokens");
  const payload = await collectFacts(
    "w",
    root,
    createPackageProviderRegistry([]),
    project,
    [],
    packageRoot,
  );
  expect(payload.package).toEqual({
    name: "@w/tokens",
    version: "1.0.0",
    rootPath: "packages/tokens",
    manifestPath: "packages/tokens/package.json",
  });
  expect(payload.files).toEqual(["packages/tokens/src/index.ts"]);
  expect(
    payload.symbols
      .filter((symbol) => symbol.kind === "variable")
      .map((symbol) => [symbol.qualifiedName, symbol.signature]),
  ).toEqual([["fromTokens", "fromTokens"]]);

  const registry = createPackageProviderRegistry([]);
  await expect(
    collectFacts("w", root, registry, "tsconfig.json", [], packageRoot),
  ).rejects.toThrow(/absolute --project/);
  await expect(
    collectFacts("w", root, registry, project, [], "packages/tokens"),
  ).rejects.toThrow(/must be absolute/);
  await expect(
    collectFacts("w", root, registry, project, [], outside),
  ).rejects.toThrow(/escapes repository root/);
});

// One import proves a dependency, and the first may be in a file the
// repository excludes. Every occurrence travels, so the reader can take the
// first it keeps.
it("sends every occurrence of a package dependency", async () => {
  const root = await workspace();
  await put(
    root,
    "apps/app/src/zz-second.ts",
    'import { fromLib } from "@w/lib";\nexport const again = fromLib();\n',
  );
  const siblings = await siblingProviders("w", root, [
    path.join(root, "packages/lib"),
  ]);
  const payload = await collectFacts(
    "w",
    root,
    createPackageProviderRegistry(siblings),
    "apps/app/tsconfig.json",
  );
  expect(
    payload.dependencies.map((dependency) => [
      dependency.package,
      dependency.file,
      dependency.alternates.map((witness) => witness.file),
    ]),
  ).toEqual([
    ["@w/lib", "apps/app/src/index.ts", ["apps/app/src/zz-second.ts"]],
  ]);
});
