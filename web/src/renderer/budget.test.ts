import { describe, expect, it } from "vitest";

import {
  MAX_TILE_BUDGET,
  MIN_TILE_BUDGET,
  TILE_BUDGET_STEP,
  tileBudget,
} from "@/renderer/budget";

describe("tile budget", () => {
  it("passes a value the server and the adapter both accept", () => {
    expect(tileBudget(1_200, 50_000)).toBe(1_200);
  });

  // Asking for more than the adapter materialises only earns a rejected tile.
  it("never exceeds the adapter limit", () => {
    expect(tileBudget(50_000, 50_000)).toBe(MAX_TILE_BUDGET);
  });

  // A snapshot may offer fewer nodes per tile than the adapter can draw.
  it("never exceeds what the snapshot offers", () => {
    expect(tileBudget(2_000, 500)).toBe(500);
  });

  it("keeps a floor so a view always shows something", () => {
    expect(tileBudget(0, 50_000)).toBe(MIN_TILE_BUDGET);
    expect(tileBudget(-100, 50_000)).toBe(MIN_TILE_BUDGET);
    expect(tileBudget(Number.NaN, 50_000)).toBe(MIN_TILE_BUDGET);
  });

  it("rounds to the slider step so equal positions request equal tiles", () => {
    expect(tileBudget(1_249, 50_000) % TILE_BUDGET_STEP).toBe(0);
    expect(tileBudget(1_249, 50_000)).toBe(1_200);
    expect(tileBudget(1_251, 50_000)).toBe(1_300);
  });
});
