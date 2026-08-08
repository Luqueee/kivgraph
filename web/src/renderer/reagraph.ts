import type {
  GraphEdge,
  GraphNode,
  InternalGraphPosition,
  LayoutOverrides,
} from "reagraph";

import {
  GraphBinaryError,
  type GraphEdgeRecord,
  type GraphNodeRecord,
  type GraphPayload,
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
  readEdge,
  readNode,
  VIEWER_EDGE_FLAG_PACKAGE,
  VIEWER_FLAG_TRUNCATED,
} from "./binary";
import { computeStructuralLayout, type LayoutGraph } from "./layout";

/**
 * Most nodes the adapter will materialise. It matches the ceiling the tiles
 * endpoint enforces, so the limit that bites is the server's and not a second
 * one hidden here. The deepest level of a large snapshot needs the whole range:
 * the first symbol of `~/kena` is node 4.351, behind every repository, package
 * and file the tile carries first.
 */
export const DEFAULT_REAGRAPH_NODE_LIMIT = 10_000;
export const DEFAULT_REAGRAPH_EDGE_LIMIT = 8_000;

/** Colours the viewer legend explains; kept beside the palette they describe. */
export const CONTAINMENT_COLOR = "#475569";
export const LOCAL_DEPENDENCY_COLOR = "#4b5563";
export const CROSS_DEPENDENCY_COLOR = "#94a3b8";
export const EXACT_DEPENDENCY_COLOR = "#16a34a";

/**
 * How much of the containment colour a link keeps, by what it holds.
 *
 * There is one containment link per node, so at the deep levels they outnumber
 * everything else and every container turns into a starburst brighter than the
 * nodes it holds. A repository holding a package is structure worth drawing; a
 * file holding a symbol is texture, and texture belongs near the background.
 */
export function containmentPresence(childKind: number): number {
  switch (childKind) {
    case NODE_KIND_PACKAGE:
      return 0.78;
    case NODE_KIND_FILE:
      return 0.46;
    default:
      return 0.3;
  }
}
/** A dependency inside one cluster is context, not news. */
export const LOCAL_EDGE_SIZE = 0.5;
/** A dependency that leaves its cluster is the structure of the codebase. */
export const CROSS_EDGE_SIZE = 0.9;

/**
 * Drawn radius per rank in the hierarchy.
 *
 * The stock range is `5` to `15`, which makes a symbol two thirds of a
 * repository and leaves the picture flat: everything competes. Opening the
 * range is what lets a repository read as a landmark and a symbol as a speck.
 * Reagraph rescales the sizes it is given onto `[minNodeSize, maxNodeSize]`,
 * so the canvas is told these exact bounds and the mapping stays the identity
 * - the layout reserves space in the same units the renderer draws in.
 *
 * The small end does not shrink below four units. A deep tile puts thousands
 * of symbols in a world thousands of units wide, and anything smaller stops
 * being a dot and becomes nothing at all.
 */
export const NODE_SIZE_REPOSITORY = 22;
export const NODE_SIZE_HUB = 12;
export const NODE_SIZE_PACKAGE = 7;
export const NODE_SIZE_FILE = 5;
export const NODE_SIZE_SYMBOL = 4;

/**
 * Above this drawn size a node carries a permanent caption: repositories and
 * hubs, and nothing else.
 */
export const ALWAYS_LABELLED_SIZE = NODE_SIZE_PACKAGE;

/** Most a tile will ever call a hub, however many nodes it holds. */
const MAX_HUBS = 18;

/** Share of a tile eligible to be a hub before the cap applies. */
const HUB_SHARE = 0.06;

export const NODE_COLORS: ReadonlyArray<{ kind: number; color: string }> = [
  { kind: NODE_KIND_REPOSITORY, color: "#2563eb" },
  { kind: NODE_KIND_PACKAGE, color: "#7c3aed" },
  { kind: NODE_KIND_FILE, color: "#059669" },
  { kind: NODE_KIND_SYMBOL, color: "#ea580c" },
];

