import { useFrameRate } from "@/hooks/useFrameRate";

/**
 * The frame counter owns its own state.
 *
 * It refreshes twice a second, and a re-render of the viewer is a re-render of
 * the graph canvas: React Three Fiber commits, asks for a frame and resets the
 * frame loop to `always`. Keeping the number in its own component is what lets
 * the scene stay still while the readout keeps ticking.
 */
export function FrameRate(): React.ReactElement {
  return <>{useFrameRate()} fps</>;
}
