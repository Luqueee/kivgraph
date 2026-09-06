import { describe, expect, it } from "vitest";

import type { TopologyResponse } from "@/api/client";
import {
  createTopologyModel,
  displayWorktreeLabel,
  filterTopology,
  type TopologyFilters,
} from "@/topology";

const topology: TopologyResponse = {
  apiVersion: "v1",
  topologyVersion: 1,
  status: "ready",
  selectedProfiles: ["default", "other"],
  profiles: [
    {
      id: "default",
      generationId: "000007",
      status: "ready",
      compositionComplete: true,
      worktrees: ["wt-shared"],
    },
    {
      id: "other",
      generationId: "000008",
      status: "stale",
      compositionComplete: true,
      reason: "source changed",
      worktrees: ["wt-shared", "wt-other"],
    },
  ],
  repositories: [
    { id: "repo-z", name: "Zeta", languages: ["rust"] },
    { id: "repo-a", name: "Alpha", languages: ["go", "typescript"] },
  ],
  worktrees: [
    { id: "wt-other", repository: "repo-z", path: "/src/zeta" },
    { id: "wt-shared", repository: "repo-a", path: "/src/alpha" },
  ],
  sources: [
    {
      profile: "other",
      repository: "repo-a",
      worktree: "wt-shared",
      status: "stale",
      reason: "working tree is dirty",
      indexed: {
        id: "obs-old",
        worktree: "wt-shared",
        commit: "abc123",
        branch: "main",
        dirty: false,
        contentDigest: "digest-old",
      },
    },
  ],
  sharedInputs: [
    {
      type: "worktree",
      id: "wt-shared",
      repository: "repo-a",
      owners: ["default", "other"],
      status: "stale",
      reason: "working tree is dirty",
    },
  ],
  relationships: [
    {
      profile: "default",
      type: "membership",
      source: { type: "profile", id: "default" },
      target: { type: "worktree", id: "wt-shared" },
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      provenance: "TOPOLOGY_DECLARATION",
    },
    {
      profile: "default",
      type: "code_dependency",
      source: { type: "repository", id: "repo-a" },
      target: { type: "repository", id: "repo-z" },
      kind: "CALLS_DIRECT",
      status: "exact",
      confidence: "EXACT_TYPECHECKED",
      provenance: "GO_TYPES_USE",
      evidence: "evidence-1",
    },
    {
      profile: "other",
      type: "unresolved_reference",
      source: { type: "repository", id: "repo-z" },
      status: "unresolved",
      confidence: "UNRESOLVED",
      provenance: "UNRESOLVED_REFERENCE",
      reason: "missing package",
    },
    {
      profile: "default",
      generationId: "000007",
      type: "shared_input_usage",
      source: { type: "profile", id: "default" },
      target: { type: "shared_input", id: "worktree:wt-shared" },
      kind: "uses",
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      provenance: "TOPOLOGY_DECLARATION",
    },
    {
      profile: "other",
      generationId: "000008",
      type: "shared_input_usage",
      source: { type: "profile", id: "other" },
      target: { type: "shared_input", id: "worktree:wt-shared" },
      kind: "uses",
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      provenance: "TOPOLOGY_DECLARATION",
    },
  ],
  completeness: { complete: true, truncated: false },
};

const allFilters: TopologyFilters = {
  query: "",
  profile: "__all__",
  worktree: "__all__",
  repository: "__all__",
  language: "__all__",
  edgeKind: "__all__",
};

