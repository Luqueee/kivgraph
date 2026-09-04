import { describe, expect, it } from "vitest";

import type { TopologyRelationship } from "@/api/client";
import {
  createTopologyFlowEdges,
  createTopologyFlowNodes,
} from "@/components/TopologyFlow";
import type { TopologyModel } from "@/topology";

const sourceNode = {
  key: "repository:source",
  id: "source",
  type: "repository" as const,
  label: "source",
  subtitle: "Go",
  status: "ready",
  profileIds: ["default"],
  worktreeIds: [],
  repositoryIds: ["source"],
  languages: ["go"],
};

const targetNode = {
  key: "repository:target",
  id: "target",
  type: "repository" as const,
  label: "target",
  subtitle: "TypeScript",
  status: "ready",
  profileIds: ["default"],
  worktreeIds: [],
  repositoryIds: ["target"],
  languages: ["typescript"],
};

const relationship: TopologyRelationship = {
  profile: "default",
  type: "code_dependency",
  source: { type: "repository", id: "source" },
  target: { type: "repository", id: "target" },
  kind: "CALLS_DIRECT",
  status: "exact",
  confidence: "EXACT_TYPECHECKED",
  provenance: "GO_TYPES_USE",
  evidence: "source.go:10",
};

const model: TopologyModel = {
  nodes: [sourceNode, targetNode],
  edges: [
    {
      key: "edge:0",
      relationshipIndex: 0,
      sourceKey: sourceNode.key,
      targetKey: targetNode.key,
      relationship,
    },
    {
      key: "edge:1",
      relationshipIndex: 1,
      sourceKey: sourceNode.key,
      targetKey: targetNode.key,
      relationship: { ...relationship, evidence: "source.go:20" },
    },
  ],
  relationships: [relationship, { ...relationship, evidence: "source.go:20" }],
  unrenderedRelationships: [],
  boundaries: [],
  layout: {
    width: 520,
    height: 180,
    nodes: [
      { key: sourceNode.key, x: 24, y: 24, width: 228, height: 110 },
      { key: targetNode.key, x: 288, y: 24, width: 228, height: 110 },
    ],
  },
};

describe("TopologyFlow", () => {
  it("does not draw relationships whose target was not resolved", () => {
    const edges = createTopologyFlowEdges(
      {
        ...model,
        edges: [{ ...model.edges[0], targetKey: undefined }],
      },
      null,
    );

    expect(edges).toEqual([]);
  });

  it("groups repeated relationships into one readable edge", () => {
    const edges = createTopologyFlowEdges(model, null);

    expect(edges).toHaveLength(1);
    expect(edges[0].data?.count).toBe(2);
    expect(edges[0].label).toBe("×2");
    expect(edges[0].source).toBe(sourceNode.key);
    expect(edges[0].target).toBe(targetNode.key);
  });

  it("keeps unrelated nodes and edges visually quiet around a selection", () => {
    const edges = createTopologyFlowEdges(model, sourceNode.key);
    const nodes = createTopologyFlowNodes(model, sourceNode.key, () => {});

    expect(edges[0].style?.opacity).toBe(0.78);
    expect(nodes).toHaveLength(2);
    expect(nodes.find((node) => node.id === sourceNode.key)?.selected).toBe(
      true,
    );
    expect(nodes.every((node) => node.data.active)).toBe(true);
    expect(nodes.every((node) => node.draggable === false)).toBe(true);
  });
});
