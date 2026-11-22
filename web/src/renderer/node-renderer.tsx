import type { NodeRendererProps } from "reagraph";
import {
  CanvasTexture,
  Color,
  type ColorRepresentation,
  LinearFilter,
  MeshBasicMaterial,
  SphereGeometry,
  SpriteMaterial,
  SRGBColorSpace,
} from "three";
import { MeshBasicNodeMaterial, SpriteNodeMaterial } from "three/webgpu";

import { captionPlacement } from "./text-labels";

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

/** Reagraph's dark theme: a caption, and the caption of the node pointed at. */
const CAPTION_COLOR = "#ACBAC7";
const CAPTION_ACTIVE_COLOR = "#1DE9AC";
/** Pixels of texture per caption line. */
const CAPTION_PIXELS = 64;

interface Caption {
  readonly material: SpriteMaterial | SpriteNodeMaterial;
  readonly width: number;
  readonly height: number;
}

const CAPTIONS = new Map<string, Caption>();

/**
 * A caption as a sprite: its text drawn by the browser onto a texture.
 *
 * Reagraph draws captions with Troika, and the viewer stopped asking it to.
 * Troika resolves the font of every character through data it fetches from a
 * CDN: a local, read-only viewer made a request to a third party on each load,
 * and without a route to it the canvas waited for an answer that never came
 * and drew no node at all. And its glyph material is a GLSL patch the WebGPU
 * renderer does not run, so there every caption was one untransformed quad --
 * a dot under each repository. The browser already draws text, in every
 * script and with nothing to fetch, and only repositories and hubs are
 * captioned.
 */
function caption(text: string, color: string, webgpu: boolean): Caption {
  const key = `${webgpu ? "gpu" : "gl"}:${color}:${text}`;
  const known = CAPTIONS.get(key);
  if (known !== undefined) return known;
  const canvas = document.createElement("canvas");
  const font = `500 ${CAPTION_PIXELS * 0.72}px "Geist Variable", system-ui, sans-serif`;
  const context = canvas.getContext("2d");
  let width = CAPTION_PIXELS;
  if (context !== null) {
    context.font = font;
    width = Math.max(1, Math.ceil(context.measureText(text).width) + 8);
  }
  canvas.width = width;
  canvas.height = CAPTION_PIXELS;
  if (context !== null) {
    // Resizing a canvas resets its state.
    context.font = font;
    context.textAlign = "center";
    context.textBaseline = "middle";
    context.fillStyle = color;
    context.fillText(text, width / 2, CAPTION_PIXELS / 2);
  }
  const texture = new CanvasTexture(canvas);
  texture.colorSpace = SRGBColorSpace;
  texture.minFilter = LinearFilter;
  texture.generateMipmaps = false;
  const options = { map: texture, transparent: true, depthWrite: false };
  // Three's WebGPU renderer does not accept a classic SpriteMaterial.
  const material = webgpu
    ? new SpriteNodeMaterial(options)
    : new SpriteMaterial(options);
  // What the frame governor hides while the camera moves.
  material.userData = { caption: true };
  const made = { material, width, height: CAPTION_PIXELS };
  CAPTIONS.set(key, made);
  return made;
}

function renderCaptioned(
  sphere: React.ReactElement,
  { size, node, active, selected }: NodeRendererProps,
  webgpu: boolean,
): React.ReactElement {
  if (typeof node.label !== "string" || node.label === "") return sphere;
  const drawn = caption(
    node.label,
    active || selected ? CAPTION_ACTIVE_COLOR : CAPTION_COLOR,
    webgpu,
  );
  const placement = captionPlacement(size, drawn.width, drawn.height);
  return (
    <>
      {sphere}
      <sprite
        material={drawn.material}
        position={placement.position}
        scale={placement.scale}
        renderOrder={1}
        // Shared by every node with this caption.
        dispose={null}
      />
    </>
  );
}

/**
 * Draws a node as a flat-shaded sphere using the standard WebGL material.
 */
export function renderGraphNode(props: NodeRendererProps): React.ReactElement {
  return renderCaptioned(
    renderNode(nodeMaterial(props.color, props.opacity), props.size),
    props,
    false,
  );
}

/**
 * Draws the same node with a TSL node material. Three's WebGPU renderer does
 * not accept classic MeshBasicMaterial instances.
 */
export function renderGraphNodeWebGPU(
  props: NodeRendererProps,
): React.ReactElement {
  return renderCaptioned(
    renderNode(nodeMaterialWebGPU(props.color, props.opacity), props.size),
    props,
    true,
  );
}
