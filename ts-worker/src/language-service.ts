/**
 * Persistent TypeScript Language Service for the Kivgraph worker.
 *
 * Per ADR 0010 the engine is always the native TypeScript compiler, driven
 * through its asynchronous API. This module owns everything that must survive
 * between requests: the live snapshot, the file versions, the client side
 * source file cache, and the Program, Checker and Project handles derived from
 * the current snapshot.
 *
 * The single hard invariant is that handles are snapshot scoped. A Project,
 * Program or Checker obtained from a snapshot is invalid once that snapshot is
 * disposed, so this class never hands out a handle without binding it to the
 * generation it came from.
 */

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import type {
  Checker,
  Program,
  Project,
  Snapshot,
} from "typescript/unstable/async";
import { API } from "typescript/unstable/async";

/** Classified failures of the Language Service. */
export type LanguageServiceErrorCode =
  | "SERVICE_CLOSED"
  | "UNKNOWN_PROJECT"
  | "STALE_GENERATION"
  | "INVALID_ARGUMENT"
  | "ENGINE_UNAVAILABLE";

/** LanguageServiceError carries a stable code; callers never match on text. */
export class LanguageServiceError extends Error {
  readonly code: LanguageServiceErrorCode;

  constructor(
    code: LanguageServiceErrorCode,
    message: string,
    options?: { cause?: unknown },
  ) {
    super(message, options);
    this.name = "LanguageServiceError";
    this.code = code;
  }
}

export interface LanguageServiceOptions {
  /** Working directory the native server resolves relative paths against. */
  cwd: string;
  /** Override the bundled native executable. Used by tests and packaging. */
  tsserverPath?: string;
}

/** A file the supervisor announced as changed, with the hash it expects. */
export interface FileChangeAnnouncement {
  path: string;
  /**
   * Hash the supervisor read. When present it is compared against the bytes on
   * disk, which is how the protocol detects a desynchronised read.
   */
  contentHash?: string;
}

export interface ApplyChangesRequest {
  changed?: readonly FileChangeAnnouncement[];
  created?: readonly FileChangeAnnouncement[];
  deleted?: readonly string[];
}

export interface ApplyChangesResult {
  generation: number;
  /** Files whose content really changed, so their version was bumped. */
  updated: string[];
  /** Files announced whose content is byte identical to the tracked version. */
  unchanged: string[];
  deleted: string[];
  /**
   * Files whose on-disk hash differs from the announced one. The supervisor
   * read a different revision than the worker sees; the caller must not treat
   * facts derived from them as trustworthy.
   */
  desynchronised: string[];
}

export interface OpenProjectResult {
  configFileName: string;
  generation: number;
  rootFiles: readonly string[];
  /** Tracked files of the project, that is, those inside the workspace. */
  trackedFiles: number;
}

/** A snapshot bound view of one project. */
export interface ProjectView {
  generation: number;
  configFileName: string;
  /**
   * The directory that bounds the files this project owns. For a configured
   * project it is the directory of its tsconfig. An inferred project has no
   * tsconfig at all -- TypeScript names it `/dev/null/inferred` -- so the
   * bound is the repository root whoever opened the files declared, and
   * deriving it from `configFileName` would bound the project by `/dev/null`
   * and select no file at all.
   */
  localRoot: string;
  project: Project;
  program: Program;
  checker: Checker;
}

/** One project TypeScript resolved as the owner of some opened files. */
export interface FileOwner {
  view: ProjectView;
  /** The opened files this project owns, sorted. */
  files: string[];
}

export interface OpenFilesResult {
  generation: number;
  /**
   * One entry per project TypeScript resolved as an owner. A file inside a
   * configured project resolves to that project; a file no tsconfig contains
   * resolves to the inferred project, which the engine builds from its own
   * default compiler options.
   */
  owners: FileOwner[];
  /**
   * Files the engine resolved no project for at all. They are reported
   * instead of dropped: the caller asked for them by name.
   */
  unowned: string[];
  /** Tracked files of every owning project, that is, those inside the workspace. */
  trackedFiles: number;
}

export interface LanguageServiceStatus {
  generation: number;
  projectsOpen: string[];
  filesTracked: number;
  snapshotLive: boolean;
  /**
   * Snapshots released so far. A snapshot that is never disposed keeps its
   * projects alive in the native server, which is the memory accumulation
   * ADR 0005 warns about, so the count is observable.
   */
  snapshotsDisposed: number;
  closed: boolean;
}

