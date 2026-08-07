import { DEFAULT_REAGRAPH_NODE_LIMIT } from "@/renderer/reagraph";

/** Fewest nodes worth drawing: below this a level says nothing. */
export const MIN_TILE_BUDGET = 100;

/** Step the slider moves in, and the granularity every budget is rounded to. */
export const TILE_BUDGET_STEP = 100;

/**
 * Most nodes a single tile may carry, matching the server's own per-tile
 * ceiling. Reaching it is expensive - the renderer builds an object per node -
 * but the deep levels are unreachable below it.
 */
export const MAX_TILE_BUDGET = DEFAULT_REAGRAPH_NODE_LIMIT;

/**
 * Nodes to request for one level.
 *
 * Three ceilings apply and the smallest wins: what the user asked for, what
 * the server offers for this snapshot, and what the adapter will materialise.
 * Asking for more than any of them only produces a rejected tile.
 */
export function tileBudget(requested: number, layoutMaxNodes: number): number {
  const ceiling = Math.min(
    layoutMaxNodes > 0 ? layoutMaxNodes : DEFAULT_REAGRAPH_NODE_LIMIT,
    DEFAULT_REAGRAPH_NODE_LIMIT,
  );
  if (!Number.isFinite(requested)) return Math.min(ceiling, MIN_TILE_BUDGET);
  const rounded = Math.round(requested / TILE_BUDGET_STEP) * TILE_BUDGET_STEP;
  return Math.max(MIN_TILE_BUDGET, Math.min(ceiling, rounded));
}
