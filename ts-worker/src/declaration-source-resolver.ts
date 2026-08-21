import { fileURLToPath } from "node:url";
import { access, readFile } from "node:fs/promises";
import path from "node:path";

import type { LanguageService, ProjectView } from "./language-service.js";
import type {
  PackageImport,
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import type { ProviderExportResolution } from "./provider-export-resolver.js";

export type DeclarationSourceStatus =
  | "DECLARATION_MAP"
  | "PROJECT_REFERENCE"
  | "PROVIDER_REGISTRY"
  | "ROOT_DIR_OUT_DIR"
  | "INSTALLED_PACKAGE"
  | "UNRESOLVED";

/** File-level mapping from a declaration artifact to its source files. */
export interface DeclarationSourceMapping {
  readonly declarationFile: string;
  readonly sourceFiles: readonly string[];
  readonly status: DeclarationSourceStatus;
}

export interface DeclarationSourceResolution {
  readonly generation: number;
  readonly configFileName: string;
  readonly mappings: readonly DeclarationSourceMapping[];
}

/**
 * Map declaration files discovered by provider export resolution to sources.
 *
 * The resolver deliberately reports file mappings, not nominal symbol edges.
 * Symbol identity remains the checker's responsibility and is consumed by
 * LUQUE-0705 after this file-level bridge has been established.
 */
export async function resolveDeclarationSources(
  service: LanguageService,
  view: ProjectView,
  resolution: ProviderExportResolution,
  registry: PackageProviderRegistry,
): Promise<DeclarationSourceResolution> {
  service.assertFresh(view);
  if (resolution.generation !== view.generation) {
    throw new Error(
      `provider export resolution belongs to generation ${resolution.generation}, current is ${view.generation}`,
    );
  }

  const providersByDeclaration = collectProviders(resolution.imports);
  for (const entry of resolution.exports) {
    if (entry.status !== "RESOLVED") {
      continue;
    }
    const provider = providerForPackage(resolution.imports, entry.packageName);
    for (const declarationFile of entry.targetFiles) {
      if (isDeclarationFile(declarationFile)) {
        providersByDeclaration.set(
          declarationFile,
          provider ?? providersByDeclaration.get(declarationFile),
        );
      }
    }
  }

  const mappings = await Promise.all(
    [...providersByDeclaration.entries()].map(
      async ([declarationFile, provider]) =>
        mapDeclarationFile(declarationFile, provider, registry),
    ),
  );
  service.assertFresh(view);
  mappings.sort((left, right) =>
    left.declarationFile.localeCompare(right.declarationFile),
  );
  return {
    generation: view.generation,
    configFileName: view.configFileName,
    mappings,
  };
}

function collectProviders(
  imports: readonly PackageImport[],
): Map<string, PackageProvider | undefined> {
  const providers = new Map<string, PackageProvider | undefined>();
  for (const entry of imports) {
    if (entry.status !== "RESOLVED") {
      continue;
    }
    for (const declarationFile of entry.resolvedFiles) {
      if (isDeclarationFile(declarationFile)) {
        providers.set(declarationFile, entry.provider);
      }
    }
  }
  return providers;
}

function providerForPackage(
  imports: readonly PackageImport[],
  packageName: string,
): PackageProvider | undefined {
  return imports.find(
    (entry) => entry.packageName === packageName && entry.status === "RESOLVED",
  )?.provider;
}

async function mapDeclarationFile(
  declarationFile: string,
  provider: PackageProvider | undefined,
  registry: PackageProviderRegistry,
): Promise<DeclarationSourceMapping> {
  const declarationMap = await mapWithDeclarationMap(declarationFile);
  if (declarationMap.length > 0) {
    return {
      declarationFile,
      sourceFiles: declarationMap,
      status: "DECLARATION_MAP",
    };
  }

  if (provider?.projectPath !== undefined) {
    const project = await readProjectHints(provider.projectPath);
    if (project !== undefined) {
      const projectSources = await mapWithRoots(
        declarationFile,
        project.declarationRoots,
        project.sourceRoots,
      );
      if (projectSources.length > 0) {
        return {
          declarationFile,
          sourceFiles: projectSources,
          status: "PROJECT_REFERENCE",
        };
      }
    }
  }

  if (provider !== undefined) {
    const registrySources = await mapWithRoots(
      declarationFile,
      declarationRoots(provider),
      (provider.sourceRoots ?? []).map((sourceRoot) =>
        providerPath(provider.rootPath, sourceRoot),
      ),
    );
    if (registrySources.length > 0) {
      return {
        declarationFile,
        sourceFiles: registrySources,
        status: "PROVIDER_REGISTRY",
      };
    }

    const rootOutSources =
      provider.rootDir !== undefined &&
      (provider.outDir !== undefined || provider.declarationDir !== undefined)
        ? await mapWithRoots(
            declarationFile,
            [
              providerPath(
                provider.rootPath,
                provider.declarationDir ?? provider.outDir ?? "",
              ),
            ],
            [providerPath(provider.rootPath, provider.rootDir)],
          )
        : [];
    if (rootOutSources.length > 0) {
      return {
        declarationFile,
        sourceFiles: rootOutSources,
        status: "ROOT_DIR_OUT_DIR",
      };
    }
  }

  const installedSources = await mapInstalledPackage(declarationFile, registry);
  if (installedSources.length > 0) {
    return {
      declarationFile,
      sourceFiles: installedSources,
      status: "INSTALLED_PACKAGE",
    };
  }

  return {
    declarationFile,
    sourceFiles: [],
    status: "UNRESOLVED",
  };
}

/**
 * Load the declaration map of an artifact with its sources already resolved.
 *
 * A source that does not exist on disk is reported as `undefined` so callers
 * never build a fact on a path nobody can open, while the segment indexes of
 * `mappings` stay aligned with `sources`.
 */
export async function loadDeclarationSourceMap(
  declarationFile: string,
): Promise<DeclarationSourceMap | undefined> {
  const mapFile = `${declarationFile}.map`;
  const contents = await readText(mapFile);
  if (contents === undefined) {
    return undefined;
  }
  const parsed = parseSourceMap(contents);
  if (parsed === undefined || parsed.sources.length === 0) {
    return undefined;
  }
  const sourceRoot = parsed.sourceRoot ?? "";
  const sources = await Promise.all(
    parsed.sources.map(async (source) => {
      const resolved = resolveSourcePath(mapFile, sourceRoot, source);
      return resolved !== undefined && (await exists(resolved))
        ? resolved
        : undefined;
    }),
  );
  return { sources, mappings: parsed.mappings };
}

async function mapWithDeclarationMap(
  declarationFile: string,
): Promise<string[]> {
  const sourceMap = await loadDeclarationSourceMap(declarationFile);
  if (sourceMap === undefined) {
    return [];
  }
  return uniqueSorted(
    sourceMap.sources.filter(
      (source): source is string => source !== undefined,
    ),
  );
}

async function mapWithRoots(
  declarationFile: string,
  declarationRoots: readonly string[],
  sourceRoots: readonly string[],
  packageRoot?: string,
): Promise<string[]> {
  const declarationCandidates = declarationRoots.map((root) =>
    root === "" && packageRoot !== undefined ? packageRoot : root,
  );
  const sources: string[] = [];
  for (const declarationRoot of declarationCandidates) {
    if (declarationRoot === "") {
      continue;
    }
    const relative = path.relative(
      path.resolve(declarationRoot),
      path.resolve(declarationFile),
    );
    if (!isRelativePath(relative)) {
      continue;
    }
    for (const sourceRoot of sourceRoots) {
      const sourceBase =
        sourceRoot === "" && packageRoot !== undefined
          ? packageRoot
          : sourceRoot;
      if (sourceBase === "") {
        continue;
      }
      for (const candidate of sourceCandidates(
        path.resolve(sourceBase, relative),
      )) {
        if (await exists(candidate)) {
          sources.push(candidate);
        }
      }
    }
  }
  return uniqueSorted(sources);
}

/**
 * Bridge an installed copy of a package back to the workspace repository
 * that declares it.
 *
 * A consumer that writes `import { x } from "pkg"` gets the copy its package
 * manager installed, and that copy is a different File from the source that
 * produced it: a published tarball ships `dist` without `src`, without a
 * `tsconfig.json` and -- because publishing a map whose `sources` name files
 * the tarball omits is worse than useless -- usually without a `.d.ts.map`.
 * None of the provider-rooted transforms above can relate the two paths,
 * because the artifact does not live under the provider's root at all.
 *
 * The nearest enclosing `package.json` of the artifact names which package
 * this copy *is*, and that name is what the registry is asked about -- never
 * the name the consumer imported, which for a transitive dependency belongs
 * to a different repository. When a registered repository declares that
 * exact name, its own build configuration says where inside itself the
 * artifact came from, and the same `rootDir`/`outDir` transform runs with the
 * declaration root re-rooted onto the installed copy.
 *
 * That step is an assertion of the provider's build configuration, not of a
 * map it emitted: the caller grades the resulting identity
 * `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`, never
 * `EXACT_TYPECHECKED`. A name no registered repository declares, or a source
 * tree that no longer holds the file, names nothing and stays `UNRESOLVED`.
 */
async function mapInstalledPackage(
  declarationFile: string,
  registry: PackageProviderRegistry,
): Promise<string[]> {
  const installed = await installedPackage(declarationFile);
  if (installed === undefined) {
    return [];
  }
  const provider = registry.get(installed.name);
  if (provider === undefined) {
    return [];
  }
  const rootPath = path.resolve(provider.rootPath);
  // An artifact already inside the provider's own root is not an installed
  // copy of it -- a workspace link resolves there, and the transforms above
  // own that case. Re-rooting would only rediscover what they answered.
  if (isRelativePath(path.relative(rootPath, path.resolve(declarationFile)))) {
    return [];
  }
  const hints =
    provider.projectPath === undefined
      ? undefined
      : await readProjectHints(provider.projectPath);
  const installedDeclarationRoots = uniqueSorted([
    ...reroot(rootPath, installed.root, [
      ...declarationRoots(provider),
      ...(hints?.declarationRoots ?? []),
      ...(provider.declarationDir === undefined
        ? []
        : [providerPath(rootPath, provider.declarationDir)]),
      ...(provider.outDir === undefined
        ? []
        : [providerPath(rootPath, provider.outDir)]),
    ]),
    // The copy's own manifest is the one statement about its layout that
    // travels with it, and it is what a provider that declares no tsconfig
    // path options leaves to go on.
    ...(installed.typesPath === undefined
      ? []
      : [path.dirname(providerPath(installed.root, installed.typesPath))]),
  ]);
  const sourceRoots = uniqueSorted([
    ...(provider.sourceRoots ?? []).map((sourceRoot) =>
      providerPath(rootPath, sourceRoot),
    ),
    ...(hints?.sourceRoots ?? []),
    ...(provider.rootDir === undefined
      ? []
      : [providerPath(rootPath, provider.rootDir)]),
  ]);
  return mapWithRoots(declarationFile, installedDeclarationRoots, sourceRoots);
}

/**
 * Re-root every declaration root of `rootPath` onto `installedRoot`.
 *
 * A root the provider places outside its own tree says nothing about the
 * layout of a copy of that tree, so it is dropped rather than joined.
 */
function reroot(
  rootPath: string,
  installedRoot: string,
  roots: readonly string[],
): string[] {
  const rerooted: string[] = [];
  for (const root of roots) {
    const relative = path.relative(rootPath, path.resolve(root));
    if (!isRelativePath(relative)) {
      continue;
    }
    rerooted.push(
      relative === "" ? installedRoot : path.join(installedRoot, relative),
    );
  }
  return rerooted;
}

/** The installed package a declaration artifact belongs to. */
interface InstalledPackage {
  /** Directory holding the manifest that names the package. */
  readonly root: string;
  readonly name: string;
  /** `types`/`typings` exactly as the manifest spells it, when present. */
  readonly typesPath?: string;
}

/**
 * Name the installed package an artifact under a `node_modules` belongs to.
 *
 * The search stops at the `node_modules` the copy was installed into: the
 * first manifest above that directory belongs to whoever ran the install, and
 * that repository declares none of the code underneath it. Nobody owns
 * anything inside a `node_modules`, which is exactly why the owner has to be
 * found by name instead of by path.
 */
async function installedPackage(
  declarationFile: string,
): Promise<InstalledPackage | undefined> {
  const resolved = path.resolve(declarationFile);
  const segments = resolved.split(path.sep);
  const depth = segments.lastIndexOf("node_modules");
  if (depth < 0) {
    return undefined;
  }
  const ceiling = segments.slice(0, depth + 1).join(path.sep) + path.sep;
  let directory = path.dirname(resolved);
  while (directory.startsWith(ceiling)) {
    const contents = await readText(path.join(directory, "package.json"));
    const manifest =
      contents === undefined ? undefined : parsePackageManifest(contents);
    if (manifest !== undefined) {
      return { root: directory, ...manifest };
    }
    directory = path.dirname(directory);
  }
  return undefined;
}

function parsePackageManifest(
  contents: string,
): Omit<InstalledPackage, "root"> | undefined {
  const parsed = parseJson(contents);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return undefined;
  }
  const manifest = parsed as { name?: unknown; types?: unknown };
  const name = manifest.name;
  if (typeof name !== "string" || name.trim() === "") {
    return undefined;
  }
  const types = manifest.types;
  return typeof types === "string" && types.trim() !== ""
    ? { name, typesPath: types }
    : { name };
}

