import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { LanguageService, LanguageServiceError } from "./language-service.js";

// These tests drive the real native TypeScript server. It is the engine ADR
// 0010 selected, and a mock of it would prove nothing about the persistence
// this task is meant to deliver.

const services: LanguageService[] = [];
const workspaces: string[] = [];

afterEach(async () => {
  await Promise.all(services.splice(0).map((service) => service.close()));
  await Promise.all(
    workspaces
      .splice(0)
      .map((workspace) => rm(workspace, { recursive: true, force: true })),
  );
});

interface Workspace {
  root: string;
  configFileName: string;
  file(relative: string): string;
  write(relative: string, contents: string): Promise<void>;
}

async function createWorkspace(
  files: Record<string, string> = {},
): Promise<Workspace> {
  const root = await mkdtemp(path.join(tmpdir(), "ladygraph-ls-"));
  workspaces.push(root);

  const workspace: Workspace = {
    root,
    configFileName: path.join(root, "tsconfig.json"),
    file: (relative) => path.join(root, relative),
    write: async (relative, contents) => {
      const target = path.join(root, relative);
      await mkdir(path.dirname(target), { recursive: true });
      await writeFile(target, contents, "utf8");
    },
  };

  await writeFile(
    workspace.configFileName,
    JSON.stringify({
      compilerOptions: {
        strict: true,
        target: "ES2022",
        module: "nodenext",
        moduleResolution: "nodenext",
      },
      include: ["src/**/*.ts"],
    }),
    "utf8",
  );
  await mkdir(path.join(root, "src"), { recursive: true });
  for (const [relative, contents] of Object.entries(files)) {
    await workspace.write(relative, contents);
  }
  return workspace;
}

function startService(root: string): LanguageService {
  const service = LanguageService.create({ cwd: root });
  services.push(service);
  return service;
}

function sha256(contents: string): string {
  return createHash("sha256")
    .update(Buffer.from(contents, "utf8"))
    .digest("hex");
}

describe("project", () => {
  it("loads a project and exposes its roots and options", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 42;\n",
      "src/other.ts":
        'import { answer } from "./index.js";\nexport const doubled = answer * 2;\n',
    });
    const service = startService(workspace.root);

    const opened = await service.openProject(workspace.configFileName);
    expect(opened.configFileName).toBe(workspace.configFileName);
    expect([...opened.rootFiles].sort()).toEqual([
      workspace.file("src/index.ts"),
      workspace.file("src/other.ts"),
    ]);
    expect(opened.trackedFiles).toBe(2);

    const view = service.project(workspace.configFileName);
    expect(view.project.compilerOptions.strict).toBe(true);

    const status = service.status();
    expect(status.projectsOpen).toEqual([workspace.configFileName]);
    expect(status.snapshotLive).toBe(true);
    expect(status.closed).toBe(false);
  });

  it("rejects a project that was never opened", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const a = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    expect(() =>
      service.project(workspace.file("missing/tsconfig.json")),
    ).toThrowError(expect.objectContaining({ code: "UNKNOWN_PROJECT" }));
    await expect(
      service.closeProject(workspace.file("missing/tsconfig.json")),
    ).rejects.toThrowError(
      expect.objectContaining({ code: "UNKNOWN_PROJECT" }),
    );
  });

  it("releases a closed project", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const a = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    await service.closeProject(workspace.configFileName);
    expect(service.status().projectsOpen).toEqual([]);
    expect(() => service.project(workspace.configFileName)).toThrowError(
      expect.objectContaining({ code: "UNKNOWN_PROJECT" }),
    );
  });
});

