import { ApiError } from "@/api/client";
import type { ViewerReagraphView } from "@/renderer/reagraph";
import { loadTileView, type TileLoadRequest } from "@/renderer/tile-loader";
import type {
  TileWorkerRequest,
  TileWorkerResponse,
} from "@/worker/tile-worker";

export interface TileWorkerClient {
  load(
    request: TileLoadRequest,
    signal?: AbortSignal,
  ): Promise<ViewerReagraphView>;
  close(): void;
}

interface Pending {
  resolve(view: ViewerReagraphView): void;
  reject(error: unknown): void;
}

/**
 * Runs tile loading in a worker so fetching, decoding and adapting never block
 * the render thread.
 *
 * Where `Worker` is unavailable — server rendering, and the unit tests — the
 * same code runs inline. The caller cannot tell the difference, and there is
 * no second implementation to keep in sync.
 */
export function createTileWorkerClient(): TileWorkerClient {
  if (typeof Worker === "undefined") {
    return {
      load: (request, signal) => loadTileView(request, signal),
      close: () => {},
    };
  }

  const worker = new Worker(new URL("./tile-worker.ts", import.meta.url), {
    type: "module",
  });
  const pending = new Map<number, Pending>();
  let nextId = 0;

  worker.addEventListener(
    "message",
    (event: MessageEvent<TileWorkerResponse>) => {
      const entry = pending.get(event.data.id);
      if (!entry) return;
      pending.delete(event.data.id);
      if ("error" in event.data) {
        entry.reject(
          new ApiError(event.data.error.code, 0, event.data.error.message),
        );
        return;
      }
      entry.resolve(event.data.view);
    },
  );
  worker.addEventListener("error", (event) => {
    const failure = new ApiError("WORKER_FAILED", 0, event.message);
    for (const entry of pending.values()) entry.reject(failure);
    pending.clear();
  });

  return {
    load(request, signal) {
      const id = nextId++;
      const message: TileWorkerRequest = { id, request };
      return new Promise<ViewerReagraphView>((resolve, reject) => {
        pending.set(id, { resolve, reject });
        // An abandoned level stops downloading instead of racing the one that
        // replaced it.
        signal?.addEventListener("abort", () => {
          if (!pending.delete(id)) return;
          const cancel: TileWorkerRequest = { id, cancel: true };
          worker.postMessage(cancel);
          reject(
            signal.reason ?? new ApiError("ABORTED", 0, "request aborted"),
          );
        });
        worker.postMessage(message);
      });
    },
    close() {
      worker.terminate();
      pending.clear();
    },
  };
}
