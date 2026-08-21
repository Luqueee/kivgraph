/**
 * The provider identity of a symbol imported from an *installed* copy of a
 * package, which is the shape a package manager actually produces.
 *
 * Every other cross-repository fixture reaches its provider inside the
 * provider's own tree -- by `paths`, or through a `node_modules` symlink whose
 * target is that tree. A published dependency is not that: the consumer
 * resolves into `node_modules/.pnpm/<name>@<version>/node_modules/<name>`, a
 * real directory holding `dist` with no `.d.ts.map`, no `src` and no
 * `tsconfig.json`. Nothing under it belongs to any repository, so the source
 * has to be reached by the package *name* its manifest declares. See ADR 0051.
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

// Go hands the worker `repository.RealPath`, and the engine resolves the
// install symlink the same way, so both ends agree on one spelling.
const FIXTURE = realpathSync(
  path.resolve(
    import.meta.dirname,
    "../../testdata/typescript/installed-package",
  ),
);
const SHARED_ROOT = path.join(FIXTURE, "provider-shared");
const DRIFTED_ROOT = path.join(FIXTURE, "provider-drifted");
const CONSUMER = path.join(FIXTURE, "consumer");
const STORE = path.join(FIXTURE, "node_modules/.pnpm");

const services: LanguageService[] = [];

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
});

// The workspace repository that declares `@kivgraph-fixture/installed`. Its
// own `dist` does not exist: nothing was ever built here, and nothing needs
// to be -- what the bridge borrows is the `rootDir`/`outDir` statement.
const installedProvider: PackageProvider = {
  name: "@kivgraph-fixture/installed",
  version: "1.0.0",
  repository: "provider-shared",
  rootPath: SHARED_ROOT,
  manifestPath: path.join(SHARED_ROOT, "package.json"),
  projectPath: path.join(SHARED_ROOT, "tsconfig.json"),
  typesPath: path.join(SHARED_ROOT, "dist/index.d.ts"),
  sourceRoots: [path.join(SHARED_ROOT, "src")],
  declarationRoots: [path.join(SHARED_ROOT, "dist")],
  rootDir: "src",
  outDir: "dist",
};

// Same package name, one publish behind: the installed `1.0.0` exports
// `legacyRetry`, the registered `1.1.0` source renamed it.
const driftedProvider: PackageProvider = {
  name: "@kivgraph-fixture/drifted",
  version: "1.1.0",
  repository: "provider-drifted",
  rootPath: DRIFTED_ROOT,
  manifestPath: path.join(DRIFTED_ROOT, "package.json"),
  projectPath: path.join(DRIFTED_ROOT, "tsconfig.json"),
  typesPath: path.join(DRIFTED_ROOT, "dist/index.d.ts"),
  sourceRoots: [path.join(DRIFTED_ROOT, "src")],
  declarationRoots: [path.join(DRIFTED_ROOT, "dist")],
  rootDir: "src",
  outDir: "dist",
};

async function resolveConsumer(): Promise<ImportedSymbolResolution> {
  const service = LanguageService.create({ cwd: CONSUMER });
  services.push(service);
  const configFileName = path.join(CONSUMER, "tsconfig.json");
  await service.openProject(configFileName);
  const view = service.project(configFileName);
  return resolveImportedSymbols(
    service,
    view,
    // `@kivgraph-fixture/vendored` is deliberately absent: it is the
    // transitive dependency the store hangs beside the package the consumer
    // asked for, and no repository declares it.
    createPackageProviderRegistry([installedProvider, driftedProvider]),
  );
}

const installedRoot = path.join(
  STORE,
  "@kivgraph-fixture+installed@1.0.0/node_modules/@kivgraph-fixture/installed",
);

describe("a symbol imported from an installed copy of a package", () => {
  it("resolves to the workspace source that declares the package", async () => {
    const resolution = await resolveConsumer();
    const withRetry = resolution.symbols.find(
      (entry) => entry.exportedName === "withRetry",
    );
    if (withRetry === undefined) {
      throw new Error("expected the consumer to import withRetry");
    }

    // What the checker resolved is the installed artifact, and it carries no
    // map: this is the state the corpus measurement found and abandoned.
    const [declaration] = withRetry.target.declarations;
    expect(declaration?.fileName).toBe(
      path.join(installedRoot, "dist/retry.d.ts"),
    );
    expect(declaration?.sourcePosition).toBeUndefined();
    expect(declaration?.sourceStatus).toBe("INSTALLED_PACKAGE");
    expect(declaration?.sourceFiles).toEqual([
      path.join(SHARED_ROOT, "src/retry.ts"),
    ]);

    // The identity names the workspace repository, at the position its own
    // checker gives. `PROVIDER_EXPORT` is what grades the edge
    // EXACT_PACKAGE_MAPPED/TYPESCRIPT_PROJECT_REFERENCE: the artifact-to-source
    // step rests on the provider's build configuration, not on a map.
    expect(withRetry.target.identityReason).toBeUndefined();
    expect(withRetry.target.identity).toEqual({
      repository: "provider-shared",
      package: "@kivgraph-fixture/installed",
      qualifiedName: "withRetry",
      kind: "function",
      signature:
        "export async function withRetry<T>(run: () => Promise<T>, attempts = 3): Promise<T>",
      file: "src/retry.ts",
      startLine: 5,
      source: "PROVIDER_EXPORT",
    });
  });

  it("refuses a name the workspace source no longer exports", async () => {
    const resolution = await resolveConsumer();
    const legacy = resolution.symbols.find(
      (entry) => entry.exportedName === "legacyRetry",
    );
    if (legacy === undefined) {
      throw new Error("expected the consumer to import legacyRetry");
    }

    // The bridge names the source -- `dist/legacy.d.ts` re-roots onto a
    // `src/legacy.ts` that exists -- so the file step succeeded.
    expect(legacy.target.declarations[0]?.sourceStatus).toBe(
      "INSTALLED_PACKAGE",
    );
    expect(legacy.target.declarations[0]?.sourceFiles).toEqual([
      path.join(DRIFTED_ROOT, "src/legacy.ts"),
    ]);

    // The name step did not. Falling back to the installed artifact would
    // compose a key `provider-drifted` does not publish, so there is no
    // identity and no edge: only a reason.
    expect(legacy.target.identity).toBeUndefined();
    expect(legacy.target.identityReason).toBe("PROVIDER_SOURCE_UNAVAILABLE");
    expect(legacy.target.identityDetail).toBe(
      `provider project exports no legacyRetry in ${path.join(DRIFTED_ROOT, "src/legacy.ts")}`,
    );
  });

  it("leaves a package no registered repository declares unresolved", async () => {
    const resolution = await resolveConsumer();
    const vendored = resolution.symbols.find(
      (entry) => entry.exportedName === "vendoredHelper",
    );
    if (vendored === undefined) {
      throw new Error("expected the consumer to import vendoredHelper");
    }

    // The import spelled `@kivgraph-fixture/installed`, which *is* declared
    // by a registered repository -- but the declaration it reaches belongs to
    // the transitive `@kivgraph-fixture/vendored`, and the nearest manifest is
    // what the registry is asked about. Crediting the package the consumer
    // spelled would attribute code to a repository that never wrote it.
    expect(vendored.packageName).toBe("@kivgraph-fixture/installed");
    expect(vendored.target.declarations[0]?.fileName).toBe(
      path.join(
        STORE,
        "@kivgraph-fixture+vendored@2.0.0/node_modules/@kivgraph-fixture/vendored/dist/index.d.ts",
      ),
    );
    expect(vendored.target.declarations[0]?.sourceStatus).toBe("UNRESOLVED");
    expect(vendored.target.declarations[0]?.sourceFiles).toEqual([]);
    expect(vendored.target.identity).toBeUndefined();
    expect(vendored.target.identityReason).toBe("PROVIDER_SOURCE_UNAVAILABLE");
  });
});
