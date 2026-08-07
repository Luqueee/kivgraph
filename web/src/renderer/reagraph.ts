import type {
  GraphEdge,
  GraphNode,
  InternalGraphPosition,
  LayoutOverrides,
} from "reagraph";

import {
  GraphBinaryError,
  type GraphNodeRecord,
  type GraphPayload,
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
  readCoordinateBounds,
  readEdge,
  readNode,
  VIEWER_EDGE_FLAG_PACKAGE,
  VIEWER_FLAG_TRUNCATED,
} from "./binary";

export const DEFAULT_REAGRAPH_NODE_LIMIT = 2_000;
export const DEFAULT_REAGRAPH_EDGE_LIMIT = 8_000;
export const REAGRAPH_WORLD_SIZE = 800;

/** Colours the viewer legend explains; kept beside the palette they describe. */
export const CONTAINMENT_COLOR = "#475569";
export const DEPENDENCY_COLOR = "#94a3b8";
export const EXACT_DEPENDENCY_COLOR = "#16a34a";

/** Containment reads as a thin hairline; dashes cost a curve per dash. */
export const CONTAINMENT_EDGE_SIZE = 0.4;

export const NODE_COLORS: ReadonlyArray<{ kind: number; color: string }> = [
  { kind: NODE_KIND_REPOSITORY, color: "#2563eb" },
  { kind: NODE_KIND_PACKAGE, color: "#7c3aed" },
  { kind: NODE_KIND_FILE, color: "#059669" },
  { kind: NODE_KIND_SYMBOL, color: "#ea580c" },
];

function endpointKey(kind: number, id: number): string {
  return `${kind}:${id}`;
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
  /** Full name from the snapshot; the caption is shortened for the canvas. */
  readonly label: string;
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
}

export type ViewerReagraphNode = GraphNode & {
  readonly data: ReagraphNodeData;
};

export type ViewerReagraphEdge = GraphEdge & {
  readonly data: ReagraphEdgeData;
};

export interface ViewerReagraphGraph {
  readonly nodes: ViewerReagraphNode[];
  readonly edges: ViewerReagraphEdge[];
  readonly layoutOverrides: LayoutOverrides;
}

/**
 * The part of a graph a worker can hand over: plain data, no closures. The
 * layout overrides are rebuilt on the receiving side with
 * `createLayoutOverrides`.
 */
export interface ViewerReagraphView {
  readonly nodes: ViewerReagraphNode[];
  readonly edges: ViewerReagraphEdge[];
  readonly truncated: boolean;
  readonly snapshotId: number;
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

  const bounds = readCoordinateBounds(payload);
  const spanX = Number(bounds.maxX - bounds.minX);
  const spanY = Number(bounds.maxY - bounds.minY);
  if (!Number.isFinite(spanX) || !Number.isFinite(spanY)) {
    throw new GraphBinaryError(
      "INVALID_BOUNDS",
      "Reagraph coordinates exceed the supported numeric range",
    );
  }

  // The world grows with the node count: a fixed extent turns a 2.000 node
  // tile into an unreadable clump.
  const worldSize = Math.max(
    REAGRAPH_WORLD_SIZE,
    Math.round(Math.sqrt(payload.header.nodeCount) * 60),
  );

  const records = new Array<GraphNodeRecord>(payload.header.nodeCount);
  const rawX = new Array<number>(payload.header.nodeCount);
  const rawY = new Array<number>(payload.header.nodeCount);
  for (let index = 0; index < payload.header.nodeCount; index += 1) {
    const record = readNode(payload, index);
    records[index] = record;
    rawX[index] = centerOffset(record.minX, record.maxX, bounds.minX);
    rawY[index] = centerOffset(record.minY, record.maxY, bounds.minY);
  }
  const columns = rankAxis(rawX, worldSize);
  const rows = rankAxis(rawY, worldSize);

  // Dense IDs are only unique per node kind, and edges carry them, not payload
  // indices. Package relations are flagged; everything else connects symbols.
  const nodeIdsByKind = new Map<string, string>();
  const nodes: ViewerReagraphNode[] = [];
  const occupiedCenters = new Map<string, number>();
  for (let index = 0; index < payload.header.nodeCount; index += 1) {
    const record = records[index];
    const { x, y, z } = spreadCenter(
      columns[index],
      rows[index],
      kindPlane(record.kind, worldSize),
      occupiedCenters,
      worldSize,
    );
    const label =
      payload.labels[index] ?? `${kindLabel(record.kind)} ${record.id}`;
    const data: ReagraphNodeData = {
      index,
      sourceId: record.id,
      kind: record.kind,
      depth: record.depth,
      label,
      x,
      y,
      z,
    };
    const id = `node-${index}`;
    nodeIdsByKind.set(endpointKey(record.kind, record.id), id);
    nodes.push({
      id,
      label: shortLabel(label),
      labelVisible: true,
      size: nodeSize(record.kind),
      fill: nodeColor(record.kind),
      data,
    });
  }

