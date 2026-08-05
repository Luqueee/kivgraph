import { readFile } from "node:fs/promises";
import path from "node:path";
import { Readable } from "node:stream";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import {
  FRAME_HEADER_BYTES,
  FrameDecoder,
  FramingError,
  MAX_FRAME_BYTES,
  PROTOCOL_VERSION,
  canonicalJSON,
  decodeFrame,
  encodeFrame,
  newEnvelope,
  readFrames,
} from "./framing.js";

const fixtureDirectory = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../testdata/protocol/ts-worker-v1",
);

interface FixtureCase {
  name: string;
  file: string;
  expect: "ok" | "error";
  error_code?: string;
  fatal: boolean;
  envelope?: { v: number; id: number; type: string };
  canonical_encoding: boolean;
}

interface FixtureManifest {
  protocol: string;
  version: number;
  byte_order: string;
  header_bytes: number;
  max_frame_bytes: number;
  cases: FixtureCase[];
}

async function loadManifest(): Promise<FixtureManifest> {
  const raw = await readFile(
    path.join(fixtureDirectory, "manifest.json"),
    "utf8",
  );
  return JSON.parse(raw) as FixtureManifest;
}

function rawFrame(body: string): Uint8Array {
  const encoded = new TextEncoder().encode(body);
  const frame = new Uint8Array(FRAME_HEADER_BYTES + encoded.length);
  new DataView(frame.buffer).setUint32(0, encoded.length, false);
  frame.set(encoded, FRAME_HEADER_BYTES);
  return frame;
}

function framingError(action: () => unknown): FramingError {
  try {
    action();
  } catch (error: unknown) {
    if (error instanceof FramingError) {
      return error;
    }
    throw error;
  }
  throw new Error("expected a FramingError");
}

describe("shared wire fixtures", () => {
  it("agrees with the Go implementation on the transport constants", async () => {
    const manifest = await loadManifest();
    expect(manifest.version).toBe(PROTOCOL_VERSION);
    expect(manifest.byte_order).toBe("big-endian");
    expect(manifest.header_bytes).toBe(FRAME_HEADER_BYTES);
    expect(manifest.max_frame_bytes).toBe(MAX_FRAME_BYTES);
    expect(manifest.cases.length).toBeGreaterThan(0);
  });

  it("decodes every fixture exactly as the manifest declares", async () => {
    const manifest = await loadManifest();

    for (const entry of manifest.cases) {
      const frame = new Uint8Array(
        await readFile(path.join(fixtureDirectory, entry.file)),
      );

      if (entry.expect === "ok") {
        const envelope = decodeFrame(frame);
        expect(envelope.v, entry.file).toBe(entry.envelope?.v);
        expect(envelope.id, entry.file).toBe(entry.envelope?.id);
        expect(envelope.type, entry.file).toBe(entry.envelope?.type);

        if (entry.canonical_encoding) {
          // Byte-for-byte compatibility with Go: re-encoding a frame produced
          // by the Go writer must reproduce the identical bytes.
          expect(
            Buffer.from(encodeFrame(envelope)).equals(Buffer.from(frame)),
            entry.file,
          ).toBe(true);
        }
        continue;
      }

      const error = framingError(() => decodeFrame(frame));
      expect(error.kind, entry.file).toBe(entry.error_code);
      expect(error.fatal, entry.file).toBe(entry.fatal);
    }
  });
});

describe("frame encoding", () => {
  it("writes a big-endian length prefix that counts only the body", () => {
    const frame = encodeFrame(
      newEnvelope(7, "HELLO", { supervisor_version: "0.1.0-dev" }),
    );
    const declared = new DataView(
      frame.buffer,
      frame.byteOffset,
      frame.byteLength,
    ).getUint32(0, false);

    expect(declared).toBe(frame.length - FRAME_HEADER_BYTES);
    expect(Array.from(frame.subarray(0, FRAME_HEADER_BYTES))).toEqual([
      0,
      0,
      declared >> 8,
      declared & 0xff,
    ]);
    expect(decodeFrame(frame)).toEqual({
      v: PROTOCOL_VERSION,
      id: 7,
      type: "HELLO",
      payload: { supervisor_version: "0.1.0-dev" },
    });
  });

  it("orders payload keys the way Go orders map keys", () => {
    expect(canonicalJSON({ b: 1, a: 2 })).toBe('{"a":2,"b":1}');
    expect(canonicalJSON({ z: [3, { y: 1, x: 2 }] })).toBe(
      '{"z":[3,{"x":2,"y":1}]}',
    );

    const unordered = encodeFrame(
      newEnvelope(1, "FACTS", { file: "a.ts", final: true }),
    );
    const reordered = encodeFrame(
      newEnvelope(1, "FACTS", { final: true, file: "a.ts" }),
    );
    expect(Buffer.from(unordered).equals(Buffer.from(reordered))).toBe(true);
  });

  it("rejects envelopes the protocol forbids", () => {
    const invalid = [
      { v: 2, id: 1, type: "HELLO", payload: {} },
      { v: PROTOCOL_VERSION, id: 1, type: "", payload: {} },
      { v: PROTOCOL_VERSION, id: -1, type: "HELLO", payload: {} },
      { v: PROTOCOL_VERSION, id: 1, type: "HELLO", payload: null },
    ];

    for (const envelope of invalid) {
      const error = framingError(() => encodeFrame(envelope));
      expect(error.kind).toBe("INVALID_PAYLOAD");
    }
  });
});

