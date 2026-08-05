/**
 * Snapshot-scoped extraction and classification of local TypeScript uses.
 *
 * References are discovered from AST occurrences and resolved in one checker
 * batch. The checker, rather than spelling or qualified-name matching, decides
 * whether an occurrence targets one of the local symbols from LUQUE-0608.
 */

import path from "node:path";

import {
  isArrayBindingPattern,
  isArrowFunction,
  isAssignmentOperator,
  isBinaryExpression,
  isBindingElement,
  isCallExpression,
  isClassDeclaration,
  isEnumDeclaration,
  isEnumMember,
  isExportAssignment,
  isExportDeclaration,
  isExpressionWithTypeArguments,
  isFunctionDeclaration,
  isFunctionExpression,
  isGetAccessorDeclaration,
  isIdentifier,
  isImportDeclaration,
  isImportEqualsDeclaration,
  isInterfaceDeclaration,
  isMethodDeclaration,
  isMethodSignatureDeclaration,
  isModuleDeclaration,
  isNewExpression,
  isObjectBindingPattern,
  isParameterDeclaration,
  isPropertyAccessExpression,
  isPropertyDeclaration,
  isPropertySignatureDeclaration,
  isReturnStatement,
  isSetAccessorDeclaration,
  isTypeAliasDeclaration,
  isTypeNode,
  isTypeParameterDeclaration,
  isVariableDeclaration,
} from "typescript/unstable/ast/is";
import type {
  BindingName,
  Identifier,
  Node,
  SourceFile,
} from "typescript/unstable/ast";
import {
  LanguageServiceError,
  type LanguageService,
  type ProjectView,
} from "./language-service.js";
import type { LocalSymbol, LocalSymbolExtraction } from "./symbol-extractor.js";
import { resolveLocalSymbols } from "./symbol-resolution.js";

/** Classification emitted for one resolved local use. */
export type LocalReferenceKind =
  | "REFERENCES"
  | "CALLS_DIRECT"
  | "PASSES_AS_CALLBACK"
  | "ASSIGNS_FUNCTION"
  | "RETURNS_FUNCTION"
  | "TYPE_USES";

/** One source occurrence resolved to one local symbol. */
export interface LocalReference {
  fileName: string;
  kind: LocalReferenceKind;
  /** Undefined for a top-level use without a containing local declaration. */
  source: LocalSymbol | undefined;
  target: LocalSymbol;
  /** UTF-16 source offsets, with `end` exclusive. */
  start: number;
  end: number;
  startLine: number;
  endLine: number;
  text: string;
}

export interface ReferenceExtractionOptions {
  /** Restrict extraction to these project source files. */
  files?: readonly string[];
}

export interface LocalReferenceExtraction {
  generation: number;
  configFileName: string;
  references: LocalReference[];
}

type ContextKind = LocalReferenceKind | undefined;

interface ReferenceRequest {
  file: SourceFile;
  node: Identifier;
  kind: LocalReferenceKind;
}

const KIND_PRIORITY: Record<LocalReferenceKind, number> = {
  REFERENCES: 1,
  TYPE_USES: 2,
  PASSES_AS_CALLBACK: 3,
  ASSIGNS_FUNCTION: 4,
  RETURNS_FUNCTION: 5,
  CALLS_DIRECT: 6,
};

/**
 * Extract local references from the symbols and project view of one snapshot.
 *
 * `symbols` must have been produced from the same `view`. TypeScript aliases
 * are followed through the native checker before the target is matched to a
 * local declaration; unresolved and external aliases are omitted.
 */
export async function extractLocalReferences(
  service: LanguageService,
  view: ProjectView,
  symbols: LocalSymbolExtraction,
  options: ReferenceExtractionOptions = {},
): Promise<LocalReferenceExtraction> {
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

  const localFiles = await selectLocalFiles(view, options.files);
  const requests = new Map<string, ReferenceRequest>();
  const functionValueNodes: Identifier[] = [];

  for (const fileName of localFiles) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (sourceFile === undefined) {
      continue;
    }
    if (await view.program.isSourceFileFromExternalLibrary(sourceFile)) {
      continue;
    }
    collectFileReferences(sourceFile, requests, functionValueNodes);
  }

  const requestList = [...requests.values()];
  const requestedNodes = [
    ...requestList.map((request) => request.node),
    ...functionValueNodes,
  ];
  const resolved =
    requestedNodes.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(requestedNodes);
  const referenceSymbols = resolved.slice(0, requestList.length);
  const functionValueSymbols = resolved.slice(requestList.length);

  const localById = new Map<number, LocalSymbol>();
  const functionSymbolIds = new Set<number>();
  for (const local of symbols.symbols) {
    if (!localById.has(local.symbolId)) {
      localById.set(local.symbolId, local);
    }
    if (local.kind === "function" || local.kind === "method") {
      functionSymbolIds.add(local.symbolId);
    }
  }
  for (const symbol of functionValueSymbols) {
    if (symbol !== undefined && localById.has(symbol.id)) {
      functionSymbolIds.add(symbol.id);
    }
  }

  const localTargets = await resolveLocalSymbols(
    view.checker,
    referenceSymbols,
    localById,
  );

  const references: LocalReference[] = [];
  for (const [index, request] of requestList.entries()) {
    const target = localTargets[index];
    if (target === undefined) {
      continue;
    }
    const kind = normaliseKind(request.kind, functionSymbolIds, target);
    const start = request.node.getStart(request.file);
    const end = request.node.getEnd();
    const endPosition = Math.max(start, end - 1);
    references.push({
      fileName: request.file.fileName,
      kind,
      source: findOwner(symbols.symbols, request.file.fileName, start),
      target,
      start,
      end,
      startLine: request.file.getLineAndCharacterOfPosition(start).line + 1,
      endLine: request.file.getLineAndCharacterOfPosition(endPosition).line + 1,
      text: request.node.getText(request.file),
    });
  }

  service.assertFresh(view);
  references.sort(compareReferences);
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    references,
  };
}

