import { NODE_KIND_REPOSITORY } from "@/renderer/binary";

/**
 * The tile as the layout needs to see it: a containment forest plus a weighted
 * dependency digraph over the same node indices.
 */
export interface LayoutGraph {
  readonly nodeCount: number;
  /** Node kind per index, as published by the payload. */
  readonly kind: Uint8Array;
  /** Container index per node, or `-1` when the container is not in the tile. */
  readonly parent: Int32Array;
  /**
   * Stable hash of the node's own identity, not of its position in the tile.
   * Seeding the layout with it is what keeps a package in the same corner of
   * the world when the level or the node budget changes.
   */
  readonly identity: Uint32Array;
  readonly edgeSource: Int32Array;
  readonly edgeTarget: Int32Array;
  readonly edgeWeight: Float32Array;
}

export interface LayoutCluster {
  /** Container every member hangs from; `-1` for a cluster with no root. */
  readonly root: number;
  readonly members: readonly number[];
  /** Mean dependency layer of the members: where the cluster sits vertically. */
  readonly layer: number;
  /** Weighted dependencies to other clusters, keyed by cluster index. */
  readonly links: ReadonlyMap<number, number>;
}

export interface GraphStructure {
  readonly children: readonly (readonly number[])[];
  readonly cluster: Int32Array;
  readonly clusters: readonly LayoutCluster[];
  /** Community inside the cluster; siblings that depend on each other share one. */
  readonly community: Int32Array;
  /** Depth in the condensed dependency DAG: 0 is what nothing depends on. */
  readonly layer: Int32Array;
  readonly layerCount: number;
  /** Centrality in `[0, 1]`, from PageRank over the dependency graph. */
  readonly importance: Float32Array;
  readonly degree: Int32Array;
}

/** Containment nests four kinds; anything deeper is a malformed payload. */
const MAX_CONTAINMENT_DEPTH = 8;

/**
 * Derives every structural fact the placement needs, in one pass over the tile.
 *
 * Nothing here looks at a coordinate: the shape of the drawing comes from the
 * repositories, the containment forest and the dependency graph, which is the
 * only way the picture can claim to show the architecture.
 */
export function buildStructure(graph: LayoutGraph): GraphStructure {
  const children = buildChildren(graph);
  const roots = resolveRoots(graph);
  const { cluster, clusterRoots } = assignClusters(graph, roots);
  const degree = countDegree(graph);
  const layer = computeLayers(graph);
  const importance = computePageRank(graph);
  const clusters = describeClusters(graph, cluster, clusterRoots, layer);
  const community = detectCommunities(graph, children, cluster, clusters);

  let layerCount = 0;
  for (let index = 0; index < graph.nodeCount; index += 1) {
    layerCount = Math.max(layerCount, layer[index] + 1);
  }

  return {
    children,
    cluster,
    clusters,
    community,
    layer,
    layerCount,
    importance,
    degree,
  };
}

function buildChildren(graph: LayoutGraph): readonly (readonly number[])[] {
  const children: number[][] = Array.from(
    { length: graph.nodeCount },
    () => [],
  );
  for (let index = 0; index < graph.nodeCount; index += 1) {
    const parent = graph.parent[index];
    if (parent >= 0 && parent !== index) children[parent].push(index);
  }
  return children;
}

/** Topmost container of each node inside this tile; itself when it has none. */
function resolveRoots(graph: LayoutGraph): Int32Array {
  const roots = new Int32Array(graph.nodeCount);
  for (let index = 0; index < graph.nodeCount; index += 1) {
    let current = index;
    for (let step = 0; step < MAX_CONTAINMENT_DEPTH; step += 1) {
      const parent = graph.parent[current];
      if (parent < 0 || parent === current) break;
      current = parent;
    }
    roots[index] = current;
  }
  return roots;
}

/**
 * One cluster per repository. Nodes whose repository did not make it into the
 * tile keep their own root, and those roots are merged into components by the
 * dependencies between them: a handful of orphan packages that call each other
 * read as one neighbourhood, not as a scatter of singletons.
 */
