/**
 * Snapshot-scoped extraction of local TypeScript declarations.
 *
 * The native TypeScript 7 server is the source of symbol identity. AST walking
 * only finds declaration sites; every emitted symbol is backed by the checker
 * and every checker lookup is batched for the whole selected file set.
 */

import path from "node:path";

import { ModifierFlags } from "typescript/unstable/ast";
import {
  isExportAssignment,
  isExportDeclaration,
  isExportSpecifier,
  isIdentifier,
  isNamedExports,
  isVariableDeclaration,
  isVariableDeclarationList,
  isVariableStatement,
} from "typescript/unstable/ast/is";
import type { Node, SourceFile } from "typescript/unstable/ast";
import type { Symbol as TypeScriptSymbol } from "typescript/unstable/async";

import {
  bindingIdentifiers,
  compactSignature,
  declarationCandidate,
  declarationScope,
  displayName,
  modifierFlags,
  type DeclarationCandidate,
  type LocalSymbolKind,
} from "./declaration-classifier.js";
import type { LanguageService, ProjectView } from "./language-service.js";
import { resolveLocalSymbols } from "./symbol-resolution.js";

export type { LocalSymbolKind } from "./declaration-classifier.js";

/** A declaration backed by a checker symbol in one live snapshot. */
export interface LocalSymbol {
  /** TypeScript's snapshot-local symbol id. */
  symbolId: number;
  /** Snapshot-scoped checker object used by the next extraction stages. */
  symbol: TypeScriptSymbol;
  fileName: string;
  name: string;
  qualifiedName: string;
  kind: LocalSymbolKind;
  /** UTF-16 source offsets, with `end` exclusive. */
  start: number;
  end: number;
  startLine: number;
  endLine: number;
  /** A compact declaration header, not an inferred semantic signature. */
  signature: string;
  exported: boolean;
  exportedNames: string[];
}

/** A local export binding resolved to its declaration symbol. */
export interface LocalExport {
  fileName: string;
  exportedName: string;
  localName: string;
  isTypeOnly: boolean;
  symbolId: number;
  symbol: TypeScriptSymbol;
  start: number;
  end: number;
}

export interface SymbolExtractionOptions {
  /** Restrict extraction to these project source files. */
  files?: readonly string[];
}

export interface LocalSymbolExtraction {
  generation: number;
  configFileName: string;
  symbols: LocalSymbol[];
  exports: LocalExport[];
}

interface ExportRequest {
  file: SourceFile;
  node: Node;
  nameNode: Node;
  exportedName: string;
  localName: string;
  isTypeOnly: boolean;
  reExport: boolean;
}

interface ExportStarRequest {
  file: SourceFile;
  node: Node;
  moduleSpecifier: Node;
  isTypeOnly: boolean;
}

interface CollectedFile {
  candidates: DeclarationCandidate[];
  exports: ExportRequest[];
  exportStars: ExportStarRequest[];
}

/**
 * Extract declarations and local export bindings from one live project view.
 *
 * `view` is snapshot-scoped. The service is checked before and after all
 * native calls so a concurrent update cannot return handles from a disposed
 * snapshot as if they were current.
 */
