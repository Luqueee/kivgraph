/**
 * Emit the canonical fact payload of one TypeScript repository.
 *
 * The payload is the wire contract `ts-facts-v4` consumed by
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
 * `extends` is the same idea again, for `class A extends B` and `interface A
 * extends B, C`: one entry per base, whose `qualifiedName` names the class or
 * interface declaring the clause — already an emitted symbol, since a
 * heritage clause introduces no binding of its own. A base declared in this
 * repository resolves through `targetQualifiedName`/`targetFile`; a base
 * introduced by an import reuses the exact provider-source identity an
 * `IMPORTS_SYMBOL` edge for that same binding already carries, never a
 * second resolution of its own. `implements` never appears here: see
 * `extends-resolver.ts` for why.
 *
 * `dependencies` is `PACKAGE_DEPENDS_ON`: one entry per package this
 * repository's own package really imports from, backed by a checker-resolved
 * module, never by a `package.json` entry nothing imports. TypeScript has no
 * module concept distinct from its package, so this worker never emits
 * `MODULE_DEPENDS_ON` — only Go's package/module split does.
 *
 * Usage:
 *
 *   pnpm facts <repository-name> <repository-root> <output.json> \
 *     [--project <path>] [--provider <name>=<path>]... \
 *     [--unclaimed <absolute path>]...
 *
 * `--project <path>` selects the project relative to the repository root.
 * Without it the CLI uses `<repository-root>/tsconfig.json`.
 *
 * `--provider <name>=<path>` declares one provider repository this indexing
 * run may import from: `<name>` is the repository name (as Kivgraph names it,
 * not the npm package name) and `<path>` is its root directory. The optional
 * `--provider-project <name>=<path>` selects the provider's project relative to
 * that provider root. The CLI derives package identity and source/declaration
 * roots from the selected `package.json` and `tsconfig`.
 *
 * `--unclaimed <absolute path>` names one source file that no project of this
 * repository claims — a file outside every `files`/`include` of every
 * tsconfig, which therefore belongs to no program and is invisible to the
 * configured pass. Repeatable. Each path must be absolute and inside the
 * repository root, the same containment rule `--project` obeys. Those files
 * are indexed through TypeScript's *inferred* project, so their symbols and
 * references carry the same confidence and provenance a configured file's do
 * while resting on compiler options Kivgraph chose rather than ones the
 * project declared. Nothing else is collected from them: imports, exports,
 * `extends` and package dependencies all grade their identity by a
 * provider's configuration, and an inferred project has none. The Go side
 * only passes this when `typescript.include_unclaimed_sources` is on.
 *
 * Regenerate the `ts-facts-v4` goldens, from `ts-worker/`:
 *
 *   pnpm facts shared-library ../testdata/typescript/cross-repository/shared-library \
 *     ../testdata/protocol/ts-facts-v4/shared-library.json
 *   pnpm facts consumer-a ../testdata/typescript/cross-repository/consumer-a \
 *     ../testdata/protocol/ts-facts-v4/consumer-a.json \
 *     --provider shared-library=../testdata/typescript/cross-repository/shared-library
 *   pnpm facts consumer-b ../testdata/typescript/cross-repository/consumer-b \
 *     ../testdata/protocol/ts-facts-v4/consumer-b.json \
 *     --provider shared-library=../testdata/typescript/cross-repository/shared-library
 */

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

import { isEntryPoint } from "./entry-point.js";
import { type ExtendsEdge, resolveExtends } from "./extends-resolver.js";
import type {
  ImportedSymbol,
  ReexportedSymbol,
} from "./imported-symbol-resolver.js";
import { LanguageService, type ProjectView } from "./language-service.js";
import {
  type PackageDependency,
  resolvePackageDependencies,
} from "./package-dependency-resolver.js";
import {
  createPackageProviderRegistry,
  type PackageProvider,
  type PackageProviderRegistry,
} from "./package-import-resolver.js";
import {
  extractLocalReferences,
  type ImportBindingSymbol,
} from "./reference-extractor.js";
import type { LocalExport } from "./symbol-extractor.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import { resolveUnresolvedReferences } from "./unresolved-reference-resolver.js";

