/**
 * Classification of TypeScript cross-repository references Luque cannot turn
 * into an exact edge.
 *
 * Every reason is backed by an observed fact: a registry conflict reported by
 * the Go workspace, a module the checker did not resolve, a semantic
 * diagnostic covering the import, an export the module does not expose, or a
 * declaration artifact with no source mapping. Nothing here is inferred from
 * names.
 */

import { DiagnosticCategory } from "typescript/unstable/async";

import type { DeclarationSourceMapping } from "./declaration-source-resolver.js";
import {
  resolveImportedSymbols,
  type ImportedSymbol,
} from "./imported-symbol-resolver.js";
import type { LanguageService, ProjectView } from "./language-service.js";
import type {
  PackageImport,
  PackageImportResolutionOptions,
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import type { ProviderExport } from "./provider-export-resolver.js";

export type UnresolvedReferenceReason =
  | "AMBIGUOUS_PACKAGE_PROVIDER"
  | "VERSION_MISMATCH"
  | "PACKAGE_PROVIDER_NOT_FOUND"
  | "MODULE_NOT_RESOLVED"
  | "TYPECHECK_FAILED"
  | "EXPORT_NOT_FOUND"
  | "DECLARATION_SOURCE_NOT_MAPPED";

/**
 * A cross-repository provider conflict detected by the Go registry.
 *
 * The worker never decides package identity across repositories; it only
 * reports the consequence for the import sites it sees.
 */
export interface PackageProviderConflict {
  readonly packageName: string;
  readonly kind: "AMBIGUOUS_PACKAGE_PROVIDER" | "PACKAGE_VERSION_MISMATCH";
  readonly repositories?: readonly string[];
  readonly versions?: readonly string[];
}

export interface UnresolvedReferenceOptions
  extends PackageImportResolutionOptions {
  /** Conflicts produced by `internal/workspace.DetectProviderConflicts`. */
  conflicts?: readonly PackageProviderConflict[];
}

/** One import occurrence that did not produce an exact symbol edge. */
export interface UnresolvedReference {
  readonly fileName: string;
  readonly specifier: string;
  readonly packageName: string;
  /** Public name requested from the provider, when the reason is per name. */
  readonly requestedSymbol: string | undefined;
  readonly reason: UnresolvedReferenceReason;
  readonly provider: PackageProvider | undefined;
  /** Observed evidence: repositories, versions, diagnostic or declaration. */
  readonly detail: string | undefined;
  /** UTF-16 source offsets of the module specifier, with `end` exclusive. */
  readonly start: number;
  readonly end: number;
}

export interface UnresolvedReferenceResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly imports: readonly PackageImport[];
  readonly exports: readonly ProviderExport[];
  readonly mappings: readonly DeclarationSourceMapping[];
  readonly symbols: readonly ImportedSymbol[];
  readonly unresolved: readonly UnresolvedReference[];
}

/**
 * Resolve exact symbol edges and classify every import that fails to produce
 * one.
 *
 * A module-level failure is reported once for the import and suppresses the
 * per-name reasons of the same occurrence: an ambiguous provider or an
 * unresolved module makes every requested name meaningless.
 */
export async function resolveUnresolvedReferences(
  service: LanguageService,
  view: ProjectView,
  registry: PackageProviderRegistry,
  options: UnresolvedReferenceOptions = {},
): Promise<UnresolvedReferenceResolution> {
  service.assertFresh(view);
  const resolution = await resolveImportedSymbols(
    service,
    view,
    registry,
    options,
  );

  const conflicts = new Map<string, PackageProviderConflict>();
  for (const conflict of options.conflicts ?? []) {
    if (
      !conflicts.has(conflict.packageName) ||
      conflict.kind === "AMBIGUOUS_PACKAGE_PROVIDER"
    ) {
      conflicts.set(conflict.packageName, conflict);
    }
  }

  const mappings = new Map(
    resolution.mappings.map((mapping) => [mapping.declarationFile, mapping]),
  );
  const brokenModules = await collectBrokenModules(view, resolution.imports);
  const modulesWithEdges = new Set(
    resolution.symbols.map((symbol) =>
      specifierKey(symbol.consumer.fileName, symbol.specifier),
    ),
  );

  const unresolved: UnresolvedReference[] = [];
  const failedImports = new Set<string>();
  for (const packageImport of resolution.imports) {
    const reason = classifyImport(
      packageImport,
      conflicts,
      brokenModules,
      modulesWithEdges,
    );
    if (reason === undefined) {
      continue;
    }
    failedImports.add(importKey(packageImport));
    unresolved.push({
      fileName: packageImport.fileName,
      specifier: packageImport.specifier,
      packageName: packageImport.packageName,
      requestedSymbol: undefined,
      reason: reason.reason,
      provider: packageImport.provider,
      detail: reason.detail,
      start: packageImport.start,
      end: packageImport.end,
    });
  }

  const importsBySpecifier = new Map<string, PackageImport>();
  for (const packageImport of resolution.imports) {
    const key = specifierKey(packageImport.fileName, packageImport.specifier);
    if (!importsBySpecifier.has(key)) {
      importsBySpecifier.set(key, packageImport);
    }
  }

  for (const entry of resolution.exports) {
    const packageImport = importsBySpecifier.get(
      specifierKey(entry.fileName, entry.specifier),
    );
    if (
      packageImport === undefined ||
      failedImports.has(importKey(packageImport))
    ) {
      continue;
    }
    if (entry.status !== "RESOLVED") {
      unresolved.push({
        fileName: entry.fileName,
        specifier: entry.specifier,
        packageName: entry.packageName,
        requestedSymbol: entry.exportedName,
        reason: "EXPORT_NOT_FOUND",
        provider: packageImport.provider,
        detail: entry.status,
        start: packageImport.start,
        end: packageImport.end,
      });
      continue;
    }
    const unmapped = entry.targetFiles.filter(
      (targetFile) =>
        isDeclarationFile(targetFile) &&
        (mappings.get(targetFile)?.sourceFiles.length ?? 0) === 0,
    );
    if (unmapped.length > 0 && unmapped.length === entry.targetFiles.length) {
      unresolved.push({
        fileName: entry.fileName,
        specifier: entry.specifier,
        packageName: entry.packageName,
        requestedSymbol: entry.exportedName,
        reason: "DECLARATION_SOURCE_NOT_MAPPED",
        provider: packageImport.provider,
        detail: unmapped.join(", "),
        start: packageImport.start,
        end: packageImport.end,
      });
    }
  }

  service.assertFresh(view);
  unresolved.sort(compareUnresolved);
  return {
    generation: resolution.generation,
    configFileName: resolution.configFileName,
    imports: resolution.imports,
    exports: resolution.exports,
    mappings: resolution.mappings,
    // A conflicted package has no proven provider identity, so an edge built
    // from it would be a false exact edge.
    symbols: resolution.symbols.filter(
      (symbol) => !conflicts.has(symbol.packageName),
    ),
    unresolved,
  };
}