function declarationRoots(provider: PackageProvider): readonly string[] {
  if (provider.declarationRoots !== undefined) {
    return provider.declarationRoots.map((declarationRoot) =>
      providerPath(provider.rootPath, declarationRoot),
    );
  }
  return provider.typesPath === undefined
    ? []
    : [path.dirname(providerPath(provider.rootPath, provider.typesPath))];
}

function providerPath(rootPath: string, candidate: string): string {
  return path.isAbsolute(candidate)
    ? path.normalize(candidate)
    : path.resolve(rootPath, candidate);
}

interface ProjectConfig {
  readonly extends?: string;
  readonly compilerOptions: CompilerOptionsConfig;
}

interface CompilerOptionsConfig {
  readonly rootDir?: unknown;
  readonly outDir?: unknown;
  readonly declarationDir?: unknown;
}

/** A declaration map with every source resolved against the map file. */
export interface DeclarationSourceMap {
  /** Absolute path per source index, or undefined when it does not exist. */
  readonly sources: readonly (string | undefined)[];
  /** Raw VLQ segments, aligned with `sources` by index. */
  readonly mappings: string;
}

interface SourceMapDocument {
  readonly sourceRoot?: string;
  readonly sources: readonly string[];
  readonly mappings: string;
}

interface ProjectHints {
  readonly sourceRoots: readonly string[];
  readonly declarationRoots: readonly string[];
}

