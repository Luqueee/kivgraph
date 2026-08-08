import { describe, expect, it } from "vitest";

import {
  decodeGraphPayload,
  type GraphBinaryError,
  NODE_KIND_REPOSITORY,
  VIEWER_EDGE_FLAG_PACKAGE,
} from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";
import { ALWAYS_LABELLED_SIZE, createReagraphGraph } from "@/renderer/reagraph";

describe("Reagraph payload adapter", () => {
  it("maps dense records to unique nodes and linked edges", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.nodes).toHaveLength(8);
    expect(new Set(graph.nodes.map((node) => node.id)).size).toBe(8);
    expect(graph.nodes[3]).toMatchObject({
      id: "node-3",
      data: { index: 3, sourceId: 0, kind: 4, label: "core.load" },
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

  // A repository and its packages are related, and the payload says so through
  // each node's parent reference. Without those links the picture claims a
  // disconnection the graph does not have. They are index pairs, not edges:
  // nothing picks them and there is one per node.
  it("joins every node to the container the payload names", () => {
    const payload = decodeGraphPayload(createDemoPayload());
    const graph = createReagraphGraph(payload);

    // The fixture nests repository 0 > package 0 > file 0 > four symbols, and
    // package 1 under repository 0: seven children with a container present.
    expect(graph.containment.source).toHaveLength(7);
    expect(graph.containment.target).toHaveLength(7);
    for (let index = 0; index < graph.containment.source.length; index += 1) {
      const source = graph.nodes[graph.containment.source[index]];
      const target = graph.nodes[graph.containment.target[index]];
      expect(source).toBeDefined();
      expect(target).toBeDefined();
      // A container is always coarser than what it holds.
      expect(source.data.kind).toBeLessThan(target.data.kind);
    }
    // And they are no longer part of what the renderer treats as edges.
    expect(graph.edges.every((edge) => edge.data.containment !== true)).toBe(
      true,
    );
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
    // Only what the reader needs to orient themselves is drawn: the
    // repository, plus whatever the tile's centrality picked out as a hub.
    const captioned = graph.nodes.filter((node) => node.label !== undefined);
    expect(captioned.length).toBeGreaterThan(0);
    expect(captioned.length).toBeLessThan(graph.nodes.length);
    expect(captioned[0].id).toBe("node-0");
  });

  it("shortens a long module path to its last two segments", () => {
    const buffer = createDemoPayload();
    const payload = decodeGraphPayload(buffer);
    const long = "kena.bot/api-db-go/internal/domain/errors";
    const labels = [...payload.labels];
    labels[0] = long;
    const graph = createReagraphGraph({ ...payload, labels });

    expect(graph.nodes[0].label).toBe("domain/errors");
    expect(graph.nodes[0].data.label).toBe(long);
  });

  // Positions are derived from the payload alone, so the same tile always
  // renders the same picture.
  it("derives the same positions from the same payload", () => {
    const first = createReagraphGraph(decodeGraphPayload(createDemoPayload()));
    const second = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(first.nodes.map((node) => node.data)).toEqual(
      second.nodes.map((node) => node.data),
    );
  });

  // A container and the children it holds are one thing on screen: the layout
  // hangs a file on a shell around its own package, never around a package it
  // has nothing to do with.
  it("keeps a node beside the container that holds it", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));
    const between = (left: number, right: number): number =>
      Math.hypot(
        graph.nodes[left].data.x - graph.nodes[right].data.x,
        graph.nodes[left].data.y - graph.nodes[right].data.y,
        graph.nodes[left].data.z - graph.nodes[right].data.z,
      );

    // The fixture nests repository 0 > package 0 (index 1) > file 0 (index 2)
    // > four symbols, and hangs package 1 (index 7) off the same repository.
    expect(between(2, 1)).toBeLessThan(between(2, 7));
    for (const symbol of [3, 4, 5, 6]) {
      expect(between(symbol, 2)).toBeLessThan(between(symbol, 7));
    }
  });

  // A drawing that lives on one plane is a sheet inside a 3D scene: rotating
  // it shows nothing. Every axis has to carry part of the structure.
  it("spreads the tile across all three axes", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    const widest = Math.max(...graph.stats.spread);
    expect(widest).toBeGreaterThan(0);
    for (const axis of graph.stats.spread) {
      expect(axis / widest).toBeGreaterThan(0.4);
    }
  });

  // Reagraph keeps a caption on screen at any distance once a node is drawn
  // above `ALWAYS_LABELLED_SIZE`. That threshold is the level-of-detail rule:
  // repositories and hubs are named, the rest wait for the camera.
  it("reserves permanent captions for repositories and hubs", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    const repository = graph.nodes[0];
    expect(repository.data.kind).toBe(NODE_KIND_REPOSITORY);
    expect(repository.size ?? 0).toBeGreaterThan(ALWAYS_LABELLED_SIZE);

    const named = graph.nodes.filter(
      (node) => (node.size ?? 0) > ALWAYS_LABELLED_SIZE,
    );
    expect(named.length).toBeLessThan(graph.nodes.length);
  });

  // The level a reader asked for and the nodes a budgeted tile carries are
  // different things; the viewer describes what it actually drew.
  it("counts the nodes of the tile by kind", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    expect(graph.stats.nodesByKind).toEqual([1, 2, 1, 4]);
    expect(graph.stats.clusterCount).toBe(1);
  });

  // One repository is one cluster: nothing in this tile crosses a boundary,
  // and the viewer must not paint a crossing that does not exist.
  it("marks no dependency as cross-cluster inside a single repository", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));

    const dependencies = graph.edges.filter((edge) => !edge.data.containment);
    expect(dependencies.length).toBeGreaterThan(0);
    for (const edge of dependencies) {
      expect(edge.data.crossCluster).toBe(false);
    }
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
