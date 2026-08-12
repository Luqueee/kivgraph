import { mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { resolveExtends } from "./extends-resolver.js";
import {
  type ImportedSymbol,
  resolveImportedSymbols,
} from "./imported-symbol-resolver.js";
import { LanguageService } from "./language-service.js";
import {
  createPackageProviderRegistry,
  type PackageProvider,
} from "./package-import-resolver.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import { temporaryRoot } from "./temporary-root.js";

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
  const root = await temporaryRoot("ladygraph-extends-");
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

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    workspaces
      .splice(0)
      .map((workspace) => rm(workspace, { recursive: true, force: true })),
  );
});

describe("extends resolution", () => {
  it("resolves a class extends base declared in this project", async () => {
    const workspace = await createWorkspace({
      "package.json": `{ "name": "consumer", "version": "1.0.0" }`,
      "src/base.ts": `export class Base {\n  readonly id = "base";\n}\n`,
      "src/derived.ts": `
import { Base } from "./base.js";
export class Derived extends Base {
  readonly label = "derived";
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);

    const resolution = await resolveExtends(service, view, symbols, []);

    expect(resolution.generation).toBe(view.generation);
    expect(resolution.extends).toHaveLength(1);
    const [edge] = resolution.extends;
    expect(edge?.base.sourceQualifiedName).toBe("Derived");
    expect(edge?.base.text).toBe("Base");
    expect(edge?.targetQualifiedName).toBe("Base");
    expect(edge?.targetFile).toBe(workspace.file("src/base.ts"));
    expect(edge?.packageName).toBeUndefined();
    expect(edge?.identity).toBeUndefined();
    expect(edge?.unresolvedReason).toBeUndefined();
  });

  it("emits one edge per base of an interface extends clause, all within this repository", async () => {
    const workspace = await createWorkspace({
      "package.json": `{ "name": "consumer", "version": "1.0.0" }`,
      "src/shapes.ts": `
export interface Shape {
  readonly value: number;
}
export interface Named {
  readonly name: string;
}
export interface NamedShape extends Shape, Named {
  readonly label: string;
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);

    const resolution = await resolveExtends(service, view, symbols, []);

    const targets = resolution.extends
      .filter((edge) => edge.base.sourceQualifiedName === "NamedShape")
      .map((edge) => edge.targetQualifiedName)
      .sort();
    expect(targets).toEqual(["Named", "Shape"]);
  });

  it("reuses an already-resolved IMPORTS_SYMBOL identity for a cross-repository base, never recomputing it", async () => {
    const workspace = await createWorkspace({
      "package.json": `{ "name": "consumer", "version": "1.0.0" }`,
      "src/consumer.ts": `
import { Widget } from "shared";
export class LabeledWidget extends Widget {
  readonly label = "widget";
}
`,
      "node_modules/shared/package.json": `{
  "name": "shared",
  "version": "1.0.0",
  "type": "module",
  "types": "./index.d.ts"
}`,
      "node_modules/shared/index.d.ts": `export declare class Widget {\n  readonly id: string;\n}\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);

    const sharedProvider: PackageProvider = {
      name: "shared",
      version: "1.0.0",
      repository: "shared-repo",
      rootPath: workspace.file("node_modules/shared"),
      manifestPath: workspace.file("node_modules/shared/package.json"),
    };
    const registry = createPackageProviderRegistry([sharedProvider]);

    const imported = await resolveImportedSymbols(service, view, registry);
    const [firstImported] = imported.symbols;
    expect(firstImported).toBeDefined();
    if (firstImported === undefined) {
      return;
    }
    // The fake provider publishes no declaration map, so the real
    // resolution has no identity yet — exactly the case `resolveExtends`
    // must carry the reason for, unmodified.
    expect(firstImported.target.identity).toBeUndefined();

    const unresolved = await resolveExtends(
      service,
      view,
      symbols,
      imported.symbols,
    );
    expect(unresolved.extends).toHaveLength(1);
    const [unresolvedEdge] = unresolved.extends;
    expect(unresolvedEdge?.packageName).toBe("shared");
    expect(unresolvedEdge?.exportedName).toBe("Widget");
    expect(unresolvedEdge?.identity).toBeUndefined();
    expect(unresolvedEdge?.unresolvedReason).toBe(
      firstImported.target.identityReason,
    );

    // Swap in the identity a declaration map would have supplied, on the
    // very same already-resolved entry: `resolveExtends` only reads
    // `target.identity`, it never derives one of its own.
    const withIdentity: ImportedSymbol = {
      ...firstImported,
      target: {
        ...firstImported.target,
        identity: {
          repository: "shared-repo",
          package: "shared",
          qualifiedName: "Widget",
          kind: "class",
          signature: "export declare class Widget",
          file: "index.ts",
          startLine: 1,
          source: "DECLARATION_MAP",
        },
        identityReason: undefined,
        identityDetail: undefined,
      },
    };
    const resolved = await resolveExtends(service, view, symbols, [
      withIdentity,
    ]);
    expect(resolved.extends).toHaveLength(1);
    const [resolvedEdge] = resolved.extends;
    expect(resolvedEdge?.identity).toEqual(withIdentity.target.identity);
    expect(resolvedEdge?.targetQualifiedName).toBeUndefined();
    expect(resolvedEdge?.unresolvedReason).toBeUndefined();
  });

  it("reports a base that resolves to neither a local declaration nor an import as unresolved", async () => {
    const workspace = await createWorkspace({
      "package.json": `{ "name": "consumer", "version": "1.0.0" }`,
      "src/errors.ts": `
export class CustomError extends Error {
  readonly code = "CUSTOM";
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);

    const resolution = await resolveExtends(service, view, symbols, []);

    expect(resolution.extends).toHaveLength(1);
    const [edge] = resolution.extends;
    expect(edge?.base.text).toBe("Error");
    expect(edge?.targetQualifiedName).toBeUndefined();
    expect(edge?.identity).toBeUndefined();
    expect(edge?.unresolvedReason).toBe("DECLARATION_NOT_RESOLVED");
  });

  it("never produces an edge for an implements clause", async () => {
    const workspace = await createWorkspace({
      "package.json": `{ "name": "consumer", "version": "1.0.0" }`,
      "src/shape.ts": `
export interface Shape {
  readonly value: number;
}
export class Square implements Shape {
  readonly value = 4;
}
`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const symbols = await extractLocalSymbols(service, view);

    const resolution = await resolveExtends(service, view, symbols, []);

    expect(resolution.extends).toHaveLength(0);
  });
});
