import type { GraphLayoutConfig } from "./config";
import type { GraphStructure, LayoutGraph } from "./structure";

export interface Placement {
  readonly x: Float32Array;
  readonly y: Float32Array;
  readonly z: Float32Array;
  /** Space each node reserves: its drawn radius plus the air around it. */
  readonly radius: Float32Array;
  /** Position of each node in the tile ordered by importance; `0` is first. */
  readonly rank: Int32Array;
  readonly center: readonly [number, number, number];
  /** Radius of the sphere the camera must frame. */
  readonly boundingRadius: number;
  /** Standard deviation per axis; the proof that no axis collapsed. */
  readonly spread: readonly [number, number, number];
}

/**
 * Radius the renderer will draw a node with.
 *
 * The layout needs it because every distance it computes is a multiple of it:
 * reserving space in units unrelated to what ends up on screen is how a
 * drawing becomes either a clump or a field of invisible dots. `rank` is the
 * node's position in the tile ordered by importance, `0` being the most
 * central, so a caller can single out a bounded number of hubs without
 * needing to see the whole distribution first.
 */
export type NodeRadius = (
  kind: number,
  importance: number,
  rank: number,
) => number;

const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5));

/**
 * Places every node of the tile in three dimensions.
 *
 * Structure decides the architecture and physics only refines it: cluster
 * balls are laid out first and relaxed against each other, each node is then
 * hung on a shell around its own container, and a short local relaxation
 * resolves the leftovers. Running the physics first would produce a different
 * drawing on every reload and would hide the hierarchy it is meant to show.
 */
export function placeStructure(
  graph: LayoutGraph,
  structure: GraphStructure,
  drawnRadius: NodeRadius,
  config: GraphLayoutConfig,
): Placement {
  const count = graph.nodeCount;
  const x = new Float32Array(count);
  const y = new Float32Array(count);
  const z = new Float32Array(count);
  const radius = new Float32Array(count);

  const rank = new Int32Array(count);
  [...Array(count).keys()]
    .sort(
      (left, right) =>
        structure.importance[right] - structure.importance[left] ||
        left - right,
    )
    .forEach((node, position) => {
      rank[node] = position;
    });

  let unit = 0;
  for (let index = 0; index < count; index += 1) {
    const importance = structure.importance[index];
    const drawn = drawnRadius(graph.kind[index], importance, rank[index]);
    radius[index] = drawn * (config.leafAir + config.hubAir * importance);
    unit = Math.max(unit, drawn);
  }
  // Padding is one figure for the whole tile: derived per pair it would make
  // the gap between two small nodes and two large ones incomparable.
  const padding = unit * config.nodePadding;

  const order = containmentOrder(graph, structure);
  const subtree = measureSubtrees(graph, structure, padding, radius, order);
  const shapes = shapeClusters(graph, structure, padding, radius, subtree);

  // One layer of the dependency hierarchy is worth a fraction of a cluster:
  // tied to the balls it separates, the height reads the same whether the
  // tile holds thirty nodes or two thousand.
  let meanRadius = 0;
  for (const shape of shapes) meanRadius += shape.radius;
  meanRadius /= Math.max(shapes.length, 1);
  const metrics: Metrics = {
    padding,
    hierarchy: meanRadius * config.hierarchySpacing,
    radius,
    subtree,
  };

  const centers = placeClusters(structure, config, metrics, shapes);
  layNodes(graph, structure, config, metrics, shapes, centers, x, y, z);
  refine(graph, structure, config, metrics, x, y, z);
  return finish(x, y, z, radius, rank, config);
}

/** Absolute distances the placement works in, derived once per tile. */
interface Metrics {
  /** Free space kept between two siblings on a shell. */
  readonly padding: number;
  /** Height of one dependency layer. */
  readonly hierarchy: number;
  /** Space each node reserves. */
  readonly radius: Float32Array;
  /** Space each node and everything it contains reserve. */
  readonly subtree: Float32Array;
}

