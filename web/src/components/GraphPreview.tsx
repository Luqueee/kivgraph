import { useEffect, useMemo, useRef, useState } from "react";
import { GraphCanvas, darkTheme } from "reagraph";
import type { GraphCanvasRef, InternalGraphNode } from "reagraph";

import { ApiError, fetchMeta, type SnapshotMeta } from "@/api/client";
import { useFrameRate } from "@/hooks/useFrameRate";
import { frameGraph } from "@/renderer/camera";
import {
  MAX_TILE_BUDGET,
  MIN_TILE_BUDGET,
  TILE_BUDGET_STEP,
  tileBudget,
} from "@/renderer/budget";
import {
  CONTAINMENT_COLOR,
  createLayoutOverrides,
  CROSS_DEPENDENCY_COLOR,
  EXACT_DEPENDENCY_COLOR,
  LOCAL_DEPENDENCY_COLOR,
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
 * Nodes requested until the reader moves the slider. Reagraph builds an object
 * per node, so the whole of a deep level does not fit in a reasonable frame;
 * this is the measured point where the view still appears in about a second.
 */
const DEFAULT_TILE_BUDGET = 1_200;

/**
 * Opening camera angles, in radians.
 *
 * Looking straight down an axis hides the depth the layout just built, so the
 * first frame is already off-axis in both directions: the world reads as a
 * volume before the reader touches anything.
 */
const CAMERA_AZIMUTH = 0.62;
const CAMERA_POLAR = Math.PI / 2 - 0.42;

/** Slack around the content: the graph fills about four fifths of the frame. */
const CAMERA_MARGIN = 1.2;

/** Share of the nodes the opening view must contain; outliers are a pan away. */
const CAMERA_QUANTILE = 0.97;

/** Stable empty selection: a fresh array on every hover would re-render. */
const NOTHING_ACTIVE: readonly string[] = [];

/**
 * How long the cursor has to rest on a node before the highlight is applied.
 * Reagraph rebuilds every edge mesh when the active set changes - a second of
 * work on a tile with a thousand edges - so a cursor crossing the graph must
 * not queue one rebuild per node it grazes.
 */
const HOVER_SETTLE_MS = 120;

/**
 * Hovering has to answer "what does this touch?" at a glance. The stock dark
 * theme fades the rest to a fifth, which on a tile of a thousand faint nodes
 * is not a change a reader notices; the neighbourhood has to be the only thing
 * left with any weight.
 */
const VIEWER_THEME = {
  ...darkTheme,
  node: { ...darkTheme.node, inactiveOpacity: 0.18 },
  edge: { ...darkTheme.edge, inactiveOpacity: 0.04 },
};

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
  const [requestedBudget, setRequestedBudget] = useState(DEFAULT_TILE_BUDGET);
  const [appliedBudget, setAppliedBudget] = useState(DEFAULT_TILE_BUDGET);
  const [state, setState] = useState<ViewerState>(INITIAL_STATE);
  const [status, setStatus] = useState(ROTATE_STATUS);
  const [actives, setActives] = useState<readonly string[]>(NOTHING_ACTIVE);
  const [selected, setSelected] = useState<readonly string[]>(NOTHING_ACTIVE);
  const fps = useFrameRate();
  const worker = useRef<TileWorkerClient | null>(null);
  const canvas = useRef<GraphCanvasRef | null>(null);
  const highlight = useRef<number | null>(null);
  const hovered = useRef<string | null>(null);

  if (worker.current === null) {
    worker.current = createTileWorkerClient();
  }

  useEffect(() => {
    const client = worker.current;
    return () => {
      client?.close();
      if (highlight.current !== null) window.clearTimeout(highlight.current);
    };
  }, []);

  // Dragging the slider must not fire a tile per pixel: only the value the
  // user rests on is fetched.
  useEffect(() => {
    const timer = setTimeout(() => setAppliedBudget(requestedBudget), 250);
    return () => clearTimeout(timer);
  }, [requestedBudget]);

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
      const view = await (worker.current as TileWorkerClient).load(
        {
          bounds: meta.layout,
          lod: level,
          maxNodes: tileBudget(appliedBudget, meta.layout.maxNodes),
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
          stats: view.stats,
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
  }, [lod, appliedBudget]);

  const graph = state.graph;

  // What the tile actually holds, not what the level is called: a budgeted
  // tile of `symbols` may be entirely repositories, packages and files, and
  // saying "symbols" would be a lie the picture contradicts.
  const summary = useMemo(() => {
    if (state.loading) return "loading snapshot…";
    if (!graph) return "no graph";
    const present = graph.stats.nodesByKind
      .map((count, position) => ({ count, label: LOD_LABELS[position] }))
      .filter((entry) => entry.count > 0)
      .map((entry) => `${entry.count} ${entry.label}`);
    const counts = state.meta?.counts;
    const available = counts
      ? [counts.repositories, counts.packages, counts.files, counts.symbols][
          lod
        ]
      : undefined;
    const drawn = `${present.join(" · ")} · ${graph.stats.clusterCount} clusters · ${graph.edges.length} edges`;
    // The budget bites before the deepest level is reached, so a tile asked
    // for `symbols` can hold none. Naming what is drawn and what it is a
    // sample of keeps the readout from claiming either.
    return state.truncated && available !== undefined
      ? `${drawn} · sample of ${available} ${LOD_LABELS[lod]}`
      : drawn;
  }, [graph, lod, state.loading, state.meta, state.truncated]);

  // The camera frames whatever the layout produced: thirty repositories and
  // two thousand symbols do not share a distance, and no constant fits both.
  // Reagraph's own fit would snap the camera to the closest axis - it treats a
  // `custom` layout as two-dimensional - and opening off-axis is the whole
  // point of a layout with depth in it.
  useEffect(() => {
    if (!graph) return;
    const points = graph.nodes.map((node) => node.data);
    const largest = graph.nodes.reduce(
      (widest, node) => Math.max(widest, node.size ?? 0),
      0,
    );
    let frame = 0;
    let applied = 0;
    const place = () => {
      const controls = canvas.current?.getControls();
      const camera = controls?.camera;
      if (controls && camera && "isPerspectiveCamera" in camera) {
        const { position } = frameGraph({
          points,
          center: graph.stats.center,
          azimuth: rotate ? CAMERA_AZIMUTH : 0,
          polar: rotate ? CAMERA_POLAR : Math.PI / 2,
          fov: camera.fov,
          aspect: camera.aspect,
          margin: CAMERA_MARGIN,
          quantile: CAMERA_QUANTILE,
          padding: largest * 2,
        });
        void controls.setLookAt(
          position[0],
          position[1],
          position[2],
          graph.stats.center[0],
          graph.stats.center[1],
          graph.stats.center[2],
          false,
        );
        applied += 1;
      }
      // Reagraph centres and fits once on mount, asynchronously and after this
      // effect runs. Re-applying for a few frames wins that race; the reader
      // cannot have touched the camera yet.
      if (applied < 8) frame = requestAnimationFrame(place);
    };
    frame = requestAnimationFrame(place);
    return () => cancelAnimationFrame(frame);
  }, [graph, rotate]);

  // Which edges and neighbours light up when the cursor lands on a node.
  // Built once per graph: doing it per hover would walk every edge on every
  // pointer move, and a tile carries thousands.
  const neighbourhood = useMemo(() => {
    const map = new Map<string, string[]>();
    const attach = (node: string, related: string): void => {
      const bucket = map.get(node);
      if (bucket === undefined) map.set(node, [related]);
      else bucket.push(related);
    };
    for (const edge of graph?.edges ?? []) {
      attach(edge.source, edge.id);
      attach(edge.source, edge.target);
      attach(edge.target, edge.id);
      attach(edge.target, edge.source);
    }
    return map;
  }, [graph]);

  // Both edges of the hover go through the same timer. Entering and leaving
  // arrive interleaved when the cursor crosses from one node to the next, and
  // a highlight applied on every one of them would flash; waiting for the
  // cursor to settle also spares Reagraph a full edge-mesh rebuild per node
  // grazed on the way - about a second on a tile with a thousand edges.
  const settle = (apply: () => void): void => {
    if (highlight.current !== null) window.clearTimeout(highlight.current);
    highlight.current = window.setTimeout(apply, HOVER_SETTLE_MS);
  };

  const enterNode = (node: InternalGraphNode): void => {
    const data = node.data as ReagraphNodeData | undefined;
    const kind = LOD_LABELS[(data?.kind ?? 1) - 1] ?? "node";
    hovered.current = node.id;
    // The caption on the canvas is shortened; the readout is the full name.
    setStatus(`${data?.label ?? node.label ?? node.id} · ${kind}`);
    settle(() => {
      // Reagraph only dims the rest of the graph when something is selected,
      // so the node under the cursor is the selection and everything it
      // touches is active. Without the selection the highlight would light one
      // node and leave the other thousand as bright as before.
      setSelected([node.id]);
      setActives(neighbourhood.get(node.id) ?? NOTHING_ACTIVE);
    });
  };

  const clearHighlight = (): void => {
    hovered.current = null;
    setStatus(rotate ? ROTATE_STATUS : READY_STATUS);
    setActives(NOTHING_ACTIVE);
    setSelected(NOTHING_ACTIVE);
  };

  // A pointer-out for a node the cursor already left behind is stale: moving
  // between neighbours delivers the two events in either order.
  const leaveNode = (node: InternalGraphNode): void => {
    if (hovered.current !== node.id) return;
    settle(clearHighlight);
  };

  const leaveCanvas = (): void => {
    if (highlight.current !== null) window.clearTimeout(highlight.current);
    highlight.current = null;
    clearHighlight();
  };

  return (
    <div
      className="relative h-full w-full bg-background"
      role="img"
      aria-label="Interactive Reagraph graph preview"
      onPointerLeave={leaveCanvas}
    >
      {graph ? (
        <GraphCanvas
          ref={canvas}
          key={`${state.meta?.snapshotId ?? 0}-${lod}-${rotate ? "3d" : "2d"}`}
          nodes={graph.nodes}
          edges={graph.edges}
          theme={VIEWER_THEME}
          layoutType="custom"
          layoutOverrides={graph.layoutOverrides}
          animated={false}
          // The adapter decides which nodes carry a caption at all - only
          // repositories and hubs do - so the canvas draws every label it is
          // given. Reagraph's own `auto` mode is evaluated once at mount and
          // never again, which on a camera that moves means captions that
          // never come back.
          labelType="nodes"
          // A tile of thirty repositories is a small world; the stock floor of
          // 1.000 units would keep the camera outside it.
          minDistance={40}
          // The layout is a volume, not a sheet: rotating separates clusters
          // that overlap head-on and shows which ones sit in front.
          cameraMode={rotate ? "rotate" : "pan"}
          // Hovering a node lights it, its edges and its neighbours, and dims
          // everything else to the theme's inactive opacity. On a tile with a
          // thousand edges that is the only way to read where one of them goes.
          actives={actives as string[]}
          selections={selected as string[]}
          onNodePointerOver={enterNode}
          onNodePointerOut={leaveNode}
        />
      ) : null}
      <div className="pointer-events-none absolute inset-x-4 top-4 flex items-center justify-between gap-4 text-xs font-medium">
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          {state.error ? `error · ${state.error}` : summary}
        </span>
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          {fps} fps · {status}
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
            style={{ backgroundColor: CROSS_DEPENDENCY_COLOR }}
          />
          cross-cluster dependency
        </span>
        <span className="flex items-center gap-2">
          <span
            className="inline-block h-px w-4"
            style={{ backgroundColor: LOCAL_DEPENDENCY_COLOR }}
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
        <label className="ml-2 flex items-center gap-2 rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          <span>nodes</span>
          <input
            type="range"
            min={MIN_TILE_BUDGET}
            max={MAX_TILE_BUDGET}
            step={TILE_BUDGET_STEP}
            value={requestedBudget}
            onChange={(event) =>
              setRequestedBudget(Number(event.currentTarget.value))
            }
            className="h-1 w-40 cursor-pointer accent-primary"
            aria-label="nodes per view"
          />
          <span className="w-12 text-right tabular-nums text-foreground">
            {requestedBudget}
          </span>
        </label>
      </div>
    </div>
  );
}