interface TrackedFile {
  version: number;
  contentHash: string;
}

const HASH_ALGORITHM = "sha256";

/** Directories whose contents never change during an indexing session. */
const UNTRACKED_DIRECTORY = `${path.sep}node_modules${path.sep}`;

export class LanguageService {
  readonly #api: API;
  readonly #cwd: string;
  readonly #openProjects = new Set<string>();
  readonly #versions = new Map<string, TrackedFile>();
  #snapshot: Snapshot | undefined;
  #generation = 0;
  #snapshotsDisposed = 0;
  #closed = false;

  private constructor(api: API, cwd: string) {
    this.#api = api;
    this.#cwd = cwd;
  }

  /** create starts the native server lazily; no project is loaded yet. */
  static create(options: LanguageServiceOptions): LanguageService {
    const cwd = options.cwd?.trim();
    if (!cwd || !path.isAbsolute(cwd)) {
      throw new LanguageServiceError(
        "INVALID_ARGUMENT",
        `cwd must be an absolute path, got ${JSON.stringify(options.cwd)}`,
      );
    }
    const api = new API(
      options.tsserverPath === undefined
        ? { cwd }
        : { cwd, tsserverPath: options.tsserverPath },
    );
    return new LanguageService(api, path.resolve(cwd));
  }

  /** generation increases on every snapshot roll. It invalidates handles. */
  get generation(): number {
    return this.#generation;
  }

