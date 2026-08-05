import path from "node:path";

import {
  isCallExpression,
  isExportDeclaration,
  isExternalModuleReference,
  isImportDeclaration,
  isImportEqualsDeclaration,
  isImportTypeNode,
  isLiteralTypeNode,
  isStringLiteral,
} from "typescript/unstable/ast/is";
import { SyntaxKind } from "typescript/unstable/ast";
import { SymbolFlags } from "typescript/unstable/async";
import type {
  Checker,
  Symbol as TypeScriptSymbol,
} from "typescript/unstable/async";
import type { Node, SourceFile } from "typescript/unstable/ast";

import type { LanguageService, ProjectView } from "./language-service.js";

/** Metadata supplied by the Go package registry for one provider package. */
export interface PackageProvider {
  readonly name: string;
  readonly version: string;
  readonly repository: string;
  readonly rootPath: string;
}

/** Read-only package-name lookup shared with the workspace registry. */
export interface PackageProviderRegistry {
  get(name: string): PackageProvider | undefined;
}

export type PackageImportStatus =
  | "RESOLVED"
  | "PACKAGE_PROVIDER_NOT_FOUND"
  | "MODULE_NOT_RESOLVED";

/** One package import occurrence and its checker-backed resolution. */
export interface PackageImport {
  fileName: string;
  specifier: string;
  packageName: string;
  status: PackageImportStatus;
  provider: PackageProvider | undefined;
  resolvedFiles: readonly string[];
  start: number;
  end: number;
}

export interface PackageImportResolutionOptions {
  /** Relative to the project directory; omitted means every project file. */
  files?: readonly string[];
}

export interface PackageImportResolution {
  generation: number;
  configFileName: string;
  imports: readonly PackageImport[];
}

interface ImportRequest {
  file: SourceFile;
  node: Node;
  packageName: string;
  specifier: string;
}

/**
 * Resolve package imports from one live TypeScript project.
 *
 * The native checker decides whether the module specifier resolves. The
 * package registry supplies the cross-repository provider identity; neither
 * result is inferred from a matching filename or symbol name.
 */
export async function resolvePackageImports(
  service: LanguageService,
  view: ProjectView,
  registry: PackageProviderRegistry,
  options: PackageImportResolutionOptions = {},
): Promise<PackageImportResolution> {
  service.assertFresh(view);
  const localFiles = await selectProjectFiles(view, options.files);
  const requests: ImportRequest[] = [];

  for (const fileName of localFiles) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (
      sourceFile === undefined ||
      (await view.program.isSourceFileFromExternalLibrary(sourceFile))
    ) {
      continue;
    }
    collectPackageImports(sourceFile, requests);
  }

  const symbols =
    requests.length === 0
      ? []
      : await view.checker.getSymbolAtLocation(
          requests.map((request) => request.node),
        );
  const imports = await Promise.all(
    requests.map(async (request, index) => {
      const symbol = await resolveModuleSymbol(view.checker, symbols[index]);
      const resolvedFiles = moduleDeclarationFiles(symbol);
      const provider = cloneProvider(registry.get(request.packageName));
      let status: PackageImportStatus;
      if (resolvedFiles.length === 0) {
        status = "MODULE_NOT_RESOLVED";
      } else if (provider === undefined) {
        status = "PACKAGE_PROVIDER_NOT_FOUND";
      } else {
        status = "RESOLVED";
      }
      return {
        fileName: request.file.fileName,
        specifier: request.specifier,
        packageName: request.packageName,
        status,
        provider,
        resolvedFiles,
        start: request.node.getStart(request.file),
        end: request.node.getEnd(),
      } satisfies PackageImport;
    }),
  );

  service.assertFresh(view);
  imports.sort(comparePackageImports);
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    imports,
  };
}

async function resolveModuleSymbol(
  checker: Checker,
  symbol: TypeScriptSymbol | undefined,
): Promise<TypeScriptSymbol | undefined> {
  if (symbol === undefined || (symbol.flags & SymbolFlags.Alias) === 0) {
    return symbol;
  }
  const target = await checker.getAliasedSymbol(symbol);
  return (await checker.isUnknownSymbol(target)) ? undefined : target;
}

