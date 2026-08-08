import { describe, expect, it } from "vitest";

import {
  filterNeighborhoodEdges,
  filterSymbols,
} from "@/components/ViewerChrome";
import type { NeighborhoodResponse, SymbolView } from "@/api/client";

const symbol = (
  stableKey: string,
  repository: string,
  kind: string,
): SymbolView => ({
  stableKey,
  canonicalIdentity: stableKey,
  repository,
  repositoryPath: `/${repository}`,
  package: `${repository}/pkg`,
  modulePath: repository,
  file: "src/index.ts",
  language: "typescript",
  name: stableKey,
  qualifiedName: `${repository}.${stableKey}`,
  kind,
  signature: "function ()",
  startLine: 1,
  endLine: 2,
});

const neighborhood: NeighborhoodResponse = {
  snapshotId: 7,
  root: "symbol-a",
  direction: "both",
  depth: 1,
  truncated: false,
  nodes: [symbol("symbol-a", "repo-a", "function")],
  edges: [
    {
      source: "symbol-a",
      target: "symbol-b",
      kind: "CALLS_DIRECT",
      confidence: "EXACT_TYPECHECKED",
      provenance: "GO_TYPES_USE",
    },
    {
      source: "symbol-c",
      target: "symbol-a",
      kind: "REFERENCES",
      confidence: "CANDIDATE",
      provenance: "GO_TYPES_USE",
    },
  ],
};

describe("viewer chrome filters", () => {
  it("filters search results by repository and symbol kind together", () => {
    const symbols = [
      symbol("a", "repo-a", "function"),
      symbol("b", "repo-a", "class"),
      symbol("c", "repo-b", "function"),
    ];

    expect(
      filterSymbols(symbols, "repo-a", "function").map(
        (item) => item.stableKey,
      ),
    ).toEqual(["a"]);
    expect(
      filterSymbols(symbols, "__all__", "class").map((item) => item.stableKey),
    ).toEqual(["b"]);
  });

  it("filters neighborhood edges by confidence without changing the response", () => {
    expect(filterNeighborhoodEdges(neighborhood, "CANDIDATE")).toHaveLength(1);
    expect(filterNeighborhoodEdges(neighborhood, "__all__")).toHaveLength(2);
    expect(neighborhood.edges).toHaveLength(2);
  });
});