/** Parents before children, so a subtree can be measured bottom up. */
function containmentOrder(
  graph: LayoutGraph,
  structure: GraphStructure,
): Int32Array {
  const order = new Int32Array(graph.nodeCount);
  let cursor = 0;
  for (let index = 0; index < graph.nodeCount; index += 1) {
    if (graph.parent[index] < 0) {
      order[cursor] = index;
      cursor += 1;
    }
  }
  for (let head = 0; head < cursor; head += 1) {
    for (const child of structure.children[order[head]]) {
      order[cursor] = child;
      cursor += 1;
    }
  }
  return order;
}

/**
 * Radius of the ball a node and everything it contains need.
 *
 * Sizing the containers before placing anything is what makes overlap
 * structurally impossible instead of something the relaxation has to repair.
 */
function measureSubtrees(
  graph: LayoutGraph,
  structure: GraphStructure,
  padding: number,
  radius: Float32Array,
  order: Int32Array,
): Float32Array {
  const subtree = new Float32Array(graph.nodeCount);
  for (let position = order.length - 1; position >= 0; position -= 1) {
    const node = order[position];
    const children = structure.children[node];
    if (children.length === 0) {
      subtree[node] = radius[node];
      continue;
    }
    const shell = childShell(
      radius[node],
      children,
      (child) => subtree[child],
      padding,
    );
    subtree[node] = shell.radius + shell.widest;
  }
  return subtree;
}

export interface ChildShell {
  /** Distance from the container's centre to where its children sit. */
  readonly radius: number;
  /** Radius of the largest child, which the container has to make room for. */
  readonly widest: number;
}

/**
 * Distance from a container's centre to the shell its children sit on.
 *
 * The shell is the quadratic sum of the children's radii. For children of the
 * same size that is exactly the room they need: `n` points on a sphere sit
 * about `R · 2/√n` apart, which has to cover two radii, so `R ≥ r√n = √(Σr²)`.
 * The reason to write it as a sum of squares is the case where the children
 * are *not* the same size: taking the largest and multiplying by `√n` would
 * push thirty small packages out to the orbit of the one big one and leave the
 * whole sphere between them empty - the single biggest source of dead space in
 * a nested layout.
 *
 * A second condition applies: the children must also clear the container
 * itself, which is what a single child would otherwise fail, landing exactly
 * on top of its parent.
 */
function childShell<T>(
  parentRadius: number,
  children: readonly T[],
  radiusOf: (child: T) => number,
  padding: number,
): ChildShell {
  let squares = 0;
  let widest = 0;
  for (const child of children) {
    const reach = radiusOf(child) + padding / 2;
    squares += reach * reach;
    widest = Math.max(widest, radiusOf(child));
  }
  if (children.length === 0) return { radius: 0, widest: 0 };
  return {
    radius: Math.max(parentRadius + widest + padding, Math.sqrt(squares)),
    widest,
  };
}

interface CommunityGroup {
  readonly id: number;
  readonly members: readonly number[];
  /** Distance from the lobe centre to its members; zero when there is one. */
  readonly memberShell: number;
  /** Radius of the whole lobe. */
  readonly radius: number;
  readonly layer: number;
}

interface ClusterShape {
  readonly groups: readonly CommunityGroup[];
  /** Distance from the cluster centre to each community lobe centre. */
  readonly lobeShell: number;
  /** Radius of the cluster ball, anchor and lobes included. */
  readonly radius: number;
}

/**
 * Sizes each cluster: its communities, where their lobes sit, and how much
 * room the whole ball needs. Placement and the cluster relaxation both read
 * these numbers, so the space reserved and the space used are the same space.
 */
