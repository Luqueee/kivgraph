# Viewer instructions (`web/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

The server that publishes the tiles and the `LGVB` payload lives in `internal/`.

## Scope and style

- The web application in `web/` keeps strict TypeScript, ESM, Biome and
  Vitest; large binary payloads stay out of React state and `web/dist` is
  regenerated with the Vite build.
- `web/` is only the graph previewer: `App` mounts the viewer full screen and
  holds no landing page, header or presentation sections. The viewer is dark
  by construction (`class="dark"` and Reagraph's `darkTheme`).
- The UI components that are needed are added with
  `pnpm dlx shadcn@latest init`/`add` and Tailwind CSS, not by hand and not with
  a second styling library; the package does not vendor unused primitives.
- `web/src/renderer` receives the versioned `LGVB` payload as an `ArrayBuffer`,
  keeps its views outside React and only materializes the visible limit that
  `reagraph` consumes; the adapter rejects payloads that exceed the limit
  instead of silently truncating the topology.

## World layout

- The viewer does not draw the published coordinates: it derives its own 3D
  layout from the structure of the tile - containment, dependencies,
  communities and hierarchical depth - and computes it in the worker, once per
  tile. A tile is a sample of the world, and the grid the server packed says
  nothing about which packages belong together when most of them are missing.
- Structure first and physics after: clusters are placed and relaxed against
  each other, every node hangs from its container, and only at the end does a
  local relaxation correct what the structure did not decide. There is no
  per-frame simulation.
- The layout is deterministic: directions come from the hash of the node's
  identity, not from its position in the tile or from a random generator. The
  same tile always draws the same world.
- The radius of a shell is the quadratic sum of the radii of its children,
  never the largest one times the square root of the count: that sends the
  small children to the orbit of the large one and leaves the intermediate
  volume empty.
- Every layout distance is a proportion of the radius a node is drawn with.
  Reserving space in units unrelated to what is painted produces either a
  tangle or a field of invisible points.
- No axis may collapse: if one ends up narrower than half of the widest it is
  rescaled around the centroid. Depth that exists only in the numbers is worth
  nothing.

## What is drawn and how

- The viewer shows no dense IDs: every node is labelled with the name the
  snapshot knows, shortened on the canvas and complete on hover. A dense ID is
  unique only within its node type, so the ends of an edge are resolved by
  `(type, id)`.
- The viewer draws the containment declared by `parent_kind`/`parent_id` when
  the container travels in the same tile, and a legend names every colour and
  every stroke. The palette is declared once, in the adapter.
- No viewer edge is dashed: Reagraph builds one curve and one tube per dash,
  and with one edge per node that dominates the frame. The distinction is made
  with colour and thickness; edges between clusters are curved so they do not
  cross the centre in a straight line.
- Only repositories and hubs carry a permanent label; the rest appear on
  hover. Every label has a fixed size in world units, so labelling everything
  only produces a grey smear.
- The label is drawn by the node renderer as a sprite, with the text painted by
  the browser into a texture, and `labelType` is `none`. Reagraph's labels are
  Troika, and Troika resolves the font of each character with data it requests
  from a CDN: a local viewer made a request to a third party on every load, and
  with no route to it the canvas waited for a response that never arrived and
  drew no node. Besides, its glyph material is GLSL that the WebGPU renderer
  does not run, so there every label was a dot.
- The viewer requests nothing outside its origin. The server says so in every
  response with `Content-Security-Policy` (`internal/webassets`), with no
  `blob:` in `script-src` and no other host: a dependency that goes back to
  requesting something outside is blocked and named in the console.
- Hovering a node lights up its neighbourhood and dims the rest; leaving turns
  it off. Reagraph only dims when there is a selection, so the pointed node
  goes in `selections` and what it touches in `actives`. Turning on and off
  wait for the cursor to settle: every change of the active set rebuilds the
  edge meshes.
- The camera frames the projected extent on its own axes, not the bounding
  sphere, and opens off axis. A structural layout is never a ball, and framing
  one leaves the graph in a third of the screen.
- The hierarchy is read by size: the drawn range goes from `4` to `22` units and
  the canvas receives those same limits as `minNodeSize`/`maxNodeSize`, because
  Reagraph rescales the sizes it receives. The small end does not go below `4`:
  below that a symbol stops being a point and is nothing.
- Containment is dimmed according to what it holds, and the spacing between
  siblings is set by the typical node of the tile, not the largest. A container
  with all its strokes at the same brightness is a hedgehog, and spacing
  symbols with the measure of a repository inflates the world until they are
  invisible.
- Nodes are drawn with one shared sphere geometry and materials shared by
  colour and opacity. A `25 × 25` sphere per node is fifteen hundred triangles
  for a five-pixel dot.
- Containment is not an edge: it travels as pairs of indices and is drawn as a
  single mesh of segments with colour per vertex. There is one per node,
  nothing selects it, and as an edge it would force rebuilding the whole
  graph's geometry every time the highlight moves.
- The viewer chooses the level of detail from the camera's projected pixels,
  keeps hysteresis between `1.1` and `1` pixels and raises dependencies toward
  visible containers; changing level only reprojects during one interaction
  frame and never modifies the tile.

## Cost per frame

- The viewer draws on demand: the render loop stops while the graph is still
  and wakes with the canvas's pointer events. A published graph does not move,
  and redrawing it sixty times a second costs a core for nothing. Any component
  that refreshes on its own -the FPS counter- lives outside the canvas: every
  commit reapplies its `frameloop`.
- The viewer requests tiles from a Web Worker with an `AbortController` per
  request; rendering stays on the main thread. The number of nodes per view is
  adjustable from the interface, a trimmed tile is declared as such, and an FPS
  counter exposes the cost of the choice.
- The viewer queries `navigator.gpu` before mounting WebGPU; without a usable
  adapter it shows the reason and keeps WebGL. The WebGPU path uses node
  materials and is never declared active by a mere configuration property.
- The viewer's WebGL factory disables antialiasing; `FrameGovernor` limits the
  DPR to `1` while the pointer moves - also when hovering, not only when
  dragging - and during a sustained gesture it also pauses picking and hides
  the labels with `visible = false`. Everything is restored when it goes idle,
  and every change of the drawing buffer is repainted synchronously before it
  is exposed to the browser, so as not to show a black frame.
- Labels are not unmounted during a gesture: the governor only changes their
  `visible`, and recognizes them by the `userData.caption` mark of their
  material.
- The label of the pointed node does not live in the viewer's state: it travels
  through a channel of its own (`createStatusChannel`) because one `setState`
  per brushed node rebuilds the element tree of the whole graph. The selection
  and its neighbourhood are a single state and are applied inside
  `startTransition`.
- Troika's instanced geometries start with `instanceCount: 0` until the
  asynchronous glyph atlas arrives and do not call `dispose()` when replacing an
  attribute: `troika-three-text@0.52.5` stays pinned and patched because WebGPU
  rejects the default value `Infinity` and because freeing the buffers of a live
  geometry leaves the backend binding a nonexistent index.
- Before building the `WebGPURenderer`, the viewer resolves `instanceCount` in
  `InstancedBufferGeometry` with the same rule as WebGL - the minimum of
  `meshPerAttribute * count` across the instanced attributes, `0` with none -
  so that no library can carry an `Infinity` into `drawIndexed`.

## Verification

```bash
cd web
pnpm check
pnpm build
```

`pnpm check` is `format:check`, `lint`, `typecheck` and `test`. `dist/` is
generated with Vite and `internal/webassets` serves it only in builds with the
`webassets` tag.
The end-to-end benchmark is in `benchmarks/web-viewer/`.
