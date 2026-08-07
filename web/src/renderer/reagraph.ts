import type {
  GraphEdge,
  GraphNode,
  InternalGraphPosition,
  LayoutOverrides,
  NodePositionArgs,
} from "reagraph";

import {
  GraphBinaryError,
  type GraphPayload,
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
  readCoordinateBounds,
  readEdge,
  readNode,
} from "./binary";

export const DEFAULT_REAGRAPH_NODE_LIMIT = 2_000;
export const DEFAULT_REAGRAPH_EDGE_LIMIT = 8_000;
export const REAGRAPH_WORLD_SIZE = 800;

export interface ReagraphNodeData {
  readonly index: number;
  readonly sourceId: number;
  readonly kind: number;
  readonly depth: number;
  readonly x: number;
  readonly y: number;
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

export interface ReagraphGraphLimits {
  readonly maxNodes?: number;
  readonly maxEdges?: number;
}

/**
 * Converts only a bounded binary view into the object shape required by
 * Reagraph. The transferred LGVB buffer remains owned by the caller and is
 * never stored in React state or copied into this graph model.
 */
export function createReagraphGraph(
  payload: GraphPayload,
  limits: ReagraphGraphLimits = {},
): ViewerReagraphGraph {
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

  const nodeIds = new Array<string>(payload.header.nodeCount);
  const nodes: ViewerReagraphNode[] = [];
  const occupiedCenters = new Map<string, number>();
  for (let index = 0; index < payload.header.nodeCount; index += 1) {
    const record = readNode(payload, index);
    const rawX = centerCoordinate(record.minX, record.maxX, bounds.minX, spanX);
    const rawY = centerCoordinate(record.minY, record.maxY, bounds.minY, spanY);
    const { x, y } = disambiguateCenter(rawX, rawY, occupiedCenters);
    const data: ReagraphNodeData = {
      index,
      sourceId: record.id,
      kind: record.kind,
      depth: record.depth,
      x,
      y,
    };
    const id = `node-${index}`;
    nodeIds[index] = id;
    nodes.push({
      id,
      label: `${kindLabel(record.kind)} ${record.id}`,
      labelVisible: true,
      size: nodeSize(record.kind),
      fill: nodeColor(record.kind),
      data,
    });
  }

  const edges: ViewerReagraphEdge[] = [];
  for (let index = 0; index < payload.header.edgeCount; index += 1) {
    const record = readEdge(payload, index);
    if (record.source >= nodeIds.length || record.target >= nodeIds.length) {
      throw new GraphBinaryError(
        "INVALID_REFERENCES",
        `Reagraph edge ${index} references a node outside the payload`,
      );
    }
    edges.push({
      id: `edge-${index}`,
      source: nodeIds[record.source],
      target: nodeIds[record.target],
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

  return {
    nodes,
    edges,
    layoutOverrides: createLayoutOverrides(),
  };
}

function createLayoutOverrides(): LayoutOverrides {
  return {
    getNodePosition: (
      id: string,
      args: NodePositionArgs,
    ): InternalGraphPosition => {
      const node = args.nodes.find((candidate) => candidate.id === id);
      if (!node) {
        throw new GraphBinaryError(
          "LAYOUT_NODE_NOT_FOUND",
          `Reagraph layout cannot find node ${id}`,
        );
      }
      const data = node.data as ReagraphNodeData;
      return {
        id,
        data,
        links: [],
        index: data.index,
        vx: 0,
        vy: 0,
        x: data.x,
        y: data.y,
        z: 0,
      };
    },
  };
}

function centerCoordinate(
  minimum: bigint,
  maximum: bigint,
  origin: bigint,
  span: number,
): number {
  const center = (Number(minimum - origin) + Number(maximum - origin)) / 2;
  return (center / span - 0.5) * REAGRAPH_WORLD_SIZE;
}

function disambiguateCenter(
  x: number,
  y: number,
  occupiedCenters: Map<string, number>,
): { x: number; y: number } {
  const key = `${x}:${y}`;
  const occurrence = occupiedCenters.get(key) ?? 0;
  occupiedCenters.set(key, occurrence + 1);
  if (occurrence === 0) return { x, y };
  return {
    x: x + occurrence * 80,
    y: y + (occurrence % 2 === 0 ? 40 : -40),
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
  switch (kind) {
    case NODE_KIND_REPOSITORY:
      return "#2563eb";
    case NODE_KIND_PACKAGE:
      return "#7c3aed";
    case NODE_KIND_FILE:
      return "#059669";
    case NODE_KIND_SYMBOL:
      return "#ea580c";
    default:
      return "#64748b";
  }
}

function edgeColor(confidence: number): string {
  return confidence >= 2 ? "#16a34a" : "#94a3b8";
}
