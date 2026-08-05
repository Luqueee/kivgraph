import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import { extractLocalReferences } from "./reference-extractor.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import type { LocalReference } from "./reference-extractor.js";
import type { LocalSymbol } from "./symbol-extractor.js";

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
  const root = await mkdtemp(path.join(tmpdir(), "luque-local-suite-"));
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

function symbolsInFile(
  symbols: readonly LocalSymbol[],
  fileName: string,
): LocalSymbol[] {
  return symbols.filter((symbol) => symbol.fileName === fileName);
}

function referencesInFile(
  references: readonly LocalReference[],
  fileName: string,
): LocalReference[] {
  return references.filter((reference) => reference.fileName === fileName);
}

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    workspaces
      .splice(0)
      .map((workspace) => rm(workspace, { recursive: true, force: true })),
  );
});

describe("local TypeScript suite", () => {
  it("covers homonyms, shadowing, overloads, methods, generics, barrels, callbacks and aliases", async () => {
    const workspace = await createWorkspace({
      "src/model.ts": `
export const shared = "model";

export function duplicate(): string {
  return shared;
}

export function overloaded(value: string): string;
export function overloaded(value: number): number;
export function overloaded(value: string | number): string | number {
  return value;
}

export class Box<T> {
  method(value: T): T {
    return value;
  }
}

export function identity<T>(value: T): T {
  return value;
}

export const callback = (value: number): number => value;
export type Payload<T> = { value: T };
`,
      "src/other.ts": `
export const shared = "other";
export function duplicate(): string {
  return shared;
}
`,
      "src/barrel.ts": `
export * from "./model.js";
export { duplicate as otherDuplicate } from "./other.js";
`,
      "src/consumer.ts": `
import {
  Box,
  callback as callbackAlias,
  duplicate as modelDuplicate,
  identity as aliasIdentity,
  overloaded,
  type Payload as AliasPayload,
} from "./barrel.js";
import { duplicate as otherDuplicate } from "./other.js";

export function useGeneric<T extends AliasPayload<string>>(
  values: number[],
  payload: AliasPayload<string>,
): string {
  const shadow = "function";
  const before = shadow;
  {
    const shadow = "block";
    const inside = shadow;
    const box: Box<string> = new Box<string>();
    const methodValue = box.method(payload.value);
    return (
      aliasIdentity(payload.value) +
      String(methodValue) +
      String(values.map(callbackAlias)[0]) +
      modelDuplicate() +
      otherDuplicate() +
      overloaded(1) +
      overloaded("x") +
      before +
      inside
    );
  }
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
    const consumerSymbols = symbolsInFile(
      symbols.symbols,
      workspace.file("src/consumer.ts"),
    );
    const references = referencesInFile(
      extraction.references,
      workspace.file("src/consumer.ts"),
    );

    const overloadedSymbols = symbols.symbols.filter(
      (symbol) => symbol.name === "overloaded",
    );
    expect(overloadedSymbols).toHaveLength(3);
    expect(
      new Set(overloadedSymbols.map((symbol) => symbol.symbolId)),
    ).toHaveLength(1);

    const directCall = (text: string): LocalReference => {
      const reference = references.find(
        (candidate) =>
          candidate.text === text && candidate.kind === "CALLS_DIRECT",
      );
      expect(reference).toBeDefined();
      return reference as LocalReference;
    };

    expect(directCall("aliasIdentity").target.name).toBe("identity");
    expect(path.basename(directCall("aliasIdentity").target.fileName)).toBe(
      "model.ts",
    );
    expect(
      references.some(
        (reference) =>
          reference.text === "callbackAlias" &&
          reference.kind === "PASSES_AS_CALLBACK" &&
          reference.target.name === "callback" &&
          path.basename(reference.target.fileName) === "model.ts",
      ),
    ).toBe(true);
    expect(
      references.some(
        (reference) =>
          reference.text === "AliasPayload" &&
          reference.kind === "TYPE_USES" &&
          reference.target.name === "Payload" &&
          path.basename(reference.target.fileName) === "model.ts",
      ),
    ).toBe(true);
    expect(
      references.some(
        (reference) =>
          reference.text === "Box" &&
          reference.kind === "TYPE_USES" &&
          reference.target.name === "Box" &&
          path.basename(reference.target.fileName) === "model.ts",
      ),
    ).toBe(true);
    expect(directCall("method").target.qualifiedName).toBe("Box.method");

    const duplicateTargets = references
      .filter(
        (reference) =>
          reference.kind === "CALLS_DIRECT" &&
          ["modelDuplicate", "otherDuplicate"].includes(reference.text),
      )
      .map((reference) => [
        reference.text,
        path.basename(reference.target.fileName),
        reference.target.name,
      ]);
    expect(duplicateTargets).toEqual([
      ["modelDuplicate", "model.ts", "duplicate"],
      ["otherDuplicate", "other.ts", "duplicate"],
    ]);

    expect(
      references.filter(
        (reference) =>
          reference.text === "overloaded" && reference.kind === "CALLS_DIRECT",
      ),
    ).toHaveLength(2);

    const shadowSymbols = consumerSymbols.filter(
      (symbol) => symbol.name === "shadow",
    );
    const shadowReferences = references.filter(
      (reference) => reference.text === "shadow",
    );
    expect(shadowSymbols).toHaveLength(2);
    expect(shadowReferences).toHaveLength(2);
    expect(
      new Set(shadowReferences.map((reference) => reference.target.symbolId)),
    ).toHaveLength(2);
    expect(
      shadowReferences.map((reference) => reference.target.start).sort(),
    ).toEqual(shadowSymbols.map((symbol) => symbol.start).sort());
  });

  it("keeps resolvable facts when the project contains broken code", async () => {
    const workspace = await createWorkspace({
      "src/model.ts": `
export function identity<T>(value: T): T {
  return value;
}
`,
      "src/barrel.ts": `export { identity } from "./model.js";\n`,
      "src/broken.ts": `
import { identity as aliasIdentity } from "./barrel.js";

export function known(): number {
  return aliasIdentity(1);
}

export const broken: number = "not a number";

export function unresolved(): number {
  return missing();
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const diagnostics = await view.program.getSemanticDiagnostics();
    const symbols = await extractLocalSymbols(service, view);
    const extraction = await extractLocalReferences(service, view, symbols, {
      files: ["src/broken.ts"],
    });

    expect(diagnostics.length).toBeGreaterThan(0);
    expect(
      symbols.symbols.some(
        (symbol) =>
          symbol.fileName === workspace.file("src/broken.ts") &&
          symbol.name === "known",
      ),
    ).toBe(true);
    expect(
      extraction.references.some(
        (reference) =>
          reference.text === "aliasIdentity" &&
          reference.kind === "CALLS_DIRECT" &&
          reference.target.name === "identity" &&
          path.basename(reference.target.fileName) === "model.ts",
      ),
    ).toBe(true);
    expect(
      extraction.references.some((reference) => reference.text === "missing"),
    ).toBe(false);
  });
});
