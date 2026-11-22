/**
 * Snapshot-scoped extraction and classification of local TypeScript uses.
 *
 * References are discovered from AST occurrences and resolved in one checker
 * batch. The checker, rather than spelling or qualified-name matching, decides
 * whether an occurrence targets one of the local symbols from LUQUE-0608.
 */

import path from "node:path";
import {
  type BindingName,
  type Expression,
  type Identifier,
  type Node,
  type ObjectLiteralExpression,
  type PrivateIdentifier,
  type SourceFile,
  type StringLiteral,
  SyntaxKind,
  type TypeNode,
} from "typescript/unstable/ast";
import {
  isArrayBindingPattern,
  isArrowFunction,
  isAssignmentOperator,
  isBinaryExpression,
  isBindingElement,
  isCallExpression,
  isClassDeclaration,
  isElementAccessExpression,
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
  isIndexedAccessTypeNode,
  isInterfaceDeclaration,
  isLiteralTypeNode,
  isMethodDeclaration,
  isMethodSignatureDeclaration,
  isModuleDeclaration,
  isNewExpression,
  isObjectBindingPattern,
  isObjectLiteralExpression,
  isParameterDeclaration,
  isParenthesizedExpression,
  isParenthesizedTypeNode,
  isPrivateIdentifier,
  isPostfixUnaryExpression,
  isPrefixUnaryExpression,
  isPropertyAccessExpression,
  isPropertyAssignment,
  isPropertyDeclaration,
  isPropertySignatureDeclaration,
  isReturnStatement,
  isSetAccessorDeclaration,
  isShorthandPropertyAssignment,
  isStringLiteral,
  isTypeAliasDeclaration,
  isTypeNode,
  isTypeParameterDeclaration,
  isUnionTypeNode,
  isVariableDeclaration,
} from "typescript/unstable/ast/is";
import {
  isUnionType,
  type Symbol as TypeScriptSymbol,
  type Type,
} from "typescript/unstable/async";
import { isDependencySourceFile } from "./dependency-source-file.js";
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

/** Classification emitted for one resolved local use. */
export type LocalReferenceKind =
  | "REFERENCES"
  | "CALLS_DIRECT"
  | "PASSES_AS_CALLBACK"
  | "ASSIGNS_FUNCTION"
  | "RETURNS_FUNCTION"
  | "TYPE_USES";

/**
 * The import binding a reference falls back to when it never resolves to a
 * genuine local declaration — LUQUE-0907's "import" kind symbol, described
 * just enough for a reference to target it.
 */
export interface ImportBindingSymbol {
  readonly symbolId: number;
  readonly fileName: string;
  readonly name: string;
  readonly qualifiedName: string;
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  readonly endLine: number;
}

/** One source occurrence resolved to one local symbol or import binding. */
export interface LocalReference {
  fileName: string;
  kind: LocalReferenceKind;
  /** Undefined for a top-level use without a containing local declaration. */
  source: LocalSymbol | undefined;
  target: LocalSymbol | ImportBindingSymbol;
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
  /**
   * Import bindings already emitted as "import" kind symbols elsewhere in
   * the payload, keyed implicitly by their own checker symbol id: a use that
   * never resolves to a genuine local declaration can still target the
   * binding itself, exactly as LUQUE-0907 already targets it from `imports`.
   */
  importBindings?: readonly ImportBindingSymbol[];
}

export interface LocalReferenceExtraction {
  generation: number;
  configFileName: string;
  references: LocalReference[];
}

type ContextKind = LocalReferenceKind | undefined;

/**
 * The declarations a use of an accessor pair reaches: the setter when the
 * property is assigned, the getter when it is read, both for `+=` and `++`.
 * A pair with one half, and anything that is not an accessor, is its target.
 */
function accessorTargets<T extends { symbolId: number }>(
  target: T,
  node: Node,
  getters: ReadonlyMap<number, T & LocalSymbol>,
  setters: ReadonlyMap<number, T & LocalSymbol>,
): T[] {
  const getter = getters.get(target.symbolId);
  const setter = setters.get(target.symbolId);
  if (getter === undefined || setter === undefined) {
    return [target];
  }
  const access = node.parent;
  if (!isPropertyAccessExpression(access) || !sameNode(access.name, node)) {
    return [getter];
  }
  // `(box.size) = 2` writes through the parentheses, so the write is matched
  // at the outermost wrapper, not at the property access.
  let operand: Node = access;
  let user: Node = access.parent;
  while (isParenthesizedExpression(user)) {
    operand = user;
    user = user.parent;
  }
  if (isBinaryExpression(user) && sameNode(user.left, operand)) {
    if (user.operatorToken.kind === SyntaxKind.EqualsToken) {
      return [setter];
    }
    if (isAssignmentOperator(user.operatorToken.kind)) {
      return [getter, setter];
    }
  }
  if (
    (isPrefixUnaryExpression(user) || isPostfixUnaryExpression(user)) &&
    (user.operator === SyntaxKind.PlusPlusToken ||
      user.operator === SyntaxKind.MinusMinusToken)
  ) {
    return [getter, setter];
  }
  return [getter];
}

