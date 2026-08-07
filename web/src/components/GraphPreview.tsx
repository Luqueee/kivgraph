import { useEffect, useMemo, useRef, useState } from "react";
import { GraphCanvas, darkTheme } from "reagraph";
import type { InternalGraphNode } from "reagraph";

import { ApiError, fetchMeta, type SnapshotMeta } from "@/api/client";
import {
  CONTAINMENT_COLOR,
  createLayoutOverrides,
  DEFAULT_REAGRAPH_NODE_LIMIT,
  DEPENDENCY_COLOR,
  EXACT_DEPENDENCY_COLOR,
  NODE_COLORS,
  type ReagraphNodeData,
  type ViewerReagraphGraph,
} from "@/renderer/reagraph";
import { createTileWorkerClient, type TileWorkerClient } from "@/worker/client";

const READY_STATUS = "drag to pan · wheel to zoom";
const ROTATE_STATUS = "drag to rotate · wheel to zoom";

/** Detail levels of the published layout, from repositories to symbols. */
const LOD_LABELS = ["repositories", "packages", "files", "symbols"] as const;

/**
 * Nodes requested per level. Reagraph builds an object per node — measured at
 * roughly four milliseconds each — so a whole level of files would take ten
 * seconds to appear. The coarse levels fit entirely; the deep ones are capped
 * and the view says so.
 */
const LOD_NODE_BUDGET = [2_000, 2_000, 1_200, 1_200];

/** Above this node count captions overlap; names move to hover only. */
const LABEL_LIMIT = 200;

/** Legend dots mirror the node sizes the canvas draws, scaled down. */
const LEGEND_DOT_SIZES = [10, 8, 7, 6];

interface ViewerState {
  readonly meta: SnapshotMeta | null;
  readonly graph: ViewerReagraphGraph | null;
  readonly truncated: boolean;
  readonly error: string | null;
  readonly loading: boolean;
}

const INITIAL_STATE: ViewerState = {
  meta: null,
  graph: null,
  truncated: false,
  error: null,
  loading: true,
};

function describe(error: unknown): string {
  if (error instanceof ApiError) return `${error.code}: ${error.message}`;
  if (error instanceof Error) return error.message;
  return "unknown error";
}