function shapeClusters(
  graph: LayoutGraph,
  structure: GraphStructure,
  padding: number,
  radius: Float32Array,
  subtreeRadius: Float32Array,
): readonly ClusterShape[] {
  return structure.clusters.map((cluster) => {
    const anchor = cluster.root >= 0 ? radius[cluster.root] : 0;
    const branches =
      cluster.root >= 0
        ? structure.children[cluster.root]
        : cluster.members.filter((node) => graph.parent[node] < 0);

    const byCommunity = new Map<number, number[]>();
    for (const branch of branches) {
      const bucket = byCommunity.get(structure.community[branch]);
      if (bucket === undefined) {
        byCommunity.set(structure.community[branch], [branch]);
      } else {
        bucket.push(branch);
      }
    }

    const groups: CommunityGroup[] = [];
    for (const [id, members] of byCommunity) {
      // Importance first: a hub sits at the head of its lobe, where the
      // Fibonacci spiral starts, and its neighbours fan out around it.
      const ordered = [...members].sort(
        (left, right) =>
          structure.importance[right] - structure.importance[left] ||
          graph.identity[left] - graph.identity[right],
      );
      let layerSum = 0;
      for (const member of ordered) layerSum += structure.layer[member];
      const shell =
        ordered.length > 1
          ? childShell(0, ordered, (member) => subtreeRadius[member], padding)
          : { radius: 0, widest: subtreeRadius[ordered[0]] };
      groups.push({
        id,
        members: ordered,
        memberShell: shell.radius,
        radius: shell.radius + shell.widest,
        layer: layerSum / ordered.length,
      });
    }
    groups.sort((left, right) => right.members.length - left.members.length);

    if (groups.length === 0) {
      return { groups, lobeShell: 0, radius: Math.max(anchor, padding) };
    }
    const lobes = childShell(anchor, groups, (group) => group.radius, padding);
    return {
      groups,
      lobeShell: lobes.radius,
      radius: lobes.radius + lobes.widest,
    };
  });
}

/**
 * Distributes the cluster balls through the volume and relaxes them.
 *
 * Their starting direction comes from the identity of the cluster itself, not
 * from its position in the tile, so a repository keeps roughly the same corner
 * of the world across levels and reloads. The relaxation then pushes
 * overlapping balls apart, pulls those that depend on each other closer -
 * never closer than touching - and lets the dependency depth decide height.
 */