function moduleDeclarationFiles(
  symbol: TypeScriptSymbol | undefined,
): string[] {
  if (symbol === undefined) {
    return [];
  }
  return [
    ...new Set(symbol.declarations.map((declaration) => declaration.path)),
  ].sort();
}

function cloneProvider(
  provider: PackageProvider | undefined,
): PackageProvider | undefined {
  return provider === undefined ? undefined : { ...provider };
}

async function selectProjectFiles(
  view: ProjectView,
  requested: readonly string[] | undefined,
): Promise<string[]> {
  const projectRoot = path.dirname(path.resolve(view.configFileName));
  const sourceFileNames = await view.program.getSourceFileNames();
  const requestedSet =
    requested === undefined
      ? undefined
      : new Set(
          requested.map((fileName) =>
            resolveRequestedFile(projectRoot, fileName),
          ),
        );
  return sourceFileNames
    .map((fileName) => path.resolve(fileName))
    .filter((fileName) => isWithin(projectRoot, fileName))
    .filter(
      (fileName) => requestedSet === undefined || requestedSet.has(fileName),
    )
    .sort();
}

function resolveRequestedFile(projectRoot: string, fileName: string): string {
  return path.isAbsolute(fileName)
    ? path.resolve(fileName)
    : path.resolve(projectRoot, fileName);
}

function collectPackageImports(
  file: SourceFile,
  requests: ImportRequest[],
): void {
  const visit = (node: Node): void => {
    if (isImportDeclaration(node)) {
      addSpecifier(file, node.moduleSpecifier, requests);
    } else if (
      isExportDeclaration(node) &&
      node.moduleSpecifier !== undefined
    ) {
      addSpecifier(file, node.moduleSpecifier, requests);
    } else if (
      isImportEqualsDeclaration(node) &&
      isExternalModuleReference(node.moduleReference)
    ) {
      addSpecifier(file, node.moduleReference.expression, requests);
    } else if (isImportTypeNode(node)) {
      addSpecifier(file, node.argument, requests);
    } else if (
      isCallExpression(node) &&
      node.expression.kind === SyntaxKind.ImportKeyword &&
      node.arguments.length === 1
    ) {
      const argument = node.arguments[0];
      if (argument !== undefined) {
        addSpecifier(file, argument, requests);
      }
    }
    node.forEachChild(visit);
  };

  file.forEachChild(visit);
}

function addSpecifier(
  file: SourceFile,
  node: Node,
  requests: ImportRequest[],
): void {
  const literal = stringLiteralFromNode(node);
  if (literal === undefined) {
    return;
  }
  const packageName = packageNameFromSpecifier(literal.specifier);
  if (packageName === undefined) {
    return;
  }
  requests.push({
    file,
    node: literal.node,
    packageName,
    specifier: literal.specifier,
  });
}

function stringLiteralFromNode(
  node: Node,
): { node: Node; specifier: string } | undefined {
  if (isStringLiteral(node)) {
    return { node, specifier: node.text };
  }
  if (isLiteralTypeNode(node) && isStringLiteral(node.literal)) {
    return { node: node.literal, specifier: node.literal.text };
  }
  return undefined;
}

function packageNameFromSpecifier(specifier: string): string | undefined {
  const value = specifier.trim();
  if (
    value === "" ||
    value.startsWith(".") ||
    value.startsWith("/") ||
    value.startsWith("#") ||
    value.startsWith("node:") ||
    /^[A-Za-z]:[\\/]/.test(value)
  ) {
    return undefined;
  }
  const parts = value.split("/");
  if (value.startsWith("@")) {
    return parts.length >= 2 && parts[0] !== "@" && parts[1] !== ""
      ? `${parts[0]}/${parts[1]}`
      : undefined;
  }
  return parts[0] === "" ? undefined : parts[0];
}

function comparePackageImports(
  left: PackageImport,
  right: PackageImport,
): number {
  return (
    left.fileName.localeCompare(right.fileName) ||
    left.start - right.start ||
    left.end - right.end ||
    left.specifier.localeCompare(right.specifier)
  );
}

function isWithin(root: string, candidate: string): boolean {
  const relative = path.relative(root, candidate);
  return (
    relative === "" ||
    (relative !== ".." &&
      !relative.startsWith(`..${path.sep}`) &&
      !path.isAbsolute(relative))
  );
}
