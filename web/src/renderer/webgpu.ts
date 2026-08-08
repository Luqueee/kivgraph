import type { GLProps } from "@react-three/fiber";
import { WebGPURenderer } from "three/webgpu";

export type RendererBackend = "webgpu" | "webgl";

export interface RendererSelection {
  readonly backend: RendererBackend;
  /** Human-readable reason shown when WebGL is the deliberate fallback. */
  readonly reason: string | null;
}

export interface GpuAdapterProbe {
  requestAdapter(options?: {
    readonly powerPreference?: "low-power" | "high-performance";
  }): Promise<unknown | null>;
}

function errorMessage(error: unknown): string {
  return error instanceof Error && error.message !== ""
    ? error.message
    : "unknown WebGPU probe error";
}

/**
 * Selects the renderer from an injectable probe so capability decisions stay
 * testable without a browser or a fake WebGPU implementation in Vitest.
 */
export async function selectRendererBackend(
  gpu: GpuAdapterProbe | null,
): Promise<RendererSelection> {
  if (gpu === null) {
    return {
      backend: "webgl",
      reason: "WebGPU is not exposed by this browser",
    };
  }
  try {
    const adapter = await gpu.requestAdapter({
      powerPreference: "high-performance",
    });
    if (adapter === null) {
      return {
        backend: "webgl",
        reason: "WebGPU has no usable adapter on this device",
      };
    }
    return { backend: "webgpu", reason: null };
  } catch (error: unknown) {
    return {
      backend: "webgl",
      reason: `WebGPU probe failed: ${errorMessage(error)}`,
    };
  }
}

/**
 * Probes the actual browser capability before mounting Reagraph. A missing or
 * rejected adapter is visible to the reader and never becomes a silent
 * performance claim.
 */
export function detectRendererBackend(): Promise<RendererSelection> {
  if (typeof navigator === "undefined") {
    return selectRendererBackend(null);
  }
  const navigatorWithGpu = navigator as Navigator & {
    readonly gpu?: GpuAdapterProbe;
  };
  return selectRendererBackend(navigatorWithGpu.gpu ?? null);
}

/**
 * R3F accepts a renderer factory, but Reagraph 4.32 only typed and spread
 * object options. The local Reagraph patch preserves this factory unchanged.
 */
export function createWebGPUFactory(
  onFallback?: (reason: string) => void,
): GLProps {
  return async (defaultProps) => {
    const renderer = new WebGPURenderer({
      canvas: defaultProps.canvas as
        | HTMLCanvasElement
        | globalThis.OffscreenCanvas,
      alpha: defaultProps.alpha,
      antialias: defaultProps.antialias,
      powerPreference:
        defaultProps.powerPreference === "low-power" ||
        defaultProps.powerPreference === "high-performance"
          ? defaultProps.powerPreference
          : "high-performance",
    });
    await renderer.init();
    if (!("isWebGPUBackend" in renderer.backend)) {
      onFallback?.("WebGPU initialization fell back to WebGL");
    }
    return renderer;
  };
}