  const edges: ViewerReagraphEdge[] = [];
  for (let index = 0; index < payload.header.edgeCount; index += 1) {
    const record = readEdge(payload, index);
    const endpointKind =
      (record.flags & VIEWER_EDGE_FLAG_PACKAGE) !== 0
        ? NODE_KIND_PACKAGE
        : NODE_KIND_SYMBOL;
    const source = nodeIdsByKind.get(endpointKey(endpointKind, record.source));
    const target = nodeIdsByKind.get(endpointKey(endpointKind, record.target));
    if (source === undefined || target === undefined) {
      throw new GraphBinaryError(
        "INVALID_REFERENCES",
        `Reagraph edge ${index} references a node outside the payload`,
      );
    }
    edges.push({
      id: `edge-${index}`,
      source,
      target,
      fill: edgeColor(record.confidence),
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
      },
    });
  }

  // Every node carries the container it belongs to. Without those edges a
  // repository floats next to its own packages with nothing joining them, and
  // the picture claims a disconnection the graph does not have. Only drawn
  // when the container is part of this tile.
  for (let index = 0; index < nodes.length; index += 1) {
    const record = records[index];
    if (record.parentKind === 0) continue;
    const parent = nodeIdsByKind.get(
      endpointKey(record.parentKind, record.parentId),
    );
    if (parent === undefined) continue;
    // Solid and thin, never dashed: Reagraph builds a Catmull-Rom curve and a
    // tube per dash, so one dashed containment edge per node costs more than
    // every dependency in the tile put together.
    edges.push({
      id: `contains-${index}`,
      source: parent,
      target: nodes[index].id,
      fill: CONTAINMENT_COLOR,
      size: CONTAINMENT_EDGE_SIZE,
      dashed: false,
      arrowPlacement: "none",
      data: {
        index,
        sourceIndex: record.parentId,
        targetIndex: record.id,
        evidence: 0,
        kind: 0,
        confidence: 0,
        provenance: 0,
        flags: 0,
        containment: true,
      },
    });
  }

  return {
    nodes,
    edges,
    truncated: (payload.header.flags & VIEWER_FLAG_TRUNCATED) !== 0,
    snapshotId: Number(payload.header.snapshotId),
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
    layoutOverrides: createLayoutOverrides(view.nodes),
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

function centerOffset(
  minimum: bigint,
  maximum: bigint,
  origin: bigint,
): number {
  return (Number(minimum - origin) + Number(maximum - origin)) / 2;
}

/**
 * Spreads one axis by rank instead of by absolute coordinate.
 *
 * The published layout packs a repository's packages inside its own box, so a
 * linear projection of the whole world squeezes forty packages into a blob
 * while half the canvas stays empty. Ranking keeps the layout's order and its
 * columns — nodes that share a coordinate still share a slot — and gives every
 * distinct position the same room.
 */
function rankAxis(values: readonly number[], worldSize: number): number[] {
  const unique = [...new Set(values)].sort((left, right) => left - right);
  const slots = new Map(unique.map((value, index) => [value, index]));
  const divisor = Math.max(unique.length - 1, 1);
  return values.map((value) => {
    const slot = slots.get(value) ?? 0;
    return (slot / divisor - 0.5) * worldSize;
  });
}

// Each node kind gets its own plane: repositories at the front, symbols at the
// back. The published layout nests them inside one another, so on a flat
// projection a container and its children land on the same pixels.
function kindPlane(kind: number, worldSize: number): number {
  const step = worldSize / 6;
  switch (kind) {
    case NODE_KIND_REPOSITORY:
      return step * 1.5;
    case NODE_KIND_PACKAGE:
      return step * 0.5;
    case NODE_KIND_FILE:
      return -step * 0.5;
    case NODE_KIND_SYMBOL:
      return -step * 1.5;
    default:
      return 0;
  }
}

// Layout containers place children on a grid, so distinct nodes can share a
// centre once coordinates are projected. Collisions spread along a fixed
// spiral and step away in depth: deterministic, and no two nodes end up under
// one label.
function spreadCenter(
  x: number,
  y: number,
  z: number,
  occupiedCenters: Map<string, number>,
  worldSize: number,
): { x: number; y: number; z: number } {
  const key = `${Math.round(x)}:${Math.round(y)}:${Math.round(z)}`;
  const occurrence = occupiedCenters.get(key) ?? 0;
  occupiedCenters.set(key, occurrence + 1);
  if (occurrence === 0) return { x, y, z };
  const angle = occurrence * 2.399963229728653;
  const radius = (worldSize / 24) * Math.sqrt(occurrence);
  return {
    x: x + radius * Math.cos(angle),
    y: y + radius * Math.sin(angle),
    z: z + (worldSize / 48) * occurrence,
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

function nodeSize(kind: number): number {
  switch (kind) {
    case NODE_KIND_REPOSITORY:
      return 15;
    case NODE_KIND_PACKAGE:
      return 12;
    case NODE_KIND_FILE:
      return 10;
    default:
      return 7;
  }
}

function nodeColor(kind: number): string {
  return NODE_COLORS.find((entry) => entry.kind === kind)?.color ?? "#64748b";
}

function edgeColor(confidence: number): string {
  return confidence >= 2 ? EXACT_DEPENDENCY_COLOR : DEPENDENCY_COLOR;
}