function endpointKey(kind: number, id: number): string {
  return `${kind}:${id}`;
}

/**
 * Stable 32-bit identity of a node, from what it is and not from where it
 * landed in this tile. The layout seeds its jitter with it, so the same
 * package keeps the same corner of the world across levels and reloads.
 */
function identityOf(kind: number, id: number): number {
  let value =
    (Math.imul(id, 0x85ebca6b) ^ Math.imul(kind + 1, 0xc2b2ae35)) >>> 0;
  value = Math.imul(value ^ (value >>> 15), 0x2545f491) >>> 0;
  return (value ^ (value >>> 13)) >>> 0;
}

// A caption competes for pixels with every neighbour, and module paths are
// long: `kena.bot/api-db-go/internal/domain/errors` covers a dozen nodes. The
// canvas shows the last two segments; the full name stays in the node data and
// in the hover readout, so nothing is lost.
function shortLabel(label: string): string {
  const segments = label.split("/").filter((segment) => segment !== "");
  const tail = segments.length > 2 ? segments.slice(-2).join("/") : label;
  return tail.length > 32 ? `${tail.slice(0, 31)}…` : tail;
}

export interface ReagraphNodeData {
  readonly index: number;
  readonly sourceId: number;
  readonly kind: number;
  readonly depth: number;
  /** Full name from the snapshot; the caption on the canvas is shortened. */
  readonly label: string;
  /** Cluster the node belongs to: its repository, or its orphan component. */
  readonly cluster: number;
  /** Community inside the cluster; siblings that depend on each other share one. */
  readonly community: number;
  /** Depth in the condensed dependency DAG. */
  readonly layer: number;
  /** Centrality in `[0, 1]` from PageRank over the tile's dependencies. */
  readonly importance: number;
  readonly x: number;
  readonly y: number;
  readonly z: number;
}

export interface ReagraphEdgeData {
  readonly index: number;
  readonly sourceIndex: number;
  readonly targetIndex: number;
  readonly evidence: number;
  readonly kind: number;
  readonly confidence: number;
  readonly provenance: number;
  readonly flags: number;
  /** True for the container-to-child link the payload carries per node. */
  readonly containment?: boolean;
  /** True when the two endpoints live in different clusters. */
  readonly crossCluster?: boolean;
}

export type ViewerReagraphNode = GraphNode & {
  readonly data: ReagraphNodeData;
};

export type ViewerReagraphEdge = GraphEdge & {
  readonly data: ReagraphEdgeData;
};

/**
 * What the layout found in the tile: enough for the viewer to describe the
 * drawing and to frame it without measuring the scene again.
 */
export interface ViewerGraphStats {
  /** Node count per kind, in repository, package, file, symbol order. */
  readonly nodesByKind: readonly [number, number, number, number];
  readonly clusterCount: number;
  readonly layerCount: number;
  readonly center: readonly [number, number, number];
  readonly boundingRadius: number;
  /** Standard deviation per axis; a near-zero entry means a flat drawing. */
  readonly spread: readonly [number, number, number];
}

/**
 * Container-to-child links, as node indices into the tile.
 *
 * One per node, so they are kept out of the edge list: nothing picks them and
 * the renderer draws them as a single mesh of line segments.
 */
export interface ContainmentLinks {
  readonly source: Uint32Array;
  readonly target: Uint32Array;
}

export interface ViewerReagraphGraph {
  readonly nodes: ViewerReagraphNode[];
  readonly edges: ViewerReagraphEdge[];
  readonly containment: ContainmentLinks;
  readonly layoutOverrides: LayoutOverrides;
  readonly stats: ViewerGraphStats;
}

/**
 * The part of a graph a worker can hand over: plain data, no closures. The
 * layout overrides are rebuilt on the receiving side with
 * `createLayoutOverrides`.
 */
