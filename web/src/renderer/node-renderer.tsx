import type { NodeRendererProps } from "reagraph";
import {
  Color,
  type ColorRepresentation,
  MeshBasicMaterial,
  SphereGeometry,
} from "three";
import { MeshBasicNodeMaterial } from "three/webgpu";

/**
 * One geometry for every node in the scene.
 *
 * Reagraph draws each node with a `25 × 25` sphere - about `1.250` triangles
 * for a dot five pixels wide - so a tile of a thousand nodes asks the GPU for
 * a million and a half triangles of detail nobody can see. Ten by eight is
 * `160` triangles and is indistinguishable at the size the viewer draws.
 */
const NODE_GEOMETRY = new SphereGeometry(1, 10, 8);

/**
 * Materials shared by colour and opacity.
 *
 * The palette has four node colours plus the active one, and dimming has two
 * levels, so the whole scene needs about ten materials rather than one per
 * node. Fewer materials mean fewer uniform uploads and fewer objects for the
 * garbage collector to walk when a tile is replaced.
 */
const WEBGL_MATERIALS = new Map<string, MeshBasicMaterial>();
const WEBGPU_MATERIALS = new Map<string, MeshBasicNodeMaterial>();

function opacityLevel(opacity: number): number {
  // Quantised so a hundred marginally different opacities cannot become a
  // hundred materials.
  return Math.min(1, Math.max(0, Math.round(opacity * 20) / 20));
}

function materialKey(color: ColorRepresentation, opacity: number): string {
  return `${new Color(color).getHexString()}:${opacityLevel(opacity)}`;
}

function nodeMaterial(
  color: ColorRepresentation,
  opacity: number,
): MeshBasicMaterial {
  const level = opacityLevel(opacity);
  const key = materialKey(color, opacity);
  const known = WEBGL_MATERIALS.get(key);
  if (known !== undefined) return known;
  const material = new MeshBasicMaterial({
    color,
    opacity: level,
    // A transparent material cannot be depth-sorted away, so the opaque case
    // stays opaque: it is the one that covers most of the tile.
    transparent: level < 1,
    depthWrite: level >= 1,
  });
  WEBGL_MATERIALS.set(key, material);
  return material;
}

function nodeMaterialWebGPU(
  color: ColorRepresentation,
  opacity: number,
): MeshBasicNodeMaterial {
  const level = opacityLevel(opacity);
  const key = materialKey(color, opacity);
  const known = WEBGPU_MATERIALS.get(key);
  if (known !== undefined) return known;
  const material = new MeshBasicNodeMaterial({
    color,
    opacity: level,
    transparent: level < 1,
    depthWrite: level >= 1,
  });
  WEBGPU_MATERIALS.set(key, material);
  return material;
}

function renderNode(
  material: MeshBasicMaterial | MeshBasicNodeMaterial,
  size: number,
): React.ReactElement {
  return (
    <mesh
      geometry={NODE_GEOMETRY}
      material={material}
      scale={size}
      // The geometry and the material outlive every node that borrows them.
      dispose={null}
    />
  );
}

/**
 * Draws a node as a flat-shaded sphere using the standard WebGL material.
 */
export function renderGraphNode({
  color,
  size,
  opacity,
}: NodeRendererProps): React.ReactElement {
  return renderNode(nodeMaterial(color, opacity), size);
}

/**
 * Draws the same node with a TSL node material. Three's WebGPU renderer does
 * not accept classic MeshBasicMaterial instances.
 */
export function renderGraphNodeWebGPU({
  color,
  size,
  opacity,
}: NodeRendererProps): React.ReactElement {
  return renderNode(nodeMaterialWebGPU(color, opacity), size);
}
