import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import type { TopologyRelationship } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import {
  topologyEdgeKind,
  type TopologyEdge,
  type TopologyModel,
  type TopologyNode,
} from "@/topology";
import { cn } from "@/lib/utils";

const NODE_TYPE_LABELS: Record<TopologyNode["type"], string> = {
  profile: "profile",
  worktree: "worktree",
  repository: "repository",
  shared_input: "shared input",
};

const NODE_COLORS: Record<TopologyNode["type"], string> = {
  profile: "#a78bfa",
  worktree: "#38bdf8",
  repository: "#34d399",
  shared_input: "#fbbf24",
};

const MAX_RENDERED_FLOW_EDGES = 600;

export type TopologyFlowNodeData = Record<string, unknown> & {
  readonly topologyNode: TopologyNode;
  readonly active: boolean;
  readonly onSelect: (key: string) => void;
};

export type TopologyFlowNode = Node<TopologyFlowNodeData, "topology">;

export type TopologyFlowEdgeData = Record<string, unknown> & {
  readonly relationship: TopologyRelationship;
  readonly count: number;
};

export type TopologyFlowEdge = Edge<TopologyFlowEdgeData, "smoothstep">;

function relationshipColor(relationship: TopologyRelationship): string {
  return (
    {
      structural: "#94a3b8",
      exact: "#34d399",
      candidate: "#fbbf24",
      unresolved: "#fb923c",
      conflict: "#fb7185",
      shared_input_usage: "#fbbf24",
    }[relationship.type] ??
    {
      structural: "#94a3b8",
      exact: "#34d399",
      candidate: "#fbbf24",
      unresolved: "#fb923c",
      conflict: "#fb7185",
    }[relationship.status] ??
    "#94a3b8"
  );
}

function statusClass(status: string): string {
  switch (status) {
    case "ready":
    case "current":
    case "structural":
      return "border-emerald-400/40 bg-emerald-400/10 text-emerald-200";
    case "stale":
    case "partial":
    case "candidate":
    case "shared":
      return "border-amber-400/40 bg-amber-400/10 text-amber-200";
    case "missing":
    case "unavailable":
    case "unresolved":
    case "conflict":
      return "border-rose-400/40 bg-rose-400/10 text-rose-200";
    default:
      return "border-border/80 bg-muted/20 text-muted-foreground";
  }
}

function flowEdgeGroupKey(edge: TopologyEdge): string {
  const relationship = edge.relationship;
  return JSON.stringify([
    edge.sourceKey,
    edge.targetKey,
    relationship.profile ?? "",
    relationship.type,
    relationship.kind ?? "",
    relationship.status,
  ]);
}

function flowEdgeLabel(count: number): string | undefined {
  return count > 1 ? `×${count}` : undefined;
}

export function createTopologyFlowNodes(
  model: TopologyModel,
  selectedKey: string | null,
  onSelect: (key: string) => void,
): TopologyFlowNode[] {
  const nodesByKey = new Map(model.nodes.map((node) => [node.key, node]));
  const neighbours = topologyNeighbourKeys(model, selectedKey);

  return model.layout.nodes.flatMap((layoutNode) => {
    const topologyNode = nodesByKey.get(layoutNode.key);
    if (!topologyNode) return [];
    return [
      {
        id: topologyNode.key,
        type: "topology" as const,
        position: { x: layoutNode.x, y: layoutNode.y },
        width: layoutNode.width,
        height: layoutNode.height,
        selected: topologyNode.key === selectedKey,
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
        draggable: false,
        selectable: false,
        connectable: false,
        ariaLabel: `${NODE_TYPE_LABELS[topologyNode.type]} ${topologyNode.label}`,
        data: {
          topologyNode,
          active: selectedKey === null || neighbours.has(topologyNode.key),
          onSelect,
        },
      },
    ];
  });
}

