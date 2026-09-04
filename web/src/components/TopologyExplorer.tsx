import { useEffect, useMemo, useState } from "react";

import {
  ApiError,
  fetchTopology,
  type TopologyNodeReference,
  type TopologyProfile,
  type TopologyRelationship,
  type TopologyResponse,
  type TopologySource,
} from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  ALL_TOPOLOGY_FILTER,
  createTopologyModel,
  filterTopology,
  topologyEdgeKind,
  type TopologyFilters,
  type TopologyModel,
  type TopologyNode,
} from "@/topology";
import { cn } from "@/lib/utils";

const INITIAL_FILTERS: TopologyFilters = {
  query: "",
  profile: ALL_TOPOLOGY_FILTER,
  worktree: ALL_TOPOLOGY_FILTER,
  repository: ALL_TOPOLOGY_FILTER,
  language: ALL_TOPOLOGY_FILTER,
  edgeKind: ALL_TOPOLOGY_FILTER,
};

const NODE_TYPE_LABELS: Record<TopologyNode["type"], string> = {
  profile: "profile",
  worktree: "worktree",
  repository: "repository",
  shared_input: "shared input",
};

const NODE_COLORS: Record<TopologyNode["type"], string> = {
  profile: "#a78bfa",
  worktree: "#38bdf8",
  repository: "#34d399",
  shared_input: "#fbbf24",
};

const EDGE_COLORS: Record<string, string> = {
  structural: "#94a3b8",
  exact: "#34d399",
  candidate: "#fbbf24",
  unresolved: "#fb923c",
  conflict: "#fb7185",
  shared_input_usage: "#fbbf24",
};

interface TopologyState {
  readonly data: TopologyResponse | null;
  readonly error: string | null;
  readonly loading: boolean;
}

const INITIAL_STATE: TopologyState = {
  data: null,
  error: null,
  loading: true,
};

function describe(error: unknown): string {
  if (error instanceof ApiError) return `${error.code}: ${error.message}`;
  if (error instanceof Error) return error.message;
  return "unknown error";
}

function statusClass(status: string): string {
  switch (status) {
    case "ready":
    case "current":
    case "structural":
      return "border-emerald-400/40 bg-emerald-400/10 text-emerald-200";
    case "stale":
    case "partial":
    case "candidate":
    case "shared":
      return "border-amber-400/40 bg-amber-400/10 text-amber-200";
    case "missing":
    case "unavailable":
    case "unresolved":
    case "conflict":
      return "border-rose-400/40 bg-rose-400/10 text-rose-200";
    default:
      return "border-border/80 bg-muted/20 text-muted-foreground";
  }
}

function referenceKey(reference: TopologyNodeReference): string {
  return `${reference.type}:${reference.id}`;
}

function readable(value: string | undefined): string {
  return value && value.length > 0 ? value : "not observed";
}

export function pinnedTopologyURL(
  profileIds: readonly string[],
  profiles: readonly Pick<TopologyProfile, "id" | "generationId">[],
): string {
  const applicableProfiles = profiles
    .filter((profile) => profileIds.includes(profile.id))
    .sort((left, right) =>
      left.id < right.id ? -1 : left.id > right.id ? 1 : 0,
    );
  if (applicableProfiles.length === 0) return "/api/v1/topology";

  const query = new URLSearchParams();
  for (const profile of applicableProfiles) query.append("profile", profile.id);
  for (const profile of applicableProfiles) {
    query.append("generation", `${profile.id}:${profile.generationId}`);
  }
  return `/api/v1/topology?${query.toString()}`;
}

function relationshipColor(relationship: TopologyRelationship): string {
  return (
    EDGE_COLORS[relationship.type] ??
    EDGE_COLORS[relationship.status] ??
    "#94a3b8"
  );
}