async function selectLocalFiles(
  view: ProjectView,
  requested: readonly string[] | undefined,
): Promise<string[]> {
  const projectRoot = path.dirname(path.resolve(view.configFileName));
  const requestedSet =
    requested === undefined
      ? undefined
      : new Set(
          requested.map((fileName) =>
            path.isAbsolute(fileName)
              ? path.resolve(fileName)
              : path.resolve(projectRoot, fileName),
          ),
        );
  const sourceFileNames = await view.program.getSourceFileNames();
  return sourceFileNames
    .map((fileName) => path.resolve(fileName))
    .filter((fileName, index, names) => names.indexOf(fileName) === index)
    .filter((fileName) =>
      requestedSet === undefined ? true : requestedSet.has(fileName),
    )
    .filter((fileName) => isWithin(fileName, projectRoot))
    .sort();
}

function collectFileReferences(
  file: SourceFile,
  requests: Map<string, ReferenceRequest>,
  functionValueNodes: Identifier[],
): void {
  const visit = (
    node: Node,
    inheritedKind: ContextKind,
    ignored: boolean,
  ): void => {
    const ignoredHere = ignored || isIgnoredContainer(node);
    if (!ignoredHere && isIdentifier(node) && !isDeclarationName(node)) {
      addRequest(requests, {
        file,
        node,
        kind: kindForIdentifier(node, inheritedKind),
      });
    }

    if (!ignoredHere) {
      if (isCallExpression(node) || isNewExpression(node)) {
        addDirectCallee(requests, file, node.expression);
      }
      if (
        isVariableDeclaration(node) &&
        node.initializer !== undefined &&
        (isArrowFunction(node.initializer) ||
          isFunctionExpression(node.initializer))
      ) {
        functionValueNodes.push(...bindingIdentifiers(node.name));
      }
    }

    const nextDefaultKind =
      isTypeNode(node) || isExpressionWithTypeArguments(node)
        ? "TYPE_USES"
        : inheritedKind;
    node.forEachChild((child) => {
      const childKind = childContext(node, child, nextDefaultKind);
      visit(child, childKind, ignoredHere);
    });
  };

  file.forEachChild((child) => visit(child, undefined, false));
}

function childContext(
  parent: Node,
  child: Node,
  inheritedKind: ContextKind,
): ContextKind {
  if (isTypeNode(parent) || isExpressionWithTypeArguments(parent)) {
    return "TYPE_USES";
  }
  if (
    isCallExpression(parent) &&
    parent.arguments.some((argument) => sameNode(argument, child))
  ) {
    return "PASSES_AS_CALLBACK";
  }
  if (
    isBinaryExpression(parent) &&
    isAssignmentOperator(parent.operatorToken.kind) &&
    sameNode(parent.right, child)
  ) {
    return "ASSIGNS_FUNCTION";
  }
  if (
    isVariableDeclaration(parent) &&
    parent.initializer !== undefined &&
    sameNode(parent.initializer, child)
  ) {
    return "ASSIGNS_FUNCTION";
  }
  if (
    isReturnStatement(parent) &&
    parent.expression !== undefined &&
    sameNode(parent.expression, child)
  ) {
    return "RETURNS_FUNCTION";
  }
  return inheritedKind;
}

function addDirectCallee(
  requests: Map<string, ReferenceRequest>,
  file: SourceFile,
  expression: Node,
): void {
  if (isIdentifier(expression)) {
    addRequest(requests, { file, node: expression, kind: "CALLS_DIRECT" });
    return;
  }
  if (isPropertyAccessExpression(expression) && isIdentifier(expression.name)) {
    addRequest(requests, {
      file,
      node: expression.name,
      kind: "CALLS_DIRECT",
    });
  }
}

function addRequest(
  requests: Map<string, ReferenceRequest>,
  request: ReferenceRequest,
): void {
  const key = `${request.file.fileName}\u0000${request.node.pos}:${request.node.end}`;
  const previous = requests.get(key);
  if (
    previous === undefined ||
    KIND_PRIORITY[request.kind] > KIND_PRIORITY[previous.kind]
  ) {
    requests.set(key, request);
  }
}

