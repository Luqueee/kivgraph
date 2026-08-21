/**
 * Exact `IMPORTS_SYMBOL` and `REEXPORTS` edges between one repository and the
 * symbol another provider declares.
 *
 * Every edge is produced by the native checker: the module specifier is
 * resolved by TypeScript, the binding is resolved to its alias symbol, and
 * the alias is followed to the declaration it targets. A name that spells
 * the same in two packages never produces an edge.
 *
 * Two shapes of binding produce this pair of edge kinds:
 *
 *  - An import (`import { x } from "pkg"`, a default import, or a namespace
 *    member access `ns.x`) consumes the provider symbol into this file's own
 *    scope and becomes `IMPORTS_SYMBOL`, anchored at the binding itself.
 *  - A re-export with a `from` clause (`export { x } from "pkg"`, or
 *    `export * from "pkg"`) exposes the provider symbol under a public name
 *    of this module and becomes `REEXPORTS`, anchored at the export site.
 *    A `from` clause is what makes it a re-export, never whether the target
 *    happens to live in this repository: a same-repository
 *    `export { x } from "./y.js"` is `REEXPORTS` too, just resolved locally
 *    by `symbol-extractor.ts` instead of here, since a relative specifier
 *    never becomes a `PackageImport` in the first place.
 *
 * A namespace import (`import * as ns from "pkg"`) never itself produces an
 * edge — the binding names no concrete symbol — but each property access on
 * it (`ns.member`) does, exactly as if `member` had been imported by name:
 * the checker, never the object expression's spelling, decides whether an
 * access truly targets the namespace binding or some shadowing local of the
 * same name.
 *
 * LUQUE-0907 adds the provider's own identity to the target. The durable key
 * a consumer would derive for a symbol must be byte-identical to the one the
 * provider assigns its own declaration, or the edge dangles. That identity is
 * computed by reading the provider's source at the exact position LUQUE-0703
 * mapped and classifying it with the same functions the provider runs on
 * itself (`declaration-classifier.ts`) — never inferred from the `.d.ts`
 * text, which the provider itself never classifies against. `REEXPORTS`
 * targets reuse this exact machinery: a re-exported symbol reached across
 * repositories needs the same proof an import does, or it is an unresolved
 * reference, never a guessed edge.
 */

import path from "node:path";

import {
  isExportDeclaration,
  isIdentifier,
  isImportDeclaration,
  isNamedExports,
  isNamedImports,
  isNamespaceImport,
  isPropertyAccessExpression,
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
import { enginePath } from "./engine-path.js";
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
import { locateProviderExport } from "./provider-source-position-resolver.js";

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

/** The public name a re-export exposes, and the provider symbol it reaches. */
export interface ReexportedSymbolExport {
  readonly fileName: string;
  readonly symbolId: number;
  /** Public name this module exposes the provider symbol under. */
  readonly name: string;
  /** Source text of the export site: the specifier, or the whole star clause. */
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
  /** How the provider source position behind this identity was reached. */
  readonly source: ImportedSymbolIdentitySource;
}

/** The provider symbol a binding or a re-export resolves to. */
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

/** Fields shared by every exact cross-repository symbol edge. */
interface CrossRepositorySymbolEdge {
  readonly packageName: string;
  readonly specifier: string;
  readonly provider: PackageProvider;
  /** Public name requested from the provider module. */
  readonly exportedName: string;
  readonly target: ImportedSymbolTarget;
}

/** One exact symbol-level dependency across packages. */
export interface ImportedSymbol extends CrossRepositorySymbolEdge {
  readonly kind: "IMPORTS_SYMBOL";
  readonly consumer: ImportedSymbolConsumer;
}

/** One exact re-export of a provider symbol under a public name. */
export interface ReexportedSymbol extends CrossRepositorySymbolEdge {
  readonly kind: "REEXPORTS";
  readonly export: ReexportedSymbolExport;
}

export interface ImportedSymbolResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly imports: readonly PackageImport[];
  readonly exports: readonly ProviderExport[];
  readonly mappings: readonly DeclarationSourceMapping[];
  readonly symbols: readonly ImportedSymbol[];
  readonly reexports: readonly ReexportedSymbol[];
}

interface BindingRequest {
  origin: "import" | "export";
  file: SourceFile;
  nameNode: Node;
  localName: string;
  text: string;
  exportedName: string;
  packageImport: PackageImport;
}

