import { describe, expect, it } from "vitest";

import { selectRendererBackend } from "@/renderer/webgpu";

describe("renderer backend selection", () => {
  it("falls back explicitly when the browser exposes no WebGPU", async () => {
    await expect(selectRendererBackend(null)).resolves.toEqual({
      backend: "webgl",
      reason: "WebGPU is not exposed by this browser",
    });
  });

  it("selects WebGPU only after an adapter is returned", async () => {
    let requestedPreference: string | undefined;
    const selection = await selectRendererBackend({
      requestAdapter: async (options) => {
        requestedPreference = options?.powerPreference;
        return { name: "test-adapter" };
      },
    });

    expect(selection).toEqual({ backend: "webgpu", reason: null });
    expect(requestedPreference).toBe("high-performance");
  });

  it("falls back when the browser has WebGPU but no usable adapter", async () => {
    await expect(
      selectRendererBackend({ requestAdapter: async () => null }),
    ).resolves.toEqual({
      backend: "webgl",
      reason: "WebGPU has no usable adapter on this device",
    });
  });

  it("keeps probe failures visible instead of claiming WebGPU", async () => {
    await expect(
      selectRendererBackend({
        requestAdapter: async () => {
          throw new Error("permission denied");
        },
      }),
    ).resolves.toEqual({
      backend: "webgl",
      reason: "WebGPU probe failed: permission denied",
    });
  });

  it("starts Troika glyph geometry with a finite WebGPU instance count", async () => {
    const moduleName = "troika-three-text";
    const troika = (await import(moduleName)) as {
      Text: new () => {
        geometry: { instanceCount: number };
      };
    };
    const text = new troika.Text();

    expect(Number.isFinite(text.geometry.instanceCount)).toBe(true);
    expect(text.geometry.instanceCount).toBe(0);
  });

  it("keeps a glyph geometry's GPU buffers alive when its attributes grow", async () => {
    const moduleName = "troika-three-text";
    const troika = (await import(moduleName)) as {
      Text: new () => {
        geometry: {
          dispose: () => void;
          updateAttributeData: (
            name: string,
            data: Float32Array,
            itemSize: number,
          ) => void;
        };
      };
    };
    const geometry = new troika.Text().geometry;
    let disposed = 0;
    geometry.dispose = () => {
      disposed += 1;
    };

    geometry.updateAttributeData("aTroikaGlyphBounds", new Float32Array(8), 4);

    expect(disposed).toBe(0);
  });
});
