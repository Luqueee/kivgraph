import { useThree } from "@react-three/fiber";
import { useEffect, useRef } from "react";
import { setTextLabelsHidden } from "./text-labels";

/** Interaction trades a little sharpness for a cheaper camera frame. */
const INTERACTION_DPR = 1;
const IDLE_MS = 700;

const WAKING_EVENTS = ["pointermove", "wheel"] as const;
const POINTER_RELEASE_EVENTS = ["pointerup", "pointercancel"] as const;
interface ResizableRenderer {
  setSize(width: number, height: number, updateStyle?: boolean): void;
}

interface CanvasSize {
  readonly clientWidth: number;
  readonly clientHeight: number;
}

type AdvanceFrame = (timestamp: number, runGlobalEffects: boolean) => void;

/** Returns the DPR to restore, or null when resizing would be redundant. */
export function savedInteractionDpr(
  currentDpr: number,
  targetDpr: number,
): number | null {
  return currentDpr > targetDpr ? currentDpr : null;
}

/** Rebuilds the drawing buffer and paints it before the browser can show it. */
export function resizeAndPaint(
  renderer: ResizableRenderer,
  canvas: CanvasSize,
  advance: AdvanceFrame,
): void {
  renderer.setSize(canvas.clientWidth, canvas.clientHeight, false);
  advance(performance.now(), true);
}

/**
 * Moves R3F into its interactive loop once per interaction. Calling
 * `setFrameloop` repeatedly resets R3F's clock, which makes camera-controls'
 * delta shrink to zero while pointermove events arrive.
 */
export function wakeFrame(
  working: { current: boolean },
  setFrameloop: (frameloop: "always") => void,
): void {
  if (working.current) return;
  working.current = true;
  setFrameloop("always");
}

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
 * `demand` once it has been still. While the pointer moves it also caps the
 * renderer DPR at `1` - a hover on a HiDPI screen otherwise redraws four
 * times the pixels of the frame the reader is waiting for - and a held
 * pointer additionally pauses scene picking and hides the text labels.
 * Waking on the raw DOM events rather than on the controls' own events keeps
 * this independent of how Reagraph drives them.
 */
export function FrameGovernor(): null {
  const gl = useThree((state) => state.gl);
  const scene = useThree((state) => state.scene);
  const frameloop = useThree((state) => state.frameloop);
  const setFrameloop = useThree((state) => state.setFrameloop);
  const setEvents = useThree((state) => state.setEvents);
  const advance = useThree((state) => state.advance);
  const interactionDpr = useRef<number | null>(null);
  const working = useRef(false);
  const dragging = useRef(false);

  useEffect(() => {
    const canvas = gl.domElement;
    let idle = 0;
    const resizeRenderer = (): void => {
      resizeAndPaint(gl, canvas, advance);
    };
    const hideLabels = (hidden: boolean): void => {
      if (setTextLabelsHidden(scene, hidden) > 0)
        advance(performance.now(), true);
    };
    const restoreDpr = (): void => {
      const previousDpr = interactionDpr.current;
      if (previousDpr === null) return;
      interactionDpr.current = null;
      gl.setPixelRatio(previousDpr);
      resizeRenderer();
    };
    const lowerDpr = (): void => {
      if (interactionDpr.current !== null) return;
      const previousDpr = savedInteractionDpr(
        gl.getPixelRatio(),
        INTERACTION_DPR,
      );
      if (previousDpr === null) return;
      interactionDpr.current = previousDpr;
      gl.setPixelRatio(INTERACTION_DPR);
      resizeRenderer();
    };
    const rest = (): void => {
      working.current = false;
      setFrameloop("demand");
      restoreDpr();
    };
    const wake = (): void => {
      lowerDpr();
      wakeFrame(working, setFrameloop);
      window.clearTimeout(idle);
      idle = window.setTimeout(rest, IDLE_MS);
    };
    const press = (): void => {
      if (!dragging.current) {
        dragging.current = true;
        lowerDpr();
        hideLabels(true);
        setEvents({ enabled: false });
      }
      wake();
    };
    const release = (): void => {
      if (!dragging.current) return;
      dragging.current = false;
      hideLabels(false);
      setEvents({ enabled: true });
    };
    idle = window.setTimeout(rest, IDLE_MS);
    for (const event of WAKING_EVENTS) {
      canvas.addEventListener(event, wake, { passive: true });
    }
    canvas.addEventListener("pointerdown", press, { passive: true });
    for (const event of POINTER_RELEASE_EVENTS) {
      window.addEventListener(event, release, { passive: true });
    }
    window.addEventListener("blur", release);
    return () => {
      window.clearTimeout(idle);
      for (const event of WAKING_EVENTS) {
        canvas.removeEventListener(event, wake);
      }
      canvas.removeEventListener("pointerdown", press);
      for (const event of POINTER_RELEASE_EVENTS) {
        window.removeEventListener(event, release);
      }
      window.removeEventListener("blur", release);
      const wasDragging = dragging.current;
      dragging.current = false;
      if (wasDragging) hideLabels(false);
      setEvents({ enabled: true });
      restoreDpr();
      setFrameloop("always");
    };
  }, [advance, gl, scene, setEvents, setFrameloop]);

  // Every commit of the canvas re-applies its own `frameloop` prop, which
  // Reagraph leaves unset - so any re-render of the viewer silently puts the
  // loop back to `always`. Claiming it back is what makes the setting stick.
  useEffect(() => {
    if (frameloop === "always" && !working.current) setFrameloop("demand");
  }, [frameloop, setFrameloop]);

  return null;
}
