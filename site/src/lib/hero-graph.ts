/**
 * The hero canvas: a synthetic graph drawn in the viewer's palette.
 *
 * This is deliberately not the viewer. It carries no three.js, no reagraph and
 * no React; it is a 2D canvas whose only job is to say what the product looks
 * at. Everything it draws is deterministic, so the landing page paints the same
 * field on every load and on every machine.
 */

/** Node ranks, in the order the viewer nests them. */
const KIND_REPOSITORY = 0;
const KIND_PACKAGE = 1;
const KIND_FILE = 2;
const KIND_SYMBOL = 3;

const REPOSITORY_COUNT = 4;
const PACKAGE_COUNT = 16;
const FILE_COUNT = 56;
const SYMBOL_COUNT = 180;
const CROSS_DEPENDENCY_COUNT = 24;

/** Drawn radius per rank at DPR 1. */
const NODE_RADIUS = [9, 6, 3.5, 2] as const;

/** How much of the containment colour a link keeps, by what it holds. */
const CONTAINMENT_PRESENCE = [0, 0.78, 0.46, 0.3] as const;

/** Each orbit as a fraction of its parent's orbit. */
const PACKAGE_ORBIT = 0.55;
const FILE_ORBIT = 0.5;
const SYMBOL_ORBIT = 0.45;

/**
 * How far the deepest node sits from the centre, in units of the repository
 * ring radius. A repository is one ring out and every rank adds its own orbit
 * on top, so the ring is sized from this rather than from a fixed share of the
 * viewport: a share picked for the ring alone puts the symbols off the canvas.
 */
const TOTAL_REACH =
  1 +
  PACKAGE_ORBIT +
  PACKAGE_ORBIT * FILE_ORBIT +
  PACKAGE_ORBIT * FILE_ORBIT * SYMBOL_ORBIT;

/** Radians per second the whole field turns at. */
const FIELD_SPIN = 0.02;
/** Radians per second a node's own orbital phase advances at, before depth. */
const ORBIT_SPIN = 0.05;
/** Most the pointer may translate the field, in CSS pixels, per axis. */
const POINTER_TRAVEL = 12;

/** The palette, in the order `readPalette` returns it. */
const PALETTE_PROPERTIES = [
  ["--color-graph-repository", "#2563eb"],
  ["--color-graph-package", "#7c3aed"],
  ["--color-graph-file", "#059669"],
  ["--color-graph-symbol", "#ea580c"],
  ["--color-graph-containment", "#475569"],
  ["--color-graph-local", "#4b5563"],
  ["--color-graph-cross", "#94a3b8"],
  ["--color-graph-exact", "#16a34a"],
] as const;

interface Palette {
  readonly node: readonly [string, string, string, string];
  readonly containment: string;
  readonly local: string;
  readonly cross: string;
  readonly exact: string;
}

interface Node {
  readonly kind: number;
  readonly parent: number;
  /** Angle around the parent, in radians; assigned once by `buildNodes`. */
  angle: number;
  /** Radius of this node's orbit around its parent, in CSS pixels. */
  orbit: number;
  x: number;
  y: number;
}

/**
 * Stable 32-bit identity of an index, reusing the mix in
 * `web/src/renderer/reagraph.ts` so the two fields scatter the same way.
 */
function identityOf(index: number, salt: number): number {
  let value =
    (Math.imul(index + 1, 0x85ebca6b) ^ Math.imul(salt + 1, 0xc2b2ae35)) >>> 0;
  value = Math.imul(value ^ (value >>> 15), 0x2545f491) >>> 0;
  return (value ^ (value >>> 13)) >>> 0;
}

/** The identity of an index as a fraction in `[0, 1)`. */
function unitOf(index: number, salt: number): number {
  return identityOf(index, salt) / 0x1_0000_0000;
}

/**
 * The eight colours, read from the document so the palette lives only in
 * `global.css`. A property that resolves empty falls back to its literal
 * rather than drawing nothing.
 */
function readPalette(): Palette {
  const computed = getComputedStyle(document.documentElement);
  const resolved = PALETTE_PROPERTIES.map(([property, fallback]) => {
    const value = computed.getPropertyValue(property).trim();
    return value === "" ? fallback : value;
  });
  return {
    node: [
      resolved[0] as string,
      resolved[1] as string,
      resolved[2] as string,
      resolved[3] as string,
    ],
    containment: resolved[4] as string,
    local: resolved[5] as string,
    cross: resolved[6] as string,
    exact: resolved[7] as string,
  };
}

/** Most of a sibling step a node's own hash may move it, either way. */
const ANGLE_JITTER = 0.3;

/**
 * The 256 nodes, each one attached to a parent round-robin: a package to a
 * repository, a file to a package, a symbol to a file.
 *
 * Siblings are spread evenly around their parent and the hash only jitters
 * them within their own slot. Angles taken straight from the hash cluster with
 * four repositories, and a field that leaves a third of the canvas empty reads
 * as a bug rather than as a graph.
 */