describe("topology model", () => {
  it("keeps historical worktree prefixes out of display labels", () => {
    expect(displayWorktreeLabel("legacy:frontend")).toBe("frontend");
    expect(displayWorktreeLabel("frontend")).toBe("frontend");
  });

  it("uses typed identities and deterministic layer positions", () => {
    const model = createTopologyModel(topology);

    expect(model.nodes.map((node) => node.key)).toEqual([
      "profile:default",
      "profile:other",
      "worktree:wt-other",
      "worktree:wt-shared",
      "repository:repo-a",
      "repository:repo-z",
      "shared_input:worktree:wt-shared",
    ]);
    expect(
      model.layout.nodes.map((node) => [node.key, node.x, node.y]),
    ).toEqual([
      ["profile:default", 24, 24],
      ["profile:other", 24, 166],
      ["worktree:wt-other", 288, 24],
      ["worktree:wt-shared", 288, 166],
      ["repository:repo-a", 552, 24],
      ["repository:repo-z", 552, 166],
      ["shared_input:worktree:wt-shared", 288, 308],
    ]);
    expect(model.layout.width).toBe(804);
  });

  it.each([
    ["current", "ready"],
    ["ready", "current"],
  ] as const)(
    "selects the same status for sources ordered %s then %s",
    (firstStatus, secondStatus) => {
      const model = createTopologyModel({
        ...topology,
        sources: [
          { ...topology.sources[0], status: firstStatus },
          { ...topology.sources[0], status: secondStatus },
        ],
      });

      expect(
        model.nodes.find((node) => node.key === "worktree:wt-shared")?.status,
      ).toBe("current");
    },
  );

  it("keeps unresolved relationships without fabricating a target", () => {
    const model = createTopologyModel({
      ...topology,
      relationships: [
        {
          ...topology.relationships[2],
          source: { type: "unknown", id: "not-a-node" },
        },
        topology.relationships[2],
      ],
    });

    expect(model.edges).toHaveLength(1);
    const unresolvedEdge = model.edges.find(
      (edge) => edge.relationship.type === "unresolved_reference",
    );
    expect(unresolvedEdge?.targetKey).toBeUndefined();
    expect(model.unrenderedRelationships).toHaveLength(1);
    expect(model.unrenderedRelationships[0].source.id).toBe("not-a-node");
  });

  it("combines topology filters without changing the response data", () => {
    const filtered = filterTopology(createTopologyModel(topology), {
      ...allFilters,
      profile: "other",
      language: "rust",
      edgeKind: "CALLS_DIRECT",
    });

    expect(filtered.nodes.map((node) => node.key)).toEqual([
      "profile:other",
      "worktree:wt-other",
      "repository:repo-z",
    ]);
    expect(filtered.edges).toHaveLength(0);
    expect(topology.relationships).toHaveLength(5);
  });

  it("keeps relationships and profile boundaries in the selected profile scope", () => {
    const variants: TopologyResponse = {
      ...topology,
      selectedProfiles: ["A", "B"],
      profiles: [
        {
          id: "A",
          generationId: "000101",
          status: "ready",
          compositionComplete: true,
          worktrees: ["front-a", "back-a"],
        },
        {
          id: "B",
          generationId: "000102",
          status: "ready",
          compositionComplete: true,
          worktrees: ["front-b", "back-b"],
        },
      ],
      repositories: [
        { id: "front", name: "Front", languages: ["typescript"] },
        { id: "back", name: "Back", languages: ["go"] },
      ],
      worktrees: [
        { id: "front-a", repository: "front", path: "/src/a/front" },
        { id: "back-a", repository: "back", path: "/src/a/back" },
        { id: "front-b", repository: "front", path: "/src/b/front" },
        { id: "back-b", repository: "back", path: "/src/b/back" },
      ],
      sources: [
        {
          profile: "A",
          repository: "front",
          worktree: "front-a",
          status: "current",
        },
        {
          profile: "B",
          repository: "front",
          worktree: "front-b",
          status: "stale",
        },
      ],
      sharedInputs: [],
      relationships: [
        {
          profile: "A",
          type: "code_dependency",
          source: { type: "repository", id: "front" },
          target: { type: "repository", id: "back" },
          kind: "CALLS_DIRECT",
          status: "candidate",
          confidence: "CANDIDATE",
          provenance: "GO_TYPES_USE",
          evidence: "a.go:10",
        },
        {
          profile: "B",
          type: "code_dependency",
          source: { type: "repository", id: "front" },
          target: { type: "repository", id: "back" },
          kind: "CALLS_DIRECT",
          status: "exact",
          confidence: "EXACT_TYPECHECKED",
          provenance: "GO_TYPES_USE",
          evidence: "b.go:10",
        },
      ],
    };

    const filtered = filterTopology(createTopologyModel(variants), {
      ...allFilters,
      profile: "A",
    });

    expect(filtered.nodes.map((node) => node.key)).toEqual([
      "profile:A",
      "worktree:back-a",
      "worktree:front-a",
      "repository:back",
      "repository:front",
    ]);
    expect(filtered.relationships).toEqual([
      expect.objectContaining({
        profile: "A",
        status: "candidate",
        evidence: "a.go:10",
      }),
    ]);
    expect(filtered.response?.sources).toEqual([
      expect.objectContaining({
        profile: "A",
        worktree: "front-a",
        status: "current",
      }),
    ]);
    expect(filtered.boundaries).toEqual([]);
  });

  it("reuses the profile-scoped model when only query filters change", () => {
    const model = createTopologyModel(topology);
    const first = filterTopology(model, {
      ...allFilters,
      profile: "other",
    });
    const second = filterTopology(model, {
      ...allFilters,
      profile: "other",
      query: "alpha",
    });

    expect(second.response).toBe(first.response);
    expect(second.nodes.find((node) => node.key === "repository:repo-a")).toBe(
      first.nodes.find((node) => node.key === "repository:repo-a"),
    );
  });

  it("does not retain relationships for an unknown profile", () => {
    const filtered = filterTopology(createTopologyModel(topology), {
      ...allFilters,
      profile: "missing",
    });

    expect(filtered.nodes).toEqual([]);
    expect(filtered.relationships).toEqual([]);
    expect(filtered.response?.sharedInputs).toEqual([]);
  });
});
