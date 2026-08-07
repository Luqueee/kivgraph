import type { GraphLayoutConfig } from "./config";
import { resolveLayoutConfig } from "./config";
import type { NodeRadius } from "./place";
import { placeStructure } from "./place";
import type { GraphStructure, LayoutGraph } from "./structure";
import { buildStructure } from "./structure";

export type { GraphLayoutConfig } from "./config";
export type { NodeRadius } from "./place";
export type { GraphStructure, LayoutCluster, LayoutGraph } from "./structure";

export interface StructuralLayout {
  readonly x: Float32Array;
  readonly y: Float32Array;
  readonly z: Float32Array;
  /** Space each node reserves: its drawn radius plus the air around it. */
  readonly radius: Float32Array;
  /** Position of each node in the tile ordered by importance; `0` is first. */
  readonly rank: Int32Array;
  readonly cluster: Int32Array;
  readonly community: Int32Array;
  readonly layer: Int32Array;
  /** Centrality in `[0, 1]`; drives node size, spacing and label priority. */
  readonly importance: Float32Array;
  readonly clusterCount: number;
  readonly layerCount: number;
  readonly center: readonly [number, number, number];
  readonly boundingRadius: number;
  /** Standard deviation per axis. A near-zero entry means a collapsed layout. */
  readonly spread: readonly [number, number, number];
}

/**
 * Turns a decoded tile into positions that show the architecture.
 *
 * Order matters and is the whole point: clusters, hierarchy and communities
 * are derived from the graph first, the volume is then divided among them, and
 * only at the end does a short relaxation refine what structure could not
 * decide. Nothing here reads a published coordinate and nothing is random.
 */
export function computeStructuralLayout(
  graph: LayoutGraph,
  drawnRadius: NodeRadius,
  overrides: Partial<GraphLayoutConfig> = {},
): StructuralLayout {
  const structure: GraphStructure = buildStructure(graph);
  const config = resolveLayoutConfig(graph.nodeCount, overrides);
  const placement = placeStructure(graph, structure, drawnRadius, config);
  return {
    x: placement.x,
    y: placement.y,
    z: placement.z,
    radius: placement.radius,
    rank: placement.rank,
    cluster: structure.cluster,
    community: structure.community,
    layer: structure.layer,
    importance: structure.importance,
    clusterCount: structure.clusters.length,
    layerCount: structure.layerCount,
    center: placement.center,
    boundingRadius: placement.boundingRadius,
    spread: placement.spread,
  };
}