/** A namespace import binding (`import * as ns from "pkg"`) in one file. */
interface NamespaceImportBinding {
  file: SourceFile;
  nameNode: Node;
  localName: string;
  packageImport: PackageImport;
}

/** A property access that might read a member off a namespace import. */
interface NamespaceMemberCandidate {
  file: SourceFile;
  node: Node;
  objectNode: Node;
  objectName: string;
  memberNode: Node;
}

/** A star re-export (`export * from "pkg"`) whose members are not yet known. */
interface StarReexportRequest {
  file: SourceFile;
  node: Node;
  moduleSpecifier: Node;
  packageImport: PackageImport;
}

/** A resolved edge, still missing the provider identity of its target. */
interface PendingSymbol {
  origin: "import" | "export";
  packageName: string;
  specifier: string;
  provider: PackageProvider;
  exportedName: string;
  binding: ImportedSymbolConsumer;
  targetSymbolId: number;
  targetName: string;
  declarations: ImportedSymbolDeclaration[];
}

/**
 * Resolve exact `IMPORTS_SYMBOL` and `REEXPORTS` edges for one live project
 * view.
 *
 * The resolution reuses LUQUE-0701 module resolution, LUQUE-0702 provider
 * exports and the LUQUE-0703 declaration-to-source bridge, and adds the
 * symbol-level link the graph stores. Both edge kinds share every stage past
 * "which binding, from which package, resolves to which alias" — only the
 * shape of the binding (a consumed import vs. an exposed public name)
 * differs, so they are computed together and split apart only at the end.
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
    registry,
  );
  // Both sides of this index are normalised to the casing on disk: the
  // mappings come from provider metadata the worker resolved itself, the
  // lookups from paths the engine canonicalised. Only a common form matches.
  const mappingsByFile = new Map(
    declarationSources.mappings.map((mapping) => [
      enginePath(mapping.declarationFile),
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
  const namespaceBindings: NamespaceImportBinding[] = [];
  const memberCandidates: NamespaceMemberCandidate[] = [];
  const starRequests: StarReexportRequest[] = [];
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
    collectBindings(
      sourceFile,
      importsByLocation,
      requests,
      namespaceBindings,
      memberCandidates,
      starRequests,
    );
  }

  // A namespace member (`ns.member`) has no declaration site of its own to
  // anchor on; the checker resolves whether the object expression it looks
  // like it reads from really is the namespace binding, never the object's
  // spelling alone. Both node sets are resolved in one batch: the binding's
  // own name (to know what it *is*) and every candidate's object expression
  // (to know what it resolves to at that specific use).
  const namespaceByKey = new Map<string, NamespaceImportBinding>();
  for (const binding of namespaceBindings) {
    namespaceByKey.set(
      `${binding.file.fileName}\u0000${binding.localName}`,
      binding,
    );
  }
  const namespaceCandidates = memberCandidates.filter((candidate) =>
    namespaceByKey.has(
      `${candidate.file.fileName}\u0000${candidate.objectName}`,
    ),
  );
  const namespaceCheckNodes = [
    ...namespaceBindings.map((binding) => binding.nameNode),
    ...namespaceCandidates.map((candidate) => candidate.objectNode),
  ];
  const namespaceCheckSymbols =
    namespaceCheckNodes.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(namespaceCheckNodes);
  const namespaceById = new Map<number, NamespaceImportBinding>();
  for (const [index, binding] of namespaceBindings.entries()) {
    const symbol = namespaceCheckSymbols[index];
    if (symbol !== undefined) {
      namespaceById.set(symbol.id, binding);
    }
  }
  const namespaceObjectSymbols = namespaceCheckSymbols.slice(
    namespaceBindings.length,
  );
  for (const [index, candidate] of namespaceCandidates.entries()) {
    const objectSymbol = namespaceObjectSymbols[index];
    if (objectSymbol === undefined) {
      continue;
    }
    const binding = namespaceById.get(objectSymbol.id);
    if (binding === undefined) {
      continue;
    }
    const memberName = bindingName(candidate.memberNode);
    requests.push({
      origin: "import",
      file: candidate.file,
      nameNode: candidate.memberNode,
      localName: memberName,
      exportedName: memberName,
      text: candidate.node.getText(candidate.file),
      packageImport: binding.packageImport,
    });
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
    const entry = await buildPendingSymbol(view, mappingsByFile, mappers, {
      origin: request.origin,
      packageImport: request.packageImport,
      exportedName: request.exportedName,
      file: request.file,
      node: request.nameNode,
      localName: request.localName,
      text: request.text,
      alias,
    });
    if (entry !== undefined) {
      pending.push(entry);
    }
  }

  // `export * from "pkg"` re-exports every member of the module; each member
  // already has its own checker symbol from `getExportsOfModule`, so it
  // skips straight to alias-following instead of a `getSymbolAtLocation`
  // round trip on a name that is never spelled in this file's own source.
  const starModuleSymbols =
    starRequests.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(
          starRequests.map((request) => request.moduleSpecifier),
        );
  const starMembers = (
    await Promise.all(
      starRequests.map(async (request, index) => {
        const moduleSymbol = starModuleSymbols[index];
        if (moduleSymbol === undefined) {
          return [];
        }
        const moduleExports =
          await view.checker.getExportsOfModule(moduleSymbol);
        return moduleExports
          .filter((symbol) => symbol.name !== "default")
          .map((symbol) => ({ request, symbol }));
      }),
    )
  ).flat();
  for (const { request, symbol } of starMembers) {
    const entry = await buildPendingSymbol(view, mappingsByFile, mappers, {
      origin: "export",
      packageImport: request.packageImport,
      exportedName: symbol.name,
      file: request.file,
      node: request.node,
      localName: symbol.name,
      text: request.node.getText(request.file),
      alias: symbol,
    });
    if (entry !== undefined) {
      pending.push(entry);
    }
  }

  const identities = await resolveTargetIdentities(pending, registry);
  const symbols: ImportedSymbol[] = [];
  const reexports: ReexportedSymbol[] = [];
  for (const [index, entry] of pending.entries()) {
    const target: ImportedSymbolTarget = {
      symbolId: entry.targetSymbolId,
      name: entry.targetName,
      declarations: entry.declarations,
      ...(identities[index] ?? IDENTITY_NOT_ATTEMPTED),
    };
    if (entry.origin === "import") {
      symbols.push({
        kind: "IMPORTS_SYMBOL",
        packageName: entry.packageName,
        specifier: entry.specifier,
        provider: entry.provider,
        exportedName: entry.exportedName,
        consumer: entry.binding,
        target,
      });
    } else {
      reexports.push({
        kind: "REEXPORTS",
        packageName: entry.packageName,
        specifier: entry.specifier,
        provider: entry.provider,
        exportedName: entry.exportedName,
        export: entry.binding,
        target,
      });
    }
  }

  service.assertFresh(view);
  symbols.sort(compareImportedSymbols);
  reexports.sort(compareReexportedSymbols);
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    imports: providerExports.imports,
    exports: providerExports.exports,
    mappings: declarationSources.mappings,
    symbols,
    reexports,
  };
}

function collectBindings(
  file: SourceFile,
  importsByLocation: ReadonlyMap<string, PackageImport>,
  requests: BindingRequest[],
  namespaceBindings: NamespaceImportBinding[],
  memberCandidates: NamespaceMemberCandidate[],
  starRequests: StarReexportRequest[],
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
            origin: "import",
            file,
            nameNode: clause.name,
            localName: bindingName(clause.name),
            exportedName: "default",
            text: clause.name.getText(file),
            packageImport,
          });
        }
        if (clause.namedBindings !== undefined) {
          if (isNamedImports(clause.namedBindings)) {
            for (const element of clause.namedBindings.elements) {
              requests.push({
                origin: "import",
                file,
                nameNode: element.name,
                localName: bindingName(element.name),
                exportedName: bindingName(element.propertyName ?? element.name),
                text: element.getText(file),
                packageImport,
              });
            }
          } else if (isNamespaceImport(clause.namedBindings)) {
            namespaceBindings.push({
              file,
              nameNode: clause.namedBindings.name,
              localName: bindingName(clause.namedBindings.name),
              packageImport,
            });
          }
        }
      }
    } else if (
      isExportDeclaration(node) &&
      node.moduleSpecifier !== undefined
    ) {
      const packageImport = importFor(
        file,
        node.moduleSpecifier,
        importsByLocation,
      );
      if (packageImport !== undefined) {
        if (
          node.exportClause !== undefined &&
          isNamedExports(node.exportClause)
        ) {
          for (const element of node.exportClause.elements) {
            requests.push({
              origin: "export",
              file,
              nameNode: element.name,
              localName: bindingName(element.name),
              exportedName: bindingName(element.propertyName ?? element.name),
              text: element.getText(file),
              packageImport,
            });
          }
        } else if (node.exportClause === undefined) {
          starRequests.push({
            file,
            node,
            moduleSpecifier: node.moduleSpecifier,
            packageImport,
          });
        }
      }
    } else if (
      isPropertyAccessExpression(node) &&
      isIdentifier(node.expression) &&
      isIdentifier(node.name)
    ) {
      memberCandidates.push({
        file,
        node,
        objectNode: node.expression,
        objectName: bindingName(node.expression),
        memberNode: node.name,
      });
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

/** One binding request whose alias has already been resolved to `alias`. */
interface PendingRequest {
  readonly origin: "import" | "export";
  readonly packageImport: PackageImport;
  readonly exportedName: string;
  /** File and anchor node the binding's position and text come from. */
  readonly file: SourceFile;
  readonly node: Node;
  readonly localName: string;
  readonly text: string;
  readonly alias: TypeScriptSymbol;
}

