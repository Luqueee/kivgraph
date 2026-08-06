import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import { extractLocalSymbols } from "./symbol-extractor.js";

const services: LanguageService[] = [];
const workspaces: string[] = [];

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    workspaces
      .splice(0)
      .map((workspace) => rm(workspace, { recursive: true, force: true })),
  );
});

interface Workspace {
  root: string;
  configFileName: string;
  file(relative: string): string;
}

async function createWorkspace(
  files: Record<string, string>,
): Promise<Workspace> {
  const root = await mkdtemp(path.join(tmpdir(), "luque-symbols-"));
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
        strict: true,
        target: "ES2022",
        module: "nodenext",
        moduleResolution: "nodenext",
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

describe("local TypeScript symbols", () => {
  it("extracts declarations, nested scopes and local exports", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": `
export function greet(name: string): string {
  return name;
}

export class Box {
  private value = 1;
  get current(): number {
    return this.value;
  }
  set current(value: number) {
    this.value = value;
  }
  method(): number {
    return this.value;
  }
}

export interface Shape { area(): number; }
export type Alias = Shape;
export enum Color { Red, Blue }
export namespace Nested {
  export const value = 1;
}

const hidden = 1;
export { hidden as publicHidden };
const defaultValue = 2;
export default defaultValue;
`,
      "src/other.ts": "export const other = true;\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);

    const extraction = await extractLocalSymbols(service, view);
    const names = extraction.symbols.map((symbol) => symbol.name);
    expect(names).toEqual([
      "greet",
      "Box",
      "value",
      "current",
      "current",
      "method",
      "Shape",
      "Alias",
      "Color",
      "Nested",
      "value",
      "hidden",
      "defaultValue",
      "other",
    ]);

    expect(extraction.symbols.map((symbol) => symbol.kind)).toEqual([
      "function",
      "class",
      "property",
      "method",
      "method",
      "method",
      "interface",
      "type",
      "enum",
      "namespace",
      "variable",
      "variable",
      "variable",
      "variable",
    ]);

    const byName = new Map(
      extraction.symbols.map((symbol) => [symbol.qualifiedName, symbol]),
    );
    expect(byName.get("Box.method")?.signature).toContain("method()");
    expect(byName.get("Nested.value")?.exportedNames).toEqual(["value"]);
    expect(byName.get("hidden")?.exportedNames).toEqual(["publicHidden"]);
    expect(byName.get("defaultValue")?.exportedNames).toEqual(["default"]);

    expect(
      extraction.exports
        .map((entry) => [entry.exportedName, entry.localName])
        .sort(([leftName, leftLocal], [rightName, rightLocal]) =>
          `${leftName}:${leftLocal}`.localeCompare(
            `${rightName}:${rightLocal}`,
          ),
        ),
    ).toEqual(
      [
        ["Alias", "Alias"],
        ["Box", "Box"],
        ["Color", "Color"],
        ["Nested", "Nested"],
        ["Shape", "Shape"],
        ["default", "defaultValue"],
        ["greet", "greet"],
        ["other", "other"],
        ["publicHidden", "hidden"],
        ["value", "value"],
      ].sort(([leftName, leftLocal], [rightName, rightLocal]) =>
        `${leftName}:${leftLocal}`.localeCompare(`${rightName}:${rightLocal}`),
      ),
    );
  });

  it("resolves named, default, alias, export-from, star and barrel exports", async () => {
    const workspace = await createWorkspace({
      "src/definitions.ts": `
export const named = 1;
export function callable(): number {
  return named;
}
export type Shape = { value: number };
export default function defaultFn(): number {
  return named;
}
`,
      "src/aliases.ts": `
export { named as renamed, default as aliasedDefault } from "./definitions.js";
export type { Shape as PublicShape } from "./definitions.js";
export * from "./definitions.js";
`,
      "src/barrel.ts": `export * from "./aliases.js";\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const extraction = await extractLocalSymbols(
      service,
      service.project(workspace.configFileName),
    );

    const exportsByFile = new Map<string, string[]>();
    for (const entry of extraction.exports) {
      const key = path.basename(entry.fileName);
      const entries = exportsByFile.get(key) ?? [];
      entries.push(
        `${entry.exportedName}:${entry.localName}:${entry.symbolId}:${entry.isTypeOnly ? "type" : "value"}`,
      );
      exportsByFile.set(key, entries);
    }
    for (const entries of exportsByFile.values()) {
      entries.sort();
    }

    expect(exportsByFile.get("definitions.ts")).toEqual([
      expect.stringMatching(/^Shape:Shape:\d+:type$/u),
      expect.stringMatching(/^callable:callable:\d+:value$/u),
      expect.stringMatching(/^default:defaultFn:\d+:value$/u),
      expect.stringMatching(/^named:named:\d+:value$/u),
    ]);
    expect(exportsByFile.get("aliases.ts")).toEqual([
      expect.stringMatching(/^PublicShape:Shape:\d+:type$/u),
      expect.stringMatching(/^Shape:Shape:\d+:type$/u),
      expect.stringMatching(/^aliasedDefault:default:\d+:value$/u),
      expect.stringMatching(/^callable:callable:\d+:value$/u),
      expect.stringMatching(/^named:named:\d+:value$/u),
      expect.stringMatching(/^renamed:named:\d+:value$/u),
    ]);
    expect(exportsByFile.get("barrel.ts")).toEqual([
      expect.stringMatching(/^PublicShape:Shape:\d+:type$/u),
      expect.stringMatching(/^Shape:Shape:\d+:type$/u),
      expect.stringMatching(/^aliasedDefault:defaultFn:\d+:value$/u),
      expect.stringMatching(/^callable:callable:\d+:value$/u),
      expect.stringMatching(/^named:named:\d+:value$/u),
      expect.stringMatching(/^renamed:named:\d+:value$/u),
    ]);
  });

  it("limits extraction to project-local files and requested files", async () => {
    const workspace = await createWorkspace({
      "src/first.ts": "export const first = 1;\n",
      "src/second.ts": "export const second = 2;\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);

    const extraction = await extractLocalSymbols(service, view, {
      files: ["src/second.ts"],
    });
    expect(extraction.symbols.map((symbol) => symbol.name)).toEqual(["second"]);
    expect(
      extraction.symbols.every((symbol) =>
        symbol.fileName.startsWith(workspace.root),
      ),
    ).toBe(true);
  });

  it("rejects a view that became stale during a prior update", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const value = 1;\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);

    await writeFile(
      workspace.file("src/index.ts"),
      "export const value = 2;\n",
      "utf8",
    );
    await service.applyChanges({
      changed: [{ path: workspace.file("src/index.ts") }],
    });

    await expect(extractLocalSymbols(service, view)).rejects.toThrowError(
      expect.objectContaining({ code: "STALE_GENERATION" }),
    );
  });
  it("emits every destructured binding and skips external libraries", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": `import { external } from "external-lib";
export const { first, second: renamed } = {
  first: external,
  second: 2,
};
`,
      "node_modules/external-lib/package.json": JSON.stringify({
        name: "external-lib",
        types: "index.d.ts",
      }),
      "node_modules/external-lib/index.d.ts":
        "export declare const external: number;\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);

    const extraction = await extractLocalSymbols(
      service,
      service.project(workspace.configFileName),
    );
    expect(extraction.symbols.map((symbol) => symbol.name)).toEqual([
      "first",
      "renamed",
    ]);
    expect(extraction.symbols.map((symbol) => symbol.exportedNames)).toEqual([
      ["first"],
      ["renamed"],
    ]);
  });

  it("preserves overloaded declaration sites sharing one checker symbol", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": `export function overloaded(value: string): string;
export function overloaded(value: number): number;
export function overloaded(value: string | number): string | number {
  return value;
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);

    const extraction = await extractLocalSymbols(
      service,
      service.project(workspace.configFileName),
    );
    expect(extraction.symbols.map((symbol) => symbol.name)).toEqual([
      "overloaded",
      "overloaded",
      "overloaded",
    ]);
    expect(
      new Set(extraction.symbols.map((symbol) => symbol.symbolId)).size,
    ).toBe(1);
    expect(extraction.exports).toHaveLength(3);
  });

  it("marks re-exports and threads the exposed declaration through", async () => {
    const workspace = await createWorkspace({
      "src/lib.ts": `export function helper(): number {
  return 1;
}
`,
      "src/index.ts": `export { helper as aliasedHelper } from "./lib.js";
export * from "./lib.js";
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const extraction = await extractLocalSymbols(
      service,
      service.project(workspace.configFileName),
    );

    const byExportSite = new Map(
      extraction.exports.map((entry) => [
        `${path.basename(entry.fileName)}#${entry.exportedName}`,
        entry,
      ]),
    );

    const direct = byExportSite.get("lib.ts#helper");
    expect(direct?.reExport).toBe(false);
    expect(direct?.targetQualifiedName).toBe("helper");
    expect(direct?.targetFile).toBe(workspace.file("src/lib.ts"));

    const aliased = byExportSite.get("index.ts#aliasedHelper");
    expect(aliased?.reExport).toBe(true);
    expect(aliased?.targetQualifiedName).toBe("helper");
    expect(aliased?.targetFile).toBe(workspace.file("src/lib.ts"));

    const starred = byExportSite.get("index.ts#helper");
    expect(starred?.reExport).toBe(true);
    expect(starred?.targetQualifiedName).toBe("helper");
    expect(starred?.targetFile).toBe(workspace.file("src/lib.ts"));

    // The star and named re-exports share the module specifier's whole
    // statement as their evidence text, and both are anchored on the
    // `export ...` statement rather than the exposed declaration's body.
    expect(aliased?.text).toBe("helper as aliasedHelper");
    expect(starred?.text).toBe('export * from "./lib.js";');
  });
});
