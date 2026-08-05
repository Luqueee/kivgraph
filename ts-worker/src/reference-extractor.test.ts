import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import {
  extractLocalReferences,
  type LocalReference,
} from "./reference-extractor.js";
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
  const root = await mkdtemp(path.join(tmpdir(), "luque-references-"));
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

function localReferences(
  references: readonly LocalReference[],
  fileName: string,
): LocalReference[] {
  return references.filter((reference) => reference.fileName === fileName);
}

describe("local TypeScript references", () => {
  it("classifies checker-resolved calls, callbacks, assignments, returns and types", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": `
export type Shape = { value: number };

export function target(value: number): number {
  return value;
}

export function caller(items: number[], shape: Shape): typeof target {
  const alias = target;
  target.apply(null, [1]);
  items.map(target);
  target(items[0]);
  return target;
}

export function returnFunction(): typeof target {
  return target;
}

export const direct = target(1);
target(2);
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);
    const extraction = await extractLocalReferences(service, view, symbols);
    const references = localReferences(
      extraction.references,
      workspace.file("src/index.ts"),
    );

    expect(
      references.map((reference) => `${reference.kind}:${reference.text}`),
    ).toEqual([
      "TYPE_USES:Shape",
      "TYPE_USES:target",
      "ASSIGNS_FUNCTION:target",
      "REFERENCES:target",
      "PASSES_AS_CALLBACK:target",
      "CALLS_DIRECT:target",
      "RETURNS_FUNCTION:target",
      "TYPE_USES:target",
      "RETURNS_FUNCTION:target",
      "CALLS_DIRECT:target",
      "CALLS_DIRECT:target",
    ]);

    const byKind = new Map(
      references.map((reference) => [reference.kind, reference]),
    );
    expect(byKind.get("ASSIGNS_FUNCTION")?.source?.name).toBe("alias");
    expect(byKind.get("PASSES_AS_CALLBACK")?.source?.name).toBe("caller");
    expect(byKind.get("RETURNS_FUNCTION")?.source?.name).toBe("returnFunction");
    expect(
      references.some(
        (reference) =>
          reference.kind === "CALLS_DIRECT" && reference.source === undefined,
      ),
    ).toBe(true);
    expect(
      references.every(
        (reference) =>
          reference.target.name === "target" ||
          reference.target.name === "Shape",
      ),
    ).toBe(true);
  });

  it("limits files and never emits imported external symbols", async () => {
    const workspace = await createWorkspace({
      "src/first.ts": `export function first(): number { return second(); }
export function second(): number { return 2; }
import { external } from "external-lib";
export function externalValue(): number { return external; }
`,
      "src/second.ts": "export function unrelated(): number { return 3; }\n",
      "node_modules/external-lib/package.json": JSON.stringify({
        name: "external-lib",
        types: "index.d.ts",
      }),
      "node_modules/external-lib/index.d.ts":
        "export declare const external: number;\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);
    const extraction = await extractLocalReferences(service, view, symbols, {
      files: ["src/first.ts"],
    });

    expect(
      extraction.references.map((reference) => reference.target.name),
    ).toEqual(["second"]);
    expect(extraction.references.map((reference) => reference.kind)).toEqual([
      "CALLS_DIRECT",
    ]);
  });

  it("follows value and type import aliases to local declarations", async () => {
    const workspace = await createWorkspace({
      "src/definitions.ts": `
export type Shape = { value: number };
export function target(value: number): number {
  return value;
}
`,
      "src/consumer.ts": `
import {
  target as importedTarget,
  type Shape as ImportedShape,
} from "./definitions.js";

export function use(
  items: number[],
  shape: ImportedShape,
): typeof importedTarget {
  importedTarget(1);
  items.map(importedTarget);
  return importedTarget;
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);
    const extraction = await extractLocalReferences(service, view, symbols, {
      files: ["src/consumer.ts"],
    });

    expect(
      extraction.references.map((reference) => [
        reference.kind,
        reference.text,
        reference.target.name,
        path.basename(reference.target.fileName),
      ]),
    ).toEqual([
      ["TYPE_USES", "ImportedShape", "Shape", "definitions.ts"],
      ["TYPE_USES", "importedTarget", "target", "definitions.ts"],
      ["CALLS_DIRECT", "importedTarget", "target", "definitions.ts"],
      ["PASSES_AS_CALLBACK", "importedTarget", "target", "definitions.ts"],
      ["RETURNS_FUNCTION", "importedTarget", "target", "definitions.ts"],
    ]);
  });

  it("recognizes local arrow variables as callable targets", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": `
export const callback = (value: number): number => value;

export function use(items: number[]): typeof callback {
  const alias = callback;
  items.map(callback);
  return callback;
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);
    const extraction = await extractLocalReferences(service, view, symbols);

    expect(
      extraction.references.map((reference) => [
        reference.kind,
        reference.target.name,
      ]),
    ).toEqual([
      ["TYPE_USES", "callback"],
      ["ASSIGNS_FUNCTION", "callback"],
      ["PASSES_AS_CALLBACK", "callback"],
      ["RETURNS_FUNCTION", "callback"],
    ]);
  });

  it("rejects symbols from a stale snapshot", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export function value(): number { return 1; }\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);

    await writeFile(
      workspace.file("src/index.ts"),
      "export function value(): number { return 2; }\n",
      "utf8",
    );
    await service.applyChanges({
      changed: [{ path: workspace.file("src/index.ts") }],
    });

    await expect(
      extractLocalReferences(service, view, symbols),
    ).rejects.toThrowError(
      expect.objectContaining({ code: "STALE_GENERATION" }),
    );
  });
});