function placeClusters(
  structure: GraphStructure,
  config: GraphLayoutConfig,
  metrics: Metrics,
  shapes: readonly ClusterShape[],
): Float32Array {
  const count = structure.clusters.length;
  const centers = new Float32Array(count * 3);
  if (count === 0) return centers;

  const ranking = [...structure.clusters.keys()].sort(
    (left, right) =>
      structure.clusters[right].members.length -
        structure.clusters[left].members.length || left - right,
  );
  let volume = 0;
  let widest = 0;
  for (let index = 0; index < count; index += 1) {
    volume += (shapes[index].radius * (1 + config.clusterSpacing)) ** 3;
    widest = Math.max(widest, shapes[index].radius);
  }
  const worldRadius = Math.cbrt(volume) * 1.15;

  let meanLayer = 0;
  for (const cluster of structure.clusters) meanLayer += cluster.layer;
  meanLayer /= count;
  // With a single layer every cluster wants the same height, and pulling them
  // all onto one plane is exactly the flat drawing this layout replaces.
  const layered = structure.layerCount > 1;

  // Even coverage of the sphere needs the Fibonacci spiral, and the spiral
  // needs an index. Taking it from the size ranking would coil the clusters
  // into a cone - big ones on top, small ones below - so the index is a
  // deterministic shuffle by identity instead: evenly spread, and unrelated
  // to how far out the cluster sits.
  const bearing = new Int32Array(count);
  [...structure.clusters.keys()]
    .sort(
      (left, right) =>
        hashUnit(identityOfCluster(structure, left), 23) -
        hashUnit(identityOfCluster(structure, right), 23),
    )
    .forEach((cluster, position) => {
      bearing[cluster] = position;
    });

  ranking.forEach((cluster, rank) => {
    // Big clusters take the middle, small ones the outside: the eye finds the
    // mass of the codebase first.
    const shell =
      count === 1 ? 0 : worldRadius * Math.cbrt((rank + 0.6) / count);
    const seed = identityOfCluster(structure, cluster);
    const [dx, dy, dz] = shellDirection(
      bearing[cluster],
      count,
      seed,
      config.organicJitter,
    );
    centers[cluster * 3] = dx * shell;
    centers[cluster * 3 + 1] =
      dy * shell +
      (layered
        ? (structure.clusters[cluster].layer - meanLayer) * metrics.hierarchy
        : 0);
    centers[cluster * 3 + 2] = dz * shell;
  });

  const displacement = new Float32Array(count * 3);
  // A step longer than the widest ball means the pass overshot; clamping is
  // what separates a relaxation from a system that oscillates apart.
  const maxStep = widest * 0.5;
  for (let pass = 0; pass < config.clusterIterations; pass += 1) {
    const alpha = 1 - pass / config.clusterIterations;
    displacement.fill(0);
    for (let a = 0; a < count; a += 1) {
      for (let b = a + 1; b < count; b += 1) {
        const dx = centers[b * 3] - centers[a * 3];
        const dy = centers[b * 3 + 1] - centers[a * 3 + 1];
        const dz = centers[b * 3 + 2] - centers[a * 3 + 2];
        const distance = Math.hypot(dx, dy, dz) || 1e-6;
        const weight = structure.clusters[a].links.get(b) ?? 0;
        const reach = shapes[a].radius + shapes[b].radius;
        const touching = reach * (1 + config.minClusterSpacing);
        // Two clusters that never meet keep the full gap between them; two
        // that depend on each other may come as close as touching, and no
        // closer, however heavy the dependency.
        const gap = reach * config.clusterSpacing;
        const floor = weight > 0 ? touching : touching + gap;
        let move = 0;
        if (distance < floor) {
          move = -(floor - distance) * 0.5 * alpha;
        } else if (weight > 0) {
          const wanted = touching + gap / (1 + Math.log2(1 + weight));
          if (distance > wanted) {
            move = (distance - wanted) * config.clusterAttraction * alpha;
          }
        }
        if (move === 0) continue;
        const step = Math.max(-maxStep, Math.min(maxStep, move)) / distance;
        displacement[a * 3] += dx * step;
        displacement[a * 3 + 1] += dy * step;
        displacement[a * 3 + 2] += dz * step;
        displacement[b * 3] -= dx * step;
        displacement[b * 3 + 1] -= dy * step;
        displacement[b * 3 + 2] -= dz * step;
      }
    }
    for (let index = 0; index < count; index += 1) {
      // Weak gravity: without it a set of clusters that never reference each
      // other drifts outwards for ever.
      displacement[index * 3] -= centers[index * 3] * 0.012 * alpha;
      displacement[index * 3 + 2] -= centers[index * 3 + 2] * 0.012 * alpha;
      if (layered) {
        const wantedY =
          (structure.clusters[index].layer - meanLayer) * metrics.hierarchy;
        displacement[index * 3 + 1] +=
          (wantedY - centers[index * 3 + 1]) * 0.08 * alpha;
      } else {
        // Nothing depends on anything: height carries no meaning, so it is
        // treated like the other two axes instead of being pulled flat.
        displacement[index * 3 + 1] -= centers[index * 3 + 1] * 0.012 * alpha;
      }
      centers[index * 3] += displacement[index * 3];
      centers[index * 3 + 1] += displacement[index * 3 + 1];
      centers[index * 3 + 2] += displacement[index * 3 + 2];
    }
  }
  return centers;
}

function identityOfCluster(structure: GraphStructure, index: number): number {
  const cluster = structure.clusters[index];
  return cluster.root >= 0 ? cluster.root + 1 : cluster.members[0] + 1;
}

