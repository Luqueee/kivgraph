export interface CameraFrame {
  /** Distance from the target the camera has to sit at. */
  readonly distance: number;
  /** Where to put the camera, given the target it looks at. */
  readonly position: readonly [number, number, number];
}

export interface FramedPoint {
  readonly x: number;
  readonly y: number;
  readonly z: number;
}

export interface FrameRequest {
  readonly points: readonly FramedPoint[];
  readonly center: readonly [number, number, number];
  /** Angle around the vertical axis, in radians. */
  readonly azimuth: number;
  /** Angle from the vertical axis, in radians; `π/2` is level. */
  readonly polar: number;
  /** Vertical field of view of the camera, in degrees. */
  readonly fov: number;
  /** Viewport width divided by its height. */
  readonly aspect: number;
  /** Slack around the content: `1.1` leaves a tenth of the frame empty. */
  readonly margin: number;
  /** Share of the points that must fit; the rest stay a pan away. */
  readonly quantile: number;
  /** Half-size of the largest thing drawn at a point, so it is not clipped. */
  readonly padding: number;
}

/**
 * Distance and position that put a graph on screen at a given angle.
 *
 * Framing the bounding sphere is the obvious answer and the wrong one: a
 * structural layout is never a ball, and a cloud two thirds as tall as it is
 * wide ends up occupying a third of the frame. The extent is measured along
 * the axes the camera actually uses - its right and its up - so the fit is the
 * one the viewer will see, and it adapts to any node count, field of view or
 * aspect ratio without a constant tuned for one dataset.
 */
export function frameGraph(request: FrameRequest): CameraFrame {
  const { azimuth, polar } = request;
  // Camera-controls convention: azimuth turns around +Y starting at +Z, polar
  // opens from +Y. This is the direction from the target towards the camera.
  const offset: readonly [number, number, number] = [
    Math.sin(polar) * Math.sin(azimuth),
    Math.cos(polar),
    Math.sin(polar) * Math.cos(azimuth),
  ];
  const right = normalise([offset[2], 0, -offset[0]]);
  const up = cross(right, offset);

  const across: number[] = [];
  const along: number[] = [];
  for (const point of request.points) {
    const dx = point.x - request.center[0];
    const dy = point.y - request.center[1];
    const dz = point.z - request.center[2];
    across.push(Math.abs(dx * right[0] + dy * right[1] + dz * right[2]));
    along.push(Math.abs(dx * up[0] + dy * up[1] + dz * up[2]));
  }

  const halfWidth = quantileOf(across, request.quantile) + request.padding;
  const halfHeight = quantileOf(along, request.quantile) + request.padding;
  const vertical = (request.fov * Math.PI) / 180;
  const horizontal = 2 * Math.atan(Math.tan(vertical / 2) * request.aspect);
  const distance =
    Math.max(
      halfHeight / Math.tan(vertical / 2),
      halfWidth / Math.tan(horizontal / 2),
      1,
    ) * request.margin;

  return {
    distance,
    position: [
      request.center[0] + offset[0] * distance,
      request.center[1] + offset[1] * distance,
      request.center[2] + offset[2] * distance,
    ],
  };
}

function quantileOf(values: number[], quantile: number): number {
  if (values.length === 0) return 0;
  values.sort((left, right) => left - right);
  const position = Math.min(
    values.length - 1,
    Math.max(0, Math.floor(values.length * quantile)),
  );
  return values[position];
}

function normalise(
  vector: readonly [number, number, number],
): readonly [number, number, number] {
  const length = Math.hypot(vector[0], vector[1], vector[2]);
  if (length === 0) return [1, 0, 0];
  return [vector[0] / length, vector[1] / length, vector[2] / length];
}

function cross(
  left: readonly [number, number, number],
  right: readonly [number, number, number],
): readonly [number, number, number] {
  return [
    left[1] * right[2] - left[2] * right[1],
    left[2] * right[0] - left[0] * right[2],
    left[0] * right[1] - left[1] * right[0],
  ];
}
