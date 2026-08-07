import { useEffect, useRef } from "react";

import { GraphRenderer, type GraphNodeHit } from "@/renderer/GraphRenderer";
import { decodeGraphPayload, NODE_KIND_SYMBOL } from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";

export function GraphPreview() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const labelsRef = useRef<HTMLDivElement>(null);
  const statusRef = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    const labels = labelsRef.current;
    const status = statusRef.current;
    if (!canvas || !labels || !status) return;

    let renderer: GraphRenderer | null = null;
    let observer: ResizeObserver | null = null;
    const onWindowResize = (): void => renderer?.resize();
    const updateStatus = (hit: GraphNodeHit | null): void => {
      if (!hit) {
        status.textContent = "GPU picking ready · drag to pan · wheel to zoom";
        return;
      }
      status.textContent = `Node ${hit.id} · kind ${hit.kind} · GPU picked`;
    };

    try {
      renderer = new GraphRenderer(canvas, {
        labelContainer: labels,
        labelProvider: (id, kind) =>
          kind === NODE_KIND_SYMBOL ? `symbol ${id}` : null,
        onHover: updateStatus,
      });
      renderer.load(decodeGraphPayload(createDemoPayload()));
      renderer.start();
      updateStatus(null);

      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(() => renderer?.resize());
        observer.observe(canvas.parentElement ?? canvas);
      } else {
        window.addEventListener("resize", onWindowResize);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "unknown error";
      status.textContent = `Renderer unavailable · ${message}`;
      renderer?.dispose();
      renderer = null;
    }

    return () => {
      observer?.disconnect();
      if (!observer) window.removeEventListener("resize", onWindowResize);
      renderer?.dispose();
    };
  }, []);

  return (
    <div className="relative h-[28rem] overflow-hidden rounded-3xl border border-border bg-slate-50 shadow-inner dark:bg-slate-950">
      <canvas
        ref={canvasRef}
        className="absolute inset-0 h-full w-full touch-none"
        aria-label="Interactive graph renderer preview"
      />
      <div
        ref={labelsRef}
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
      />
      <div className="pointer-events-none absolute inset-x-4 top-4 flex items-center justify-between gap-4 text-xs font-medium">
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          Buffer preview · 7 nodes · 4 edges
        </span>
        <span
          ref={statusRef}
          className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur"
        >
          Initializing renderer…
        </span>
      </div>
    </div>
  );
}