function buildNodes(): Node[] {
  const nodes: Node[] = [];
  const push = (kind: number, parent: number) => {
    nodes.push({ kind, parent, angle: 0, orbit: 0, x: 0, y: 0 });
  };
  for (let index = 0; index < REPOSITORY_COUNT; index += 1) {
    push(KIND_REPOSITORY, -1);
  }
  const packageBase = nodes.length;
  for (let index = 0; index < PACKAGE_COUNT; index += 1) {
    push(KIND_PACKAGE, index % REPOSITORY_COUNT);
  }
  const fileBase = nodes.length;
  for (let index = 0; index < FILE_COUNT; index += 1) {
    push(KIND_FILE, packageBase + (index % PACKAGE_COUNT));
  }
  for (let index = 0; index < SYMBOL_COUNT; index += 1) {
    push(KIND_SYMBOL, fileBase + (index % FILE_COUNT));
  }

  // Slot every node inside its own parent, in one pass over the sibling sets.
  const siblings = new Map<number, number[]>();
  for (let index = 0; index < nodes.length; index += 1) {
    const parent = (nodes[index] as Node).parent;
    const group = siblings.get(parent);
    if (group === undefined) {
      siblings.set(parent, [index]);
    } else {
      group.push(index);
    }
  }
  for (const [parent, group] of siblings) {
    const phase = unitOf(parent + 2, 7) * Math.PI * 2;
    for (let ordinal = 0; ordinal < group.length; ordinal += 1) {
      const index = group[ordinal] as number;
      const jitter = (unitOf(index, 8) - 0.5) * 2 * ANGLE_JITTER;
      (nodes[index] as Node).angle =
        phase + ((ordinal + jitter) / group.length) * Math.PI * 2;
    }
  }
  return nodes;
}

/**
 * The cross-repository dependencies, drawn between packages that do not share
 * a repository. They are the claim the product makes, so they are the one
 * relation the hero draws.
 */
function buildCrossDependencies(nodes: readonly Node[]): [number, number][] {
  const packages: number[] = [];
  for (let index = 0; index < nodes.length; index += 1) {
    if (nodes[index]?.kind === KIND_PACKAGE) {
      packages.push(index);
    }
  }
  const pairs: [number, number][] = [];
  for (let index = 0; pairs.length < CROSS_DEPENDENCY_COUNT; index += 1) {
    const from = packages[
      Math.floor(unitOf(index, 5) * packages.length)
    ] as number;
    const to = packages[
      Math.floor(unitOf(index, 6) * packages.length)
    ] as number;
    const source = nodes[from] as Node;
    const target = nodes[to] as Node;
    if (source.parent === target.parent) {
      continue;
    }
    pairs.push([from, to]);
    if (index > CROSS_DEPENDENCY_COUNT * 16) {
      break;
    }
  }
  return pairs;
}

/** `#rrggbb` with an alpha, without allocating a colour parser. */
function withAlpha(color: string, alpha: number): string {
  if (color.startsWith("#") && color.length === 7) {
    const red = Number.parseInt(color.slice(1, 3), 16);
    const green = Number.parseInt(color.slice(3, 5), 16);
    const blue = Number.parseInt(color.slice(5, 7), 16);
    return `rgba(${red}, ${green}, ${blue}, ${alpha})`;
  }
  return color;
}

/**
 * Starts the hero canvas and returns the teardown: it cancels the pending
 * frame and disconnects every observer and listener it installed.
 */
export function startHeroGraph(canvas: HTMLCanvasElement): () => void {
  const context = canvas.getContext("2d");
  if (context === null) {
    return () => {};
  }
  return run(canvas, context);
}

/**
 * The field itself, given a context that exists. Everything below reads and
 * writes one canvas and owns every observer it installs.
 */
