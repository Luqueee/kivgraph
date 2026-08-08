import {
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
} from "./binary";
import {
  CROSS_DEPENDENCY_COLOR,
  CROSS_EDGE_SIZE,
  EXACT_DEPENDENCY_COLOR,
  LOCAL_DEPENDENCY_COLOR,
  LOCAL_EDGE_SIZE,
  NODE_SIZE_FILE,
  NODE_SIZE_PACKAGE,
  NODE_SIZE_SYMBOL,
  type ContainmentLinks,
  type ReagraphEdgeData,
  type ViewerReagraphEdge,
  type ViewerReagraphGraph,
  type ViewerReagraphNode,
} from "./reagraph";

export type ViewerLodKind =
  | typeof NODE_KIND_REPOSITORY
  | typeof NODE_KIND_PACKAGE
  | typeof NODE_KIND_FILE
  | typeof NODE_KIND_SYMBOL;

export interface LodCamera {
  /** Distance between the camera and its orbit target in world units. */
  readonly distance: number;
  /** Perspective vertical field of view in degrees. */
  readonly fov: number;
  /** Canvas height in CSS pixels. */
  readonly viewportHeight: number;
}

/** A node needs roughly this many pixels before its children are worth drawing. */
export const LOD_ENTER_PIXELS = 1.1;
/** Hysteresis prevents a wheel gesture from flickering at a boundary. */
export const LOD_LEAVE_PIXELS = 1;

const MAX_LOD_KIND: ViewerLodKind = NODE_KIND_SYMBOL;
const MIN_LOD_KIND: ViewerLodKind = NODE_KIND_REPOSITORY;

const BASE_SIZE_BY_KIND: Readonly<Record<ViewerLodKind, number>> = {
  [NODE_KIND_REPOSITORY]: 0,
  [NODE_KIND_PACKAGE]: NODE_SIZE_PACKAGE,
  [NODE_KIND_FILE]: NODE_SIZE_FILE,
  [NODE_KIND_SYMBOL]: NODE_SIZE_SYMBOL,
};

/**
 * Approximate the projected diameter of a base node. Node sizes are radii in
 * Reagraph's sphere renderer, so the projection uses a diameter of `2 * size`.
 */
export function projectedNodePixels(size: number, camera: LodCamera): number {
  if (
    !Number.isFinite(size) ||
    !Number.isFinite(camera.distance) ||
    !Number.isFinite(camera.fov) ||
    !Number.isFinite(camera.viewportHeight) ||
    size <= 0 ||
    camera.distance <= 0 ||
    camera.viewportHeight <= 0
  ) {
    return 0;
  }
  const fov = Math.min(179, Math.max(1, camera.fov)) * (Math.PI / 180);
  return (size * camera.viewportHeight) / (camera.distance * Math.tan(fov / 2));
}

/**
 * Pick the deepest kind whose base node is readable at the current distance.
 * Entering a detail level is harder than keeping it, so camera damping cannot
 * make the scene alternate between two populations while the wheel is moving.
 */
export function lodKindForCamera(
  camera: LodCamera,
  previous: ViewerLodKind = MAX_LOD_KIND,
): ViewerLodKind {
  let kind = clampKind(previous);
  while (
    kind < MAX_LOD_KIND &&
    projectedNodePixels(
      BASE_SIZE_BY_KIND[(kind + 1) as ViewerLodKind],
      camera,
    ) >= LOD_ENTER_PIXELS
  ) {
    kind = (kind + 1) as ViewerLodKind;
  }
  while (
    kind > MIN_LOD_KIND &&
    projectedNodePixels(BASE_SIZE_BY_KIND[kind], camera) < LOD_LEAVE_PIXELS
  ) {
    kind = (kind - 1) as ViewerLodKind;
  }
  return kind;
}

export interface LodGraphProjection {
  readonly nodes: ViewerReagraphNode[];
  readonly edges: ViewerReagraphEdge[];
  readonly containment: ContainmentLinks;
  readonly maxKind: ViewerLodKind;
  readonly hiddenNodeCount: number;
  /** Dependency edges removed or folded into a coarser route. */
  readonly hiddenEdgeCount: number;
}

interface EdgeBucket {
  edge: ViewerReagraphEdge;
  count: number;
  aggregate: boolean;
  exact: boolean;
  crossCluster: boolean;
}

/**
 * Project a loaded tile to the detail that the camera can resolve.
 *
 * Hidden dependency edges are lifted through the containment forest. Several
 * symbol-to-symbol relations can therefore become one package-to-package
 * route. This is a visual aggregate, not a new semantic edge: its payload is
 * marked with `lodAggregate` and the count remains visible to future UI work.
 */