describe("frame decoding", () => {
  it("waits for a partial frame instead of guessing", () => {
    const frame = encodeFrame(
      newEnvelope(1, "INDEX_PROJECT", { project_id: "p" }),
    );
    const decoder = new FrameDecoder();

    decoder.push(frame.subarray(0, FRAME_HEADER_BYTES + 2));
    expect(decoder.next()).toBeUndefined();
    expect(decoder.buffered).toBe(FRAME_HEADER_BYTES + 2);

    decoder.push(frame.subarray(FRAME_HEADER_BYTES + 2));
    expect(decoder.next()?.type).toBe("INDEX_PROJECT");
    expect(decoder.next()).toBeUndefined();
  });

  it("separates end of input from a truncated frame", () => {
    const clean = new FrameDecoder();
    clean.end();
    expect(clean.next()).toBeUndefined();

    const frame = encodeFrame(newEnvelope(2, "GET_STATUS", {}));
    const truncated = new FrameDecoder();
    truncated.push(frame.subarray(0, frame.length - 2));
    truncated.end();
    expect(framingError(() => truncated.next()).kind).toBe("FRAME_TRUNCATED");

    const headerOnly = new FrameDecoder();
    headerOnly.push(frame.subarray(0, 2));
    headerOnly.end();
    expect(framingError(() => headerOnly.next()).kind).toBe("FRAME_TRUNCATED");
  });

  it("rejects an oversized prefix before allocating and an empty body", () => {
    const oversized = new Uint8Array(FRAME_HEADER_BYTES);
    new DataView(oversized.buffer).setUint32(0, MAX_FRAME_BYTES + 1, false);
    const decoder = new FrameDecoder();
    decoder.push(oversized);
    expect(framingError(() => decoder.next()).kind).toBe("FRAME_TOO_LARGE");
    expect(decoder.buffered).toBe(FRAME_HEADER_BYTES);

    const empty = new FrameDecoder();
    empty.push(new Uint8Array(FRAME_HEADER_BYTES));
    expect(framingError(() => empty.next()).kind).toBe("FRAME_EMPTY");
  });

  it("keeps the stream aligned after an invalid payload", () => {
    const decoder = new FrameDecoder();
    decoder.push(rawFrame('{"v":1,"id":1,"type":"HELLO"'));
    decoder.push(rawFrame('{"v":1,"id":2,"type":"","payload":{}}'));
    decoder.push(encodeFrame(newEnvelope(3, "GET_STATUS", {})));

    for (let attempt = 0; attempt < 2; attempt++) {
      const error = framingError(() => decoder.next());
      expect(error.kind).toBe("INVALID_PAYLOAD");
      expect(error.fatal).toBe(false);
    }
    expect(decoder.next()?.id).toBe(3);
  });

  it("treats a foreign protocol version as fatal", () => {
    const error = framingError(() =>
      decodeFrame(rawFrame('{"v":2,"id":3,"type":"HELLO","payload":{}}')),
    );
    expect(error.kind).toBe("VERSION_MISMATCH");
    expect(error.fatal).toBe(true);
  });
});

describe("frame streaming", () => {
  it("yields frames split arbitrarily across transport chunks", async () => {
    const frames = [
      encodeFrame(newEnvelope(1, "HELLO", { supervisor_version: "0.1.0-dev" })),
      encodeFrame(newEnvelope(2, "GET_STATUS", {})),
      encodeFrame(newEnvelope(0, "FACTS", { final: true })),
    ];
    const joined = Buffer.concat(frames.map((frame) => Buffer.from(frame)));
    const chunks: Uint8Array[] = [];
    for (let offset = 0; offset < joined.length; offset += 7) {
      chunks.push(new Uint8Array(joined.subarray(offset, offset + 7)));
    }

    const seen: number[] = [];
    for await (const envelope of readFrames(Readable.from(chunks))) {
      seen.push(envelope.id);
    }
    expect(seen).toEqual([1, 2, 0]);
  });
});
