import { useEffect, useMemo, useRef } from "react";
import {
  BufferAttribute,
  BufferGeometry,
  Color,
  type LineSegments,
} from "three";

import {
  CONTAINMENT_COLOR,
  containmentPresence,
  type ContainmentLinks,
  type ViewerReagraphNode,
} from "./reagraph";

/** Colour an inactive line fades towards: the canvas background. */
const BACKGROUND = new Color("#1e2026");

export interface ContainmentLinesProps {
  readonly nodes: readonly ViewerReagraphNode[];
  readonly links: ContainmentLinks;
  /** Ids that keep their colour while something is selected. */
  readonly actives: ReadonlySet<string>;
  /** How much of the colour an inactive line keeps. */
  readonly inactiveOpacity: number;
}

/**
 * Draws every container-to-child link as one mesh of line segments.
 *
 * There is one of these per node, so on a tile of a thousand nodes they
 * outnumber the dependencies four to one. Reagraph builds a tube per edge and
 * merges the lot again whenever the highlighted set changes, which turns
 * pointing at a node into half a second of geometry building. Containment
 * never needs to be picked, so it does not have to be an edge at all: two
 * vertices per link in a single buffer, and highlighting is a write into the
 * colour attribute instead of a rebuild.
 */
export function ContainmentLines({
  nodes,
  links,
  actives,
  inactiveOpacity,
}: ContainmentLinesProps): React.ReactElement | null {
  const mesh = useRef<LineSegments>(null);

  const geometry = useMemo(() => {
    const count = links.source.length;
    const positions = new Float32Array(count * 6);
    const colors = new Float32Array(count * 6);
    for (let index = 0; index < count; index += 1) {
      const from = nodes[links.source[index]]?.data;
      const to = nodes[links.target[index]]?.data;
      if (from === undefined || to === undefined) continue;
      positions.set([from.x, from.y, from.z, to.x, to.y, to.z], index * 6);
    }
    const built = new BufferGeometry();
    built.setAttribute("position", new BufferAttribute(positions, 3));
    built.setAttribute("color", new BufferAttribute(colors, 3));
    return built;
  }, [nodes, links]);

  useEffect(() => () => geometry.dispose(), [geometry]);

  // Recolouring touches a typed array and one dirty flag; the geometry, the
  // material and the draw call all survive a hover untouched.
  useEffect(() => {
    const attribute = geometry.getAttribute("color") as BufferAttribute;
    const colors = attribute.array as Float32Array;
    const full = new Color(CONTAINMENT_COLOR);
    const dimming = actives.size > 0;
    const tone = new Color();
    for (let index = 0; index < links.source.length; index += 1) {
      const child = nodes[links.target[index]];
      if (child === undefined) continue;
      const lit =
        !dimming ||
        actives.has(nodes[links.source[index]]?.id ?? "") ||
        actives.has(child.id);
      const presence =
        containmentPresence(child.data.kind) * (lit ? 1 : inactiveOpacity);
      tone.copy(BACKGROUND).lerp(full, presence);
      for (let vertex = 0; vertex < 2; vertex += 1) {
        colors.set([tone.r, tone.g, tone.b], index * 6 + vertex * 3);
      }
    }
    attribute.needsUpdate = true;
  }, [geometry, links, nodes, actives, inactiveOpacity]);
  if (links.source.length === 0) return null;
  return (
    <lineSegments ref={mesh} geometry={geometry} frustumCulled={false}>
      <lineBasicMaterial vertexColors />
    </lineSegments>
  );
}
