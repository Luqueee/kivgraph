import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import App from "@/App";
import { Button } from "@/components/ui/button";

describe("web foundation", () => {
  it("renders the viewer shell without external services", () => {
    const markup = renderToStaticMarkup(<App />);

    expect(markup).toContain("Ladygraph");
    expect(markup).toContain("Read-only graph viewer");
    expect(markup).toContain("shadcn/ui");
    expect(markup).toContain("GPU buffers");
  });

  it("renders the CLI-generated Button primitive with variants", () => {
    const markup = renderToStaticMarkup(
      <Button variant="outline">Inspect</Button>,
    );

    expect(markup).toContain('data-slot="button"');
    expect(markup).toContain("border-border");
    expect(markup).toContain("Inspect");
  });
});
