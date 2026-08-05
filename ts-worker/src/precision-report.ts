/**
 * Precision and recall of TypeScript cross-repository resolution.
 *
 * The ground truth is the pair of fixtures of LUQUE-0707 and LUQUE-0708. Both
 * are checked against the real resolver output, so a regression in exactness
 * shows up as a false exact edge instead of a silent quality loss.
 */

import path from "node:path";

import { LanguageService } from "./language-service.js";
import type {
  PackageProvider,
  PackageProviderRegistry,
} from "./package-import-resolver.js";
import {
  resolveUnresolvedReferences,
  type PackageProviderConflict,
} from "./unresolved-reference-resolver.js";

const REPOSITORY_ROOT = path.resolve(import.meta.dirname, "../..");
const POSITIVE = path.join(
  REPOSITORY_ROOT,
  "testdata/typescript/cross-repository",
);
const NEGATIVE = path.join(
  REPOSITORY_ROOT,
  "testdata/typescript/cross-repository-negative",
);
const SHARED_ROOT = path.join(POSITIVE, "shared-library");

export interface PrecisionMetrics {
  readonly expectedEdges: number;
  readonly truePositives: number;
  readonly falsePositives: number;
  readonly falseNegatives: number;
  readonly precision: number;
  readonly recall: number;
  /** Exact edges the fixtures prove wrong. The gate requires zero. */
  readonly falseExactEdges: number;
  readonly expectedUnresolved: number;
  readonly unresolvedCorrectlyClassified: number;
  readonly unresolvedMisclassified: number;
  /** Edges whose declaration was placed in its real source position. */
  readonly expectedSourcePositions: number;
  readonly mappedSourcePositions: number;
}

export interface PrecisionCaseReport {
  readonly name: string;
  readonly metrics: PrecisionMetrics;
  readonly missingEdges: readonly string[];
  readonly unexpectedEdges: readonly string[];
  readonly missingUnresolved: readonly string[];
  readonly unexpectedUnresolved: readonly string[];
}

export interface PrecisionReport {
  readonly fixtures: readonly string[];
  readonly cases: readonly PrecisionCaseReport[];
  readonly totals: PrecisionMetrics;
  readonly gate: "TYPESCRIPT_CROSS_REPO_PASS" | "TYPESCRIPT_CROSS_REPO_FAIL";
}

interface PrecisionCase {
  readonly name: string;
  readonly root: string;
  readonly providers: readonly PackageProvider[];
  readonly conflicts: readonly PackageProviderConflict[];
  /** `consumerFile#localName -> package:exportedName -> declarationFile`. */
  readonly expectedEdges: readonly string[];
  /** `package:reason:requestedSymbol`. */
  readonly expectedUnresolved: readonly string[];
  /** Edges whose declaration map must place the symbol in its source. */
  readonly expectedSourcePositions: number;
}

function provider(
  name: string,
  version: string,
  repository: string,
  rootPath: string,
  extra: Partial<PackageProvider> = {},
): PackageProvider {
  return {
    name,
    version,
    repository,
    rootPath,
    manifestPath: path.join(rootPath, "package.json"),
    typesPath: path.join(rootPath, "dist/index.d.ts"),
    ...extra,
  };
}

const sharedProvider = provider(
  "@luque-fixture/shared",
  "1.4.2",
  "shared-library",
  SHARED_ROOT,
  {
    projectPath: path.join(SHARED_ROOT, "tsconfig.json"),
    sourceRoots: [path.join(SHARED_ROOT, "src")],
    declarationRoots: [path.join(SHARED_ROOT, "dist")],
    rootDir: "src",
    outDir: "dist",
  },
);

