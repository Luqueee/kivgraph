import { afterEach, describe, expect, it, vi } from "vitest";

import { fetchTopology } from "@/api/client";

const topologyPayload = {
  api_version: "v1",
  topology_version: 1,
  status: "ready",
  generation_id: "000007",
  selected_profiles: ["default"],
  profiles: [
    {
      id: "default",
      generation_id: "000007",
      status: "ready",
      composition_complete: true,
      worktrees: ["wt-main"],
    },
  ],
  repositories: [
    { id: "repo-a", name: "Repository A", languages: ["go", "typescript"] },
  ],
  worktrees: [
    {
      id: "wt-main",
      repository: "repo-a",
      path: "/workspace/repo-a",
      git: { common_directory: "/workspace/.git" },
    },
  ],
  sources: [],
  shared_inputs: [
    {
      type: "worktree",
      id: "shared-main",
      repository: "repo-a",
      owners: ["default"],
      status: "stale",
      reason: "shared content changed after indexing",
    },
  ],
  relationships: [
    {
      profile: "default",
      generation_id: "000007",
      type: "shared_input_invalidation",
      source: { type: "shared_input", id: "worktree:shared-main" },
      target: { type: "profile", id: "default" },
      kind: "invalidates",
      status: "structural",
      confidence: "STRUCTURAL_CERTAIN",
      provenance: "SOURCE_INVALIDATION",
      reason: "shared content changed after indexing",
    },
  ],
  completeness: { complete: true, truncated: false },
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("fetchTopology", () => {
  it("requests every published profile when no selection is supplied", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(topologyPayload), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await fetchTopology();

    expect(fetchMock).toHaveBeenCalledWith("/api/v1/topology?profile=*", {
      signal: undefined,
      headers: { Accept: "application/json" },
    });
  });

  it("decodes pinned profiles and preserves the typed topology fields", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(topologyPayload), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const topology = await fetchTopology({
      profiles: ["default", "other"],
      generationPins: { other: "000008", default: "000007" },
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/topology?profile=default&profile=other&generation=default%3A000007&generation=other%3A000008",
      {
        signal: undefined,
        headers: { Accept: "application/json" },
      },
    );
    expect(topology.generationId).toBe("000007");
    expect(topology.repositories[0]).toEqual({
      id: "repo-a",
      name: "Repository A",
      languages: ["go", "typescript"],
    });
    expect(topology.worktrees[0].git).toEqual({
      commonDirectory: "/workspace/.git",
    });
    expect(topology.profiles[0].compositionComplete).toBe(true);
    expect(topology.sharedInputs[0]).toEqual({
      type: "worktree",
      id: "shared-main",
      repository: "repo-a",
      owners: ["default"],
      status: "stale",
      reason: "shared content changed after indexing",
    });
    expect(topology.relationships[0]).toEqual(
      expect.objectContaining({
        generationId: "000007",
        type: "shared_input_invalidation",
        kind: "invalidates",
      }),
    );
  });

  it("rejects a profile without its composition completeness", async () => {
    const { composition_complete: _compositionComplete, ...incompleteProfile } =
      topologyPayload.profiles[0];
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ ...topologyPayload, profiles: [incompleteProfile] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchTopology()).rejects.toEqual(
      expect.objectContaining({
        code: "INVALID_RESPONSE",
        status: 200,
        message: expect.stringContaining("profiles[0].composition_complete"),
      }),
    );
  });

  it("surfaces generation changes as the server error instead of rendering stale data", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: {
            code: "GENERATION_CHANGED",
            message: "refresh the selected profile",
          },
        }),
        { status: 409, statusText: "Conflict" },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = fetchTopology();

    await expect(result).rejects.toEqual(
      expect.objectContaining({
        code: "GENERATION_CHANGED",
        status: 409,
        message: "refresh the selected profile",
      }),
    );
  });

  it("rejects a successful response whose topology envelope is malformed", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ status: "ready" })));
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchTopology()).rejects.toEqual(
      expect.objectContaining({
        code: "INVALID_RESPONSE",
        status: 200,
      }),
    );
  });
});
