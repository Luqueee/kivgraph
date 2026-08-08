export interface InstancedGeometryLike {
  readonly isInstancedBufferGeometry?: boolean;
  instanceCount: number;
}

interface GeometryOwnerLike {
  readonly geometry?: InstancedGeometryLike;
}

export interface SceneTraversalLike {
  traverse(callback: (object: unknown) => void): void;
}

/** Converts an invalid WebGPU instance count into a safe empty draw. */
export function finiteInstanceCount(value: number): number {
  return Number.isFinite(value) && value >= 0 ? value : 0;
}

/**
 * Prevents WebGPU's unsigned draw arguments from receiving Three's Infinity
 * default while an asynchronously prepared instanced geometry has no data.
 * Returns the number of geometries changed so the caller can report it once.
 */
export function guardWebGPUInstancedGeometry(
  scene: SceneTraversalLike,
): number {
  let repaired = 0;
  scene.traverse((object) => {
    const geometry = (object as GeometryOwnerLike).geometry;
    if (
      geometry?.isInstancedBufferGeometry === true &&
      !Number.isFinite(geometry.instanceCount)
    ) {
      geometry.instanceCount = finiteInstanceCount(geometry.instanceCount);
      repaired += 1;
    }
  });
  return repaired;
}
