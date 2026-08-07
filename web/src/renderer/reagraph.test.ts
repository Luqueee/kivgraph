import { describe, expect, it } from "vitest";

import {
  decodeGraphPayload,
  type GraphBinaryError,
  VIEWER_EDGE_FLAG_PACKAGE,
} from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";
import { createReagraphGraph } from "@/renderer/reagraph";

describe("Reagraph payload adapter", () => {
  it("maps dense records to unique nodes and linked edges", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.nodes).toHaveLength(8);
    expect(new Set(graph.nodes.map((node) => node.id)).size).toBe(8);
    expect(graph.nodes[3]).toMatchObject({
      id: "node-3",
      label: "core.load",
      data: { index: 3, sourceId: 0, kind: 4 },
    });
    // Edge 2 links symbol 2 to symbol 3, which are payload nodes 5 and 6.
    expect(graph.edges[2]).toMatchObject({
      id: "edge-2",
      source: "node-5",
      target: "node-6",
      dashed: false,
      data: { confidence: 2 },
    });
  });

  // Dense IDs repeat across node kinds: package 0 and symbol 0 are different
  // nodes. A flagged edge must resolve against the package nodes, never
  // against the symbol that happens to share the number.
  it("resolves package relations against package nodes", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));
    const packageEdge = graph.edges.find(
      (edge) => (edge.data.flags & VIEWER_EDGE_FLAG_PACKAGE) !== 0,
    );

    expect(packageEdge).toBeDefined();
    expect(packageEdge).toMatchObject({ source: "node-1", target: "node-7" });
    const source = graph.nodes.find((node) => node.id === "node-1");
    const target = graph.nodes.find((node) => node.id === "node-7");
    expect(source?.data.kind).toBe(2);
    expect(target?.data.kind).toBe(2);
  });

  // A dense ID says nothing to a reader: every node carries the name the server
  // resolved from the snapshot. The caption is shortened to the last two path
  // segments so long module paths do not bury their neighbours, and the full
  // name stays on the node for the hover readout.
  it("labels nodes with the names carried by the payload", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.nodes.map((node) => node.data.label)).toEqual([
      "acme/widgets",
      "@acme/core",
      "src/index.ts",
      "core.load",
      "core.parse",
      "core.render",
      "core.dispose",
      "@acme/ui",
    ]);
    expect(graph.nodes.map((node) => node.label)).toEqual([
      "acme/widgets",
      "@acme/core",
      "src/index.ts",
      "core.load",
      "core.parse",
      "core.render",
      "core.dispose",
      "@acme/ui",
    ]);
  });

  it("shortens a long module path to its last two segments", () => {
    const buffer = createDemoPayload();
    const payload = decodeGraphPayload(buffer);
    const long = "kena.bot/api-db-go/internal/domain/errors";
    const labels = [...payload.labels];
    labels[1] = long;
    const graph = createReagraphGraph({ ...payload, labels });

    expect(graph.nodes[1].label).toBe("domain/errors");
    expect(graph.nodes[1].data.label).toBe(long);
  });

  it("keeps the deterministic layout coordinates from the payload", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.nodes[3].data).toMatchObject({
      x: -248.88888888888889,
      y: -266.6666666666667,
    });
  });

  // The layout nests a repository around its packages around its files, so a
  // flat projection stacks a container on top of its own children. Each kind
  // gets its own depth plane instead, which is what makes rotating readable.
  it("places each node kind on its own depth plane", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    const planes = new Map<number, number>();
    for (const node of graph.nodes) {
      const previous = planes.get(node.data.kind);
      if (previous !== undefined) {
        // Collisions step away in depth, so compare the nearest plane.
        expect(Math.abs(node.data.z - previous)).toBeLessThan(200);
        continue;
      }
      planes.set(node.data.kind, node.data.z);
    }
    const repository = planes.get(1) ?? 0;
    const pkg = planes.get(2) ?? 0;
    const file = planes.get(3) ?? 0;
    const symbol = planes.get(4) ?? 0;
    expect(repository).toBeGreaterThan(pkg);
    expect(pkg).toBeGreaterThan(file);
    expect(file).toBeGreaterThan(symbol);
  });

  it("rejects a payload edge whose endpoints are outside the node section", () => {
    const buffer = createDemoPayload();
    // Point the first edge's source at a symbol dense ID the tile never sent.
    new DataView(buffer).setUint32(64 + 8 * 48, 99, true);

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
