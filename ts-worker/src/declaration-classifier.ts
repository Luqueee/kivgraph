/**
 * Pure syntactic classification of a TypeScript declaration node.
 *
 * `symbol-extractor.ts` walks a project top-down and classifies every
 * declaration it visits while indexing a repository. `imported-symbol-resolver.ts`
 * walks bottom-up from one exact position in a *different* project — the
 * provider's own source, reached through LUQUE-0703's declaration map — and
 * must classify that single node with the exact same rules. Both call the
 * functions below; neither reimplements them, so two independent walks of
 * the same bytes always agree on kind, qualified name and signature.
 *
 * Nothing here touches the checker or the native language service: this
 * module only looks at the syntax tree the caller already has.
 */

import {
  getTokenAtPosition,
  ModifierFlags,
  SyntaxKind,
} from "typescript/unstable/ast";
import {
  isArrayBindingPattern,
  isClassDeclaration,
  isEnumDeclaration,
  isEnumMember,
  isFunctionDeclaration,
  isGetAccessorDeclaration,
  isIdentifier,
  isInterfaceDeclaration,
  isMethodDeclaration,
  isMethodSignatureDeclaration,
  isPropertySignatureDeclaration,
  isModuleDeclaration,
  isObjectBindingPattern,
  isPropertyDeclaration,
  isParameterDeclaration,
  isSetAccessorDeclaration,
  isTypeAliasDeclaration,
  isTypeParameterDeclaration,
  isVariableDeclaration,
} from "typescript/unstable/ast/is";
import type {
  BindingName,
  Identifier,
  Node,
  SourceFile,
} from "typescript/unstable/ast";

/** Kinds emitted by the local declaration extractor. */
export type LocalSymbolKind =
  | "function"
  | "class"
  | "interface"
  | "method"
  | "variable"
  | "property"
  | "type"
  | "enum"
  | "namespace"
  | "enum_member"
  | "parameter"
  | "type_parameter";

/** One classified declaration site: its syntax kind, name and scope chain. */
export interface DeclarationCandidate {
  file: SourceFile;
  declaration: Node;
  nameNode: Node;
  name: string;
  scope: readonly string[];
  kind: LocalSymbolKind;
  directExportName: string | undefined;
}

/**
 * Classify one declaration node, when `node` is a syntax kind this extractor
 * recognizes.
 *
 * `scope` is stored verbatim on the result; it never affects whether `node`
 * classifies or which name and kind are selected, so a caller that only
 * needs to test a node's shape may pass an empty scope and fill in the real
 * one afterwards — `classifyDeclarationAt` below does exactly that.
 */
export function declarationCandidate(
  file: SourceFile,
  node: Node,
  scope: readonly string[],
  exportedByVariableList: boolean,
  directDefault: boolean,
): DeclarationCandidate | undefined {
  let nameNode: Node | undefined;
  let kind: LocalSymbolKind | undefined;

  if (isFunctionDeclaration(node)) {
    nameNode = node.name;
    kind = "function";
  } else if (isClassDeclaration(node)) {
    nameNode = node.name;
    kind = "class";
  } else if (isInterfaceDeclaration(node)) {
    nameNode = node.name;
    kind = "interface";
  } else if (isTypeAliasDeclaration(node)) {
    nameNode = node.name;
    kind = "type";
  } else if (isVariableDeclaration(node)) {
    const names = bindingIdentifiers(node.name);
    // A destructuring declaration has one candidate per bound identifier.
    // This function always reports the first; a caller that needs the
    // remaining identifiers matches them against the sibling set itself (see
    // `symbol-extractor.ts`'s sibling handling and `classifyDeclarationAt`).
    nameNode = names[0];
    kind = "variable";
  } else if (
    isPropertyDeclaration(node) ||
    isPropertySignatureDeclaration(node)
  ) {
    nameNode = node.name;
    kind = "property";
  } else if (
    isMethodDeclaration(node) ||
    isMethodSignatureDeclaration(node) ||
    isGetAccessorDeclaration(node) ||
    isSetAccessorDeclaration(node)
  ) {
    nameNode = node.name;
    kind = "method";
  } else if (isEnumMember(node)) {
    nameNode = node.name;
    kind = "enum_member";
  } else if (isParameterDeclaration(node)) {
    nameNode = node.name;
    kind = "parameter";
  } else if (isTypeParameterDeclaration(node)) {
    nameNode = node.name;
    kind = "type_parameter";
  } else if (isEnumDeclaration(node)) {
    nameNode = node.name;
    kind = "enum";
  } else if (isModuleDeclaration(node) && isIdentifier(node.name)) {
    nameNode = node.name;
    kind = "namespace";
  }

  if (nameNode === undefined || kind === undefined) {
    return undefined;
  }

  const name = displayName(nameNode, file);
  if (name === "") {
    return undefined;
  }

  return {
    file,
    declaration: node,
    nameNode,
    name,
    scope,
    kind,
    directExportName:
      exportedByVariableList || modifierFlags(node) & ModifierFlags.Export
        ? directDefault
          ? "default"
          : name
        : undefined,
  };
}