async function readProjectHints(
  projectPath: string,
  visited = new Set<string>(),
): Promise<ProjectHints | undefined> {
  const configPath = path.resolve(projectPath);
  if (visited.has(configPath)) {
    return undefined;
  }
  visited.add(configPath);
  const contents = await readText(configPath);
  if (contents === undefined) {
    return undefined;
  }
  const config = parseProjectConfig(contents);
  if (config === undefined) {
    return undefined;
  }

  const inherited =
    config.extends === undefined
      ? undefined
      : await readProjectHints(
          resolveExtends(configPath, config.extends),
          visited,
        );
  const compilerOptions = config.compilerOptions;
  const rootDir = pathOption(configPath, compilerOptions.rootDir);
  const outDir = pathOption(configPath, compilerOptions.outDir);
  const declarationDir = pathOption(configPath, compilerOptions.declarationDir);
  return {
    sourceRoots:
      rootDir === undefined ? (inherited?.sourceRoots ?? []) : [rootDir],
    declarationRoots:
      declarationDir !== undefined
        ? [declarationDir]
        : outDir === undefined
          ? (inherited?.declarationRoots ?? [])
          : [outDir],
  };
}

function resolveExtends(configPath: string, extendsValue: string): string {
  const raw = extendsValue.startsWith(".")
    ? path.resolve(path.dirname(configPath), extendsValue)
    : path.resolve(path.dirname(configPath), extendsValue);
  if (path.extname(raw) !== "") {
    return raw;
  }
  return `${raw}.json`;
}

