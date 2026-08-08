import { describe, expect, it } from "vitest";

import {
  decodeGraphPayload,
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
} from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";
import { createReagraphGraph } from "@/renderer/reagraph";
import {
  LOD_ENTER_PIXELS,
  LOD_LEAVE_PIXELS,
  lodKindForCamera,
  projectGraphAtKind,
  projectedNodePixels,
} from "@/renderer/lod";

describe("camera level of detail", () => {
  it("keeps detail when the projected node is readable and sheds it when not", () => {
    const close = {
      distance: 100,
      fov: 45,
      viewportHeight: 900,
    };
    const far = {
      distance: 30_000,
      fov: 45,
      viewportHeight: 900,
    };

    expect(lodKindForCamera(close, NODE_KIND_SYMBOL)).toBe(NODE_KIND_SYMBOL);
    expect(lodKindForCamera(far, NODE_KIND_SYMBOL)).toBe(NODE_KIND_REPOSITORY);
    expect(projectedNodePixels(4, close)).toBeGreaterThan(LOD_ENTER_PIXELS);
    expect(projectedNodePixels(4, far)).toBeLessThan(LOD_LEAVE_PIXELS);
  });

  it("drops symbol texture at the far camera limit", () => {
    const far = {
      distance: 50_000,
      fov: 10,
      viewportHeight: 900,
    };

    expect(lodKindForCamera(far, NODE_KIND_SYMBOL)).toBe(NODE_KIND_FILE);
  });

  it("uses hysteresis so a boundary does not flicker during damping", () => {
    const distance = (size: number, pixels: number): number =>
      (size * 900) / (pixels * Math.tan((45 * Math.PI) / 360));
    const boundary = {
      distance: distance(7, (LOD_ENTER_PIXELS + LOD_LEAVE_PIXELS) / 2),
      fov: 45,
      viewportHeight: 900,
    };

    expect(lodKindForCamera(boundary, NODE_KIND_REPOSITORY)).toBe(
      NODE_KIND_REPOSITORY,
    );
    expect(lodKindForCamera(boundary, NODE_KIND_PACKAGE)).toBe(
      NODE_KIND_PACKAGE,
    );
  });
});

describe("graph LOD projection", () => {
  it("keeps ancestors, reindexes containment, and folds hidden relations", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));
    const packages = projectGraphAtKind(graph, NODE_KIND_PACKAGE);

    expect(packages.nodes.map((node) => node.data.kind)).toEqual([
      NODE_KIND_REPOSITORY,
      NODE_KIND_PACKAGE,
      NODE_KIND_PACKAGE,
    ]);
    expect(packages.containment.source).toEqual(Uint32Array.from([0, 0]));
    expect(packages.containment.target).toEqual(Uint32Array.from([1, 2]));
    expect(packages.edges).toHaveLength(1);
    expect(packages.edges[0]).toMatchObject({
      source: "node-1",
      target: "node-7",
      data: { lodAggregate: false },
    });
    expect(packages.hiddenNodeCount).toBe(5);
  });

  it("does not invent a route for relations that collapse into one node", () => {
    const graph = createReagraphGraph(decodeGraphPayload(createDemoPayload()));
    const files = projectGraphAtKind(graph, NODE_KIND_FILE);

    expect(files.nodes.map((node) => node.data.kind)).toEqual([
      NODE_KIND_REPOSITORY,
      NODE_KIND_PACKAGE,
      NODE_KIND_FILE,
      NODE_KIND_PACKAGE,
    ]);
    expect(files.edges).toHaveLength(1);
    expect(files.edges[0]?.data.lodAggregate).toBe(false);
  });
});
