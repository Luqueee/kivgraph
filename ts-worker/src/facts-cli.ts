/**
 * Emit the canonical fact payload of one TypeScript repository.
 *
 * The payload is the wire contract `ts-facts-v3` consumed by
 * `internal/facts`: the worker reports identity components and positions, and
 * Go derives the durable keys. Nothing here computes a key, so both languages
 * cannot drift into two identities for one symbol.
 *
 * `imports` carries the provider identity LUQUE-0907 adds: for each exact
 * `IMPORTS_SYMBOL` edge the worker can prove, the target's repository,
 * package, qualified name, kind and signature come from parsing the
 * *provider's own source*, never from the `.d.ts` text — so a cross-repository
 * edge is byte-identical to the key the provider would assign itself. An edge
 * without that proof still gets a `FactImport` (with `target: null` and a
 * `reason`) plus a matching `unresolved` entry, never a guess.
 *
 * `exports` is the same idea turned outward: the public name a module exposes
 * a declaration under is its own "export" kind symbol, and it reaches the
 * declaration through `EXPORTS` when the declaration is defined here without
 * a `from` clause, or `REEXPORTS` when a `from` clause introduced it — the
 * same clause can point at this repository or at another one, and a
 * cross-repository `REEXPORTS` target carries the identical provider-source
 * identity an import does, with the same fallback to `target: null` plus an
 * `unresolved` entry when that identity cannot be proven.
 *
 * Usage:
 *
 *   pnpm facts <repository-name> <repository-root> <output.json> \
 *     [--provider <name>=<path>]...
 *
 * `--provider <name>=<path>` declares one provider repository this indexing
 * run may import from: `<name>` is the repository name (as Luque names it,
 * not the npm package name) and `<path>` is its root directory. The CLI
 * derives the provider's package name, version and source/declaration roots
 * by reading `<path>/package.json` and `<path>/tsconfig.json` — the same
 * shape `cross-repository-positive.test.ts` builds by hand for its registry.
 * Repeat the flag once per provider repository.
 *
 * Regenerate the `ts-facts-v3` goldens, from `ts-worker/`:
 *
 *   pnpm facts shared-library ../testdata/typescript/cross-repository/shared-library \
 *     ../testdata/protocol/ts-facts-v3/shared-library.json
 *   pnpm facts consumer-a ../testdata/typescript/cross-repository/consumer-a \
 *     ../testdata/protocol/ts-facts-v3/consumer-a.json \
 *     --provider shared-library=../testdata/typescript/cross-repository/shared-library
 *   pnpm facts consumer-b ../testdata/typescript/cross-repository/consumer-b \
 *     ../testdata/protocol/ts-facts-v3/consumer-b.json \
 *     --provider shared-library=../testdata/typescript/cross-repository/shared-library
 */

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

