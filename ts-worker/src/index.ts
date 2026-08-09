import { realpathSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { createInterface } from "node:readline";

export { extractLocalSymbols } from "./symbol-extractor.js";
export type {
  LocalExport,
  LocalSymbol,
  LocalSymbolExtraction,
  LocalSymbolKind,
  SymbolExtractionOptions,
} from "./symbol-extractor.js";
export { extractLocalReferences } from "./reference-extractor.js";
export type {
  ImportBindingSymbol,
  LocalReference,
  LocalReferenceExtraction,
  LocalReferenceKind,
  ReferenceExtractionOptions,
} from "./reference-extractor.js";

export { resolvePackageImports } from "./package-import-resolver.js";
export type {
  PackageExportMode,
  PackageImport,
  PackageImportResolution,
  PackageImportResolutionOptions,
  PackageImportStatus,
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
export { resolveProviderExports } from "./provider-export-resolver.js";
export type {
  ProviderExport,
  ProviderExportResolution,
  ProviderExportStatus,
} from "./provider-export-resolver.js";
export { resolveDeclarationSources } from "./declaration-source-resolver.js";
export type {
  DeclarationSourceMapping,
  DeclarationSourceResolution,
  DeclarationSourceStatus,
} from "./declaration-source-resolver.js";

export {
  createPackageDependencies,
  resolvePackageDependencies,
} from "./package-dependency-resolver.js";
export type {
  PackageDependency,
  PackageDependencyImport,
  PackageDependencyResolution,
} from "./package-dependency-resolver.js";

export { resolveImportedSymbols } from "./imported-symbol-resolver.js";
export type {
  ImportedSymbol,
  ImportedSymbolConsumer,
  ImportedSymbolDeclaration,
  ImportedSymbolResolution,
  ImportedSymbolTarget,
  ReexportedSymbol,
  ReexportedSymbolExport,
} from "./imported-symbol-resolver.js";

export { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";
export type {
  PackageProviderConflict,
  UnresolvedReference,
  UnresolvedReferenceOptions,
  UnresolvedReferenceReason,
  UnresolvedReferenceResolution,
} from "./unresolved-reference-resolver.js";

export {
  DeclarationPositionMapper,
  decodeMappings,
} from "./declaration-position-mapper.js";
export type {
  DeclarationMapSegment,
  SourcePosition,
} from "./declaration-position-mapper.js";
export { loadDeclarationSourceMap } from "./declaration-source-resolver.js";
export type { DeclarationSourceMap } from "./declaration-source-resolver.js";

export { resolveProviderSourcePositions } from "./provider-source-position-resolver.js";
export type {
  ProviderSourcePosition,
  ProviderSourcePositionOptions,
  ProviderSourcePositionResolution,
} from "./provider-source-position-resolver.js";
export { declarationName } from "./declaration-name.js";

export function handleCommand(command: string): string {
  if (command.trim() === "hello") {
    return "hello";
  }

  throw new Error(`unknown command: ${command.trim()}`);
}

export async function run(
  stdin: NodeJS.ReadableStream,
  stdout: NodeJS.WritableStream,
): Promise<number> {
  const input = createInterface({ input: stdin });

  try {
    for await (const line of input) {
      if (line.trim() === "") {
        continue;
      }

      try {
        stdout.write(`${handleCommand(line)}\n`);
      } catch (error: unknown) {
        const message =
          error instanceof Error ? error.message : "unknown error";
        stdout.write(`error: ${message}\n`);
        return 1;
      }
    }

    return 0;
  } finally {
    input.close();
  }
}

// isEntryPoint decides whether this module was started as a program or
// imported as a library. Comparing pathToFileURL(process.argv[1]) alone is not
// enough: Node resolves the main module through realpath, so any invocation
// path with a symlinked component - every path under /tmp or /var on macOS,
// and any symlinked install root - produced a silent exit instead of the
// protocol. Both forms are accepted because --preserve-symlinks-main keeps the
// logical path in import.meta.url.
export function isEntryPoint(
  entry: string | undefined,
  moduleURL: string,
): boolean {
  if (!entry) {
    return false;
  }
  if (pathToFileURL(entry).href === moduleURL) {
    return true;
  }
  try {
    return pathToFileURL(realpathSync(entry)).href === moduleURL;
  } catch {
    return false;
  }
}

if (isEntryPoint(process.argv[1], import.meta.url)) {
  process.exitCode = await run(process.stdin, process.stdout);
}