export function GraphPreview() {
  const [lod, setLod] = useState(1);
  const [rotate, setRotate] = useState(true);
  const [state, setState] = useState<ViewerState>(INITIAL_STATE);
  const [status, setStatus] = useState(ROTATE_STATUS);
  const worker = useRef<TileWorkerClient | null>(null);

  if (worker.current === null) {
    worker.current = createTileWorkerClient();
  }

  useEffect(() => {
    const client = worker.current;
    return () => client?.close();
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setState((previous) => ({ ...previous, loading: true, error: null }));
    (async () => {
      const meta = await fetchMeta(controller.signal);
      if (!meta.layout) {
        throw new ApiError(
          "NO_LAYOUT",
          200,
          "the published snapshot has no layout to render",
        );
      }
      const level = Math.min(lod, meta.layout.maxLod);
      // Reagraph builds an object per node, so a level is budgeted by what the
      // renderer stays interactive with, not by what the server can send.
      const maxNodes = Math.min(
        meta.layout.maxNodes,
        DEFAULT_REAGRAPH_NODE_LIMIT,
        LOD_NODE_BUDGET[level] ?? DEFAULT_REAGRAPH_NODE_LIMIT,
      );
      const view = await (worker.current as TileWorkerClient).load(
        {
          bounds: meta.layout,
          lod: level,
          maxNodes,
        },
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setState({
        meta,
        graph: {
          nodes: view.nodes,
          edges: view.edges,
          layoutOverrides: createLayoutOverrides(view.nodes),
        },
        truncated: view.truncated,
        error: null,
        loading: false,
      });
    })().catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setState({
        meta: null,
        graph: null,
        truncated: false,
        error: describe(error),
        loading: false,
      });
    });
    return () => controller.abort();
  }, [lod]);

  const graph = state.graph;
  const summary = useMemo(() => {
    if (state.loading) return "loading snapshot…";
    if (!graph) return "no graph";
    const counts = state.meta?.counts;
    const available = counts
      ? [counts.repositories, counts.packages, counts.files, counts.symbols][
          lod
        ]
      : undefined;
    // A truncated tile must say so: the view is a budgeted sample, not the
    // whole level.
    const scope =
      state.truncated && available !== undefined
        ? `${graph.nodes.length} of ${available} ${LOD_LABELS[lod]}`
        : `${graph.nodes.length} ${LOD_LABELS[lod]}`;
    return `${scope} · ${graph.edges.length} edges`;
  }, [graph, lod, state.loading, state.meta, state.truncated]);

  const updateStatus = (node: InternalGraphNode): void => {
    const data = node.data as ReagraphNodeData | undefined;
    const kind = LOD_LABELS[(data?.kind ?? 1) - 1] ?? "node";
    // The caption on the canvas is shortened; the readout is the full name.
    setStatus(`${data?.label ?? node.label ?? node.id} · ${kind}`);
  };

  return (
    <div
      className="relative h-full w-full bg-background"
      role="img"
      aria-label="Interactive Reagraph graph preview"
    >
      {graph ? (
        <GraphCanvas
          key={`${state.meta?.snapshotId ?? 0}-${lod}-${rotate ? "3d" : "2d"}`}
          nodes={graph.nodes}
          edges={graph.edges}
          theme={darkTheme}
          layoutType="custom"
          layoutOverrides={graph.layoutOverrides}
          animated={false}
          // Beyond a few hundred nodes every caption overlaps its neighbours;
          // the name stays reachable by hovering the node.
          labelType={graph.nodes.length <= LABEL_LIMIT ? "nodes" : "none"}
          // Rotating reads the depth the layout already has: each node kind
          // sits on its own plane, so a cluster that overlaps head-on
          // separates as soon as the camera turns.
          cameraMode={rotate ? "rotate" : "pan"}
          onNodePointerOver={updateStatus}
          onNodePointerOut={() =>
            setStatus(rotate ? ROTATE_STATUS : READY_STATUS)
          }
        />
      ) : null}
      <div className="pointer-events-none absolute inset-x-4 top-4 flex items-center justify-between gap-4 text-xs font-medium">
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          {state.error ? `error · ${state.error}` : summary}
        </span>
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          {status}
        </span>
      </div>
      <div className="pointer-events-none absolute bottom-4 left-4 flex flex-col gap-1.5 rounded-2xl border border-border/80 bg-background/85 px-3 py-2 text-[11px] text-muted-foreground backdrop-blur">
        {NODE_COLORS.map((entry, position) => (
          <span key={entry.kind} className="flex items-center gap-2">
            <span
              className="inline-block rounded-full"
              style={{
                backgroundColor: entry.color,
                width: LEGEND_DOT_SIZES[position],
                height: LEGEND_DOT_SIZES[position],
              }}
            />
            {LOD_LABELS[position]}
          </span>
        ))}
        <span className="mt-1 flex items-center gap-2">
          <span
            className="inline-block h-px w-4"
            style={{ backgroundColor: EXACT_DEPENDENCY_COLOR }}
          />
          exact dependency
        </span>
        <span className="flex items-center gap-2">
          <span
            className="inline-block h-px w-4"
            style={{ backgroundColor: DEPENDENCY_COLOR }}
          />
          dependency
        </span>
        <span className="flex items-center gap-2">
          <span
            className="inline-block h-px w-4 opacity-60"
            style={{ backgroundColor: CONTAINMENT_COLOR }}
          />
          contains
        </span>
      </div>
      <div className="pointer-events-auto absolute inset-x-4 bottom-4 flex items-center justify-center gap-2 text-xs">
        {LOD_LABELS.map((label, level) => (
          <button
            key={label}
            type="button"
            onClick={() => setLod(level)}
            className={`rounded-full border px-3 py-1 backdrop-blur transition-colors ${
              level === lod
                ? "border-primary/60 bg-primary/20 text-foreground"
                : "border-border/80 bg-background/85 text-muted-foreground hover:text-foreground"
            }`}
          >
            {label}
          </button>
        ))}
        <button
          type="button"
          onClick={() => {
            setRotate((previous) => {
              setStatus(previous ? READY_STATUS : ROTATE_STATUS);
              return !previous;
            });
          }}
          className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur transition-colors hover:text-foreground"
        >
          {rotate ? "3D" : "2D"}
        </button>
      </div>
    </div>
  );
}
