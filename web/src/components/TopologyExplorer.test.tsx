import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { TopologyExplorer } from "@/components/TopologyExplorer";

describe("TopologyExplorer", () => {
  it("exposes a read-only loading surface before the topology request resolves", () => {
    const markup = renderToStaticMarkup(<TopologyExplorer />);

    expect(markup).toContain("Topology explorer");
    expect(markup).toContain("loading topology");
    expect(markup).toContain("read-only");
    expect(markup).not.toContain("<canvas");
  });
});