function classifyImport(
  packageImport: PackageImport,
  conflicts: ReadonlyMap<string, PackageProviderConflict>,
  brokenModules: ReadonlyMap<string, string>,
  modulesWithEdges: ReadonlySet<string>,
):
  | { reason: UnresolvedReferenceReason; detail: string | undefined }
  | undefined {
  const conflict = conflicts.get(packageImport.packageName);
  if (conflict !== undefined) {
    return {
      reason:
        conflict.kind === "AMBIGUOUS_PACKAGE_PROVIDER"
          ? "AMBIGUOUS_PACKAGE_PROVIDER"
          : "VERSION_MISMATCH",
      detail: conflictDetail(conflict),
    };
  }
  if (packageImport.status === "PACKAGE_PROVIDER_NOT_FOUND") {
    return { reason: "PACKAGE_PROVIDER_NOT_FOUND", detail: undefined };
  }
  if (packageImport.status === "MODULE_NOT_RESOLVED") {
    return { reason: "MODULE_NOT_RESOLVED", detail: undefined };
  }
  const broken = packageImport.resolvedFiles
    .map((resolvedFile) => brokenModules.get(resolvedFile))
    .find((detail) => detail !== undefined);
  if (
    broken !== undefined &&
    !modulesWithEdges.has(
      specifierKey(packageImport.fileName, packageImport.specifier),
    )
  ) {
    return { reason: "TYPECHECK_FAILED", detail: broken };
  }
  return undefined;
}

function conflictDetail(conflict: PackageProviderConflict): string | undefined {
  const parts: string[] = [];
  if (conflict.repositories !== undefined && conflict.repositories.length > 0) {
    parts.push(`repositories: ${[...conflict.repositories].sort().join(", ")}`);
  }
  if (conflict.versions !== undefined && conflict.versions.length > 0) {
    parts.push(`versions: ${[...conflict.versions].sort().join(", ")}`);
  }
  return parts.length === 0 ? undefined : parts.join("; ");
}

/**
 * Map each resolved provider module to its first semantic error.
 *
 * A provider whose declarations do not typecheck cannot back a trustworthy
 * symbol: the checker resolved the module, but its types are broken.
 */
async function collectBrokenModules(
  view: ProjectView,
  imports: readonly PackageImport[],
): Promise<Map<string, string>> {
  const moduleFiles = [
    ...new Set(
      imports
        .filter((packageImport) => packageImport.status === "RESOLVED")
        .flatMap((packageImport) => [...packageImport.resolvedFiles]),
    ),
  ].sort();
  const broken = new Map<string, string>();
  for (const fileName of moduleFiles) {
    const diagnostics = await view.program.getSemanticDiagnostics(fileName);
    const error = diagnostics.find(
      (diagnostic) => diagnostic.category === DiagnosticCategory.Error,
    );
    if (error !== undefined) {
      broken.set(fileName, `TS${error.code}: ${error.text}`);
    }
  }
  return broken;
}

function isDeclarationFile(fileName: string): boolean {
  return /\.d\.(?:ts|mts|cts)$/.test(fileName);
}

function importKey(packageImport: PackageImport): string {
  return `${packageImport.fileName}\u0000${packageImport.start}\u0000${packageImport.end}`;
}

function specifierKey(fileName: string, specifier: string): string {
  return `${fileName}\u0000${specifier}`;
}

function compareUnresolved(
  left: UnresolvedReference,
  right: UnresolvedReference,
): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.start - right.start ||
    left.reason.localeCompare(right.reason) ||
    (left.requestedSymbol ?? "").localeCompare(right.requestedSymbol ?? "")
  );
}