function pathOption(configPath: string, value: unknown): string | undefined {
  return typeof value !== "string" || value.trim() === ""
    ? undefined
    : path.resolve(path.dirname(configPath), value);
}

function sourceCandidates(base: string): string[] {
  const stem = base.replace(/\.d\.(?:ts|mts|cts)$/, "");
  return [
    `${stem}.ts`,
    `${stem}.tsx`,
    `${stem}.mts`,
    `${stem}.cts`,
    path.join(stem, "index.ts"),
    path.join(stem, "index.tsx"),
    path.join(stem, "index.mts"),
    path.join(stem, "index.cts"),
  ];
}

function resolveSourcePath(
  mapFile: string,
  sourceRoot: string,
  source: string,
): string | undefined {
  if (source.startsWith("file://")) {
    try {
      return path.resolve(fileURLToPath(source));
    } catch {
      return undefined;
    }
  }
  if (/^[A-Za-z][A-Za-z+.-]*:\/\//.test(source)) {
    return undefined;
  }
  if (/^[A-Za-z][A-Za-z+.-]*:\/\//.test(sourceRoot)) {
    return undefined;
  }
  return path.resolve(path.dirname(mapFile), sourceRoot, source);
}

function isDeclarationFile(fileName: string): boolean {
  return /\.d\.(?:ts|mts|cts)$/.test(fileName);
}