export function createTopologyFlowEdges(
  model: TopologyModel,
  selectedKey: string | null,
): TopologyFlowEdge[] {
  const neighbours = topologyNeighbourKeys(model, selectedKey);
  const groups = new Map<string, { edge: TopologyEdge; count: number }>();

  for (const edge of model.edges) {
    if (!edge.targetKey) continue;
    const key = flowEdgeGroupKey(edge);
    const group = groups.get(key);
    if (group) {
      group.count += 1;
    } else {
      groups.set(key, { edge, count: 1 });
    }
  }

  return [...groups.values()]
    .slice(0, MAX_RENDERED_FLOW_EDGES)
    .map(({ edge, count }, index) => {
      const relationship = edge.relationship;
      const color = relationshipColor(relationship);
      const active =
        selectedKey === null ||
        neighbours.has(edge.sourceKey) ||
        neighbours.has(edge.targetKey ?? "");
      const label = flowEdgeLabel(count);

      return {
        id: `topology-flow-edge-${index}`,
        source: edge.sourceKey,
        target: edge.targetKey as string,
        sourceHandle: "source",
        targetHandle: "target",
        type: "smoothstep" as const,
        data: { relationship, count },
        selectable: false,
        focusable: true,
        ariaLabel: `${topologyEdgeKind(relationship)} relationship from ${edge.sourceKey} to ${edge.targetKey}${count > 1 ? `, ${count} grouped relationships` : ""}`,
        label,
        labelShowBg: Boolean(label),
        labelStyle: {
          fill: "#cbd5e1",
          fontSize: 10,
          fontWeight: 600,
        },
        labelBgStyle: { fill: "#0f172a", fillOpacity: 0.92 },
        labelBgPadding: [5, 3] as [number, number],
        labelBgBorderRadius: 5,
        markerEnd: { type: MarkerType.ArrowClosed, color },
        style: {
          stroke: color,
          strokeWidth: relationship.status === "structural" ? 1.5 : 2.2,
          opacity: active ? 0.78 : 0.1,
        },
      };
    });
}

function topologyNeighbourKeys(
  model: TopologyModel,
  selectedKey: string | null,
): Set<string> {
  if (!selectedKey) return new Set();
  const keys = new Set([selectedKey]);
  for (const edge of model.edges) {
    if (edge.sourceKey === selectedKey && edge.targetKey)
      keys.add(edge.targetKey);
    if (edge.targetKey === selectedKey) keys.add(edge.sourceKey);
  }
  return keys;
}

function TopologyFlowNodeView({
  data,
  selected,
}: NodeProps<TopologyFlowNode>): React.ReactElement {
  const { topologyNode, active, onSelect } = data;
  const color = NODE_COLORS[topologyNode.type];

  return (
    <>
      <Handle
        id="target"
        type="target"
        position={Position.Left}
        className="!pointer-events-none !h-2 !w-2 !border-2 !border-slate-950 !bg-slate-500"
      />
      <button
        type="button"
        className={cn(
          "grid h-full w-full content-start gap-2 rounded-xl border px-3 py-2.5 text-left shadow-xl transition-[opacity,box-shadow,border-color] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-300",
          active ? "opacity-100" : "opacity-25",
          selected
            ? "border-white/90 shadow-[0_0_0_2px_rgba(255,255,255,0.18),0_16px_32px_rgba(0,0,0,0.28)]"
            : "border-white/10 hover:border-white/30",
        )}
        style={{
          background: `linear-gradient(135deg, ${color}20, rgba(15,23,42,0.96) 72%)`,
          borderColor: selected ? "#f8fafc" : `${color}55`,
        }}
        onClick={() => onSelect(topologyNode.key)}
        aria-pressed={selected}
        aria-label={`${NODE_TYPE_LABELS[topologyNode.type]} ${topologyNode.label}`}
      >
        <span className="flex items-center justify-between gap-2">
          <span
            className="truncate text-[9px] font-semibold uppercase tracking-[0.16em]"
            style={{ color }}
          >
            {NODE_TYPE_LABELS[topologyNode.type]}
          </span>
          <span
            className="h-2 w-2 shrink-0 rounded-full shadow-[0_0_12px_currentColor]"
            style={{ backgroundColor: color, color }}
          />
        </span>
        <span className="truncate text-sm font-semibold text-slate-100">
          {topologyNode.label}
        </span>
        <span className="line-clamp-2 text-[10px] leading-4 text-slate-400">
          {topologyNode.subtitle}
        </span>
        <span className="flex flex-wrap gap-1">
          <Badge className={statusClass(topologyNode.status)} variant="outline">
            {topologyNode.status}
          </Badge>
          {topologyNode.languages.slice(0, 2).map((language) => (
            <Badge key={language} variant="secondary">
              {language}
            </Badge>
          ))}
        </span>
      </button>
      <Handle
        id="source"
        type="source"
        position={Position.Right}
        className="!pointer-events-none !h-2 !w-2 !border-2 !border-slate-950 !bg-slate-500"
      />
    </>
  );
}

