import { mkdir, rm, writeFile, symlink } from "node:fs/promises";
import path from "node:path";
import { afterEach, expect, it } from "vitest";
import { resolveImportedSymbols } from "./imported-symbol-resolver.js";
import { createPackageProviderRegistry } from "./package-import-resolver.js";
import { resolveImplementations } from "./implements-resolver.js";
import { LanguageService } from "./language-service.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import { temporaryRoot } from "./temporary-root.js";

const services: LanguageService[] = [];
const roots: string[] = [];
afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    roots.splice(0).map((root) => rm(root, { recursive: true, force: true })),
  );
});

async function fixture(source: string) {
  const root = await temporaryRoot("kivgraph-implementations-");
  roots.push(root);
  await mkdir(path.join(root, "src"));
  const config = path.join(root, "tsconfig.json");
  await writeFile(
    config,
    JSON.stringify({
      compilerOptions: { strict: true, target: "ES2022", noEmit: true },
      include: ["src/*.ts"],
    }),
  );
  await writeFile(path.join(root, "src/main.ts"), source);
  const service = LanguageService.create({ cwd: root });
  services.push(service);
  await service.openProject(config);
  const view = service.project(config);
  return { service, view, symbols: await extractLocalSymbols(service, view) };
}

it("rejects incompatible and erroneous classes; optimized selection equals the exhaustive compiler oracle", async () => {
  const { service, view, symbols } = await fixture(`
export interface Reader { read(): string; }
export class Wrong { read(): number { return 1; } }
export class Missing {}
export class Broken implements Reader { read(): MissingType { throw 1; } }
export abstract class Abstract implements Reader { abstract read(): string; }
export class Declared implements Reader { read(): string { return 'ok'; } }
export class Structural { read(): string { return 'ok'; } }
export class Inherited extends Structural {}
export abstract class Base { abstract run(): string; }
export class Concrete extends Base { run(): string { return 'ok'; } }
`);
  const result = await resolveImplementations(service, view, symbols, []);
  const brute = await resolveImplementations(service, view, symbols, [], {
    exhaustive: true,
  });
  expect(result.edges).toEqual(brute.edges);
  const types = result.edges.filter(
    (edge) => edge.targetQualifiedName === "Reader",
  );
  expect(
    types.map((edge) => [edge.base.sourceQualifiedName, edge.detection]),
  ).toEqual([
    ["Declared", "declared"],
    ["Inherited", "structural"],
    ["Structural", "structural"],
  ]);
  expect(
    result.edges.map(
      (edge) => `${edge.base.sourceQualifiedName}->${edge.targetQualifiedName}`,
    ),
  ).toContain("Declared.read->Reader.read");
  expect(
    result.edges.some((edge) =>
      edge.base.sourceQualifiedName.startsWith("Broken"),
    ),
  ).toBe(false);
  expect(
    result.edges
      .filter((edge) => edge.base.sourceQualifiedName.startsWith("Concrete"))
      .map((edge) => [
        edge.base.sourceQualifiedName,
        edge.targetQualifiedName,
        edge.relation,
      ]),
  ).toEqual([
    ["Concrete", "Base", "IMPLEMENTS"],
    ["Concrete.run", "Base.run", "OVERRIDES"],
  ]);
  expect(result.limitations).toContain(
    "Type declarations with compiler errors are excluded from implementation proofs.",
  );
});

it("uses concrete generic instances without replacing unknown parameters with any", async () => {
  const { service, view, symbols } = await fixture(`
export interface Box<T> { get(): T; }
export class StringBox implements Box<string> { get(): string { return ''; } }
export class NumericBox { get(): number { return 0; } }
export class Generic<T> { constructor(private value: T) {} get(): T { return this.value; } }
export const instance = new Generic<string>('value');
export type TextBox = Box<string>;
`);
  const result = await resolveImplementations(service, view, symbols, []);
  const brute = await resolveImplementations(service, view, symbols, [], {
    exhaustive: true,
  });
  expect(result.edges).toEqual(brute.edges);
  expect(
    result.edges.map((edge) => [
      edge.base.sourceQualifiedName,
      edge.targetQualifiedName,
      edge.detection,
    ]),
  ).toEqual([
    ["Generic", "Box", "structural"],
    ["Generic", "TextBox", "structural"],
    ["Generic.get", "Box.get", "structural"],
    ["StringBox", "Box", "declared"],
    ["StringBox", "TextBox", "structural"],
    ["StringBox.get", "Box.get", "declared"],
  ]);
});