export interface ViewerReagraphView {
  readonly nodes: ViewerReagraphNode[];
  readonly edges: ViewerReagraphEdge[];
  readonly containment: ContainmentLinks;
  readonly truncated: boolean;
  readonly snapshotId: number;
  readonly stats: ViewerGraphStats;
}

export interface ReagraphGraphLimits {
  readonly maxNodes?: number;
  readonly maxEdges?: number;
}

/**
 * Converts only a bounded binary view into the object shape required by
 * Reagraph. The transferred LGVB buffer remains owned by the caller and is
 * never stored in React state or copied into this graph model.
 *
 * Positions come from the structural layout, not from the published
 * coordinates: a tile is a sample of the world, and the grid the server packed
 * says nothing about which packages belong together once most of them are
 * missing. Clusters, dependency depth and communities are derived here from
 * the containment forest and the dependency graph the tile does carry.
 *
 * The result is plain data so a worker can post it across the thread
 * boundary; `createLayoutOverrides` rebuilds the callback on the other side.
 */
export function createReagraphView(
  payload: GraphPayload,
  limits: ReagraphGraphLimits = {},
): ViewerReagraphView {
  const maxNodes = limits.maxNodes ?? DEFAULT_REAGRAPH_NODE_LIMIT;
  const maxEdges = limits.maxEdges ?? DEFAULT_REAGRAPH_EDGE_LIMIT;

  if (!Number.isInteger(maxNodes) || maxNodes < 0) {
    throw new RangeError(`invalid Reagraph node limit ${maxNodes}`);
  }
  if (!Number.isInteger(maxEdges) || maxEdges < 0) {
    throw new RangeError(`invalid Reagraph edge limit ${maxEdges}`);
  }
  if (payload.header.nodeCount > maxNodes) {
    throw new GraphBinaryError(
      "REAGRAPH_NODE_LIMIT",
      `Reagraph preview supports at most ${maxNodes} nodes; request a smaller tile or neighborhood`,
    );
  }
  if (payload.header.edgeCount > maxEdges) {
    throw new GraphBinaryError(
      "REAGRAPH_EDGE_LIMIT",
      `Reagraph preview supports at most ${maxEdges} edges; request a smaller tile or neighborhood`,
    );
  }

  const nodeCount = payload.header.nodeCount;
  const records = new Array<GraphNodeRecord>(nodeCount);
  const kind = new Uint8Array(nodeCount);
  const identity = new Uint32Array(nodeCount);
  const parent = new Int32Array(nodeCount).fill(-1);
  const nodeIndexByKey = new Map<string, number>();
  for (let index = 0; index < nodeCount; index += 1) {
    const record = readNode(payload, index);
    records[index] = record;
    kind[index] = record.kind;
    identity[index] = identityOf(record.kind, record.id);
    nodeIndexByKey.set(endpointKey(record.kind, record.id), index);
  }
  for (let index = 0; index < nodeCount; index += 1) {
    const record = records[index];
    if (record.parentKind === 0) continue;
    const container = nodeIndexByKey.get(
      endpointKey(record.parentKind, record.parentId),
    );
    if (container !== undefined) parent[index] = container;
  }

  // Dense IDs are only unique per node kind, and edges carry them, not payload
  // indices. Package relations are flagged; everything else connects symbols.
  const edgeCount = payload.header.edgeCount;
  const edgeRecords = new Array<GraphEdgeRecord>(edgeCount);
  const edgeSource = new Int32Array(edgeCount);
  const edgeTarget = new Int32Array(edgeCount);
  const edgeWeight = new Float32Array(edgeCount);
  for (let index = 0; index < edgeCount; index += 1) {
    const record = readEdge(payload, index);
    edgeRecords[index] = record;
    const endpointKind =
      (record.flags & VIEWER_EDGE_FLAG_PACKAGE) !== 0
        ? NODE_KIND_PACKAGE
        : NODE_KIND_SYMBOL;
    const source = nodeIndexByKey.get(endpointKey(endpointKind, record.source));
    const target = nodeIndexByKey.get(endpointKey(endpointKind, record.target));
    if (source === undefined || target === undefined) {
      throw new GraphBinaryError(
        "INVALID_REFERENCES",
        `Reagraph edge ${index} references a node outside the payload`,
      );
    }
    edgeSource[index] = source;
    edgeTarget[index] = target;
    // An exact dependency is evidence; an inferred one is a hint. The layout
    // lets the first pull twice as hard.
    edgeWeight[index] = record.confidence >= 2 ? 2 : 1;
  }

  const graph: LayoutGraph = {
    nodeCount,
    kind,
    parent,
    identity,
    edgeSource,
    edgeTarget,
    edgeWeight,
  };
  // A share of the tile is drawn - and captioned - as a hub, with an absolute
  // cap so a large tile does not fill with competing captions. The layout
  // hands over each node's rank by importance, which is all the adapter needs
  // to decide without seeing the whole distribution.
  const hubs = Math.min(MAX_HUBS, Math.ceil(nodeCount * HUB_SHARE));
  const sizeOf = (nodeKind: number, importance: number, rank: number): number =>
    nodeSize(nodeKind, importance, rank < hubs);
  const layout = computeStructuralLayout(graph, sizeOf);

  const nodes: ViewerReagraphNode[] = [];
  const nodesByKind: [number, number, number, number] = [0, 0, 0, 0];
  for (let index = 0; index < nodeCount; index += 1) {
    const record = records[index];
    const label =
      payload.labels[index] ?? `${kindLabel(record.kind)} ${record.id}`;
    const importance = layout.importance[index];
    const data: ReagraphNodeData = {
      index,
      sourceId: record.id,
      kind: record.kind,
      depth: record.depth,
      label,
      cluster: layout.cluster[index],
      community: layout.community[index],
      layer: layout.layer[index],
      importance,
      x: layout.x[index],
      y: layout.y[index],
      z: layout.z[index],
    };
    if (record.kind >= 1 && record.kind <= 4) {
      nodesByKind[record.kind - 1] += 1;
    }
    const size = sizeOf(record.kind, importance, layout.rank[index]);
    nodes.push({
      id: `node-${index}`,
      // Only the nodes that carry the structure are captioned. Reagraph draws
      // every label it is given at a fixed size in world units, so on a tile
      // of a thousand nodes the rest would be a grey blur; the full name of
      // any node is one hover away.
      label: size > ALWAYS_LABELLED_SIZE ? shortLabel(label) : undefined,
      size,
      fill: nodeColor(record.kind),
      data,
    });
  }

  const edges: ViewerReagraphEdge[] = [];
  for (let index = 0; index < edgeCount; index += 1) {
    const record = edgeRecords[index];
    const source = edgeSource[index];
    const target = edgeTarget[index];
    const crossCluster = layout.cluster[source] !== layout.cluster[target];
    const exact = record.confidence >= 2;
    edges.push({
      id: `edge-${index}`,
      source: `node-${source}`,
      target: `node-${target}`,
      fill: exact
        ? EXACT_DEPENDENCY_COLOR
        : crossCluster
          ? CROSS_DEPENDENCY_COLOR
          : LOCAL_DEPENDENCY_COLOR,
      size: crossCluster || exact ? CROSS_EDGE_SIZE : LOCAL_EDGE_SIZE,
      // A straight line between two clusters cuts through everything in
      // between. Bowing them makes the ones that share a direction read as one
      // channel, which is as close to bundling as the renderer allows.
      interpolation: crossCluster ? "curved" : "linear",
      dashed: false,
      arrowPlacement: "none",
      data: {
        index,
        sourceIndex: record.source,
        targetIndex: record.target,
        evidence: record.evidence,
        kind: record.kind,
        confidence: record.confidence,
        provenance: record.provenance,
        flags: record.flags,
        crossCluster,
      },
    });
  }

  // Every node carries the container it belongs to. Without those links a
  // repository floats next to its own packages with nothing joining them, and
  // the picture claims a disconnection the graph does not have. They travel as
  // plain index pairs rather than as edges: there is one per node, nothing
  // ever picks them, and as edges they would quadruple the geometry the
  // renderer rebuilds every time the highlight moves.
  let containmentCount = 0;
  for (let index = 0; index < nodeCount; index += 1) {
    if (parent[index] >= 0) containmentCount += 1;
  }
  const containment: ContainmentLinks = {
    source: new Uint32Array(containmentCount),
    target: new Uint32Array(containmentCount),
  };
  let written = 0;
  for (let index = 0; index < nodeCount; index += 1) {
    const container = parent[index];
    if (container < 0) continue;
    containment.source[written] = container;
    containment.target[written] = index;
    written += 1;
  }

  return {
    nodes,
    edges,
    containment,
    truncated: (payload.header.flags & VIEWER_FLAG_TRUNCATED) !== 0,
    snapshotId: Number(payload.header.snapshotId),
    stats: {
      nodesByKind,
      clusterCount: layout.clusterCount,
      layerCount: layout.layerCount,
      center: layout.center,
      boundingRadius: layout.boundingRadius,
      spread: layout.spread,
    },
  };
}

