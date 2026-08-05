/**
 * Emit the canonical fact payload of one TypeScript repository.
 *
 * The payload is the wire contract `ts-facts-v1` consumed by
 * `internal/facts`: the worker reports identity components and positions, and
 * Go derives the durable keys. Nothing here computes a key, so both languages
 * cannot drift into two identities for one symbol.
 *
 * Usage:
 *
 *   pnpm facts <repository-name> <repository-root> <output.json>
 */

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

import { LanguageService } from "./language-service.js";
import { extractLocalReferences } from "./reference-extractor.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";
import type { PackageProviderRegistry } from "./package-import-resolver.js";

interface FactsPayload {
  readonly version: 1;
  readonly repository: { readonly name: string };
  readonly package: {
    readonly name: string;
    readonly version: string;
    readonly rootPath: string;
    readonly manifestPath: string;
  } | null;
  readonly files: readonly string[];
  readonly symbols: readonly FactSymbol[];
  readonly references: readonly FactReference[];
  readonly unresolved: readonly FactUnresolved[];
}

interface FactSymbol {
  readonly file: string;
  readonly name: string;
  readonly qualifiedName: string;
  readonly kind: string;
  readonly exported: boolean;
  readonly signature: string;
  readonly startLine: number;
  readonly endLine: number;
  readonly start: number;
  readonly end: number;
}

interface FactReference {
  readonly file: string;
  readonly kind: string;
  readonly sourceQualifiedName: string | null;
  readonly targetQualifiedName: string;
  readonly targetFile: string;
  readonly startLine: number;
  readonly start: number;
  readonly end: number;
  readonly text: string;
}

interface FactUnresolved {
  readonly file: string;
  readonly reason: string;
  readonly requestedPackage: string;
  readonly requestedSymbol: string | null;
  readonly detail: string | null;
  readonly start: number;
}

const emptyRegistry: PackageProviderRegistry = { get: () => undefined };

export async function collectFacts(
  repositoryName: string,
  repositoryRoot: string,
): Promise<FactsPayload> {
  const root = path.resolve(repositoryRoot);
  const configFileName = path.join(root, "tsconfig.json");
  const service = LanguageService.create({ cwd: root });
  try {
    await service.openProject(configFileName);
    const view = service.project(configFileName);

    const symbols = await extractLocalSymbols(service, view);
    const references = await extractLocalReferences(service, view, symbols);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      emptyRegistry,
    );

    const files = [
      ...new Set(symbols.symbols.map((symbol) => symbol.fileName)),
    ].sort();

    return {
      version: 1,
      repository: { name: repositoryName },
      package: await readPackage(root),
      files: files.map((file) => relative(root, file)),
      symbols: symbols.symbols.map((symbol) => ({
        file: relative(root, symbol.fileName),
        name: symbol.name,
        qualifiedName: symbol.qualifiedName,
        kind: symbol.kind,
        exported: symbol.exported,
        signature: symbol.signature,
        startLine: symbol.startLine,
        endLine: symbol.endLine,
        start: symbol.start,
        end: symbol.end,
      })),
      references: references.references.map((reference) => ({
        file: relative(root, reference.fileName),
        kind: reference.kind,
        sourceQualifiedName: reference.source?.qualifiedName ?? null,
        targetQualifiedName: reference.target.qualifiedName,
        targetFile: relative(root, reference.target.fileName),
        startLine: reference.startLine,
        start: reference.start,
        end: reference.end,
        text: reference.text,
      })),
      unresolved: resolution.unresolved.map((entry) => ({
        file: relative(root, entry.fileName),
        reason: entry.reason,
        requestedPackage: entry.packageName,
        requestedSymbol: entry.requestedSymbol ?? null,
        detail: entry.detail ?? null,
        start: entry.start,
      })),
    };
  } finally {
    await service.close();
  }
}

async function readPackage(root: string): Promise<FactsPayload["package"]> {
  const manifestPath = path.join(root, "package.json");
  try {
    const contents = await readFile(manifestPath, "utf8");
    const parsed: unknown = JSON.parse(contents);
    if (typeof parsed !== "object" || parsed === null) {
      return null;
    }
    const manifest = parsed as { name?: unknown; version?: unknown };
    if (typeof manifest.name !== "string" || manifest.name === "") {
      return null;
    }
    return {
      name: manifest.name,
      version: typeof manifest.version === "string" ? manifest.version : "",
      // Repository relative, so a recorded payload stays portable.
      rootPath: ".",
      manifestPath: "package.json",
    };
  } catch {
    return null;
  }
}

function relative(root: string, file: string): string {
  return path.relative(root, file).split(path.sep).join("/");
}

const [repositoryName, repositoryRoot, output] = process.argv.slice(2);
if (!repositoryName || !repositoryRoot || !output) {
  process.stderr.write(
    "usage: pnpm facts <repository-name> <repository-root> <output.json>\n",
  );
  process.exitCode = 1;
} else {
  const payload = await collectFacts(repositoryName, repositoryRoot);
  await mkdir(path.dirname(path.resolve(output)), { recursive: true });
  await writeFile(
    path.resolve(output),
    `${JSON.stringify(payload, null, 2)}\n`,
    "utf8",
  );
  process.stdout.write(
    `${payload.symbols.length} symbols, ${payload.references.length} references\n`,
  );
}
