import { useMemo, useState } from "react";
import { GraphCanvas, darkTheme } from "reagraph";
import type { InternalGraphNode } from "reagraph";

import { decodeGraphPayload } from "@/renderer/binary";
import { createDemoPayload } from "@/renderer/fixture";
import {
  createReagraphGraph,
  type ReagraphNodeData,
  type ViewerReagraphGraph,
} from "@/renderer/reagraph";

const READY_STATUS = "drag to pan · wheel to zoom";

export function GraphPreview() {
  const result = useMemo((): {
    graph: ViewerReagraphGraph | null;
    error: string | null;
  } => {
    try {
      return {
        graph: createReagraphGraph(decodeGraphPayload(createDemoPayload())),
        error: null,
      };
    } catch (error) {
      return {
        graph: null,
        error: error instanceof Error ? error.message : "unknown error",
      };
    }
  }, []);
  const [status, setStatus] = useState(READY_STATUS);

  const updateStatus = (node: InternalGraphNode): void => {
    const data = node.data as ReagraphNodeData | undefined;
    setStatus(
      `Node ${data?.index ?? node.id} · kind ${data?.kind ?? "unknown"} · hover`,
    );
  };

  return (
    <div
      className="relative h-full w-full bg-background"
      role="img"
      aria-label="Interactive Reagraph graph preview"
    >
      {result.graph ? (
        <GraphCanvas
          nodes={result.graph.nodes}
          edges={result.graph.edges}
          theme={darkTheme}
          layoutType="custom"
          layoutOverrides={result.graph.layoutOverrides}
          animated={false}
          labelType="nodes"
          cameraMode="pan"
          onNodePointerOver={updateStatus}
          onNodePointerOut={() => setStatus(READY_STATUS)}
        />
      ) : null}
      <div className="pointer-events-none absolute inset-x-4 top-4 flex items-center justify-between gap-4 text-xs font-medium">
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          {result.graph?.nodes.length ?? 0} nodes ·{" "}
          {result.graph?.edges.length ?? 0} edges
        </span>
        <span className="rounded-full border border-border/80 bg-background/85 px-3 py-1 text-muted-foreground backdrop-blur">
          {result.graph ? status : `graph unavailable · ${result.error}`}
        </span>
      </div>
    </div>
  );
}
