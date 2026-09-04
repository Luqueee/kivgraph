import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import type { TopologyProfile } from "@/api/client";
import {
  pinnedTopologyURL,
  topologyFilterLabelID,
  TopologyExplorer,
} from "@/components/TopologyExplorer";

describe("TopologyExplorer", () => {
  it("exposes a read-only loading surface before the topology request resolves", () => {
    const markup = renderToStaticMarkup(<TopologyExplorer />);

    expect(markup).toContain("Topology explorer");
    expect(markup).toContain("loading topology");
    expect(markup).toContain("read-only");
    expect(markup).not.toContain("<canvas");
  });

  it("pins topology links to every displayed profile generation", () => {
    const profiles: readonly Pick<TopologyProfile, "id" | "generationId">[] = [
      { id: "other", generationId: "000008" },
      { id: "default", generationId: "000007" },
    ];

    expect(pinnedTopologyURL(["other", "default"], profiles)).toBe(
      "/api/v1/topology?profile=default&profile=other&generation=default%3A000007&generation=other%3A000008",
    );
    expect(pinnedTopologyURL([], profiles)).toBe("/api/v1/topology");
  });

  it("creates a single accessible label ID for multi-word filters", () => {
    expect(topologyFilterLabelID("edge kind")).toBe(
      "topology-filter-edge-kind",
    );
  });
});
