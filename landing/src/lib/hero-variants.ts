/**
 * The four candidate hero fields, drawn side by side on `/hero-variants/` so
 * the layout is chosen by looking at it instead of by describing it.
 *
 * Every variant fixes the three things the shipped hero gets wrong: a position
 * encodes a relation, no edge crosses another without meaning, and the motion
 * has a cause -- a propagation or a traversal, never an orbit.
 *
 * The palette, the hash and the alpha helper are imported rather than
 * reimplemented: the colour literals live once, in `global.css`, and this
 * module declares none.
 */
import { readPalette, unitOf, withAlpha } from "./hero-graph";

export type VariantName = "ripple" | "layers" | "clusters" | "spine";

const TAU = Math.PI * 2;

/** Drawn radius per rank at DPR 1, deepest rank last. */
const NODE_RADIUS = [8, 5.5, 3.4, 2.1] as const;

/**
 * A node in normalised space. `nx` and `ny` are fractions of the field, so a
 * resize rescales the scene instead of rebuilding it; `rank` picks both the
 * radius and the colour; `tier` is the order the motion reaches it in.
 */
interface Node {
  readonly nx: number;
  readonly ny: number;
  readonly rank: number;
  readonly tier: number;
}

/** An edge between two node indices. `carry` marks the relation being sold. */
interface Edge {
  readonly from: number;
  readonly to: number;
  readonly tier: number;
  readonly carry: boolean;
}

interface Scene {
  readonly nodes: readonly Node[];
  readonly edges: readonly Edge[];
  /** Normalised radii of the depth rings, for the variants that draw them. */
  readonly rings: readonly number[];
}

/**
 * A: the impact radius. One symbol at the centre, concentric rings by hop
 * count, every edge radial. This is `get_blast_radius` drawn literally, and no
 * two edges can cross because a child never leaves its parent's bearing.
 */
function buildRipple(): Scene {
  const nodes: Node[] = [{ nx: 0.5, ny: 0.5, rank: 0, tier: 0 }];
  const edges: Edge[] = [];
  const rings = [0.17, 0.31, 0.45];
  const angles: number[] = [];

  for (let index = 0; index < 6; index += 1) {
    const angle = (index / 6) * TAU + 0.3;
    angles.push(angle);
    nodes.push({
      nx: 0.5 + Math.cos(angle) * 0.17,
      ny: 0.5 + Math.sin(angle) * 0.17,
      rank: 1,
      tier: 1,
    });
    edges.push({ from: 0, to: nodes.length - 1, tier: 1, carry: true });
  }

  const depthTwoBase = nodes.length;
  const depthTwoAngles: number[] = [];
  for (let index = 0; index < 12; index += 1) {
    const parent = index >> 1;
    const spread = index % 2 === 0 ? -0.26 : 0.26;
    const angle =
      (angles[parent] as number) + spread + (unitOf(index, 3) - 0.5) * 0.12;
    depthTwoAngles.push(angle);
    nodes.push({
      nx: 0.5 + Math.cos(angle) * 0.31,
      ny: 0.5 + Math.sin(angle) * 0.31,
      rank: 2,
      tier: 2,
    });
    edges.push({
      from: 1 + parent,
      to: nodes.length - 1,
      tier: 2,
      carry: true,
    });
  }

  for (let index = 0; index < 18; index += 1) {
    const parent = Math.floor((index * 2) / 3);
    const spread = ((index % 3) - 1) * 0.13;
    const angle =
      (depthTwoAngles[parent] as number) +
      spread +
      (unitOf(index, 5) - 0.5) * 0.08;
    nodes.push({
      nx: 0.5 + Math.cos(angle) * 0.45,
      ny: 0.5 + Math.sin(angle) * 0.45,
      rank: 3,
      tier: 3,
    });
    edges.push({
      from: depthTwoBase + parent,
      to: nodes.length - 1,
      tier: 3,
      carry: true,
    });
  }

  return { nodes, edges, rings };
}

/**
 * B: the layered dependency graph. Three columns, every edge monotone left to
 * right, so crossings are impossible by construction. It is the shape an
 * engineer already recognises from a monorepo task graph.
 */
function buildLayers(): Scene {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const repositoryY = [0.22, 0.5, 0.78];

  for (let index = 0; index < 3; index += 1) {
    nodes.push({
      nx: 0.12,
      ny: repositoryY[index] as number,
      rank: 0,
      tier: 0,
    });
  }

  const packageBase = nodes.length;
  for (let index = 0; index < 6; index += 1) {
    const parent = index >> 1;
    const offset = index % 2 === 0 ? -0.085 : 0.085;
    nodes.push({
      nx: 0.5,
      ny: (repositoryY[parent] as number) + offset,
      rank: 1,
      tier: 1,
    });
    edges.push({ from: parent, to: nodes.length - 1, tier: 1, carry: true });
  }

  for (let index = 0; index < 12; index += 1) {
    const parent = index >> 1;
    const source = nodes[packageBase + parent] as Node;
    const offset = index % 2 === 0 ? -0.042 : 0.042;
    nodes.push({ nx: 0.88, ny: source.ny + offset, rank: 2, tier: 2 });
    edges.push({
      from: packageBase + parent,
      to: nodes.length - 1,
      tier: 2,
      carry: true,
    });
  }

  return { nodes, edges, rings: [] };
}