/** Convenience for callers that adapt and render on the same thread. */
export function createReagraphGraph(
  payload: GraphPayload,
  limits: ReagraphGraphLimits = {},
): ViewerReagraphGraph {
  const view = createReagraphView(payload, limits);
  return {
    nodes: view.nodes,
    edges: view.edges,
    containment: view.containment,
    layoutOverrides: createLayoutOverrides(view.nodes),
    stats: view.stats,
  };
}

/**
 * Reagraph asks for a node's position by id, once per node and again on every
 * relayout. Scanning the node array per call is quadratic — with a 2.000 node
 * tile that is four million comparisons before the first frame — so positions
 * are resolved through a map built once.
 */
export function createLayoutOverrides(
  nodes: readonly ViewerReagraphNode[],
): LayoutOverrides {
  const positions = new Map<string, ReagraphNodeData>();
  for (const node of nodes) {
    positions.set(node.id, node.data);
  }
  return {
    getNodePosition: (id: string): InternalGraphPosition => {
      const data = positions.get(id);
      if (!data) {
        throw new GraphBinaryError(
          "LAYOUT_NODE_NOT_FOUND",
          `Reagraph layout cannot find node ${id}`,
        );
      }
      return {
        id,
        data,
        links: [],
        index: data.index,
        vx: 0,
        vy: 0,
        x: data.x,
        y: data.y,
        z: data.z,
      };
    },
  };
}

function kindLabel(kind: number): string {
  switch (kind) {
    case NODE_KIND_REPOSITORY:
      return "repository";
    case NODE_KIND_PACKAGE:
      return "package";
    case NODE_KIND_FILE:
      return "file";
    case NODE_KIND_SYMBOL:
      return "symbol";
    default:
      return `kind-${kind}`;
  }
}

/**
 * Drawn radius, and with it label priority.
 *
 * Repositories anchor the picture and are always named. A hub is named because
 * the reader is looking for it. Everything else stays under the threshold and
 * gives up its caption until the camera comes close.
 */
function nodeSize(kind: number, importance: number, hub: boolean): number {
  if (kind === NODE_KIND_REPOSITORY) return NODE_SIZE_REPOSITORY;
  if (hub && importance > 0) return NODE_SIZE_HUB;
  switch (kind) {
    case NODE_KIND_PACKAGE:
      return NODE_SIZE_PACKAGE;
    case NODE_KIND_FILE:
      return NODE_SIZE_FILE;
    default:
      return NODE_SIZE_SYMBOL;
  }
}

function nodeColor(kind: number): string {
  return NODE_COLORS.find((entry) => entry.kind === kind)?.color ?? "#64748b";
}
