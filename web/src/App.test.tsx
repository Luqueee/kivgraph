import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import App from "@/App";

describe("viewer shell", () => {
  it("renders only the graph surface and its detail controls", () => {
    const markup = renderToStaticMarkup(<App />);

    expect(markup).toContain("Graph explorer");
    expect(markup).toContain("topology");
    expect(markup).toContain("loading snapshot");
    expect(markup).toContain("Search symbols");
    expect(markup).toContain("repositories");
    expect(markup).toContain("symbols");
    expect(markup).not.toContain("<header");
    expect(markup).not.toContain("<section");
  });
});