describe("snapshots", () => {
  it("rolls the generation and invalidates older views", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);

    await service.openProject(workspace.configFileName);
    const first = service.project(workspace.configFileName);
    const firstGeneration = service.generation;
    service.assertFresh(first);

    await workspace.write("src/index.ts", "export const answer = 2;\n");
    await service.applyChanges({
      changed: [{ path: workspace.file("src/index.ts") }],
    });

    expect(service.generation).toBeGreaterThan(firstGeneration);
    // A handle captured before the roll belongs to a disposed snapshot. The
    // service must say so instead of letting it be used.
    expect(() => service.assertFresh(first)).toThrowError(
      expect.objectContaining({ code: "STALE_GENERATION" }),
    );
    service.assertFresh(service.project(workspace.configFileName));
  });

  it("keeps the project loaded across snapshots without reopening it", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    await workspace.write("src/index.ts", "export const answer = 2;\n");
    await service.applyChanges({
      changed: [{ path: workspace.file("src/index.ts") }],
    });

    // Opens are reference counted by the server, so the project survives the
    // roll. This is what makes the service persistent rather than per request.
    const view = service.project(workspace.configFileName);
    expect(await view.program.getSourceFileNames()).toContain(
      workspace.file("src/index.ts"),
    );
    expect(service.status().projectsOpen).toEqual([workspace.configFileName]);
  });

  it("releases every snapshot it replaces", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 0;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    for (let revision = 1; revision <= 3; revision += 1) {
      await workspace.write(
        "src/index.ts",
        `export const answer = ${revision};\n`,
      );
      await service.applyChanges({
        changed: [{ path: workspace.file("src/index.ts") }],
      });
    }

    // Four snapshots existed; three were superseded and must be gone. A
    // snapshot left alive keeps its projects loaded in the native server,
    // which is the leak ADR 0005 warns about for long lived workers.
    const status = service.status();
    expect(status.generation).toBe(4);
    expect(status.snapshotsDisposed).toBe(3);
    expect(status.snapshotLive).toBe(true);

    await service.close();
    expect(service.status().snapshotsDisposed).toBe(4);
  });
});

describe("program and checker", () => {
  it("reports the source files of the project", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 42;\n",
      "src/nested/deep.ts": "export const deep = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    const names = await service
      .project(workspace.configFileName)
      .program.getSourceFileNames();
    const owned = names
      .filter((name) => name.startsWith(workspace.root))
      .sort();
    expect(owned).toEqual([
      workspace.file("src/index.ts"),
      workspace.file("src/nested/deep.ts"),
    ]);
  });

  it("resolves a symbol and sees the new content after a change", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 42;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    const before = service.project(workspace.configFileName);
    const symbol = await before.checker.getSymbolAtPosition(
      workspace.file("src/index.ts"),
      13,
    );
    expect(symbol?.name).toBe("answer");

    await workspace.write("src/index.ts", "export const renamed = 42;\n");
    await service.applyChanges({
      changed: [{ path: workspace.file("src/index.ts") }],
    });

    // The checker of the new snapshot must see the edit; a stale checker would
    // still answer "answer" and quietly poison every fact derived from it.
    const after = service.project(workspace.configFileName);
    const renamed = await after.checker.getSymbolAtPosition(
      workspace.file("src/index.ts"),
      13,
    );
    expect(renamed?.name).toBe("renamed");
  });

  it("surfaces semantic diagnostics from the live program", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer: number = 42;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    const clean = await service
      .project(workspace.configFileName)
      .program.getSemanticDiagnostics();
    expect(clean).toHaveLength(0);

    await workspace.write(
      "src/index.ts",
      'export const answer: number = "not a number";\n',
    );
    await service.applyChanges({
      changed: [{ path: workspace.file("src/index.ts") }],
    });

    const broken = await service
      .project(workspace.configFileName)
      .program.getSemanticDiagnostics();
    expect(broken.length).toBeGreaterThan(0);
  });
});

