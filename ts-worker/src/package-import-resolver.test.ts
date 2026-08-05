import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import {
  resolvePackageImports,
  type PackageProvider,
  type PackageProviderRegistry,
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
  const root = await mkdtemp(path.join(tmpdir(), "luque-package-imports-"));
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

describe("package import resolution", () => {
  it("uses TypeScript module resolution before mapping package providers", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import { value } from "@scope/shared";
import type { Shape } from "@scope/shared/types";
export { value as reexported } from "@scope/shared";
const lazy = import("@scope/shared");
const lazyTypes = import("@scope/shared/types");
import "./local.js";
import "node:fs";
export type Alias = Shape;
console.log(value, lazy, lazyTypes);
`,
      "src/local.ts": `export const local = 1;\n`,
      "node_modules/@scope/shared/package.json": `{
  "name": "@scope/shared",
  "version": "1.2.3",
  "type": "module",
  "types": "./index.d.ts",
  "exports": {
    ".": { "types": "./index.d.ts", "default": "./index.js" },
    "./types": { "types": "./types.d.ts", "default": "./types.js" }
  }
}`,
      "node_modules/@scope/shared/index.js": `export const value = 1;\n`,
      "node_modules/@scope/shared/index.d.ts": `export declare const value: number;\n`,
      "node_modules/@scope/shared/types.js": `export {};\n`,
      "node_modules/@scope/shared/types.d.ts": `export interface Shape { value: string }\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const providerRoot = workspace.file("node_modules/@scope/shared");
    const resolution = await resolvePackageImports(
      service,
      view,
      registryFor([
        {
          name: "@scope/shared",
          version: "1.2.3",
          repository: "shared-library",
          rootPath: providerRoot,
        },
      ]),
    );

    expect(resolution.generation).toBe(view.generation);
    expect(resolution.configFileName).toBe(workspace.configFileName);
    expect(resolution.imports).toHaveLength(5);
    expect(
      resolution.imports.map((entry) => [
        entry.specifier,
        entry.packageName,
        entry.status,
        entry.provider?.repository,
      ]),
    ).toEqual([
      ["@scope/shared", "@scope/shared", "RESOLVED", "shared-library"],
      ["@scope/shared/types", "@scope/shared", "RESOLVED", "shared-library"],
      ["@scope/shared", "@scope/shared", "RESOLVED", "shared-library"],
      ["@scope/shared", "@scope/shared", "RESOLVED", "shared-library"],
      ["@scope/shared/types", "@scope/shared", "RESOLVED", "shared-library"],
    ]);
    expect(
      resolution.imports.some((entry) =>
        entry.resolvedFiles.some((fileName) => fileName.endsWith("index.d.ts")),
      ),
    ).toBe(true);
    expect(
      resolution.imports.some((entry) =>
        entry.resolvedFiles.some((fileName) => fileName.endsWith("types.d.ts")),
      ),
    ).toBe(true);
    expect(
      resolution.imports.every(
        (entry) => entry.fileName === workspace.file("src/consumer.ts"),
      ),
    ).toBe(true);
  });

  it("classifies missing providers and unresolved modules without nominal edges", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import { registered } from "registered";
import { absent } from "absent";
import "./local.js";
`,
      "src/local.ts": `export const local = 1;\n`,
      "node_modules/registered/package.json": `{"name":"registered","version":"1.0.0"}`,
      "node_modules/registered/index.d.ts": `export declare const registered: number;\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const resolution = await resolvePackageImports(
      service,
      view,
      registryFor([
        {
          name: "registered",
          version: "1.0.0",
          repository: "registered-repo",
          rootPath: workspace.file("node_modules/registered"),
        },
        {
          name: "absent",
          version: "2.0.0",
          repository: "absent-repo",
          rootPath: workspace.file("provider/absent"),
        },
      ]),
    );

    expect(resolution.imports).toHaveLength(2);
    const byPackage = new Map(
      resolution.imports.map((entry) => [entry.packageName, entry]),
    );
    expect(byPackage.get("absent")).toMatchObject({
      packageName: "absent",
      specifier: "absent",
      status: "MODULE_NOT_RESOLVED",
      provider: { repository: "absent-repo" },
      resolvedFiles: [],
    });
    expect(byPackage.get("registered")).toMatchObject({
      packageName: "registered",
      specifier: "registered",
      status: "RESOLVED",
      provider: { repository: "registered-repo" },
    });
  });

  it("reports a resolved module with no registered provider", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `import { value } from "unregistered";\nconsole.log(value);\n`,
      "node_modules/unregistered/package.json": `{"name":"unregistered","version":"1.0.0"}`,
      "node_modules/unregistered/index.d.ts": `export declare const value: number;\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const resolution = await resolvePackageImports(
      service,
      view,
      registryFor([]),
    );

    expect(resolution.imports).toHaveLength(1);
    expect(resolution.imports[0]).toMatchObject({
      packageName: "unregistered",
      status: "PACKAGE_PROVIDER_NOT_FOUND",
      provider: undefined,
    });
    expect(resolution.imports[0]?.resolvedFiles.length).toBeGreaterThan(0);
  });
});
