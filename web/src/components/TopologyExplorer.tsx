import { useEffect, useMemo, useState } from "react";

import {
  ApiError,
  fetchTopology,
  type TopologyNodeReference,
  type TopologyProfile,
  type TopologyResponse,
  type TopologySource,
} from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { TopologyFlow } from "@/components/TopologyFlow";
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

export function topologyFilterLabelID(label: string): string {
  return `topology-filter-${label.trim().replace(/\s+/g, "-")}`;
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
  const labelID = topologyFilterLabelID(label);
  return (
    <div className="grid gap-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500">
      <span id={labelID}>{label}</span>
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger
          className="h-9 w-full border-white/10 bg-white/[0.04] text-xs font-normal normal-case tracking-normal text-slate-200"
          aria-labelledby={labelID}
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
      <aside className="flex min-h-0 flex-col justify-center rounded-2xl border border-white/10 bg-slate-950/55 p-5 text-sm text-slate-400 shadow-xl">
        <span className="mb-3 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
          inspector
        </span>
        <span>Select a node to inspect its source evidence.</span>
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
      className="flex min-h-0 flex-col gap-4 overflow-y-auto rounded-2xl border border-white/10 bg-slate-950/55 p-4 shadow-xl"
      aria-label="Topology details"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
            {NODE_TYPE_LABELS[node.type]}
          </p>
          <h2 className="mt-1 truncate text-base font-semibold text-slate-100">
            {node.label}
          </h2>
          <p className="truncate text-[10px] text-slate-400">{node.subtitle}</p>
        </div>
        <Badge className={statusClass(node.status)} variant="outline">
          {node.status}
        </Badge>
      </div>

      <dl className="rounded-xl border border-white/10 bg-white/[0.03] px-3">
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
          <h3 className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
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
        <h3 className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
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
                className="rounded-xl border border-white/10 bg-white/[0.03] p-2.5 text-[10px]"
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

      <div className="flex flex-wrap gap-2 border-t border-white/10 pt-3 text-[10px]">
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

function TopologyLegend(): React.ReactElement {
  return (
    <div className="grid gap-2 text-[10px] text-slate-400">
      <p className="font-semibold uppercase tracking-[0.14em] text-slate-500">
        legend
      </p>
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
    <div className="overflow-x-auto rounded-xl border border-white/10">
      <table className="w-full min-w-[44rem] text-left text-[11px]">
        <caption className="sr-only">
          Visible topology relationships and evidence
        </caption>
        <thead className="bg-white/[0.04] text-[10px] uppercase tracking-[0.14em] text-slate-500">
          <tr>
            <th className="px-3 py-2 font-medium">profile</th>
            <th className="px-3 py-2 font-medium">relationship</th>
            <th className="px-3 py-2 font-medium">source → target</th>
            <th className="px-3 py-2 font-medium">evidence</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-white/10">
          {model.relationships.map((relationship) => (
            <tr
              key={`${relationship.profile ?? ""}:${relationship.type}:${referenceKey(relationship.source)}:${referenceKey(relationship.target ?? { type: "", id: "" })}:${relationship.kind ?? ""}:${relationship.evidence ?? relationship.reason ?? relationship.provenance}`}
            >
              <td className="px-3 py-2 text-slate-400">
                {relationship.profile ?? "all"}
              </td>
              <td className="px-3 py-2">
                <span className="font-medium">
                  {topologyEdgeKind(relationship)}
                </span>
                <span className="ml-2 text-slate-500">
                  {relationship.status}
                </span>
              </td>
              <td className="px-3 py-2 text-slate-400">
                {referenceKey(relationship.source)}
                {relationship.target
                  ? ` → ${referenceKey(relationship.target)}`
                  : " → not resolved"}
              </td>
              <td className="max-w-[20rem] break-words px-3 py-2 text-slate-400">
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

  return (
    <div
      className="h-full min-h-0 overflow-hidden bg-[#08090b] text-foreground"
      data-testid="topology-explorer"
    >
      <div className="mx-auto flex h-full min-h-0 w-full max-w-[1920px] flex-col gap-4 overflow-hidden px-4 py-4 md:px-6 md:py-5 lg:px-8">
        <header className="flex shrink-0 flex-wrap items-end justify-between gap-4 pr-28 sm:pr-36">
          <div>
            <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.2em] text-violet-300">
              <span className="h-2 w-2 rounded-full bg-violet-300 shadow-[0_0_14px_rgba(196,181,253,0.9)]" />
              <span>Kivgraph / topology</span>
              <span className="rounded-full border border-white/10 px-2 py-0.5 text-[9px] tracking-[0.14em] text-slate-500">
                read only
              </span>
            </div>
            <h1 className="mt-2 text-2xl font-semibold tracking-tight text-slate-100 md:text-3xl">
              Profile topology
            </h1>
            <p className="mt-1 text-xs text-slate-400">
              Explore how profiles, worktrees and repositories compose a
              resolution universe.
            </p>
          </div>
          <div className="flex items-center gap-2">
            {state.data ? (
              <Badge
                className={statusClass(state.data.status)}
                variant="outline"
              >
                {state.data.status}
              </Badge>
            ) : null}
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setReloadToken((value) => value + 1)}
            >
              refresh
            </Button>
          </div>
        </header>

        {state.loading ? (
          <div className="shrink-0 rounded-xl border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-slate-400">
            loading topology…
          </div>
        ) : null}
        {state.error ? (
          <div
            className="flex shrink-0 flex-wrap items-center justify-between gap-2 rounded-xl border border-rose-400/30 bg-rose-400/10 px-3 py-2 text-xs text-rose-100"
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
            <div className="grid shrink-0 gap-2 sm:grid-cols-2 xl:grid-cols-4">
              <div className="flex items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/[0.035] px-4 py-3">
                <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500">
                  visible nodes
                </span>
                <span className="text-sm font-semibold tabular-nums text-slate-100">
                  {filteredModel.nodes.length}/{model.nodes.length}
                </span>
              </div>
              <div className="flex items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/[0.035] px-4 py-3">
                <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500">
                  relationships
                </span>
                <span className="text-sm font-semibold tabular-nums text-slate-100">
                  {filteredModel.relationships.length.toLocaleString()}/
                  {model.relationships.length.toLocaleString()}
                </span>
              </div>
              <div className="flex items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/[0.035] px-4 py-3">
                <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500">
                  profiles
                </span>
                <span className="text-sm font-semibold tabular-nums text-slate-100">
                  {state.data.profiles.length}
                </span>
              </div>
              <div className="min-w-0 rounded-xl border border-white/10 bg-white/[0.035] px-4 py-3">
                <span className="block text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500">
                  generation
                </span>
                <span
                  className="mt-1 block truncate text-xs font-medium text-slate-200"
                  title={state.data.profiles
                    .map((profile) => `${profile.id} ${profile.generationId}`)
                    .join(" · ")}
                >
                  {state.data.profiles
                    .map((profile) => `${profile.id} ${profile.generationId}`)
                    .join(" · ")}
                </span>
              </div>
            </div>

            {!state.data.completeness.complete ||
            state.data.completeness.truncated ? (
              <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-amber-300/20 bg-amber-300/[0.06] px-4 py-3 text-[11px] text-amber-100">
                <span className="font-semibold uppercase tracking-[0.14em] text-amber-200">
                  data quality
                </span>
                {!state.data.completeness.complete ? (
                  <span>
                    incomplete ·{" "}
                    {state.data.completeness.reason ?? "reason not supplied"}
                  </span>
                ) : null}
                {state.data.completeness.truncated ? (
                  <span>relationship list truncated</span>
                ) : null}
              </div>
            ) : null}

            {filteredModel.boundaries.length > 0 ? (
              <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-sky-300/20 bg-sky-300/[0.06] px-4 py-3 text-[11px] text-sky-100">
                <span className="font-semibold uppercase tracking-[0.14em] text-sky-200">
                  profile isolation
                </span>
                <span>
                  {filteredModel.boundaries
                    .map(
                      (boundary) =>
                        `${boundary.leftProfile} ↔ ${boundary.rightProfile}`,
                    )
                    .join(" · ")}
                </span>
                <span className="text-sky-200/70">
                  cross-profile code relationships are not evaluated
                </span>
              </div>
            ) : null}

            <div className="grid min-h-0 min-w-0 flex-1 gap-4 overflow-y-auto lg:overflow-hidden lg:grid-cols-[17rem_minmax(0,1fr)_20rem]">
              <aside className="min-h-0 overflow-y-auto rounded-2xl border border-white/10 bg-slate-950/55 p-4 shadow-xl">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                      explore
                    </p>
                    <h2 className="mt-1 text-sm font-semibold text-slate-100">
                      Filters
                    </h2>
                  </div>
                  <Button
                    type="button"
                    size="xs"
                    variant="ghost"
                    onClick={() => setFilters(INITIAL_FILTERS)}
                  >
                    reset
                  </Button>
                </div>

                <div className="mt-4 grid gap-3">
                  <div className="grid gap-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-500">
                    <span>search topology</span>
                    <Input
                      className="h-9 border-white/10 bg-white/[0.04] text-xs text-slate-200 placeholder:text-slate-600"
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
                </div>

                <div className="mt-5 border-t border-white/10 pt-4">
                  <TopologyLegend />
                </div>
              </aside>

              <section
                className="flex min-h-[30rem] min-w-0 flex-col overflow-hidden rounded-2xl border border-white/10 bg-[#0b1220] shadow-2xl lg:min-h-0"
                aria-label="Topology map"
              >
                <div className="flex shrink-0 items-center justify-between gap-3 border-b border-white/10 bg-slate-950/40 px-4 py-3">
                  <div>
                    <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                      canvas
                    </p>
                    <p className="mt-1 text-xs text-slate-300">
                      Click a node to inspect its evidence
                    </p>
                  </div>
                  <div className="text-right text-[10px] tabular-nums text-slate-500">
                    <span className="block text-slate-200">
                      {filteredModel.nodes.length} nodes
                    </span>
                    <span>{filteredModel.edges.length} linked edges</span>
                  </div>
                </div>
                <div className="min-h-0 flex-1 p-1">
                  <TopologyFlow
                    model={filteredModel}
                    selectedKey={selectedKey}
                    onSelect={setSelectedKey}
                  />
                </div>
              </section>

              <DetailsPanel
                node={selectedNode}
                data={state.data}
                model={model}
              />
            </div>

            <details className="shrink-0 overflow-hidden rounded-2xl border border-white/10 bg-slate-950/55 shadow-xl">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 [&::-webkit-details-marker]:hidden">
                <div>
                  <span className="block text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                    accessibility
                  </span>
                  <span className="mt-1 block text-sm font-semibold text-slate-100">
                    Relationship list
                  </span>
                </div>
                <div className="text-right text-[10px] text-slate-400">
                  <span className="block tabular-nums text-slate-200">
                    {filteredModel.relationships.length.toLocaleString()} rows
                  </span>
                  <span>keyboard-friendly detail view</span>
                </div>
              </summary>
              <div className="max-h-72 overflow-auto border-t border-white/10 p-3">
                {filteredModel.unrenderedRelationships.length > 0 ? (
                  <p className="mb-3 text-[10px] text-amber-200">
                    {filteredModel.unrenderedRelationships.length}{" "}
                    relationship(s) omitted because an endpoint is not present.
                  </p>
                ) : null}
                {filteredModel.relationships.length > 0 ? (
                  <RelationshipTable model={filteredModel} />
                ) : (
                  <p className="rounded-xl border border-white/10 px-3 py-4 text-xs text-slate-400">
                    No relationships match the current filters.
                  </p>
                )}
              </div>
            </details>
          </>
        ) : null}
      </div>
    </div>
  );
}