interface FactsPayload {
  readonly version: 4;
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
  readonly extends: readonly FactExtends[];
  readonly dependencies: readonly FactDependency[];
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
  /** Provider repository, as Kivgraph names it. */
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
  /**
   * How the provider source position was reached: `DECLARATION_MAP` when the
   * artifact's own map placed the symbol, `PROVIDER_EXPORT` when the
   * provider's checker named the export inside a source file its project
   * roots mapped the artifact to. Kivgraph grades the two apart.
   */
  readonly source: string;
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

/**
 * One base of a `class ... extends` or `interface ... extends` clause, and
 * the declaration it names. `qualifiedName` names the class or interface
 * declaring the clause — already present in this payload's `symbols`, since
 * a heritage clause introduces no binding of its own, unlike an import or an
 * export. It resolves exactly one of two ways, exactly like `FactExport`:
 *
 *   - `targetQualifiedName`/`targetFile` name a declaration already present
 *     in this same payload's `symbols` — a base declared in this
 *     repository.
 *   - `target` carries the provider-source identity of a declaration in
 *     another repository — a base introduced by an import whose module
 *     specifier names a package. `target` is null when that identity is
 *     not exactly known, exactly like an import without one: the base
 *     still becomes an `UnresolvedReference`, never a guessed edge.
 */
interface FactExtends {
  readonly file: string;
  /** Qualified name of the class/interface symbol emitted in `symbols`. */
  readonly qualifiedName: string;
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
  /** Source text of the base, the evidence of the edge. */
  readonly text: string;
  readonly targetQualifiedName: string | null;
  readonly targetFile: string | null;
  readonly target: FactImportTarget | null;
  /** Package the base was imported from, null for a local target. */
  readonly requestedPackage: string | null;
  readonly requestedSymbol: string | null;
  /** Why a cross-repository target has no identity. Null otherwise. */
  readonly reason: string | null;
  readonly detail: string | null;
}

/**
 * One real dependency from this repository's own package to another package
 * the checker proved this repository imports from — never a nominal
 * `package.json` entry, which may list a package nothing actually imports.
 * TypeScript has no module concept distinct from its package, so this never
 * becomes `MODULE_DEPENDS_ON`; only Go's package/module split does.
 */
interface FactDependency {
  /** Provider repository, as Kivgraph names it. */
  readonly repository: string;
  /** Provider package name. */
  readonly package: string;
  /** One deterministic import occurrence proving the dependency. */
  readonly file: string;
  readonly specifier: string;
  readonly start: number;
  readonly end: number;
  readonly startLine: number;
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
  projectPath?: string,
  unclaimedPaths: readonly string[] = [],
): Promise<FactsPayload> {
  const root = path.resolve(repositoryRoot);
  const configFileName = resolveProjectPath(root, projectPath);
  const unclaimed = resolveUnclaimedPaths(root, unclaimedPaths);
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

    // The consumer is this same repository: the identical `loadProvider`
    // used for every `--provider` flag, applied to the repository being
    // indexed. Package identity stays authoritative on both ends of a
    // PACKAGE_DEPENDS_ON edge, exactly as decision 1 requires.
    const consumerProvider = await loadProvider(
      repositoryName,
      root,
      configFileName,
    );
    const dependencyResolution = await resolvePackageDependencies(
      service,
      view,
      registry,
      consumerProvider,
    );
    const extendsResolution = await resolveExtends(
      service,
      view,
      symbols,
      resolution.symbols,
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

    const dependencyFacts = await dependencyFactSymbols(
      view,
      root,
      dependencyResolution.dependencies,
    );
    const manifest = await readPackage(root, configFileName);
    const extendsFacts = extendsFactSymbols(
      root,
      extendsResolution.extends,
      manifest?.name ?? repositoryName,
    );

    const dependencyEvidenceFiles = dependencyResolution.dependencies
      .map((dependency) => dependency.imports[0]?.fileName)
      .filter((fileName): fileName is string => fileName !== undefined);
    const configuredFiles = [
      ...new Set([
        ...symbols.symbols.map((symbol) => symbol.fileName),
        ...symbols.exports.map((entry) => entry.fileName),
        ...resolution.symbols.map((entry) => entry.consumer.fileName),
        ...resolution.reexports.map((entry) => entry.export.fileName),
        ...dependencyEvidenceFiles,
      ]),
    ].sort();

    // Last, because it rolls the snapshot: every handle above belongs to the
    // previous generation, so nothing after this point may read one. The
    // plain values already read out of them are unaffected.
    const unclaimedFacts = await collectUnclaimedFacts(
      service,
      root,
      unclaimed,
      new Set(configuredFiles),
    );
    const files = [...configuredFiles, ...unclaimedFacts.files].sort();

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
      ...extendsFacts.unresolved,
      ...unclaimedFacts.unresolved,
    ].sort(compareUnresolved);

    return {
      version: 4,
      repository: { name: repositoryName },
      package: manifest,
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
        ...unclaimedFacts.symbols,
      ],
      references: [
        ...references.references.map(
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
        ...unclaimedFacts.references,
      ],
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
                  source: identity.source,
                },
          reason: entry.target.identityReason ?? null,
          detail: entry.target.identityDetail ?? null,
        };
      }),
      exports: exportSymbols.exports,
      extends: extendsFacts.extends,
      dependencies: dependencyFacts,
      unresolved,
    };
  } finally {
    await service.close();
  }
}

