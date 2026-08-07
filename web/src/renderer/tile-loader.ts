import { fetchTile, type TileRequest } from "@/api/client";
import { decodeGraphPayload } from "@/renderer/binary";
import {
  createReagraphView,
  type ReagraphGraphLimits,
  type ViewerReagraphView,
} from "@/renderer/reagraph";

export interface TileLoadRequest extends TileRequest {
  readonly limits?: ReagraphGraphLimits;
}

/**
 * Fetches one tile and adapts it to the renderer's view model.
 *
 * Kept free of DOM and React so the worker and the tests run the same code:
 * the worker is a thin wrapper over this function.
 */
export async function loadTileView(
  request: TileLoadRequest,
  signal?: AbortSignal,
  fetchImpl: typeof fetchTile = fetchTile,
): Promise<ViewerReagraphView> {
  const buffer = await fetchImpl(request, signal);
  return createReagraphView(decodeGraphPayload(buffer), request.limits ?? {});
}
