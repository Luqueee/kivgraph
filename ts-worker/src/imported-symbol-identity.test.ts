/**
 * LUQUE-0907 — the provider identity `ImportedSymbolTarget.identity` carries.
 *
 * Every case here runs over the real, `tsc`-compiled fixtures of LUQUE-0707
 * and LUQUE-0711: no hand-written payload stands in for the checker or for
 * `declaration-classifier.ts`. The point of the ticket is that two
 * independent walks of the same bytes agree by construction, and a synthetic
 * fixture could not prove that.
 */

import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  resolveImportedSymbols,
  type ImportedSymbolResolution,
} from "./imported-symbol-resolver.js";
import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
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
  name: "@luque-fixture/shared",
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

const sharedRegistry: PackageProviderRegistry = {
  get: (name) => (name === sharedProvider.name ? sharedProvider : undefined),
};

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
      package: "@luque-fixture/shared",
      qualifiedName: "compute",
      kind: "function",
      signature: "export function compute(input: number): number",
      file: "src/value.ts",
      startLine: 7,
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
      package: "@luque-fixture/shared",
      qualifiedName: "helper",
      kind: "function",
      signature: "export function helper(shape: Shape): number",
      file: "src/helper.ts",
      startLine: 3,
    });
  });

  it("reports no identity, only a reason, for a provider with no declaration map", async () => {
    const nomapProvider: PackageProvider = {
      name: "@luque-fixture/nomap",
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
    const resolution = await resolveImportedSymbols(service, view, {
      get: (name) => (name === nomapProvider.name ? nomapProvider : undefined),
    });

    const plain = resolution.symbols.find(
      (entry) => entry.consumer.name === "plain",
    );
    expect(plain).toBeDefined();
    // `nomap` ships no `.d.ts.map`: the checker still resolves the exact
    // declaration, but nothing places it inside the provider's source.
    expect(
      plain?.target.declarations.some(
        (declaration) => declaration.sourceStatus === "DECLARATION_MAP",
      ),
    ).toBe(false);
    expect(plain?.target.identity).toBeUndefined();
    expect(plain?.target.identityReason).toBe("PROVIDER_SOURCE_UNAVAILABLE");
    expect(plain?.target.identityDetail).toBeTruthy();
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
