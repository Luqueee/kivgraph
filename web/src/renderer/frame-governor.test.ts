import { describe, expect, it } from "vitest";

import { wakeFrame } from "@/renderer/frame-governor";

describe("frame governor", () => {
  it("changes to the interactive loop only once per gesture", () => {
    const working = { current: false };
    const modes: string[] = [];

    wakeFrame(working, (mode) => modes.push(mode));
    wakeFrame(working, (mode) => modes.push(mode));

    expect(working.current).toBe(true);
    expect(modes).toEqual(["always"]);
  });

  it("can wake a new gesture after the idle transition", () => {
    const working = { current: false };
    let calls = 0;

    wakeFrame(working, () => {
      calls += 1;
    });
    working.current = false;
    wakeFrame(working, () => {
      calls += 1;
    });

    expect(calls).toBe(2);
  });
});
