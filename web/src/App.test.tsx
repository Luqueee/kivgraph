import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import App from "@/App";

describe("viewer shell", () => {
  it("renders only the graph preview surface", () => {
    const markup = renderToStaticMarkup(<App />);

    expect(markup).toContain("Interactive Reagraph graph preview");
    expect(markup).toContain("7 nodes");
    expect(markup).toContain("4 edges");
    expect(markup).not.toContain("<header");
    expect(markup).not.toContain("<section");
  });
});
