import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, sep } from "node:path";
import { Readable, Writable } from "node:stream";
import { pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

import { isEntryPoint } from "./entry-point.js";
import { handleCommand, run } from "./index.js";

describe("worker commands", () => {
  it("accepts hello with surrounding whitespace", () => {
    expect(handleCommand("  hello  ")).toBe("hello");
  });

  it("writes one response per command line", async () => {
    const responses: string[] = [];
    const stdout = new Writable({
      write(chunk: Buffer, _encoding, callback) {
        responses.push(chunk.toString());
        callback();
      },
    });

    const exitCode = await run(Readable.from(["hello\n"]), stdout);

    expect(exitCode).toBe(0);
    expect(responses).toEqual(["hello\n"]);
  });

  it("reports unknown commands", async () => {
    const responses: string[] = [];
    const stdout = new Writable({
      write(chunk: Buffer, _encoding, callback) {
        responses.push(chunk.toString());
        callback();
      },
    });

    const exitCode = await run(Readable.from(["goodbye\n"]), stdout);

    expect(exitCode).toBe(1);
    expect(responses).toEqual(["error: unknown command: goodbye\n"]);
  });
});

describe("entry point detection", () => {
  it("ignores an unrelated module", () => {
    expect(
      isEntryPoint(
        "/opt/other/cli.js",
        pathToFileURL("/opt/app/index.js").href,
      ),
    ).toBe(false);
  });

  it("ignores a missing argv entry", () => {
    expect(
      isEntryPoint(undefined, pathToFileURL("/opt/app/index.js").href),
    ).toBe(false);
  });

  it("accepts the module invoked through its own path", () => {
    const entry = join("/opt", "app", "index.js");
    expect(isEntryPoint(entry, pathToFileURL(entry).href)).toBe(true);
  });

  // Node resolves the main module through realpath, so a bundle installed
  // under a symlinked directory - /tmp and /var are symlinks on macOS - sees a
  // logical argv[1] and a physical import.meta.url. Comparing only the logical
  // path made the worker exit silently instead of speaking the protocol.
  it("accepts an invocation path with a symlinked component", () => {
    const root = mkdtempSync(join(tmpdir(), "worker-entry-"));
    const real = join(root, "real");
    mkdirSync(real);
    const script = join(real, "index.js");
    writeFileSync(script, "");
    const link = join(root, "link");
    symlinkSync(real, link);
    const throughLink = join(link, "index.js");
    // import.meta.url is what Node reports for the main module: the realpath.
    const moduleURL = pathToFileURL(realpathSync(script)).href;

    expect(throughLink).toContain(`link${sep}`);
    expect(pathToFileURL(throughLink).href).not.toBe(moduleURL);
    expect(isEntryPoint(throughLink, moduleURL)).toBe(true);
  });
});
