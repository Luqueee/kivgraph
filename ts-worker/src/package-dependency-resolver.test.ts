import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import {
  createPackageDependencies,
  resolvePackageDependencies,
} from "./package-dependency-resolver.js";
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
    path.join(tmpdir(), "ladygraph-package-dependencies-"),
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

describe("package dependency resolution", () => {
  it("aggregates only checker-backed imports between real package identities", async () => {
    const workspace = await createWorkspace({
      "package.json": `{
  "name": "consumer",
  "version": "3.0.0"
}`,
      "src/consumer.ts": `
import { value } from "shared";
import type { Shape } from "shared/types";
export { value as reexported } from "shared";
import { missing } from "missing";
import "./local.js";
console.log(value, Shape, missing);
`,
      "src/local.ts": `export const local = 1;\n`,
      "node_modules/shared/package.json": `{
  "name": "shared",
  "version": "1.2.3",
  "type": "module",
  "types": "./index.d.ts",
  "exports": {
    ".": { "types": "./index.d.ts", "default": "./index.js" },
    "./types": { "types": "./types.d.ts", "default": "./types.js" }
  }
}`,
      "node_modules/shared/index.d.ts": `export declare const value: number;\n`,
      "node_modules/shared/types.d.ts": `export interface Shape { value: string }\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const consumer: PackageProvider = {
      name: "consumer",
      version: "3.0.0",
      repository: "consumer-repo",
      rootPath: workspace.root,
      manifestPath: workspace.file("package.json"),
    };
    const provider: PackageProvider = {
      name: "shared",
      version: "1.2.3",
      repository: "shared-repo",
      rootPath: workspace.file("node_modules/shared"),
      manifestPath: workspace.file("node_modules/shared/package.json"),
    };

    const resolution = await resolvePackageDependencies(
      service,
      view,
      registryFor([provider]),
      consumer,
    );

    expect(resolution.generation).toBe(view.generation);
    expect(resolution.dependencies).toHaveLength(1);
    expect(resolution.dependencies[0]).toMatchObject({
      kind: "PACKAGE_DEPENDS_ON",
      consumer: {
        name: "consumer",
        version: "3.0.0",
        repository: "consumer-repo",
        manifestPath: workspace.file("package.json"),
      },
      provider: {
        name: "shared",
        version: "1.2.3",
        repository: "shared-repo",
        manifestPath: workspace.file("node_modules/shared/package.json"),
      },
    });
    expect(
      resolution.dependencies[0]?.imports.map((entry) => entry.specifier),
    ).toEqual(["shared", "shared/types", "shared"]);
    expect(
      resolution.dependencies[0]?.imports.every(
        (entry) => entry.fileName === workspace.file("src/consumer.ts"),
      ),
    ).toBe(true);
    expect(
      resolution.imports.some(
        (entry) =>
          entry.packageName === "missing" && entry.status !== "RESOLVED",
      ),
    ).toBe(true);
  });

  it("does not emit nominal, malformed, or self-package edges", () => {
    const consumer: PackageProvider = {
      name: "consumer",
      version: "1.0.0",
      repository: "repo",
      rootPath: "/workspace/consumer",
      manifestPath: "/workspace/consumer/package.json",
    };
    const provider: PackageProvider = {
      name: "provider",
      version: "1.0.0",
      repository: "provider-repo",
      rootPath: "/workspace/provider",
      manifestPath: "/workspace/provider/package.json",
    };

    const dependencies = createPackageDependencies(consumer, [
      {
        fileName: "/workspace/consumer/src/index.ts",
        specifier: "provider",
        packageName: "provider",
        status: "RESOLVED",
        provider,
        resolvedFiles: ["/workspace/provider/index.d.ts"],
        requestedExports: [],
        exportMode: "NONE",
        start: 0,
        end: 8,
      },
      {
        fileName: "/workspace/consumer/src/index.ts",
        specifier: "missing",
        packageName: "missing",
        status: "PACKAGE_PROVIDER_NOT_FOUND",
        provider: undefined,
        resolvedFiles: [],
        requestedExports: [],
        exportMode: "NONE",
        start: 9,
        end: 16,
      },
      {
        fileName: "/workspace/consumer/src/index.ts",
        specifier: "provider",
        packageName: "wrong-name",
        status: "RESOLVED",
        provider,
        resolvedFiles: ["/workspace/provider/index.d.ts"],
        requestedExports: [],
        exportMode: "NONE",
        start: 17,
        end: 25,
      },
      {
        fileName: "/workspace/consumer/src/index.ts",
        specifier: "consumer",
        packageName: "consumer",
        status: "RESOLVED",
        provider: consumer,
        resolvedFiles: ["/workspace/consumer/index.d.ts"],
        requestedExports: [],
        exportMode: "NONE",
        start: 26,
        end: 34,
      },
    ]);

    expect(dependencies).toHaveLength(1);
    expect(dependencies[0]?.provider).toEqual(provider);
  });
});