function assignClusters(
  graph: LayoutGraph,
  roots: Int32Array,
): { cluster: Int32Array; clusterRoots: number[] } {
  const parentOf = new Int32Array(graph.nodeCount);
  for (let index = 0; index < graph.nodeCount; index += 1)
    parentOf[index] = index;

  const find = (node: number): number => {
    let root = node;
    while (parentOf[root] !== root) root = parentOf[root];
    for (let walk = node; parentOf[walk] !== root; ) {
      const next = parentOf[walk];
      parentOf[walk] = root;
      walk = next;
    }
    return root;
  };

  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    const left = roots[graph.edgeSource[edge]];
    const right = roots[graph.edgeTarget[edge]];
    if (left === right) continue;
    if (
      graph.kind[left] === NODE_KIND_REPOSITORY ||
      graph.kind[right] === NODE_KIND_REPOSITORY
    ) {
      continue;
    }
    const a = find(left);
    const b = find(right);
    if (a !== b) parentOf[Math.max(a, b)] = Math.min(a, b);
  }

  const cluster = new Int32Array(graph.nodeCount).fill(-1);
  const clusterRoots: number[] = [];
  const byRepresentative = new Map<number, number>();
  for (let index = 0; index < graph.nodeCount; index += 1) {
    const representative = find(roots[index]);
    let id = byRepresentative.get(representative);
    if (id === undefined) {
      id = clusterRoots.length;
      byRepresentative.set(representative, id);
      // A merged component has no single container to anchor on.
      clusterRoots.push(representative === roots[index] ? roots[index] : -1);
    }
    cluster[index] = id;
  }
  for (let index = 0; index < graph.nodeCount; index += 1) {
    const id = cluster[index];
    if (clusterRoots[id] >= 0 && graph.parent[clusterRoots[id]] >= 0) {
      clusterRoots[id] = -1;
    }
  }
  return { cluster, clusterRoots };
}

function countDegree(graph: LayoutGraph): Int32Array {
  const degree = new Int32Array(graph.nodeCount);
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    degree[graph.edgeSource[edge]] += 1;
    degree[graph.edgeTarget[edge]] += 1;
  }
  return degree;
}

function describeClusters(
  graph: LayoutGraph,
  cluster: Int32Array,
  clusterRoots: readonly number[],
  layer: Int32Array,
): readonly LayoutCluster[] {
  const members: number[][] = clusterRoots.map(() => []);
  const layerSum = new Float64Array(clusterRoots.length);
  for (let index = 0; index < graph.nodeCount; index += 1) {
    members[cluster[index]].push(index);
    layerSum[cluster[index]] += layer[index];
  }
  const links = clusterRoots.map(() => new Map<number, number>());
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    const from = cluster[graph.edgeSource[edge]];
    const to = cluster[graph.edgeTarget[edge]];
    if (from === to) continue;
    const weight = graph.edgeWeight[edge];
    links[from].set(to, (links[from].get(to) ?? 0) + weight);
    links[to].set(from, (links[to].get(from) ?? 0) + weight);
  }
  return clusterRoots.map((root, index) => ({
    root,
    members: members[index],
    layer:
      members[index].length > 0 ? layerSum[index] / members[index].length : 0,
    links: links[index],
  }));
}

/**
 * Depth in the dependency graph, with cycles collapsed first.
 *
 * A real codebase has cycles, and a longest-path depth over a cyclic graph is
 * undefined. Tarjan's components are condensed into a DAG, the depth is
 * computed there, and every node of a cycle shares the depth of its component:
 * mutually recursive packages belong to the same layer, which is what they are.
 */