interface ReferenceRequest {
  file: SourceFile;
  node: Identifier | PrivateIdentifier;
  kind: LocalReferenceKind;
}

/**
 * A property name written where the checker does not point it at the property:
 * the key of an object literal, or a destructured binding with no explicit
 * property name. `getSymbolAtLocation` answers with the literal's own member or
 * the new local variable, so these are resolved through the type the literal
 * is checked against or the value being destructured.
 */
interface PropertyKeyRequest {
  file: SourceFile;
  node: Identifier | StringLiteral;
  container: Node;
  source: PropertyKeySource;
}

type PropertyKeySource =
  | "literal"
  | "pattern"
  | "indexed-value"
  | "indexed-type";

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
  const allLocalFiles = await selectLocalFiles(view, undefined);
  const requests = new Map<string, ReferenceRequest>();
  const propertyKeys = new Map<string, PropertyKeyRequest>();
  // A key is only worth a type query if some local declaration has that name.
  const propertyNames = new Set(
    symbols.symbols
      .filter((local) => local.kind === "property" || local.kind === "method")
      .map((local) => local.name),
  );
  // `const { load } = await import("./x")` names an export of a module, of
  // any kind. The binding is a new local, so nothing else points it at the
  // declaration, and what a route or a command loads this way had no use.
  const exportedNames = new Set(
    symbols.symbols.flatMap((local) => local.exportedNames),
  );
  const functionValueNodes: Identifier[] = [];
  const sourceFiles = new Map<string, SourceFile>();

  for (const fileName of allLocalFiles) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (
      sourceFile === undefined ||
      (await isDependencySourceFile(view.program, sourceFile))
    ) {
      continue;
    }
    sourceFiles.set(fileName, sourceFile);
    collectFunctionValueNodes(sourceFile, functionValueNodes);
  }
  for (const fileName of localFiles) {
    const sourceFile = sourceFiles.get(fileName);
    if (sourceFile !== undefined) {
      collectFileReferences(
        sourceFile,
        requests,
        propertyKeys,
        propertyNames,
        exportedNames,
      );
    }
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
  const localByDeclaration = new Map<string, LocalSymbol>();
  const functionSymbolIds = new Set<number>();
  for (const local of symbols.symbols) {
    if (!localById.has(local.symbolId)) {
      localById.set(local.symbolId, local);
    }
    for (const declaration of local.symbol.declarations) {
      if (!localByDeclaration.has(symbolDeclarationKey(declaration))) {
        localByDeclaration.set(symbolDeclarationKey(declaration), local);
      }
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

  const importBindingById = new Map<number, ImportBindingSymbol>();
  for (const binding of options.importBindings ?? []) {
    importBindingById.set(binding.symbolId, binding);
  }

  const localTargets = await resolveLocalSymbols<
    LocalSymbol | ImportBindingSymbol
  >(
    view.checker,
    referenceSymbols,
    localById,
    localByDeclaration,
    importBindingById,
  );

  // The checker has one symbol for a `get`/`set` pair and both declarations
  // are published. Every use went to whichever came first, so the other had
  // no reference at all and an answer about it said nothing uses it.
  const setters = new Map<number, LocalSymbol>();
  const getters = new Map<number, LocalSymbol>();
  for (const local of symbols.symbols) {
    if (local.accessor === "set" && !setters.has(local.symbolId)) {
      setters.set(local.symbolId, local);
    } else if (local.accessor === "get" && !getters.has(local.symbolId)) {
      getters.set(local.symbolId, local);
    }
  }

  const references: LocalReference[] = [];
  for (const [index, request] of requestList.entries()) {
    const resolved = localTargets[index];
    if (resolved === undefined) {
      continue;
    }
    for (const target of accessorTargets(
      resolved,
      request.node,
      getters,
      setters,
    )) {
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
        endLine:
          request.file.getLineAndCharacterOfPosition(endPosition).line + 1,
        text: request.node.getText(request.file),
      });
    }
  }

  const keyList = [...propertyKeys.values()];
  const keyTargets = await resolveLocalSymbols<
    LocalSymbol | ImportBindingSymbol
  >(
    view.checker,
    await resolvePropertyKeys(
      view.checker,
      keyList,
      (symbol) =>
        localById.has(symbol.id) ||
        symbol.declarations.some((declaration) =>
          localByDeclaration.has(symbolDeclarationKey(declaration)),
        ),
    ),
    localById,
    localByDeclaration,
    importBindingById,
  );
  for (const [index, request] of keyList.entries()) {
    const target = keyTargets[index];
    if (target === undefined) {
      continue;
    }
    const start = request.node.getStart(request.file);
    const end = request.node.getEnd();
    references.push({
      fileName: request.file.fileName,
      kind: "REFERENCES",
      source: findOwner(symbols.symbols, request.file.fileName, start),
      target,
      start,
      end,
      startLine: request.file.getLineAndCharacterOfPosition(start).line + 1,
      endLine:
        request.file.getLineAndCharacterOfPosition(Math.max(start, end - 1))
          .line + 1,
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
  const projectRoot = view.localRoot;
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
  propertyKeys: Map<string, PropertyKeyRequest>,
  propertyNames: ReadonlySet<string>,
  exportedNames: ReadonlySet<string>,
): void {
  const visit = (
    node: Node,
    inheritedKind: ContextKind,
    ignored: boolean,
  ): void => {
    const ignoredHere = ignored || isIgnoredContainer(node);
    if (!ignoredHere && (isIdentifier(node) || isStringLiteral(node))) {
      const key = propertyKeyContainer(node);
      const name = isStringLiteral(node) ? node.text : node.getText(file);
      if (
        key !== undefined &&
        (propertyNames.has(name) ||
          (exportedNames.has(name) && destructuresDynamicImport(key.container)))
      ) {
        propertyKeys.set(`${file.fileName}\u0000${node.pos}:${node.end}`, {
          file,
          node,
          container: key.container,
          source: key.source,
        });
      }
    }
    if (
      !ignoredHere &&
      (isIdentifier(node) || isPrivateIdentifier(node)) &&
      !isDeclarationName(node)
    ) {
      addRequest(requests, {
        file,
        node,
        kind: kindForIdentifier(node, inheritedKind),
      });
    }

    if (!ignoredHere && (isCallExpression(node) || isNewExpression(node))) {
      addDirectCallee(requests, file, node.expression);
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

/** Whether a binding pattern destructures `await import(...)`. */
function destructuresDynamicImport(pattern: Node): boolean {
  const declaration = pattern.parent;
  if (
    declaration === undefined ||
    !isVariableDeclaration(declaration) ||
    declaration.initializer === undefined
  ) {
    return false;
  }
  let value: Node = declaration.initializer;
  while (
    value.kind === SyntaxKind.AwaitExpression ||
    value.kind === SyntaxKind.ParenthesizedExpression
  ) {
    value = (value as unknown as { expression: Node }).expression;
  }
  return (
    isCallExpression(value) &&
    value.expression.kind === SyntaxKind.ImportKeyword
  );
}

function propertyKeyContainer(
  node: Identifier | StringLiteral,
): { container: Node; source: PropertyKeySource } | undefined {
  const parent = node.parent;
  if (parent === undefined) {
    return undefined;
  }
  if (isStringLiteral(node)) {
    return stringKeyContainer(node, parent);
  }
  if (
    (isPropertyAssignment(parent) ||
      isShorthandPropertyAssignment(parent) ||
      isMethodDeclaration(parent) ||
      isGetAccessorDeclaration(parent) ||
      isSetAccessorDeclaration(parent)) &&
    sameNode(parent.name, node) &&
    parent.parent !== undefined &&
    isObjectLiteralExpression(parent.parent)
  ) {
    return { container: parent.parent, source: "literal" };
  }
  if (
    isBindingElement(parent) &&
    parent.propertyName === undefined &&
    parent.dotDotDotToken === undefined &&
    sameNode(parent.name, node) &&
    parent.parent !== undefined &&
    isObjectBindingPattern(parent.parent)
  ) {
    return { container: parent.parent, source: "pattern" };
  }
  return undefined;
}

/** A string literal that names a property: `value["key"]`, `T["key"]`. */
function stringKeyContainer(
  node: StringLiteral,
  parent: Node,
): { container: Node; source: PropertyKeySource } | undefined {
  if (
    isElementAccessExpression(parent) &&
    sameNode(parent.argumentExpression, node)
  ) {
    return { container: parent.expression, source: "indexed-value" };
  }
  if (!isLiteralTypeNode(parent)) {
    return undefined;
  }
  // `T["a" | "b"]` names both: climb out of the union to the indexed access.
  let previous: Node = parent;
  let current = parent.parent;
  while (
    current !== undefined &&
    (isUnionTypeNode(current) || isParenthesizedTypeNode(current))
  ) {
    previous = current;
    current = current.parent;
  }
  if (
    current !== undefined &&
    isIndexedAccessTypeNode(current) &&
    sameNode(current.indexType, previous)
  ) {
    return { container: current.objectType, source: "indexed-type" };
  }
  return undefined;
}

/**
 * Find, for each key, the property it names on the type the key is checked
 * against. Types are looked up once per object literal or pattern and their
 * members once per type; `null` and `undefined` are removed first so an optional
 * parameter still has the properties of what it holds.
 */
async function resolvePropertyKeys(
  checker: ProjectView["checker"],
  requests: readonly PropertyKeyRequest[],
  isLocal: (symbol: TypeScriptSymbol) => boolean,
): Promise<(TypeScriptSymbol | undefined)[]> {
  if (requests.length === 0) {
    return [];
  }
  const types: (Type | undefined)[] = new Array(requests.length).fill(
    undefined,
  );
  const valueSources = requests.flatMap((request, index) =>
    request.source === "pattern" || request.source === "indexed-value"
      ? [{ index, container: request.container }]
      : [],
  );
  if (valueSources.length > 0) {
    const valueTypes = await checker.getTypeAtLocation(
      valueSources.map((source) => source.container),
    );
    for (const [position, source] of valueSources.entries()) {
      types[source.index] = valueTypes[position];
    }
  }
  await Promise.all(
    requests.map(async (request, index) => {
      if (request.source === "literal") {
        types[index] = await checker.getContextualType(
          request.container as Expression,
        );
      } else if (request.source === "indexed-type") {
        // See implements-resolver: the engine cannot type every node.
        types[index] = await checker
          .getTypeFromTypeNode(request.container as TypeNode)
          .catch(() => undefined);
      }
    }),
  );

  const members = new Map<number, Promise<Map<string, TypeScriptSymbol>>>();
  const membersOf = (type: Type): Promise<Map<string, TypeScriptSymbol>> => {
    let cached = members.get(type.id);
    if (cached === undefined) {
      cached = (async () => {
        const present = (await checker.getNonNullableType(type)) ?? type;
        const byName = new Map<string, TypeScriptSymbol>();
        for (const member of await checker.getPropertiesOfType(present)) {
          byName.set(member.name, member);
        }
        return byName;
      })();
      members.set(type.id, cached);
    }
    return cached;
  };
  return Promise.all(
    requests.map(async (request, index) => {
      const type = types[index];
      if (type === undefined) {
        return undefined;
      }
      const name = isStringLiteral(request.node)
        ? request.node.text
        : request.node.getText(request.file);
      const found = (await membersOf(type)).get(name);
      if (
        request.source !== "literal" ||
        (found !== undefined && (isLocal(found) || !isLiteralMember(found)))
      ) {
        return found;
      }
      // The contextual type of an argument of a generic call is what the
      // checker inferred from the literal, so its members are the literal's
      // own. The declared type is the one the parameter is constrained to.
      const declared = await declaredLiteralTypes(
        checker,
        request.container as ObjectLiteralExpression,
      );
      let fallback: TypeScriptSymbol | undefined;
      for (const candidate of declared) {
        const member = (await membersOf(candidate)).get(name);
        if (member !== undefined && isLocal(member)) {
          return member;
        }
        fallback ??= member;
      }
      return fallback ?? found;
    }),
  );
}

/**
 * Whether a member is one the literal itself declares, which is what the
 * checker answers when it found nothing declared to check the literal against.
 */
function isLiteralMember(member: TypeScriptSymbol): boolean {
  return member.declarations.every((declaration) =>
    LITERAL_MEMBER_KINDS.has(declaration.kind),
  );
}

const LITERAL_MEMBER_KINDS: ReadonlySet<SyntaxKind> = new Set([
  SyntaxKind.PropertyAssignment,
  SyntaxKind.ShorthandPropertyAssignment,
  SyntaxKind.MethodDeclaration,
  SyntaxKind.GetAccessor,
  SyntaxKind.SetAccessor,
]);

/**
 * The types an object literal is declared to have, when it is an argument of a
 * call (or of a literal that is, through property values) and the parameter
 * is a type parameter: the constraint of that parameter, walked down the
 * properties that lead to the literal. A union is walked member by member, as
 * the checker does for a contextual type, because a property is on the union
 * only when every member has it. Empty when the literal is not in that
 * position or the declared type has no such property.
 */
async function declaredLiteralTypes(
  checker: ProjectView["checker"],
  literal: ObjectLiteralExpression,
): Promise<Type[]> {
  const keys: string[] = [];
  let current: ObjectLiteralExpression = literal;
  while (
    current.parent !== undefined &&
    isPropertyAssignment(current.parent) &&
    current.parent.parent !== undefined &&
    isObjectLiteralExpression(current.parent.parent)
  ) {
    const name = current.parent.name;
    if (!isIdentifier(name) && !isStringLiteral(name)) {
      return [];
    }
    keys.unshift(name.text);
    current = current.parent.parent;
  }
  const call = current.parent;
  if (
    call === undefined ||
    !(isCallExpression(call) || isNewExpression(call))
  ) {
    return [];
  }
  const index = (call.arguments ?? []).findIndex((argument) =>
    sameNode(argument, current),
  );
  const signature =
    index < 0 ? undefined : await checker.getResolvedSignature(call);
  const parameter =
    signature === undefined
      ? undefined
      : await checker.getParameterType(
          (await signature.getTarget()) ?? signature,
          index,
        );
  if (parameter === undefined) {
    return [];
  }

  const members = async (type: Type): Promise<Type[]> => {
    const bound = (await checker.getBaseConstraintOfType(type)) ?? type;
    const present = (await checker.getNonNullableType(bound)) ?? bound;
    return isUnionType(present) ? [...(await present.getTypes())] : [present];
  };
  let types = await members(parameter);
  for (const key of keys) {
    const next: Type[] = [];
    for (const type of types) {
      const member = await checker.getPropertyOfType(type, key);
      const memberType =
        member === undefined
          ? undefined
          : await checker.getTypeOfSymbol(member);
      if (memberType !== undefined) {
        next.push(...(await members(memberType)));
      }
    }
    types = next;
  }
  return types;
}

function collectFunctionValueNodes(
  file: SourceFile,
  output: Identifier[],
): void {
  const visit = (node: Node): void => {
    if (
      isVariableDeclaration(node) &&
      node.initializer !== undefined &&
      (isArrowFunction(node.initializer) ||
        isFunctionExpression(node.initializer))
    ) {
      output.push(...bindingIdentifiers(node.name));
    }
    node.forEachChild(visit);
  };

  file.forEachChild(visit);
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
  if (
    isPropertyAccessExpression(expression) &&
    (isIdentifier(expression.name) || isPrivateIdentifier(expression.name))
  ) {
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
  node: Identifier | PrivateIdentifier,
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
  target: LocalSymbol | ImportBindingSymbol,
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

// symbolsByFile groups a symbol list by file, once per list. findOwner is
// asked once per reference, and scanning every symbol of the program each time
// made the pass quadratic: a program of 2760 files took 25 minutes, 73 % of
// them here.
const symbolsByFile = new WeakMap<
  readonly LocalSymbol[],
  Map<string, LocalSymbol[]>
>();

/**
 * The narrowest symbol of a file that contains a position, and the earliest
 * of two of the same width.
 */
export function findOwner(
  symbols: readonly LocalSymbol[],
  fileName: string,
  position: number,
): LocalSymbol | undefined {
  let byFile = symbolsByFile.get(symbols);
  if (byFile === undefined) {
    byFile = new Map();
    for (const symbol of symbols) {
      const inFile = byFile.get(symbol.fileName);
      if (inFile === undefined) {
        byFile.set(symbol.fileName, [symbol]);
      } else {
        inFile.push(symbol);
      }
    }
    symbolsByFile.set(symbols, byFile);
  }
  let owner: LocalSymbol | undefined;
  for (const candidate of byFile.get(fileName) ?? []) {
    if (position < candidate.start || position >= candidate.end) {
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

function isDeclarationName(node: Identifier | PrivateIdentifier): boolean {
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