/**
 * The facts of the files no TypeScript project claims.
 *
 * A file no project's `files`/`include` reaches belongs to no program, so the
 * configured pass above cannot see it: nothing type-checks it and nothing
 * reports it absent. Opening those files loads them into TypeScript's
 * inferred project, whose compiler options are the engine's defaults and not
 * a declaration of the project that would have owned them -- there is none.
 *
 * Only `symbols` and `references` are collected. An unclaimed file is
 * indexed so its uses of the repository's own code become edges; import,
 * export, `extends` and package-dependency identity all rest on the
 * *provider's* configuration, which the inferred project does not have.
 */
interface UnclaimedFacts {
  /** Absolute paths of the unclaimed files that produced a symbol. */
  readonly files: readonly string[];
  readonly symbols: readonly FactSymbol[];
  readonly references: readonly FactReference[];
  readonly unresolved: readonly FactUnresolved[];
}

const NO_UNCLAIMED_FACTS: UnclaimedFacts = {
  files: [],
  symbols: [],
  references: [],
  unresolved: [],
};

async function collectUnclaimedFacts(
  service: LanguageService,
  root: string,
  unclaimedPaths: readonly string[],
  configuredFiles: ReadonlySet<string>,
): Promise<UnclaimedFacts> {
  if (unclaimedPaths.length === 0) {
    return NO_UNCLAIMED_FACTS;
  }
  const opened = await service.openFiles(unclaimedPaths, root);

  const files = new Set<string>();
  const symbols: FactSymbol[] = [];
  const references: FactReference[] = [];
  const unresolved: FactUnresolved[] = opened.unowned.map(
    (fileName): FactUnresolved => ({
      file: relative(root, fileName),
      reason: "UNCLAIMED_FILE_WITHOUT_PROJECT",
      requestedPackage: "",
      requestedSymbol: null,
      detail:
        "the engine resolved no project for this file, not even the inferred one, so nothing it declares or uses is in the graph",
      start: 0,
    }),
  );

  for (const owner of opened.owners) {
    // The symbol table spans the whole owning program, and only the opened
    // files are emitted from it. Both halves are load-bearing: an unclaimed
    // file's whole point is that it calls the repository's own code, and a
    // reference resolves only against a declaration this extraction saw --
    // restricting the table to the opened files would drop exactly the edge
    // the feature exists to produce. Restricting what is *emitted* is what
    // keeps the inferred program's copy of a claimed file from being
    // declared a second time.
    const owned = new Set(
      owner.files.filter((fileName) => !configuredFiles.has(fileName)),
    );
    const extraction = await extractLocalSymbols(service, owner.view);
    const owning = await extractLocalReferences(
      service,
      owner.view,
      extraction,
      { files: [...owned] },
    );
    for (const symbol of extraction.symbols) {
      if (!owned.has(symbol.fileName)) {
        continue;
      }
      files.add(symbol.fileName);
      symbols.push({
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
      });
    }
    for (const reference of owning.references) {
      references.push({
        file: relative(root, reference.fileName),
        kind: reference.kind,
        sourceQualifiedName: reference.source?.qualifiedName ?? null,
        targetQualifiedName: reference.target.qualifiedName,
        targetFile: relative(root, reference.target.fileName),
        startLine: reference.startLine,
        start: reference.start,
        end: reference.end,
        text: reference.text,
      });
    }
  }

  return {
    files: [...files].sort(),
    symbols,
    references,
    unresolved,
  };
}

