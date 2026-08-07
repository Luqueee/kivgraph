import { describe, expect, it } from "vitest";

import { decodeGraphPayload, type GraphBinaryError } from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";
import { createReagraphGraph } from "@/renderer/reagraph";

describe("Reagraph payload adapter", () => {
  it("maps dense records to unique nodes and linked edges", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.nodes).toHaveLength(7);
    expect(new Set(graph.nodes.map((node) => node.id)).size).toBe(7);
    expect(graph.nodes[3]).toMatchObject({
      id: "node-3",
      label: "symbol 0",
      data: { index: 3, sourceId: 0, kind: 4 },
    });
    expect(graph.edges[2]).toMatchObject({
      id: "edge-2",
      source: "node-2",
      target: "node-3",
      dashed: false,
      data: { confidence: 2 },
    });
  });

  it("keeps the deterministic layout coordinates from the payload", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.nodes[3].data).toMatchObject({
      x: -187.5,
      y: -100,
    });
  });

  it("rejects a payload edge whose endpoints are outside the node section", () => {
    const buffer = createDemoPayload();
    new DataView(buffer).setUint32(64 + 7 * 48, 7, true);

    expect(() => createReagraphGraph(decodeGraphPayload(buffer))).toThrowError(
      expect.objectContaining<Partial<GraphBinaryError>>({
        code: "INVALID_REFERENCES",
      }),
    );
  });

  it("fails visibly instead of materializing an oversized Reagraph graph", () => {
    expect(() =>
      createReagraphGraph(decodeGraphPayload(createDemoPayload()), {
        maxNodes: 6,
      }),
    ).toThrowError(
      expect.objectContaining<Partial<GraphBinaryError>>({
        code: "REAGRAPH_NODE_LIMIT",
      }),
    );
  });
});
