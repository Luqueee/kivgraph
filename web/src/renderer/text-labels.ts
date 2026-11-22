interface MaterialLike {
  readonly isTroikaTextMaterial?: boolean;
  /** A caption the node renderer draws as a sprite carries this mark. */
  readonly userData?: { readonly caption?: boolean };
}

const isCaption = (material: MaterialLike | undefined): boolean =>
  material?.isTroikaTextMaterial === true ||
  material?.userData?.caption === true;

interface SceneObjectLike {
  visible: boolean;
  readonly material?: MaterialLike | readonly MaterialLike[];
}

export interface SceneTraversalLike {
  traverse(callback: (object: SceneObjectLike) => void): void;
}

function isTextLabel(object: SceneObjectLike): boolean {
  const material = object.material;
  if (material === undefined) return false;
  return Array.isArray(material)
    ? material.some(isCaption)
    : isCaption(material as MaterialLike);
}

/**
 * Hides the text labels for the duration of a camera gesture.
 *
 * Reagraph's own switch unmounts every `Label`, which tears down and rebuilds
 * hundreds of glyph meshes: two stalls of well over a hundred milliseconds per
 * drag, which is exactly the cost the switch was meant to avoid. Flipping
 * `visible` skips the draws without touching the React tree or the meshes.
 *
 * Returns how many objects changed so the caller can skip a needless frame.
 */
export function setTextLabelsHidden(
  scene: SceneTraversalLike,
  hidden: boolean,
): number {
  let changed = 0;
  scene.traverse((object) => {
    if (!isTextLabel(object)) return;
    if (object.visible === !hidden) return;
    object.visible = !hidden;
    changed += 1;
  });
  return changed;
}

/** The height of a caption in world units: Reagraph's own label size. */
export const CAPTION_HEIGHT = 7 * 1.2;

/**
 * Where a caption sprite sits and how large it is, for a node of `size` and a
 * texture of `width` by `height` pixels: below the node, as tall as a Reagraph
 * label, as wide as its text.
 */
export function captionPlacement(
  size: number,
  width: number,
  height: number,
): {
  position: [number, number, number];
  scale: [number, number, number];
} {
  const aspect = height > 0 ? width / height : 1;
  return {
    position: [0, -(size + CAPTION_HEIGHT), 2],
    scale: [CAPTION_HEIGHT * aspect, CAPTION_HEIGHT, 1],
  };
}