/**
 * Follow one already-resolved alias to its provider declaration and build
 * the pending edge, or drop the request when the alias leads nowhere exact.
 *
 * Shared by every binding shape this module resolves — named and default
 * imports, namespace members, named re-exports and star re-export members —
 * because all of them reduce to the same question once an alias symbol is in
 * hand: does it lead to a real declaration this consumer's provider owns?
 */
async function buildPendingSymbol(
  view: ProjectView,
  mappingsByFile: ReadonlyMap<string, DeclarationSourceMapping>,
  mappers: Map<string, Promise<DeclarationPositionMapper | undefined>>,
  request: PendingRequest,
): Promise<PendingSymbol | undefined> {
  const target = await resolveAliasTarget(view.checker, request.alias);
  if (target === undefined || target.declarations.length === 0) {
    return undefined;
  }
  const provider = request.packageImport.provider;
  if (provider === undefined) {
    return undefined;
  }
  const declarations = await resolveDeclarations(
    view,
    target.declarations,
    mappingsByFile,
    mappers,
  );
  if (declarations.length === 0) {
    return undefined;
  }
  const start = request.node.getStart(request.file);
  const end = request.node.getEnd();
  return {
    origin: request.origin,
    packageName: request.packageImport.packageName,
    specifier: request.packageImport.specifier,
    provider,
    exportedName: request.exportedName,
    binding: {
      fileName: request.file.fileName,
      symbolId: request.alias.id,
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
  };
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
        // The engine reports a module it resolved itself with its canonical
        // casing, which is lower case on a folding filesystem.
        const filePath = enginePath(handle.path);
        const start = node.getStart(sourceFile);
        const end = node.getEnd();
        const mapping = mappingsByFile.get(filePath);
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
          fileName: filePath,
          start,
          end,
          startLine: startPosition.line + 1,
          endLine:
            sourceFile.getLineAndCharacterOfPosition(Math.max(start, end - 1))
              .line + 1,
          sourceFiles: (mapping?.sourceFiles ?? []).map(enginePath),
          sourceStatus: mapping?.status ?? "UNRESOLVED",
          sourcePosition: await lookupSourcePosition(
            mappers,
            filePath,
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

/**
 * How the provider source position behind an identity was established.
 *
 * `DECLARATION_MAP` is the artifact's own `.d.ts.map` placing the symbol.
 * `PROVIDER_EXPORT` is the provider's checker naming the export inside a
 * source file its project roots already mapped the artifact to: the position
 * is exact, but the artifact-to-source step rests on the provider's build
 * configuration rather than on a map it emitted. Kivgraph grades the two
 * apart, so a consumer can tell which edges rest on which proof.
 */
export type ImportedSymbolIdentitySource =
  | "DECLARATION_MAP"
  | "PROVIDER_EXPORT";

/** One target whose provider identity still needs to be classified. */
interface IdentityRequest {
  readonly index: number;
  /** The package the consumer imported from. */
  readonly provider: PackageProvider;
  /**
   * The package that declares the code, and is credited with it.
   *
   * These differ whenever a package re-exports another's symbol, and again
   * whenever the artifact is an installed copy: the bridge names a file in
   * the owner's repository, and only the owner's project can parse it.
   * Crediting the importing package would compose an identity against a
   * repository that publishes no such symbol.
   */
  readonly owner: PackageProvider;
  readonly declaration: ImportedSymbolDeclaration;
  /** Public name requested from the provider module. */
  readonly exportedName: string;
  readonly source: ImportedSymbolIdentitySource;
}

/**
 * Compute the provider identity of every pending target some project can
 * place, grouped by project so each project opens once.
 *
 * A declaration map is the cheaper answer and wins when it exists. Without
 * one the provider still names its source through its project roots, and its
 * checker can say which declaration that source exports under the requested
 * name — the same answer, reached by asking the compiler that owns the code
 * instead of reading a map the build never emitted. Everything else keeps
 * the default "not attempted" outcome without spawning anything.
 *
 * The project that answers is the one that owns the mapped file, which is not
 * always the package the consumer imported from. A facade package exists to
 * re-export its workspace's symbols, so its map points into the repository
 * that declares them; asking the facade's own program for that file returns
 * nothing, and the target used to be abandoned as PROVIDER_SOURCE_UNAVAILABLE
 * even though the declaration was indexed, exact, and one directory away.
 */
async function resolveTargetIdentities(
  pending: readonly PendingSymbol[],
  registry: PackageProviderRegistry,
): Promise<TargetIdentityOutcome[]> {
  const outcomes: TargetIdentityOutcome[] = pending.map(
    () => IDENTITY_NOT_ATTEMPTED,
  );
  const byProject = new Map<string, IdentityRequest[]>();

  for (const [index, entry] of pending.entries()) {
    const mapped = entry.declarations.find(
      (candidate) =>
        candidate.sourceStatus === "DECLARATION_MAP" &&
        candidate.sourcePosition !== undefined,
    );
    const declaration =
      mapped ??
      entry.declarations.find((candidate) => candidate.sourceFiles.length > 0);
    if (declaration === undefined) {
      continue;
    }
    // Ownership is read off the file the bridge named, whichever bridge
    // named it. An installed copy is not owned by the consumer that
    // installed it nor by the package the import spelled: the source it was
    // re-rooted onto belongs to the repository that declares the package,
    // and only that repository publishes a key for the symbol.
    const ownedFile =
      mapped?.sourcePosition?.fileName ?? declaration.sourceFiles[0];
    const owner =
      ownedFile === undefined
        ? entry.provider
        : (registry.owning(ownedFile) ?? entry.provider);
    if (owner.projectPath === undefined) {
      outcomes[index] = identityUnresolved(
        "PROVIDER_SOURCE_UNAVAILABLE",
        `provider ${owner.repository} declares no project of its own`,
      );
      continue;
    }
    const projectPath = path.resolve(owner.projectPath);
    const request: IdentityRequest = {
      index,
      provider: entry.provider,
      owner,
      declaration,
      exportedName: entry.exportedName,
      source: mapped === undefined ? "PROVIDER_EXPORT" : "DECLARATION_MAP",
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
  const position =
    request.source === "DECLARATION_MAP"
      ? request.declaration.sourcePosition
      : await locateProviderExport(
          providerView,
          request.declaration.sourceFiles,
          request.exportedName,
        );
  if (position === undefined) {
    return identityUnresolved(
      "PROVIDER_SOURCE_UNAVAILABLE",
      request.source === "DECLARATION_MAP"
        ? "declaration map segment has no source position"
        : `provider project exports no ${request.exportedName} in ${request.declaration.sourceFiles.join(", ")}`,
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
      repository: request.owner.repository,
      package: request.owner.name,
      qualifiedName: [...candidate.scope, candidate.name].join("."),
      kind: candidate.kind,
      signature: compactSignature(candidate.declaration, sourceFile),
      file: path
        .relative(request.owner.rootPath, sourceFile.fileName)
        .split(path.sep)
        .join("/"),
      startLine:
        sourceFile.getLineAndCharacterOfPosition(declarationStart).line + 1,
      source: request.source,
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

function compareReexportedSymbols(
  left: ReexportedSymbol,
  right: ReexportedSymbol,
): number {
  return (
    left.export.fileName.localeCompare(right.export.fileName) ||
    left.export.start - right.export.start ||
    left.export.end - right.export.end ||
    left.exportedName.localeCompare(right.exportedName)
  );
}
