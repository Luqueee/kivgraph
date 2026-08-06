/**
 * Exact `IMPORTS_SYMBOL` edges between a consumer binding and the symbol the
 * provider really declares.
 *
 * Every edge is produced by the native checker: the module specifier is
 * resolved by TypeScript, the local binding is resolved to its alias symbol,
 * and the alias is followed to the declaration it targets. A name that spells
 * the same in two packages never produces an edge.
 *
 * LUQUE-0907 adds the provider's own identity to the target. The durable key
 * a consumer would derive for a symbol must be byte-identical to the one the
 * provider assigns its own declaration, or the edge dangles. That identity is
 * computed by reading the provider's source at the exact position LUQUE-0703
 * mapped and classifying it with the same functions the provider runs on
 * itself (`declaration-classifier.ts`) — never inferred from the `.d.ts`
 * text, which the provider itself never classifies against.
 */

import path from "node:path";

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

import {
  classifyDeclarationAt,
  compactSignature,
  type LocalSymbolKind,
} from "./declaration-classifier.js";
import { declarationName } from "./declaration-name.js";
import {
  DeclarationPositionMapper,
  type SourcePosition,
} from "./declaration-position-mapper.js";
import type {
  DeclarationSourceMapping,
  DeclarationSourceStatus,
} from "./declaration-source-resolver.js";
import { resolveDeclarationSources } from "./declaration-source-resolver.js";
import { LanguageService, type ProjectView } from "./language-service.js";
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
  /** Source text of the binding: the specifier, or the identifier alone. */
  readonly text: string;
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
  /**
   * Exact position in the source file, decoded from the declaration map.
   * Undefined when the artifact has no map or no segment covers the symbol.
   */
  readonly sourcePosition: SourcePosition | undefined;
}

/** Why `ImportedSymbolTarget.identity` could not be computed. */
export type ImportedSymbolIdentityReason =
  | "PROVIDER_SOURCE_UNAVAILABLE"
  | "PROVIDER_DECLARATION_NOT_FOUND";

/**
 * The provider declaration, described exactly as the provider indexes it.
 *
 * `repository` and `package` come straight from the registry-supplied
 * `PackageProvider`; `qualifiedName`, `kind` and `signature` are computed by
 * parsing the provider's own source and classifying the node with
 * `declaration-classifier.ts` — the same code path the provider runs when it
 * indexes itself, over the same bytes.
 */
export interface ImportedSymbolIdentity {
  readonly repository: string;
  readonly package: string;
  readonly qualifiedName: string;
  readonly kind: LocalSymbolKind;
  readonly signature: string;
  /** Provider source file, relative to the provider repository root. */
  readonly file: string;
  readonly startLine: number;
}

/** The provider symbol the consumer binding resolves to. */
export interface ImportedSymbolTarget {
  readonly symbolId: number;
  readonly name: string;
  readonly declarations: readonly ImportedSymbolDeclaration[];
  /**
   * The provider's own identity for this declaration, or undefined when it
   * cannot be reproduced exactly. Never a guess: a consumer that cannot
   * prove the identity reports none, rather than risk a false edge.
   */
  readonly identity: ImportedSymbolIdentity | undefined;
  /** Why `identity` is undefined. Undefined when `identity` is set. */
  readonly identityReason: ImportedSymbolIdentityReason | undefined;
  readonly identityDetail: string | undefined;
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
  text: string;
  exportedName: string;
  packageImport: PackageImport;
}

/** A resolved edge, still missing the provider identity of its target. */
interface PendingSymbol {
  packageName: string;
  specifier: string;
  provider: PackageProvider;
  exportedName: string;
  consumer: ImportedSymbolConsumer;
  targetSymbolId: number;
  targetName: string;
  declarations: ImportedSymbolDeclaration[];
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
  const mappers = new Map<
    string,
    Promise<DeclarationPositionMapper | undefined>
  >();

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

