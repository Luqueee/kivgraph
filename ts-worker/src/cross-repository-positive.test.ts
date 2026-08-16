import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import { resolvePackageDependencies } from "./package-dependency-resolver.js";
import {
  createPackageProviderRegistry,
  type PackageProvider,
} from "./package-import-resolver.js";
import { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";

const FIXTURE = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/cross-repository",
);
const SHARED_ROOT = path.join(FIXTURE, "shared-library");

const services: LanguageService[] = [];

function openConsumer(name: string): {
  service: LanguageService;
  configFileName: string;
  root: string;
} {
  const root = path.join(FIXTURE, name);
  const service = LanguageService.create({ cwd: root });
  services.push(service);
  return { service, configFileName: path.join(root, "tsconfig.json"), root };
}

const sharedProvider: PackageProvider = {
  name: "@kivgraph-fixture/shared",
  version: "1.4.2",
  repository: "shared-library",
  rootPath: SHARED_ROOT,
  manifestPath: path.join(SHARED_ROOT, "package.json"),
  projectPath: path.join(SHARED_ROOT, "tsconfig.json"),
  typesPath: path.join(SHARED_ROOT, "dist/index.d.ts"),
  sourceRoots: [path.join(SHARED_ROOT, "src")],
  declarationRoots: [path.join(SHARED_ROOT, "dist")],
  rootDir: "src",
  outDir: "dist",
};

const registry = createPackageProviderRegistry([sharedProvider]);

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
});

describe("cross-repository positive fixture", () => {
  it("resolves direct value and type imports to mapped provider sources", async () => {
    const { service, configFileName, root } = openConsumer("consumer-a");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registry,
    );

    expect(resolution.unresolved).toEqual([]);
    // "Widget" comes from src/derived.ts, which sorts before src/direct.ts;
    // the other three are direct.ts's value and type imports.
    expect(
      resolution.symbols.map((entry) => [
        entry.consumer.name,
        entry.exportedName,
        entry.target.name,
      ]),
    ).toEqual([
      ["Widget", "Widget", "Widget"],
      ["compute", "compute", "compute"],
      ["value", "value", "value"],
      ["Shape", "Shape", "Shape"],
    ]);
    const [widget, ...directImports] = resolution.symbols;

    expect(widget?.consumer.fileName).toBe(path.join(root, "src/derived.ts"));
    expect(widget?.provider.repository).toBe("shared-library");
    expect(widget?.target.declarations).toEqual([
      expect.objectContaining({
        fileName: path.join(SHARED_ROOT, "dist/inheritance.d.ts"),
        sourceStatus: "DECLARATION_MAP",
        sourceFiles: [path.join(SHARED_ROOT, "src/inheritance.ts")],
      }),
    ]);
    expect(widget?.target.declarations[0]?.sourcePosition).toEqual({
      fileName: path.join(SHARED_ROOT, "src/inheritance.ts"),
      line: 26,
      character: 13,
    });

    expect(
      directImports.every(
        (entry) =>
          entry.consumer.fileName === path.join(root, "src/direct.ts") &&
          entry.provider.repository === "shared-library",
      ),
    ).toBe(true);
    for (const entry of directImports) {
      expect(entry.target.declarations).toEqual([
        expect.objectContaining({
          fileName: path.join(SHARED_ROOT, "dist/value.d.ts"),
          sourceStatus: "DECLARATION_MAP",
          sourceFiles: [path.join(SHARED_ROOT, "src/value.ts")],
        }),
      ]);
    }

    expect(
      directImports.map((entry) => [
        entry.consumer.name,
        entry.target.declarations[0]?.sourcePosition,
      ]),
    ).toEqual([
      [
        "compute",
        {
          fileName: path.join(SHARED_ROOT, "src/value.ts"),
          line: 7,
          character: 16,
        },
      ],
      [
        "value",
        {
          fileName: path.join(SHARED_ROOT, "src/value.ts"),
          line: 1,
          character: 13,
        },
      ],
      [
        "Shape",
        {
          fileName: path.join(SHARED_ROOT, "src/value.ts"),
          line: 3,
          character: 17,
        },
      ],
    ]);
  });

  it("resolves barrels, aliases, and a namespace member, keeping re-exports separate", async () => {
    const { service, configFileName, root } = openConsumer("consumer-b");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registry,
    );

    expect(resolution.unresolved).toEqual([]);
    // "helper" is a genuine import; "compute" is read off the namespace
    // import `shared.compute` — both are `IMPORTS_SYMBOL` edges. The
    // re-export "republished" is a different edge kind entirely, checked
    // separately below.
    expect(
      resolution.symbols.map((entry) => [
        entry.consumer.name,
        entry.exportedName,
        entry.target.name,
        entry.target.declarations[0]?.fileName,
        entry.target.declarations[0]?.sourceFiles[0],
      ]),
    ).toEqual([
      [
        "helper",
        "aliasedHelper",
        "helper",
        path.join(SHARED_ROOT, "dist/helper.d.ts"),
        path.join(SHARED_ROOT, "src/helper.ts"),
      ],
      [
        "compute",
        "compute",
        "compute",
        path.join(SHARED_ROOT, "dist/value.d.ts"),
        path.join(SHARED_ROOT, "src/value.ts"),
      ],
    ]);
    expect(
      resolution.symbols.map(
        (entry) => entry.target.declarations[0]?.sourcePosition,
      ),
    ).toEqual([
      {
        fileName: path.join(SHARED_ROOT, "src/helper.ts"),
        line: 3,
        character: 16,
      },
      {
        fileName: path.join(SHARED_ROOT, "src/value.ts"),
        line: 7,
        character: 16,
      },
    ]);
    expect(
      resolution.imports.some((entry) => entry.exportMode === "NAMESPACE"),
    ).toBe(true);
    expect(
      resolution.symbols.every(
        (entry) => entry.consumer.fileName === path.join(root, "src/barrel.ts"),
      ),
    ).toBe(true);

    // `export { value as republished } from "@kivgraph-fixture/shared"` is a
    // re-export, not an import: it reaches the provider's "value" through
    // `REEXPORTS`, resolved by the exact same identity machinery.
    expect(
      resolution.reexports.map((entry) => [
        entry.export.name,
        entry.exportedName,
        entry.target.name,
        entry.target.declarations[0]?.fileName,
        entry.target.declarations[0]?.sourcePosition,
      ]),
    ).toEqual([
      [
        "republished",
        "value",
        "value",
        path.join(SHARED_ROOT, "dist/value.d.ts"),
        {
          fileName: path.join(SHARED_ROOT, "src/value.ts"),
          line: 1,
          character: 13,
        },
      ],
    ]);
    expect(
      resolution.reexports.every(
        (entry) => entry.export.fileName === path.join(root, "src/barrel.ts"),
      ),
    ).toBe(true);
  });

  it("creates one package dependency per consumer repository", async () => {
    const { service, configFileName } = openConsumer("consumer-b");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolvePackageDependencies(
      service,
      view,
      registry,
      {
        name: "@kivgraph-fixture/consumer-b",
        version: "3.1.0",
        repository: "consumer-b",
        rootPath: path.join(FIXTURE, "consumer-b"),
        manifestPath: path.join(FIXTURE, "consumer-b/package.json"),
      },
    );

    expect(resolution.dependencies).toHaveLength(1);
    expect(resolution.dependencies[0]).toMatchObject({
      kind: "PACKAGE_DEPENDS_ON",
      consumer: { repository: "consumer-b" },
      provider: { repository: "shared-library", version: "1.4.2" },
    });
    expect(resolution.dependencies[0]?.imports).toHaveLength(3);
  });
});
