/**
 * Precision and recall of TypeScript cross-repository resolution.
 *
 * The ground truth is the pair of fixtures of LUQUE-0707 and LUQUE-0708. Both
 * are checked against the real resolver output, so a regression in exactness
 * shows up as a false exact edge instead of a silent quality loss.
 */

import path from "node:path";

import { LanguageService } from "./language-service.js";
import {
  createPackageProviderRegistry,
  type PackageProvider,
} from "./package-import-resolver.js";
import { resolveProviderSourcePositions } from "./provider-source-position-resolver.js";
import {
  type PackageProviderConflict,
  resolveUnresolvedReferences,
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
  "@kivgraph-fixture/shared",
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
      "src/derived.ts#Widget -> @kivgraph-fixture/shared:Widget -> cross-repository/shared-library/dist/inheritance.d.ts",
      "src/direct.ts#compute -> @kivgraph-fixture/shared:compute -> cross-repository/shared-library/dist/value.d.ts",
      "src/direct.ts#value -> @kivgraph-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
      "src/direct.ts#Shape -> @kivgraph-fixture/shared:Shape -> cross-repository/shared-library/dist/value.d.ts",
    ],
    expectedUnresolved: [],
    expectedSourcePositions: 4,
  },
  {
    name: "consumer-b",
    root: path.join(POSITIVE, "consumer-b"),
    providers: [sharedProvider],
    conflicts: [],
    expectedEdges: [
      "src/barrel.ts#helper -> @kivgraph-fixture/shared:aliasedHelper -> cross-repository/shared-library/dist/helper.d.ts",
      "src/barrel.ts#compute -> @kivgraph-fixture/shared:compute -> cross-repository/shared-library/dist/value.d.ts",
      "src/barrel.ts#republished -> @kivgraph-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
      // The same declaration, bound in a file whose own text names no package:
      // `through-barrel.ts` imports it from "./barrel.js", and the provider is
      // the one that barrel already resolved.
      "src/through-barrel.ts#republished -> @kivgraph-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
    ],
    expectedUnresolved: [],
    expectedSourcePositions: 4,
  },
  {
    name: "consumer-negative",
    root: path.join(NEGATIVE, "consumer"),
    providers: [
      sharedProvider,
      provider(
        "@kivgraph-fixture/twin",
        "1.0.0",
        "twin",
        path.join(NEGATIVE, "twin"),
      ),
      provider(
        "@kivgraph-fixture/unmapped",
        "1.0.0",
        "unmapped",
        path.join(NEGATIVE, "unmapped"),
      ),
      provider(
        "@kivgraph-fixture/duplicated",
        "1.0.0",
        "duplicated-a",
        path.join(NEGATIVE, "duplicated-a"),
      ),
      provider(
        "@kivgraph-fixture/drifting",
        "2.0.0",
        "drifting",
        path.join(NEGATIVE, "drifting"),
      ),
      {
        ...provider(
          "@kivgraph-fixture/nomap",
          "1.0.0",
          "nomap",
          path.join(NEGATIVE, "nomap"),
        ),
        projectPath: path.join(NEGATIVE, "nomap/tsconfig.json"),
      },
    ],
    conflicts: [
      {
        packageName: "@kivgraph-fixture/duplicated",
        kind: "AMBIGUOUS_PACKAGE_PROVIDER",
        repositories: ["duplicated-a", "duplicated-b"],
      },
      {
        packageName: "@kivgraph-fixture/drifting",
        kind: "PACKAGE_VERSION_MISMATCH",
        versions: ["1.0.0", "2.0.0"],
      },
    ],
    expectedEdges: [
      "src/consumer.ts#compute -> @kivgraph-fixture/twin:compute -> cross-repository-negative/twin/dist/index.d.ts",
      "src/consumer.ts#sharedValue -> @kivgraph-fixture/shared:value -> cross-repository/shared-library/dist/value.d.ts",
      "src/consumer.ts#unmapped -> @kivgraph-fixture/unmapped:unmapped -> cross-repository-negative/unmapped/dist/index.d.ts",
      "src/consumer.ts#plain -> @kivgraph-fixture/nomap:plain -> cross-repository-negative/nomap/dist/index.d.ts",
    ],
    expectedUnresolved: [
      "@kivgraph-fixture/unmapped:DECLARATION_SOURCE_NOT_MAPPED:unmapped",
      "@kivgraph-fixture/unmapped:EXPORT_NOT_FOUND:missing",
      "@kivgraph-fixture/duplicated:AMBIGUOUS_PACKAGE_PROVIDER:-",
      "@kivgraph-fixture/drifting:VERSION_MISMATCH:-",
    ],
    // `unmapped` publishes neither map nor sources, so it stays unplaceable;
    // `nomap` has no map but its own project can place the symbol.
    expectedSourcePositions: 3,
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
  const registry = createPackageProviderRegistry(testCase.providers);
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

    const crossRepositoryEdges = [
      ...resolution.symbols,
      ...resolution.reexports,
    ];
    const observedEdges = new Set(
      crossRepositoryEdges.map((edge) => {
        const binding =
          edge.kind === "IMPORTS_SYMBOL" ? edge.consumer : edge.export;
        return [
          path.relative(testCase.root, binding.fileName),
          `#${binding.name} -> ${edge.packageName}:${edge.exportedName} -> `,
          shortDeclaration(edge.target.declarations[0]?.fileName ?? ""),
        ].join("");
      }),
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

    // Positions come from the declaration map first and, when the provider
    // ships none, from the provider's own project.
    const providerPositions = await resolveProviderSourcePositions(resolution);
    const mappedSourcePositions =
      crossRepositoryEdges.filter(
        (edge) => edge.target.declarations[0]?.sourcePosition !== undefined,
      ).length + providerPositions.positions.length;
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
