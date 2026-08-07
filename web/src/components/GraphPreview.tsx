import { useEffect, useMemo, useState } from "react";
import { GraphCanvas, darkTheme } from "reagraph";
import type { InternalGraphNode } from "reagraph";

import {
  ApiError,
  fetchMeta,
  fetchTile,
  type SnapshotMeta,
} from "@/api/client";
import { decodeGraphPayload } from "@/renderer/binary";
import {
  createReagraphGraph,
  DEFAULT_REAGRAPH_NODE_LIMIT,
  type ReagraphNodeData,
  type ViewerReagraphGraph,
} from "@/renderer/reagraph";

const READY_STATUS = "drag to pan · wheel to zoom";

/** Detail levels of the published layout, from repositories to symbols. */
const LOD_LABELS = ["repositories", "packages", "files", "symbols"] as const;

/** Above this node count captions overlap; names move to hover only. */
const LABEL_LIMIT = 200;

interface ViewerState {
  readonly meta: SnapshotMeta | null;
  readonly graph: ViewerReagraphGraph | null;
  readonly error: string | null;
  readonly loading: boolean;
}

const INITIAL_STATE: ViewerState = {
  meta: null,
  graph: null,
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
  const [state, setState] = useState<ViewerState>(INITIAL_STATE);
  const [status, setStatus] = useState(READY_STATUS);

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
      // Never ask for more nodes than the adapter will materialise: the
      // server would happily send a tile this view then has to reject.
      const buffer = await fetchTile(
        {
          bounds: meta.layout,
          lod: Math.min(lod, meta.layout.maxLod),
          maxNodes: Math.min(meta.layout.maxNodes, DEFAULT_REAGRAPH_NODE_LIMIT),
        },
        controller.signal,
      );
      const graph = createReagraphGraph(decodeGraphPayload(buffer));
      setState({ meta, graph, error: null, loading: false });
    })().catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setState({
        meta: null,
        graph: null,
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
    return `${graph.nodes.length} nodes · ${graph.edges.length} edges${
      counts ? ` · snapshot ${counts.symbols} symbols` : ""
    }`;
  }, [graph, state.loading, state.meta]);

  const updateStatus = (node: InternalGraphNode): void => {
    const data = node.data as ReagraphNodeData | undefined;
    const kind = LOD_LABELS[(data?.kind ?? 1) - 1] ?? "node";
    setStatus(
      `${node.label ?? node.id} · ${kind} · id ${data?.sourceId ?? "?"}`,
    );
  };

  return (
    <div
      className="relative h-full w-full bg-background"
      role="img"
      aria-label="Interactive Reagraph graph preview"
    >
      {graph ? (
        <GraphCanvas
          key={`${state.meta?.snapshotId ?? 0}-${lod}`}
          nodes={graph.nodes}
          edges={graph.edges}
          theme={darkTheme}
          layoutType="custom"
          layoutOverrides={graph.layoutOverrides}
          animated={false}
          // Beyond a few hundred nodes every caption overlaps its neighbours;
          // the name stays reachable by hovering the node.
          labelType={graph.nodes.length <= LABEL_LIMIT ? "nodes" : "none"}
          cameraMode="pan"
          onNodePointerOver={updateStatus}
          onNodePointerOut={() => setStatus(READY_STATUS)}
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
      </div>
    </div>
  );
}