/** Where the three repositories sit in variant C, in normalised space. */
const CLUSTER_CENTRES = [
  [0.27, 0.31],
  [0.74, 0.28],
  [0.5, 0.75],
] as const;

/**
 * C: three clusters and the edges between them. Containment stays as a faint
 * texture and only the cross-repository relations are lit, which is the one
 * claim the hero makes.
 */
function buildClusters(): Scene {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const packageIndex: number[][] = [];

  for (let cluster = 0; cluster < CLUSTER_CENTRES.length; cluster += 1) {
    const centre = CLUSTER_CENTRES[cluster] as readonly [number, number];
    const root = nodes.length;
    nodes.push({ nx: centre[0], ny: centre[1], rank: 0, tier: 0 });

    const packages: number[] = [];
    for (let index = 0; index < 4; index += 1) {
      const angle = (index / 4) * TAU + cluster * 0.7;
      packages.push(nodes.length);
      nodes.push({
        nx: centre[0] + Math.cos(angle) * 0.075,
        ny: centre[1] + Math.sin(angle) * 0.075,
        rank: 1,
        tier: 0,
      });
      edges.push({ from: root, to: nodes.length - 1, tier: 0, carry: false });
    }
    packageIndex.push(packages);

    for (let index = 0; index < 9; index += 1) {
      const parent = packages[index % 4] as number;
      const source = nodes[parent] as Node;
      const angle = unitOf(cluster * 16 + index, 11) * TAU;
      const reach = 0.038 + unitOf(index, cluster + 2) * 0.026;
      nodes.push({
        nx: source.nx + Math.cos(angle) * reach,
        ny: source.ny + Math.sin(angle) * reach,
        rank: 3,
        tier: 0,
      });
      edges.push({ from: parent, to: nodes.length - 1, tier: 0, carry: false });
    }
  }

  const links: readonly [number, number, number, number][] = [
    [0, 1, 1, 0],
    [1, 2, 2, 1],
    [2, 0, 3, 2],
    [0, 2, 0, 0],
    [1, 0, 2, 3],
    [2, 1, 1, 3],
  ];
  for (let index = 0; index < links.length; index += 1) {
    const link = links[index] as readonly [number, number, number, number];
    const source = (packageIndex[link[0]] as number[])[link[2]] as number;
    const target = (packageIndex[link[1]] as number[])[link[3]] as number;
    edges.push({ from: source, to: target, tier: 1, carry: true });
  }

  return { nodes, edges, rings: [] };
}

/**
 * D: one traced path. A component reaching the handler that serves it, with
 * the rest of the graph as a quiet field around it.
 */
function buildSpine(): Scene {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const spineX = [0.14, 0.38, 0.62, 0.86];
  const spineY = [0.44, 0.54, 0.47, 0.57];
  const spineRank = [0, 1, 1, 2];

  for (let index = 0; index < spineX.length; index += 1) {
    nodes.push({
      nx: spineX[index] as number,
      ny: spineY[index] as number,
      rank: spineRank[index] as number,
      tier: 0,
    });
    if (index > 0) {
      edges.push({ from: index - 1, to: index, tier: 1, carry: true });
    }
  }

  for (let index = 0; index < 24; index += 1) {
    const anchor = index % spineX.length;
    const source = nodes[anchor] as Node;
    const angle = unitOf(index, 13) * TAU;
    const reach = 0.07 + unitOf(index, 17) * 0.14;
    nodes.push({
      nx: source.nx + Math.cos(angle) * reach,
      ny: source.ny + Math.sin(angle) * reach * 0.8,
      rank: 3,
      tier: 0,
    });
    edges.push({ from: anchor, to: nodes.length - 1, tier: 0, carry: false });
  }

  return { nodes, edges, rings: [] };
}

/** How long one full pass of the motion takes, per variant, in seconds. */
const CYCLE: Record<VariantName, number> = {
  ripple: 5.2,
  layers: 4.6,
  clusters: 6,
  spine: 7,
};

/**
 * Mounts one field and returns its teardown. The canvas takes no pointer
 * input: it is a figure, not a control, so no listener is installed on it.
 */
export function mountVariant(
  canvas: HTMLCanvasElement,
  variant: VariantName,
): () => void {
  const context = canvas.getContext("2d");
  return context === null ? () => {} : run(canvas, context, variant);
}

/**
 * The field itself, given a context that exists. Everything below reads and
 * writes one canvas and owns every observer it installs.
 */
