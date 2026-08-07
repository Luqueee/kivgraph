import { describe, expect, it } from "vitest";

import {
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
} from "@/renderer/binary";
import {
  computeStructuralLayout,
  type LayoutGraph,
  type StructuralLayout,
} from "@/renderer/layout";
import { GRAPH_LAYOUT_CONFIG } from "@/renderer/layout/config";

/** The sizes the viewer draws with, so the test measures real distances. */
function drawnRadius(kind: number): number {
  if (kind === NODE_KIND_REPOSITORY) return 15;
  if (kind === NODE_KIND_PACKAGE) return 6.5;
  return 5;
}

interface Blueprint {
  readonly kind: number;
  readonly parent: number;
}

/**
 * Two repositories with the shape of a real one: a chain of packages that
 * depend on each other, a mutually recursive pair, files hanging off their
 * package, and a single dependency crossing between the repositories.
 *
 *   r0 ─ p0 → p1 → p2 → p3 → p4 ⇄ p5        r1 ─ p6 → p7
 *          └ f0 f1        └ f2 f3                 └ f4 f5
 */
function workspace(): LayoutGraph {
  const blueprint: Blueprint[] = [
    { kind: NODE_KIND_REPOSITORY, parent: -1 },
    { kind: NODE_KIND_REPOSITORY, parent: -1 },
  ];
  for (let index = 0; index < 6; index += 1) {
    blueprint.push({ kind: NODE_KIND_PACKAGE, parent: 0 });
  }
  blueprint.push({ kind: NODE_KIND_PACKAGE, parent: 1 });
  blueprint.push({ kind: NODE_KIND_PACKAGE, parent: 1 });
  for (const owner of [2, 2, 5, 5, 8, 8]) {
    blueprint.push({ kind: NODE_KIND_FILE, parent: owner });
  }

  const links: [number, number][] = [
    [2, 3],
    [3, 4],
    [4, 5],
    [5, 6],
    [6, 7],
    [7, 6],
    [8, 9],
    [2, 8],
  ];

  const kind = new Uint8Array(blueprint.length);
  const parent = new Int32Array(blueprint.length);
  const identity = new Uint32Array(blueprint.length);
  blueprint.forEach((node, index) => {
    kind[index] = node.kind;
    parent[index] = node.parent;
    identity[index] = (index + 1) * 2654435761;
  });

  return {
    nodeCount: blueprint.length,
    kind,
    parent,
    identity,
    edgeSource: Int32Array.from(links.map(([source]) => source)),
    edgeTarget: Int32Array.from(links.map(([, target]) => target)),
    edgeWeight: Float32Array.from(links.map(() => 1)),
  };
}

function distance(
  layout: StructuralLayout,
  left: number,
  right: number,
): number {
  return Math.hypot(
    layout.x[left] - layout.x[right],
    layout.y[left] - layout.y[right],
    layout.z[left] - layout.z[right],
  );
}

describe("structural 3D layout", () => {
  const layout = computeStructuralLayout(workspace(), drawnRadius);

  // Spatial memory is the point: the same tile must always draw the same
  // picture, or a reader cannot learn where anything lives.
  it("places the same graph in the same positions every time", () => {
    const again = computeStructuralLayout(workspace(), drawnRadius);
    expect([...again.x]).toEqual([...layout.x]);
    expect([...again.y]).toEqual([...layout.y]);
    expect([...again.z]).toEqual([...layout.z]);
  });

  it("gives each repository its own cluster", () => {
    expect(layout.clusterCount).toBe(2);
    expect(layout.cluster[0]).not.toBe(layout.cluster[1]);
    // A package belongs to the cluster of the repository that holds it.
    expect(layout.cluster[2]).toBe(layout.cluster[0]);
    expect(layout.cluster[8]).toBe(layout.cluster[1]);
  });

  // Clusters that interpenetrate are the failure this layout exists to fix:
  // the gap between two repositories has to be wider than either of them.
  it("keeps negative space between clusters", () => {
    const centroids = [0, 1].map((id) => {
      const members = [...layout.cluster].flatMap((cluster, index) =>
        cluster === id ? [index] : [],
      );
      const sum = members.reduce(
        (total, index) => [
          total[0] + layout.x[index],
          total[1] + layout.y[index],
          total[2] + layout.z[index],
        ],
        [0, 0, 0],
      );
      const center = sum.map((value) => value / members.length);
      const reach = Math.max(
        ...members.map((index) =>
          Math.hypot(
            layout.x[index] - center[0],
            layout.y[index] - center[1],
            layout.z[index] - center[2],
          ),
        ),
      );
      return { center, reach };
    });

    const apart = Math.hypot(
      centroids[0].center[0] - centroids[1].center[0],
      centroids[0].center[1] - centroids[1].center[1],
      centroids[0].center[2] - centroids[1].center[2],
    );
    expect(apart).toBeGreaterThan(centroids[0].reach);
    expect(apart).toBeGreaterThan(centroids[1].reach);
  });

  // Depth that only exists in the numbers is worth nothing: a viewer that
  // rotates has to find volume on every axis.
  it("uses all three axes", () => {
    const widest = Math.max(...layout.spread);
    for (const axis of layout.spread) {
      expect(axis / widest).toBeGreaterThanOrEqual(
        GRAPH_LAYOUT_CONFIG.minAxisSpreadRatio - 1e-6,
      );
    }
  });

  // Two nodes sharing a point are one node with two labels on top of it.
  it("never leaves a node inside another", () => {
    for (let left = 0; left < layout.x.length; left += 1) {
      for (let right = left + 1; right < layout.x.length; right += 1) {
        expect(distance(layout, left, right)).toBeGreaterThan(
          Math.max(layout.radius[left], layout.radius[right]),
        );
      }
    }
  });

  // A file belongs beside the package that holds it, not beside a package it
  // has nothing to do with.
  it("keeps a child closer to its container than to any other", () => {
    const packages = [2, 3, 4, 5, 6, 7, 8, 9];
    for (const [file, owner] of [
      [10, 2],
      [11, 2],
      [12, 5],
      [13, 5],
      [14, 8],
      [15, 8],
    ]) {
      const own = distance(layout, file, owner);
      for (const other of packages) {
        if (other === owner) continue;
        expect(own).toBeLessThan(distance(layout, file, other));
      }
    }
  });

  // Cycles have no depth of their own: mutually recursive packages are one
  // level, and the depth is measured over the condensed graph.
  it("collapses a cycle into a single dependency layer", () => {
    expect(layout.layer[6]).toBe(layout.layer[7]);
    expect(layout.layer[2]).toBe(0);
    expect(layout.layer[3]).toBe(1);
    expect(layout.layer[5]).toBe(3);
  });

  // Dependency depth is an orientation, not a grid: it does not have to be
  // exact, but what everything depends on has to end up below its dependents.
  it("lifts a dependent above what it depends on", () => {
    expect(layout.y[6]).toBeGreaterThan(layout.y[2]);
  });

  // Importance is what makes a hub bigger and keeps its caption on screen.
  it("scores a depended-upon package above a leaf file", () => {
    expect(layout.importance[6]).toBeGreaterThan(layout.importance[10]);
  });
});