describe("versions", () => {
  it("bumps a version only when the content really changed", async () => {
    const contents = "export const answer = 1;\n";
    const workspace = await createWorkspace({ "src/index.ts": contents });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    const target = workspace.file("src/index.ts");
    expect(service.version(target)).toBe(1);
    expect(service.contentHash(target)).toBe(sha256(contents));

    // Announced but byte identical: no version bump, and the caller is told.
    const untouched = await service.applyChanges({
      changed: [{ path: target }],
    });
    expect(untouched.unchanged).toEqual([target]);
    expect(untouched.updated).toEqual([]);
    expect(service.version(target)).toBe(1);

    await workspace.write("src/index.ts", "export const answer = 2;\n");
    const edited = await service.applyChanges({ changed: [{ path: target }] });
    expect(edited.updated).toEqual([target]);
    expect(service.version(target)).toBe(2);
  });

  it("reports a file whose announced hash does not match the disk", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);
    const target = workspace.file("src/index.ts");
    const generationBefore = service.generation;

    await workspace.write("src/index.ts", "export const answer = 2;\n");
    const result = await service.applyChanges({
      changed: [
        { path: target, contentHash: sha256("export const answer = 99;\n") },
      ],
    });

    // The supervisor read a different revision than the worker sees. Applying
    // it anyway would produce facts nobody can trace to a source revision.
    expect(result.desynchronised).toEqual([target]);
    expect(result.updated).toEqual([]);
    expect(service.version(target)).toBe(1);
    expect(service.generation).toBe(generationBefore);
  });

  it("accepts a change whose announced hash matches", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);
    const target = workspace.file("src/index.ts");

    const next = "export const answer = 3;\n";
    await workspace.write("src/index.ts", next);
    const result = await service.applyChanges({
      changed: [{ path: target, contentHash: sha256(next) }],
    });
    expect(result.updated).toEqual([target]);
    expect(result.desynchronised).toEqual([]);
    expect(service.contentHash(target)).toBe(sha256(next));
  });

  it("tracks a created file and forgets a deleted one", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    const added = workspace.file("src/added.ts");
    await workspace.write("src/added.ts", "export const added = 1;\n");
    const creation = await service.applyChanges({ created: [{ path: added }] });
    expect(creation.updated).toEqual([added]);
    expect(service.version(added)).toBe(1);

    const removal = await service.applyChanges({ deleted: [added] });
    expect(removal.deleted).toEqual([added]);
    expect(service.version(added)).toBe(0);
  });

  it("reports a vanished file instead of inventing content for it", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    const ghost = workspace.file("src/ghost.ts");
    const result = await service.applyChanges({ changed: [{ path: ghost }] });
    expect(result.desynchronised).toEqual([ghost]);
    expect(result.updated).toEqual([]);
  });

  it("does not track files outside the workspace or inside node_modules", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);

    // Library declarations live in the compiler install and never change while
    // the session runs, so hashing them would cost time and buy nothing.
    const opened = await service.openProject(workspace.configFileName);
    expect(opened.trackedFiles).toBe(1);
    expect(service.status().filesTracked).toBe(1);
  });
});

describe("module cache", () => {
  it("re-reads everything after an explicit invalidation", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);
    const target = workspace.file("src/index.ts");
    const generationBefore = service.generation;

    // A change nobody announced: only a full invalidation can recover from it.
    await workspace.write("src/index.ts", "export const rebuilt = 1;\n");
    const tracked = await service.invalidateAll();

    expect(tracked).toBe(1);
    expect(service.generation).toBeGreaterThan(generationBefore);
    expect(service.contentHash(target)).toBe(
      sha256("export const rebuilt = 1;\n"),
    );
    expect(service.version(target)).toBe(1);

    const symbol = await service
      .project(workspace.configFileName)
      .checker.getSymbolAtPosition(target, 13);
    expect(symbol?.name).toBe("rebuilt");
  });
});

describe("lifecycle", () => {
  it("refuses work after close and closes idempotently", async () => {
    const workspace = await createWorkspace({
      "src/index.ts": "export const answer = 1;\n",
    });
    const service = startService(workspace.root);
    await service.openProject(workspace.configFileName);

    await service.close();
    await service.close();

    expect(service.status().closed).toBe(true);
    expect(service.status().projectsOpen).toEqual([]);
    expect(() => service.project(workspace.configFileName)).toThrowError(
      expect.objectContaining({ code: "SERVICE_CLOSED" }),
    );
    await expect(
      service.openProject(workspace.configFileName),
    ).rejects.toThrowError(expect.objectContaining({ code: "SERVICE_CLOSED" }));
  });

  it("rejects a relative working directory", () => {
    expect(() => LanguageService.create({ cwd: "relative/path" })).toThrowError(
      LanguageServiceError,
    );
  });
});
