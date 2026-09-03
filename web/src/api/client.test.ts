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
  shared_inputs: [],
  relationships: [],
  completeness: { complete: true, truncated: false },
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("fetchTopology", () => {
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
