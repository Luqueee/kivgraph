/**
 * Write the versioned precision artifacts of LUQUE-0709.
 *
 * The output is deterministic: no timestamps, no machine paths, so a rerun on
 * another host produces byte-identical files and a regression is visible in
 * the diff.
 */

import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import {
  measureCrossRepositoryPrecision,
  type PrecisionMetrics,
  type PrecisionReport,
} from "./precision-report.js";

const OUTPUT = path.resolve(
  import.meta.dirname,
  "../../benchmarks/typescript-cross-repo",
);

export async function writePrecisionArtifacts(
  outputDirectory: string = OUTPUT,
): Promise<PrecisionReport> {
  const report = await measureCrossRepositoryPrecision();
  await mkdir(outputDirectory, { recursive: true });
  await writeFile(
    path.join(outputDirectory, "results.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
  await writeFile(
    path.join(outputDirectory, "report.md"),
    renderReport(report),
    "utf8",
  );
  return report;
}

function renderReport(report: PrecisionReport): string {
  const lines: string[] = [
    "# Precisión cross-repository TypeScript",
    "",
    "Medición de LUQUE-0709 sobre los fixtures de LUQUE-0707 y LUQUE-0708.",
    "Se regenera con `pnpm precision` desde `ts-worker`.",
    "",
    "## Fixtures",
    "",
    ...report.fixtures.map((fixture) => `- \`${fixture}\``),
    "",
    "## Totales",
    "",
    ...renderMetrics(report.totals),
    "",
    "## Casos",
    "",
    "| Caso | Aristas esperadas | TP | FP | FN | Precisión | Recall | No resueltas correctas | Posiciones en fuente |",
    "| --- | --- | --- | --- | --- | --- | --- | --- | --- |",
    ...report.cases.map((entry) => {
      const metrics = entry.metrics;
      return `| ${entry.name} | ${metrics.expectedEdges} | ${metrics.truePositives} | ${metrics.falsePositives} | ${metrics.falseNegatives} | ${format(metrics.precision)} | ${format(metrics.recall)} | ${metrics.unresolvedCorrectlyClassified}/${metrics.expectedUnresolved} | ${metrics.mappedSourcePositions}/${metrics.expectedSourcePositions} |`;
    }),
    "",
    "## Gate",
    "",
    "```text",
    report.gate,
    "```",
    "",
  ];
  return lines.join("\n");
}

function renderMetrics(metrics: PrecisionMetrics): string[] {
  return [
    `- true positives: ${metrics.truePositives}`,
    `- false positives: ${metrics.falsePositives}`,
    `- false negatives: ${metrics.falseNegatives}`,
    `- precision: ${format(metrics.precision)}`,
    `- recall: ${format(metrics.recall)}`,
    `- false exact edges: ${metrics.falseExactEdges}`,
    `- unresolved correctly classified: ${metrics.unresolvedCorrectlyClassified}/${metrics.expectedUnresolved}`,
    `- exact source positions: ${metrics.mappedSourcePositions}/${metrics.expectedSourcePositions}`,
  ];
}

function format(value: number): string {
  return value.toFixed(4);
}

const report = await writePrecisionArtifacts();
process.stdout.write(`${report.gate}\n`);
process.exitCode = report.gate === "TYPESCRIPT_CROSS_REPO_PASS" ? 0 : 1;
