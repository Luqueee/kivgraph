import { fileURLToPath } from "node:url";
import { access, readFile } from "node:fs/promises";
import path from "node:path";

import type { LanguageService, ProjectView } from "./language-service.js";
import type {
  PackageImport,
  PackageProvider,
} from "./package-import-resolver.js";
import type { ProviderExportResolution } from "./provider-export-resolver.js";

export type DeclarationSourceStatus =
  | "DECLARATION_MAP"
  | "PROJECT_REFERENCE"
  | "PROVIDER_REGISTRY"
  | "ROOT_DIR_OUT_DIR"
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
        mapDeclarationFile(declarationFile, provider),
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

  return {
    declarationFile,
    sourceFiles: [],
    status: "UNRESOLVED",
  };
}

async function mapWithDeclarationMap(
  declarationFile: string,
): Promise<string[]> {
  const mapFile = `${declarationFile}.map`;
  const contents = await readText(mapFile);
  if (contents === undefined) {
    return [];
  }
  const parsed = parseSourceMap(contents);
  if (parsed === undefined || parsed.sources.length === 0) {
    return [];
  }
  const sourceRoot = parsed.sourceRoot ?? "";
  const sourceFiles = await Promise.all(
    parsed.sources.map(async (source) => {
      const resolved = resolveSourcePath(mapFile, sourceRoot, source);
      return resolved !== undefined && (await exists(resolved))
        ? resolved
        : undefined;
    }),
  );
  return uniqueSorted(
    sourceFiles.filter((source): source is string => source !== undefined),
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

interface SourceMapDocument {
  readonly sourceRoot?: string;
  readonly sources: readonly string[];
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
