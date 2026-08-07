import { describe, expect, it } from "vitest";

import {
  edgeVisibilityForZoom,
  encodePickColor,
} from "@/renderer/GraphRenderer";

describe("graph renderer helpers", () => {
  it("encodes GPU picking IDs without using the zero color", () => {
    expect(encodePickColor(0)).toEqual([1, 0, 0]);
    expect(encodePickColor(255)).toEqual([0, 1, 0]);
    expect(encodePickColor(0xfffffe)).toEqual([255, 255, 255]);
  });

  it("rejects picking IDs that cannot fit in an RGB color", () => {
    expect(() => encodePickColor(-1)).toThrow(RangeError);
    expect(() => encodePickColor(0xffffff)).toThrow(RangeError);
  });

  it("hides dense edge buffers below the configured zoom level", () => {
    expect(edgeVisibilityForZoom(0.69, 0.7)).toBe(false);
    expect(edgeVisibilityForZoom(0.7, 0.7)).toBe(true);
    expect(edgeVisibilityForZoom(Number.NaN, 0.7)).toBe(false);
  });
});
