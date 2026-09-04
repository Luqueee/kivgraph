import { describe, expect, it } from "vitest";

import {
  calculateTopologyLayout,
  createTopologyLayoutFallback,
  normalizeTopologyLayout,
  topologySemanticLayer,
} from "@/topology-layout";

const nodes = [
  { id: "repository:api", kind: "repository" as const, width: 220, height: 90 },
  { id: "profile:default", kind: "profile" as const, width: 220, height: 90 },
  { id: "worktree:api", kind: "worktree" as const, width: 220, height: 90 },
  {
    id: "shared:config",
    kind: "shared_input" as const,
    width: 220,
    height: 90,
  },
];

const edges = [
  {
    id: "membership:profile-worktree",
    source: "profile:default",
    target: "worktree:api",
  },
  {
    id: "membership:worktree-repository",
    source: "worktree:api",
    target: "repository:api",
  },
  {
    id: "dependency:config",
    source: "repository:api",
    target: "shared:config",
  },
];

describe("topology layout normalization", () => {
  it("is deterministic and orders semantic layers from profile to shared input", () => {
    const first = normalizeTopologyLayout(nodes, edges);
    const second = normalizeTopologyLayout(nodes, edges);

    expect(first).toEqual(second);
    expect(first.nodes.map((node) => [node.id, node.layer])).toEqual([
      ["profile:default", 0],
      ["worktree:api", 1],
      ["repository:api", 2],
      ["shared:config", 3],
    ]);
  });

  it("drops invalid endpoints and self edges without corrupting the layout", () => {
    const normalized = normalizeTopologyLayout(
      [
        ...nodes,
        { id: "unknown:input", kind: "unknown", width: 220, height: 90 },
      ],
      [
        ...edges,
        { id: "missing", source: "repository:api", target: "missing" },
        { id: "self", source: "repository:api", target: "repository:api" },
      ],
    );
    const layout = createTopologyLayoutFallback(
      normalized.nodes,
      normalized.edges,
    );

    expect(normalized.edges).toHaveLength(edges.length);
    expect(layout.nodes).toHaveLength(nodes.length + 1);
    expect(
      layout.nodes.every(
        (node) => Number.isFinite(node.x) && Number.isFinite(node.y),
      ),
    ).toBe(true);
    expect(topologySemanticLayer("unknown")).toBe(3);
  });

  it("returns valid empty and single-node layouts", () => {
    const empty = createTopologyLayoutFallback([], []);
    const single = createTopologyLayoutFallback([nodes[0]], []);

    expect(empty.nodes).toEqual([]);
    expect(empty.width).toBeGreaterThan(0);
    expect(single.nodes).toHaveLength(1);
    expect(single.nodes[0].x).toBeGreaterThanOrEqual(0);
  });

  it("uses the layered engine for stable left-to-right orthogonal routes", async () => {
    const parallelEdges = [
      ...edges,
      {
        id: "dependency:config-secondary",
        source: "repository:api",
        target: "shared:config",
      },
    ];
    const layout = await calculateTopologyLayout(nodes, parallelEdges);
    const positions = new Map(layout.nodes.map((node) => [node.id, node]));

    expect(layout.nodes).toHaveLength(nodes.length);
    expect(
      layout.nodes.every(
        (node) =>
          Number.isFinite(node.x) &&
          Number.isFinite(node.y) &&
          Number.isFinite(node.width) &&
          Number.isFinite(node.height),
      ),
    ).toBe(true);
    expect(positions.get("profile:default")?.x).toBeLessThan(
      positions.get("worktree:api")?.x ?? 0,
    );
    expect(positions.get("worktree:api")?.x).toBeLessThan(
      positions.get("repository:api")?.x ?? 0,
    );
    expect(layout.routes.map((route) => route.id).sort()).toEqual(
      parallelEdges.map((edge) => edge.id).sort(),
    );
    expect(
      layout.routes.every((route) =>
        route.points.every(
          (point, index, points) =>
            index === 0 ||
            point.x === points[index - 1].x ||
            point.y === points[index - 1].y,
        ),
      ),
    ).toBe(true);
    expect(
      positions
        .get("repository:api")
        ?.ports.some((port) => port.side === "right"),
    ).toBe(true);
  });

  it("keeps inbound ports on the left when a node ID contains out", async () => {
    const layout = await calculateTopologyLayout(
      [
        { id: "repository:source", kind: "repository", width: 220, height: 90 },
        { id: "repository:out", kind: "repository", width: 220, height: 90 },
      ],
      [
        {
          id: "dependency:source-out",
          source: "repository:source",
          target: "repository:out",
        },
      ],
    );

    expect(
      layout.nodes.find((node) => node.id === "repository:out")?.ports,
    ).toEqual(
      expect.arrayContaining([expect.objectContaining({ side: "left" })]),
    );
  });
});
