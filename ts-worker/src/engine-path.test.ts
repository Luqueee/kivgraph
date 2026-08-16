import { existsSync } from "node:fs";
import { mkdir, symlink, writeFile } from "node:fs/promises";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { enginePath, forgetEnginePaths } from "./engine-path.js";
import { temporaryRoot } from "./temporary-root.js";

describe("engine paths", () => {
  it("leaves a relative path alone", () => {
    expect(enginePath("src/index.ts")).toBe("src/index.ts");
  });

  it("recovers the spelling on disk of a folded path", async () => {
    const root = await temporaryRoot("kivgraph-engine-path-");
    const directory = path.join(root, "Dist");
    await mkdir(directory);
    const file = path.join(directory, "Index.d.ts");
    await writeFile(file, "export {};\n");
    const folded = path.join(root, "dist", "index.d.ts");

    const answer = enginePath(folded);

    // On a folding filesystem the lower-cased path the engine reports names
    // this file, so it must come back spelled as the disk spells it. On a
    // case-sensitive one it names nothing and must be returned untouched.
    expect(answer).toBe(existsSync(folded) ? file : folded);
  });

  it("keeps a component that does not exist", async () => {
    const root = await temporaryRoot("kivgraph-engine-path-");
    const missing = path.join(root, "absent", "file.ts");

    expect(enginePath(missing)).toBe(missing);
  });

  // realpath would rewrite a pnpm node_modules entry into its .pnpm store
  // location and change the facts, so the walk corrects casing only.
  it("does not resolve a symlinked component", async () => {
    const root = await temporaryRoot("kivgraph-engine-path-");
    const real = path.join(root, "store");
    await mkdir(real);
    await writeFile(path.join(real, "index.d.ts"), "export {};\n");
    const link = path.join(root, "linked");
    await symlink(real, link, "dir");

    const throughLink = path.join(link, "index.d.ts");

    expect(enginePath(throughLink)).toBe(throughLink);
  });

  // The memoised listing predates anything an indexing run writes, so a miss
  // is only trusted after the directory is read again.
  it("sees an entry created after the directory was first read", async () => {
    const root = await temporaryRoot("kivgraph-engine-path-");
    const later = path.join(root, "Later.ts");
    const asked = path.join(root, "later.ts");
    forgetEnginePaths();
    // Reading the empty directory memoises a listing without the file.
    expect(enginePath(asked)).toBe(asked);

    await writeFile(later, "export {};\n");

    // Only a folding filesystem can answer the lower-cased spelling, and only
    // if the stale listing is read again. On a case-sensitive one that
    // spelling names nothing and must stay untouched.
    expect(enginePath(asked)).toBe(existsSync(asked) ? later : asked);
  });
});
