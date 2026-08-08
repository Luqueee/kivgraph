import { describe, expect, it } from "vitest";

import {
  resizeAndPaint,
  savedInteractionDpr,
  wakeFrame,
} from "@/renderer/frame-governor";

describe("frame governor", () => {
  it("changes to the interactive loop only once per gesture", () => {
    const working = { current: false };
    const modes: string[] = [];

    wakeFrame(working, (mode) => modes.push(mode));
    wakeFrame(working, (mode) => modes.push(mode));

    expect(working.current).toBe(true);
    expect(modes).toEqual(["always"]);
  });

  it("does not resize when the renderer already uses the target DPR", () => {
    expect(savedInteractionDpr(1, 1)).toBeNull();
    expect(savedInteractionDpr(0.75, 1)).toBeNull();
    expect(savedInteractionDpr(1.25, 1)).toBe(1.25);
  });

  it("paints synchronously after rebuilding the drawing buffer", () => {
    const calls: string[] = [];

    resizeAndPaint(
      {
        setSize: (width, height, updateStyle) => {
          calls.push(`resize:${width}:${height}:${updateStyle}`);
        },
      },
      { clientWidth: 640, clientHeight: 360 },
      (_timestamp, runGlobalEffects) => {
        calls.push(`paint:${runGlobalEffects}`);
      },
    );

    expect(calls).toEqual(["resize:640:360:false", "paint:true"]);
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
