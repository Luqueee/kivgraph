#!/usr/bin/env node

import { readFile } from "node:fs/promises";

const LIMITS = {
  meta_http_p95_ms: 50,
  payload_topology_p95_ms: 500,
  first_frame_p95_ms: 1000,
  pan_zoom_p95_ms: 33.3,
  hover_p95_ms: 5,
  neighborhood_p95_ms: 5,
  payload_max_bytes: 32 * 1024 * 1024,
  heap_bytes: 512 * 1024 * 1024,
  errors: 0,
};

function argument(name, fallback) {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

function validate(results) {
  const metrics = results.metrics ?? {};
  const failures = [];
  for (const [metric, limit] of Object.entries(LIMITS)) {
    const value = metrics[metric];
    if (typeof value !== "number" || !Number.isFinite(value)) {
      failures.push(`${metric}: missing or non-finite`);
      continue;
    }
    const passes = metric === "errors" ? value === limit : value <= limit;
    if (!passes) failures.push(`${metric}: ${value} exceeds ${limit}`);
  }
  if (results.dataset?.reference_match !== true) {
    failures.push("dataset: reference corpus is not the required 100k symbols / 1m edges corpus");
  }
  if (results.gate?.snapshot_and_payload_validated !== true) {
    failures.push("gate: snapshot and payload validation evidence is missing");
  }
  return failures;
}

const path = argument("--results", "benchmarks/web-viewer/results.json");
const allowLimitations = process.argv.includes("--allow-limitations");
const results = JSON.parse(await readFile(path, "utf8"));
const failures = validate(results);
if (failures.length === 0) {
  console.log("WEB_VIEWER_PERFORMANCE_PASS");
  process.exit(0);
}

console.error("WEB_VIEWER_PERFORMANCE_PASS not emitted");
for (const failure of failures) console.error(`- ${failure}`);
if (!allowLimitations) process.exit(1);
console.log("WEB_VIEWER_PERFORMANCE_PASS_WITH_LIMITS");