  const pending: PendingSymbol[] = [];
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
      mappers,
    );
    if (declarations.length === 0) {
      continue;
    }
    const start = request.nameNode.getStart(request.file);
    const end = request.nameNode.getEnd();
    pending.push({
      packageName: request.packageImport.packageName,
      specifier: request.packageImport.specifier,
      provider,
      exportedName: request.exportedName,
      consumer: {
        fileName: request.file.fileName,
        symbolId: alias.id,
        name: request.localName,
        text: request.text,
        start,
        end,
        startLine: request.file.getLineAndCharacterOfPosition(start).line + 1,
        endLine:
          request.file.getLineAndCharacterOfPosition(Math.max(start, end - 1))
            .line + 1,
      },
      targetSymbolId: target.id,
      targetName: target.name,
      declarations,
    });
  }

  const identities = await resolveTargetIdentities(pending);
  const symbols: ImportedSymbol[] = pending.map(
    (entry, index): ImportedSymbol => ({
      kind: "IMPORTS_SYMBOL",
      packageName: entry.packageName,
      specifier: entry.specifier,
      provider: entry.provider,
      exportedName: entry.exportedName,
      consumer: entry.consumer,
      target: {
        symbolId: entry.targetSymbolId,
        name: entry.targetName,
        declarations: entry.declarations,
        ...(identities[index] ?? IDENTITY_NOT_ATTEMPTED),
      },
    }),
  );

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
            text: clause.name.getText(file),
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
              text: element.getText(file),
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
            text: element.getText(file),
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
  mappers: Map<string, Promise<DeclarationPositionMapper | undefined>>,
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
        const startPosition = sourceFile.getLineAndCharacterOfPosition(start);
        // The declaration map is queried at the declared name, not at the
        // statement start, so the source position points at the symbol.
        const nameNode = declarationName(node);
        const namePosition =
          nameNode === undefined
            ? startPosition
            : sourceFile.getLineAndCharacterOfPosition(
                nameNode.getStart(sourceFile),
              );
        return {
          fileName: handle.path,
          start,
          end,
          startLine: startPosition.line + 1,
          endLine:
            sourceFile.getLineAndCharacterOfPosition(Math.max(start, end - 1))
              .line + 1,
          sourceFiles: mapping?.sourceFiles ?? [],
          sourceStatus: mapping?.status ?? "UNRESOLVED",
          sourcePosition: await lookupSourcePosition(
            mappers,
            handle.path,
            namePosition,
          ),
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

/**
 * Resolve one declaration position through its declaration map.
 *
 * Mappers are cached per declaration file: a provider barrel is asked for
 * dozens of symbols and the map is parsed once.
 */
async function lookupSourcePosition(
  mappers: Map<string, Promise<DeclarationPositionMapper | undefined>>,
  declarationFile: string,
  position: { line: number; character: number },
): Promise<SourcePosition | undefined> {
  let mapper = mappers.get(declarationFile);
  if (mapper === undefined) {
    mapper = DeclarationPositionMapper.create(declarationFile);
    mappers.set(declarationFile, mapper);
  }
  return (await mapper)?.lookup(position.line + 1, position.character);
}

/** The three `ImportedSymbolTarget` fields an identity lookup produces. */
type TargetIdentityOutcome = Pick<
  ImportedSymbolTarget,
  "identity" | "identityReason" | "identityDetail"
>;

const IDENTITY_NOT_ATTEMPTED: TargetIdentityOutcome = identityUnresolved(
  "PROVIDER_SOURCE_UNAVAILABLE",
  "no declaration map places this symbol in the provider's source",
);

function identityUnresolved(
  reason: ImportedSymbolIdentityReason,
  detail: string,
): TargetIdentityOutcome {
  return {
    identity: undefined,
    identityReason: reason,
    identityDetail: detail,
  };
}

/** One target whose provider identity still needs to be classified. */
interface IdentityRequest {
  readonly index: number;
  readonly provider: PackageProvider;
  readonly declaration: ImportedSymbolDeclaration;
}

/**
 * Compute the provider identity of every pending target that has an exact
 * source position, grouped by provider project so each project opens once.
 *
 * Opening the provider's own project has real cost, so it only happens for a
 * target a declaration map actually places: everything else keeps the
 * default "not attempted" outcome without spawning anything.
 */
async function resolveTargetIdentities(
  pending: readonly PendingSymbol[],
): Promise<TargetIdentityOutcome[]> {
  const outcomes: TargetIdentityOutcome[] = pending.map(
    () => IDENTITY_NOT_ATTEMPTED,
  );
  const byProject = new Map<string, IdentityRequest[]>();

  for (const [index, entry] of pending.entries()) {
    const declaration = entry.declarations.find(
      (candidate) =>
        candidate.sourceStatus === "DECLARATION_MAP" &&
        candidate.sourcePosition !== undefined,
    );
    if (declaration === undefined) {
      continue;
    }
    if (entry.provider.projectPath === undefined) {
      outcomes[index] = identityUnresolved(
        "PROVIDER_SOURCE_UNAVAILABLE",
        `provider ${entry.provider.repository} declares no project of its own`,
      );
      continue;
    }
    const projectPath = path.resolve(entry.provider.projectPath);
    const request: IdentityRequest = {
      index,
      provider: entry.provider,
      declaration,
    };
    const group = byProject.get(projectPath);
    if (group === undefined) {
      byProject.set(projectPath, [request]);
    } else {
      group.push(request);
    }
  }

  for (const [projectPath, group] of [...byProject.entries()].sort(
    ([left], [right]) => left.localeCompare(right),
  )) {
    const providerService = LanguageService.create({
      cwd: path.dirname(projectPath),
    });
    try {
      await providerService.openProject(projectPath);
      const providerView = providerService.project(projectPath);
      for (const request of group) {
        outcomes[request.index] = await classifyTarget(providerView, request);
      }
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      for (const request of group) {
        outcomes[request.index] = identityUnresolved(
          "PROVIDER_SOURCE_UNAVAILABLE",
          detail,
        );
      }
    } finally {
      await providerService.close();
    }
  }

  return outcomes;
}

/**
 * Classify one target in the provider's own, already-open project.
 *
 * The provider source is parsed and walked with the exact same functions
 * `symbol-extractor.ts` uses to index that repository, so the qualified
 * name, kind and signature this produces are identical to what the provider
 * would report for itself — by construction, not by coincidence.
 */
async function classifyTarget(
  providerView: ProjectView,
  request: IdentityRequest,
): Promise<TargetIdentityOutcome> {
  // The request was only queued for a declaration with a source position;
  // this guards the type, not a real branch.
  const position = request.declaration.sourcePosition;
  if (position === undefined) {
    return identityUnresolved(
      "PROVIDER_SOURCE_UNAVAILABLE",
      "declaration map segment has no source position",
    );
  }
  const sourceFile = await providerView.program.getSourceFile(
    position.fileName,
  );
  if (sourceFile === undefined) {
    return identityUnresolved(
      "PROVIDER_SOURCE_UNAVAILABLE",
      `${position.fileName} is not part of the provider's own project`,
    );
  }
  const offset = sourceFile.getPositionOfLineAndCharacter(
    position.line - 1,
    position.character,
  );
  const candidate = classifyDeclarationAt(sourceFile, offset);
  if (candidate === undefined) {
    return identityUnresolved(
      "PROVIDER_DECLARATION_NOT_FOUND",
      `no declaration at ${position.fileName}:${position.line}:${position.character}`,
    );
  }
  const declarationStart = candidate.declaration.getStart(sourceFile);
  return {
    identity: {
      repository: request.provider.repository,
      package: request.provider.name,
      qualifiedName: [...candidate.scope, candidate.name].join("."),
      kind: candidate.kind,
      signature: compactSignature(candidate.declaration, sourceFile),
      file: path
        .relative(request.provider.rootPath, sourceFile.fileName)
        .split(path.sep)
        .join("/"),
      startLine:
        sourceFile.getLineAndCharacterOfPosition(declarationStart).line + 1,
    },
    identityReason: undefined,
    identityDetail: undefined,
  };
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
