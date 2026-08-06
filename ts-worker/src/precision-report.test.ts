import { describe, expect, it } from "vitest";

import { measureCrossRepositoryPrecision } from "./precision-report.js";

describe("cross-repository precision", () => {
  it("resolves both fixtures with no false exact edge", async () => {
    const report = await measureCrossRepositoryPrecision();

    for (const entry of report.cases) {
      expect({
        name: entry.name,
        missingEdges: entry.missingEdges,
        unexpectedEdges: entry.unexpectedEdges,
        missingUnresolved: entry.missingUnresolved,
        unexpectedUnresolved: entry.unexpectedUnresolved,
      }).toEqual({
        name: entry.name,
        missingEdges: [],
        unexpectedEdges: [],
        missingUnresolved: [],
        unexpectedUnresolved: [],
      });
    }

    expect(report.totals).toMatchObject({
      expectedEdges: 11,
      truePositives: 11,
      falsePositives: 0,
      falseNegatives: 0,
      precision: 1,
      recall: 1,
      falseExactEdges: 0,
      expectedUnresolved: 4,
      unresolvedCorrectlyClassified: 4,
      expectedSourcePositions: 10,
      mappedSourcePositions: 10,
      unresolvedMisclassified: 0,
    });
    expect(report.gate).toBe("TYPESCRIPT_CROSS_REPO_PASS");
  });
});