function isRelativePath(relative: string): boolean {
  return (
    relative === "" ||
    (relative !== ".." &&
      !relative.startsWith(`..${path.sep}`) &&
      !path.isAbsolute(relative))
  );
}

async function exists(fileName: string): Promise<boolean> {
  try {
    await access(fileName);
    return true;
  } catch (error: unknown) {
    if (isNotFound(error)) {
      return false;
    }
    throw error;
  }
}

async function readText(fileName: string): Promise<string | undefined> {
  try {
    return await readFile(fileName, "utf8");
  } catch (error: unknown) {
    if (isNotFound(error)) {
      return undefined;
    }
    throw error;
  }
}

function parseJson(contents: string): unknown {
  try {
    return JSON.parse(
      stripJsonComments(contents).replace(/,(\s*[}\]])/g, "$1"),
    );
  } catch {
    return undefined;
  }
}

function stripJsonComments(contents: string): string {
  let output = "";
  let inString = false;
  let escaped = false;
  let lineComment = false;
  let blockComment = false;
  for (let index = 0; index < contents.length; index += 1) {
    const current = contents[index];
    const next = contents[index + 1];
    if (lineComment) {
      if (current === "\n" || current === "\r") {
        lineComment = false;
        output += current;
      } else {
        output += " ";
      }
      continue;
    }
    if (blockComment) {
      if (current === "*" && next === "/") {
        blockComment = false;
        output += "  ";
        index += 1;
      } else {
        output += current === "\n" || current === "\r" ? current : " ";
      }
      continue;
    }
    if (inString) {
      output += current;
      if (escaped) {
        escaped = false;
      } else if (current === "\\") {
        escaped = true;
      } else if (current === '"') {
        inString = false;
      }
      continue;
    }
    if (current === '"') {
      inString = true;
      output += current;
    } else if (current === "/" && next === "/") {
      lineComment = true;
      output += "  ";
      index += 1;
    } else if (current === "/" && next === "*") {
      blockComment = true;
      output += "  ";
      index += 1;
    } else {
      output += current;
    }
  }
  return output;
}

function parseProjectConfig(contents: string): ProjectConfig | undefined {
  const parsed = parseJson(contents);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return undefined;
  }
  const rawConfig = parsed as {
    extends?: unknown;
    compilerOptions?: unknown;
  };
  const rawCompilerOptions = rawConfig.compilerOptions;
  const compilerOptions =
    typeof rawCompilerOptions === "object" &&
    rawCompilerOptions !== null &&
    !Array.isArray(rawCompilerOptions)
      ? (rawCompilerOptions as CompilerOptionsConfig)
      : {};
  return {
    extends:
      typeof rawConfig.extends === "string" ? rawConfig.extends : undefined,
    compilerOptions,
  };
}

function parseSourceMap(contents: string): SourceMapDocument | undefined {
  const parsed = parseJson(contents);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return undefined;
  }
  const rawSourceMap = parsed as {
    sourceRoot?: unknown;
    sources?: unknown;
    mappings?: unknown;
  };
  if (
    !Array.isArray(rawSourceMap.sources) ||
    !rawSourceMap.sources.every((source) => typeof source === "string")
  ) {
    return undefined;
  }
  return {
    sourceRoot:
      typeof rawSourceMap.sourceRoot === "string"
        ? rawSourceMap.sourceRoot
        : undefined,
    sources: rawSourceMap.sources,
    mappings:
      typeof rawSourceMap.mappings === "string" ? rawSourceMap.mappings : "",
  };
}

function uniqueSorted(values: readonly string[]): string[] {
  return [...new Set(values)].sort();
}

function isNotFound(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    (error.code === "ENOENT" || error.code === "ENOTDIR")
  );
}
