import {
  isClassDeclaration,
  isEnumDeclaration,
  isEnumMember,
  isFunctionDeclaration,
  isInterfaceDeclaration,
  isMethodDeclaration,
  isMethodSignatureDeclaration,
  isModuleDeclaration,
  isPropertyDeclaration,
  isPropertySignatureDeclaration,
  isTypeAliasDeclaration,
  isVariableDeclaration,
} from "typescript/unstable/ast/is";
import type { Node } from "typescript/unstable/ast";

/**
 * The name node of a declaration, when the syntax has one.
 *
 * Only declarations the checker already selected are inspected, so this
 * locates a token inside a resolved node; it never matches symbols by name.
 */
export function declarationName(node: Node): Node | undefined {
  if (
    isVariableDeclaration(node) ||
    isFunctionDeclaration(node) ||
    isClassDeclaration(node) ||
    isInterfaceDeclaration(node) ||
    isTypeAliasDeclaration(node) ||
    isEnumDeclaration(node) ||
    isEnumMember(node) ||
    isModuleDeclaration(node) ||
    isMethodDeclaration(node) ||
    isMethodSignatureDeclaration(node) ||
    isPropertyDeclaration(node) ||
    isPropertySignatureDeclaration(node)
  ) {
    return node.name;
  }
  return undefined;
}