/** Hangs every node on a shell around its own container. */
function layNodes(
  graph: LayoutGraph,
  structure: GraphStructure,
  config: GraphLayoutConfig,
  metrics: Metrics,
  shapes: readonly ClusterShape[],
  centers: Float32Array,
  x: Float32Array,
  y: Float32Array,
  z: Float32Array,
): void {
  for (let index = 0; index < structure.clusters.length; index += 1) {
    const cluster = structure.clusters[index];
    const cx = centers[index * 3];
    const cy = centers[index * 3 + 1];
    const cz = centers[index * 3 + 2];
    const { groups, lobeShell } = shapes[index];

    if (cluster.root >= 0) {
      x[cluster.root] = cx;
      y[cluster.root] = cy;
      z[cluster.root] = cz;
    }

    groups.forEach((group, rank) => {
      const [dx, dy, dz] = shellDirection(
        rank,
        groups.length,
        group.id + 1,
        config.organicJitter,
      );
      const lobeX = cx + dx * lobeShell;
      const lobeY =
        cy +
        dy * lobeShell +
        (group.layer - cluster.layer) *
          metrics.hierarchy *
          config.communityHierarchyBias;
      const lobeZ = cz + dz * lobeShell;
      const memberShell = group.memberShell;

      group.members.forEach((member, position) => {
        const direction = shellDirection(
          position,
          group.members.length,
          graph.identity[member],
          config.organicJitter,
        );
        const wobble =
          1 + hashUnit(graph.identity[member], 7) * config.organicJitter * 0.5;
        x[member] = lobeX + direction[0] * memberShell * wobble;
        y[member] =
          lobeY +
          direction[1] * memberShell * wobble +
          (structure.layer[member] - group.layer) *
            metrics.hierarchy *
            config.nodeHierarchyBias;
        z[member] = lobeZ + direction[2] * memberShell * wobble;
        layDescendants(graph, structure, config, metrics, member, x, y, z);
      });
    });
  }
}

function layDescendants(
  graph: LayoutGraph,
  structure: GraphStructure,
  config: GraphLayoutConfig,
  metrics: Metrics,
  root: number,
  x: Float32Array,
  y: Float32Array,
  z: Float32Array,
): void {
  const queue = [root];
  for (let head = 0; head < queue.length; head += 1) {
    const node = queue[head];
    const children = structure.children[node];
    if (children.length === 0) continue;
    const ordered = [...children].sort(
      (left, right) =>
        structure.importance[right] - structure.importance[left] ||
        graph.identity[left] - graph.identity[right],
    );
    const shell = childShell(
      metrics.radius[node],
      ordered,
      (child) => metrics.subtree[child],
      metrics.padding,
    ).radius;
    ordered.forEach((child, rank) => {
      const direction = shellDirection(
        rank,
        ordered.length,
        graph.identity[child],
        config.organicJitter,
      );
      // Outwards only: the shell is the closest a child may sit, so noise that
      // shortened it would push the child back inside its own parent.
      const wobble =
        1 + hashUnit(graph.identity[child], 11) * config.organicJitter * 0.5;
      x[child] = x[node] + direction[0] * shell * wobble;
      y[child] = y[node] + direction[1] * shell * wobble;
      z[child] = z[node] + direction[2] * shell * wobble;
      queue.push(child);
    });
  }
}

/**
 * A point on a sphere, evenly spread by rank and nudged by identity.
 *
 * The Fibonacci spiral covers the sphere without clumping; the nudge keeps the
 * result from looking machined. Both parts are deterministic, so the drawing
 * is reproducible without being regular.
 */
function shellDirection(
  rank: number,
  count: number,
  seed: number,
  jitter: number,
): readonly [number, number, number] {
  const height =
    count <= 1 ? (hashUnit(seed, 3) - 0.5) * 1.2 : 1 - (2 * rank + 1) / count;
  const wobbled = Math.max(
    -0.999,
    Math.min(
      0.999,
      height + ((hashUnit(seed, 5) - 0.5) * jitter) / Math.max(count, 1),
    ),
  );
  const ring = Math.sqrt(Math.max(0, 1 - wobbled * wobbled));
  const angle =
    GOLDEN_ANGLE * rank + (hashUnit(seed, 2) - 0.5) * jitter * Math.PI;
  return [ring * Math.cos(angle), wobbled, ring * Math.sin(angle)];
}