export function projectGraphAtKind(
  graph: ViewerReagraphGraph,
  maxKind: ViewerLodKind,
): LodGraphProjection {
  if (maxKind >= NODE_KIND_SYMBOL) {
    return {
      nodes: graph.nodes,
      edges: graph.edges,
      containment: graph.containment,
      maxKind,
      hiddenNodeCount: 0,
      hiddenEdgeCount: 0,
    };
  }

  const parent = new Int32Array(graph.nodes.length).fill(-1);
  const indexById = new Map<string, number>();
  for (let index = 0; index < graph.nodes.length; index += 1) {
    indexById.set(graph.nodes[index].id, index);
  }
  for (let index = 0; index < graph.containment.source.length; index += 1) {
    const source = graph.containment.source[index];
    const target = graph.containment.target[index];
    if (source < graph.nodes.length && target < graph.nodes.length) {
      parent[target] = source;
    }
  }

  const projectedIndex = new Int32Array(graph.nodes.length).fill(-1);
  const nodes: ViewerReagraphNode[] = [];
  for (let index = 0; index < graph.nodes.length; index += 1) {
    if (graph.nodes[index].data.kind > maxKind) continue;
    projectedIndex[index] = nodes.length;
    nodes.push(graph.nodes[index]);
  }

  const containmentSource: number[] = [];
  const containmentTarget: number[] = [];
  for (let index = 0; index < graph.containment.source.length; index += 1) {
    const source = graph.containment.source[index];
    const target = graph.containment.target[index];
    const projectedSource = projectedIndex[source] ?? -1;
    const projectedTarget = projectedIndex[target] ?? -1;
    if (projectedSource < 0 || projectedTarget < 0) continue;
    containmentSource.push(projectedSource);
    containmentTarget.push(projectedTarget);
  }

  const routeToVisible = (index: number): number => {
    let current = index;
    for (let step = 0; step <= graph.nodes.length; step += 1) {
      if (current < 0 || current >= graph.nodes.length) return -1;
      const visible = projectedIndex[current];
      if (visible >= 0) return current;
      current = parent[current];
    }
    return -1;
  };

  const buckets = new Map<string, EdgeBucket>();
  for (const edge of graph.edges) {
    const sourceIndex = indexById.get(edge.source);
    const targetIndex = indexById.get(edge.target);
    if (sourceIndex === undefined || targetIndex === undefined) continue;
    const routedSource = routeToVisible(sourceIndex);
    const routedTarget = routeToVisible(targetIndex);
    if (routedSource < 0 || routedTarget < 0 || routedSource === routedTarget) {
      continue;
    }
    const source = nodes[projectedIndex[routedSource]];
    const target = nodes[projectedIndex[routedTarget]];
    if (source === undefined || target === undefined) continue;
    const crossCluster = source.data.cluster !== target.data.cluster;
    const exact = edge.data.confidence >= 2;
    const wasLifted =
      routedSource !== sourceIndex || routedTarget !== targetIndex;
    const key = `${source.id}\0${target.id}`;
    const existing = buckets.get(key);
    if (existing === undefined) {
      buckets.set(key, {
        edge: makeProjectedEdge(
          edge,
          source,
          target,
          exact,
          crossCluster,
          wasLifted,
          1,
        ),
        count: 1,
        aggregate: wasLifted,
        exact,
        crossCluster,
      });
      continue;
    }
    existing.count += 1;
    existing.aggregate = true;
    existing.exact ||= exact;
    existing.crossCluster ||= crossCluster;
    existing.edge = makeProjectedEdge(
      existing.edge,
      source,
      target,
      existing.exact,
      existing.crossCluster,
      true,
      existing.count,
    );
  }

  const edges = [...buckets.values()].map(({ edge }) => edge);
  return {
    nodes,
    edges,
    containment: {
      source: Uint32Array.from(containmentSource),
      target: Uint32Array.from(containmentTarget),
    },
    maxKind,
    hiddenNodeCount: graph.nodes.length - nodes.length,
    hiddenEdgeCount: graph.edges.length - edges.length,
  };
}

function makeProjectedEdge(
  edge: ViewerReagraphEdge,
  source: ViewerReagraphNode,
  target: ViewerReagraphNode,
  exact: boolean,
  crossCluster: boolean,
  aggregate: boolean,
  count: number,
): ViewerReagraphEdge {
  const data: ReagraphEdgeData = {
    ...edge.data,
    crossCluster,
    lodAggregate: aggregate,
    aggregateCount: aggregate ? count : undefined,
  };
  return {
    ...edge,
    id: aggregate ? `lod-edge-${source.id}-${target.id}` : edge.id,
    source: source.id,
    target: target.id,
    fill: exact
      ? EXACT_DEPENDENCY_COLOR
      : crossCluster
        ? CROSS_DEPENDENCY_COLOR
        : LOCAL_DEPENDENCY_COLOR,
    size: crossCluster || exact ? CROSS_EDGE_SIZE : LOCAL_EDGE_SIZE,
    interpolation: crossCluster ? "curved" : "linear",
    data,
  };
}

function clampKind(value: ViewerLodKind): ViewerLodKind {
  if (value <= MIN_LOD_KIND) return MIN_LOD_KIND;
  if (value >= MAX_LOD_KIND) return MAX_LOD_KIND;
  return value;
}
