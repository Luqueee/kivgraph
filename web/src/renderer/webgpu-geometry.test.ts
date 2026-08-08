import { describe, expect, it } from "vitest";
import {
  finiteInstanceCount,
  guardWebGPUInstancedGeometry,
} from "@/renderer/webgpu-geometry";

describe("WebGPU instanced geometry guard", () => {
  it("turns non-finite and negative counts into an empty draw", () => {
    expect(finiteInstanceCount(Number.POSITIVE_INFINITY)).toBe(0);
    expect(finiteInstanceCount(Number.NaN)).toBe(0);
    expect(finiteInstanceCount(-1)).toBe(0);
    expect(finiteInstanceCount(3)).toBe(3);
  });

  it("repairs only flagged geometries and reports the repairs", () => {
    const invalid = {
      isInstancedBufferGeometry: true,
      instanceCount: Number.POSITIVE_INFINITY,
    };
    const valid = {
      isInstancedBufferGeometry: true,
      instanceCount: 4,
    };
    const ordinary = {
      isInstancedBufferGeometry: false,
      instanceCount: Number.POSITIVE_INFINITY,
    };
    const objects = [
      { geometry: invalid },
      { geometry: valid },
      { geometry: ordinary },
      {},
    ];

    const repaired = guardWebGPUInstancedGeometry({
      traverse: (visit) => objects.forEach(visit),
    });

    expect(repaired).toBe(1);
    expect(invalid.instanceCount).toBe(0);
    expect(valid.instanceCount).toBe(4);
    expect(ordinary.instanceCount).toBe(Number.POSITIVE_INFINITY);
  });
});
