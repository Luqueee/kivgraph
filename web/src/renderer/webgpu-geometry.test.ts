import { InstancedBufferAttribute, InstancedBufferGeometry } from "three";
import { describe, expect, it } from "vitest";
import {
  installWebGPUInstanceCountGuard,
  resolvedInstanceCount,
} from "@/renderer/webgpu-geometry";

describe("WebGPU instance count guard", () => {
  it("resolves the count Three's Infinity default stands for", () => {
    expect(
      resolvedInstanceCount({
        attributes: {
          position: { count: 12 },
          offset: { isInstancedBufferAttribute: true, count: 7 },
          color: {
            isInstancedBufferAttribute: true,
            meshPerAttribute: 2,
            count: 6,
          },
        },
      }),
    ).toBe(7);
  });

  it("draws nothing while a geometry has no instanced attributes", () => {
    expect(
      resolvedInstanceCount({ attributes: { position: { count: 4 } } }),
    ).toBe(0);
  });

  it("keeps the default finite and installs only once", () => {
    const first = installWebGPUInstanceCountGuard();
    const second = installWebGPUInstanceCountGuard();

    expect(first).toBe(true);
    expect(second).toBe(false);

    const geometry = new InstancedBufferGeometry();
    expect(geometry.instanceCount).toBe(0);

    geometry.setAttribute(
      "offset",
      new InstancedBufferAttribute(new Float32Array(9), 3),
    );
    expect(geometry.instanceCount).toBe(3);

    geometry.instanceCount = 2;
    expect(geometry.instanceCount).toBe(2);

    geometry.instanceCount = Number.POSITIVE_INFINITY;
    expect(geometry.instanceCount).toBe(3);
  });
});
