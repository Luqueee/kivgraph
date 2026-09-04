import { useState } from "react";

import { GraphPreview } from "@/components/GraphPreview";
import { TopologyExplorer } from "@/components/TopologyExplorer";
import { Button } from "@/components/ui/button";

function App() {
  const [mode, setMode] = useState<"graph" | "topology">("graph");

  return (
    <main className="dark relative h-svh w-svw overflow-hidden bg-background text-foreground">
      <fieldset className="pointer-events-auto absolute right-4 top-3 z-50 flex items-center gap-1 rounded-full border border-white/10 bg-slate-950/90 p-1 shadow-2xl backdrop-blur">
        <legend className="sr-only">Viewer mode</legend>
        <Button
          type="button"
          size="xs"
          variant={mode === "graph" ? "secondary" : "ghost"}
          onClick={() => setMode("graph")}
          aria-pressed={mode === "graph"}
        >
          graph
        </Button>
        <Button
          type="button"
          size="xs"
          variant={mode === "topology" ? "secondary" : "ghost"}
          onClick={() => setMode("topology")}
          aria-pressed={mode === "topology"}
        >
          topology
        </Button>
      </fieldset>
      {mode === "graph" ? <GraphPreview /> : <TopologyExplorer />}
    </main>
  );
}

export default App;
