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
} from "./imported-symbol-resolver.js";

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

if (
  process.argv[1] &&
  pathToFileURL(process.argv[1]).href === import.meta.url
) {
  process.exitCode = await run(process.stdin, process.stdout);
}