const topologyNodeTypes = { topology: TopologyFlowNodeView };

export function TopologyFlow({
  model,
  selectedKey,
  onSelect,
}: {
  readonly model: TopologyModel;
  readonly selectedKey: string | null;
  readonly onSelect: (key: string | null) => void;
}): React.ReactElement {
  const nodes = createTopologyFlowNodes(model, selectedKey, (key) =>
    onSelect(key),
  );
  const edges = createTopologyFlowEdges(model, selectedKey);
  const renderedRelationshipCount = edges.reduce(
    (count, edge) => count + (edge.data?.count ?? 1),
    0,
  );

  return (
    <div className="relative h-full min-h-[32rem] w-full overflow-hidden rounded-xl">
      <ReactFlow<TopologyFlowNode, TopologyFlowEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={topologyNodeTypes}
        colorMode="dark"
        fitView
        fitViewOptions={{ padding: 0.18, minZoom: 0.25, maxZoom: 1.2 }}
        minZoom={0.2}
        maxZoom={2}
        onlyRenderVisibleElements
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        nodesFocusable
        edgesFocusable
        autoPanOnNodeFocus
        onNodeClick={(_, node) => onSelect(node.id)}
        onPaneClick={() => onSelect(null)}
        proOptions={{ hideAttribution: false }}
        defaultEdgeOptions={{
          type: "smoothstep",
          selectable: false,
          focusable: true,
        }}
        className="topology-flow"
        aria-label="Topology map"
      >
        <Background
          color="#334155"
          gap={24}
          size={1}
          variant={BackgroundVariant.Dots}
        />
        <Controls
          showInteractive={false}
          className="!m-3 overflow-hidden rounded-xl !border !border-slate-700/80 !bg-slate-900/90 shadow-xl"
          aria-label="Topology map controls"
        />
        <MiniMap<TopologyFlowNode>
          pannable
          zoomable
          nodeColor={(node) =>
            NODE_COLORS[(node.data as TopologyFlowNodeData).topologyNode.type]
          }
          nodeStrokeColor="#0f172a"
          nodeStrokeWidth={2}
          className="!m-3 overflow-hidden rounded-xl !border !border-slate-700/80 !bg-slate-950/90 shadow-xl"
          aria-label="Topology minimap"
        />
        <div className="pointer-events-none absolute left-3 top-3 z-10 rounded-lg border border-slate-700/80 bg-slate-950/80 px-3 py-2 text-[10px] text-slate-300 shadow-xl backdrop-blur">
          <span className="font-semibold text-slate-100">topology map</span>
          <span className="mx-1.5 text-slate-600">·</span>
          <span>drag to pan · scroll to zoom</span>
        </div>
        <div className="pointer-events-none absolute bottom-3 left-3 z-10 rounded-lg border border-slate-700/80 bg-slate-950/80 px-3 py-2 text-[10px] text-slate-400 shadow-xl backdrop-blur">
          {renderedRelationshipCount}/{model.edges.length} relationships
          rendered
          {renderedRelationshipCount < model.edges.length
            ? " · grouped for readability"
            : ""}
        </div>
      </ReactFlow>
    </div>
  );
}
