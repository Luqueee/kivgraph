import {
  Background,
  BackgroundVariant,
  BaseEdge,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import { useEffect, useMemo, useState } from "react";
import "@xyflow/react/dist/style.css";

import type { TopologyRelationship } from "@/api/client";
import {
  topologyEdgeKind,
  type TopologyEdge,
  type TopologyModel,
  type TopologyNode,
} from "@/topology";
import {
  calculateTopologyLayout,
  type TopologyLayoutInputEdge,
  type TopologyLayoutInputNode,
} from "@/topology-layout";
import { cn } from "@/lib/utils";

export const TOPOLOGY_NODE_STYLES: Record<
  TopologyNode["type"],
  { readonly label: string; readonly color: string }
> = {
  profile: { label: "profile", color: "#7c3aed" },
  worktree: { label: "worktree", color: "#94a3b8" },
  repository: { label: "repository", color: "#2563eb" },
  shared_input: { label: "shared input", color: "#059669" },
};

const REPOSITORY_GROUP_COLOR = "#2563eb";
export const TOPOLOGY_EDGE_COLORS = {
  exact: "#16a34a",
  candidate: "#ea580c",
  unresolved: "#eab308",
  conflict: "#ef4444",
  structural: "#64748b",
  overlay: "#a855f7",
  invalidation: "#0ea5e9",
} as const;
const FLOW_NODE_WIDTH = 220;
const FLOW_NODE_HEIGHT = 90;
const FLOW_COLUMN_GAP = 52;
const FLOW_ROW_GAP = 24;
const FLOW_PADDING = 28;
const MAX_RENDERED_FLOW_EDGES = 600;
const MAX_TRACE_DEPTH = 3;
const MAX_TRACE_NODES = 48;

export interface TopologyFlowOptions {
  readonly showWorktrees: boolean;
  readonly showInternalRelationships: boolean;
  readonly expandedProfiles: readonly string[];
  readonly hoveredKey: string | null;
}

const DEFAULT_FLOW_OPTIONS: TopologyFlowOptions = {
  showWorktrees: true,
  showInternalRelationships: false,
  expandedProfiles: [],
  hoveredKey: null,
};

interface FlowTopologyNode {
  readonly kind: "topology";
  readonly topologyNode: TopologyNode;
}

interface FlowRepositoryGroupNode {
  readonly kind: "repository_group";
  readonly key: string;
  readonly profileID: string;
  readonly label: string;
  readonly subtitle: string;
  readonly repositoryCount: number;
}

type FlowDisplayNode = FlowTopologyNode | FlowRepositoryGroupNode;

interface FlowLayoutNode {
  readonly key: string;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
  readonly ports?: readonly FlowLayoutPort[];
}

interface FlowLayoutPort {
  readonly key: string;
  readonly side: "left" | "right";
  readonly y: number;
}

interface FlowLayoutRoute {
  readonly id: string;
  readonly path: string;
}

interface FlowGraph {
  readonly nodes: readonly FlowDisplayNode[];
  readonly edges: readonly TopologyEdge[];
  readonly nodesByKey: ReadonlyMap<string, FlowDisplayNode>;
  readonly edgeGroups: readonly FlowEdgeGroup[];
  readonly dependencyEdgesByNode: ReadonlyMap<string, readonly TopologyEdge[]>;
  readonly layout: {
    readonly width: number;
    readonly height: number;
    readonly nodes: readonly FlowLayoutNode[];
    readonly routes?: readonly FlowLayoutRoute[];
  };
}

interface FlowEdgeGroup {
  readonly key: string;
  readonly edge: TopologyEdge;
  readonly count: number;
}

export type TopologyFlowNodeData = Record<string, unknown> & {
  readonly displayNode: FlowDisplayNode;
  readonly ports: readonly FlowLayoutPort[];
  readonly active: boolean;
  readonly emphasized: boolean;
  readonly onSelect: (key: string) => void;
  readonly onToggleProfile: (profileID: string) => void;
};

export type TopologyFlowNode = Node<TopologyFlowNodeData, "topology">;

export type TopologyFlowEdgeData = Record<string, unknown> & {
  readonly relationship: TopologyRelationship;
  readonly count: number;
  readonly routePath?: string;
};

export type TopologyFlowEdge = Edge<TopologyFlowEdgeData, "routed">;

function displayNodeKey(node: FlowDisplayNode): string {
  return node.kind === "topology" ? node.topologyNode.key : node.key;
}

function displayNodeType(node: FlowDisplayNode): string {
  return node.kind === "topology"
    ? TOPOLOGY_NODE_STYLES[node.topologyNode.type].label
    : "repository group";
}

function displayNodeLabel(node: FlowDisplayNode): string {
  return node.kind === "topology" ? node.topologyNode.label : node.label;
}

function displayNodeSubtitle(node: FlowDisplayNode): string {
  return node.kind === "topology" ? node.topologyNode.subtitle : node.subtitle;
}

function displayNodeColor(node: FlowDisplayNode): string {
  return node.kind === "topology"
    ? TOPOLOGY_NODE_STYLES[node.topologyNode.type].color
    : REPOSITORY_GROUP_COLOR;
}

function topologyDisplayNode(node: TopologyNode): FlowTopologyNode {
  return { kind: "topology", topologyNode: node };
}

function relationshipLabel(relationship: TopologyRelationship): string {
  if (relationship.kind === "contains" || relationship.type === "membership") {
    return "contains";
  }
  if (
    relationship.kind === "uses" ||
    relationship.type === "shared_input_usage"
  ) {
    return "uses";
  }
  if (
    relationship.kind === "overlays" ||
    relationship.type === "worktree_overlay"
  ) {
    return "overlays";
  }
  if (
    relationship.kind === "invalidates" ||
    relationship.type === "shared_input_invalidation"
  ) {
    return "invalidates";
  }
  if (relationship.status === "conflict") return "conflicts with";
  if (relationship.status === "unresolved") return "not resolved";
  if (relationship.status === "candidate") return "candidate dependency";
  if (relationship.type === "code_dependency") return "depends on";
  return topologyEdgeKind(relationship).toLocaleLowerCase();
}

type RelationshipVisualStatus = keyof typeof TOPOLOGY_EDGE_COLORS;

function relationshipVisualStatus(
  relationship: TopologyRelationship,
): RelationshipVisualStatus {
  if (
    relationship.kind === "overlays" ||
    relationship.type === "worktree_overlay"
  ) {
    return "overlay";
  }
  if (
    relationship.kind === "invalidates" ||
    relationship.type === "shared_input_invalidation"
  ) {
    return "invalidation";
  }
  if (
    relationship.status === "structural" ||
    relationshipLabel(relationship) === "contains"
  ) {
    return "structural";
  }
  switch (relationship.status) {
    case "exact":
    case "candidate":
    case "unresolved":
    case "conflict":
      return relationship.status;
    default:
      return "unresolved";
  }
}

function flowEdgeGroupKey(edge: TopologyEdge): string {
  const relationship = edge.relationship;
  return JSON.stringify([
    edge.sourceKey,
    edge.targetKey,
    relationship.profile ?? "",
    relationshipLabel(relationship),
    relationship.status,
  ]);
}

function flowEdgeLabel(
  relationship: TopologyRelationship,
  count: number,
): string {
  const label = relationshipLabel(relationship);
  return `${label}${count > 1 ? ` ×${count}` : ""}`;
}

function groupFlowEdges(
  edges: readonly TopologyEdge[],
): readonly FlowEdgeGroup[] {
  const groups = new Map<
    string,
    { key: string; edge: TopologyEdge; count: number }
  >();
  for (const edge of edges) {
    const key = flowEdgeGroupKey(edge);
    const group = groups.get(key);
    if (group) {
      group.count += 1;
    } else {
      groups.set(key, { key, edge, count: 1 });
    }
  }
  return [...groups.values()];
}

function dependencyEdgesByNode(
  edges: readonly TopologyEdge[],
): ReadonlyMap<string, readonly TopologyEdge[]> {
  const byNode = new Map<string, TopologyEdge[]>();
  for (const edge of edges) {
    if (!edge.targetKey || !dependencyEdge(edge)) continue;
    const sourceEdges = byNode.get(edge.sourceKey) ?? [];
    sourceEdges.push(edge);
    byNode.set(edge.sourceKey, sourceEdges);
    const targetEdges = byNode.get(edge.targetKey) ?? [];
    targetEdges.push(edge);
    byNode.set(edge.targetKey, targetEdges);
  }
  return byNode;
}

function structuralEdge(
  key: string,
  source: FlowDisplayNode,
  target: FlowDisplayNode,
  profile?: string,
): TopologyEdge {
  return {
    key,
    relationshipIndex: -1,
    sourceKey: displayNodeKey(source),
    targetKey: displayNodeKey(target),
    relationship: {
      profile,
      type: "membership",
      source: { type: displayNodeType(source), id: displayNodeLabel(source) },
      target: { type: displayNodeType(target), id: displayNodeLabel(target) },
      kind: "contains",
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      provenance: "TOPOLOGY_DECLARATION",
      reason: `${displayNodeLabel(source)} contains ${displayNodeLabel(target)}`,
    },
  };
}

function repositoryGroup(profile: TopologyNode): FlowRepositoryGroupNode {
  const count = profile.repositoryIds.length;
  return {
    kind: "repository_group",
    key: `topology:repository-group:${profile.id}`,
    profileID: profile.id,
    label: "Repository network",
    subtitle: `${count} ${count === 1 ? "repository" : "repositories"} · click to explore`,
    repositoryCount: count,
  };
}

function includesAny(
  values: readonly string[],
  candidates: ReadonlySet<string>,
): boolean {
  return values.some((value) => candidates.has(value));
}

function flowLayer(node: FlowDisplayNode, showWorktrees: boolean): number {
  if (node.kind === "repository_group") return 1;
  if (node.topologyNode.type === "profile") return 0;
  if (node.topologyNode.type === "worktree") return 1;
  return showWorktrees ? 2 : 1;
}

function columnsForLayer(layer: number, count: number): number {
  const maximum = layer === 0 ? 2 : 4;
  return Math.max(1, Math.min(maximum, count));
}

function createFallbackLayout(
  nodes: readonly FlowDisplayNode[],
  showWorktrees: boolean,
): FlowGraph["layout"] {
  const layers = new Map<number, FlowDisplayNode[]>();
  for (const node of nodes) {
    const layer = flowLayer(node, showWorktrees);
    const layerNodes = layers.get(layer) ?? [];
    layerNodes.push(node);
    layers.set(layer, layerNodes);
  }

  const layoutNodes: FlowLayoutNode[] = [];
  let x = FLOW_PADDING;
  let maxHeight = FLOW_NODE_HEIGHT;
  let right = FLOW_PADDING;
  for (const layer of [0, 1, 2]) {
    const layerNodes = layers.get(layer) ?? [];
    if (layerNodes.length === 0) continue;
    const columns = columnsForLayer(layer, layerNodes.length);
    const rows = Math.ceil(layerNodes.length / columns);
    const laneWidth =
      columns * FLOW_NODE_WIDTH + (columns - 1) * FLOW_COLUMN_GAP;
    for (const [index, node] of layerNodes.entries()) {
      layoutNodes.push({
        key: displayNodeKey(node),
        x: x + (index % columns) * (FLOW_NODE_WIDTH + FLOW_COLUMN_GAP),
        y:
          FLOW_PADDING +
          Math.floor(index / columns) * (FLOW_NODE_HEIGHT + FLOW_ROW_GAP),
        width: FLOW_NODE_WIDTH,
        height: FLOW_NODE_HEIGHT,
      });
    }
    maxHeight = Math.max(
      maxHeight,
      rows * FLOW_NODE_HEIGHT + (rows - 1) * FLOW_ROW_GAP,
    );
    right = x + laneWidth;
    x += laneWidth + FLOW_COLUMN_GAP;
  }

  return {
    width: Math.max(400, right + FLOW_PADDING),
    height: FLOW_PADDING * 2 + maxHeight,
    nodes: layoutNodes,
  };
}

function sortedNeighbours(
  neighbours: ReadonlyMap<string, ReadonlySet<string>>,
  key: string,
): readonly string[] {
  return [...(neighbours.get(key) ?? [])].sort((left, right) =>
    left.localeCompare(right),
  );
}

// Condense cycles before assigning ranks. The fallback runs synchronously while
// ELK is loading, so neither traversal may consume the browser call stack.
function dependencyRanks(
  outgoing: ReadonlyMap<string, ReadonlySet<string>>,
  incoming: ReadonlyMap<string, ReadonlySet<string>>,
): ReadonlyMap<string, number> {
  const keys = [...outgoing.keys()].sort((left, right) =>
    left.localeCompare(right),
  );
  const visited = new Set<string>();
  const finishOrder: string[] = [];

  for (const root of keys) {
    if (visited.has(root)) continue;
    visited.add(root);
    const stack: {
      readonly key: string;
      readonly targets: Iterator<string>;
    }[] = [{ key: root, targets: sortedNeighbours(outgoing, root).values() }];
    while (stack.length > 0) {
      const current = stack.at(-1);
      if (!current) break;
      const next = current.targets.next();
      if (next.done) {
        finishOrder.push(current.key);
        stack.pop();
        continue;
      }
      if (visited.has(next.value)) continue;
      visited.add(next.value);
      stack.push({
        key: next.value,
        targets: sortedNeighbours(outgoing, next.value).values(),
      });
    }
  }

  const componentFor = new Map<string, number>();
  let componentTotal = 0;
  for (const root of [...finishOrder].reverse()) {
    if (componentFor.has(root)) continue;
    const component = componentTotal;
    componentTotal += 1;
    componentFor.set(root, component);
    const stack = [root];
    while (stack.length > 0) {
      const current = stack.pop();
      if (!current) continue;
      for (const neighbour of sortedNeighbours(incoming, current)) {
        if (componentFor.has(neighbour)) continue;
        componentFor.set(neighbour, component);
        stack.push(neighbour);
      }
    }
  }

  const componentOutgoing = Array.from(
    { length: componentTotal },
    () => new Set<number>(),
  );
  const componentIncoming = Array.from(
    { length: componentTotal },
    () => new Set<number>(),
  );
  for (const key of keys) {
    const source = componentFor.get(key);
    if (source === undefined) continue;
    for (const targetKey of outgoing.get(key) ?? []) {
      const target = componentFor.get(targetKey);
      if (target === undefined || target === source) continue;
      componentOutgoing[source]?.add(target);
      componentIncoming[target]?.add(source);
    }
  }

  const remainingTargets = componentOutgoing.map((targets) => targets.size);
  const ranks = Array.from({ length: componentTotal }, () => 0);
  const ready = remainingTargets.flatMap((count, component) =>
    count === 0 ? [component] : [],
  );
  while (ready.length > 0) {
    const component = ready.pop();
    if (component === undefined) continue;
    for (const source of componentIncoming[component] ?? []) {
      ranks[source] = Math.max(ranks[source] ?? 0, (ranks[component] ?? 0) + 1);
      const remaining = (remainingTargets[source] ?? 0) - 1;
      remainingTargets[source] = remaining;
      if (remaining === 0) ready.push(source);
    }
  }

  const result = new Map<string, number>();
  for (const key of keys) {
    const component = componentFor.get(key);
    if (component !== undefined) result.set(key, ranks[component] ?? 0);
  }
  return result;
}

function createDependencyLayout(
  nodes: readonly FlowDisplayNode[],
  edges: readonly TopologyEdge[],
  showWorktrees: boolean,
): FlowGraph["layout"] {
  const repositoryNodes = nodes.filter(
    (node): node is FlowTopologyNode =>
      node.kind === "topology" && node.topologyNode.type === "repository",
  );
  if (repositoryNodes.length === 0) {
    return createFallbackLayout(nodes, showWorktrees);
  }

  const repositoryKeys = new Set(
    repositoryNodes.map((node) => node.topologyNode.key),
  );
  const outgoing = new Map<string, Set<string>>();
  const incoming = new Map<string, Set<string>>();
  for (const key of repositoryKeys) {
    outgoing.set(key, new Set());
    incoming.set(key, new Set());
  }
  for (const edge of edges) {
    if (
      !edge.targetKey ||
      !repositoryKeys.has(edge.sourceKey) ||
      !repositoryKeys.has(edge.targetKey)
    ) {
      continue;
    }
    outgoing.get(edge.sourceKey)?.add(edge.targetKey);
    incoming.get(edge.targetKey)?.add(edge.sourceKey);
  }

  const ranks = dependencyRanks(outgoing, incoming);
  let maxRank = 0;
  for (const rank of ranks.values()) maxRank = Math.max(maxRank, rank);
  const repositoriesByRank = new Map<number, string[]>();
  for (const node of repositoryNodes) {
    const rank = ranks.get(node.topologyNode.key) ?? 0;
    const entries = repositoriesByRank.get(rank) ?? [];
    entries.push(node.topologyNode.key);
    repositoriesByRank.set(rank, entries);
  }
  const labels = new Map(
    repositoryNodes.map((node) => [
      node.topologyNode.key,
      node.topologyNode.label,
    ]),
  );
  const order = new Map<string, number>();
  const setOrder = (rank: number): void => {
    for (const [index, key] of (repositoriesByRank.get(rank) ?? []).entries()) {
      order.set(key, index);
    }
  };
  for (const rank of repositoriesByRank.keys()) {
    repositoriesByRank
      .get(rank)
      ?.sort((left, right) =>
        (labels.get(left) ?? "").localeCompare(labels.get(right) ?? ""),
      );
    setOrder(rank);
  }
  const reorder = (
    rank: number,
    neighbours: ReadonlyMap<string, ReadonlySet<string>>,
  ): void => {
    const entries = repositoriesByRank.get(rank);
    if (!entries) return;
    entries.sort((left, right) => {
      const average = (key: string): number | null => {
        const values = [...(neighbours.get(key) ?? [])]
          .filter((node) => ranks.get(node) !== rank)
          .map((node) => order.get(node))
          .filter((value): value is number => value !== undefined);
        return values.length === 0
          ? null
          : values.reduce((total, value) => total + value, 0) / values.length;
      };
      const leftAverage = average(left);
      const rightAverage = average(right);
      if (leftAverage !== null && rightAverage !== null) {
        if (leftAverage !== rightAverage) return leftAverage - rightAverage;
      } else if (leftAverage !== null) {
        return -1;
      } else if (rightAverage !== null) {
        return 1;
      }
      return (labels.get(left) ?? "").localeCompare(labels.get(right) ?? "");
    });
    setOrder(rank);
  };
  for (let pass = 0; pass < 3; pass += 1) {
    for (let rank = 1; rank <= maxRank; rank += 1) reorder(rank, incoming);
    for (let rank = maxRank - 1; rank >= 0; rank -= 1) {
      reorder(rank, outgoing);
    }
  }

  const hasProfile = nodes.some(
    (node) => node.kind === "topology" && node.topologyNode.type === "profile",
  );
  const repositoryOffset = hasProfile ? (showWorktrees ? 2 : 1) : 0;
  const columns = new Map<number, FlowDisplayNode[]>();
  const addToColumn = (column: number, node: FlowDisplayNode): void => {
    const entries = columns.get(column) ?? [];
    entries.push(node);
    columns.set(column, entries);
  };
  for (const node of nodes) {
    if (node.kind === "repository_group") {
      addToColumn(1, node);
    } else if (node.topologyNode.type === "profile") {
      addToColumn(0, node);
    } else if (node.topologyNode.type === "worktree") {
      addToColumn(1, node);
    } else if (node.topologyNode.type === "repository") {
      const rank = ranks.get(node.topologyNode.key) ?? 0;
      addToColumn(repositoryOffset + maxRank - rank, node);
    } else {
      addToColumn(repositoryOffset + maxRank + 1, node);
    }
  }

  const layoutNodes: FlowLayoutNode[] = [];
  let x = FLOW_PADDING;
  let maxHeight = FLOW_NODE_HEIGHT;
  for (const [, column] of [...columns.entries()].sort(
    ([left], [right]) => left - right,
  )) {
    column.sort((left, right) => {
      const leftOrder = order.get(displayNodeKey(left));
      const rightOrder = order.get(displayNodeKey(right));
      if (leftOrder !== undefined && rightOrder !== undefined) {
        return leftOrder - rightOrder;
      }
      return displayNodeLabel(left).localeCompare(displayNodeLabel(right));
    });
    for (const [index, node] of column.entries()) {
      layoutNodes.push({
        key: displayNodeKey(node),
        x,
        y: FLOW_PADDING + index * (FLOW_NODE_HEIGHT + FLOW_ROW_GAP),
        width: FLOW_NODE_WIDTH,
        height: FLOW_NODE_HEIGHT,
      });
    }
    maxHeight = Math.max(
      maxHeight,
      column.length * FLOW_NODE_HEIGHT +
        Math.max(0, column.length - 1) * FLOW_ROW_GAP,
    );
    x += FLOW_NODE_WIDTH + FLOW_COLUMN_GAP;
  }

  return {
    width: Math.max(400, x - FLOW_COLUMN_GAP + FLOW_PADDING),
    height: FLOW_PADDING * 2 + maxHeight,
    nodes: layoutNodes,
  };
}

function layoutNodeKind(
  node: FlowDisplayNode,
): TopologyLayoutInputNode["kind"] {
  return node.kind === "repository_group"
    ? "repository"
    : node.topologyNode.type;
}

function routePath(
  points: readonly { readonly x: number; readonly y: number }[],
): string {
  return `M ${points.map((point) => `${point.x},${point.y}`).join(" L ")}`;
}

// The renderer only adapts normalized layout output to React Flow. Node
// placement, crossing minimization, ports, and orthogonal routes live in the
// dedicated topology-layout module.
async function createElkLayout(
  nodes: readonly FlowDisplayNode[],
  edgeGroups: readonly FlowEdgeGroup[],
): Promise<FlowGraph["layout"]> {
  const layoutNodes: TopologyLayoutInputNode[] = nodes.map((node) => ({
    id: displayNodeKey(node),
    kind: layoutNodeKind(node),
    width: FLOW_NODE_WIDTH,
    height: FLOW_NODE_HEIGHT,
  }));
  const layoutEdges: TopologyLayoutInputEdge[] = edgeGroups.flatMap((group) =>
    group.edge.targetKey && group.edge.targetKey !== group.edge.sourceKey
      ? [
          {
            id: group.key,
            source: group.edge.sourceKey,
            target: group.edge.targetKey,
          },
        ]
      : [],
  );
  const layout = await calculateTopologyLayout(layoutNodes, layoutEdges);
  return {
    width: layout.width,
    height: layout.height,
    nodes: layout.nodes.map((node) => ({
      key: node.id,
      x: node.x,
      y: node.y,
      width: node.width,
      height: node.height,
      ports: node.ports.map((port) => ({
        key: port.id,
        side: port.side,
        y: port.y,
      })),
    })),
    routes: layout.routes.map((route) => ({
      id: route.id,
      path: routePath(route.points),
    })),
  };
}

function createTopologyFlowGraph(
  model: TopologyModel,
  options: TopologyFlowOptions,
): FlowGraph {
  const topologyNodesByKey = new Map(
    model.nodes.map((node) => [node.key, node]),
  );
  const profiles = model.nodes.filter((node) => node.type === "profile");
  const expandedProfiles = new Set(options.expandedProfiles);
  const expandedProfileIDs = new Set(
    profiles
      .filter(
        (profile) => options.showWorktrees || expandedProfiles.has(profile.id),
      )
      .map((profile) => profile.id),
  );
  const displayNodes = new Map<string, FlowDisplayNode>();
  const structuralEdges: TopologyEdge[] = [];

  for (const profile of profiles) {
    const profileNode = topologyDisplayNode(profile);
    displayNodes.set(profile.key, profileNode);
    if (!expandedProfileIDs.has(profile.id)) {
      const group = repositoryGroup(profile);
      displayNodes.set(group.key, group);
      structuralEdges.push(
        structuralEdge(
          `topology:contains:${profile.key}:${group.key}`,
          profileNode,
          group,
          profile.id,
        ),
      );
      continue;
    }

    if (options.showWorktrees) {
      for (const worktreeID of profile.worktreeIds) {
        const worktree = topologyNodesByKey.get(`worktree:${worktreeID}`);
        if (!worktree) continue;
        const worktreeNode = topologyDisplayNode(worktree);
        displayNodes.set(worktree.key, worktreeNode);
      }
      continue;
    }

    for (const repositoryID of profile.repositoryIds) {
      const repository = topologyNodesByKey.get(`repository:${repositoryID}`);
      if (!repository) continue;
      const repositoryNode = topologyDisplayNode(repository);
      displayNodes.set(repository.key, repositoryNode);
    }
  }

  if (options.showWorktrees) {
    for (const worktree of model.nodes) {
      if (
        worktree.type !== "worktree" ||
        !includesAny(worktree.profileIds, expandedProfileIDs)
      ) {
        continue;
      }
      const repositoryID = worktree.repositoryIds[0];
      const repository = repositoryID
        ? topologyNodesByKey.get(`repository:${repositoryID}`)
        : undefined;
      if (!repository) continue;
      const worktreeNode = topologyDisplayNode(worktree);
      const repositoryNode = topologyDisplayNode(repository);
      displayNodes.set(worktree.key, worktreeNode);
      displayNodes.set(repository.key, repositoryNode);
    }
  }

  for (const node of model.nodes) {
    if (node.type === "repository" && profiles.length === 0) {
      displayNodes.set(node.key, topologyDisplayNode(node));
    }
    if (
      node.type === "shared_input" &&
      includesAny(node.profileIds, expandedProfileIDs)
    ) {
      displayNodes.set(node.key, topologyDisplayNode(node));
    }
  }

  const visibleKeys = new Set(displayNodes.keys());
  const availableEvidenceEdges = model.edges.filter((edge) => {
    if (!edge.targetKey || !visibleKeys.has(edge.sourceKey)) return false;
    if (!visibleKeys.has(edge.targetKey)) return false;
    if (
      !options.showInternalRelationships &&
      edge.sourceKey === edge.targetKey
    ) {
      return false;
    }
    return true;
  });
  const nodes = [...displayNodes.values()];
  const edges = [...structuralEdges, ...availableEvidenceEdges];

  return {
    nodes,
    edges,
    nodesByKey: displayNodes,
    edgeGroups: groupFlowEdges(edges),
    dependencyEdgesByNode: dependencyEdgesByNode(edges),
    layout: createDependencyLayout(nodes, edges, options.showWorktrees),
  };
}

interface TopologyFlowFocus {
  readonly key: string | null;
  readonly mode: "selection" | "hover" | null;
  readonly directNodeKeys: ReadonlySet<string>;
  readonly traceNodeKeys: ReadonlySet<string>;
  readonly directEdgeKeys: ReadonlySet<string>;
  readonly traceEdgeKeys: ReadonlySet<string>;
}

function dependencyEdge(edge: TopologyEdge): boolean {
  return relationshipLabel(edge.relationship) !== "contains";
}

function createTopologyFlowFocus(
  graph: FlowGraph,
  selectedKey: string | null,
  hoveredKey: string | null,
): TopologyFlowFocus {
  const key = selectedKey ?? hoveredKey;
  const mode = selectedKey ? "selection" : hoveredKey ? "hover" : null;
  const directNodeKeys = new Set<string>();
  const traceNodeKeys = new Set<string>();
  const directEdgeKeys = new Set<string>();
  const traceEdgeKeys = new Set<string>();
  if (!key || !mode) {
    return {
      key: null,
      mode: null,
      directNodeKeys,
      traceNodeKeys,
      directEdgeKeys,
      traceEdgeKeys,
    };
  }

  directNodeKeys.add(key);
  for (const edge of graph.dependencyEdgesByNode.get(key) ?? []) {
    if (!edge.targetKey) continue;
    directEdgeKeys.add(edge.key);
    directNodeKeys.add(edge.sourceKey);
    directNodeKeys.add(edge.targetKey);
  }
  if (mode === "hover") {
    return {
      key,
      mode,
      directNodeKeys,
      traceNodeKeys,
      directEdgeKeys,
      traceEdgeKeys,
    };
  }

  const frontier = [key];
  traceNodeKeys.add(key);
  for (let depth = 0; depth < MAX_TRACE_DEPTH; depth += 1) {
    const current = frontier.splice(0);
    for (const nodeKey of current) {
      for (const edge of graph.dependencyEdgesByNode.get(nodeKey) ?? []) {
        if (!edge.targetKey) continue;
        const neighbour =
          edge.sourceKey === nodeKey ? edge.targetKey : edge.sourceKey;
        traceEdgeKeys.add(edge.key);
        if (traceNodeKeys.has(neighbour)) continue;
        if (traceNodeKeys.size >= MAX_TRACE_NODES) continue;
        traceNodeKeys.add(neighbour);
        frontier.push(neighbour);
      }
    }
    if (frontier.length === 0 || traceNodeKeys.size >= MAX_TRACE_NODES) break;
  }
  return {
    key,
    mode,
    directNodeKeys,
    traceNodeKeys,
    directEdgeKeys,
    traceEdgeKeys,
  };
}

function createTopologyFlowNodesForGraph(
  graph: FlowGraph,
  selectedKey: string | null,
  onSelect: (key: string) => void,
  hoveredKey: string | null,
  onToggleProfile: (profileID: string) => void = () => {},
): TopologyFlowNode[] {
  const focus = createTopologyFlowFocus(graph, selectedKey, hoveredKey);

  return graph.layout.nodes.flatMap((layoutNode) => {
    const displayNode = graph.nodesByKey.get(layoutNode.key);
    if (!displayNode) return [];
    const nodeKey = displayNodeKey(displayNode);
    const isTopologyNode = displayNode.kind === "topology";
    return [
      {
        id: nodeKey,
        type: "topology" as const,
        position: { x: layoutNode.x, y: layoutNode.y },
        width: layoutNode.width,
        height: layoutNode.height,
        selected: isTopologyNode && nodeKey === selectedKey,
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
        draggable: false,
        selectable: false,
        connectable: false,
        ariaLabel: `${displayNodeType(displayNode)} ${displayNodeLabel(displayNode)}`,
        data: {
          displayNode,
          ports: layoutNode.ports ?? [],
          active:
            displayNode.kind === "repository_group" ||
            focus.mode !== "selection" ||
            focus.directNodeKeys.has(nodeKey) ||
            focus.traceNodeKeys.has(nodeKey),
          emphasized: focus.mode !== null && focus.directNodeKeys.has(nodeKey),
          onSelect,
          onToggleProfile,
        },
      },
    ];
  });
}

export function createTopologyFlowNodes(
  model: TopologyModel,
  selectedKey: string | null,
  onSelect: (key: string) => void,
  options: Partial<TopologyFlowOptions> = {},
  onToggleProfile: (profileID: string) => void = () => {},
): TopologyFlowNode[] {
  const flowOptions = { ...DEFAULT_FLOW_OPTIONS, ...options };
  return createTopologyFlowNodesForGraph(
    createTopologyFlowGraph(model, flowOptions),
    selectedKey,
    onSelect,
    flowOptions.hoveredKey,
    onToggleProfile,
  );
}

function createTopologyFlowEdgesForGraph(
  graph: FlowGraph,
  selectedKey: string | null,
  hoveredKey: string | null,
): TopologyFlowEdge[] {
  const focus = createTopologyFlowFocus(graph, selectedKey, hoveredKey);
  const routesByGroupKey = new Map(
    (graph.layout.routes ?? []).map((route) => [route.id, route.path]),
  );

  return graph.edgeGroups
    .slice(0, MAX_RENDERED_FLOW_EDGES)
    .map(({ key, edge, count }) => {
      const relationship = edge.relationship;
      const semanticLabel = relationshipLabel(relationship);
      const visualStatus = relationshipVisualStatus(relationship);
      const isStructural = visualStatus === "structural";
      const isDashed =
        isStructural ||
        visualStatus === "overlay" ||
        visualStatus === "invalidation";
      const isDirect = focus.directEdgeKeys.has(edge.key);
      const isTrace = focus.traceEdgeKeys.has(edge.key);
      const isFocused = isDirect || isTrace;
      const color = TOPOLOGY_EDGE_COLORS[visualStatus];
      const label =
        count > 1 || isStructural || relationship.status !== "exact"
          ? flowEdgeLabel(relationship, count)
          : undefined;

      return {
        id: `topology-flow-edge-${key}`,
        source: edge.sourceKey,
        target: edge.targetKey as string,
        sourceHandle: "source",
        targetHandle: "target",
        type: "routed",
        data: {
          relationship,
          count,
          routePath: routesByGroupKey.get(key),
        },
        selectable: false,
        focusable: true,
        ariaLabel: `${semanticLabel} relationship from ${edge.sourceKey} to ${edge.targetKey}${count > 1 ? `, ${count} grouped relationships` : ""}`,
        label,
        labelShowBg: Boolean(label),
        labelStyle: {
          fill: color,
          fontFamily: "ui-monospace, monospace",
          fontSize: 10,
          fontWeight: 600,
        },
        labelBgStyle: { fill: "#101215", fillOpacity: 0.95 },
        labelBgPadding: [5, 3] as [number, number],
        labelBgBorderRadius: 0,
        markerEnd: { type: MarkerType.ArrowClosed, color },
        style: {
          stroke: color,
          strokeDasharray: isDashed ? "6 4" : undefined,
          strokeWidth:
            focus.mode === null
              ? isStructural
                ? 1.35
                : 1.25
              : isDirect
                ? 2.4
                : 1.2,
          opacity:
            focus.mode === null
              ? isStructural
                ? 0.68
                : 0.6
              : focus.mode === "hover"
                ? isDirect
                  ? 0.9
                  : 0.5
                : isDirect
                  ? 0.98
                  : isTrace
                    ? 0.58
                    : isFocused
                      ? 0.25
                      : 0.1,
        },
      };
    });
}

export function createTopologyFlowEdges(
  model: TopologyModel,
  selectedKey: string | null,
  options: Partial<TopologyFlowOptions> = {},
): TopologyFlowEdge[] {
  const flowOptions = { ...DEFAULT_FLOW_OPTIONS, ...options };
  return createTopologyFlowEdgesForGraph(
    createTopologyFlowGraph(model, flowOptions),
    selectedKey,
    flowOptions.hoveredKey,
  );
}

function TopologyFlowNodeView({
  data,
  selected,
}: NodeProps<TopologyFlowNode>): React.ReactElement {
  const { displayNode, ports, active, emphasized, onSelect, onToggleProfile } =
    data;
  const isGroup = displayNode.kind === "repository_group";
  const color = displayNodeColor(displayNode);
  const label = displayNodeLabel(displayNode);
  const subtitle = displayNodeSubtitle(displayNode);
  const topologyNode =
    displayNode.kind === "topology" ? displayNode.topologyNode : undefined;

  return (
    <>
      <Handle
        id="target"
        type="target"
        position={Position.Left}
        className="!pointer-events-none !h-px !w-px !border-0 !bg-transparent !opacity-0"
      />
      {ports.map((port) => (
        <span
          key={port.key}
          className="pointer-events-none absolute z-10 h-1 w-1 -translate-y-1/2 rounded-full bg-rule-strong"
          style={{ top: port.y, [port.side]: -2 }}
        />
      ))}
      <button
        type="button"
        className={cn(
          "grid h-full w-full content-start gap-1.5 rounded-none border px-3 py-2 text-left shadow-[0_8px_24px_rgba(0,0,0,0.24)] transition-[opacity,box-shadow,border-color] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-200",
          active ? "opacity-100" : "opacity-25",
          selected
            ? "border-gray-100 shadow-[0_0_0_2px_rgba(245,245,245,0.18),0_12px_28px_rgba(0,0,0,0.32)]"
            : emphasized
              ? "border-graph-exact shadow-[0_0_0_1px_rgba(34,197,94,0.35)]"
              : "border-rule-strong hover:border-gray-500",
        )}
        style={{
          background: `linear-gradient(135deg, ${color}22, #101215 76%)`,
          borderColor: selected ? "#f5f5f5" : `${color}99`,
        }}
        onClick={() => {
          if (displayNode.kind === "repository_group") {
            onToggleProfile(displayNode.profileID);
          } else {
            onSelect(displayNode.topologyNode.key);
          }
        }}
        aria-pressed={selected}
        aria-label={
          isGroup
            ? `Open ${displayNode.repositoryCount} repositories in profile ${displayNode.profileID}`
            : `${displayNodeType(displayNode)} ${label}`
        }
      >
        <span className="flex items-center justify-between gap-2 font-mono">
          <span
            className="truncate text-[9px] font-semibold uppercase tracking-[0.14em]"
            style={{ color }}
          >
            {displayNodeType(displayNode)}
          </span>
          <span
            className="h-1.5 w-1.5 shrink-0 rounded-full"
            style={{ backgroundColor: color }}
            title={
              topologyNode ? `status: ${topologyNode.status}` : "collapsed"
            }
          />
        </span>
        <span className="truncate text-[13px] font-semibold text-gray-100">
          {label}
        </span>
        <span className="truncate text-[10px] leading-4 text-gray-400">
          {subtitle}
        </span>
        {topologyNode ? (
          <span className="sr-only">
            status: {topologyNode.status}; languages:{" "}
            {topologyNode.languages.join(", ") || "not observed"}
          </span>
        ) : null}
      </button>
      <Handle
        id="source"
        type="source"
        position={Position.Right}
        className="!pointer-events-none !h-px !w-px !border-0 !bg-transparent !opacity-0"
      />
    </>
  );
}

const topologyNodeTypes = { topology: TopologyFlowNodeView };

function RoutedTopologyEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  markerEnd,
  style,
  data,
  label,
  labelStyle,
  labelShowBg,
  labelBgStyle,
  labelBgPadding,
  labelBgBorderRadius,
}: EdgeProps<TopologyFlowEdge>): React.ReactElement {
  const path =
    data?.routePath ??
    `M ${sourceX},${sourceY} H ${(sourceX + targetX) / 2} V ${targetY} H ${targetX}`;
  return (
    <BaseEdge
      id={id}
      path={path}
      markerEnd={markerEnd}
      style={style}
      label={label}
      labelX={(sourceX + targetX) / 2}
      labelY={(sourceY + targetY) / 2}
      labelStyle={labelStyle}
      labelShowBg={labelShowBg}
      labelBgStyle={labelBgStyle}
      labelBgPadding={labelBgPadding}
      labelBgBorderRadius={labelBgBorderRadius}
    />
  );
}

