/**
 * Every tunable of the structural 3D layout, in one place.
 *
 * Almost all of them are ratios rather than distances. A hundred nodes and two
 * thousand nodes cannot share an absolute spacing - the first would be lost in
 * the void and the second a ball of wool - but they can share a proportion, so
 * the drawing keeps its density at any size without a table of magic numbers
 * per scale. The one absolute unit is the radius the renderer draws a node
 * with, and every distance is expressed against it.
 */
export interface GraphLayoutConfig {
  /** Space a node reserves, as a multiple of the radius it is drawn with. */
  readonly leafAir: number;
  /** Extra room granted to a node at full importance, in drawn radii. */
  readonly hubAir: number;
  /** Free space between two siblings on a shell, in drawn radii. */
  readonly nodePadding: number;
  /**
   * Gap between two unrelated cluster balls, as a fraction of the two radii.
   * This is the outermost of the four link distances: same community, same
   * cluster, dependent clusters, unrelated clusters.
   */
  readonly clusterSpacing: number;
  /** Floor of that gap: two repositories approach but never merge. */
  readonly minClusterSpacing: number;
  /** Height of one dependency layer, as a fraction of the mean cluster ball. */
  readonly hierarchySpacing: number;
  /** Fraction of its layer offset a community lobe actually takes. */
  readonly communityHierarchyBias: number;
  /** Fraction of its layer offset an individual node takes inside a lobe. */
  readonly nodeHierarchyBias: number;
  /** Angular and radial noise that keeps a shell from looking machined. */
  readonly organicJitter: number;
  /** Passes of the cluster-level relaxation. */
  readonly clusterIterations: number;
  /** How hard a dependency pulls two cluster balls towards each other. */
  readonly clusterAttraction: number;
  /** Passes of the relaxation run inside each cluster. */
  readonly refineIterations: number;
  /** How hard refinement holds a node to its structural target. */
  readonly structuralSpring: number;
  /** How hard a dependency pulls its two endpoints together. */
  readonly linkStrength: number;
  /** How hard two overlapping nodes push each other apart. */
  readonly collisionStrength: number;
  /** Resting length of a dependency, as a fraction of the two node radii. */
  readonly linkDistance: {
    /** Both endpoints in the same community: the tightest relation drawn. */
    readonly sameCommunity: number;
    /** Same cluster, different communities. */
    readonly sameCluster: number;
  };
  /** Narrowest an axis may get, as a fraction of the widest one. */
  readonly minAxisSpreadRatio: number;
  /**
   * Share of the nodes the reported bounding sphere must contain. The camera
   * frames that sphere, and a single far outlier would otherwise push the
   * whole graph into the middle third of the screen.
   */
  readonly boundingQuantile: number;
}

export const GRAPH_LAYOUT_CONFIG: GraphLayoutConfig = {
  leafAir: 1.35,
  hubAir: 1,
  nodePadding: 0.6,
  clusterSpacing: 0.22,
  minClusterSpacing: 0.08,
  hierarchySpacing: 0.85,
  communityHierarchyBias: 0.55,
  nodeHierarchyBias: 0.3,
  organicJitter: 0.35,
  clusterIterations: 420,
  clusterAttraction: 0.1,
  refineIterations: 42,
  structuralSpring: 0.22,
  linkStrength: 0.09,
  collisionStrength: 0.65,
  linkDistance: {
    sameCommunity: 2.4,
    sameCluster: 5,
  },
  minAxisSpreadRatio: 0.5,
  boundingQuantile: 0.94,
};

/**
 * Adapts the configuration to the tile being drawn.
 *
 * The distances are proportions and need no adjusting; the relaxation budget
 * does. It runs before the first frame can be drawn, so a large tile trades
 * passes for latency - the structural placement it refines is already correct
 * on its own.
 */
export function resolveLayoutConfig(
  nodeCount: number,
  overrides: Partial<GraphLayoutConfig> = {},
): GraphLayoutConfig {
  const base = GRAPH_LAYOUT_CONFIG;
  return {
    ...base,
    refineIterations: Math.max(
      10,
      Math.min(
        base.refineIterations,
        Math.round(26_000 / Math.max(nodeCount, 1)),
      ),
    ),
    ...overrides,
  };
}