function computeLayers(graph: LayoutGraph): Int32Array {
  const component = stronglyConnectedComponents(graph);
  let componentCount = 0;
  for (let index = 0; index < graph.nodeCount; index += 1) {
    componentCount = Math.max(componentCount, component[index] + 1);
  }

  const incoming = new Int32Array(componentCount);
  const condensed: number[][] = Array.from(
    { length: componentCount },
    () => [],
  );
  const seen = new Set<number>();
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    const from = component[graph.edgeSource[edge]];
    const to = component[graph.edgeTarget[edge]];
    if (from === to) continue;
    const key = from * componentCount + to;
    if (seen.has(key)) continue;
    seen.add(key);
    condensed[from].push(to);
    incoming[to] += 1;
  }

  const depth = new Int32Array(componentCount);
  const queue: number[] = [];
  for (let index = 0; index < componentCount; index += 1) {
    if (incoming[index] === 0) queue.push(index);
  }
  for (let head = 0; head < queue.length; head += 1) {
    const current = queue[head];
    for (const next of condensed[current]) {
      depth[next] = Math.max(depth[next], depth[current] + 1);
      incoming[next] -= 1;
      if (incoming[next] === 0) queue.push(next);
    }
  }

  const layer = new Int32Array(graph.nodeCount);
  for (let index = 0; index < graph.nodeCount; index += 1) {
    layer[index] = depth[component[index]];
  }
  return layer;
}

/** Tarjan's algorithm, iterative: a deep call stack is a crash, not a layout. */
function stronglyConnectedComponents(graph: LayoutGraph): Int32Array {
  const heads = new Int32Array(graph.nodeCount + 1);
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    heads[graph.edgeSource[edge] + 1] += 1;
  }
  for (let index = 0; index < graph.nodeCount; index += 1) {
    heads[index + 1] += heads[index];
  }
  const targets = new Int32Array(graph.edgeSource.length);
  const cursor = heads.slice(0, graph.nodeCount);
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    const source = graph.edgeSource[edge];
    targets[cursor[source]] = graph.edgeTarget[edge];
    cursor[source] += 1;
  }

  const index = new Int32Array(graph.nodeCount).fill(-1);
  const low = new Int32Array(graph.nodeCount);
  const onStack = new Uint8Array(graph.nodeCount);
  const component = new Int32Array(graph.nodeCount).fill(-1);
  const stack: number[] = [];
  const frames: number[] = [];
  const edgeCursor: number[] = [];
  let counter = 0;
  let components = 0;

  for (let start = 0; start < graph.nodeCount; start += 1) {
    if (index[start] >= 0) continue;
    frames.push(start);
    edgeCursor.push(heads[start]);
    index[start] = counter;
    low[start] = counter;
    counter += 1;
    stack.push(start);
    onStack[start] = 1;

    while (frames.length > 0) {
      const node = frames[frames.length - 1];
      const at = edgeCursor[edgeCursor.length - 1];
      if (at < heads[node + 1]) {
        edgeCursor[edgeCursor.length - 1] = at + 1;
        const next = targets[at];
        if (index[next] < 0) {
          index[next] = counter;
          low[next] = counter;
          counter += 1;
          stack.push(next);
          onStack[next] = 1;
          frames.push(next);
          edgeCursor.push(heads[next]);
        } else if (onStack[next] === 1) {
          low[node] = Math.min(low[node], index[next]);
        }
        continue;
      }
      frames.pop();
      edgeCursor.pop();
      if (frames.length > 0) {
        const parent = frames[frames.length - 1];
        low[parent] = Math.min(low[parent], low[node]);
      }
      if (low[node] === index[node]) {
        for (;;) {
          const member = stack.pop() as number;
          onStack[member] = 0;
          component[member] = components;
          if (member === node) break;
        }
        components += 1;
      }
    }
  }
  return component;
}

/**
 * PageRank over the dependency graph, used as the importance of a node.
 *
 * Degree alone calls every file with many symbols a hub. PageRank asks who
 * depends on you and how important they are, which is the question a reader
 * is actually asking when they look for the centre of a codebase.
 */