const topologyEdgeTypes = { routed: RoutedTopologyEdge };

export function TopologyFlow({
  model,
  selectedKey,
  onSelect,
  onToggleProfile = () => {},
  showWorktrees = DEFAULT_FLOW_OPTIONS.showWorktrees,
  showInternalRelationships = DEFAULT_FLOW_OPTIONS.showInternalRelationships,
  expandedProfiles = DEFAULT_FLOW_OPTIONS.expandedProfiles,
}: {
  readonly model: TopologyModel;
  readonly selectedKey: string | null;
  readonly onSelect: (key: string | null) => void;
  readonly onToggleProfile?: (profileID: string) => void;
  readonly showWorktrees?: boolean;
  readonly showInternalRelationships?: boolean;
  readonly expandedProfiles?: readonly string[];
}): React.ReactElement {
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);
  const [elkLayout, setElkLayout] = useState<FlowGraph["layout"] | null>(null);
  const graph = useMemo(
    () =>
      createTopologyFlowGraph(model, {
        showWorktrees,
        showInternalRelationships,
        expandedProfiles,
        hoveredKey: null,
      }),
    [expandedProfiles, model, showInternalRelationships, showWorktrees],
  );
  useEffect(() => {
    let cancelled = false;
    setElkLayout(null);
    void createElkLayout(graph.nodes, graph.edgeGroups)
      .then((layout) => {
        if (!cancelled) setElkLayout(layout);
      })
      .catch(() => {
        if (!cancelled) setElkLayout(null);
      });
    return () => {
      cancelled = true;
    };
  }, [graph]);
  const renderedGraph = useMemo(
    () => (elkLayout ? { ...graph, layout: elkLayout } : graph),
    [elkLayout, graph],
  );
  const nodes = useMemo(
    () =>
      createTopologyFlowNodesForGraph(
        renderedGraph,
        selectedKey,
        (key) => onSelect(key),
        hoveredKey,
        onToggleProfile,
      ),
    [hoveredKey, onSelect, onToggleProfile, renderedGraph, selectedKey],
  );
  const edges = useMemo(
    () =>
      createTopologyFlowEdgesForGraph(renderedGraph, selectedKey, hoveredKey),
    [hoveredKey, renderedGraph, selectedKey],
  );
  const focus = useMemo(
    () => createTopologyFlowFocus(renderedGraph, selectedKey, hoveredKey),
    [hoveredKey, renderedGraph, selectedKey],
  );
  const representedRelationshipCount = edges.reduce(
    (count, edge) => count + (edge.data?.count ?? 1),
    0,
  );
  const totalRelationshipCount = renderedGraph.edgeGroups.reduce(
    (count, group) => count + group.count,
    0,
  );
  const edgeGroupsTruncated = edges.length < renderedGraph.edgeGroups.length;
  const selectedRepository = model.nodes.find(
    (node) => node.key === selectedKey && node.type === "repository",
  );

  return (
    <div className="relative h-full min-h-[32rem] w-full overflow-hidden rounded-none">
      <ReactFlow<TopologyFlowNode, TopologyFlowEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={topologyNodeTypes}
        edgeTypes={topologyEdgeTypes}
        colorMode="dark"
        fitView
        fitViewOptions={{ padding: 0.24, minZoom: 0.35, maxZoom: 1.25 }}
        minZoom={0.25}
        maxZoom={2}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        nodesFocusable
        edgesFocusable
        autoPanOnNodeFocus
        onNodeClick={(_, node) => {
          if (node.data.displayNode.kind === "topology") onSelect(node.id);
        }}
        onNodeMouseEnter={(_, node) => {
          if (node.data.displayNode.kind === "topology") setHoveredKey(node.id);
        }}
        onNodeMouseLeave={(_, node) => {
          setHoveredKey((current) => (current === node.id ? null : current));
        }}
        onPaneClick={() => onSelect(null)}
        proOptions={{ hideAttribution: false }}
        defaultEdgeOptions={{
          type: "routed",
          selectable: false,
          focusable: true,
        }}
        className="topology-flow"
        aria-label="Profile topology map"
      >
        <Background
          color="#333a42"
          gap={28}
          size={1}
          variant={BackgroundVariant.Dots}
        />
        <Controls
          showInteractive={false}
          className="!m-3 overflow-hidden !border !border-rule-strong !bg-panel shadow-xl"
          aria-label="Topology map controls"
        />
        <MiniMap<TopologyFlowNode>
          pannable
          zoomable
          nodeColor={(node) => displayNodeColor(node.data.displayNode)}
          nodeStrokeColor="#0a0b0d"
          nodeStrokeWidth={2}
          className="!m-3 !border !border-rule-strong !bg-panel shadow-xl"
          aria-label="Topology minimap"
        />
        <div className="pointer-events-none absolute left-3 top-3 z-10 border border-rule-strong bg-panel px-3 py-2 font-mono text-[10px] text-gray-400 shadow-xl">
          <span className="text-gray-100">
            {expandedProfiles.length > 0 || showWorktrees
              ? "repository map"
              : "profile overview"}
          </span>
          <span className="mx-1.5 text-gray-500">·</span>
          <span>
            {expandedProfiles.length > 0 || showWorktrees
              ? "select a repository to highlight its direct links"
              : "open the repository group to explore"}
          </span>
        </div>
        {selectedRepository ? (
          <div className="absolute right-3 top-3 z-10 flex border border-rule-strong bg-panel font-mono text-[10px] shadow-xl">
            <span className="px-3 py-2 text-gray-300">
              ← {focus.directEdgeKeys.size} direct links
            </span>
            <span className="border-x border-rule-strong px-3 py-2 font-semibold text-gray-100">
              {selectedRepository.label}
            </span>
            <button
              type="button"
              className="px-3 py-2 text-gray-300 transition-colors hover:bg-raise hover:text-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-200"
              onClick={() => onSelect(null)}
            >
              clear trace
            </button>
          </div>
        ) : null}
        <div className="pointer-events-none absolute bottom-3 left-3 z-10 border border-rule-strong bg-panel px-3 py-2 font-mono text-[10px] text-gray-400 shadow-xl">
          {nodes.length} visible nodes · {edges.length}/
          {renderedGraph.edgeGroups.length} visual links ·{" "}
          {representedRelationshipCount}/{totalRelationshipCount} relationships
          represented
          {edgeGroupsTruncated
            ? ` · link display limited to ${MAX_RENDERED_FLOW_EDGES} groups`
            : ""}
          {focus.mode === "selection"
            ? " · trace shows up to three relationship steps"
            : expandedProfiles.length > 0
              ? " · select or hover a repository to trace its links"
              : ""}
        </div>
      </ReactFlow>
    </div>
  );
}
