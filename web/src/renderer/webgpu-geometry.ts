import { InstancedBufferGeometry } from "three";

interface InstancedAttributeLike {
  readonly isInstancedBufferAttribute?: boolean;
  readonly meshPerAttribute?: number;
  readonly count: number;
}

export interface InstancedGeometryLike {
  readonly attributes: Record<string, InstancedAttributeLike | undefined>;
}

/** Backing store for the raw value libraries assign, kept off the prototype. */
const RAW_INSTANCE_COUNT = Symbol.for("kivgraph.rawInstanceCount");

/**
 * Mirrors what WebGL derives from the instanced attributes, which is what
 * Three's `Infinity` default means: draw every instance the buffers hold.
 * A geometry whose attributes have not arrived yet draws nothing.
 */
export function resolvedInstanceCount(geometry: InstancedGeometryLike): number {
  let resolved = Number.POSITIVE_INFINITY;
  for (const attribute of Object.values(geometry.attributes)) {
    if (attribute?.isInstancedBufferAttribute !== true) continue;
    const perAttribute = attribute.meshPerAttribute ?? 1;
    resolved = Math.min(resolved, perAttribute * attribute.count);
  }
  return Number.isFinite(resolved) ? resolved : 0;
}

/**
 * WebGPU's `drawIndexed` takes an unsigned integer, so Three's `Infinity`
 * default for `InstancedBufferGeometry.instanceCount` - which Troika and other
 * libraries keep until their asynchronous data lands - aborts the frame with a
 * `TypeError` instead of drawing. Resolving the count on read fixes every
 * instanced geometry the scene will ever hold, including the ones React mounts
 * mid-commit, and it keeps the WebGL meaning of the default.
 *
 * Returns `true` when this call installed the accessor.
 */
export function installWebGPUInstanceCountGuard(): boolean {
  const prototype = InstancedBufferGeometry.prototype as unknown as Record<
    string,
    unknown
  >;
  const existing = Object.getOwnPropertyDescriptor(prototype, "instanceCount");
  if (existing?.get !== undefined) return false;

  Object.defineProperty(prototype, "instanceCount", {
    configurable: true,
    get(this: InstancedGeometryLike & Record<symbol, unknown>): number {
      const raw = this[RAW_INSTANCE_COUNT];
      if (typeof raw === "number" && Number.isFinite(raw)) {
        return Math.max(0, raw);
      }
      return resolvedInstanceCount(this);
    },
    set(this: Record<symbol, unknown>, value: number): void {
      this[RAW_INSTANCE_COUNT] = value;
    },
  });
  return true;
}