function run(
  canvas: HTMLCanvasElement,
  context: CanvasRenderingContext2D,
  variant: VariantName,
): () => void {
  const palette = readPalette();
  const scene =
    variant === "ripple"
      ? buildRipple()
      : variant === "layers"
        ? buildLayers()
        : variant === "clusters"
          ? buildClusters()
          : buildSpine();
  const reducedMotion = window.matchMedia(
    "(prefers-reduced-motion: reduce)",
  ).matches;

  let width = 0;
  let height = 0;
  let frame = 0;
  let running = false;
  let startedAt = 0;

  function draw(elapsed: number): void {
    context.clearRect(0, 0, width, height);
    const inset = NODE_RADIUS[0] + 4;
    const span = Math.min(width, height) - inset * 2;
    const originX = (width - span) / 2 + inset;
    const originY = (height - span) / 2 + inset;
    const phase = (elapsed % CYCLE[variant]) / CYCLE[variant];

    // The depth rings of the impact radius: they are the axis the propagation
    // is read against, so they are drawn under everything else.
    if (scene.rings.length > 0) {
      context.setLineDash([2, 6]);
      context.lineWidth = 1;
      context.strokeStyle = withAlpha(palette.containment, 0.28);
      for (let index = 0; index < scene.rings.length; index += 1) {
        const ring = scene.rings[index] as number;
        context.beginPath();
        context.arc(
          originX + span * 0.5,
          originY + span * 0.5,
          span * ring,
          0,
          TAU,
        );
        context.stroke();
      }
      context.setLineDash([]);
    }

    context.lineCap = "round";
    for (let index = 0; index < scene.edges.length; index += 1) {
      const edge = scene.edges[index] as Edge;
      const source = scene.nodes[edge.from] as Node;
      const target = scene.nodes[edge.to] as Node;
      const x1 = originX + source.nx * span;
      const y1 = originY + source.ny * span;
      const x2 = originX + target.nx * span;
      const y2 = originY + target.ny * span;

      // Containment is texture, never the subject: one flat faint stroke.
      if (!edge.carry) {
        context.lineWidth = 1;
        context.strokeStyle = withAlpha(palette.containment, 0.22);
        context.beginPath();
        context.moveTo(x1, y1);
        context.lineTo(x2, y2);
        context.stroke();
        continue;
      }

      // A carried edge is reached by the motion. `arrival` is how far the
      // wavefront has moved past this edge's tier, in [0, 1].
      const arrival =
        variant === "clusters"
          ? (Math.sin(phase * TAU + index * 1.05) + 1) / 2
          : Math.min(
              1,
              Math.max(0, phase * (scene.rings.length + 1.6) - (edge.tier - 1)),
            );

      context.lineWidth = 1 + arrival * 1.1;
      context.strokeStyle = withAlpha(
        variant === "layers" ? palette.local : palette.cross,
        0.14 + arrival * 0.46,
      );
      context.beginPath();
      context.moveTo(x1, y1);
      if (variant === "layers") {
        const bend = (x2 - x1) * 0.5;
        context.bezierCurveTo(x1 + bend, y1, x2 - bend, y2, x2, y2);
      } else {
        context.lineTo(x2, y2);
      }
      context.stroke();

      // The travelling head: what makes the motion read as propagation
      // instead of as a fade. It exists only while the edge is being reached.
      if (arrival > 0 && arrival < 1) {
        context.fillStyle = withAlpha(palette.exact, 0.75);
        context.beginPath();
        context.arc(
          x1 + (x2 - x1) * arrival,
          y1 + (y2 - y1) * arrival,
          2.4,
          0,
          TAU,
        );
        context.fill();
      }
    }
    context.lineCap = "butt";

    // The traced path of variant D carries one head over the whole chain
    // rather than one per edge, because the claim is the path, not the hops.
    if (variant === "spine") {
      const travelled = phase * 3;
      const segment = Math.min(2, Math.floor(travelled));
      const local = travelled - segment;
      const source = scene.nodes[segment] as Node;
      const target = scene.nodes[segment + 1] as Node;
      context.fillStyle = palette.exact;
      context.beginPath();
      context.arc(
        originX + (source.nx + (target.nx - source.nx) * local) * span,
        originY + (source.ny + (target.ny - source.ny) * local) * span,
        3.2,
        0,
        TAU,
      );
      context.fill();
    }

    // Nodes last, deepest rank first, so a repository is never hidden by the
    // symbols it holds.
    for (let rank = NODE_RADIUS.length - 1; rank >= 0; rank -= 1) {
      context.fillStyle = palette.node[rank] as string;
      const radius = NODE_RADIUS[rank] as number;
      for (let index = 0; index < scene.nodes.length; index += 1) {
        const node = scene.nodes[index] as Node;
        if (node.rank !== rank) {
          continue;
        }
        context.beginPath();
        context.arc(
          originX + node.nx * span,
          originY + node.ny * span,
          radius,
          0,
          TAU,
        );
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
    draw(running ? (performance.now() - startedAt) / 1000 : 0);
  }

  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);

  // A canvas nobody can see is a core spent on nothing, and this page holds
  // four of them.
  const intersectionObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting && !running && !reducedMotion) {
        running = true;
        frame = window.requestAnimationFrame(tick);
      } else if (!entry.isIntersecting) {
        stop();
      }
    }
  });
  intersectionObserver.observe(canvas);

  resize();
  if (reducedMotion) {
    draw(CYCLE[variant]);
  }

  return () => {
    stop();
    resizeObserver.disconnect();
    intersectionObserver.disconnect();
  };
}
