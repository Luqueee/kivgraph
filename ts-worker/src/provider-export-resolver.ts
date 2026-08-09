import { SymbolFlags } from "typescript/unstable/async";
import type {
  Checker,
  Symbol as TypeScriptSymbol,
} from "typescript/unstable/async";

import type { LanguageService, ProjectView } from "./language-service.js";
import {
  resolvePackageImports,
  type PackageImport,
  type PackageImportResolutionOptions,
  type PackageProviderRegistry,
} from "./package-import-resolver.js";
import { enginePath } from "./engine-path.js";

export type ProviderExportStatus =
  | "RESOLVED"
  | "EXPORT_NOT_FOUND"
  | "MODULE_SYMBOL_NOT_FOUND";

/** One checker-backed export exposed by a resolved provider module. */
export interface ProviderExport {
  readonly fileName: string;
  readonly specifier: string;
  readonly packageName: string;
  readonly exportedName: string | undefined;
  readonly status: ProviderExportStatus;
  readonly targetName: string | undefined;
  readonly targetFiles: readonly string[];
}

export interface ProviderExportResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly imports: readonly PackageImport[];
  readonly exports: readonly ProviderExport[];
}

/**
 * Resolve the public exports requested from provider modules.
 *
 * TypeScript's checker performs module resolution, including package
 * `exports`, `types`, `typings`, `typesVersions`, `paths`, project references,
 * and the configured `moduleResolution`. This layer only asks the checker for
 * the requested members; it enumerates a module only for namespace imports or
 * export-star declarations.
 */
export async function resolveProviderExports(
  service: LanguageService,
  view: ProjectView,
  registry: PackageProviderRegistry,
  options: PackageImportResolutionOptions = {},
): Promise<ProviderExportResolution> {
  service.assertFresh(view);
  const imports = await resolvePackageImports(service, view, registry, options);
  const exports: ProviderExport[] = [];

  for (const packageImport of imports.imports) {
    if (packageImport.status !== "RESOLVED") {
      continue;
    }
    const moduleSymbol = await findModuleSymbol(view, packageImport);
    if (moduleSymbol === undefined) {
      appendMissingModuleExports(packageImport, exports);
      continue;
    }
    await appendProviderExports(
      view.checker,
      moduleSymbol,
      packageImport,
      exports,
    );
  }

  service.assertFresh(view);
  exports.sort(compareProviderExports);
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    imports: imports.imports,
    exports,
  };
}

async function findModuleSymbol(
  view: ProjectView,
  packageImport: PackageImport,
): Promise<TypeScriptSymbol | undefined> {
  for (const fileName of packageImport.resolvedFiles) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (sourceFile === undefined) {
      continue;
    }
    const symbol = await view.checker.getSymbolAtLocation(sourceFile);
    if (symbol !== undefined) {
      return symbol;
    }
  }
  return undefined;
}

function appendMissingModuleExports(
  packageImport: PackageImport,
  output: ProviderExport[],
): void {
  const names = selectedExportNames(packageImport, []);
  for (const exportedName of names) {
    output.push({
      fileName: packageImport.fileName,
      specifier: packageImport.specifier,
      packageName: packageImport.packageName,
      exportedName,
      status: "MODULE_SYMBOL_NOT_FOUND",
      targetName: undefined,
      targetFiles: [],
    });
  }
}

async function appendProviderExports(
  checker: Checker,
  moduleSymbol: TypeScriptSymbol,
  packageImport: PackageImport,
  output: ProviderExport[],
): Promise<void> {
  const allSymbols =
    packageImport.exportMode === "NONE" || packageImport.exportMode === "NAMED"
      ? []
      : await checker.getExportsOfModule(moduleSymbol);
  const selectedNames = selectedExportNames(
    packageImport,
    allSymbols.map((symbol) => symbol.name),
  );
  for (const exportedName of selectedNames) {
    const symbol =
      packageImport.exportMode === "NAMED"
        ? await checker.getMemberInModuleExports(moduleSymbol, exportedName)
        : allSymbols.find((candidate) => candidate.name === exportedName);
    if (symbol === undefined || (await checker.isUnknownSymbol(symbol))) {
      output.push({
        fileName: packageImport.fileName,
        specifier: packageImport.specifier,
        packageName: packageImport.packageName,
        exportedName,
        status: "EXPORT_NOT_FOUND",
        targetName: undefined,
        targetFiles: [],
      });
      continue;
    }
    const target = await resolveAlias(checker, symbol);
    output.push({
      fileName: packageImport.fileName,
      specifier: packageImport.specifier,
      packageName: packageImport.packageName,
      exportedName,
      status: "RESOLVED",
      targetName: target?.name ?? symbol.name,
      targetFiles: declarationFiles(target ?? symbol),
    });
  }
}

function selectedExportNames(
  packageImport: PackageImport,
  allNames: readonly string[],
): string[] {
  switch (packageImport.exportMode) {
    case "NAMED":
      return unique(packageImport.requestedExports);
    case "STAR":
      return unique(allNames.filter((name) => name !== "default"));
    case "NAMESPACE":
      return unique(allNames);
    case "NONE":
      return [];
  }
}

async function resolveAlias(
  checker: Checker,
  symbol: TypeScriptSymbol,
): Promise<TypeScriptSymbol | undefined> {
  if ((symbol.flags & SymbolFlags.Alias) === 0) {
    return symbol;
  }
  const target = await checker.getAliasedSymbol(symbol);
  return (await checker.isUnknownSymbol(target)) ? undefined : target;
}

function declarationFiles(symbol: TypeScriptSymbol): string[] {
  return [
    ...new Set(
      // A declaration the engine resolved carries its canonical casing, which
      // folds to lower case on macOS. Every later comparison - provider roots,
      // declaration-map indexes, emitted evidence - is against paths as they
      // are spelled on disk, so the correction belongs here, at the boundary.
      symbol.declarations.map((declaration) => enginePath(declaration.path)),
    ),
  ].sort();
}

function unique(names: readonly string[]): string[] {
  return [...new Set(names)];
}

function compareProviderExports(
  left: ProviderExport,
  right: ProviderExport,
): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.specifier.localeCompare(right.specifier) ||
    (left.exportedName ?? "").localeCompare(right.exportedName ?? "")
  );
}