/** Deterministic `[0, 1)` from an integer key: same node, same nudge, always. */
function hashUnit(key: number, salt: number): number {
  let value = (key ^ (salt * 0x9e3779b9)) >>> 0;
  value = Math.imul(value ^ (value >>> 16), 0x21f0aaad) >>> 0;
  value = Math.imul(value ^ (value >>> 15), 0x735a2d97) >>> 0;
  value = (value ^ (value >>> 15)) >>> 0;
  return value / 4294967296;
}

/**
 * Short local relaxation: collisions, dependencies, and a spring home.
 *
 * Only nodes of the same cluster interact, so no local force can undo the
 * global arrangement. The spring towards the structural target is what keeps
 * the hierarchy visible after the physics has had its say.
 */
function refine(
  graph: LayoutGraph,
  structure: GraphStructure,
  config: GraphLayoutConfig,
  metrics: Metrics,
  x: Float32Array,
  y: Float32Array,
  z: Float32Array,
): void {
  const count = graph.nodeCount;
  if (count === 0) return;
  const radius = metrics.radius;
  const targetX = Float32Array.from(x);
  const targetY = Float32Array.from(y);
  const targetZ = Float32Array.from(z);
  const dx = new Float32Array(count);
  const dy = new Float32Array(count);
  const dz = new Float32Array(count);

  let widest = 0;
  for (let index = 0; index < count; index += 1) {
    widest = Math.max(widest, radius[index]);
  }
  const cell = Math.max(widest * 2, 1);
  const buckets = new Map<number, number[]>();

  const intra: number[] = [];
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    const source = graph.edgeSource[edge];
    const target = graph.edgeTarget[edge];
    if (structure.cluster[source] !== structure.cluster[target]) continue;
    intra.push(edge);
  }

  for (let pass = 0; pass < config.refineIterations; pass += 1) {
    dx.fill(0);
    dy.fill(0);
    dz.fill(0);
    buckets.clear();
    for (let index = 0; index < count; index += 1) {
      const key = cellKey(x[index], y[index], z[index], cell);
      const bucket = buckets.get(key);
      if (bucket === undefined) buckets.set(key, [index]);
      else bucket.push(index);
    }

    for (let index = 0; index < count; index += 1) {
      const cx = Math.floor(x[index] / cell);
      const cy = Math.floor(y[index] / cell);
      const cz = Math.floor(z[index] / cell);
      for (let ox = -1; ox <= 1; ox += 1) {
        for (let oy = -1; oy <= 1; oy += 1) {
          for (let oz = -1; oz <= 1; oz += 1) {
            const bucket = buckets.get(hashCell(cx + ox, cy + oy, cz + oz));
            if (bucket === undefined) continue;
            for (const other of bucket) {
              if (other <= index) continue;
              const sx = x[other] - x[index];
              const sy = y[other] - y[index];
              const sz = z[other] - z[index];
              const distance = Math.hypot(sx, sy, sz) || 1e-6;
              const wanted = radius[index] + radius[other] + metrics.padding;
              if (distance >= wanted) continue;
              const push =
                ((wanted - distance) / distance) *
                0.5 *
                config.collisionStrength;
              dx[index] -= sx * push;
              dy[index] -= sy * push;
              dz[index] -= sz * push;
              dx[other] += sx * push;
              dy[other] += sy * push;
              dz[other] += sz * push;
            }
          }
        }
      }
    }

    for (const edge of intra) {
      const source = graph.edgeSource[edge];
      const target = graph.edgeTarget[edge];
      const sx = x[target] - x[source];
      const sy = y[target] - y[source];
      const sz = z[target] - z[source];
      const distance = Math.hypot(sx, sy, sz) || 1e-6;
      const resting =
        (radius[source] + radius[target]) *
        (structure.community[source] === structure.community[target]
          ? config.linkDistance.sameCommunity
          : config.linkDistance.sameCluster);
      const pull =
        ((distance - resting) / distance) * config.linkStrength * 0.5;
      dx[source] += sx * pull;
      dy[source] += sy * pull;
      dz[source] += sz * pull;
      dx[target] -= sx * pull;
      dy[target] -= sy * pull;
      dz[target] -= sz * pull;
    }

    for (let index = 0; index < count; index += 1) {
      x[index] +=
        dx[index] + (targetX[index] - x[index]) * config.structuralSpring;
      y[index] +=
        dy[index] + (targetY[index] - y[index]) * config.structuralSpring;
      z[index] +=
        dz[index] + (targetZ[index] - z[index]) * config.structuralSpring;
    }
  }
}

