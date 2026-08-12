import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService } from "./language-service.js";
import {
  createPackageProviderRegistry,
  type PackageProvider,
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
  provider(
    "@ladygraph-fixture/twin",
    "1.0.0",
    "twin",
    path.join(NEGATIVE, "twin"),
  ),
  {
    ...provider(
      "@ladygraph-fixture/shared",
      "1.4.2",
      "shared-library",
      SHARED_ROOT,
    ),
    projectPath: path.join(SHARED_ROOT, "tsconfig.json"),
  },
  provider(
    "@ladygraph-fixture/unmapped",
    "1.0.0",
    "unmapped",
    path.join(NEGATIVE, "unmapped"),
  ),
  provider(
    "@ladygraph-fixture/duplicated",
    "1.0.0",
    "duplicated-a",
    path.join(NEGATIVE, "duplicated-a"),
  ),
  provider(
    "@ladygraph-fixture/drifting",
    "2.0.0",
    "drifting",
    path.join(NEGATIVE, "drifting"),
  ),
  {
    ...provider(
      "@ladygraph-fixture/nomap",
      "1.0.0",
      "nomap",
      path.join(NEGATIVE, "nomap"),
    ),
    projectPath: path.join(NEGATIVE, "nomap/tsconfig.json"),
  },
];

const registry = createPackageProviderRegistry(providers);

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
            packageName: "@ladygraph-fixture/duplicated",
            kind: "AMBIGUOUS_PACKAGE_PROVIDER",
            repositories: ["duplicated-a", "duplicated-b"],
          },
          {
            packageName: "@ladygraph-fixture/drifting",
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
      [
        "@ladygraph-fixture/unmapped",
        "DECLARATION_SOURCE_NOT_MAPPED",
        "unmapped",
      ],
      ["@ladygraph-fixture/unmapped", "EXPORT_NOT_FOUND", "missing"],
      [
        "@ladygraph-fixture/duplicated",
        "AMBIGUOUS_PACKAGE_PROVIDER",
        undefined,
      ],
      ["@ladygraph-fixture/drifting", "VERSION_MISMATCH", undefined],
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
        "@ladygraph-fixture/twin",
        "compute",
        path.join(NEGATIVE, "twin/dist/index.d.ts"),
      ],
      [
        "sharedValue",
        "@ladygraph-fixture/shared",
        "value",
        path.join(SHARED_ROOT, "dist/value.d.ts"),
      ],
      [
        "unmapped",
        "@ladygraph-fixture/unmapped",
        "unmapped",
        path.join(NEGATIVE, "unmapped/dist/index.d.ts"),
      ],
      [
        "plain",
        "@ladygraph-fixture/nomap",
        "plain",
        path.join(NEGATIVE, "nomap/dist/index.d.ts"),
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
    expect(compute?.packageName).toBe("@ladygraph-fixture/twin");
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
