import { describe, expect, it } from "vitest";

import { isEmptySnapshot } from "@/components/GraphPreview";
import type { SnapshotMeta } from "@/api/client";

const emptyMeta: SnapshotMeta = {
  snapshotId: 7,
  status: "ready",
  counts: {
    repositories: 0,
    packages: 0,
    files: 0,
    symbols: 0,
    edges: 0,
    unresolved: 0,
  },
  layout: null,
};

describe("snapshot state", () => {
  it("recognizes an empty published snapshot without treating missing layout as an error", () => {
    expect(isEmptySnapshot(emptyMeta)).toBe(true);
  });

  it("does not classify a populated snapshot without layout as empty", () => {
    expect(
      isEmptySnapshot({
        ...emptyMeta,
        counts: { ...emptyMeta.counts, symbols: 1 },
      }),
    ).toBe(false);
  });
});
