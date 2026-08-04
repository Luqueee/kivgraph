import { Readable, Writable } from "node:stream";

import { describe, expect, it } from "vitest";

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
