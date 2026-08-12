/**
 * LUQUE-0907 — the provider identity `ImportedSymbolTarget.identity` carries.
 *
 * Every case here runs over the real, `tsc`-compiled fixtures of LUQUE-0707
 * and LUQUE-0711: no hand-written payload stands in for the checker or for
 * `declaration-classifier.ts`. The point of the ticket is that two
 * independent walks of the same bytes agree by construction, and a synthetic
 * fixture could not prove that.
 */

import { realpathSync } from "node:fs";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  resolveImportedSymbols,
  type ImportedSymbolResolution,
} from "./imported-symbol-resolver.js";
import { LanguageService } from "./language-service.js";
import {
  createPackageProviderRegistry,
  type PackageProvider,
} from "./package-import-resolver.js";

const FIXTURE = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/cross-repository",
);
const SHARED_ROOT = path.join(FIXTURE, "shared-library");

const NEGATIVE = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/cross-repository-negative",
);
const NOMAP_ROOT = path.join(NEGATIVE, "nomap");
const NEGATIVE_CONSUMER = path.join(NEGATIVE, "consumer");

const services: LanguageService[] = [];

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
});

// Same shape `cross-repository-positive.test.ts` builds by hand: a real
// declaration map lives on disk for every declaration this provider reaches.
const sharedProvider: PackageProvider = {
  name: "@ladygraph-fixture/shared",
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

const sharedRegistry = createPackageProviderRegistry([sharedProvider]);

async function resolveConsumer(
  name: string,
): Promise<ImportedSymbolResolution> {
  const root = path.join(FIXTURE, name);
  const service = LanguageService.create({ cwd: root });
  services.push(service);
  const configFileName = path.join(root, "tsconfig.json");
  await service.openProject(configFileName);
  const view = service.project(configFileName);
  return resolveImportedSymbols(service, view, sharedRegistry);
}

describe("provider identity of an exact IMPORTS_SYMBOL target", () => {
  it("classifies compute from the provider's source, not from its .d.ts", async () => {
    const resolution = await resolveConsumer("consumer-a");
    const compute = resolution.symbols.find(
      (entry) => entry.consumer.name === "compute",
    );

    expect(compute?.target.identityReason).toBeUndefined();
    expect(compute?.target.identityDetail).toBeUndefined();
    // The .d.ts reads `export declare function compute(input: number): number`;
    // proving the signature came from `src/value.ts` is the whole ticket.
    expect(compute?.target.identity).toEqual({
      repository: "shared-library",
      package: "@ladygraph-fixture/shared",
      qualifiedName: "compute",
      kind: "function",
      signature: "export function compute(input: number): number",
      file: "src/value.ts",
      startLine: 7,
      source: "DECLARATION_MAP",
    });
  });

  it("classifies an aliased barrel import from its real declaration", async () => {
    const resolution = await resolveConsumer("consumer-b");
    const helper = resolution.symbols.find(
      (entry) => entry.consumer.name === "helper",
    );

    // The binding is `aliasedHelper as helper`; the identity must name the
    // provider's real declaration, "helper", never the requested alias.
    expect(helper?.exportedName).toBe("aliasedHelper");
    expect(helper?.target.identityReason).toBeUndefined();
    expect(helper?.target.identity).toEqual({
      repository: "shared-library",
      package: "@ladygraph-fixture/shared",
      qualifiedName: "helper",
      kind: "function",
      signature: "export function helper(shape: Shape): number",
      file: "src/helper.ts",
      startLine: 3,
      source: "DECLARATION_MAP",
    });
  });

  it("places a symbol of a provider that ships no declaration map", async () => {
    const nomapProvider: PackageProvider = {
      name: "@ladygraph-fixture/nomap",
      version: "1.0.0",
      repository: "nomap",
      rootPath: NOMAP_ROOT,
      manifestPath: path.join(NOMAP_ROOT, "package.json"),
      typesPath: path.join(NOMAP_ROOT, "dist/index.d.ts"),
      projectPath: path.join(NOMAP_ROOT, "tsconfig.json"),
    };
    const service = LanguageService.create({ cwd: NEGATIVE_CONSUMER });
    services.push(service);
    const configFileName = path.join(NEGATIVE_CONSUMER, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveImportedSymbols(
      service,
      view,
      createPackageProviderRegistry([nomapProvider]),
    );

    const plain = resolution.symbols.find(
      (entry) => entry.consumer.name === "plain",
    );
    expect(plain).toBeDefined();
    // `nomap` ships no `.d.ts.map`, so nothing places the symbol inside the
    // source its project roots name. The provider's own checker still says
    // which declaration that source exports under the requested name, and
    // the identity is graded by how it was reached.
    expect(
      plain?.target.declarations.some(
        (declaration) => declaration.sourceStatus === "DECLARATION_MAP",
      ),
    ).toBe(false);
    expect(plain?.target.identityReason).toBeUndefined();
    expect(plain?.target.identity).toEqual({
      repository: "nomap",
      package: "@ladygraph-fixture/nomap",
      qualifiedName: "plain",
      kind: "variable",
      signature: "plain",
      file: "src/index.ts",
      startLine: 1,
      source: "PROVIDER_EXPORT",
    });
  });

  it("reports no identity when nothing names the provider's source", async () => {
    // `unmapped` publishes a `.d.ts` and no source at all: no map, and no
    // project root that could name one. There is nothing to ask a checker.
    const unmappedRoot = path.join(NEGATIVE, "unmapped");
    const unmappedProvider: PackageProvider = {
      name: "@ladygraph-fixture/unmapped",
      version: "1.0.0",
      repository: "unmapped",
      rootPath: unmappedRoot,
      manifestPath: path.join(unmappedRoot, "package.json"),
      typesPath: path.join(unmappedRoot, "dist/index.d.ts"),
    };
    const service = LanguageService.create({ cwd: NEGATIVE_CONSUMER });
    services.push(service);
    const configFileName = path.join(NEGATIVE_CONSUMER, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveImportedSymbols(
      service,
      view,
      createPackageProviderRegistry([unmappedProvider]),
    );

    const unmapped = resolution.symbols.find(
      (entry) => entry.consumer.name === "unmapped",
    );
    expect(unmapped).toBeDefined();
    expect(unmapped?.target.declarations[0]?.sourceFiles).toEqual([]);
    expect(unmapped?.target.identity).toBeUndefined();
    expect(unmapped?.target.identityReason).toBe("PROVIDER_SOURCE_UNAVAILABLE");
    expect(unmapped?.target.identityDetail).toBeTruthy();
  });

  it("places a provider reached through a node_modules symlink", async () => {
    // The shape a package manager actually installs: the consumer resolves
    // the provider through `node_modules`, and the engine reports the link
    // target. Every fixture that resolves by `paths` misses this, which is
    // how a provider consumed from a real workspace went unplaced.
    const linkedConsumer = path.join(NEGATIVE, "consumer-linked");
    // Go hands the worker `repository.RealPath`; the engine resolves the
    // link the same way, so both ends agree on one spelling.
    const nomapRoot = realpathSync(NOMAP_ROOT);
    const nomapProvider: PackageProvider = {
      name: "@ladygraph-fixture/nomap",
      version: "1.0.0",
      repository: "nomap",
      rootPath: nomapRoot,
      manifestPath: path.join(nomapRoot, "package.json"),
      typesPath: path.join(nomapRoot, "dist/index.d.ts"),
      projectPath: path.join(nomapRoot, "tsconfig.json"),
      sourceRoots: [path.join(nomapRoot, "src")],
      declarationRoots: [path.join(nomapRoot, "dist")],
    };
    const service = LanguageService.create({ cwd: linkedConsumer });
    services.push(service);
    const configFileName = path.join(linkedConsumer, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveImportedSymbols(
      service,
      view,
      createPackageProviderRegistry([nomapProvider]),
    );

    const plain = resolution.symbols.find(
      (entry) => entry.consumer.name === "plain",
    );
    expect(plain?.target.identityReason).toBeUndefined();
    expect(plain?.target.identity).toEqual({
      repository: "nomap",
      package: "@ladygraph-fixture/nomap",
      qualifiedName: "plain",
      kind: "variable",
      signature: "plain",
      file: "src/index.ts",
      startLine: 1,
      source: "PROVIDER_EXPORT",
    });
  });

  it("computes the identical payload across repeated, independent resolutions", async () => {
    const first = await resolveConsumer("consumer-b");
    const second = await resolveConsumer("consumer-b");

    expect(JSON.stringify(second.symbols)).toBe(JSON.stringify(first.symbols));
    expect(
      first.symbols.every((entry) => entry.target.identity !== undefined),
    ).toBe(true);
  });
});

const FACADE_ROOT = path.join(FIXTURE, "facade-library");

// The facade declares nothing: it re-exports the shared library so consumers
// depend on one package. Its declaration map names its own barrel, and the
// declaration a consumer actually reaches lives in the shared repository.
const facadeProvider: PackageProvider = {
  name: "@ladygraph-fixture/facade",
  version: "3.1.0",
  repository: "facade-library",
  rootPath: FACADE_ROOT,
  manifestPath: path.join(FACADE_ROOT, "package.json"),
  projectPath: path.join(FACADE_ROOT, "tsconfig.json"),
  typesPath: path.join(FACADE_ROOT, "dist/index.d.ts"),
  sourceRoots: [path.join(FACADE_ROOT, "src")],
  declarationRoots: [path.join(FACADE_ROOT, "dist")],
  rootDir: "src",
  outDir: "dist",
};

describe("a symbol reached through a re-exporting package", () => {
  it("is credited to the repository that declares it", async () => {
    const root = path.join(FIXTURE, "consumer-c");
    const service = LanguageService.create({ cwd: root });
    services.push(service);
    const configFileName = path.join(root, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveImportedSymbols(
      service,
      view,
      createPackageProviderRegistry([facadeProvider, sharedProvider]),
    );

    const compute = resolution.symbols.find(
      (entry) => entry.exportedName === "compute",
    );
    if (compute === undefined) {
      throw new Error("expected the consumer to import compute");
    }

    // The import names the facade, and that is what the edge records as the
    // package the consumer depends on.
    expect(compute.packageName).toBe("@ladygraph-fixture/facade");

    // The identity, though, has to name the declaring repository: it is what
    // composes the stable key, and only the shared library publishes a symbol
    // under it. Crediting the facade would compose a key nobody publishes and
    // leave the edge dangling.
    expect(compute.target.identityReason).toBeUndefined();
    expect(compute.target.identity).toMatchObject({
      repository: "shared-library",
      package: "@ladygraph-fixture/shared",
      qualifiedName: "compute",
      kind: "function",
      source: "DECLARATION_MAP",
    });
    expect(compute.target.identity?.file).toBe("src/value.ts");
  });
});
