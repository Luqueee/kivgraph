import type { LanguageService, ProjectView } from "./language-service.js";
import {
  resolvePackageImports,
  type PackageImport,
  type PackageImportResolutionOptions,
  type PackageProvider,
  type PackageProviderRegistry,
} from "./package-import-resolver.js";

/** Evidence for one checker-resolved import contributing to a package edge. */
export interface PackageDependencyImport {
  readonly fileName: string;
  readonly specifier: string;
  /** UTF-16 source offsets, with `end` exclusive. */
  readonly start: number;
  readonly end: number;
}

/** One real package-to-package dependency. */
export interface PackageDependency {
  readonly kind: "PACKAGE_DEPENDS_ON";
  readonly consumer: PackageProvider;
  readonly provider: PackageProvider;
  readonly imports: readonly PackageDependencyImport[];
}

export interface PackageDependencyResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly imports: readonly PackageImport[];
  readonly dependencies: readonly PackageDependency[];
}

/**
 * Resolve package imports and project them into package-level graph edges.
 *
 * The consumer is supplied by the Go package registry, just like providers.
 * This keeps package identity authoritative: the worker never turns a
 * package-name string into a graph endpoint. An edge requires both a
 * checker-resolved module and the provider returned by the registry.
 */
export async function resolvePackageDependencies(
  service: LanguageService,
  view: ProjectView,
  registry: PackageProviderRegistry,
  consumer: PackageProvider,
  options: PackageImportResolutionOptions = {},
): Promise<PackageDependencyResolution> {
  service.assertFresh(view);
  const imports = await resolvePackageImports(service, view, registry, options);
  const dependencies = createPackageDependencies(consumer, imports.imports);
  service.assertFresh(view);
  return {
    generation: imports.generation,
    configFileName: imports.configFileName,
    imports: imports.imports,
    dependencies,
  };
}

/**
 * Build deterministic package edges from one package-import resolution.
 *
 * Non-resolved imports, missing providers, provider/name mismatches and
 * self-references are deliberately omitted. None of those facts identifies a
 * dependency between two real packages.
 */
export function createPackageDependencies(
  consumer: PackageProvider,
  imports: readonly PackageImport[],
): PackageDependency[] {
  if (!isRealPackage(consumer)) {
    return [];
  }

  const byProvider = new Map<
    string,
    { provider: PackageProvider; imports: PackageDependencyImport[] }
  >();
  for (const packageImport of imports) {
    const provider = packageImport.provider;
    if (
      packageImport.status !== "RESOLVED" ||
      provider === undefined ||
      packageImport.packageName !== provider.name ||
      !isRealPackage(provider) ||
      samePackage(consumer, provider)
    ) {
      continue;
    }

    const key = packageIdentity(provider);
    let dependency = byProvider.get(key);
    if (dependency === undefined) {
      dependency = { provider: cloneProvider(provider), imports: [] };
      byProvider.set(key, dependency);
    }
    dependency.imports.push({
      fileName: packageImport.fileName,
      specifier: packageImport.specifier,
      start: packageImport.start,
      end: packageImport.end,
    });
  }

  return [...byProvider.values()]
    .map(({ provider, imports: evidence }) => ({
      kind: "PACKAGE_DEPENDS_ON" as const,
      consumer: cloneProvider(consumer),
      provider,
      imports: uniqueAndSortImports(evidence),
    }))
    .sort(compareDependencies);
}

function isRealPackage(provider: PackageProvider): boolean {
  return (
    provider.name.trim() !== "" &&
    provider.repository.trim() !== "" &&
    provider.rootPath.trim() !== ""
  );
}

function samePackage(left: PackageProvider, right: PackageProvider): boolean {
  if (left.repository !== right.repository || left.name !== right.name) {
    return false;
  }
  if (left.manifestPath !== undefined && right.manifestPath !== undefined) {
    return left.manifestPath === right.manifestPath;
  }
  return left.rootPath === right.rootPath;
}

function packageIdentity(provider: PackageProvider): string {
  return [
    provider.repository,
    provider.name,
    provider.version,
    provider.rootPath,
    provider.manifestPath ?? "",
  ].join("\u0000");
}

function cloneProvider(provider: PackageProvider): PackageProvider {
  return {
    ...provider,
    sourceRoots:
      provider.sourceRoots === undefined
        ? undefined
        : [...provider.sourceRoots],
    declarationRoots:
      provider.declarationRoots === undefined
        ? undefined
        : [...provider.declarationRoots],
  };
}

function uniqueAndSortImports(
  imports: readonly PackageDependencyImport[],
): PackageDependencyImport[] {
  const unique = new Map<string, PackageDependencyImport>();
  for (const packageImport of imports) {
    const key = [
      packageImport.fileName,
      packageImport.specifier,
      packageImport.start,
      packageImport.end,
    ].join("\u0000");
    unique.set(key, packageImport);
  }
  return [...unique.values()].sort(compareImports);
}

function compareImports(
  left: PackageDependencyImport,
  right: PackageDependencyImport,
): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.start - right.start ||
    left.end - right.end ||
    left.specifier.localeCompare(right.specifier)
  );
}

function compareDependencies(
  left: PackageDependency,
  right: PackageDependency,
): number {
  return (
    left.provider.repository.localeCompare(right.provider.repository) ||
    left.provider.name.localeCompare(right.provider.name) ||
    left.provider.version.localeCompare(right.provider.version) ||
    left.provider.rootPath.localeCompare(right.provider.rootPath)
  );
}