export async function extractLocalSymbols(
  service: LanguageService,
  view: ProjectView,
  options: SymbolExtractionOptions = {},
): Promise<LocalSymbolExtraction> {
  service.assertFresh(view);

  const sourceFileNames = await view.program.getSourceFileNames();
  const selected = selectLocalFiles(
    sourceFileNames,
    options.files,
    view.configFileName,
  );
  const collected: CollectedFile[] = [];

  for (const fileName of selected) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (sourceFile === undefined) {
      continue;
    }
    if (await view.program.isSourceFileFromExternalLibrary(sourceFile)) {
      continue;
    }
    collected.push(collectFile(sourceFile));
  }

  const candidates = collected.flatMap((file) => file.candidates);
  const exportRequests = collected.flatMap((file) => file.exports);
  const exportStarRequests = collected.flatMap((file) => file.exportStars);
  const requestedNodes = [
    ...candidates.map((candidate) => candidate.nameNode),
    ...exportRequests.map((request) => request.nameNode),
    ...exportStarRequests.map((request) => request.moduleSpecifier),
  ];
  const resolved =
    requestedNodes.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(requestedNodes);
  const candidateSymbols = resolved.slice(0, candidates.length);
  const exportSymbols = resolved.slice(
    candidates.length,
    candidates.length + exportRequests.length,
  );
  const exportStarModuleSymbols = resolved.slice(
    candidates.length + exportRequests.length,
  );

  const localById = new Map<number, LocalSymbol[]>();
  const symbols: LocalSymbol[] = [];
  const exports: LocalExport[] = [];

  for (const [index, candidate] of candidates.entries()) {
    const symbol = candidateSymbols[index];
    if (symbol === undefined) {
      continue;
    }
    const local = makeLocalSymbol(candidate, symbol);
    symbols.push(local);
    const entries = localById.get(symbol.id);
    if (entries === undefined) {
      localById.set(symbol.id, [local]);
    } else {
      entries.push(local);
    }
    if (candidate.directExportName !== undefined) {
      exports.push({
        fileName: candidate.file.fileName,
        exportedName: candidate.directExportName,
        localName: candidate.name,
        isTypeOnly: isTypeSymbol(local),
        symbolId: local.symbolId,
        symbol: local.symbol,
        start: candidate.declaration.getStart(candidate.file),
        end: candidate.declaration.getEnd(),
      });
    }
  }

  const exportTargetSymbols: (TypeScriptSymbol | undefined)[] = [];
  for (const [index, request] of exportRequests.entries()) {
    let symbol = exportSymbols[index];
    if (
      isExportSpecifier(request.node) &&
      (symbol === undefined || !localById.has(symbol.id))
    ) {
      symbol = await view.checker.getExportSpecifierLocalTargetSymbol(
        request.node,
      );
    }
    exportTargetSymbols.push(symbol);
  }
  const exportTargets = await resolveLocalSymbols(
    view.checker,
    exportTargetSymbols,
    localById,
  );

  for (const [index, request] of exportRequests.entries()) {
    const locals = exportTargets[index];
    if (locals === undefined) {
      continue;
    }
    for (const local of locals) {
      if (!request.reExport) {
        if (!local.exportedNames.includes(request.exportedName)) {
          local.exportedNames.push(request.exportedName);
          local.exportedNames.sort();
        }
        local.exported = true;
      }
      exports.push({
        fileName: request.file.fileName,
        exportedName: request.exportedName,
        localName: request.localName,
        isTypeOnly: request.isTypeOnly || isTypeSymbol(local),
        symbolId: local.symbolId,
        symbol: local.symbol,
        start: request.node.getStart(request.file),
        end: request.node.getEnd(),
      });
    }
  }

  const starEntries = (
    await Promise.all(
      exportStarRequests.map(async (request, index) => {
        const moduleSymbol = exportStarModuleSymbols[index];
        if (moduleSymbol === undefined) {
          return [];
        }
        const moduleExports =
          await view.checker.getExportsOfModule(moduleSymbol);
        return moduleExports
          .filter((symbol) => symbol.name !== "default")
          .map((symbol) => ({
            request,
            symbol,
            exportedName: symbol.name,
          }));
      }),
    )
  ).flat();
  const starTargets = await resolveLocalSymbols(
    view.checker,
    starEntries.map((entry) => entry.symbol),
    localById,
  );
  for (const [index, entry] of starEntries.entries()) {
    const locals = starTargets[index];
    if (locals === undefined) {
      continue;
    }
    for (const local of locals) {
      exports.push({
        fileName: entry.request.file.fileName,
        exportedName: entry.exportedName,
        localName: local.name,
        isTypeOnly: entry.request.isTypeOnly || isTypeSymbol(local),
        symbolId: local.symbolId,
        symbol: local.symbol,
        start: entry.request.node.getStart(entry.request.file),
        end: entry.request.node.getEnd(),
      });
    }
  }

  service.assertFresh(view);
  symbols.sort(compareSymbols);
  exports.sort(compareExports);

  return {
    generation: view.generation,
    configFileName: view.configFileName,
    symbols,
    exports,
  };
}

function selectLocalFiles(
  sourceFileNames: readonly string[],
  requested: readonly string[] | undefined,
  configFileName: string,
): string[] {
  const projectRoot = pathDirectory(configFileName);
  const requestedSet =
    requested === undefined
      ? undefined
      : new Set(
          requested.map((fileName) =>
            path.isAbsolute(fileName)
              ? resolvePath(fileName)
              : path.resolve(projectRoot, fileName),
          ),
        );

  return sourceFileNames
    .map((fileName) => resolvePath(fileName))
    .filter((fileName, index, names) => names.indexOf(fileName) === index)
    .filter((fileName) =>
      requestedSet === undefined ? true : requestedSet.has(fileName),
    )
    .filter((fileName) => isWithin(fileName, projectRoot))
    .sort();
}

