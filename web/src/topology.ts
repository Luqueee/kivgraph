import type {
  TopologyNodeReference,
  TopologyRelationship,
  TopologyResponse,
} from "@/api/client";

export const ALL_TOPOLOGY_FILTER = "__all__";

export type TopologyNodeType =
  | "profile"
  | "repository"
  | "worktree"
  | "shared_input";

export interface TopologyNode {
  readonly key: string;
  readonly id: string;
  readonly type: TopologyNodeType;
  readonly label: string;
  readonly subtitle: string;
  readonly status: string;
  readonly profileIds: readonly string[];
  readonly worktreeIds: readonly string[];
  readonly repositoryIds: readonly string[];
  readonly languages: readonly string[];
}

export interface TopologyEdge {
  readonly key: string;
  readonly relationshipIndex: number;
  readonly sourceKey: string;
  readonly targetKey?: string;
  readonly relationship: TopologyRelationship;
}

export interface TopologyLayoutNode {
  readonly key: string;
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

export interface TopologyLayout {
  readonly width: number;
  readonly height: number;
  readonly nodes: readonly TopologyLayoutNode[];
}

export interface TopologyBoundary {
  readonly leftProfile: string;
  readonly rightProfile: string;
  readonly kind: "cross_profile_not_evaluated";
}

export interface TopologyModel {
  readonly nodes: readonly TopologyNode[];
  readonly edges: readonly TopologyEdge[];
  readonly relationships: readonly TopologyRelationship[];
  readonly unrenderedRelationships: readonly TopologyRelationship[];
  readonly boundaries: readonly TopologyBoundary[];
  readonly layout: TopologyLayout;
}

export interface TopologyFilters {
  readonly query: string;
  readonly profile: string;
  readonly worktree: string;
  readonly repository: string;
  readonly language: string;
  readonly edgeKind: string;
}

const NODE_WIDTH = 228;
const NODE_HEIGHT = 110;
const COLUMN_GAP = 36;
const ROW_GAP = 32;
const PADDING = 24;

const NODE_TYPE_ORDER: readonly TopologyNodeType[] = [
  "profile",
  "worktree",
  "repository",
  "shared_input",
];

const NODE_LAYER: Readonly<Record<TopologyNodeType, number>> = {
  profile: 0,
  worktree: 1,
  repository: 2,
  shared_input: 1,
};

function nodeKey(type: string, id: string): string {
  return `${type}:${id}`;
}

function sharedInputNodeKey(type: string, id: string): string {
  return nodeKey("shared_input", `${type}:${id}`);
}

function sortStrings(values: Iterable<string>): string[] {
  return [...new Set(values)].sort((left, right) =>
    left < right ? -1 : left > right ? 1 : 0,
  );
}

function profileForWorktree(
  response: TopologyResponse,
): Map<string, readonly string[]> {
  const profiles = new Map<string, string[]>();
  for (const profile of response.profiles) {
    for (const worktree of profile.worktrees) {
      const owners = profiles.get(worktree) ?? [];
      owners.push(profile.id);
      profiles.set(worktree, owners);
    }
  }
  return new Map(
    [...profiles.entries()].map(([worktree, owners]) => [
      worktree,
      sortStrings(owners),
    ]),
  );
}

function sourceStatuses(
  response: TopologyResponse,
): Map<string, readonly string[]> {
  const statuses = new Map<string, string[]>();
  for (const source of response.sources) {
    const values = statuses.get(source.worktree) ?? [];
    values.push(source.status);
    statuses.set(source.worktree, values);
  }
  return new Map(
    [...statuses.entries()].map(([worktree, values]) => [
      worktree,
      sortStrings(values),
    ]),
  );
}

function statusRank(status: string): number {
  return (
    {
      unavailable: 7,
      missing: 6,
      conflict: 5,
      stale: 4,
      partial: 3,
      unknown: 2,
      current: 1,
      ready: 1,
      shared: 0,
    }[status] ?? 0
  );
}

function aggregateStatus(
  statuses: readonly string[],
  fallback: string,
): string {
  if (statuses.length === 0) {
    return fallback;
  }
  return statuses.reduce((selected, status) => {
    const selectedRank = statusRank(selected);
    const statusRankValue = statusRank(status);
    if (statusRankValue !== selectedRank) {
      return statusRankValue > selectedRank ? status : selected;
    }
    return status < selected ? status : selected;
  });
}

function repositoriesByWorktree(
  response: TopologyResponse,
): Map<string, string> {
  return new Map(
    response.worktrees.map((worktree) => [worktree.id, worktree.repository]),
  );
}

function languagesByRepository(
  response: TopologyResponse,
): Map<string, readonly string[]> {
  return new Map(
    response.repositories.map((repository) => [
      repository.id,
      repository.languages,
    ]),
  );
}

function languagesForRepositories(
  repositories: readonly string[],
  languages: Map<string, readonly string[]>,
): readonly string[] {
  return sortStrings(
    repositories.flatMap((repository) => languages.get(repository) ?? []),
  );
}

function nodeReferences(
  type: TopologyNodeType,
  id: string,
): TopologyNodeReference {
  return { type, id };
}

function sharedInputRelationships(
  response: TopologyResponse,
): TopologyRelationship[] {
  const relationships: TopologyRelationship[] = [];
  for (const input of response.sharedInputs) {
    for (const owner of input.owners) {
      relationships.push({
        profile: owner,
        type: "shared_input_usage",
        source: nodeReferences("profile", owner),
        target: nodeReferences("shared_input", `${input.type}:${input.id}`),
        kind: "shared_input_usage",
        status: "structural",
        confidence: "STRUCTURAL_CERTAIN",
        provenance: "TOPOLOGY_DECLARATION",
        reason: `profile ${owner} owns shared ${input.type} ${input.id}`,
      });
    }
  }
  return relationships;
}

function createNodes(response: TopologyResponse): TopologyNode[] {
  const repositoryForWorktree = repositoriesByWorktree(response);
  const languages = languagesByRepository(response);
  const profilesForWorktree = profileForWorktree(response);
  const statusesForWorktree = sourceStatuses(response);
  const nodes: TopologyNode[] = [];

  for (const profile of response.profiles) {
    const repositories = sortStrings(
      profile.worktrees.map(
        (worktree) => repositoryForWorktree.get(worktree) ?? "",
      ),
    ).filter((repository) => repository.length > 0);
    nodes.push({
      key: nodeKey("profile", profile.id),
      id: profile.id,
      type: "profile",
      label: profile.id,
      subtitle: `${profile.worktrees.length} worktree${profile.worktrees.length === 1 ? "" : "s"}`,
      status: profile.status,
      profileIds: [profile.id],
      worktreeIds: profile.worktrees,
      repositoryIds: repositories,
      languages: languagesForRepositories(repositories, languages),
    });
  }

  for (const repository of response.repositories) {
    const worktrees = response.worktrees
      .filter((worktree) => worktree.repository === repository.id)
      .map((worktree) => worktree.id);
    const profiles = sortStrings(
      worktrees.flatMap((worktree) => profilesForWorktree.get(worktree) ?? []),
    );
    nodes.push({
      key: nodeKey("repository", repository.id),
      id: repository.id,
      type: "repository",
      label: repository.name ?? repository.id,
      subtitle: repository.id,
      status: "ready",
      profileIds: profiles,
      worktreeIds: sortStrings(worktrees),
      repositoryIds: [repository.id],
      languages: repository.languages,
    });
  }

  for (const worktree of response.worktrees) {
    const statuses = statusesForWorktree.get(worktree.id) ?? [];
    nodes.push({
      key: nodeKey("worktree", worktree.id),
      id: worktree.id,
      type: "worktree",
      label: worktree.id,
      subtitle: worktree.path,
      status: aggregateStatus(statuses, "unknown"),
      profileIds: profilesForWorktree.get(worktree.id) ?? [],
      worktreeIds: [worktree.id],
      repositoryIds: [worktree.repository],
      languages: languages.get(worktree.repository) ?? [],
    });
  }

  for (const input of response.sharedInputs) {
    const repositories =
      input.type === "worktree"
        ? [repositoryForWorktree.get(input.id) ?? ""].filter(
            (repository) => repository.length > 0,
          )
        : [];
    nodes.push({
      key: sharedInputNodeKey(input.type, input.id),
      id: `${input.type}:${input.id}`,
      type: "shared_input",
      label: `shared ${input.type}`,
      subtitle: input.id,
      status: "shared",
      profileIds: sortStrings(input.owners),
      worktreeIds: input.type === "worktree" ? [input.id] : [],
      repositoryIds: repositories,
      languages: languagesForRepositories(repositories, languages),
    });
  }
  return nodes.sort((left, right) => {
    const typeDifference =
      NODE_TYPE_ORDER.indexOf(left.type) - NODE_TYPE_ORDER.indexOf(right.type);
    return typeDifference === 0
      ? left.id < right.id
        ? -1
        : left.id > right.id
          ? 1
          : 0
      : typeDifference;
  });
}

function layoutNodes(nodes: readonly TopologyNode[]): TopologyLayout {
  const layerCount = Math.max(...Object.values(NODE_LAYER)) + 1;
  const layerCounts = new Map<number, number>();
  const layoutNodes = nodes.map((node) => {
    const layer = NODE_LAYER[node.type];
    const row = layerCounts.get(layer) ?? 0;
    layerCounts.set(layer, row + 1);
    return {
      key: node.key,
      x: PADDING + layer * (NODE_WIDTH + COLUMN_GAP),
      y: PADDING + row * (NODE_HEIGHT + ROW_GAP),
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
    };
  });
  const maxRows = Math.max(1, ...layerCounts.values());
  return {
    width:
      PADDING * 2 + layerCount * NODE_WIDTH + (layerCount - 1) * COLUMN_GAP,
    height: PADDING * 2 + maxRows * NODE_HEIGHT + (maxRows - 1) * ROW_GAP,
    nodes: layoutNodes,
  };
}

function relationshipNodeKey(reference: TopologyNodeReference): string {
  return reference.type === "shared_input"
    ? nodeKey("shared_input", reference.id)
    : nodeKey(reference.type, reference.id);
}

function buildEdges(
  relationships: readonly TopologyRelationship[],
  nodes: readonly TopologyNode[],
): {
  readonly edges: TopologyEdge[];
  readonly unrendered: TopologyRelationship[];
} {
  const keys = new Set(nodes.map((node) => node.key));
  const edges: TopologyEdge[] = [];
  const unrendered: TopologyRelationship[] = [];
  relationships.forEach((relationship, relationshipIndex) => {
    const sourceKey = relationshipNodeKey(relationship.source);
    const targetKey = relationship.target
      ? relationshipNodeKey(relationship.target)
      : undefined;
    if (
      !keys.has(sourceKey) ||
      (targetKey !== undefined && !keys.has(targetKey))
    ) {
      unrendered.push(relationship);
      return;
    }
    edges.push({
      key: `relationship:${relationshipIndex}`,
      relationshipIndex,
      sourceKey,
      targetKey,
      relationship,
    });
  });
  return { edges, unrendered };
}

function createBoundaries(response: TopologyResponse): TopologyBoundary[] {
  const profiles = [...response.selectedProfiles].sort((left, right) =>
    left < right ? -1 : left > right ? 1 : 0,
  );
  const boundaries: TopologyBoundary[] = [];
  for (let left = 0; left < profiles.length; left += 1) {
    for (let right = left + 1; right < profiles.length; right += 1) {
      boundaries.push({
        leftProfile: profiles[left],
        rightProfile: profiles[right],
        kind: "cross_profile_not_evaluated",
      });
    }
  }
  return boundaries;
}

function matchesNode(node: TopologyNode, filters: TopologyFilters): boolean {
  const query = filters.query.trim().toLocaleLowerCase();
  const queryMatches =
    query.length === 0 ||
    [node.id, node.label, node.subtitle].some((value) =>
      value.toLocaleLowerCase().includes(query),
    );
  return (
    queryMatches &&
    (filters.profile === ALL_TOPOLOGY_FILTER ||
      node.profileIds.includes(filters.profile)) &&
    (filters.worktree === ALL_TOPOLOGY_FILTER ||
      node.worktreeIds.includes(filters.worktree)) &&
    (filters.repository === ALL_TOPOLOGY_FILTER ||
      node.repositoryIds.includes(filters.repository)) &&
    (filters.language === ALL_TOPOLOGY_FILTER ||
      node.languages.includes(filters.language))
  );
}

function edgeKind(relationship: TopologyRelationship): string {
  return relationship.kind ?? relationship.type;
}

export function createTopologyModel(response: TopologyResponse): TopologyModel {
  const nodes = createNodes(response);
  const relationships = [
    ...response.relationships,
    ...sharedInputRelationships(response),
  ];
  const { edges, unrendered } = buildEdges(relationships, nodes);
  return {
    nodes,
    edges,
    relationships,
    unrenderedRelationships: unrendered,
    boundaries: createBoundaries(response),
    layout: layoutNodes(nodes),
  };
}

export function filterTopology(
  model: TopologyModel,
  filters: TopologyFilters,
): TopologyModel {
  const nodes = model.nodes.filter((node) => matchesNode(node, filters));
  const visibleKeys = new Set(nodes.map((node) => node.key));
  const relationships = model.relationships.filter((relationship) => {
    const sourceVisible = visibleKeys.has(
      relationshipNodeKey(relationship.source),
    );
    const targetVisible =
      relationship.target === undefined ||
      visibleKeys.has(relationshipNodeKey(relationship.target));
    return (
      sourceVisible &&
      targetVisible &&
      (filters.edgeKind === ALL_TOPOLOGY_FILTER ||
        edgeKind(relationship) === filters.edgeKind)
    );
  });
  const { edges, unrendered } = buildEdges(relationships, nodes);
  return {
    ...model,
    nodes,
    edges,
    relationships,
    unrenderedRelationships: unrendered,
    layout: layoutNodes(nodes),
  };
}

export function topologyEdgeKind(relationship: TopologyRelationship): string {
  return edgeKind(relationship);
}
