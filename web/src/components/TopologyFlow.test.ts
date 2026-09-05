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

const unrelatedNode = {
  key: "repository:unrelated",
  id: "unrelated",
  type: "repository" as const,
  label: "unrelated",
  subtitle: "Go",
  status: "ready",
  profileIds: ["default"],
  worktreeIds: [],
  repositoryIds: ["unrelated"],
  languages: ["go"],
};

const profileNode = {
  key: "profile:default",
  id: "default",
  type: "profile" as const,
  label: "default",
  subtitle: "2 repositories",
  status: "ready",
  profileIds: ["default"],
  worktreeIds: ["legacy:source"],
  repositoryIds: ["source", "target"],
  languages: ["go", "typescript"],
};

const worktreeNode = {
  key: "worktree:legacy:source",
  id: "legacy:source",
  type: "worktree" as const,
  label: "source",
  subtitle: "/workspace/source",
  status: "current",
  profileIds: ["default"],
  worktreeIds: ["legacy:source"],
  repositoryIds: ["source"],
  languages: ["go"],
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
  nodes: [sourceNode, targetNode, unrelatedNode],
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

function dependencyModel(count: number, cycle = false): TopologyModel {
  const nodes = Array.from({ length: count }, (_, index) => ({
    ...sourceNode,
    key: `repository:deep-${index}`,
    id: `deep-${index}`,
    label: `deep-${index}`,
    subtitle: "TypeScript",
    repositoryIds: [`deep-${index}`],
    languages: ["typescript"],
  }));
  const relationships = Array.from(
    { length: cycle ? count : count - 1 },
    (_, index): TopologyRelationship => {
      const target = cycle ? (index + 1) % count : index + 1;
      return {
        ...relationship,
        source: { type: "repository", id: `deep-${index}` },
        target: { type: "repository", id: `deep-${target}` },
        evidence: `deep.ts:${index + 1}`,
      };
    },
  );
  const edges = relationships.map((relationship, index) => {
    const target = cycle ? (index + 1) % count : index + 1;
    return {
      key: `edge:deep-${index}`,
      relationshipIndex: index,
      sourceKey: `repository:deep-${index}`,
      targetKey: `repository:deep-${target}`,
      relationship,
    };
  });

  return {
    ...model,
    nodes,
    edges,
    relationships,
  };
}

function groupedSemanticsModel(): TopologyModel {
  const variants: readonly Pick<
    TopologyRelationship,
    "status" | "confidence" | "type" | "kind" | "provenance"
  >[] = [
    {
      status: "exact",
      confidence: "EXACT_TYPECHECKED",
      type: "code_dependency",
      kind: "CALLS_DIRECT",
      provenance: "GO_TYPES_USE",
    },
    {
      status: "candidate",
      confidence: "CANDIDATE",
      type: "code_dependency",
      kind: "CALLS_DIRECT",
      provenance: "TEXTUAL_CANDIDATE",
    },
    {
      status: "unresolved",
      confidence: "UNRESOLVED",
      type: "unresolved_reference",
      kind: "UNRESOLVED_REFERENCE",
      provenance: "UNRESOLVED_REFERENCE",
    },
    {
      status: "conflict",
      confidence: "CONFLICT",
      type: "code_dependency",
      kind: "CALLS_DIRECT",
      provenance: "CONFLICTING_EVIDENCE",
    },
    {
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      type: "membership",
      kind: "contains",
      provenance: "TOPOLOGY_DECLARATION",
    },
  ];
  const relationships = variants.flatMap((variant) =>
    [0, 1].map((occurrence) => ({
      ...relationship,
      ...variant,
      evidence: `source.go:${occurrence + 1}`,
    })),
  );

  return {
    ...model,
    edges: relationships.map((groupedRelationship, index) => ({
      key: `edge:semantic-${index}`,
      relationshipIndex: index,
      sourceKey: sourceNode.key,
      targetKey: targetNode.key,
      relationship: groupedRelationship,
    })),
    relationships,
  };
}

describe("TopologyFlow", () => {
  it("lays out a 5,000-node dependency chain without using the call stack", () => {
    const nodes = createTopologyFlowNodes(
      dependencyModel(5_000),
      null,
      () => {},
    );

    expect(nodes).toHaveLength(5_000);
    expect(
      nodes.every(
        (node) =>
          Number.isFinite(node.position.x) && Number.isFinite(node.position.y),
      ),
    ).toBe(true);
  });

  it("places cyclic dependencies deterministically with finite coordinates", () => {
    const cycle = dependencyModel(12, true);
    const first = createTopologyFlowNodes(cycle, null, () => {});
    const second = createTopologyFlowNodes(cycle, null, () => {});

    expect(
      first.map((node) => [node.id, node.position.x, node.position.y]),
    ).toEqual(
      second.map((node) => [node.id, node.position.x, node.position.y]),
    );
    expect(
      first.every(
        (node) =>
          Number.isFinite(node.position.x) && Number.isFinite(node.position.y),
      ),
    ).toBe(true);
    expect(new Set(first.map((node) => node.position.x)).size).toBe(1);
  });

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
    const edges = createTopologyFlowEdges(model, sourceNode.key);

    expect(edges).toHaveLength(1);
    expect(edges[0].data?.count).toBe(2);
    expect(edges[0].label).toBe("depends on ×2");
    expect(edges[0].ariaLabel).toContain("2 grouped relationships");
    expect(edges[0].source).toBe(sourceNode.key);
    expect(edges[0].target).toBe(targetNode.key);
    expect(edges[0].sourceHandle).toBe("source");
    expect(edges[0].targetHandle).toBe("target");
  });

  it("keeps grouped relationship semantics visible in canvas and accessible labels", () => {
    const expected = [
      {
        status: "exact",
        label: "depends on ×2",
        color: "#16a34a",
        ariaLabel:
          "depends on relationship from repository:source to repository:target, 2 grouped relationships",
      },
      {
        status: "candidate",
        label: "candidate dependency ×2",
        color: "#ea580c",
        ariaLabel:
          "candidate dependency relationship from repository:source to repository:target, 2 grouped relationships",
      },
      {
        status: "unresolved",
        label: "not resolved ×2",
        color: "#eab308",
        ariaLabel:
          "not resolved relationship from repository:source to repository:target, 2 grouped relationships",
      },
      {
        status: "conflict",
        label: "conflicts with ×2",
        color: "#ef4444",
        ariaLabel:
          "conflicts with relationship from repository:source to repository:target, 2 grouped relationships",
      },
      {
        status: "structural",
        label: "contains ×2",
        color: "#64748b",
        ariaLabel:
          "contains relationship from repository:source to repository:target, 2 grouped relationships",
      },
    ];
    const grouped = createTopologyFlowEdges(groupedSemanticsModel(), null);
    const focused = createTopologyFlowEdges(
      groupedSemanticsModel(),
      sourceNode.key,
    );

    expect(
      grouped.map((edge) => ({
        status: edge.data?.relationship.status,
        label: edge.label,
        color: edge.style?.stroke,
        ariaLabel: edge.ariaLabel,
      })),
    ).toEqual(expected);
    expect(focused.map((edge) => edge.style?.stroke)).toEqual(
      expected.map((edge) => edge.color),
    );
    expect(grouped.map((edge) => edge.markerEnd)).toEqual(
      expected.map((edge) => ({ type: "arrowclosed", color: edge.color })),
    );
  });

  it("highlights direct relationships without removing the surrounding map", () => {
    const edges = createTopologyFlowEdges(model, sourceNode.key);
    const nodes = createTopologyFlowNodes(model, sourceNode.key, () => {});

    expect(edges[0].style?.opacity).toBe(0.98);
    expect(edges[0].type).toBe("routed");
    expect(nodes).toHaveLength(3);
    expect(
      nodes.find((node) => node.id === unrelatedNode.key)?.data.active,
    ).toBe(false);
    expect(nodes.find((node) => node.id === sourceNode.key)?.selected).toBe(
      true,
    );
    expect(nodes.find((node) => node.id === sourceNode.key)?.data.active).toBe(
      true,
    );
    expect(nodes.every((node) => node.draggable === false)).toBe(true);
  });

  it("keeps the overview quiet and traces direct links on hover", () => {
    const overviewEdges = createTopologyFlowEdges(model, null);
    const hoveredEdges = createTopologyFlowEdges(model, null, {
      hoveredKey: sourceNode.key,
    });
    const hoveredNodes = createTopologyFlowNodes(model, null, () => {}, {
      hoveredKey: sourceNode.key,
    });

    expect(overviewEdges[0].style?.stroke).toBe("#16a34a");
    expect(overviewEdges[0].style?.opacity).toBe(0.6);
    expect(hoveredEdges[0].style?.stroke).toBe("#16a34a");
    expect(hoveredEdges[0].style?.opacity).toBe(0.9);
    expect(
      hoveredNodes.find((node) => node.id === unrelatedNode.key)?.data.active,
    ).toBe(true);
  });

  it("keeps the selected relationship distinct from its upstream and downstream trace", () => {
    const chainRelationship: TopologyRelationship = {
      ...relationship,
      source: { type: "repository", id: "target" },
      target: { type: "repository", id: "unrelated" },
      evidence: "target.go:30",
    };
    const chainModel: TopologyModel = {
      ...model,
      edges: [
        ...model.edges,
        {
          key: "edge:chain",
          relationshipIndex: 2,
          sourceKey: targetNode.key,
          targetKey: unrelatedNode.key,
          relationship: chainRelationship,
        },
      ],
    };

    const edges = createTopologyFlowEdges(chainModel, sourceNode.key);

    expect(
      edges.find((edge) => edge.source === sourceNode.key)?.style?.opacity,
    ).toBe(0.98);
    expect(
      edges.find((edge) => edge.source === targetNode.key)?.style?.opacity,
    ).toBe(0.58);
  });

  it("opens with a profile and a repository group instead of a wall of cards", () => {
    const overviewModel: TopologyModel = {
      ...model,
      nodes: [profileNode, worktreeNode, sourceNode, targetNode],
      edges: [
        ...model.edges,
        {
          key: "edge:self",
          relationshipIndex: 2,
          sourceKey: sourceNode.key,
          targetKey: sourceNode.key,
          relationship: { ...relationship, target: relationship.source },
        },
      ],
    };

    const nodes = createTopologyFlowNodes(overviewModel, null, () => {}, {
      showWorktrees: false,
    });
    const edges = createTopologyFlowEdges(overviewModel, null, {
      showWorktrees: false,
    });

    expect(nodes.map((node) => node.id)).not.toContain(worktreeNode.key);
    expect(nodes.map((node) => node.id)).not.toContain(sourceNode.key);
    expect(
      edges.some(
        (edge) =>
          edge.source === profileNode.key &&
          edge.target === "topology:repository-group:default",
      ),
    ).toBe(true);
    expect(
      edges.find(
        (edge) =>
          edge.source === profileNode.key &&
          edge.target === "topology:repository-group:default",
      )?.label,
    ).toBe("contains");
  });

  it("lays out grouped direct relationships and omits internal self-edges", () => {
    const overviewModel: TopologyModel = {
      ...model,
      nodes: [profileNode, worktreeNode, sourceNode, targetNode],
      edges: [
        ...model.edges,
        {
          key: "edge:self",
          relationshipIndex: 2,
          sourceKey: sourceNode.key,
          targetKey: sourceNode.key,
          relationship: { ...relationship, target: relationship.source },
        },
      ],
    };
    const options = { expandedProfiles: ["default"], showWorktrees: false };
    const beforeSelection = createTopologyFlowEdges(
      overviewModel,
      null,
      options,
    );
    expect(() =>
      createTopologyFlowNodes(overviewModel, sourceNode.key, () => {}, options),
    ).not.toThrow();
    const nodes = createTopologyFlowNodes(
      overviewModel,
      sourceNode.key,
      () => {},
      options,
    );
    const edges = createTopologyFlowEdges(
      overviewModel,
      sourceNode.key,
      options,
    );

    expect(nodes.map((node) => node.id)).toContain(sourceNode.key);
    expect(nodes.map((node) => node.id)).toContain(targetNode.key);
    expect(
      beforeSelection.some(
        (edge) => edge.data?.relationship.type === "code_dependency",
      ),
    ).toBe(true);
    expect(
      edges.some(
        (edge) =>
          edge.source === sourceNode.key && edge.target === targetNode.key,
      ),
    ).toBe(true);
    expect(
      edges.some(
        (edge) =>
          edge.source === sourceNode.key && edge.target === sourceNode.key,
      ),
    ).toBe(false);
  });
});