function kindForIdentifier(
  node: Identifier,
  inheritedKind: ContextKind,
): LocalReferenceKind {
  if (inheritedKind !== undefined) {
    return inheritedKind;
  }
  return node.parent !== undefined && isTypeNode(node.parent)
    ? "TYPE_USES"
    : "REFERENCES";
}

function normaliseKind(
  kind: LocalReferenceKind,
  functionSymbolIds: Set<number>,
  target: LocalSymbol,
): LocalReferenceKind {
  if (
    (kind === "PASSES_AS_CALLBACK" ||
      kind === "ASSIGNS_FUNCTION" ||
      kind === "RETURNS_FUNCTION") &&
    !functionSymbolIds.has(target.symbolId)
  ) {
    return "REFERENCES";
  }
  return kind;
}

function findOwner(
  symbols: readonly LocalSymbol[],
  fileName: string,
  position: number,
): LocalSymbol | undefined {
  let owner: LocalSymbol | undefined;
  for (const candidate of symbols) {
    if (
      candidate.fileName !== fileName ||
      position < candidate.start ||
      position >= candidate.end
    ) {
      continue;
    }
    if (
      owner === undefined ||
      candidate.end - candidate.start < owner.end - owner.start ||
      (candidate.end - candidate.start === owner.end - owner.start &&
        candidate.start < owner.start)
    ) {
      owner = candidate;
    }
  }
  return owner;
}

function isDeclarationName(node: Identifier): boolean {
  const parent = node.parent;
  if (isVariableDeclaration(parent)) {
    return bindingContains(node, parent.name);
  }
  if (isParameterDeclaration(parent)) {
    return bindingContains(node, parent.name);
  }
  if (isBindingElement(parent) && parent.name !== undefined) {
    return bindingContains(node, parent.name);
  }
  if (
    isFunctionDeclaration(parent) ||
    isClassDeclaration(parent) ||
    isInterfaceDeclaration(parent) ||
    isTypeAliasDeclaration(parent) ||
    isEnumDeclaration(parent) ||
    isModuleDeclaration(parent) ||
    isMethodDeclaration(parent) ||
    isGetAccessorDeclaration(parent) ||
    isSetAccessorDeclaration(parent) ||
    isPropertyDeclaration(parent) ||
    isMethodSignatureDeclaration(parent) ||
    isPropertySignatureDeclaration(parent) ||
    isTypeParameterDeclaration(parent) ||
    isEnumMember(parent)
  ) {
    return sameNode(node, declarationName(parent));
  }
  return false;
}

function declarationName(node: Node): Node | undefined {
  if (
    isFunctionDeclaration(node) ||
    isClassDeclaration(node) ||
    isInterfaceDeclaration(node) ||
    isTypeAliasDeclaration(node) ||
    isEnumDeclaration(node) ||
    isModuleDeclaration(node)
  ) {
    return node.name;
  }
  if (
    isMethodDeclaration(node) ||
    isGetAccessorDeclaration(node) ||
    isSetAccessorDeclaration(node) ||
    isPropertyDeclaration(node) ||
    isMethodSignatureDeclaration(node) ||
    isPropertySignatureDeclaration(node) ||
    isEnumMember(node)
  ) {
    return node.name;
  }
  if (isTypeParameterDeclaration(node)) {
    return node.name;
  }
  return undefined;
}

function bindingContains(node: Node, name: BindingName): boolean {
  if (isIdentifier(name)) {
    return sameNode(node, name);
  }
  if (isObjectBindingPattern(name) || isArrayBindingPattern(name)) {
    return name.elements.some(
      (element) =>
        element.name !== undefined && bindingContains(node, element.name),
    );
  }
  return false;
}

function bindingIdentifiers(name: BindingName): Identifier[] {
  if (isIdentifier(name)) {
    return [name];
  }
  if (isObjectBindingPattern(name) || isArrayBindingPattern(name)) {
    return name.elements.flatMap((element) =>
      element.name === undefined ? [] : bindingIdentifiers(element.name),
    );
  }
  return [];
}

function isIgnoredContainer(node: Node): boolean {
  return (
    isImportDeclaration(node) ||
    isImportEqualsDeclaration(node) ||
    isExportDeclaration(node) ||
    isExportAssignment(node)
  );
}

function sameNode(left: Node | undefined, right: Node | undefined): boolean {
  return (
    left !== undefined &&
    right !== undefined &&
    left.kind === right.kind &&
    left.pos === right.pos &&
    left.end === right.end
  );
}

function compareReferences(
  left: LocalReference,
  right: LocalReference,
): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.start - right.start ||
    left.end - right.end ||
    KIND_PRIORITY[right.kind] - KIND_PRIORITY[left.kind] ||
    left.target.name.localeCompare(right.target.name) ||
    (left.source?.name ?? "").localeCompare(right.source?.name ?? "")
  );
}

function isWithin(fileName: string, root: string): boolean {
  return fileName === root || fileName.startsWith(`${root}${path.sep}`);
}