function computePageRank(graph: LayoutGraph): Float32Array {
  const count = graph.nodeCount;
  const rank = new Float64Array(count).fill(1 / Math.max(count, 1));
  const next = new Float64Array(count);
  const outWeight = new Float64Array(count);
  for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
    outWeight[graph.edgeSource[edge]] += graph.edgeWeight[edge];
  }

  const damping = 0.85;
  for (let pass = 0; pass < 24; pass += 1) {
    let dangling = 0;
    for (let index = 0; index < count; index += 1) {
      next[index] = 0;
      if (outWeight[index] === 0) dangling += rank[index];
    }
    for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
      const source = graph.edgeSource[edge];
      if (outWeight[source] === 0) continue;
      next[graph.edgeTarget[edge]] +=
        (rank[source] * graph.edgeWeight[edge]) / outWeight[source];
    }
    const leak = (1 - damping + damping * dangling) / Math.max(count, 1);
    for (let index = 0; index < count; index += 1) {
      rank[index] = leak + damping * next[index];
    }
  }

  let highest = 0;
  for (let index = 0; index < count; index += 1) {
    highest = Math.max(highest, rank[index]);
  }
  const importance = new Float32Array(count);
  if (highest <= 0) return importance;
  for (let index = 0; index < count; index += 1) {
    // The distribution is long tailed; the square root keeps the difference
    // between a hub and a leaf readable without erasing the middle.
    importance[index] = Math.sqrt(rank[index] / highest);
  }
  return importance;
}

/**
 * Groups the direct children of each cluster root into communities.
 *
 * Two packages of the same repository that call each other belong beside each
 * other; two that never meet do not. Louvain answers that over the dependency
 * weight between the subtrees, and every descendant inherits the community of
 * the child it hangs from, so a file lands with its package.
 */
function detectCommunities(
  graph: LayoutGraph,
  children: readonly (readonly number[])[],
  cluster: Int32Array,
  clusters: readonly LayoutCluster[],
): Int32Array {
  const community = new Int32Array(graph.nodeCount).fill(-1);
  const branch = new Int32Array(graph.nodeCount).fill(-1);

  // Which top-level child of the cluster each node descends from.
  for (
    let clusterIndex = 0;
    clusterIndex < clusters.length;
    clusterIndex += 1
  ) {
    const root = clusters[clusterIndex].root;
    if (root < 0) continue;
    for (const child of children[root]) {
      const queue = [child];
      for (let head = 0; head < queue.length; head += 1) {
        const node = queue[head];
        branch[node] = child;
        for (const grandChild of children[node]) queue.push(grandChild);
      }
    }
  }

  let nextCommunity = 0;
  for (
    let clusterIndex = 0;
    clusterIndex < clusters.length;
    clusterIndex += 1
  ) {
    const root = clusters[clusterIndex].root;
    const branches =
      root >= 0
        ? children[root]
        : clusters[clusterIndex].members.filter(
            (node) => graph.parent[node] < 0,
          );
    if (branches.length === 0) continue;

    const slot = new Map<number, number>();
    branches.forEach((node, position) => {
      slot.set(node, position);
    });
    if (root < 0) {
      for (const member of clusters[clusterIndex].members) {
        if (branch[member] < 0) {
          let walk = member;
          while (graph.parent[walk] >= 0) walk = graph.parent[walk];
          branch[member] = walk;
        }
      }
    }

    const weights = new Map<number, number>();
    for (let edge = 0; edge < graph.edgeSource.length; edge += 1) {
      const source = graph.edgeSource[edge];
      if (cluster[source] !== clusterIndex) continue;
      const left = slot.get(branch[source]);
      const right = slot.get(branch[graph.edgeTarget[edge]]);
      if (left === undefined || right === undefined || left === right) continue;
      const key =
        Math.min(left, right) * branches.length + Math.max(left, right);
      weights.set(key, (weights.get(key) ?? 0) + graph.edgeWeight[edge]);
    }
    const links = [...weights].map(([key, weight]) => ({
      a: Math.floor(key / branches.length),
      b: key % branches.length,
      weight,
    }));

    const assignment = louvain(branches.length, links);
    const remapped = new Map<number, number>();
    branches.forEach((node, position) => {
      const local = assignment[position];
      let id = remapped.get(local);
      if (id === undefined) {
        id = nextCommunity;
        nextCommunity += 1;
        remapped.set(local, id);
      }
      community[node] = id;
    });
    for (const member of clusters[clusterIndex].members) {
      const owner = branch[member];
      community[member] = owner >= 0 ? community[owner] : -1;
    }
  }

  for (let index = 0; index < graph.nodeCount; index += 1) {
    if (community[index] >= 0) continue;
    community[index] = nextCommunity;
    nextCommunity += 1;
  }
  return community;
}

