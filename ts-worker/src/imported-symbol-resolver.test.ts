import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { resolveImportedSymbols } from "./imported-symbol-resolver.js";
import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";

const services: LanguageService[] = [];
const workspaces: string[] = [];

interface Workspace {
  root: string;
  configFileName: string;
  file(relative: string): string;
}

async function createWorkspace(
  files: Record<string, string>,
): Promise<Workspace> {
  const root = await mkdtemp(
    path.join(tmpdir(), "ladygraph-imported-symbols-"),
  );
  workspaces.push(root);
  const workspace: Workspace = {
    root,
    configFileName: path.join(root, "tsconfig.json"),
    file: (relative) => path.join(root, relative),
  };
  await writeFile(
    workspace.configFileName,
    JSON.stringify({
      compilerOptions: {
        module: "nodenext",
        moduleResolution: "nodenext",
        strict: true,
        target: "ES2022",
      },
      include: ["src/**/*.ts"],
    }),
    "utf8",
  );
  for (const [relative, contents] of Object.entries(files)) {
    const target = workspace.file(relative);
    await mkdir(path.dirname(target), { recursive: true });
    await writeFile(target, contents, "utf8");
  }
  return workspace;
}

function openService(workspace: Workspace): LanguageService {
  const service = LanguageService.create({ cwd: workspace.root });
  services.push(service);
  return service;
}

function registryFor(
  providers: readonly PackageProvider[],
): PackageProviderRegistry {
  const byName = new Map(
    providers.map((provider) => [provider.name, provider]),
  );
  return { get: (name) => byName.get(name) };
}

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    workspaces
      .splice(0)
      .map((workspace) => rm(workspace, { recursive: true, force: true })),
  );
});