function FilterSelect({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: {
  readonly label: string;
  readonly value: string;
  readonly options: readonly string[];
  readonly onChange: (value: string) => void;
  readonly disabled?: boolean;
}): React.ReactElement {
  return (
    <div className="grid gap-1 text-[10px] uppercase tracking-wide text-muted-foreground">
      <span id={`topology-filter-${label}`}>{label}</span>
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger
          className="h-8 w-full text-xs normal-case tracking-normal"
          aria-labelledby={`topology-filter-${label}`}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL_TOPOLOGY_FILTER}>all</SelectItem>
          {options.map((option) => (
            <SelectItem key={option} value={option}>
              {option}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function DetailsRow({
  label,
  children,
}: {
  readonly label: string;
  readonly children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="grid gap-0.5 border-b border-border/50 py-2 last:border-b-0">
      <dt className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd className="break-words text-xs text-foreground">{children}</dd>
    </div>
  );
}

function Observation({
  source,
}: {
  readonly source: TopologySource;
}): React.ReactElement {
  const observation = source.current ?? source.indexed;
  const state = source.current
    ? source.current.dirty
      ? "dirty"
      : "clean"
    : source.status === "stale"
      ? "current state stale"
      : observation
        ? `indexed ${observation.dirty ? "dirty" : "clean"}; current not observed`
        : "current state not observed";
  return (
    <div className="grid gap-1 rounded-lg border border-border/60 bg-muted/10 p-2">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{source.profile}</span>
        <Badge className={statusClass(source.status)} variant="outline">
          {source.status}
        </Badge>
      </div>
      <span className="text-[10px] text-muted-foreground">
        {observation?.branch ?? "branch not observed"} ·{" "}
        {observation?.commit ?? "commit not observed"}
      </span>
      <span className="text-[10px] text-muted-foreground">
        {state}
        {source.reason ? ` · ${source.reason}` : ""}
      </span>
      <span
        className="truncate text-[10px] text-muted-foreground"
        title={observation?.contentDigest}
      >
        digest · {observation?.contentDigest ?? "not observed"}
      </span>
    </div>
  );
}

function DetailsPanel({
  node,
  data,
  model,
}: {
  readonly node: TopologyNode | undefined;
  readonly data: TopologyResponse;
  readonly model: TopologyModel;
}): React.ReactElement {
  if (!node) {
    return (
      <aside className="rounded-2xl border border-border/80 bg-background/80 p-4 text-sm text-muted-foreground">
        Select a node to inspect its source evidence.
      </aside>
    );
  }

  const profile =
    node.type === "profile"
      ? data.profiles.find((item) => item.id === node.id)
      : undefined;
  const worktree =
    node.type === "worktree"
      ? data.worktrees.find((item) => item.id === node.id)
      : undefined;
  const repository =
    node.type === "repository"
      ? data.repositories.find((item) => item.id === node.id)
      : undefined;
  const sources = data.sources.filter((source) =>
    node.type === "worktree"
      ? source.worktree === node.id
      : source.profile === node.id || source.repository === node.id,
  );
  const relationships = model.edges.filter(
    (edge) => edge.sourceKey === node.key || edge.targetKey === node.key,
  );
  const topologyURL = pinnedTopologyURL(node.profileIds, data.profiles);
  const graphURL = `/api/v1/search?name=${encodeURIComponent(node.id)}&mode=prefix`;

  return (
    <aside
      className="flex min-h-0 flex-col gap-3 overflow-y-auto rounded-2xl border border-border/80 bg-background/80 p-4"
      aria-label="Topology details"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[10px] uppercase tracking-wide text-muted-foreground">
            {NODE_TYPE_LABELS[node.type]}
          </p>
          <h2 className="truncate text-sm font-semibold">{node.label}</h2>
          <p className="truncate text-[10px] text-muted-foreground">
            {node.subtitle}
          </p>
        </div>
        <Badge className={statusClass(node.status)} variant="outline">
          {node.status}
        </Badge>
      </div>

      <dl className="rounded-lg border border-border/60 bg-muted/10 px-2">
        {profile ? (
          <>
            <DetailsRow label="generation">{profile.generationId}</DetailsRow>
            <DetailsRow label="worktrees">
              {profile.worktrees.length > 0
                ? profile.worktrees.join(", ")
                : "none"}
            </DetailsRow>
            {profile.reason ? (
              <DetailsRow label="state">{profile.reason}</DetailsRow>
            ) : null}
          </>
        ) : null}
        {repository ? (
          <>
            <DetailsRow label="repository id">{repository.id}</DetailsRow>
            <DetailsRow label="languages">
              {repository.languages.length > 0
                ? repository.languages.join(", ")
                : "not observed"}
            </DetailsRow>
          </>
        ) : null}
        {worktree ? (
          <>
            <DetailsRow label="path">{worktree.path}</DetailsRow>
            <DetailsRow label="repository">{worktree.repository}</DetailsRow>
            <DetailsRow label="git directory">
              {readable(worktree.git?.gitDirectory)}
            </DetailsRow>
            <DetailsRow label="common directory">
              {readable(worktree.git?.commonDirectory)}
            </DetailsRow>
          </>
        ) : null}
        {node.type === "shared_input" ? (
          <DetailsRow label="owners">{node.profileIds.join(", ")}</DetailsRow>
        ) : null}
        <DetailsRow label="generation scope">
          {node.profileIds.length > 0
            ? node.profileIds.join(", ")
            : "not scoped"}
        </DetailsRow>
      </dl>

      {sources.length > 0 ? (
        <div className="grid gap-2">
          <h3 className="text-[10px] uppercase tracking-wide text-muted-foreground">
            source observations
          </h3>
          {sources.map((source) => (
            <Observation
              key={`${source.profile}:${source.worktree}`}
              source={source}
            />
          ))}
        </div>
      ) : null}

      <div className="grid gap-2">
        <h3 className="text-[10px] uppercase tracking-wide text-muted-foreground">
          evidence · {relationships.length}
        </h3>
        {relationships.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No emitted relationships touch this node.
          </p>
        ) : (
          <div className="grid gap-1.5">
            {relationships.slice(0, 12).map((edge) => (
              <div
                key={edge.key}
                className="rounded-lg border border-border/50 bg-muted/10 p-2 text-[10px]"
              >
                <div className="flex items-center justify-between gap-2">
                  <span>{topologyEdgeKind(edge.relationship)}</span>
                  <Badge
                    className={statusClass(edge.relationship.status)}
                    variant="outline"
                  >
                    {edge.relationship.status}
                  </Badge>
                </div>
                <p className="mt-1 break-words text-muted-foreground">
                  {edge.relationship.confidence} ·{" "}
                  {edge.relationship.provenance}
                  {edge.relationship.evidence
                    ? ` · ${edge.relationship.evidence}`
                    : ""}
                </p>
                {edge.relationship.reason ? (
                  <p className="mt-1 break-words text-muted-foreground">
                    {edge.relationship.reason}
                  </p>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex flex-wrap gap-2 border-t border-border/60 pt-3 text-[10px]">
        <a
          className="text-sky-300 underline-offset-2 hover:underline"
          href={topologyURL}
        >
          open pinned topology API
        </a>
        <a
          className="text-sky-300 underline-offset-2 hover:underline"
          href={graphURL}
        >
          search graph symbols
        </a>
      </div>
    </aside>
  );
}

function TopologyMinimap({
  model,
  selectedKey,
}: {
  readonly model: TopologyModel;
  readonly selectedKey: string | null;
}): React.ReactElement {
  const nodesByKey = new Map(model.nodes.map((node) => [node.key, node]));
  return (
    <svg
      className="h-auto w-full rounded-lg border border-border/60 bg-muted/10"
      viewBox={`0 0 ${model.layout.width} ${model.layout.height}`}
      role="img"
      aria-label="Topology minimap"
    >
      {model.layout.nodes.map((layoutNode) => {
        const node = nodesByKey.get(layoutNode.key);
        if (!node) return null;
        return (
          <rect
            key={layoutNode.key}
            x={layoutNode.x}
            y={layoutNode.y}
            width={layoutNode.width}
            height={layoutNode.height}
            rx={10}
            fill={NODE_COLORS[node.type]}
            fillOpacity={
              selectedKey === null || selectedKey === node.key ? 0.75 : 0.18
            }
          />
        );
      })}
    </svg>
  );
}

function TopologyLegend(): React.ReactElement {
  return (
    <div className="grid gap-2 text-[10px] text-muted-foreground">
      <p className="uppercase tracking-wide">legend</p>
      <div className="grid grid-cols-2 gap-1.5">
        {(
          Object.entries(NODE_TYPE_LABELS) as [TopologyNode["type"], string][]
        ).map(([type, label]) => (
          <span key={type} className="flex items-center gap-1.5">
            <span
              className="h-2.5 w-2.5 rounded-sm"
              style={{ backgroundColor: NODE_COLORS[type] }}
            />
            {label}
          </span>
        ))}
      </div>
      <div className="grid gap-1">
        {Object.entries(EDGE_COLORS).map(([status, color]) => (
          <span key={status} className="flex items-center gap-1.5">
            <span
              className="h-0.5 w-4 rounded-full"
              style={{ backgroundColor: color }}
            />
            {status.replaceAll("_", " ")} relationship
          </span>
        ))}
      </div>
    </div>
  );
}

function RelationshipTable({
  model,
}: {
  readonly model: TopologyModel;
}): React.ReactElement {
  return (
    <div className="overflow-x-auto rounded-xl border border-border/70">
      <table className="w-full min-w-[44rem] text-left text-[11px]">
        <caption className="sr-only">
          Visible topology relationships and evidence
        </caption>
        <thead className="bg-muted/20 text-[10px] uppercase tracking-wide text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">profile</th>
            <th className="px-3 py-2 font-medium">relationship</th>
            <th className="px-3 py-2 font-medium">source → target</th>
            <th className="px-3 py-2 font-medium">evidence</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/50">
          {model.relationships.map((relationship) => (
            <tr
              key={`${relationship.profile ?? ""}:${relationship.type}:${referenceKey(relationship.source)}:${referenceKey(relationship.target ?? { type: "", id: "" })}:${relationship.kind ?? ""}:${relationship.evidence ?? relationship.reason ?? relationship.provenance}`}
            >
              <td className="px-3 py-2 text-muted-foreground">
                {relationship.profile ?? "all"}
              </td>
              <td className="px-3 py-2">
                <span className="font-medium">
                  {topologyEdgeKind(relationship)}
                </span>
                <span className="ml-2 text-muted-foreground">
                  {relationship.status}
                </span>
              </td>
              <td className="px-3 py-2 text-muted-foreground">
                {referenceKey(relationship.source)}
                {relationship.target
                  ? ` → ${referenceKey(relationship.target)}`
                  : " → not resolved"}
              </td>
              <td className="max-w-[20rem] break-words px-3 py-2 text-muted-foreground">
                {relationship.evidence ??
                  relationship.reason ??
                  relationship.provenance}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function TopologyExplorer(): React.ReactElement {
  const [state, setState] = useState<TopologyState>(INITIAL_STATE);
  const [reloadToken, setReloadToken] = useState(0);
  const [filters, setFilters] = useState<TopologyFilters>(INITIAL_FILTERS);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [zoom, setZoom] = useState(1);

  useEffect(() => {
    // The token is the explicit input for this request effect: changing it
    // cancels the old generation read and starts a fresh one.
    void reloadToken;
    const controller = new AbortController();
    setState((previous) => ({ ...previous, loading: true, error: null }));
    void fetchTopology({}, controller.signal)
      .then((data) => {
        if (!controller.signal.aborted)
          setState({ data, error: null, loading: false });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({ data: null, error: describe(error), loading: false });
      });
    return () => controller.abort();
  }, [reloadToken]);

  const model = useMemo(
    () => (state.data ? createTopologyModel(state.data) : null),
    [state.data],
  );
  const filteredModel = useMemo(
    () => (model ? filterTopology(model, filters) : null),
    [filters, model],
  );
  const selectedNode = model?.nodes.find((node) => node.key === selectedKey);
  const neighbours = useMemo(() => {
    if (!selectedKey || !filteredModel) return new Set<string>();
    const keys = new Set([selectedKey]);
    for (const edge of filteredModel.edges) {
      if (edge.sourceKey === selectedKey && edge.targetKey)
        keys.add(edge.targetKey);
      if (edge.targetKey === selectedKey) keys.add(edge.sourceKey);
    }
    return keys;
  }, [filteredModel, selectedKey]);
  const updateFilter = (key: keyof TopologyFilters, value: string): void => {
    setFilters((previous) => ({ ...previous, [key]: value }));
  };

  const profiles = state.data?.profiles.map((profile) => profile.id) ?? [];
  const worktrees = state.data?.worktrees.map((worktree) => worktree.id) ?? [];
  const repositories =
    state.data?.repositories.map((repository) => repository.id) ?? [];
  const languages = model
    ? [...new Set(model.nodes.flatMap((node) => node.languages))].sort()
    : [];
  const edgeKinds = model
    ? [
        ...new Set(
          model.relationships.map((relationship) =>
            topologyEdgeKind(relationship),
          ),
        ),
      ].sort()
    : [];
  const layoutByKey = new Map(
    (filteredModel?.layout.nodes ?? []).map((layoutNode) => [
      layoutNode.key,
      layoutNode,
    ]),
  );
  const filteredNodesByKey = new Map(
    (filteredModel?.nodes ?? []).map((node) => [node.key, node]),
  );
  const mapWidth = (filteredModel?.layout.width ?? 0) * zoom;
  const mapHeight = (filteredModel?.layout.height ?? 0) * zoom;

  return (
    <div
      className="flex h-full min-h-0 flex-col gap-3 overflow-hidden bg-background p-4 text-foreground md:p-5"
      data-testid="topology-explorer"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-base font-semibold">Topology explorer</h1>
          <p className="text-xs text-muted-foreground">
            read-only profile and worktree composition
          </p>
        </div>
        <div className="flex items-center gap-2">
          {state.data ? (
            <Badge className={statusClass(state.data.status)} variant="outline">
              {state.data.status}
            </Badge>
          ) : null}
          <Button
            type="button"
            size="xs"
            variant="outline"
            onClick={() => setReloadToken((value) => value + 1)}
          >
            refresh topology
          </Button>
        </div>
      </div>

      {state.loading ? (
        <div className="rounded-xl border border-border/70 bg-muted/10 px-3 py-2 text-xs text-muted-foreground">
          loading topology…
        </div>
      ) : null}
      {state.error ? (
        <div
          className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive-foreground"
          role="alert"
        >
          <span>{state.error}</span>
          <Button
            type="button"
            size="xs"
            variant="outline"
            onClick={() => setReloadToken((value) => value + 1)}
          >
            retry
          </Button>
        </div>
      ) : null}

      {state.data && model && filteredModel ? (
        <>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-xl border border-border/70 bg-muted/10 px-3 py-2 text-[11px] text-muted-foreground">
            <span>
              {filteredModel.nodes.length}/{model.nodes.length} nodes
            </span>
            <span>
              {filteredModel.relationships.length}/{model.relationships.length}{" "}
              relationships
            </span>
            <span>
              generations:{" "}
              {state.data.profiles
                .map((profile) => `${profile.id} ${profile.generationId}`)
                .join(" · ")}
            </span>
            {!state.data.completeness.complete ? (
              <span className="text-amber-200">
                incomplete ·{" "}
                {state.data.completeness.reason ?? "reason not supplied"}
              </span>
            ) : null}
            {state.data.completeness.truncated ? (
              <span className="text-amber-200">
                relationship list truncated
              </span>
            ) : null}
          </div>

          {filteredModel.boundaries.length > 0 ? (
            <div className="rounded-xl border border-sky-400/30 bg-sky-400/5 px-3 py-2 text-[11px] text-sky-100">
              <span className="font-medium">profile isolation:</span>{" "}
              {filteredModel.boundaries
                .map(
                  (boundary) =>
                    `${boundary.leftProfile} ↔ ${boundary.rightProfile}`,
                )
                .join(" · ")}
              {" · code relationships are not evaluated across profiles"}
            </div>
          ) : null}

          <div className="grid min-h-0 flex-1 gap-3 lg:grid-cols-[15rem_minmax(0,1fr)_18rem]">
            <aside className="min-h-0 space-y-4 overflow-y-auto rounded-2xl border border-border/80 bg-background/80 p-3">
              <div className="grid gap-2">
                <div className="grid gap-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                  <span>search topology</span>
                  <Input
                    value={filters.query}
                    onChange={(event) =>
                      updateFilter("query", event.currentTarget.value)
                    }
                    placeholder="profile, path, repository…"
                    aria-label="Search topology"
                  />
                </div>
                <FilterSelect
                  label="profile"
                  value={filters.profile}
                  options={profiles}
                  onChange={(value) => updateFilter("profile", value)}
                />
                <FilterSelect
                  label="worktree"
                  value={filters.worktree}
                  options={worktrees}
                  onChange={(value) => updateFilter("worktree", value)}
                />
                <FilterSelect
                  label="repository"
                  value={filters.repository}
                  options={repositories}
                  onChange={(value) => updateFilter("repository", value)}
                />
                <FilterSelect
                  label="language"
                  value={filters.language}
                  options={languages}
                  disabled={languages.length === 0}
                  onChange={(value) => updateFilter("language", value)}
                />
                <FilterSelect
                  label="edge kind"
                  value={filters.edgeKind}
                  options={edgeKinds}
                  onChange={(value) => updateFilter("edgeKind", value)}
                />
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => setFilters(INITIAL_FILTERS)}
                >
                  clear filters
                </Button>
              </div>

              <TopologyLegend />
              <div className="grid gap-2">
                <p className="text-[10px] uppercase tracking-wide text-muted-foreground">
                  minimap
                </p>
                <TopologyMinimap
                  model={filteredModel}
                  selectedKey={selectedKey}
                />
                <div className="flex items-center justify-between gap-1">
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    aria-label="Zoom out"
                    onClick={() =>
                      setZoom((value) => Math.max(0.6, value - 0.2))
                    }
                  >
                    −
                  </Button>
                  <span className="text-[10px] tabular-nums text-muted-foreground">
                    {Math.round(zoom * 100)}%
                  </span>
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    aria-label="Zoom in"
                    onClick={() =>
                      setZoom((value) => Math.min(1.8, value + 0.2))
                    }
                  >
                    +
                  </Button>
                  <Button
                    type="button"
                    size="xs"
                    variant="ghost"
                    onClick={() => setZoom(1)}
                  >
                    reset
                  </Button>
                </div>
              </div>
            </aside>

            <section
              className="min-h-0 overflow-auto rounded-2xl border border-border/80 bg-[#0b1020]"
              aria-label="Topology map. Use the scrollbars to pan."
            >
              <div
                className="relative"
                style={{ width: mapWidth, height: mapHeight }}
              >
                <svg
                  className="pointer-events-none absolute left-0 top-0"
                  width={filteredModel.layout.width}
                  height={filteredModel.layout.height}
                  viewBox={`0 0 ${filteredModel.layout.width} ${filteredModel.layout.height}`}
                  preserveAspectRatio="none"
                  style={{
                    transform: `scale(${zoom})`,
                    transformOrigin: "top left",
                  }}
                  aria-hidden="true"
                >
                  <defs>
                    <marker
                      id="topology-arrow"
                      markerWidth="8"
                      markerHeight="8"
                      refX="7"
                      refY="4"
                      orient="auto"
                    >
                      <path d="M0,0 L8,4 L0,8 Z" fill="#94a3b8" />
                    </marker>
                  </defs>
                  {filteredModel.edges.map((edge) => {
                    if (!edge.targetKey) return null;
                    const source = layoutByKey.get(edge.sourceKey);
                    const target = layoutByKey.get(edge.targetKey);
                    if (!source || !target) return null;
                    const color = relationshipColor(edge.relationship);
                    const active =
                      selectedKey === null ||
                      neighbours.has(edge.sourceKey) ||
                      neighbours.has(edge.targetKey);
                    return (
                      <line
                        key={edge.key}
                        x1={source.x + source.width}
                        y1={source.y + source.height / 2}
                        x2={target.x}
                        y2={target.y + target.height / 2}
                        stroke={color}
                        strokeWidth={
                          edge.relationship.status === "structural" ? 1.5 : 2.5
                        }
                        strokeOpacity={active ? 0.8 : 0.12}
                        markerEnd="url(#topology-arrow)"
                      />
                    );
                  })}
                </svg>
                {filteredModel.layout.nodes.map((layoutNode) => {
                  const node = filteredNodesByKey.get(layoutNode.key);
                  if (!node) return null;
                  const active =
                    selectedKey === null || neighbours.has(node.key);
                  return (
                    <button
                      key={node.key}
                      type="button"
                      className={cn(
                        "absolute grid content-start gap-1 rounded-xl border p-3 text-left shadow-lg transition-opacity focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        active ? "opacity-100" : "opacity-25",
                        selectedKey === node.key
                          ? "border-foreground/90"
                          : "border-border/70",
                      )}
                      style={{
                        left: layoutNode.x * zoom,
                        top: layoutNode.y * zoom,
                        width: layoutNode.width * zoom,
                        minHeight: layoutNode.height * zoom,
                        backgroundColor: `${NODE_COLORS[node.type]}12`,
                      }}
                      onClick={() => setSelectedKey(node.key)}
                      aria-pressed={selectedKey === node.key}
                      aria-label={`${NODE_TYPE_LABELS[node.type]} ${node.label}`}
                      data-testid={`topology-node-${node.key}`}
                    >
                      <span className="flex items-center justify-between gap-2">
                        <span className="truncate text-xs font-semibold">
                          {node.label}
                        </span>
                        <span
                          className="h-2 w-2 shrink-0 rounded-full"
                          style={{ backgroundColor: NODE_COLORS[node.type] }}
                        />
                      </span>
                      <span className="line-clamp-2 text-[10px] text-muted-foreground">
                        {node.subtitle}
                      </span>
                      <span className="mt-1 flex flex-wrap gap-1">
                        <Badge
                          className={statusClass(node.status)}
                          variant="outline"
                        >
                          {node.status}
                        </Badge>
                        {node.languages.slice(0, 2).map((language) => (
                          <Badge key={language} variant="secondary">
                            {language}
                          </Badge>
                        ))}
                      </span>
                    </button>
                  );
                })}
              </div>
            </section>

            <DetailsPanel node={selectedNode} data={state.data} model={model} />
          </div>

          <div className="grid gap-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-xs font-semibold">
                Accessible relationship list
              </h2>
              {filteredModel.unrenderedRelationships.length > 0 ? (
                <span className="text-[10px] text-amber-200">
                  {filteredModel.unrenderedRelationships.length} relationship(s)
                  omitted because an endpoint is not present
                </span>
              ) : null}
            </div>
            {filteredModel.relationships.length > 0 ? (
              <RelationshipTable model={filteredModel} />
            ) : (
              <p className="rounded-xl border border-border/70 px-3 py-4 text-xs text-muted-foreground">
                No relationships match the current filters.
              </p>
            )}
          </div>
        </>
      ) : null}
    </div>
  );
}
