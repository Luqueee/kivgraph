/**
 * Exact `EXTENDS` edges for TypeScript's declared class and interface
 * inheritance.
 *
 * `class A extends B` and `interface A extends B, C` each contribute one
 * edge per base. The source is always a class or interface this project
 * already indexes as a `LocalSymbol` — a heritage clause introduces no
 * binding of its own, unlike an import or an export — so only the base
 * needs resolving, and it resolves exactly one of two ways:
 *
 *  - A base declared in this project resolves against the local symbol
 *    index, exactly as any other local reference does
 *    (`symbol-resolution.ts`'s `resolveLocalSymbols`, the same function
 *    `reference-extractor.ts` uses for every other local use).
 *  - A base introduced by an import resolves through the *already computed*
 *    `IMPORTS_SYMBOL` resolution for that same binding: `class A extends B`
 *    requires `B` to already be in scope through some `import`, and that
 *    import has already been resolved to a provider identity — or
 *    classified as unresolved, with a reason — by
 *    `imported-symbol-resolver.ts`. Reusing that result is the whole point:
 *    a second declaration-source resolution pipeline could only disagree
 *    with the first, never improve on it.
 *
 * `implements` is deliberately out of scope. The checker proves an
 * `extends` clause is real inheritance; proving `implements` needs a
 * structural conformance check (does this class really satisfy that
 * interface's shape?) this module does not attempt, so an `implements`
 * clause never becomes an edge here — it stays a documented limit.
 */

import path from "node:path";
import type { Node, SourceFile } from "typescript/unstable/ast";
import { SyntaxKind } from "typescript/unstable/ast";
import {
  isClassDeclaration,
  isInterfaceDeclaration,
} from "typescript/unstable/ast/is";
import type {
  ImportedSymbol,
  ImportedSymbolIdentity,
  ImportedSymbolIdentityReason,
} from "./imported-symbol-resolver.js";
import {
  type LanguageService,
  LanguageServiceError,
  type ProjectView,
} from "./language-service.js";
import type { LocalSymbol, LocalSymbolExtraction } from "./symbol-extractor.js";
import {
  resolveLocalSymbols,
  symbolDeclarationKey,
} from "./symbol-resolution.js";

/** One base named in an `extends` clause: the evidence of the edge. */
export interface ExtendsBase {
  readonly fileName: string;
  /** Qualified name of the class or interface declaring the clause. */
  readonly sourceQualifiedName: string;
  /** Source text of the base, the evidence of the edge. */
  readonly text: string;
  /** UTF-16 source offsets, with `end` exclusive. */
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  readonly endLine: number;
}

/** Why an `ExtendsEdge` carries neither a local nor a provider target. */
export type ExtendsUnresolvedReason =
  | ImportedSymbolIdentityReason
  | "DECLARATION_NOT_RESOLVED";

/**
 * One `extends` base, resolved exactly one of three ways: a local
 * declaration (`targetQualifiedName`/`targetFile`), a provider identity
 * reused from an `IMPORTS_SYMBOL` resolution (`identity`), or neither
 * (`unresolvedReason`).
 */
export interface ExtendsEdge {
  readonly base: ExtendsBase;
  readonly targetQualifiedName: string | undefined;
  readonly targetFile: string | undefined;
  /** Package the base was imported from, when it was imported at all. */
  readonly packageName: string | undefined;
  /** Public name requested from that package, when it was imported. */
  readonly exportedName: string | undefined;
  readonly identity: ImportedSymbolIdentity | undefined;
  readonly unresolvedReason: ExtendsUnresolvedReason | undefined;
  readonly unresolvedDetail: string | undefined;
}

export interface ExtendsResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly extends: readonly ExtendsEdge[];
}

interface HeritageRequest {
  readonly file: SourceFile;
  /** The class/interface's own name identifier. */
  readonly sourceNameNode: Node;
  /** The heritage type reference: the evidence span of the edge. */
  readonly baseNode: Node;
  /** The base's own expression, what the checker resolves. */
  readonly baseExpression: Node;
}

/**
 * Resolve every `extends` base declared in this project's own files.
 *
 * `symbols` must have been produced from the same `view`, exactly like
 * `extractLocalReferences` requires. `importedSymbols` is the `IMPORTS_
 * SYMBOL` resolution already computed for this same view — reused, never
 * recomputed.
 */
