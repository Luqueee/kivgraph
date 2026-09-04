import type { ELK, ElkExtendedEdge, ElkNode } from "elkjs/lib/elk-api.js";
import elkWorkerUrl from "elkjs/lib/elk-worker.min.js?url";

export type TopologyLayoutNodeKind =
  | "profile"
  | "worktree"
  | "repository"
  | "shared_input"
  | "unknown";

export interface TopologyLayoutInputNode {
  readonly id: string;
  readonly kind: TopologyLayoutNodeKind;
  readonly width: number;
  readonly height: number;
}

export interface TopologyLayoutInputEdge {
  readonly id: string;
  readonly source: string;
  readonly target: string;
}

export interface NormalizedTopologyLayoutNode extends TopologyLayoutInputNode {
  readonly layer: number;
}

export interface NormalizedTopologyLayout {
  readonly nodes: readonly NormalizedTopologyLayoutNode[];
  readonly edges: readonly TopologyLayoutInputEdge[];
}

export interface TopologyLayoutPort {
  readonly id: string;
  readonly side: "left" | "right";
  readonly y: number;
}

export interface TopologyLayoutNode {
  readonly id: string;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
  readonly ports: readonly TopologyLayoutPort[];
}

export interface TopologyLayoutRoute {
  readonly id: string;
  readonly source: string;
  readonly target: string;
  readonly points: readonly { readonly x: number; readonly y: number }[];
}

export interface TopologyLayoutResult {
  readonly width: number;
  readonly height: number;
  readonly nodes: readonly TopologyLayoutNode[];
  readonly routes: readonly TopologyLayoutRoute[];
}

const DEFAULT_NODE_WIDTH = 220;
const DEFAULT_NODE_HEIGHT = 90;
const LAYOUT_PADDING = 28;

export function topologySemanticLayer(kind: TopologyLayoutNodeKind): number {
  switch (kind) {
    case "profile":
      return 0;
    case "worktree":
      return 1;
    case "repository":
      return 2;
    case "shared_input":
    case "unknown":
      return 3;
  }
}

function validDimension(value: number): boolean {
  return Number.isFinite(value) && value > 0;
}

// normalizeTopologyLayout is the boundary between the topology model and ELK.
// It deliberately keeps a stable ID order only as a tie-breaker; ELK's layered
// sweeps decide the vertical order from neighbouring relationships.
export function normalizeTopologyLayout(
  nodes: readonly TopologyLayoutInputNode[],
  edges: readonly TopologyLayoutInputEdge[],
): NormalizedTopologyLayout {
  const seen = new Set<string>();
  const normalizedNodes: NormalizedTopologyLayoutNode[] = [];
  for (const node of nodes) {
    if (
      node.id.length === 0 ||
      seen.has(node.id) ||
      !validDimension(node.width) ||
      !validDimension(node.height)
    ) {
      continue;
    }
    seen.add(node.id);
    normalizedNodes.push({
      ...node,
      layer: topologySemanticLayer(node.kind),
    });
  }
  normalizedNodes.sort(
    (left, right) =>
      left.layer - right.layer || left.id.localeCompare(right.id),
  );
  const nodeIDs = new Set(normalizedNodes.map((node) => node.id));
  const edgeIDs = new Set<string>();
  const normalizedEdges: TopologyLayoutInputEdge[] = [];
  for (const edge of edges) {
    if (
      edge.id.length === 0 ||
      edgeIDs.has(edge.id) ||
      edge.source === edge.target ||
      !nodeIDs.has(edge.source) ||
      !nodeIDs.has(edge.target)
    ) {
      continue;
    }
    edgeIDs.add(edge.id);
    normalizedEdges.push(edge);
  }
  normalizedEdges.sort(
    (left, right) =>
      left.source.localeCompare(right.source) ||
      left.target.localeCompare(right.target) ||
      left.id.localeCompare(right.id),
  );
  return { nodes: normalizedNodes, edges: normalizedEdges };
}

function fallbackResult(
  normalized: NormalizedTopologyLayout,
): TopologyLayoutResult {
  const byLayer = new Map<number, NormalizedTopologyLayoutNode[]>();
  for (const node of normalized.nodes) {
    const entries = byLayer.get(node.layer) ?? [];
    entries.push(node);
    byLayer.set(node.layer, entries);
  }
  const nodes: TopologyLayoutNode[] = [];
  let x = LAYOUT_PADDING;
  let height = DEFAULT_NODE_HEIGHT + LAYOUT_PADDING * 2;
  for (const layer of [...byLayer.keys()].sort((left, right) => left - right)) {
    const entries = byLayer.get(layer) ?? [];
    let y = LAYOUT_PADDING;
    let layerWidth = DEFAULT_NODE_WIDTH;
    for (const node of entries) {
      nodes.push({
        id: node.id,
        x,
        y,
        width: node.width,
        height: node.height,
        ports: [],
      });
      y += node.height + 28;
      layerWidth = Math.max(layerWidth, node.width);
    }
    height = Math.max(height, y - 28 + LAYOUT_PADDING);
    x += layerWidth + 72;
  }
  return {
    width: Math.max(400, x - 72 + LAYOUT_PADDING),
    height: Math.max(220, height),
    nodes,
    routes: [],
  };
}