/** A compact declaration header, not an inferred semantic signature. */
export function compactSignature(node: Node, file: SourceFile): string {
  const text = node.getText(file).replace(/\s+/gu, " ").trim();
  const body = text.indexOf("{");
  const withoutBody = body >= 0 ? text.slice(0, body).trim() : text;
  const equals =
    isVariableDeclaration(node) && withoutBody.includes("=")
      ? withoutBody.slice(0, withoutBody.indexOf("=")).trim()
      : withoutBody;
  return equals.length > 512 ? `${equals.slice(0, 509)}...` : equals;
}

/** Extend `scope` with `node`'s own name, when it introduces one. */
export function declarationScope(
  node: Node,
  scope: readonly string[],
): readonly string[] {
  const name = scopeName(node);
  return name === undefined ? scope : [...scope, name];
}

/** The name `node` contributes to the qualified name of its descendants. */
export function scopeName(node: Node): string | undefined {
  const named = namedDeclarationName(node);
  if (named !== undefined && isIdentifier(named)) {
    return named.getText();
  }
  if (
    isMethodDeclaration(node) ||
    isMethodSignatureDeclaration(node) ||
    isGetAccessorDeclaration(node) ||
    isSetAccessorDeclaration(node) ||
    isPropertyDeclaration(node)
  ) {
    return displayName(node.name);
  }
  return undefined;
}

/** Every identifier bound by a (possibly destructured) binding name. */
export function bindingIdentifiers(name: BindingName): Identifier[] {
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

/** `node`'s text, unquoted when it is a string-literal property name. */
export function displayName(node: Node, file?: SourceFile): string {
  const text = file === undefined ? node.getText() : node.getText(file);
  const trimmed = text.trim();
  if (
    trimmed.length >= 2 &&
    ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
      (trimmed.startsWith("'") && trimmed.endsWith("'")))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

export function modifierFlags(node: Node): number {
  if ("modifierFlags" in node && typeof node.modifierFlags === "number") {
    return node.modifierFlags;
  }
  return ModifierFlags.None;
}

function namedDeclarationName(node: Node): Node | undefined {
  if (isClassDeclaration(node) || isFunctionDeclaration(node)) {
    return node.name;
  }
  if (
    isInterfaceDeclaration(node) ||
    isTypeAliasDeclaration(node) ||
    isEnumDeclaration(node) ||
    isModuleDeclaration(node)
  ) {
    return node.name;
  }
  return undefined;
}

/**
 * Classify the declaration that owns the identifier at `position`.
 *
 * Walks up from the token at `position` to the nearest ancestor
 * `declarationCandidate` recognizes, then reconstructs the scope chain from
 * every named ancestor above it — exactly the chain a top-down walk of the
 * same file would have accumulated by the time it reached that node.
 *
 * `position` is expected to land exactly on a declared name, as LUQUE-0710's
 * `sourcePosition` promises. A position elsewhere — or one that does not
 * cover a declaration this extractor recognizes — yields no candidate;
 * nothing is guessed.
 */
export function classifyDeclarationAt(
  file: SourceFile,
  position: number,
): DeclarationCandidate | undefined {
  const token = getTokenAtPosition(file, position);
  for (const node of ancestorsFrom(token)) {
    const candidate = matchToken(file, node, token);
    if (candidate !== undefined) {
      return { ...candidate, scope: scopeAbove(node) };
    }
  }
  return undefined;
}

/** `node` classified, only when `token` is (or binds the same name as) it. */
function matchToken(
  file: SourceFile,
  node: Node,
  token: Node,
): DeclarationCandidate | undefined {
  const candidate = declarationCandidate(file, node, [], false, false);
  if (candidate === undefined) {
    return undefined;
  }
  if (candidate.nameNode.getStart(file) === token.getStart(file)) {
    return candidate;
  }
  // A destructured variable declaration: `declarationCandidate` only reports
  // the first bound identifier, so a token on a sibling is matched here,
  // mirroring the synthetic sibling candidates `symbol-extractor.ts` pushes.
  if (isVariableDeclaration(node)) {
    const sibling = bindingIdentifiers(node.name).find(
      (identifier) => identifier.getStart(file) === token.getStart(file),
    );
    if (sibling !== undefined) {
      return {
        ...candidate,
        nameNode: sibling,
        name: displayName(sibling, file),
      };
    }
  }
  return undefined;
}

/** Strict ancestors of `node`, closest first, stopping at the source file. */
function ancestorsFrom(node: Node): Node[] {
  const chain: Node[] = [];
  let current = node;
  while (current.kind !== SyntaxKind.SourceFile) {
    const parent: Node | undefined = current.parent;
    if (parent === undefined) {
      break;
    }
    chain.push(parent);
    current = parent;
  }
  return chain;
}

/** The scope chain a top-down walk would have active just above `node`. */
function scopeAbove(node: Node): string[] {
  const scope: string[] = [];
  for (const ancestor of ancestorsFrom(node)) {
    const name = scopeName(ancestor);
    if (name !== undefined) {
      scope.push(name);
    }
  }
  return scope.reverse();
}