  status(): LanguageServiceStatus {
    return {
      generation: this.#generation,
      projectsOpen: [...this.#openProjects].sort(),
      filesTracked: this.#versions.size,
      snapshotLive:
        this.#snapshot !== undefined && !this.#snapshot.isDisposed(),
      snapshotsDisposed: this.#snapshotsDisposed,
      closed: this.#closed,
    };
  }

  /**
   * openProject loads a tsconfig and keeps it loaded. Opens are reference
   * counted by the server and persist across snapshots, so a project is loaded
   * once and reused until it is closed.
   */
  async openProject(configFileName: string): Promise<OpenProjectResult> {
    this.#assertOpen();
    const resolved = this.#resolveAgainstCwd(configFileName, "configFileName");

    await this.#roll({ openProjects: [resolved] });
    this.#openProjects.add(resolved);

    const view = this.project(resolved);
    const sourceFiles = await view.program.getSourceFileNames();
    const tracked = await this.#trackFiles(sourceFiles);

    return {
      configFileName: resolved,
      generation: this.#generation,
      rootFiles: view.project.rootFiles,
      trackedFiles: tracked,
    };
  }

  /** closeProject releases a project and forgets its bookkeeping. */
  async closeProject(configFileName: string): Promise<void> {
    this.#assertOpen();
    const resolved = path.resolve(configFileName);
    if (!this.#openProjects.has(resolved)) {
      throw new LanguageServiceError(
        "UNKNOWN_PROJECT",
        `project ${resolved} is not open`,
      );
    }
    await this.#roll({ closeProjects: [resolved] });
    this.#openProjects.delete(resolved);
  }

  /**
   * openFiles keeps files open for this client, mirroring LSP's
   * `textDocument/didOpen`, and returns the projects TypeScript resolved as
   * their owners. For each file the engine searches its ancestor directories
   * for a tsconfig that contains it; when none does, the file is loaded into
   * the inferred project, whose compiler options are the engine's defaults
   * and not any project's declaration.
   *
   * `localRoot` bounds what the returned views call their own files: an
   * inferred project has no directory of its own, so the caller has to say
   * which tree it opened these files from.
   *
   * Opens are reference counted and persist across snapshots, exactly like a
   * project open, so this rolls the snapshot and invalidates every view
   * captured before it.
   */
  async openFiles(
    files: readonly string[],
    localRoot: string,
  ): Promise<OpenFilesResult> {
    this.#assertOpen();
    const root = this.#resolveAgainstCwd(localRoot, "localRoot");
    const resolved = files.map((file) => this.#resolveAgainstCwd(file, "file"));
    if (resolved.length === 0) {
      throw new LanguageServiceError(
        "INVALID_ARGUMENT",
        "openFiles requires at least one file",
      );
    }

    await this.#roll({ openFiles: resolved });
    const snapshot = this.#snapshot;
    if (snapshot === undefined || snapshot.isDisposed()) {
      throw new LanguageServiceError(
        "STALE_GENERATION",
        "the snapshot was disposed while opening files",
      );
    }

    const owners = new Map<string, FileOwner>();
    const unowned: string[] = [];
    for (const file of resolved) {
      const project = await snapshot.getDefaultProjectForFile(file);
      if (project === undefined) {
        unowned.push(file);
        continue;
      }
      const owner = owners.get(project.id);
      if (owner === undefined) {
        owners.set(project.id, {
          view: {
            generation: this.#generation,
            configFileName: project.configFileName,
            localRoot: root,
            project,
            program: project.program,
            checker: project.checker,
          },
          files: [file],
        });
        continue;
      }
      owner.files.push(file);
    }

    let tracked = 0;
    for (const owner of owners.values()) {
      owner.files.sort();
      tracked += await this.#trackFiles(
        await owner.view.program.getSourceFileNames(),
      );
    }

    return {
      generation: this.#generation,
      owners: [...owners.values()],
      unowned: unowned.sort(),
      trackedFiles: tracked,
    };
  }

  /**
   * project returns the handles of one project, bound to the current
   * generation. The returned view is only valid until the next snapshot roll.
   */
  project(configFileName: string): ProjectView {
    this.#assertOpen();
    const snapshot = this.#snapshot;
    if (snapshot === undefined || snapshot.isDisposed()) {
      throw new LanguageServiceError(
        "STALE_GENERATION",
        "no live snapshot; open a project first",
      );
    }
    const resolved = path.resolve(configFileName);
    const project = snapshot.getProject(resolved);
    if (project === undefined) {
      throw new LanguageServiceError(
        "UNKNOWN_PROJECT",
        `project ${resolved} is not part of snapshot ${this.#generation}`,
      );
    }
    return {
      generation: this.#generation,
      configFileName: resolved,
      localRoot: path.dirname(resolved),
      project,
      program: project.program,
      checker: project.checker,
    };
  }

  /** assertFresh rejects a view captured before the last snapshot roll. */
  assertFresh(view: ProjectView): void {
    if (view.generation !== this.#generation) {
      throw new LanguageServiceError(
        "STALE_GENERATION",
        `view belongs to generation ${view.generation}, current is ${this.#generation}`,
      );
    }
  }

  /** version returns the tracked version of a file, or zero when untracked. */
  version(filePath: string): number {
    return this.#versions.get(path.resolve(filePath))?.version ?? 0;
  }

  /** contentHash returns the hash the service last read for a file. */
  contentHash(filePath: string): string | undefined {
    return this.#versions.get(path.resolve(filePath))?.contentHash;
  }

  /**
   * applyChanges rereads the announced files, bumps the version of those that
   * really changed and rolls a new snapshot.
   *
   * A file announced with a hash that does not match the bytes on disk is
   * reported as desynchronised instead of being applied silently: the protocol
   * uses that hash precisely to detect a stale read.
   */
  async applyChanges(
    request: ApplyChangesRequest,
  ): Promise<ApplyChangesResult> {
    this.#assertOpen();

    const changed: string[] = [];
    const created: string[] = [];
    const unchanged: string[] = [];
    const desynchronised: string[] = [];

    for (const [announcements, sink] of [
      [request.changed ?? [], changed],
      [request.created ?? [], created],
    ] as const) {
      for (const announcement of announcements) {
        const resolved = path.resolve(announcement.path);
        const contents = await this.#read(resolved);
        if (contents === undefined) {
          // The file vanished between the announcement and the read, so the
          // supervisor and the worker disagree on what exists.
          desynchronised.push(resolved);
          continue;
        }
        const hash = hashContents(contents);
        if (
          announcement.contentHash !== undefined &&
          announcement.contentHash !== hash
        ) {
          desynchronised.push(resolved);
          continue;
        }
        const tracked = this.#versions.get(resolved);
        if (tracked !== undefined && tracked.contentHash === hash) {
          unchanged.push(resolved);
          continue;
        }
        this.#versions.set(resolved, {
          version: (tracked?.version ?? 0) + 1,
          contentHash: hash,
        });
        sink.push(resolved);
      }
    }

    const deleted = (request.deleted ?? []).map((entry) => path.resolve(entry));
    for (const resolved of deleted) {
      this.#versions.delete(resolved);
    }

