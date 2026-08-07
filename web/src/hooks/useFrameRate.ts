import { useEffect, useState } from "react";

/** How often the readout refreshes. Per frame would itself cost frames. */
const SAMPLE_MS = 500;

/**
 * Frames per second of the render loop, sampled twice a second.
 *
 * Counts `requestAnimationFrame` callbacks, which is what the graph canvas
 * draws on: while the scene is being built the browser stops serving them and
 * the number drops, which is exactly the stall a reader wants to see.
 */
export function useFrameRate(): number {
  const [fps, setFps] = useState(0);

  useEffect(() => {
    let frames = 0;
    let since = performance.now();
    let handle = requestAnimationFrame(function tick(now: number) {
      frames += 1;
      const elapsed = now - since;
      if (elapsed >= SAMPLE_MS) {
        setFps(Math.round((frames * 1000) / elapsed));
        frames = 0;
        since = now;
      }
      handle = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(handle);
  }, []);

  return fps;
}
