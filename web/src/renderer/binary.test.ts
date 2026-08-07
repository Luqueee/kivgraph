import { describe, expect, it } from "vitest";

import {
  decodeGraphPayload,
  GraphBinaryError,
  readCoordinateBounds,
  readEdge,
  readNode,
} from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";

describe("viewer binary payload", () => {
  it("keeps the transferred ArrayBuffer and exposes dense records", () => {
    const buffer = createDemoPayload();
    const payload = decodeGraphPayload(buffer);

    expect(payload.buffer).toBe(buffer);
    expect(payload.header.nodeCount).toBe(8);
    expect(payload.header.edgeCount).toBe(5);
    expect(payload.nodes.byteLength).toBe(8 * 48);
    expect(payload.edges.byteLength).toBe(5 * 16);
    expect(readNode(payload, 3)).toMatchObject({
      id: 0,
      kind: 4,
      minX: 120n,
      maxY: 180n,
    });
    expect(readEdge(payload, 2)).toMatchObject({
      source: 2,
      target: 3,
      confidence: 2,
    });
    expect(readCoordinateBounds(payload)).toEqual({
      minX: 0n,
      minY: 0n,
      maxX: 900n,
      maxY: 400n,
    });
  });

  it.each([
    [
      "truncated payload",
      (buffer: ArrayBuffer) => buffer.slice(0, -1),
      "TRUNCATED_PAYLOAD",
    ],
    [
      "unsupported version",
      (buffer: ArrayBuffer) => {
        const copy = buffer.slice(0);
        new DataView(copy).setUint16(4, 3, true);
        return copy;
      },
      "UNSUPPORTED_VERSION",
    ],
    [
      "misaligned edge section",
      (buffer: ArrayBuffer) => {
        const copy = buffer.slice(0);
        new DataView(copy).setUint32(32, 65, true);
        return copy;
      },
      "INVALID_OFFSETS",
    ],
  ])("rejects %s before exposing views", (_name, makeBuffer, code) => {
    try {
      decodeGraphPayload(makeBuffer(createDemoPayload()));
      throw new Error("expected an invalid payload to be rejected");
    } catch (error) {
      if (!(error instanceof GraphBinaryError)) throw error;
      expect(error.code).toBe(code);
    }
  });

  it("rejects out-of-range record access", () => {
    const payload = decodeGraphPayload(createDemoPayload());

    expect(() => readNode(payload, 8)).toThrow(RangeError);
    expect(() => readEdge(payload, -1)).toThrow(RangeError);
  });
});
