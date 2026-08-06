import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import {
  resolveDeclarationSources,
  type DeclarationSourceMapping,
} from "./declaration-source-resolver.js";
import type {
  PackageImport,
  PackageProvider,
} from "./package-import-resolver.js";
import type { ProviderExportResolution } from "./provider-export-resolver.js";

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
    path.join(tmpdir(), "ladygraph-declaration-sources-"),
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

function provider(
  workspace: Workspace,
  name: string,
  fields: Partial<PackageProvider> = {},
): PackageProvider {
  return {
    name,
    version: "1.0.0",
    repository: `${name}-repo`,
    rootPath: workspace.file(name),
    ...fields,
  };
}

function packageImport(
  workspace: Workspace,
  providerValue: PackageProvider,
  declarationFile: string,
): PackageImport {
  return {
    fileName: workspace.file("src/consumer.ts"),
    specifier: providerValue.name,
    packageName: providerValue.name,
    status: "RESOLVED",
    provider: providerValue,
    resolvedFiles: [declarationFile],
    requestedExports: ["value"],
    exportMode: "NAMED",
    start: 0,
    end: providerValue.name.length + 2,
  };
}

function resolution(
  configFileName: string,
  generation: number,
  imports: readonly PackageImport[],
): ProviderExportResolution {
  return {
    configFileName,
    generation,
    imports,
    exports: [],
  };
}

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    workspaces
      .splice(0)
      .map((workspace) => rm(workspace, { recursive: true, force: true })),
  );
});

describe("declaration source resolution", () => {
  it("applies declaration maps before project, registry, and root mappings", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": "export {}\n",
      "map/src/index.ts": "export const value = 1;\n",
      "map/dist/index.d.ts": "export declare const value: number;\n",
      "map/dist/index.d.ts.map": JSON.stringify({
        version: 3,
        file: "index.d.ts",
        sources: ["../src/index.ts"],
        names: [],
        mappings: "",
      }),
      "project/src/index.ts": "export const value = 2;\n",
      "project/dist/index.d.ts": "export declare const value: number;\n",
      "project/tsconfig.json": `{
  // project output paths
  "compilerOptions": { "rootDir": "src", "outDir": "dist", },
}`,
      "registry/src/index.ts": "export const value = 3;\n",
      "registry/types/index.d.ts": "export declare const value: number;\n",
      "rootout/src/index.ts": "export const value = 4;\n",
      "rootout/dist/index.d.ts": "export declare const value: number;\n",
      "unresolved/dist/index.d.ts": "export declare const value: number;\n",
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);

    const mapProvider = provider(workspace, "map", {
      projectPath: workspace.file("map/tsconfig.json"),
      sourceRoots: [workspace.file("map/src")],
      declarationRoots: [workspace.file("map/dist")],
    });
    const projectProvider = provider(workspace, "project", {
      projectPath: workspace.file("project/tsconfig.json"),
      sourceRoots: [workspace.file("project/src")],
      declarationRoots: [workspace.file("project/dist")],
    });
    const registryProvider = provider(workspace, "registry", {
      sourceRoots: ["src"],
      declarationRoots: ["types"],
    });
    const rootOutProvider = provider(workspace, "rootout", {
      rootDir: "src",
      outDir: "dist",
    });
    const unresolvedProvider = provider(workspace, "unresolved", {
      rootDir: "src",
      outDir: "dist",
    });

    const result = await resolveDeclarationSources(
      service,
      view,
      resolution(view.configFileName, view.generation, [
        packageImport(
          workspace,
          mapProvider,
          workspace.file("map/dist/index.d.ts"),
        ),
        packageImport(
          workspace,
          projectProvider,
          workspace.file("project/dist/index.d.ts"),
        ),
        packageImport(
          workspace,
          registryProvider,
          workspace.file("registry/types/index.d.ts"),
        ),
        packageImport(
          workspace,
          rootOutProvider,
          workspace.file("rootout/dist/index.d.ts"),
        ),
        packageImport(
          workspace,
          unresolvedProvider,
          workspace.file("unresolved/dist/index.d.ts"),
        ),
      ]),
    );

    expect(result).toMatchObject({
      generation: view.generation,
      configFileName: workspace.configFileName,
    });
    expect(result.mappings).toEqual([
      mapping(
        workspace.file("map/dist/index.d.ts"),
        [workspace.file("map/src/index.ts")],
        "DECLARATION_MAP",
      ),
      mapping(
        workspace.file("project/dist/index.d.ts"),
        [workspace.file("project/src/index.ts")],
        "PROJECT_REFERENCE",
      ),
      mapping(
        workspace.file("registry/types/index.d.ts"),
        [workspace.file("registry/src/index.ts")],
        "PROVIDER_REGISTRY",
      ),
      mapping(
        workspace.file("rootout/dist/index.d.ts"),
        [workspace.file("rootout/src/index.ts")],
        "ROOT_DIR_OUT_DIR",
      ),
      mapping(workspace.file("unresolved/dist/index.d.ts"), [], "UNRESOLVED"),
    ]);
  });

  it("does not fabricate a source when a declaration map points nowhere", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": "export {}\n",
      "broken/index.d.ts": "export declare const value: number;\n",
      "broken/index.d.ts.map": JSON.stringify({
        version: 3,
        file: "index.d.ts",
        sources: ["../missing/index.ts"],
        names: [],
        mappings: "",
      }),
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const brokenProvider = provider(workspace, "broken", {
      sourceRoots: [workspace.file("broken/src")],
      declarationRoots: [workspace.file("broken")],
    });

    const result = await resolveDeclarationSources(
      service,
      view,
      resolution(view.configFileName, view.generation, [
        packageImport(
          workspace,
          brokenProvider,
          workspace.file("broken/index.d.ts"),
        ),
      ]),
    );

    expect(result.mappings).toEqual([
      mapping(workspace.file("broken/index.d.ts"), [], "UNRESOLVED"),
    ]);
  });
});

function mapping(
  declarationFile: string,
  sourceFiles: readonly string[],
  status: DeclarationSourceMapping["status"],
): DeclarationSourceMapping {
  return { declarationFile, sourceFiles, status };
}