    if (changed.length > 0 || created.length > 0 || deleted.length > 0) {
      await this.#roll({
        fileChanges: {
          ...(changed.length > 0 ? { changed } : {}),
          ...(created.length > 0 ? { created } : {}),
          ...(deleted.length > 0 ? { deleted } : {}),
        },
      });
    }

    return {
      generation: this.#generation,
      updated: [...changed, ...created].sort(),
      unchanged: unchanged.sort(),
      deleted: deleted.sort(),
      desynchronised: desynchronised.sort(),
    };
  }

  /**
   * invalidateAll drops every cached assumption: the client side source file
   * cache and the server's view of the filesystem. It is the recovery path for
   * when incremental state can no longer be trusted.
   */
  async invalidateAll(): Promise<number> {
    this.#assertOpen();
    this.#api.clearSourceFileCache();
    this.#versions.clear();
    await this.#roll({ fileChanges: { invalidateAll: true } });

    let tracked = 0;
    for (const configFileName of this.#openProjects) {
      const view = this.project(configFileName);
      tracked += await this.#trackFiles(
        await view.program.getSourceFileNames(),
      );
    }
    return tracked;
  }

  /** close disposes the snapshot and stops the native server. Idempotent. */
  async close(): Promise<void> {
    if (this.#closed) {
      return;
    }
    this.#closed = true;
    const snapshot = this.#snapshot;
    this.#snapshot = undefined;
    this.#openProjects.clear();
    this.#versions.clear();
    try {
      if (snapshot !== undefined && !snapshot.isDisposed()) {
        await snapshot.dispose();
        this.#snapshotsDisposed += 1;
      }
    } finally {
      await this.#api.close();
    }
  }

  /**
   * roll replaces the live snapshot. The previous one is disposed only after
   * the new one exists, so the server never sees a window with no snapshot.
   */
  async #roll(params: Parameters<API["updateSnapshot"]>[0]): Promise<void> {
    const previous = this.#snapshot;
    let next: Snapshot;
    try {
      next = await this.#api.updateSnapshot(params);
    } catch (error: unknown) {
      throw new LanguageServiceError(
        "ENGINE_UNAVAILABLE",
        `updateSnapshot failed: ${describe(error)}`,
        { cause: error },
      );
    }
    this.#snapshot = next;
    this.#generation += 1;
    if (previous !== undefined && !previous.isDisposed()) {
      await previous.dispose();
      this.#snapshotsDisposed += 1;
    }
  }

  /**
   * trackFiles seeds versions for the files that can change while indexing:
   * those inside the workspace and outside node_modules. Library declarations
   * and installed packages are immutable for the life of the session, so
   * hashing them would cost time and buy nothing.
   */
  async #trackFiles(sourceFiles: readonly string[]): Promise<number> {
    let tracked = 0;
    for (const sourceFile of sourceFiles) {
      const resolved = path.resolve(sourceFile);
      if (!this.#isTrackable(resolved)) {
        continue;
      }
      tracked += 1;
      if (this.#versions.has(resolved)) {
        continue;
      }
      const contents = await this.#read(resolved);
      if (contents === undefined) {
        continue;
      }
      this.#versions.set(resolved, {
        version: 1,
        contentHash: hashContents(contents),
      });
    }
    return tracked;
  }

  #isTrackable(resolved: string): boolean {
    if (resolved !== this.#cwd && !resolved.startsWith(this.#cwd + path.sep)) {
      return false;
    }
    return !resolved.includes(UNTRACKED_DIRECTORY);
  }

  async #read(filePath: string): Promise<Buffer | undefined> {
    try {
      return await readFile(filePath);
    } catch (error: unknown) {
      if (isNotFound(error)) {
        return undefined;
      }
      throw new LanguageServiceError(
        "INVALID_ARGUMENT",
        `read ${filePath}: ${describe(error)}`,
        { cause: error },
      );
    }
  }

  #resolveAgainstCwd(candidate: string, field: string): string {
    const trimmed = candidate?.trim();
    if (!trimmed) {
      throw new LanguageServiceError(
        "INVALID_ARGUMENT",
        `${field} must not be empty`,
      );
    }
    return path.resolve(this.#cwd, trimmed);
  }

  #assertOpen(): void {
    if (this.#closed) {
      throw new LanguageServiceError(
        "SERVICE_CLOSED",
        "language service is closed",
      );
    }
  }
}

function hashContents(contents: Buffer): string {
  return createHash(HASH_ALGORITHM).update(contents).digest("hex");
}

function isNotFound(error: unknown): boolean {
  if (typeof error !== "object" || error === null || !("code" in error)) {
    return false;
  }
  return error.code === "ENOENT";
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