function collectFile(file: SourceFile): CollectedFile {
  const candidates: DeclarationCandidate[] = [];
  const exports: ExportRequest[] = [];
  const exportStars: ExportStarRequest[] = [];

  const visit = (
    node: Node,
    scope: readonly string[],
    exportedVariableList: boolean,
  ): void => {
    const flags = modifierFlags(node);
    const directExport = (flags & ModifierFlags.Export) !== 0;
    const directDefault = (flags & ModifierFlags.Default) !== 0;
    const candidate = declarationCandidate(
      file,
      node,
      scope,
      exportedVariableList || directExport,
      directDefault,
    );
    if (candidate !== undefined) {
      candidates.push(candidate);
      if (isVariableDeclaration(node)) {
        const names = bindingIdentifiers(node.name);
        for (const nameNode of names.slice(1)) {
          const name = displayName(nameNode, file);
          if (name === "") {
            continue;
          }
          candidates.push({
            ...candidate,
            nameNode,
            name,
            directExportName:
              candidate.directExportName === undefined ? undefined : name,
          });
        }
      }
    }

    if (isExportDeclaration(node)) {
      collectExportDeclaration(file, node, exports, exportStars);
    } else if (isExportAssignment(node)) {
      collectExportAssignment(file, node, exports);
    }

    const nextScope = declarationScope(node, scope);
    const childExportedVariableList = isVariableStatement(node)
      ? directExport
      : isVariableDeclarationList(node)
        ? exportedVariableList
        : false;
    node.forEachChild((child) =>
      visit(child, nextScope, childExportedVariableList),
    );
  };

  file.forEachChild((child) => visit(child, [], false));
  return { candidates, exports, exportStars };
}

function collectExportDeclaration(
  file: SourceFile,
  node: Node,
  output: ExportRequest[],
  starOutput: ExportStarRequest[],
): void {
  if (!isExportDeclaration(node)) {
    return;
  }
  const clause = node.exportClause;
  if (clause !== undefined && isNamedExports(clause)) {
    for (const specifier of clause.elements) {
      if (!isExportSpecifier(specifier)) {
        continue;
      }
      const localNode = specifier.propertyName ?? specifier.name;
      output.push({
        file,
        node: specifier,
        nameNode: localNode,
        exportedName: displayName(specifier.name, file),
        localName: displayName(localNode, file),
        isTypeOnly: node.isTypeOnly || specifier.isTypeOnly,
        reExport: node.moduleSpecifier !== undefined,
      });
    }
    return;
  }
  if (node.moduleSpecifier !== undefined && clause === undefined) {
    starOutput.push({
      file,
      node,
      moduleSpecifier: node.moduleSpecifier,
      isTypeOnly: node.isTypeOnly,
    });
  }
}

function collectExportAssignment(
  file: SourceFile,
  node: Node,
  output: ExportRequest[],
): void {
  if (!isExportAssignment(node) || !isIdentifier(node.expression)) {
    return;
  }
  output.push({
    file,
    node,
    nameNode: node.expression,
    exportedName: node.isExportEquals ? "export=" : "default",
    localName: displayName(node.expression, file),
    isTypeOnly: false,
    reExport: false,
  });
}

function makeLocalSymbol(
  candidate: DeclarationCandidate,
  symbol: TypeScriptSymbol,
): LocalSymbol {
  const start = candidate.declaration.getStart(candidate.file);
  const end = candidate.declaration.getEnd();
  const startLine =
    candidate.file.getLineAndCharacterOfPosition(start).line + 1;
  const endPosition = Math.max(start, end - 1);
  const endLine =
    candidate.file.getLineAndCharacterOfPosition(endPosition).line + 1;
  const qualifiedName = [...candidate.scope, candidate.name].join(".");
  const directExportNames =
    candidate.directExportName === undefined
      ? []
      : [candidate.directExportName];

  return {
    symbolId: symbol.id,
    symbol,
    fileName: candidate.file.fileName,
    name: candidate.name,
    qualifiedName,
    kind: candidate.kind,
    start,
    end,
    startLine,
    endLine,
    signature: compactSignature(candidate.declaration, candidate.file),
    exported: directExportNames.length > 0,
    exportedNames: directExportNames,
  };
}

function isTypeSymbol(symbol: LocalSymbol): boolean {
  return symbol.kind === "interface" || symbol.kind === "type";
}

function compareSymbols(left: LocalSymbol, right: LocalSymbol): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.start - right.start ||
    left.end - right.end ||
    left.kind.localeCompare(right.kind) ||
    left.name.localeCompare(right.name)
  );
}

function compareExports(left: LocalExport, right: LocalExport): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.start - right.start ||
    left.end - right.end ||
    left.exportedName.localeCompare(right.exportedName) ||
    left.localName.localeCompare(right.localName)
  );
}

function resolvePath(fileName: string): string {
  return path.resolve(fileName);
}

function pathDirectory(fileName: string): string {
  return path.dirname(resolvePath(fileName));
}

function isWithin(fileName: string, root: string): boolean {
  return fileName === root || fileName.startsWith(`${root}${path.sep}`);
}