export function createTopologyLayoutFallback(
  nodes: readonly TopologyLayoutInputNode[],
  edges: readonly TopologyLayoutInputEdge[],
): TopologyLayoutResult {
  return fallbackResult(normalizeTopologyLayout(nodes, edges));
}

let elkLayoutEngine: Promise<ELK> | undefined;

function getElkLayoutEngine(): Promise<ELK> {
  if (!elkLayoutEngine) {
    const engine =
      import.meta.env.MODE === "test"
        ? import("elkjs/lib/elk.bundled.js").then(
            ({ default: ElkConstructor }) => new ElkConstructor(),
          )
        : import("elkjs/lib/elk-api.js").then(
            ({ default: ElkConstructor }) =>
              new ElkConstructor({
                workerUrl: elkWorkerUrl,
              }),
          );
    elkLayoutEngine = engine.catch((error: unknown) => {
      elkLayoutEngine = undefined;
      throw error;
    });
  }
  return elkLayoutEngine;
}

function edgeSectionPoints(
  edge: ElkExtendedEdge,
): readonly { readonly x: number; readonly y: number }[] {
  const points: { x: number; y: number }[] = [];
  for (const section of edge.sections ?? []) {
    for (const point of [
      section.startPoint,
      ...(section.bendPoints ?? []),
      section.endPoint,
    ]) {
      const previous = points.at(-1);
      if (previous?.x === point.x && previous.y === point.y) continue;
      points.push({ x: point.x, y: point.y });
    }
  }
  return points;
}

function layerConstraint(
  node: NormalizedTopologyLayoutNode,
): "FIRST" | "LAST" | undefined {
  if (node.kind === "profile") return "FIRST";
  if (node.kind === "shared_input") return "LAST";
  return undefined;
}

export async function calculateTopologyLayout(
  nodes: readonly TopologyLayoutInputNode[],
  edges: readonly TopologyLayoutInputEdge[],
): Promise<TopologyLayoutResult> {
  const normalized = normalizeTopologyLayout(nodes, edges);
  if (normalized.nodes.length === 0) return fallbackResult(normalized);

  const portsByNode = new Map<
    string,
    { readonly id: string; readonly side: "EAST" | "WEST" }[]
  >();
  const portSides = new Map<string, TopologyLayoutPort["side"]>();
  const addPort = (
    nodeID: string,
    direction: "in" | "out",
    edgeID: string,
  ): string => {
    const id = `topology:port:${nodeID}:${direction}:${edgeID}`;
    const ports = portsByNode.get(nodeID) ?? [];
    ports.push({ id, side: direction === "out" ? "EAST" : "WEST" });
    portsByNode.set(nodeID, ports);
    portSides.set(id, direction === "out" ? "right" : "left");
    return id;
  };
  const elkEdges = normalized.edges.map((edge) => ({
    id: edge.id,
    sources: [addPort(edge.source, "out", edge.id)],
    targets: [addPort(edge.target, "in", edge.id)],
  }));
  const graph: ElkNode = {
    id: "topology:root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "RIGHT",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.layered.spacing.nodeNodeBetweenLayers": "180",
      "elk.spacing.nodeNode": "42",
      "elk.spacing.edgeNode": "30",
      "elk.layered.spacing.edgeEdgeBetweenLayers": "14",
      "elk.spacing.portPort": "8",
    },
    children: normalized.nodes.map((node) => ({
      id: node.id,
      width: node.width,
      height: node.height,
      layoutOptions: {
        "elk.portConstraints": "FIXED_SIDE",
        ...(layerConstraint(node)
          ? {
              "elk.layered.layering.layerConstraint": layerConstraint(node),
            }
          : {}),
      },
      ports: (portsByNode.get(node.id) ?? []).map((port) => ({
        id: port.id,
        width: 2,
        height: 2,
        layoutOptions: { "elk.port.side": port.side },
      })),
    })),
    edges: elkEdges,
  };
  const laidOut = await (await getElkLayoutEngine()).layout(graph);
  const layoutNodes = (laidOut.children ?? []).map((node) => ({
    id: node.id,
    x: (node.x ?? 0) + LAYOUT_PADDING,
    y: (node.y ?? 0) + LAYOUT_PADDING,
    width: node.width ?? DEFAULT_NODE_WIDTH,
    height: node.height ?? DEFAULT_NODE_HEIGHT,
    ports: (node.ports ?? []).map((port) => ({
      id: port.id,
      side: portSides.get(port.id) ?? "left",
      y: (port.y ?? DEFAULT_NODE_HEIGHT / 2) + (port.height ?? 0) / 2,
    })),
  }));
  const routes = (laidOut.edges ?? []).flatMap((edge) => {
    const points = edgeSectionPoints(edge).map((point) => ({
      x: point.x + LAYOUT_PADDING,
      y: point.y + LAYOUT_PADDING,
    }));
    const source = normalized.edges.find(
      (candidate) => candidate.id === edge.id,
    );
    return source && points.length >= 2
      ? [{ id: edge.id, source: source.source, target: source.target, points }]
      : [];
  });
  return {
    width: Math.max(400, (laidOut.width ?? 0) + LAYOUT_PADDING * 2),
    height: Math.max(220, (laidOut.height ?? 0) + LAYOUT_PADDING * 2),
    nodes: layoutNodes,
    routes,
  };
}
