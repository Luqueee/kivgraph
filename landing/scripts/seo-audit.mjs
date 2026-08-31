import { existsSync } from "node:fs";
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import {
  auditBuiltSite,
  extractPageSignals,
  markdownPathForRoute,
  routeFromClientFile,
} from "../src/seo-audit.mjs";
import { PRODUCTION_ORIGIN } from "../src/site.mjs";

const CLIENT_DIR = new URL("../dist/client/", import.meta.url);
const SERVER_ENTRY = new URL("../dist/server/entry.mjs", import.meta.url);
const jsonOutput = process.argv.includes("--json");

if (existsSync(".env")) {
  process.loadEnvFile();
}

async function filesUnder(directory, prefix = "") {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const relativePath = join(prefix, entry.name);
    if (entry.isDirectory()) {
      files.push(
        ...(await filesUnder(
          new URL(`${entry.name}/`, directory),
          relativePath,
        )),
      );
    } else {
      files.push(relativePath.replaceAll("\\", "/"));
    }
  }
  return files;
}

async function serverHasRawMarkdownRoute() {
  try {
    const serverEntry = await readFile(SERVER_ENTRY, "utf8");
    return serverEntry.includes('"route":"/raw/[...slug].md"');
  } catch {
    return false;
  }
}

const relativeFiles = await filesUnder(CLIENT_DIR);
const documents = [];
for (const relativeFile of relativeFiles.filter((file) =>
  file.endsWith(".html"),
)) {
  if (relativeFile === "404.html") {
    continue;
  }
  documents.push({
    pathname: routeFromClientFile(relativeFile),
    html: await readFile(new URL(relativeFile, CLIENT_DIR), "utf8"),
  });
}

const rawMarkdownRoute = await serverHasRawMarkdownRoute();
const runtimePaths = rawMarkdownRoute
  ? documents
      .filter(({ html }) => extractPageSignals(html).markdownAlternate)
      .map(({ pathname }) => markdownPathForRoute(pathname))
      .filter((pathname) => pathname !== undefined)
  : [];
const issues = auditBuiltSite({
  documents,
  files: new Set(relativeFiles),
  runtimePaths,
  siteOrigin: process.env.KIVGRAPH_LANDING_URL ?? PRODUCTION_ORIGIN,
});
const errors = issues.filter((item) => item.severity === "error");
const warnings = issues.filter((item) => item.severity === "warning");
const result = {
  documents: documents.length,
  errors: errors.length,
  warnings: warnings.length,
  issues,
};

if (jsonOutput) {
  console.log(JSON.stringify(result, null, 2));
} else {
  console.log(
    `SEO/AEO audit: ${errors.length === 0 ? "PASS" : "FAIL"} — ${documents.length} documents, ${errors.length} errors, ${warnings.length} warnings`,
  );
  for (const item of issues) {
    console.log(
      `${item.severity.toUpperCase()} ${item.path} [${item.code}] ${item.message}`,
    );
  }
}

process.exitCode = errors.length === 0 ? 0 : 1;