function cellKey(x: number, y: number, z: number, cell: number): number {
  return hashCell(
    Math.floor(x / cell),
    Math.floor(y / cell),
    Math.floor(z / cell),
  );
}

function hashCell(x: number, y: number, z: number): number {
  return (
    (Math.imul(x, 73856093) ^ Math.imul(y, 19349663) ^ Math.imul(z, 83492791)) |
    0
  );
}

/**
 * Centres the world, measures it, and rescues an axis that collapsed.
 *
 * Depth that exists only in the data is worth nothing: if one axis ends up far
 * narrower than the widest, the drawing is a wall no matter what the numbers
 * say. Stretching about the centroid only increases distances, so it cannot
 * create the overlaps the placement just avoided.
 */
function finish(
  x: Float32Array,
  y: Float32Array,
  z: Float32Array,
  radius: Float32Array,
  rank: Int32Array,
  config: GraphLayoutConfig,
): Placement {
  const count = x.length;
  if (count === 0) {
    return {
      x,
      y,
      z,
      radius,
      rank,
      center: [0, 0, 0],
      boundingRadius: 0,
      spread: [0, 0, 0],
    };
  }

  const center = centroid(x, y, z);
  const axes = [x, y, z];
  const spread = axes.map((axis, position) =>
    deviation(axis, center[position]),
  );
  const widest = Math.max(...spread);
  for (let axis = 0; axis < axes.length; axis += 1) {
    const wanted = widest * config.minAxisSpreadRatio;
    if (spread[axis] >= wanted || spread[axis] <= 0) continue;
    const factor = Math.min(3, wanted / spread[axis]);
    const values = axes[axis];
    for (let index = 0; index < count; index += 1) {
      values[index] = center[axis] + (values[index] - center[axis]) * factor;
    }
    spread[axis] *= factor;
  }

  const middle = centroid(x, y, z);
  // The sphere the camera frames holds the bulk, not the last outlier: one
  // repository parked far out would otherwise shrink the whole graph into the
  // middle third of the screen. The stragglers stay reachable by panning.
  const reach = new Float64Array(count);
  for (let index = 0; index < count; index += 1) {
    reach[index] =
      Math.hypot(
        x[index] - middle[0],
        y[index] - middle[1],
        z[index] - middle[2],
      ) + radius[index];
  }
  reach.sort();
  const boundingRadius =
    reach[Math.min(count - 1, Math.floor(count * config.boundingQuantile))];
  return {
    x,
    y,
    z,
    radius,
    rank,
    center: middle,
    boundingRadius,
    spread: [spread[0], spread[1], spread[2]],
  };
}

function centroid(
  x: Float32Array,
  y: Float32Array,
  z: Float32Array,
): [number, number, number] {
  let sx = 0;
  let sy = 0;
  let sz = 0;
  for (let index = 0; index < x.length; index += 1) {
    sx += x[index];
    sy += y[index];
    sz += z[index];
  }
  const count = Math.max(x.length, 1);
  return [sx / count, sy / count, sz / count];
}

function deviation(values: Float32Array, mean: number): number {
  let total = 0;
  for (let index = 0; index < values.length; index += 1) {
    total += (values[index] - mean) ** 2;
  }
  return Math.sqrt(total / Math.max(values.length, 1));
}