const cases: readonly PrecisionCase[] = [
  {
    name: "consumer-a",
    root: path.join(POSITIVE, "consumer-a"),
    providers: [sharedProvider],
    conflicts: [],
    expectedEdges: [
      "src/direct.ts#compute -> @luque-fixture/shared:compute -> cross-repository/shared-library/dist/value.d.ts",
      "src/direct.ts#value -> @luque-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
      "src/direct.ts#Shape -> @luque-fixture/shared:Shape -> cross-repository/shared-library/dist/value.d.ts",
    ],
    expectedUnresolved: [],
    expectedSourcePositions: 3,
  },
  {
    name: "consumer-b",
    root: path.join(POSITIVE, "consumer-b"),
    providers: [sharedProvider],
    conflicts: [],
    expectedEdges: [
      "src/barrel.ts#helper -> @luque-fixture/shared:aliasedHelper -> cross-repository/shared-library/dist/helper.d.ts",
      "src/barrel.ts#republished -> @luque-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
    ],
    expectedUnresolved: [],
    expectedSourcePositions: 2,
  },
  {
    name: "consumer-negative",
    root: path.join(NEGATIVE, "consumer"),
    providers: [
      sharedProvider,
      provider(
        "@luque-fixture/twin",
        "1.0.0",
        "twin",
        path.join(NEGATIVE, "twin"),
      ),
      provider(
        "@luque-fixture/unmapped",
        "1.0.0",
        "unmapped",
        path.join(NEGATIVE, "unmapped"),
      ),
      provider(
        "@luque-fixture/duplicated",
        "1.0.0",
        "duplicated-a",
        path.join(NEGATIVE, "duplicated-a"),
      ),
      provider(
        "@luque-fixture/drifting",
        "2.0.0",
        "drifting",
        path.join(NEGATIVE, "drifting"),
      ),
    ],
    conflicts: [
      {
        packageName: "@luque-fixture/duplicated",
        kind: "AMBIGUOUS_PACKAGE_PROVIDER",
        repositories: ["duplicated-a", "duplicated-b"],
      },
      {
        packageName: "@luque-fixture/drifting",
        kind: "PACKAGE_VERSION_MISMATCH",
        versions: ["1.0.0", "2.0.0"],
      },
    ],
    expectedEdges: [
      "src/consumer.ts#compute -> @luque-fixture/twin:compute -> cross-repository-negative/twin/dist/index.d.ts",
      "src/consumer.ts#sharedValue -> @luque-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
      "src/consumer.ts#unmapped -> @luque-fixture/unmapped:unmapped -> cross-repository-negative/unmapped/dist/index.d.ts",
    ],
    expectedUnresolved: [
      "@luque-fixture/unmapped:DECLARATION_SOURCE_NOT_MAPPED:unmapped",
      "@luque-fixture/unmapped:EXPORT_NOT_FOUND:missing",
      "@luque-fixture/duplicated:AMBIGUOUS_PACKAGE_PROVIDER:-",
      "@luque-fixture/drifting:VERSION_MISMATCH:-",
    ],
    // `unmapped` ships no declaration map, so only two edges can be placed.
    expectedSourcePositions: 2,
  },
];

/** Measure every fixture case and aggregate the totals. */
export async function measureCrossRepositoryPrecision(): Promise<PrecisionReport> {
  const reports: PrecisionCaseReport[] = [];
  for (const testCase of cases) {
    reports.push(await measureCase(testCase));
  }
  const totals = aggregate(reports.map((report) => report.metrics));
  return {
    fixtures: [
      path.relative(REPOSITORY_ROOT, POSITIVE),
      path.relative(REPOSITORY_ROOT, NEGATIVE),
    ],
    cases: reports,
    totals,
    gate:
      totals.falseExactEdges === 0 &&
      totals.falseNegatives === 0 &&
      totals.unresolvedMisclassified === 0 &&
      totals.mappedSourcePositions === totals.expectedSourcePositions
        ? "TYPESCRIPT_CROSS_REPO_PASS"
        : "TYPESCRIPT_CROSS_REPO_FAIL",
  };
}

