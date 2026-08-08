import { useThree } from "@react-three/fiber";
import { useEffect, useRef } from "react";

/** Interaction is over once the pointer has been still for this long. */
const IDLE_MS = 700;

const WAKING_EVENTS = [
  "pointerdown",
  "pointermove",
  "pointerup",
  "wheel",
] as const;

/**
 * Renders the scene only when something changed.
 *
 * A published graph does not move: the layout is computed once and frozen, so
 * redrawing a still picture sixty times a second buys nothing and costs a core.
 * React Three Fiber already asks for a frame whenever the React tree commits -
 * a new tile, a highlight - which leaves the camera as the only other source
 * of motion, and the camera only moves while the pointer is on the canvas.
 *
 * So: `demand` at rest, `always` while the pointer is working, and back to
 * `demand` once it has been still. Waking on the raw DOM events rather than on
 * the controls' own events keeps this independent of how Reagraph drives them.
 */
export function FrameGovernor(): null {
  const gl = useThree((state) => state.gl);
  const frameloop = useThree((state) => state.frameloop);
  const setFrameloop = useThree((state) => state.setFrameloop);
  const working = useRef(false);

  useEffect(() => {
    const canvas = gl.domElement;
    let idle = 0;
    const rest = (): void => {
      working.current = false;
      setFrameloop("demand");
    };
    const wake = (): void => {
      working.current = true;
      setFrameloop("always");
      window.clearTimeout(idle);
      idle = window.setTimeout(rest, IDLE_MS);
    };
    idle = window.setTimeout(rest, IDLE_MS);
    for (const event of WAKING_EVENTS) {
      canvas.addEventListener(event, wake, { passive: true });
    }
    return () => {
      window.clearTimeout(idle);
      for (const event of WAKING_EVENTS) {
        canvas.removeEventListener(event, wake);
      }
      setFrameloop("always");
    };
  }, [gl, setFrameloop]);

  // Every commit of the canvas re-applies its own `frameloop` prop, which
  // Reagraph leaves unset - so any re-render of the viewer silently puts the
  // loop back to `always`. Claiming it back is what makes the setting stick.
  useEffect(() => {
    if (frameloop === "always" && !working.current) setFrameloop("demand");
  }, [frameloop, setFrameloop]);

  return null;
}