interface CommunityLink {
  readonly a: number;
  readonly b: number;
  readonly weight: number;
}

/**
 * Louvain modularity optimisation: local moving followed by aggregation.
 *
 * Ties resolve towards the lowest community index and every scan runs in index
 * order, so the same graph always produces the same partition — the layout is
 * meant to build spatial memory, and a partition that flips between reloads
 * would destroy it.
 */
export function louvain(
  size: number,
  links: readonly CommunityLink[],
): Int32Array {
  const result = new Int32Array(size);
  for (let index = 0; index < size; index += 1) result[index] = index;
  if (size <= 1 || links.length === 0) return result;

  let nodes = size;
  let current = links.map((link) => ({ ...link }));
  let mapping = new Int32Array(size);
  for (let index = 0; index < size; index += 1) mapping[index] = index;

  for (let level = 0; level < 4; level += 1) {
    const assignment = localMoving(nodes, current);
    let communityCount = 0;
    const relabel = new Map<number, number>();
    const compact = new Int32Array(nodes);
    for (let index = 0; index < nodes; index += 1) {
      let id = relabel.get(assignment[index]);
      if (id === undefined) {
        id = communityCount;
        communityCount += 1;
        relabel.set(assignment[index], id);
      }
      compact[index] = id;
    }
    if (communityCount === nodes) break;

    const next = new Int32Array(size);
    for (let index = 0; index < size; index += 1)
      next[index] = compact[mapping[index]];
    mapping = next;

    const merged = new Map<number, CommunityLink>();
    for (const link of current) {
      const a = Math.min(compact[link.a], compact[link.b]);
      const b = Math.max(compact[link.a], compact[link.b]);
      if (a === b) continue;
      const key = a * communityCount + b;
      const existing = merged.get(key);
      merged.set(
        key,
        existing === undefined
          ? { a, b, weight: link.weight }
          : { a, b, weight: existing.weight + link.weight },
      );
    }
    nodes = communityCount;
    current = [...merged.values()];
    if (current.length === 0) break;
  }
  return mapping;
}

function localMoving(
  size: number,
  links: readonly CommunityLink[],
): Int32Array {
  const neighbours: { node: number; weight: number }[][] = Array.from(
    { length: size },
    () => [],
  );
  const strength = new Float64Array(size);
  let total = 0;
  for (const link of links) {
    neighbours[link.a].push({ node: link.b, weight: link.weight });
    neighbours[link.b].push({ node: link.a, weight: link.weight });
    strength[link.a] += link.weight;
    strength[link.b] += link.weight;
    total += link.weight;
  }
  const twice = 2 * Math.max(total, Number.EPSILON);

  const community = new Int32Array(size);
  const totalWeight = new Float64Array(size);
  for (let index = 0; index < size; index += 1) {
    community[index] = index;
    totalWeight[index] = strength[index];
  }

  for (let pass = 0; pass < 12; pass += 1) {
    let moved = false;
    for (let node = 0; node < size; node += 1) {
      const own = community[node];
      totalWeight[own] -= strength[node];
      const weightTo = new Map<number, number>();
      for (const neighbour of neighbours[node]) {
        const target = community[neighbour.node];
        weightTo.set(target, (weightTo.get(target) ?? 0) + neighbour.weight);
      }
      let best = own;
      let bestGain =
        (weightTo.get(own) ?? 0) - (totalWeight[own] * strength[node]) / twice;
      for (const [candidate, weight] of weightTo) {
        const gain = weight - (totalWeight[candidate] * strength[node]) / twice;
        if (
          gain > bestGain + 1e-12 ||
          (gain > bestGain - 1e-12 && candidate < best)
        ) {
          best = candidate;
          bestGain = gain;
        }
      }
      totalWeight[best] += strength[node];
      if (best !== own) {
        community[node] = best;
        moved = true;
      }
    }
    if (!moved) break;
  }
  return community;
}
