import type { NodeRendererProps } from "reagraph";
import {
  Color,
  type ColorRepresentation,
  MeshBasicMaterial,
  SphereGeometry,
} from "three";

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
const MATERIALS = new Map<string, MeshBasicMaterial>();

function nodeMaterial(
  color: ColorRepresentation,
  opacity: number,
): MeshBasicMaterial {
  // Quantised so a hundred marginally different opacities cannot become a
  // hundred materials.
  const level = Math.min(1, Math.max(0, Math.round(opacity * 20) / 20));
  const key = `${new Color(color).getHexString()}:${level}`;
  const known = MATERIALS.get(key);
  if (known !== undefined) return known;
  const material = new MeshBasicMaterial({
    color,
    opacity: level,
    // A transparent material cannot be depth-sorted away, so the opaque case
    // stays opaque: it is the one that covers most of the tile.
    transparent: level < 1,
    depthWrite: level >= 1,
  });
  MATERIALS.set(key, material);
  return material;
}

/**
 * Draws a node as a flat-shaded sphere.
 *
 * The stock renderer uses a Phong material lit by a single ambient light and
 * a `0.7` emissive of the node's own colour, which at these sizes is a flat
 * disc of that colour. Computing it per fragment buys nothing.
 */
export function renderGraphNode({
  color,
  size,
  opacity,
}: NodeRendererProps): React.ReactElement {
  return (
    <mesh
      geometry={NODE_GEOMETRY}
      material={nodeMaterial(color, opacity)}
      scale={size}
      // The geometry and the material outlive every node that borrows them.
      dispose={null}
    />
  );
}
