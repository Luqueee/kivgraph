import { describe, expect, it } from "vitest";

import { frameGraph, type FrameRequest } from "@/renderer/camera";

const BASE: FrameRequest = {
  points: [],
  center: [0, 0, 0],
  azimuth: 0.62,
  polar: Math.PI / 2 - 0.42,
  fov: 50,
  aspect: 16 / 9,
  margin: 1.2,
  quantile: 0.97,
  padding: 0,
};

/** A flat disc in the XZ plane: wide from above, thin from the side. */
function disc(radius: number, count: number) {
  return Array.from({ length: count }, (_, index) => {
    const angle = (index / count) * Math.PI * 2;
    return {
      x: Math.cos(angle) * radius,
      y: 0,
      z: Math.sin(angle) * radius,
    };
  });
}

describe("camera framing", () => {
  it("puts the camera off both axes so depth is visible", () => {
    const { position } = frameGraph({ ...BASE, points: disc(100, 24) });

    expect(Math.abs(position[0])).toBeGreaterThan(1);
    expect(Math.abs(position[1])).toBeGreaterThan(1);
    expect(Math.abs(position[2])).toBeGreaterThan(1);
  });

  // The distance has to follow the content: the same code has to frame thirty
  // repositories and two thousand symbols.
  it("scales the distance with the graph", () => {
    const near = frameGraph({ ...BASE, points: disc(100, 24) });
    const far = frameGraph({ ...BASE, points: disc(1_000, 24) });

    expect(far.distance / near.distance).toBeCloseTo(10, 0);
  });

  // Framing the bounding sphere of a flat cloud wastes most of the screen.
  // Measuring along the camera's own axes is what keeps the graph large.
  it("frames the projected extent, not the bounding sphere", () => {
    const flat = frameGraph({
      ...BASE,
      polar: 0.15, // Looking down at a disc lying in the XZ plane.
      points: disc(100, 24).map((point) => ({ ...point, y: point.z * 0.02 })),
    });
    const sphere = frameGraph({
      ...BASE,
      polar: 0.15,
      points: disc(100, 24).map((point, index) => ({
        ...point,
        y: index % 2 === 0 ? 100 : -100,
      })),
    });

    expect(flat.distance).toBeLessThan(sphere.distance);
  });

  // A wide viewport fits a wide graph without moving back; a narrow one has to.
  it("accounts for the aspect ratio", () => {
    const wide = frameGraph({ ...BASE, aspect: 2, points: disc(100, 24) });
    const narrow = frameGraph({ ...BASE, aspect: 0.5, points: disc(100, 24) });

    expect(narrow.distance).toBeGreaterThan(wide.distance);
  });

  it("ignores the outliers the quantile leaves out", () => {
    const points = [...disc(100, 99), { x: 100_000, y: 0, z: 0 }];
    const framed = frameGraph({ ...BASE, points, quantile: 0.95 });
    const strict = frameGraph({ ...BASE, points, quantile: 1 });

    expect(framed.distance).toBeLessThan(strict.distance / 10);
  });

  it("survives an empty graph", () => {
    expect(frameGraph({ ...BASE, points: [] }).distance).toBeGreaterThan(0);
  });
});
