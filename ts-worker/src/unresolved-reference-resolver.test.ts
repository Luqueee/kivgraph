import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";

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
  const root = await mkdtemp(path.join(tmpdir(), "ladygraph-unresolved-"));
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

describe("unresolved TypeScript references", () => {
  it("classifies missing providers, unresolved modules, exports, and declarations", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import { value } from "unregistered";
import { other } from "absent";
import { missing, mapped } from "provider";
console.log(value, other, missing, mapped);
`,
      "node_modules/unregistered/package.json": `{"name":"unregistered","version":"1.0.0"}`,
      "node_modules/unregistered/index.d.ts": `export declare const value: number;\n`,
      "node_modules/provider/package.json": `{
  "name": "provider",
  "version": "1.0.0",
  "type": "module",
  "types": "./index.d.ts"
}`,
      "node_modules/provider/index.d.ts": `export declare const mapped: number;\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const providerRoot = workspace.file("node_modules/provider");
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registryFor([
        {
          name: "provider",
          version: "1.0.0",
          repository: "provider-repo",
          rootPath: providerRoot,
        },
        {
          name: "absent",
          version: "1.0.0",
          repository: "absent-repo",
          rootPath: workspace.file("provider/absent"),
        },
      ]),
    );

    expect(
      resolution.unresolved.map((entry) => [
        entry.packageName,
        entry.reason,
        entry.requestedSymbol,
      ]),
    ).toEqual([
      ["unregistered", "PACKAGE_PROVIDER_NOT_FOUND", undefined],
      ["absent", "MODULE_NOT_RESOLVED", undefined],
      ["provider", "DECLARATION_SOURCE_NOT_MAPPED", "mapped"],
      ["provider", "EXPORT_NOT_FOUND", "missing"],
    ]);
    expect(
      resolution.unresolved.find(
        (entry) => entry.reason === "DECLARATION_SOURCE_NOT_MAPPED",
      )?.detail,
    ).toBe(path.join(providerRoot, "index.d.ts"));
    expect(resolution.symbols.map((entry) => entry.exportedName)).toEqual([
      "mapped",
    ]);
  });

  it("reports registry conflicts instead of exact edges", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import { ambiguous } from "duplicated";
import { drifted } from "drifting";
console.log(ambiguous, drifted);
`,
      "node_modules/duplicated/package.json": `{
  "name": "duplicated",
  "version": "1.0.0",
  "type": "module",
  "types": "./index.d.ts"
}`,
      "node_modules/duplicated/index.d.ts": `export declare const ambiguous: number;\n`,
      "node_modules/drifting/package.json": `{
  "name": "drifting",
  "version": "2.0.0",
  "type": "module",
  "types": "./index.d.ts"
}`,
      "node_modules/drifting/index.d.ts": `export declare const drifted: number;\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registryFor([
        {
          name: "duplicated",
          version: "1.0.0",
          repository: "first-repo",
          rootPath: workspace.file("node_modules/duplicated"),
        },
        {
          name: "drifting",
          version: "2.0.0",
          repository: "drift-repo",
          rootPath: workspace.file("node_modules/drifting"),
        },
      ]),
      {
        conflicts: [
          {
            packageName: "duplicated",
            kind: "AMBIGUOUS_PACKAGE_PROVIDER",
            repositories: ["second-repo", "first-repo"],
          },
          {
            packageName: "drifting",
            kind: "PACKAGE_VERSION_MISMATCH",
            versions: ["2.0.0", "1.0.0"],
          },
        ],
      },
    );

    expect(
      resolution.unresolved.map((entry) => [
        entry.packageName,
        entry.reason,
        entry.detail,
      ]),
    ).toEqual([
      [
        "duplicated",
        "AMBIGUOUS_PACKAGE_PROVIDER",
        "repositories: first-repo, second-repo",
      ],
      ["drifting", "VERSION_MISMATCH", "versions: 1.0.0, 2.0.0"],
    ]);
  });

  it("reports a broken provider module that yields no exact edge", async () => {
    const workspace = await createWorkspace({
      "src/consumer.ts": `
import { named } from "broken";
console.log(named);
`,
      "node_modules/broken/package.json": `{
  "name": "broken",
  "version": "1.0.0",
  "type": "module",
  "types": "./index.d.ts"
}`,
      "node_modules/broken/index.d.ts": `export * from "./missing.js";\n`,
    });
    const service = openService(workspace);
    await service.openProject(workspace.configFileName);
    const view = service.project(workspace.configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registryFor([
        {
          name: "broken",
          version: "1.0.0",
          repository: "broken-repo",
          rootPath: workspace.file("node_modules/broken"),
        },
      ]),
    );

    expect(resolution.unresolved).toHaveLength(1);
    expect(resolution.unresolved[0]).toMatchObject({
      packageName: "broken",
      reason: "TYPECHECK_FAILED",
      requestedSymbol: undefined,
    });
    expect(resolution.unresolved[0]?.detail).toMatch(/^TS\d+: /);
    expect(resolution.symbols).toEqual([]);
  });
});
