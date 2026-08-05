import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";

const NEGATIVE = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/cross-repository-negative",
);
const SHARED_ROOT = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/cross-repository/shared-library",
);
const CONSUMER = path.join(NEGATIVE, "consumer");

const services: LanguageService[] = [];

function provider(
  name: string,
  version: string,
  repository: string,
  rootPath: string,
): PackageProvider {
  return {
    name,
    version,
    repository,
    rootPath,
    manifestPath: path.join(rootPath, "package.json"),
    typesPath: path.join(rootPath, "dist/index.d.ts"),
  };
}

const providers: readonly PackageProvider[] = [
  provider("@luque-fixture/twin", "1.0.0", "twin", path.join(NEGATIVE, "twin")),
  {
    ...provider(
      "@luque-fixture/shared",
      "1.4.2",
      "shared-library",
      SHARED_ROOT,
    ),
    projectPath: path.join(SHARED_ROOT, "tsconfig.json"),
  },
  provider(
    "@luque-fixture/unmapped",
    "1.0.0",
    "unmapped",
    path.join(NEGATIVE, "unmapped"),
  ),
  provider(
    "@luque-fixture/duplicated",
    "1.0.0",
    "duplicated-a",
    path.join(NEGATIVE, "duplicated-a"),
  ),
  provider(
    "@luque-fixture/drifting",
    "2.0.0",
    "drifting",
    path.join(NEGATIVE, "drifting"),
  ),
];

const registry: PackageProviderRegistry = {
  get: (name) => providers.find((entry) => entry.name === name),
};

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
});

describe("cross-repository negative fixture", () => {
  it("classifies every failing case and keeps exact edges truthful", async () => {
    const service = LanguageService.create({ cwd: CONSUMER });
    services.push(service);
    const configFileName = path.join(CONSUMER, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registry,
      {
        conflicts: [
          {
            packageName: "@luque-fixture/duplicated",
            kind: "AMBIGUOUS_PACKAGE_PROVIDER",
            repositories: ["duplicated-a", "duplicated-b"],
          },
          {
            packageName: "@luque-fixture/drifting",
            kind: "PACKAGE_VERSION_MISMATCH",
            versions: ["1.0.0", "2.0.0"],
          },
        ],
      },
    );

    expect(
      resolution.unresolved.map((entry) => [
        entry.packageName,
        entry.reason,
        entry.requestedSymbol,
      ]),
    ).toEqual([
      ["@luque-fixture/unmapped", "DECLARATION_SOURCE_NOT_MAPPED", "unmapped"],
      ["@luque-fixture/unmapped", "EXPORT_NOT_FOUND", "missing"],
      ["@luque-fixture/duplicated", "AMBIGUOUS_PACKAGE_PROVIDER", undefined],
      ["@luque-fixture/drifting", "VERSION_MISMATCH", undefined],
    ]);

    expect(
      resolution.symbols.map((entry) => [
        entry.consumer.name,
        entry.packageName,
        entry.target.name,
        entry.target.declarations[0]?.fileName,
      ]),
    ).toEqual([
      [
        "compute",
        "@luque-fixture/twin",
        "compute",
        path.join(NEGATIVE, "twin/dist/index.d.ts"),
      ],
      [
        "sharedValue",
        "@luque-fixture/shared",
        "value",
        path.join(SHARED_ROOT, "dist/value.d.ts"),
      ],
      [
        "unmapped",
        "@luque-fixture/unmapped",
        "unmapped",
        path.join(NEGATIVE, "unmapped/dist/index.d.ts"),
      ],
    ]);
  });

  it("never links a local homonym or a same-named symbol of another package", async () => {
    const service = LanguageService.create({ cwd: CONSUMER });
    services.push(service);
    const configFileName = path.join(CONSUMER, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registry,
    );

    const localHomonym = resolution.symbols.filter(
      (entry) => entry.consumer.name === "value",
    );
    expect(localHomonym).toEqual([]);

    const compute = resolution.symbols.find(
      (entry) => entry.consumer.name === "compute",
    );
    expect(compute?.packageName).toBe("@luque-fixture/twin");
    expect(
      compute?.target.declarations.every((declaration) =>
        declaration.fileName.startsWith(path.join(NEGATIVE, "twin")),
      ),
    ).toBe(true);

    expect(
      resolution.symbols.every(
        (entry) =>
          entry.target.declarations.length > 0 &&
          entry.provider.name === entry.packageName,
      ),
    ).toBe(true);
  });
});
