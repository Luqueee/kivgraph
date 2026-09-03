import { describe, expect, it } from "vitest";

import type { TopologyResponse } from "@/api/client";
import {
  createTopologyModel,
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
      worktrees: ["wt-shared"],
    },
    {
      id: "other",
      generationId: "000008",
      status: "stale",
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
    { type: "worktree", id: "wt-shared", owners: ["default", "other"] },
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
  });

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

    expect(model.edges).toHaveLength(3);
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
    expect(topology.relationships).toHaveLength(3);
  });
});