it("keeps empty and fully optional targets aligned with the exhaustive oracle", async () => {
  const { service, view, symbols } = await fixture(`
export interface Empty {}
export interface Optional { read?(): string; }
export class Blank {}
`);
  const result = await resolveImplementations(service, view, symbols, []);
  const brute = await resolveImplementations(service, view, symbols, [], {
    exhaustive: true,
  });
  expect(result.edges).toEqual(brute.edges);
  expect(
    result.edges.map((edge) => [
      edge.base.sourceQualifiedName,
      edge.targetQualifiedName,
    ]),
  ).toEqual([
    ["Blank", "Empty"],
    ["Blank", "Optional"],
  ]);
});

it("retains canonical provider identities for imported interfaces and methods", async () => {
  const root = await temporaryRoot("kivgraph-cross-implementations-");
  roots.push(root);
  const provider = path.join(root, "provider");
  const consumer = path.join(root, "consumer");
  for (const dir of [
    provider,
    path.join(provider, "src"),
    path.join(provider, "dist"),
    consumer,
    path.join(consumer, "node_modules"),
  ])
    await mkdir(dir, { recursive: true });
  await writeFile(
    path.join(provider, "package.json"),
    JSON.stringify({
      name: "contracts",
      version: "1.0.0",
      types: "./dist/contracts.d.ts",
    }),
  );
  await writeFile(
    path.join(provider, "src/contracts.ts"),
    "export interface Reader { read(): string; }\n",
  );
  const config = {
    compilerOptions: {
      strict: true,
      target: "ES2022",
      module: "NodeNext",
      moduleResolution: "NodeNext",
      noEmit: true,
    },
    include: ["*.ts"],
  };
  await writeFile(
    path.join(provider, "dist/contracts.d.ts"),
    "export interface Reader { read(): string; }\n",
  );
  await writeFile(
    path.join(provider, "tsconfig.json"),
    JSON.stringify({ ...config, include: ["src/*.ts"] }),
  );
  await writeFile(path.join(consumer, "tsconfig.json"), JSON.stringify(config));
  await symlink(provider, path.join(consumer, "node_modules/contracts"), "dir");
  await writeFile(
    path.join(consumer, "main.ts"),
    `import type {Reader as External} from 'contracts';
 export class Declared implements External { read(): string {return '';} }
 export class Structural { read(): string {return '';} }
 export class Wrong { read(): number {return 1;} }`,
  );
  const service = LanguageService.create({ cwd: consumer });
  services.push(service);
  const project = path.join(consumer, "tsconfig.json");
  await service.openProject(project);
  const view = service.project(project);
  const symbols = await extractLocalSymbols(service, view);
  const imports = await resolveImportedSymbols(
    service,
    view,
    createPackageProviderRegistry([
      {
        name: "contracts",
        version: "1.0.0",
        repository: "provider-repo",
        rootPath: provider,
        manifestPath: path.join(provider, "package.json"),
        projectPath: path.join(provider, "tsconfig.json"),
        sourceRoots: [path.join(provider, "src")],
        declarationRoots: [path.join(provider, "dist")],
      },
    ]),
  );
  expect(
    imports.symbols[0]?.target.identity?.repository,
    JSON.stringify(imports),
  ).toBe("provider-repo");
  const result = await resolveImplementations(
    service,
    view,
    symbols,
    imports.symbols,
  );
  const brute = await resolveImplementations(
    service,
    view,
    symbols,
    imports.symbols,
    { exhaustive: true },
  );
  expect(result.edges).toEqual(brute.edges);
  expect(
    result.edges.map((edge) => [
      edge.base.sourceQualifiedName,
      edge.identity?.qualifiedName,
      edge.detection,
    ]),
  ).toEqual([
    ["Declared", "Reader", "declared"],
    ["Declared.read", "Reader.read", "declared"],
    ["Structural", "Reader", "structural"],
    ["Structural.read", "Reader.read", "structural"],
  ]);
  expect(
    result.edges.map((edge) => [
      edge.base.sourceQualifiedName,
      edge.identity?.repository,
      edge.identity?.file,
    ]),
  ).toEqual([
    ["Declared", "provider-repo", "src/contracts.ts"],
    ["Declared.read", "provider-repo", "src/contracts.ts"],
    ["Structural", "provider-repo", "src/contracts.ts"],
    ["Structural.read", "provider-repo", "src/contracts.ts"],
  ]);
});
