/**
 * Exact `IMPORTS_SYMBOL` edges between a consumer binding and the symbol the
 * provider really declares.
 *
 * Every edge is produced by the native checker: the module specifier is
 * resolved by TypeScript, the local binding is resolved to its alias symbol,
 * and the alias is followed to the declaration it targets. A name that spells
 * the same in two packages never produces an edge.
 */

import {
  isExportDeclaration,
  isImportDeclaration,
  isNamedExports,
  isNamedImports,
  isStringLiteral,
} from "typescript/unstable/ast/is";
import { SymbolFlags } from "typescript/unstable/async";
import type { Node, SourceFile } from "typescript/unstable/ast";
import type {
  Checker,
  NodeHandle,
  Symbol as TypeScriptSymbol,
} from "typescript/unstable/async";

import type {
  DeclarationSourceMapping,
  DeclarationSourceStatus,
} from "./declaration-source-resolver.js";
import { resolveDeclarationSources } from "./declaration-source-resolver.js";
import type { LanguageService, ProjectView } from "./language-service.js";
import type {
  PackageImport,
  PackageImportResolutionOptions,
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import { resolveProviderExports } from "./provider-export-resolver.js";
import type { ProviderExport } from "./provider-export-resolver.js";

/** The import binding that consumes a provider symbol. */
export interface ImportedSymbolConsumer {
  readonly fileName: string;
  readonly symbolId: number;
  /** Local name introduced in the consumer module. */
  readonly name: string;
  /** UTF-16 source offsets, with `end` exclusive. */
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  readonly endLine: number;
}

/** One declaration site of the provider symbol. */
export interface ImportedSymbolDeclaration {
  readonly fileName: string;
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  readonly endLine: number;
  /** Source files mapped by LUQUE-0703 for a declaration artifact. */
  readonly sourceFiles: readonly string[];
  readonly sourceStatus: DeclarationSourceStatus;
}

/** The provider symbol the consumer binding resolves to. */
export interface ImportedSymbolTarget {
  readonly symbolId: number;
  readonly name: string;
  readonly declarations: readonly ImportedSymbolDeclaration[];
}

/** One exact symbol-level dependency across packages. */
export interface ImportedSymbol {
  readonly kind: "IMPORTS_SYMBOL";
  readonly packageName: string;
  readonly specifier: string;
  readonly provider: PackageProvider;
  /** Public name requested from the provider module. */
  readonly exportedName: string;
  readonly consumer: ImportedSymbolConsumer;
  readonly target: ImportedSymbolTarget;
}

export interface ImportedSymbolResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly imports: readonly PackageImport[];
  readonly exports: readonly ProviderExport[];
  readonly mappings: readonly DeclarationSourceMapping[];
  readonly symbols: readonly ImportedSymbol[];
}

interface BindingRequest {
  file: SourceFile;
  nameNode: Node;
  localName: string;
  exportedName: string;
  packageImport: PackageImport;
}

/**
 * Resolve exact `IMPORTS_SYMBOL` edges for one live project view.
 *
 * The resolution reuses LUQUE-0701 module resolution, LUQUE-0702 provider
 * exports and the LUQUE-0703 declaration-to-source bridge, and adds the
 * symbol-level link the graph stores.
 */
export async function resolveImportedSymbols(
  service: LanguageService,
  view: ProjectView,
  registry: PackageProviderRegistry,
  options: PackageImportResolutionOptions = {},
): Promise<ImportedSymbolResolution> {
  service.assertFresh(view);
  const providerExports = await resolveProviderExports(
    service,
    view,
    registry,
    options,
  );
  const declarationSources = await resolveDeclarationSources(
    service,
    view,
    providerExports,
  );
  const mappingsByFile = new Map(
    declarationSources.mappings.map((mapping) => [
      mapping.declarationFile,
      mapping,
    ]),
  );

  const importsByLocation = new Map<string, PackageImport>();
  for (const packageImport of providerExports.imports) {
    if (packageImport.status === "RESOLVED") {
      importsByLocation.set(
        specifierKey(packageImport.fileName, packageImport.start),
        packageImport,
      );
    }
  }

  const requests: BindingRequest[] = [];
  for (const fileName of [
    ...new Set(
      providerExports.imports
        .filter((packageImport) => packageImport.status === "RESOLVED")
        .map((packageImport) => packageImport.fileName),
    ),
  ].sort()) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (sourceFile === undefined) {
      continue;
    }
    collectBindings(sourceFile, importsByLocation, requests);
  }

  const aliasSymbols =
    requests.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(
          requests.map((request) => request.nameNode),
        );

  const symbols: ImportedSymbol[] = [];
  for (const [index, request] of requests.entries()) {
    const alias = aliasSymbols[index];
    if (alias === undefined) {
      continue;
    }
    const target = await resolveAliasTarget(view.checker, alias);
    if (target === undefined || target.declarations.length === 0) {
      continue;
    }
    const provider = request.packageImport.provider;
    if (provider === undefined) {
      continue;
    }
    const declarations = await resolveDeclarations(
      view,
      target.declarations,
      mappingsByFile,
    );
    if (declarations.length === 0) {
      continue;
    }
    const start = request.nameNode.getStart(request.file);
    const end = request.nameNode.getEnd();
    symbols.push({
      kind: "IMPORTS_SYMBOL",
      packageName: request.packageImport.packageName,
      specifier: request.packageImport.specifier,
      provider,
      exportedName: request.exportedName,
      consumer: {
        fileName: request.file.fileName,
        symbolId: alias.id,
        name: request.localName,
        start,
        end,
        startLine: request.file.getLineAndCharacterOfPosition(start).line + 1,
        endLine:
          request.file.getLineAndCharacterOfPosition(Math.max(start, end - 1))
            .line + 1,
      },
      target: {
        symbolId: target.id,
        name: target.name,
        declarations,
      },
    });
  }

  service.assertFresh(view);
  symbols.sort(compareImportedSymbols);
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    imports: providerExports.imports,
    exports: providerExports.exports,
    mappings: declarationSources.mappings,
    symbols,
  };
}

