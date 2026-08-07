import { ArrowUpRight, Layers3, Network } from "lucide-react";

import { Button } from "@/components/ui/button";
import { GraphPreview } from "@/components/GraphPreview";

function App() {
  return (
    <main className="min-h-svh bg-background text-foreground">
      <header className="border-b border-border/70 bg-background/90 backdrop-blur">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-4 lg:px-8">
          <a
            className="flex items-center gap-3"
            href="#top"
            aria-label="Ladygraph home"
          >
            <span className="flex size-9 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-sm">
              <Network aria-hidden="true" />
            </span>
            <span className="font-heading text-lg font-semibold tracking-tight">
              Ladygraph
            </span>
          </a>
          <span className="rounded-full border border-border bg-muted px-3 py-1 text-xs font-medium text-muted-foreground">
            Read-only graph viewer
          </span>
        </div>
      </header>

      <section
        id="top"
        className="mx-auto grid max-w-7xl gap-12 px-6 py-20 lg:grid-cols-[1.1fr_0.9fr] lg:px-8 lg:py-28"
      >
        <div className="flex flex-col justify-center gap-8">
          <div className="flex items-center gap-2 text-sm font-medium text-primary">
            <Layers3 aria-hidden="true" className="size-4" />
            <span>Cross-repository intelligence</span>
          </div>
          <div className="space-y-5">
            <h1 className="max-w-3xl font-heading text-4xl font-semibold tracking-tight text-balance sm:text-6xl">
              See how your codebase connects.
            </h1>
            <p className="max-w-2xl text-lg leading-8 text-muted-foreground">
              Ladygraph turns indexed repositories into a read-only semantic
              graph. This Vite and React shell is ready for the binary snapshot
              viewer.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button size="lg" asChild>
              <a href="#ui-foundation">
                Explore the foundation
                <ArrowUpRight data-icon="inline-end" aria-hidden="true" />
              </a>
            </Button>
            <Button size="lg" variant="outline" asChild>
              <a href="#contract">Read the contract</a>
            </Button>
          </div>
        </div>

        <div className="relative overflow-hidden rounded-3xl border border-border bg-card p-6 shadow-sm sm:p-8">
          <div className="absolute inset-x-0 top-0 h-1 bg-primary" />
          <div className="space-y-8">
            <div className="space-y-2">
              <p className="text-sm font-medium text-muted-foreground">
                Viewer foundation
              </p>
              <h2 className="font-heading text-2xl font-semibold tracking-tight">
                A stable surface for large graphs.
              </h2>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-2xl border border-border bg-background p-4">
                <p className="text-2xl font-semibold text-primary">Vite</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  Fast, reproducible asset builds
                </p>
              </div>
              <div className="rounded-2xl border border-border bg-background p-4">
                <p className="text-2xl font-semibold text-primary">React</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  Small state and clear boundaries
                </p>
              </div>
              <div className="rounded-2xl border border-border bg-background p-4">
                <p className="text-2xl font-semibold text-primary">shadcn/ui</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  Composable accessible primitives
                </p>
              </div>
              <div className="rounded-2xl border border-border bg-background p-4">
                <p className="text-2xl font-semibold text-primary">Tailwind</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  Token-driven visual language
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section
        id="ui-foundation"
        className="border-y border-border/70 bg-muted/40"
      >
        <div className="mx-auto max-w-7xl px-6 py-16 lg:px-8">
          <div className="max-w-2xl space-y-3">
            <p className="text-sm font-medium text-primary">UI foundation</p>
            <h2 className="font-heading text-3xl font-semibold tracking-tight">
              Components are generated and owned by the shadcn CLI.
            </h2>
            <p className="text-muted-foreground">
              The initial Button primitive comes from the configured Radix Nova
              preset. Future viewer controls can extend the same token and
              accessibility contract.
            </p>
          </div>
        </div>
      </section>

      <section
        id="renderer-preview"
        className="mx-auto max-w-7xl px-6 py-16 lg:px-8"
      >
        <div className="mb-6 max-w-2xl space-y-3">
          <p className="text-sm font-medium text-primary">Renderer preview</p>
          <h2 className="font-heading text-3xl font-semibold tracking-tight">
            GPU buffers, one scene, no per-node objects.
          </h2>
          <p className="text-muted-foreground">
            The preview decodes the versioned `LGVB` payload directly into typed
            views. Drag, zoom and hover stay on the render path.
          </p>
        </div>
        <GraphPreview />
      </section>

      <section id="contract" className="mx-auto max-w-7xl px-6 py-16 lg:px-8">
        <div className="rounded-3xl border border-border bg-card p-6 sm:p-8">
          <p className="text-sm font-medium text-primary">Snapshot contract</p>
          <div className="mt-4 flex flex-col justify-between gap-6 sm:flex-row sm:items-end">
            <div className="max-w-2xl space-y-2">
              <h2 className="font-heading text-2xl font-semibold tracking-tight">
                Read-only by construction.
              </h2>
              <p className="text-muted-foreground">
                Rendering and interaction will consume the published HotSnapshot
                through the versioned API; the web package does not import
                worker internals.
              </p>
            </div>
            <span className="inline-flex w-fit rounded-full bg-primary/10 px-3 py-1 text-xs font-medium text-primary">
              Foundation ready
            </span>
          </div>
        </div>
      </section>
    </main>
  );
}

export default App;