/**
 * Validate the `--unclaimed` paths: each one must be absolute and inside the
 * repository root, which is the containment rule `resolveProjectPath` already
 * applies to a project. A relative path would resolve against whatever
 * directory the worker happened to run in, and a path outside the root names
 * a file this repository does not own.
 */
function resolveUnclaimedPaths(
  root: string,
  unclaimedPaths: readonly string[],
): readonly string[] {
  const resolved = new Set<string>();
  for (const unclaimedPath of unclaimedPaths) {
    if (!path.isAbsolute(unclaimedPath)) {
      throw new Error(`unclaimed path ${unclaimedPath} must be absolute`);
    }
    const candidate = path.resolve(unclaimedPath);
    if (!isWithin(root, candidate)) {
      throw new Error(
        `unclaimed path ${unclaimedPath} escapes repository root ${root}`,
      );
    }
    resolved.add(candidate);
  }
  return [...resolved].sort();
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
              source: identity.source,
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

/**
 * Turn every checker-resolved package dependency into its `FactDependency`,
 * with a single deterministic import occurrence as evidence — never every
 * occurrence, since one witness is enough for a `PACKAGE_DEPENDS_ON` edge.
 * `dependency.imports` is already sorted deterministically by
 * `createPackageDependencies`, so its first entry is that witness.
 */
async function dependencyFactSymbols(
  view: ProjectView,
  root: string,
  dependencies: readonly PackageDependency[],
): Promise<readonly FactDependency[]> {
  const facts: FactDependency[] = [];
  for (const dependency of dependencies) {
    const evidence = dependency.imports[0];
    if (evidence === undefined) {
      // createPackageDependencies only ever creates a dependency alongside
      // at least one contributing import; an empty list never happens.
      continue;
    }
    const sourceFile = await view.program.getSourceFile(evidence.fileName);
    const startLine =
      sourceFile === undefined
        ? 0
        : sourceFile.getLineAndCharacterOfPosition(evidence.start).line + 1;
    facts.push({
      repository: dependency.provider.repository,
      package: dependency.provider.name,
      file: relative(root, evidence.fileName),
      specifier: evidence.specifier,
      start: evidence.start,
      end: evidence.end,
      startLine,
    });
  }
  return facts;
}

/**
 * Turn every resolved `extends` base into its `FactExtends` edge, and every
 * one without a proven identity into a matching `FactUnresolved` entry too —
 * exactly like `exportFactSymbols` does for a cross-repository re-export.
 *
 * `localPackage` names the package the base was looked up from. A base that
 * resolved to neither a local declaration nor a package import was still
 * requested from somewhere, and an unresolved reference that names nothing is
 * not a usable fact: the graph would carry a reason with no subject.
 */
function extendsFactSymbols(
  root: string,
  edges: readonly ExtendsEdge[],
  localPackage: string,
): {
  readonly extends: readonly FactExtends[];
  readonly unresolved: readonly FactUnresolved[];
} {
  const facts: FactExtends[] = [];
  const unresolved: FactUnresolved[] = [];

  for (const edge of edges) {
    const identity = edge.identity;
    facts.push({
      file: relative(root, edge.base.fileName),
      qualifiedName: edge.base.sourceQualifiedName,
      start: edge.base.start,
      end: edge.base.end,
      startLine: edge.base.startLine,
      text: edge.base.text,
      targetQualifiedName: edge.targetQualifiedName ?? null,
      targetFile:
        edge.targetFile === undefined ? null : relative(root, edge.targetFile),
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
              source: identity.source,
            },
      requestedPackage: edge.packageName ?? null,
      requestedSymbol: edge.exportedName ?? null,
      reason: edge.unresolvedReason ?? null,
      detail: edge.unresolvedDetail ?? null,
    });
    if (edge.targetQualifiedName === undefined && identity === undefined) {
      unresolved.push({
        file: relative(root, edge.base.fileName),
        reason: edge.unresolvedReason ?? "PROVIDER_SOURCE_UNAVAILABLE",
        requestedPackage: edge.packageName ?? localPackage,
        requestedSymbol: edge.exportedName ?? edge.base.text,
        detail: edge.unresolvedDetail ?? null,
        start: edge.base.start,
      });
    }
  }

  return { extends: facts, unresolved };
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

async function readPackage(
  repositoryRoot: string,
  projectPath: string,
): Promise<FactsPayload["package"]> {
  const root = path.resolve(repositoryRoot);
  const packageRoot = await findPackageRoot(root, projectPath);
  const manifestPath = path.join(packageRoot, "package.json");
  const manifest = await readManifest(manifestPath);
  if (manifest === undefined) {
    return null;
  }
  return {
    name: manifest.name,
    version: manifest.version,
    // Repository relative, so a recorded payload stays portable.
    rootPath: relativeOrDot(root, packageRoot),
    manifestPath: relativeOrDot(root, manifestPath),
  };
}

function resolveProjectPath(
  root: string,
  projectPath: string | undefined,
): string {
  const resolved = path.resolve(root, projectPath ?? "tsconfig.json");
  if (!isWithin(root, resolved)) {
    throw new Error(
      `project path ${projectPath ?? "tsconfig.json"} escapes repository root ${root}`,
    );
  }
  return resolved;
}

async function findPackageRoot(
  repositoryRoot: string,
  projectPath: string,
): Promise<string> {
  const root = path.resolve(repositoryRoot);
  let current = path.dirname(path.resolve(projectPath));
  while (isWithin(root, current)) {
    if (
      (await readManifest(path.join(current, "package.json"))) !== undefined
    ) {
      return current;
    }
    if (current === root) {
      break;
    }
    current = path.dirname(current);
  }
  throw new Error(
    `project ${projectPath} has no valid package.json inside repository root ${root}`,
  );
}

function relativeOrDot(root: string, candidate: string): string {
  const relativePath = path.relative(root, candidate);
  return relativePath === "" ? "." : relativePath.split(path.sep).join("/");
}

function isWithin(root: string, candidate: string): boolean {
  const relativePath = path.relative(
    path.resolve(root),
    path.resolve(candidate),
  );
  return (
    relativePath === "" ||
    (!relativePath.startsWith(`..${path.sep}`) &&
      relativePath !== ".." &&
      !path.isAbsolute(relativePath))
  );
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
 * Build the `PackageProvider` of one repository: a provider declared with
 * `--provider`, or the repository being indexed itself, which supplies its
 * own `PackageProvider` as the consumer side of `PACKAGE_DEPENDS_ON`.
 *
 * Only `package.json` and `tsconfig.json` are read — the same two files
 * `cross-repository-positive.test.ts` reads by hand to build its registry.
 * A repository whose manifest cannot be read fails the whole run: silently
 * resolving to nothing would hide real imports behind
 * `PACKAGE_PROVIDER_NOT_FOUND`, or a real dependency behind no edge at all.
 */
async function loadProvider(
  repository: string,
  rootPath: string,
  projectPathOverride?: string,
): Promise<PackageProvider> {
  const root = path.resolve(rootPath);
  const projectPath = resolveProjectPath(root, projectPathOverride);
  const packageRoot = await findPackageRoot(root, projectPath);
  const manifestPath = path.join(packageRoot, "package.json");
  const manifest = await readManifest(manifestPath);
  if (manifest === undefined) {
    throw new Error(
      `repository ${repository} (${rootPath}): no valid package.json at ${manifestPath}`,
    );
  }
  const compilerOptions = await readCompilerOptions(projectPath);
  const projectDirectory = path.dirname(projectPath);
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
        : path.resolve(packageRoot, manifest.types),
    sourceRoots:
      rootDir === undefined
        ? undefined
        : [path.resolve(projectDirectory, rootDir)],
    declarationRoots:
      declarationRootDir === undefined
        ? undefined
        : [path.resolve(projectDirectory, declarationRootDir)],
    rootDir,
    outDir,
    declarationDir,
  };
}

async function buildRegistry(
  providerArgs: readonly ProviderArg[],
): Promise<PackageProviderRegistry> {
  const providers: PackageProvider[] = [];
  for (const providerArg of providerArgs) {
    providers.push(
      await loadProvider(
        providerArg.repository,
        providerArg.rootPath,
        providerArg.projectPath,
      ),
    );
  }
  return createPackageProviderRegistry(providers);
}

function relative(root: string, file: string): string {
  return path.relative(root, file).split(path.sep).join("/");
}

interface ProviderArg {
  readonly repository: string;
  readonly rootPath: string;
  readonly projectPath: string | undefined;
}

interface CliArgs {
  readonly repositoryName: string;
  readonly repositoryRoot: string;
  readonly output: string;
  readonly projectPath: string | undefined;
  readonly providers: readonly ProviderArg[];
  readonly unclaimed: readonly string[];
}

const USAGE = `usage: pnpm facts <repository-name> <repository-root> <output.json> [--project <path>] [--provider <name>=<path>]... [--provider-project <name>=<path>]... [--unclaimed <absolute path>]...

Emits the ts-facts-v4 payload of <repository-root>, named <repository-name>.

  --project <path>            TypeScript project to load, relative to the
                              repository root. Defaults to <root>/tsconfig.json.
  --provider <name>=<path>    A provider repository this indexing run may import
                              from. Repeatable.
  --provider-project <name>=<path>
                              TypeScript project for the matching provider
                              occurrence, relative to that provider repository.
                              Repeatable; useful for nested workspaces.
  --unclaimed <absolute path> A source file no TypeScript project claims, to be
                              indexed through the inferred project. Must be
                              absolute and inside the repository root.
                              Repeatable.

Example — regenerate the ts-facts-v4 goldens, from ts-worker/:

  pnpm facts shared-library ../testdata/typescript/cross-repository/shared-library \\
    ../testdata/protocol/ts-facts-v4/shared-library.json
  pnpm facts consumer-a ../testdata/typescript/cross-repository/consumer-a \\
    ../testdata/protocol/ts-facts-v4/consumer-a.json \\
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
  let projectPath: string | undefined;
  const providers: ProviderArg[] = [];
  const providerProjectPaths = new Map<string, string[]>();
  const unclaimed: string[] = [];
  for (let index = 0; index < rest.length; index += 1) {
    const option = rest[index];
    const value = rest[index + 1];
    if (option === "--project") {
      if (projectPath !== undefined || value === undefined || value === "") {
        return undefined;
      }
      projectPath = value;
      index += 1;
      continue;
    }
    if (option === "--provider") {
      const separator = value === undefined ? -1 : value.indexOf("=");
      if (
        value === undefined ||
        separator <= 0 ||
        separator === value.length - 1
      ) {
        return undefined;
      }
      providers.push({
        repository: value.slice(0, separator),
        rootPath: value.slice(separator + 1),
        projectPath: undefined,
      });
      index += 1;
      continue;
    }
    if (option === "--provider-project") {
      const separator = value === undefined ? -1 : value.indexOf("=");
      if (
        value === undefined ||
        separator <= 0 ||
        separator === value.length - 1
      ) {
        return undefined;
      }
      const repository = value.slice(0, separator);
      const paths = providerProjectPaths.get(repository) ?? [];
      paths.push(value.slice(separator + 1));
      providerProjectPaths.set(repository, paths);
      index += 1;
      continue;
    }
    if (option === "--unclaimed") {
      if (value === undefined || value === "") {
        return undefined;
      }
      unclaimed.push(value);
      index += 1;
      continue;
    }
    return undefined;
  }

  const assignedProviders = providers.map((provider) => {
    const paths = providerProjectPaths.get(provider.repository);
    return {
      ...provider,
      projectPath: paths?.shift(),
    };
  });
  for (const paths of providerProjectPaths.values()) {
    if (paths.length !== 0) {
      return undefined;
    }
  }
  return {
    repositoryName,
    repositoryRoot,
    output,
    projectPath,
    providers: assignedProviders,
    unclaimed,
  };
}

// Only when started as a program. This module also exports `collectFacts`,
// which the worker's own tests call directly, and an unguarded argument block
// parsed the test runner's argv, printed the usage text and set a failing
// exit code from inside a passing run. The guard is the one `index.ts` uses,
// realpath comparison included: the bundle's launcher execs
// `node <bundle>/worker/dist/facts-cli.js`, and an install root reached
// through a symlink must still count as an entry point.
if (isEntryPoint(process.argv[1], import.meta.url)) {
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
          args.projectPath,
          args.unclaimed,
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
            `${payload.imports.length} imports, ${payload.exports.length} exports, ` +
            `${payload.extends.length} extends, ${payload.dependencies.length} dependencies\n`,
        );
      } catch (error) {
        process.stderr.write(
          `${error instanceof Error ? error.message : String(error)}\n`,
        );
        process.exitCode = 1;
      }
    }
  }
}