import type { ImportBindingSymbol } from "./reference-extractor.js";
import type {
  ImportedSymbol,
  ReexportedSymbol,
} from "./imported-symbol-resolver.js";
import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import { extractLocalReferences } from "./reference-extractor.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import type { LocalExport } from "./symbol-extractor.js";
import { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";

interface FactsPayload {
  readonly version: 3;
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
  readonly imports: readonly FactImport[];
  readonly exports: readonly FactExport[];
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

/** One import binding of the consumer, and the provider symbol it reaches. */
interface FactImport {
  /** Consumer file, repository relative. */
  readonly file: string;
  /** Qualified name of the import binding symbol emitted in `symbols`. */
  readonly qualifiedName: string;
  /** UTF-16 offsets of the binding, `end` exclusive. */
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  /** Source text of the binding, the evidence of the edge. */
  readonly text: string;
  /** Package the binding imports from. */
  readonly requestedPackage: string;
  /** Public name requested from the provider module. */
  readonly requestedSymbol: string;
  /** Identity of the provider declaration, or null when it is not exactly knowable. */
  readonly target: FactImportTarget | null;
  /** Why the target is null. Null when there is a target. */
  readonly reason: string | null;
  readonly detail: string | null;
}

/** The provider declaration, described exactly as the provider indexes it. */
interface FactImportTarget {
  /** Provider repository, as Luque names it. */
  readonly repository: string;
  /** Provider package name. */
  readonly package: string;
  /** Qualified name computed from the provider source, never from the .d.ts. */
  readonly qualifiedName: string;
  /** Class computed from the provider source declaration. */
  readonly kind: string;
  /** Compact signature of the provider source declaration. */
  readonly signature: string;
  /** Provider source file, relative to the provider repository root. */
  readonly file: string;
  readonly startLine: number;
}

/**
 * One export or re-export binding of the file, and the declaration it
 * exposes. `EXPORTS` always resolves within `targetQualifiedName` /
 * `targetFile`, a declaration already present in this payload's `symbols`.
 * `REEXPORTS` resolves that way when the `from` clause stays inside this
 * repository, or through `target` — the same provider-source identity an
 * import carries — when it crosses into another one.
 */
interface FactExport {
  readonly file: string;
  readonly kind: "EXPORTS" | "REEXPORTS";
  /** Qualified name of the export binding symbol emitted in `symbols`. */
  readonly qualifiedName: string;
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  /** Source text of the export site, the evidence of the edge. */
  readonly text: string;
  /** Local declaration this exposes, when it lives in this repository. */
  readonly targetQualifiedName: string | null;
  readonly targetFile: string | null;
  /** Provider identity of a cross-repository target, or null when local. */
  readonly target: FactImportTarget | null;
  /** Package requested from a `from` clause, null for a local target. */
  readonly requestedPackage: string | null;
  readonly requestedSymbol: string | null;
  /** Why a cross-repository target has no identity. Null otherwise. */
  readonly reason: string | null;
  readonly detail: string | null;
}

interface FactUnresolved {
  readonly file: string;
  readonly reason: string;
  readonly requestedPackage: string;
  readonly requestedSymbol: string | null;
  readonly detail: string | null;
  readonly start: number;
}

export async function collectFacts(
  repositoryName: string,
  repositoryRoot: string,
  registry: PackageProviderRegistry,
): Promise<FactsPayload> {
  const root = path.resolve(repositoryRoot);
  const configFileName = path.join(root, "tsconfig.json");
  const service = LanguageService.create({ cwd: root });
  try {
    await service.openProject(configFileName);
    const view = service.project(configFileName);

    const symbols = await extractLocalSymbols(service, view);
    // Resolved first: reference extraction needs the "import" bindings this
    // produces so a use that never resolves to a genuine local declaration
    // can still target the binding itself, exactly as `imports` does.
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registry,
    );
    const importSymbols = importFactSymbols(root, resolution.symbols);
    // An export's public name frequently repeats the local declaration it
    // exposes (`export function foo() {}` names both "foo"), unlike an
    // import's local name, which TypeScript's own scoping already keeps
    // unique against every other binding in the file. Reserving every
    // qualified name already claimed by a declaration or an import keeps
    // `exportFactSymbols` from minting one that collides and silently
    // shadows the real symbol in Go's file+qualifiedName lookup.
    const reservedQualifiedNames = new Set<string>();
    for (const local of symbols.symbols) {
      reservedQualifiedNames.add(
        `${local.fileName}\u0000${local.qualifiedName}`,
      );
    }
    for (const entry of resolution.symbols) {
      const qualifiedName =
        importSymbols.qualifiedNames.get(entry) ?? entry.consumer.name;
      reservedQualifiedNames.add(
        `${entry.consumer.fileName}\u0000${qualifiedName}`,
      );
    }
    const exportSymbols = exportFactSymbols(
      root,
      symbols.exports,
      resolution.reexports,
      reservedQualifiedNames,
    );
    const references = await extractLocalReferences(service, view, symbols, {
      importBindings: importSymbols.referenceTargets,
    });

    const files = [
      ...new Set([
        ...symbols.symbols.map((symbol) => symbol.fileName),
        ...symbols.exports.map((entry) => entry.fileName),
        ...resolution.symbols.map((entry) => entry.consumer.fileName),
        ...resolution.reexports.map((entry) => entry.export.fileName),
      ]),
    ].sort();

    const unresolved = [
      ...resolution.unresolved.map(
        (entry): FactUnresolved => ({
          file: relative(root, entry.fileName),
          reason: entry.reason,
          requestedPackage: entry.packageName,
          requestedSymbol: entry.requestedSymbol ?? null,
          detail: entry.detail ?? null,
          start: entry.start,
        }),
      ),
      // A target without a provider identity is not a dangling edge: it is
      // reported as an import with `target: null` below *and* here, so a
      // reader that only scans `unresolved` still sees it.
      ...resolution.symbols
        .filter((entry) => entry.target.identity === undefined)
        .map(
          (entry): FactUnresolved => ({
            file: relative(root, entry.consumer.fileName),
            reason:
              entry.target.identityReason ?? "PROVIDER_SOURCE_UNAVAILABLE",
            requestedPackage: entry.packageName,
            requestedSymbol: entry.exportedName,
            detail: entry.target.identityDetail ?? null,
            start: entry.consumer.start,
          }),
        ),
      ...exportSymbols.unresolved,
    ].sort(compareUnresolved);

    return {
      version: 3,
      repository: { name: repositoryName },
      package: await readPackage(root),
      files: files.map((file) => relative(root, file)),
      symbols: [
        ...symbols.symbols.map(
          (symbol): FactSymbol => ({
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
          }),
        ),
        ...importSymbols.symbols,
        ...exportSymbols.symbols,
      ],
      references: references.references.map(
        (reference): FactReference => ({
          file: relative(root, reference.fileName),
          kind: reference.kind,
          sourceQualifiedName: reference.source?.qualifiedName ?? null,
          targetQualifiedName: reference.target.qualifiedName,
          targetFile: relative(root, reference.target.fileName),
          startLine: reference.startLine,
          start: reference.start,
          end: reference.end,
          text: reference.text,
        }),
      ),
      imports: resolution.symbols.map((entry): FactImport => {
        const identity = entry.target.identity;
        return {
          file: relative(root, entry.consumer.fileName),
          qualifiedName:
            importSymbols.qualifiedNames.get(entry) ?? entry.consumer.name,
          start: entry.consumer.start,
          end: entry.consumer.end,
          startLine: entry.consumer.startLine,
          text: entry.consumer.text,
          requestedPackage: entry.packageName,
          requestedSymbol: entry.exportedName,
          target:
            identity === undefined
              ? null
              : {
                  repository: identity.repository,
                  package: identity.package,
                  qualifiedName: identity.qualifiedName,
                  kind: identity.kind,
                  signature: identity.signature,
                  file: identity.file,
                  startLine: identity.startLine,
                },
          reason: entry.target.identityReason ?? null,
          detail: entry.target.identityDetail ?? null,
        };
      }),
      exports: exportSymbols.exports,
      unresolved,
    };
  } finally {
    await service.close();
  }
}

/**
 * Turn every exact import edge into the consumer-side `FactSymbol` its
 * `FactImport` needs as an origin, so the edge is `Symbol -> Symbol` as the
 * canonical schema requires.
 *
 * A qualified name collides only when two bindings share a file and a local
 * name — a re-export alongside a same-named import, for instance. The first
 * keeps the plain name; the rest are disambiguated deterministically so every
 * `FactImport.qualifiedName` still resolves to exactly one `FactSymbol`.
 *
 * `referenceTargets` restates the same bindings as `ImportBindingSymbol`s, so
 * `reference-extractor.ts` can target one when a use never resolves to a
 * genuine local declaration — the qualified name there must be byte-identical
 * to the one on `symbols`, since both name the very same graph node.
 */
function importFactSymbols(
  root: string,
  imports: readonly ImportedSymbol[],
): {
  readonly symbols: readonly FactSymbol[];
  readonly qualifiedNames: ReadonlyMap<ImportedSymbol, string>;
  readonly referenceTargets: readonly ImportBindingSymbol[];
} {
  const occurrences = new Map<string, number>();
  const qualifiedNames = new Map<ImportedSymbol, string>();
  const symbols: FactSymbol[] = [];
  const referenceTargets: ImportBindingSymbol[] = [];

  for (const entry of imports) {
    const key = `${entry.consumer.fileName}\u0000${entry.consumer.name}`;
    const occurrence = (occurrences.get(key) ?? 0) + 1;
    occurrences.set(key, occurrence);
    const qualifiedName =
      occurrence === 1
        ? entry.consumer.name
        : `${entry.consumer.name}#${occurrence}`;
    qualifiedNames.set(entry, qualifiedName);
    symbols.push({
      file: relative(root, entry.consumer.fileName),
      name: entry.consumer.name,
      qualifiedName,
      kind: "import",
      exported: false,
      signature: entry.consumer.text,
      startLine: entry.consumer.startLine,
      endLine: entry.consumer.endLine,
      start: entry.consumer.start,
      end: entry.consumer.end,
    });
    referenceTargets.push({
      symbolId: entry.consumer.symbolId,
      fileName: entry.consumer.fileName,
      name: entry.consumer.name,
      qualifiedName,
      start: entry.consumer.start,
      end: entry.consumer.end,
      startLine: entry.consumer.startLine,
      endLine: entry.consumer.endLine,
    });
  }

  return { symbols, qualifiedNames, referenceTargets };
}

/**
 * Turn every local export and every exact cross-repository re-export into
 * the public-name `FactSymbol` decision 1 requires, plus the `FactExport`
 * edge from it to the declaration it exposes.
 *
 * `locals` (same repository, computed by `symbol-extractor.ts`) and
 * `reexports` (another repository, computed by `imported-symbol-resolver.ts`)
 * never name the same binding: a relative specifier never becomes a
 * `PackageImport`, so the two sources partition every `export`/`export …
 * from` statement in the file without overlap. Qualified names are
 * disambiguated by public name, never by the local name they expose — one
 * declaration re-exported under two public names must not collide just
 * because the underlying local name repeats.
 *
 * `reservedQualifiedNames` additionally keeps a public name from colliding
 * with a declaration or an import that already claims it in the same file —
 * `export function foo() {}` names both the function and its export "foo",
 * unlike an import's local name, which TypeScript's own scoping already
 * keeps unique against every other binding in the file.
 */
function exportFactSymbols(
  root: string,
  locals: readonly LocalExport[],
  reexports: readonly ReexportedSymbol[],
  reservedQualifiedNames: ReadonlySet<string>,
): {
  readonly symbols: readonly FactSymbol[];
  readonly exports: readonly FactExport[];
  readonly unresolved: readonly FactUnresolved[];
} {
  const occurrences = new Map<string, number>();
  const symbols: FactSymbol[] = [];
  const exports: FactExport[] = [];
  const unresolved: FactUnresolved[] = [];

  const qualify = (fileName: string, exportedName: string): string => {
    const key = `${fileName}\u0000${exportedName}`;
    let occurrence = occurrences.get(key) ?? 0;
    let candidate: string;
    do {
      occurrence += 1;
      candidate =
        occurrence === 1 ? exportedName : `${exportedName}#${occurrence}`;
    } while (reservedQualifiedNames.has(`${fileName}\u0000${candidate}`));
    occurrences.set(key, occurrence);
    return candidate;
  };

  for (const local of locals) {
    const qualifiedName = qualify(local.fileName, local.exportedName);
    symbols.push({
      file: relative(root, local.fileName),
      name: local.exportedName,
      qualifiedName,
      kind: "export",
      exported: true,
      signature: local.text,
      startLine: local.startLine,
      endLine: local.endLine,
      start: local.start,
      end: local.end,
    });
    exports.push({
      file: relative(root, local.fileName),
      kind: local.reExport ? "REEXPORTS" : "EXPORTS",
      qualifiedName,
      start: local.start,
      end: local.end,
      startLine: local.startLine,
      text: local.text,
      targetQualifiedName: local.targetQualifiedName,
      targetFile: relative(root, local.targetFile),
      target: null,
      requestedPackage: null,
      requestedSymbol: null,
      reason: null,
      detail: null,
    });
  }

  for (const entry of reexports) {
    const qualifiedName = qualify(entry.export.fileName, entry.export.name);
    const identity = entry.target.identity;
    symbols.push({
      file: relative(root, entry.export.fileName),
      name: entry.export.name,
      qualifiedName,
      kind: "export",
      exported: true,
      signature: entry.export.text,
      startLine: entry.export.startLine,
      endLine: entry.export.endLine,
      start: entry.export.start,
      end: entry.export.end,
    });
    exports.push({
      file: relative(root, entry.export.fileName),
      kind: "REEXPORTS",
      qualifiedName,
      start: entry.export.start,
      end: entry.export.end,
      startLine: entry.export.startLine,
      text: entry.export.text,
      targetQualifiedName: null,
      targetFile: null,
      target:
        identity === undefined
          ? null
          : {
              repository: identity.repository,
              package: identity.package,
              qualifiedName: identity.qualifiedName,
              kind: identity.kind,
              signature: identity.signature,
              file: identity.file,
              startLine: identity.startLine,
            },
      requestedPackage: entry.packageName,
      requestedSymbol: entry.exportedName,
      reason: entry.target.identityReason ?? null,
      detail: entry.target.identityDetail ?? null,
    });
    if (identity === undefined) {
      unresolved.push({
        file: relative(root, entry.export.fileName),
        reason: entry.target.identityReason ?? "PROVIDER_SOURCE_UNAVAILABLE",
        requestedPackage: entry.packageName,
        requestedSymbol: entry.exportedName,
        detail: entry.target.identityDetail ?? null,
        start: entry.export.start,
      });
    }
  }

  return { symbols, exports, unresolved };
}

function compareUnresolved(
  left: FactUnresolved,
  right: FactUnresolved,
): number {
  return (
    left.file.localeCompare(right.file) ||
    left.start - right.start ||
    left.reason.localeCompare(right.reason) ||
    (left.requestedSymbol ?? "").localeCompare(right.requestedSymbol ?? "")
  );
}

async function readPackage(root: string): Promise<FactsPayload["package"]> {
  const manifest = await readManifest(path.join(root, "package.json"));
  if (manifest === undefined) {
    return null;
  }
  return {
    name: manifest.name,
    version: manifest.version,
    // Repository relative, so a recorded payload stays portable.
    rootPath: ".",
    manifestPath: "package.json",
  };
}

/** The subset of a `package.json` the worker ever reads. */
interface Manifest {
  readonly name: string;
  readonly version: string;
  readonly types: string | undefined;
}

async function readManifest(
  manifestPath: string,
): Promise<Manifest | undefined> {
  try {
    const contents = await readFile(manifestPath, "utf8");
    const parsed: unknown = JSON.parse(contents);
    if (typeof parsed !== "object" || parsed === null) {
      return undefined;
    }
    const manifest = parsed as {
      name?: unknown;
      version?: unknown;
      types?: unknown;
      typings?: unknown;
    };
    if (typeof manifest.name !== "string" || manifest.name === "") {
      return undefined;
    }
    const types = manifest.types ?? manifest.typings;
    return {
      name: manifest.name,
      version: typeof manifest.version === "string" ? manifest.version : "",
      types: typeof types === "string" && types !== "" ? types : undefined,
    };
  } catch {
    return undefined;
  }
}

/** The `compilerOptions` hints used to derive a provider's roots. */
interface CompilerOptionsHints {
  readonly rootDir: string | undefined;
  readonly outDir: string | undefined;
  readonly declarationDir: string | undefined;
}

async function readCompilerOptions(
  projectPath: string,
): Promise<CompilerOptionsHints | undefined> {
  try {
    const contents = await readFile(projectPath, "utf8");
    const parsed: unknown = JSON.parse(contents);
    if (typeof parsed !== "object" || parsed === null) {
      return undefined;
    }
    const config = parsed as { compilerOptions?: unknown };
    const options =
      typeof config.compilerOptions === "object" &&
      config.compilerOptions !== null
        ? (config.compilerOptions as Record<string, unknown>)
        : {};
    return {
      rootDir: stringOption(options.rootDir),
      outDir: stringOption(options.outDir),
      declarationDir: stringOption(options.declarationDir),
    };
  } catch {
    return undefined;
  }
}

function stringOption(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/**
 * Build the `PackageProvider` of one repository declared with `--provider`.
 *
 * Only `package.json` and `tsconfig.json` are read — the same two files
 * `cross-repository-positive.test.ts` reads by hand to build its registry.
 * A provider whose manifest cannot be read fails the whole run: a provider
 * named on the command line that silently resolves to nothing would hide
 * real imports behind `PACKAGE_PROVIDER_NOT_FOUND`.
 */
async function loadProvider(
  repository: string,
  rootPath: string,
): Promise<PackageProvider> {
  const root = path.resolve(rootPath);
  const manifestPath = path.join(root, "package.json");
  const manifest = await readManifest(manifestPath);
  if (manifest === undefined) {
    throw new Error(
      `--provider ${repository}=${rootPath}: no valid package.json at ${manifestPath}`,
    );
  }
  const projectPath = path.join(root, "tsconfig.json");
  const compilerOptions = await readCompilerOptions(projectPath);
  const rootDir = compilerOptions?.rootDir;
  const outDir = compilerOptions?.outDir;
  const declarationDir = compilerOptions?.declarationDir;
  const declarationRootDir = declarationDir ?? outDir;

  return {
    name: manifest.name,
    version: manifest.version,
    repository,
    rootPath: root,
    manifestPath,
    projectPath,
    typesPath:
      manifest.types === undefined
        ? undefined
        : path.resolve(root, manifest.types),
    sourceRoots:
      rootDir === undefined ? undefined : [path.resolve(root, rootDir)],
    declarationRoots:
      declarationRootDir === undefined
        ? undefined
        : [path.resolve(root, declarationRootDir)],
    rootDir,
    outDir,
    declarationDir,
  };
}

async function buildRegistry(
  providers: readonly ProviderArg[],
): Promise<PackageProviderRegistry> {
  const byName = new Map<string, PackageProvider>();
  for (const providerArg of providers) {
    const provider = await loadProvider(
      providerArg.repository,
      providerArg.rootPath,
    );
    byName.set(provider.name, provider);
  }
  return { get: (name) => byName.get(name) };
}

function relative(root: string, file: string): string {
  return path.relative(root, file).split(path.sep).join("/");
}

interface ProviderArg {
  readonly repository: string;
  readonly rootPath: string;
}

interface CliArgs {
  readonly repositoryName: string;
  readonly repositoryRoot: string;
  readonly output: string;
  readonly providers: readonly ProviderArg[];
}

const USAGE = `usage: pnpm facts <repository-name> <repository-root> <output.json> [--provider <name>=<path>]...

Emits the ts-facts-v3 payload of <repository-root>, named <repository-name>.

  --provider <name>=<path>   A provider repository this run may import from.
                              <name> is the repository name (as Luque names
                              it, not the npm package name); <path> is its
                              root directory. package.json and tsconfig.json
                              under <path> are read to derive the provider's
                              package name, version and source/declaration
                              roots. Repeatable.

Example — regenerate the ts-facts-v3 goldens, from ts-worker/:

  pnpm facts shared-library ../testdata/typescript/cross-repository/shared-library \\
    ../testdata/protocol/ts-facts-v3/shared-library.json
  pnpm facts consumer-a ../testdata/typescript/cross-repository/consumer-a \\
    ../testdata/protocol/ts-facts-v3/consumer-a.json \\
    --provider shared-library=../testdata/typescript/cross-repository/shared-library
  pnpm facts consumer-b ../testdata/typescript/cross-repository/consumer-b \\
    ../testdata/protocol/ts-facts-v3/consumer-b.json \\
    --provider shared-library=../testdata/typescript/cross-repository/shared-library
`;

function parseArgs(argv: readonly string[]): CliArgs | undefined {
  const [repositoryName, repositoryRoot, output, ...rest] = argv;
  if (
    repositoryName === undefined ||
    repositoryRoot === undefined ||
    output === undefined
  ) {
    return undefined;
  }
  const providers: ProviderArg[] = [];
  for (let index = 0; index < rest.length; index += 1) {
    if (rest[index] !== "--provider") {
      return undefined;
    }
    const value = rest[index + 1];
    const separator = value === undefined ? -1 : value.indexOf("=");
    if (value === undefined || separator <= 0) {
      return undefined;
    }
    providers.push({
      repository: value.slice(0, separator),
      rootPath: value.slice(separator + 1),
    });
    index += 1;
  }
  return { repositoryName, repositoryRoot, output, providers };
}

const cliArgv = process.argv.slice(2);
if (cliArgv.includes("--help") || cliArgv.includes("-h")) {
  process.stdout.write(USAGE);
} else {
  const args = parseArgs(cliArgv);
  if (args === undefined) {
    process.stderr.write(USAGE);
    process.exitCode = 1;
  } else {
    try {
      const registry = await buildRegistry(args.providers);
      const payload = await collectFacts(
        args.repositoryName,
        args.repositoryRoot,
        registry,
      );
      await mkdir(path.dirname(path.resolve(args.output)), {
        recursive: true,
      });
      await writeFile(
        path.resolve(args.output),
        `${JSON.stringify(payload, null, 2)}\n`,
        "utf8",
      );
      process.stdout.write(
        `${payload.symbols.length} symbols, ${payload.references.length} references, ` +
          `${payload.imports.length} imports, ${payload.exports.length} exports\n`,
      );
    } catch (error) {
      process.stderr.write(
        `${error instanceof Error ? error.message : String(error)}\n`,
      );
      process.exitCode = 1;
    }
  }
}
