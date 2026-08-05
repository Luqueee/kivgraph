import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { resolveImportedSymbols } from "./imported-symbol-resolver.js";
import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import { resolveProviderSourcePositions } from "./provider-source-position-resolver.js";

const NEGATIVE = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/cross-repository-negative",
);
const CONSUMER = path.join(NEGATIVE, "consumer");
const NOMAP_ROOT = path.join(NEGATIVE, "nomap");
const UNMAPPED_ROOT = path.join(NEGATIVE, "unmapped");

const services: LanguageService[] = [];

const providers: readonly PackageProvider[] = [
  {
    name: "@luque-fixture/nomap",
    version: "1.0.0",
    repository: "nomap",
    rootPath: NOMAP_ROOT,
    manifestPath: path.join(NOMAP_ROOT, "package.json"),
    typesPath: path.join(NOMAP_ROOT, "dist/index.d.ts"),
    projectPath: path.join(NOMAP_ROOT, "tsconfig.json"),
  },
  {
    name: "@luque-fixture/unmapped",
    version: "1.0.0",
    repository: "unmapped",
    rootPath: UNMAPPED_ROOT,
    manifestPath: path.join(UNMAPPED_ROOT, "package.json"),
    typesPath: path.join(UNMAPPED_ROOT, "dist/index.d.ts"),
  },
];

const registry: PackageProviderRegistry = {
  get: (name) => providers.find((entry) => entry.name === name),
};

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
});

describe("provider source positions", () => {
  it("places a symbol of a provider that ships no declaration map", async () => {
    const service = LanguageService.create({ cwd: CONSUMER });
    services.push(service);
    const configFileName = path.join(CONSUMER, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveImportedSymbols(service, view, registry);

    const plain = resolution.symbols.find(
      (entry) => entry.consumer.name === "plain",
    );
    expect(plain?.target.declarations[0]).toMatchObject({
      fileName: path.join(NOMAP_ROOT, "dist/index.d.ts"),
      sourceFiles: [path.join(NOMAP_ROOT, "src/index.ts")],
      sourcePosition: undefined,
    });

    const located = await resolveProviderSourcePositions(resolution);

    expect(located.positions).toEqual([
      {
        declarationFile: path.join(NOMAP_ROOT, "dist/index.d.ts"),
        exportedName: "plain",
        position: {
          fileName: path.join(NOMAP_ROOT, "src/index.ts"),
          line: 1,
          character: 13,
        },
      },
    ]);
    expect(located.unresolved).toEqual([]);
  });

  it("requests nothing for a provider with no project of its own", async () => {
    const service = LanguageService.create({ cwd: CONSUMER });
    services.push(service);
    const configFileName = path.join(CONSUMER, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveImportedSymbols(service, view, {
      get: (name) =>
        name === "@luque-fixture/unmapped" ? providers[1] : undefined,
    });

    const unmapped = resolution.symbols.find(
      (entry) => entry.consumer.name === "unmapped",
    );
    expect(unmapped?.target.declarations[0]?.sourceFiles).toEqual([]);

    const located = await resolveProviderSourcePositions(resolution);
    expect(located).toEqual({ positions: [], unresolved: [] });
  });
});
