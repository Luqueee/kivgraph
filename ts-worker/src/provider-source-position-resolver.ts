/**
 * Exact source positions for providers that publish no declaration map.
 *
 * When a provider ships `.d.ts` without `.d.ts.map`, LUQUE-0703 can still name
 * the source file, but no map places the symbol inside it. This resolver opens
 * the provider's own TypeScript project and asks *its* checker which symbol the
 * source module exports under the requested name. The answer comes from the
 * compiler of the repository that owns the code, never from name matching
 * across packages.
 */

import path from "node:path";

import { SymbolFlags } from "typescript/unstable/async";
import type {
  Checker,
  Symbol as TypeScriptSymbol,
} from "typescript/unstable/async";

import { declarationName } from "./declaration-name.js";
import { enginePath } from "./engine-path.js";
import type { SourcePosition } from "./declaration-position-mapper.js";
import type { ImportedSymbolResolution } from "./imported-symbol-resolver.js";
import { LanguageService, type ProjectView } from "./language-service.js";

/** One provider symbol located in its own source project. */
export interface ProviderSourcePosition {
  readonly declarationFile: string;
  readonly exportedName: string;
  readonly position: SourcePosition;
}

export interface ProviderSourcePositionResolution {
  readonly positions: readonly ProviderSourcePosition[];
  /** Requests the provider project could not place. */
  readonly unresolved: readonly string[];
}

export interface ProviderSourcePositionOptions {
  /** Override the bundled native executable, as `LanguageService` does. */
  tsserverPath?: string;
}

interface Request {
  readonly projectPath: string;
  readonly declarationFile: string;
  readonly exportedName: string;
  readonly sourceFiles: readonly string[];
}

/**
 * Locate, in the provider's own project, every imported symbol whose
 * declaration map could not place it.
 *
 * Symbols already placed by a declaration map are skipped: that answer is
 * cheaper and comes from the artifact itself.
 */
export async function resolveProviderSourcePositions(
  resolution: ImportedSymbolResolution,
  options: ProviderSourcePositionOptions = {},
): Promise<ProviderSourcePositionResolution> {
  const requests = collectRequests(resolution);
  if (requests.length === 0) {
    return { positions: [], unresolved: [] };
  }

  const positions: ProviderSourcePosition[] = [];
  const unresolved: string[] = [];
  const byProject = new Map<string, Request[]>();
  for (const request of requests) {
    const pending = byProject.get(request.projectPath);
    if (pending === undefined) {
      byProject.set(request.projectPath, [request]);
    } else {
      pending.push(request);
    }
  }

  for (const [projectPath, projectRequests] of [...byProject.entries()].sort(
    ([left], [right]) => left.localeCompare(right),
  )) {
    const service = LanguageService.create({
      cwd: path.dirname(projectPath),
      ...(options.tsserverPath === undefined
        ? {}
        : { tsserverPath: options.tsserverPath }),
    });
    try {
      await service.openProject(projectPath);
      const view = service.project(projectPath);
      for (const request of projectRequests) {
        const position = await locate(view, request);
        if (position === undefined) {
          unresolved.push(requestKey(request));
          continue;
        }
        positions.push({
          declarationFile: request.declarationFile,
          exportedName: request.exportedName,
          position,
        });
      }
    } finally {
      await service.close();
    }
  }

  positions.sort(
    (left, right) =>
      left.declarationFile.localeCompare(right.declarationFile) ||
      left.exportedName.localeCompare(right.exportedName),
  );
  return { positions, unresolved: unresolved.sort() };
}

/** The `provider` and `target` fields shared by imports and re-exports. */
type CrossRepositoryEdge = Pick<
  ImportedSymbolResolution["symbols"][number],
  "provider" | "target"
>;

function collectRequests(resolution: ImportedSymbolResolution): Request[] {
  const requests = new Map<string, Request>();
  const edges: readonly CrossRepositoryEdge[] = [
    ...resolution.symbols,
    ...resolution.reexports,
  ];
  for (const edge of edges) {
    const projectPath = edge.provider.projectPath;
    if (projectPath === undefined) {
      continue;
    }
    for (const declaration of edge.target.declarations) {
      if (
        declaration.sourcePosition !== undefined ||
        declaration.sourceFiles.length === 0
      ) {
        continue;
      }
      const request: Request = {
        projectPath,
        declarationFile: declaration.fileName,
        exportedName: edge.target.name,
        sourceFiles: declaration.sourceFiles,
      };
      requests.set(requestKey(request), request);
    }
  }
  return [...requests.values()];
}

async function locate(
  view: ProjectView,
  request: Request,
): Promise<SourcePosition | undefined> {
  for (const fileName of request.sourceFiles) {
    const sourceFile = await view.program.getSourceFile(fileName);
    if (sourceFile === undefined) {
      continue;
    }
    const moduleSymbol = await view.checker.getSymbolAtLocation(sourceFile);
    if (moduleSymbol === undefined) {
      continue;
    }
    const exported = await view.checker.getMemberInModuleExports(
      moduleSymbol,
      request.exportedName,
    );
    if (
      exported === undefined ||
      (await view.checker.isUnknownSymbol(exported))
    ) {
      continue;
    }
    const target = await resolveAlias(view.checker, exported);
    if (target === undefined) {
      continue;
    }
    for (const handle of target.declarations) {
      const declarationFile = await view.program.getSourceFile(handle.path);
      const node = await handle.resolve(view.project);
      if (declarationFile === undefined || node === undefined) {
        continue;
      }
      const nameNode = declarationName(node) ?? node;
      const position = declarationFile.getLineAndCharacterOfPosition(
        nameNode.getStart(declarationFile),
      );
      return {
        // handle.path is the engine's canonical spelling of a file it
        // resolved; the position is reported against the path on disk.
        fileName: enginePath(handle.path),
        line: position.line + 1,
        character: position.character,
      };
    }
  }
  return undefined;
}

async function resolveAlias(
  checker: Checker,
  symbol: TypeScriptSymbol,
): Promise<TypeScriptSymbol | undefined> {
  if ((symbol.flags & SymbolFlags.Alias) === 0) {
    return symbol;
  }
  const target = await checker.getAliasedSymbol(symbol);
  return (await checker.isUnknownSymbol(target)) ? undefined : target;
}

function requestKey(request: Request): string {
  return `${request.declarationFile}#${request.exportedName}`;
}