function collectBindings(
  file: SourceFile,
  importsByLocation: ReadonlyMap<string, PackageImport>,
  requests: BindingRequest[],
): void {
  const visit = (node: Node): void => {
    if (isImportDeclaration(node)) {
      const packageImport = importFor(
        file,
        node.moduleSpecifier,
        importsByLocation,
      );
      const clause = node.importClause;
      if (packageImport !== undefined && clause !== undefined) {
        if (clause.name !== undefined) {
          requests.push({
            file,
            nameNode: clause.name,
            localName: bindingName(clause.name),
            exportedName: "default",
            packageImport,
          });
        }
        if (
          clause.namedBindings !== undefined &&
          isNamedImports(clause.namedBindings)
        ) {
          for (const element of clause.namedBindings.elements) {
            requests.push({
              file,
              nameNode: element.name,
              localName: bindingName(element.name),
              exportedName: bindingName(element.propertyName ?? element.name),
              packageImport,
            });
          }
        }
      }
    } else if (
      isExportDeclaration(node) &&
      node.moduleSpecifier !== undefined &&
      node.exportClause !== undefined &&
      isNamedExports(node.exportClause)
    ) {
      const packageImport = importFor(
        file,
        node.moduleSpecifier,
        importsByLocation,
      );
      if (packageImport !== undefined) {
        for (const element of node.exportClause.elements) {
          requests.push({
            file,
            nameNode: element.name,
            localName: bindingName(element.name),
            exportedName: bindingName(element.propertyName ?? element.name),
            packageImport,
          });
        }
      }
    }
    node.forEachChild(visit);
  };

  file.forEachChild(visit);
}

function importFor(
  file: SourceFile,
  moduleSpecifier: Node,
  importsByLocation: ReadonlyMap<string, PackageImport>,
): PackageImport | undefined {
  return importsByLocation.get(
    specifierKey(file.fileName, moduleSpecifier.getStart(file)),
  );
}

function specifierKey(fileName: string, start: number): string {
  return `${fileName}\u0000${start}`;
}

function bindingName(node: Node): string {
  return isStringLiteral(node) ? node.text : node.getText();
}

async function resolveAliasTarget(
  checker: Checker,
  symbol: TypeScriptSymbol,
): Promise<TypeScriptSymbol | undefined> {
  if ((symbol.flags & SymbolFlags.Alias) === 0) {
    return symbol;
  }
  const target = await checker.getAliasedSymbol(symbol);
  return (await checker.isUnknownSymbol(target)) ? undefined : target;
}

async function resolveDeclarations(
  view: ProjectView,
  handles: readonly NodeHandle[],
  mappingsByFile: ReadonlyMap<string, DeclarationSourceMapping>,
): Promise<ImportedSymbolDeclaration[]> {
  const declarations = await Promise.all(
    handles.map(
      async (handle): Promise<ImportedSymbolDeclaration | undefined> => {
        const sourceFile = await view.program.getSourceFile(handle.path);
        const node = await handle.resolve(view.project);
        if (sourceFile === undefined || node === undefined) {
          return undefined;
        }
        const start = node.getStart(sourceFile);
        const end = node.getEnd();
        const mapping = mappingsByFile.get(handle.path);
        return {
          fileName: handle.path,
          start,
          end,
          startLine: sourceFile.getLineAndCharacterOfPosition(start).line + 1,
          endLine:
            sourceFile.getLineAndCharacterOfPosition(Math.max(start, end - 1))
              .line + 1,
          sourceFiles: mapping?.sourceFiles ?? [],
          sourceStatus: mapping?.status ?? "UNRESOLVED",
        };
      },
    ),
  );
  return declarations
    .filter(
      (declaration): declaration is ImportedSymbolDeclaration =>
        declaration !== undefined,
    )
    .sort(
      (left, right) =>
        left.fileName.localeCompare(right.fileName) ||
        left.start - right.start ||
        left.end - right.end,
    );
}

function compareImportedSymbols(
  left: ImportedSymbol,
  right: ImportedSymbol,
): number {
  return (
    left.consumer.fileName.localeCompare(right.consumer.fileName) ||
    left.consumer.start - right.consumer.start ||
    left.consumer.end - right.consumer.end ||
    left.exportedName.localeCompare(right.exportedName)
  );
}