export async function resolveExtends(
  service: LanguageService,
  view: ProjectView,
  symbols: LocalSymbolExtraction,
  importedSymbols: readonly ImportedSymbol[],
): Promise<ExtendsResolution> {
  service.assertFresh(view);
  if (symbols.generation !== view.generation) {
    throw new LanguageServiceError(
      "STALE_GENERATION",
      `symbols belong to generation ${symbols.generation}, current is ${view.generation}`,
    );
  }
  if (symbols.configFileName !== view.configFileName) {
    throw new LanguageServiceError(
      "INVALID_ARGUMENT",
      `symbols belong to ${symbols.configFileName}, not ${view.configFileName}`,
    );
  }

  const localFiles = await selectLocalFiles(view);
  const requests: HeritageRequest[] = [];
  for (const fileName of localFiles) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (
      sourceFile === undefined ||
      (await view.program.isSourceFileFromExternalLibrary(sourceFile))
    ) {
      continue;
    }
    collectHeritageBases(sourceFile, requests);
  }

  const requestedNodes = [
    ...requests.map((request) => request.sourceNameNode),
    ...requests.map((request) => request.baseExpression),
  ];
  const resolved =
    requestedNodes.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(requestedNodes);
  const sourceSymbols = resolved.slice(0, requests.length);
  const baseSymbols = resolved.slice(requests.length);

  const localById = new Map<number, LocalSymbol>();
  const localByDeclaration = new Map<string, LocalSymbol>();
  for (const local of symbols.symbols) {
    if (!localById.has(local.symbolId)) {
      localById.set(local.symbolId, local);
    }
    for (const declaration of local.symbol.declarations) {
      if (!localByDeclaration.has(symbolDeclarationKey(declaration))) {
        localByDeclaration.set(symbolDeclarationKey(declaration), local);
      }
    }
  }
  const importedBySymbolId = new Map<number, ImportedSymbol>();
  for (const entry of importedSymbols) {
    importedBySymbolId.set(entry.consumer.symbolId, entry);
  }

  // Only the local index is passed in here: an alias that leads outside the
  // project must fall through to the already-resolved import below, keyed
  // by its *original* symbol — exactly the contract `resolveLocalSymbols`
  // documents for its own `importBindingById` parameter, applied by hand so
  // the two possible target shapes stay distinct types instead of one
  // artificial union.
  const localTargets = await resolveLocalSymbols<LocalSymbol>(
    view.checker,
    baseSymbols,
    localById,
    localByDeclaration,
  );

  const edges: ExtendsEdge[] = [];
  for (const [index, request] of requests.entries()) {
    const sourceSymbol = sourceSymbols[index];
    const sourceLocal =
      sourceSymbol === undefined ? undefined : localById.get(sourceSymbol.id);
    if (sourceLocal === undefined) {
      // The declaring class or interface is not itself indexed — an
      // anonymous default export, for instance. There is no source
      // identity to anchor an edge on: this base is out of scope, not
      // unresolved, since nothing about its own target was ever attempted.
      continue;
    }

    const start = request.baseNode.getStart(request.file);
    const end = request.baseNode.getEnd();
    const endPosition = Math.max(start, end - 1);
    const base: ExtendsBase = {
      fileName: request.file.fileName,
      sourceQualifiedName: sourceLocal.qualifiedName,
      text: request.baseNode.getText(request.file),
      start,
      end,
      startLine: request.file.getLineAndCharacterOfPosition(start).line + 1,
      endLine: request.file.getLineAndCharacterOfPosition(endPosition).line + 1,
    };

    const local = localTargets[index];
    if (local !== undefined) {
      edges.push({
        base,
        targetQualifiedName: local.qualifiedName,
        targetFile: local.fileName,
        packageName: undefined,
        exportedName: undefined,
        identity: undefined,
        unresolvedReason: undefined,
        unresolvedDetail: undefined,
      });
      continue;
    }

    const baseSymbol = baseSymbols[index];
    const imported =
      baseSymbol === undefined
        ? undefined
        : importedBySymbolId.get(baseSymbol.id);
    if (imported !== undefined) {
      const identity = imported.target.identity;
      edges.push({
        base,
        targetQualifiedName: undefined,
        targetFile: undefined,
        packageName: imported.packageName,
        exportedName: imported.exportedName,
        identity,
        unresolvedReason:
          identity === undefined ? imported.target.identityReason : undefined,
        unresolvedDetail:
          identity === undefined ? imported.target.identityDetail : undefined,
      });
      continue;
    }

    edges.push({
      base,
      targetQualifiedName: undefined,
      targetFile: undefined,
      packageName: undefined,
      exportedName: undefined,
      identity: undefined,
      unresolvedReason: "DECLARATION_NOT_RESOLVED",
      unresolvedDetail:
        "the base type resolved to neither a local declaration nor a resolved package import",
    });
  }

  service.assertFresh(view);
  edges.sort(compareExtendsEdges);
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    extends: edges,
  };
}

async function selectLocalFiles(view: ProjectView): Promise<string[]> {
  const projectRoot = path.dirname(path.resolve(view.configFileName));
  const sourceFileNames = await view.program.getSourceFileNames();
  return sourceFileNames
    .map((fileName) => path.resolve(fileName))
    .filter((fileName, index, names) => names.indexOf(fileName) === index)
    .filter((fileName) => isWithin(fileName, projectRoot))
    .sort();
}

/** Collect every base of every `extends` heritage clause declared in `file`. */
function collectHeritageBases(
  file: SourceFile,
  requests: HeritageRequest[],
): void {
  const visit = (node: Node): void => {
    if (
      (isClassDeclaration(node) || isInterfaceDeclaration(node)) &&
      node.name !== undefined &&
      node.heritageClauses !== undefined
    ) {
      for (const clause of node.heritageClauses) {
        if (clause.token !== SyntaxKind.ExtendsKeyword) {
          // `implements` clauses are walked no further: see the module
          // doc comment for why they never become an edge here.
          continue;
        }
        for (const base of clause.types) {
          requests.push({
            file,
            sourceNameNode: node.name,
            baseNode: base,
            baseExpression: base.expression,
          });
        }
      }
    }
    node.forEachChild(visit);
  };
  file.forEachChild(visit);
}

function compareExtendsEdges(left: ExtendsEdge, right: ExtendsEdge): number {
  return (
    left.base.fileName.localeCompare(right.base.fileName) ||
    left.base.start - right.base.start ||
    left.base.end - right.base.end
  );
}

function isWithin(fileName: string, root: string): boolean {
  return fileName === root || fileName.startsWith(`${root}${path.sep}`);
}
