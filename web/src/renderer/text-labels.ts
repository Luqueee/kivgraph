interface MaterialLike {
  readonly isTroikaTextMaterial?: boolean;
}

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
    ? material.some((entry) => entry?.isTroikaTextMaterial === true)
    : (material as MaterialLike).isTroikaTextMaterial === true;
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