describe("imported symbol resolution", () => {
  it("links consumer bindings to the declaration the checker resolves", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import defaultValue, { value, type Shape, missing } from "shared";
import { value as aliased } from "shared";
export { value as reexported } from "shared";
import * as namespace from "shared";
import { local } from "./local.js";
console.log(defaultValue, value, aliased, missing, namespace, local);
console.log(namespace.value);
export type Used = Shape;
`,
      "src/local.ts": `export const local = 1;\n`,
      "node_modules/shared/package.json": `{
  "name": "shared",
  "version": "1.0.0",
  "type": "module",
  "types": "./index.d.ts"
}`,
      "node_modules/shared/index.d.ts": `declare const defaultValue: string;
export default defaultValue;
export declare const value: number;
export interface Shape { value: string }
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const providerRoot = workspace.file("node_modules/shared");
    const resolution = await resolveImportedSymbols(
      service,
      view,
      registryFor([
        {
          name: "shared",
          version: "1.0.0",
          repository: "shared-repo",
          rootPath: providerRoot,
          manifestPath: path.join(providerRoot, "package.json"),
        },
      ]),
    );

    expect(resolution.generation).toBe(view.generation);
    // The bare namespace binding (passed to `console.log` above) names no
    // concrete symbol and produces nothing; `namespace.value` does, exactly
    // like the named import of `value` a few lines above it.
    expect(
      resolution.symbols.map((entry) => [
        entry.consumer.name,
        entry.exportedName,
        entry.target.name,
      ]),
    ).toEqual([
      ["defaultValue", "default", "defaultValue"],
      ["value", "value", "value"],
      ["Shape", "Shape", "Shape"],
      ["aliased", "value", "value"],
      ["value", "value", "value"],
    ]);
    expect(
      resolution.symbols.every(
        (entry) =>
          entry.kind === "IMPORTS_SYMBOL" &&
          entry.packageName === "shared" &&
          entry.provider.repository === "shared-repo" &&
          entry.consumer.fileName === workspace.file("src/consumer.ts"),
      ),
    ).toBe(true);

    // `export { value as reexported } from "shared"` is a re-export, not an
    // import: it never appears in `.symbols`.
    expect(
      resolution.reexports.map((entry) => [
        entry.export.name,
        entry.exportedName,
        entry.target.name,
      ]),
    ).toEqual([["reexported", "value", "value"]]);
    expect(resolution.reexports[0]?.kind).toBe("REEXPORTS");
    expect(resolution.reexports[0]?.provider.repository).toBe("shared-repo");

    const value = resolution.symbols.find(
      (entry) => entry.consumer.name === "value",
    );
    expect(value?.target.declarations).toHaveLength(1);
    const declaration = value?.target.declarations[0];
    expect(declaration?.fileName).toBe(path.join(providerRoot, "index.d.ts"));
    expect(declaration?.startLine).toBe(3);
    expect(declaration?.endLine).toBe(3);
    expect(declaration?.sourceStatus).toBe("UNRESOLVED");
    expect(declaration?.start).toBeLessThan(declaration?.end ?? 0);
    expect(declaration?.sourcePosition).toBeUndefined();

    // The two "value" entries are genuinely distinct occurrences: one at the
    // named import, one at the namespace member access.
    const namespaceMember = resolution.symbols.filter(
      (entry) => entry.consumer.name === "value",
    )[1];
    expect(namespaceMember?.consumer.text).toBe("namespace.value");
    expect(namespaceMember?.consumer.start).not.toBe(value?.consumer.start);
  });

  it("maps declarations to sources when the provider ships a declaration map", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `import { value } from "mapped";\nconsole.log(value);\n`,
      "node_modules/mapped/package.json": `{
  "name": "mapped",
  "version": "2.0.0",
  "type": "module",
  "types": "./dist/index.d.ts"
}`,
      "node_modules/mapped/src/index.ts": `export const value = 1;\n`,
      "node_modules/mapped/dist/index.d.ts": `export declare const value: number;\n//# sourceMappingURL=index.d.ts.map\n`,
      "node_modules/mapped/dist/index.d.ts.map": JSON.stringify({
        version: 3,
        file: "index.d.ts",
        sourceRoot: "",
        sources: ["../src/index.ts"],
        names: [],
        mappings: "",
      }),
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const providerRoot = workspace.file("node_modules/mapped");
    const resolution = await resolveImportedSymbols(
      service,
      view,
      registryFor([
        {
          name: "mapped",
          version: "2.0.0",
          repository: "mapped-repo",
          rootPath: providerRoot,
        },
      ]),
    );

    expect(resolution.symbols).toHaveLength(1);
    expect(resolution.symbols[0]?.target.declarations[0]).toMatchObject({
      fileName: path.join(providerRoot, "dist/index.d.ts"),
      sourceStatus: "DECLARATION_MAP",
      sourceFiles: [path.join(providerRoot, "src/index.ts")],
    });
    // A map with no segments cannot place the symbol: the file bridge holds,
    // the exact position does not, and nothing is guessed.
    expect(
      resolution.symbols[0]?.target.declarations[0]?.sourcePosition,
    ).toBeUndefined();
  });

  it("emits no edge for homonyms, unresolved modules, or unregistered providers", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import { value } from "unregistered";
import { value as absent } from "absent";
export const value2 = 1;
console.log(value, absent);
`,
      "src/homonym.ts": `export const value = "local homonym";\n`,
      "node_modules/unregistered/package.json": `{"name":"unregistered","version":"1.0.0"}`,
      "node_modules/unregistered/index.d.ts": `export declare const value: number;\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const resolution = await resolveImportedSymbols(
      service,
      view,
      registryFor([
        {
          name: "absent",
          version: "1.0.0",
          repository: "absent-repo",
          rootPath: workspace.file("provider/absent"),
        },
      ]),
    );

    expect(resolution.symbols).toEqual([]);
    expect(
      resolution.imports.map((entry) => [entry.packageName, entry.status]),
    ).toEqual([
      ["unregistered", "PACKAGE_PROVIDER_NOT_FOUND"],
      ["absent", "MODULE_NOT_RESOLVED"],
    ]);
  });
});