async function measureCase(
  testCase: PrecisionCase,
): Promise<PrecisionCaseReport> {
  const registry: PackageProviderRegistry = {
    get: (name) => testCase.providers.find((entry) => entry.name === name),
  };
  const service = LanguageService.create({ cwd: testCase.root });
  try {
    const configFileName = path.join(testCase.root, "tsconfig.json");
    await service.openProject(configFileName);
    const view = service.project(configFileName);
    const resolution = await resolveUnresolvedReferences(
      service,
      view,
      registry,
      { conflicts: testCase.conflicts },
    );

    const observedEdges = new Set(
      resolution.symbols.map((symbol) =>
        [
          path.relative(testCase.root, symbol.consumer.fileName),
          `#${symbol.consumer.name} -> ${symbol.packageName}:${symbol.exportedName} -> `,
          shortDeclaration(symbol.target.declarations[0]?.fileName ?? ""),
        ].join(""),
      ),
    );
    const observedUnresolved = new Set(
      resolution.unresolved.map(
        (entry) =>
          `${entry.packageName}:${entry.reason}:${entry.requestedSymbol ?? "-"}`,
      ),
    );

    const expectedEdges = new Set(testCase.expectedEdges);
    const expectedUnresolved = new Set(testCase.expectedUnresolved);
    const missingEdges = [...expectedEdges].filter(
      (edge) => !observedEdges.has(edge),
    );
    const unexpectedEdges = [...observedEdges].filter(
      (edge) => !expectedEdges.has(edge),
    );
    const missingUnresolved = [...expectedUnresolved].filter(
      (entry) => !observedUnresolved.has(entry),
    );
    const unexpectedUnresolved = [...observedUnresolved].filter(
      (entry) => !expectedUnresolved.has(entry),
    );

    const mappedSourcePositions = resolution.symbols.filter(
      (symbol) => symbol.target.declarations[0]?.sourcePosition !== undefined,
    ).length;
    const truePositives = expectedEdges.size - missingEdges.length;
    const metrics: PrecisionMetrics = {
      expectedEdges: expectedEdges.size,
      truePositives,
      falsePositives: unexpectedEdges.length,
      falseNegatives: missingEdges.length,
      precision: ratio(truePositives, truePositives + unexpectedEdges.length),
      recall: ratio(truePositives, expectedEdges.size),
      falseExactEdges: unexpectedEdges.length,
      expectedUnresolved: expectedUnresolved.size,
      unresolvedCorrectlyClassified:
        expectedUnresolved.size - missingUnresolved.length,
      unresolvedMisclassified:
        missingUnresolved.length + unexpectedUnresolved.length,
      expectedSourcePositions: testCase.expectedSourcePositions,
      mappedSourcePositions,
    };

    return {
      name: testCase.name,
      metrics,
      missingEdges: missingEdges.sort(),
      unexpectedEdges: unexpectedEdges.sort(),
      missingUnresolved: missingUnresolved.sort(),
      unexpectedUnresolved: unexpectedUnresolved.sort(),
    };
  } finally {
    await service.close();
  }
}

/** Keep the fixture-relative tail of a declaration path, never the temp root. */
function shortDeclaration(fileName: string): string {
  const parts = fileName.split(`testdata${path.sep}typescript${path.sep}`);
  return parts.length === 2 ? (parts[1] ?? fileName) : fileName;
}

function aggregate(metrics: readonly PrecisionMetrics[]): PrecisionMetrics {
  const sum = (select: (value: PrecisionMetrics) => number): number =>
    metrics.reduce((total, value) => total + select(value), 0);
  const truePositives = sum((value) => value.truePositives);
  const falsePositives = sum((value) => value.falsePositives);
  const expectedEdges = sum((value) => value.expectedEdges);
  return {
    expectedEdges,
    truePositives,
    falsePositives,
    falseNegatives: sum((value) => value.falseNegatives),
    precision: ratio(truePositives, truePositives + falsePositives),
    recall: ratio(truePositives, expectedEdges),
    falseExactEdges: sum((value) => value.falseExactEdges),
    expectedUnresolved: sum((value) => value.expectedUnresolved),
    unresolvedCorrectlyClassified: sum(
      (value) => value.unresolvedCorrectlyClassified,
    ),
    unresolvedMisclassified: sum((value) => value.unresolvedMisclassified),
    expectedSourcePositions: sum((value) => value.expectedSourcePositions),
    mappedSourcePositions: sum((value) => value.mappedSourcePositions),
  };
}

function ratio(numerator: number, denominator: number): number {
  return denominator === 0 ? 1 : numerator / denominator;
}