function run(
  canvas: HTMLCanvasElement,
  context: CanvasRenderingContext2D,
): () => void {
  const palette = readPalette();
  const nodes = buildNodes();
  const crossDependencies = buildCrossDependencies(nodes);
  const reducedMotion = window.matchMedia(
    "(prefers-reduced-motion: reduce)",
  ).matches;

  let width = 0;
  let height = 0;
  let pointerX = 0;
  let pointerY = 0;
  let frame = 0;
  let running = false;
  let visible = true;
  let startedAt = 0;

  /** Places every node for a given elapsed time, in seconds. */
  function layout(elapsed: number): void {
    // The pointer shifts the whole field and a repository is drawn as a disc,
    // so both come out of the half-extent before the ring is derived.
    const usable =
      Math.min(width, height) / 2 - POINTER_TRAVEL - NODE_RADIUS[0] - 2;
    const ringRadius = Math.max(1, usable) / TOTAL_REACH;
    const spin = FIELD_SPIN * elapsed;
    const centreX = width / 2 + pointerX;
    const centreY = height / 2 + pointerY;
    for (let index = 0; index < nodes.length; index += 1) {
      const node = nodes[index] as Node;
      if (node.kind === KIND_REPOSITORY) {
        node.orbit = ringRadius;
        const angle = node.angle + spin;
        node.x = centreX + Math.cos(angle) * ringRadius;
        node.y = centreY + Math.sin(angle) * ringRadius;
        continue;
      }
      const parent = nodes[node.parent] as Node;
      const share =
        node.kind === KIND_PACKAGE
          ? PACKAGE_ORBIT
          : node.kind === KIND_FILE
            ? FILE_ORBIT
            : SYMBOL_ORBIT;
      node.orbit = parent.orbit * share;
      const angle = node.angle + spin + (ORBIT_SPIN / node.kind) * elapsed;
      node.x = parent.x + Math.cos(angle) * node.orbit;
      node.y = parent.y + Math.sin(angle) * node.orbit;
    }
  }

  function draw(elapsed: number): void {
    layout(elapsed);
    context.clearRect(0, 0, width, height);

    // Containment first: it is the background texture the nodes sit on.
    context.lineWidth = 1;
    for (let index = 0; index < nodes.length; index += 1) {
      const node = nodes[index] as Node;
      if (node.parent < 0) {
        continue;
      }
      const parent = nodes[node.parent] as Node;
      context.strokeStyle = withAlpha(
        palette.containment,
        CONTAINMENT_PRESENCE[node.kind] ?? 0.3,
      );
      context.beginPath();
      context.moveTo(parent.x, parent.y);
      context.lineTo(node.x, node.y);
      context.stroke();
    }

    // Cross-repository dependencies, bowed away from the centre so they do not
    // cut through the middle of the field.
    const centreX = width / 2 + pointerX;
    const centreY = height / 2 + pointerY;
    context.lineWidth = 1.4;
    context.strokeStyle = withAlpha(palette.cross, 0.35);
    for (let index = 0; index < crossDependencies.length; index += 1) {
      const pair = crossDependencies[index] as [number, number];
      const source = nodes[pair[0]] as Node;
      const target = nodes[pair[1]] as Node;
      const midX = (source.x + target.x) / 2;
      const midY = (source.y + target.y) / 2;
      const controlX = midX + (midX - centreX) * 0.18;
      const controlY = midY + (midY - centreY) * 0.18;
      context.beginPath();
      context.moveTo(source.x, source.y);
      context.quadraticCurveTo(controlX, controlY, target.x, target.y);
      context.stroke();
    }

    // Nodes last, deepest rank first so a repository is never hidden by the
    // symbols it holds.
    for (let kind = KIND_SYMBOL; kind >= KIND_REPOSITORY; kind -= 1) {
      context.fillStyle = palette.node[kind] as string;
      const radius = NODE_RADIUS[kind] as number;
      for (let index = 0; index < nodes.length; index += 1) {
        const node = nodes[index] as Node;
        if (node.kind !== kind) {
          continue;
        }
        context.beginPath();
        context.arc(node.x, node.y, radius, 0, Math.PI * 2);
        context.fill();
      }
    }
  }

  function tick(timestamp: number): void {
    if (startedAt === 0) {
      startedAt = timestamp;
    }
    draw((timestamp - startedAt) / 1000);
    frame = window.requestAnimationFrame(tick);
  }

  function start(): void {
    if (running || reducedMotion) {
      return;
    }
    running = true;
    frame = window.requestAnimationFrame(tick);
  }

  function stop(): void {
    if (!running) {
      return;
    }
    running = false;
    window.cancelAnimationFrame(frame);
    frame = 0;
  }

  function resize(): void {
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    width = canvas.clientWidth;
    height = canvas.clientHeight;
    canvas.width = Math.max(1, Math.round(width * ratio));
    canvas.height = Math.max(1, Math.round(height * ratio));
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    // A resize changes the drawing buffer, which clears it: repaint now rather
    // than exposing an empty frame while waiting for the next tick.
    draw(running ? (performance.now() - startedAt) / 1000 : 0);
  }

  function onPointerMove(event: PointerEvent): void {
    const bounds = canvas.getBoundingClientRect();
    if (bounds.width === 0 || bounds.height === 0) {
      return;
    }
    pointerX =
      ((event.clientX - bounds.left) / bounds.width - 0.5) * 2 * POINTER_TRAVEL;
    pointerY =
      ((event.clientY - bounds.top) / bounds.height - 0.5) * 2 * POINTER_TRAVEL;
    if (reducedMotion) {
      draw(0);
    }
  }

  function onPointerLeave(): void {
    pointerX = 0;
    pointerY = 0;
    if (reducedMotion) {
      draw(0);
    }
  }

  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);

  // The viewer draws on demand and so does this: a canvas nobody can see is a
  // core spent on nothing.
  const intersectionObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      visible = entry.isIntersecting;
    }
    if (visible) {
      start();
    } else {
      stop();
    }
  });
  intersectionObserver.observe(canvas);

  canvas.addEventListener("pointermove", onPointerMove);
  canvas.addEventListener("pointerleave", onPointerLeave);

  resize();
  if (reducedMotion) {
    draw(0);
  } else {
    start();
  }

  return () => {
    stop();
    resizeObserver.disconnect();
    intersectionObserver.disconnect();
    canvas.removeEventListener("pointermove", onPointerMove);
    canvas.removeEventListener("pointerleave", onPointerLeave);
  };
}
