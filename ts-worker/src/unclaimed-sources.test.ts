import path from "node:path";

import { describe, expect, it } from "vitest";

import { collectFacts } from "./facts-cli.js";
import { createPackageProviderRegistry } from "./package-import-resolver.js";

const ROOT = path.resolve(
  import.meta.dirname,
  "../../testdata/typescript/unclaimed-sources",
);
const registry = createPackageProviderRegistry([]);

const UNCLAIMED = [
  path.join(ROOT, "scripts", "release.ts"),
  path.join(ROOT, "tests", "case.test.ts"),
  path.join(ROOT, "tests", "helpers", "fixture.ts"),
  path.join(ROOT, "tests", "widget.tsx"),
];

describe("unclaimed TypeScript sources", () => {
  it("sees nothing outside the project when no unclaimed path is given", async () => {
    const payload = await collectFacts("unclaimed", ROOT, registry);

    expect(payload.files).toEqual(["src/case.ts", "src/index.ts"]);
    expect(
      payload.references.filter((reference) =>
        reference.file.startsWith("tests/"),
      ),
    ).toEqual([]);
  });

  it("makes an unclaimed file a caller of the symbol it calls", async () => {
    const payload = await collectFacts(
      "unclaimed",
      ROOT,
      registry,
      undefined,
      UNCLAIMED,
    );

    expect(payload.files).toEqual([
      "scripts/release.ts",
      "src/case.ts",
      "src/index.ts",
      "tests/case.test.ts",
      "tests/helpers/fixture.ts",
      "tests/widget.tsx",
    ]);
    expect(
      payload.references.filter((reference) =>
        reference.file.startsWith("tests/"),
      ),
    ).toEqual([
      {
        file: "tests/case.test.ts",
        kind: "CALLS_DIRECT",
        sourceQualifiedName: "readsTheRequiredField",
        targetQualifiedName: "getRequiredField",
        targetFile: "src/case.ts",
        startLine: 5,
        start: 158,
        end: 174,
        text: "getRequiredField",
      },
      {
        file: "tests/case.test.ts",
        kind: "REFERENCES",
        sourceQualifiedName: "readsTheRequiredField",
        targetQualifiedName: "record",
        targetFile: "tests/helpers/fixture.ts",
        startLine: 5,
        start: 175,
        end: 181,
        text: "record",
      },
    ]);
  });

  // The inferred project holds its own copy of every file the unclaimed files
  // import, and the configured project already declared those. A second
  // declaration of one symbol is what makes a graph claim a symbol lives in
  // two places, so the emitted set has to stay disjoint from the configured
  // one.
  it("declares no symbol twice", async () => {
    const payload = await collectFacts(
      "unclaimed",
      ROOT,
      registry,
      undefined,
      UNCLAIMED,
    );

    const identities = payload.symbols.map(
      (symbol) => `${symbol.file}\u0000${symbol.qualifiedName}`,
    );
    expect(identities).toEqual([...new Set(identities)]);
    expect(
      payload.symbols.filter(
        (symbol) =>
          symbol.file === "src/case.ts" &&
          symbol.qualifiedName === "getRequiredField",
      ),
    ).toHaveLength(1);
  });

  it("rejects an unclaimed path that is relative or escapes the root", async () => {
    await expect(
      collectFacts("unclaimed", ROOT, registry, undefined, [
        "tests/case.test.ts",
      ]),
    ).rejects.toThrow("must be absolute");
    await expect(
      collectFacts("unclaimed", ROOT, registry, undefined, [
        path.resolve(
          ROOT,
          "..",
          "cross-repository",
          "consumer-a",
          "src",
          "direct.ts",
        ),
      ]),
    ).rejects.toThrow("escapes repository root");
  });
});
