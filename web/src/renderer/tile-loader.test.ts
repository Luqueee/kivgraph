import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/client";
import { createDemoPayload } from "@/renderer/fixture";
import { loadTileView } from "@/renderer/tile-loader";

const request = {
  bounds: { minX: 0, minY: 0, maxX: 100, maxY: 100, maxLod: 3, maxNodes: 2000 },
  lod: 1,
  maxNodes: 2000,
};

describe("tile loader", () => {
  // The worker is a wrapper over this function: whatever it returns is what
  // crosses the thread boundary, so it must be plain data with no callbacks.
  it("adapts a fetched tile into a postable view", async () => {
    const view = await loadTileView(request, undefined, async () =>
      createDemoPayload(),
    );

    expect(view.nodes).toHaveLength(8);
    expect(view.edges.length).toBeGreaterThan(0);
    expect(view.truncated).toBe(false);
    expect(view.snapshotId).toBe(1);
    expect(structuredClone(view)).toEqual(view);
  });

  it("reports a truncated tile so the viewer can say the level is a sample", async () => {
    const truncated = createDemoPayload();
    new DataView(truncated).setUint8(7, 1);

    const view = await loadTileView(request, undefined, async () => truncated);

    expect(view.truncated).toBe(true);
  });

  it("propagates the API failure instead of returning an empty graph", async () => {
    const failure = new ApiError("PAYLOAD_TOO_LARGE", 413, "tile is too large");

    await expect(
      loadTileView(request, undefined, async () => {
        throw failure;
      }),
    ).rejects.toBe(failure);
  });
});
