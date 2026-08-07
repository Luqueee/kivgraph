/// <reference lib="webworker" />

import { ApiError } from "@/api/client";
import { GraphBinaryError } from "@/renderer/binary";
import type { ViewerReagraphView } from "@/renderer/reagraph";
import { loadTileView, type TileLoadRequest } from "@/renderer/tile-loader";

export type TileWorkerRequest =
  | { readonly id: number; readonly request: TileLoadRequest }
  | { readonly id: number; readonly cancel: true };

export type TileWorkerResponse =
  | { readonly id: number; readonly view: ViewerReagraphView }
  | {
      readonly id: number;
      readonly error: { readonly code: string; readonly message: string };
    };

/** Classifies a failure into the stable code the viewer already renders. */
export function describeWorkerFailure(error: unknown): {
  code: string;
  message: string;
} {
  if (error instanceof ApiError || error instanceof GraphBinaryError) {
    return { code: error.code, message: error.message };
  }
  if (error instanceof Error) {
    return { code: "WORKER_FAILED", message: error.message };
  }
  return { code: "WORKER_FAILED", message: "unknown error" };
}

// Fetching, decoding and adapting a tile runs here so the render thread keeps
// answering clicks while a level loads. The rendering itself stays on the main
// thread: WebGL is not reachable from a worker in this setup.
//
// Each request owns an AbortController. Switching level cancels the previous
// one, so a tile nobody will look at stops downloading instead of competing
// for bandwidth with the one that replaced it.
const inFlight = new Map<number, AbortController>();

self.addEventListener("message", (event: MessageEvent<TileWorkerRequest>) => {
  const message = event.data;
  if ("cancel" in message) {
    inFlight.get(message.id)?.abort();
    inFlight.delete(message.id);
    return;
  }

  const controller = new AbortController();
  inFlight.set(message.id, controller);
  loadTileView(message.request, controller.signal)
    .then((view) => {
      if (!inFlight.delete(message.id)) return;
      const response: TileWorkerResponse = { id: message.id, view };
      self.postMessage(response);
    })
    .catch((error: unknown) => {
      if (!inFlight.delete(message.id)) return;
      const response: TileWorkerResponse = {
        id: message.id,
        error: describeWorkerFailure(error),
      };
      self.postMessage(response);
    });
});
