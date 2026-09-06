import { useCallback, useEffect, useMemo, useState } from "react";

import {
  ApiError,
  fetchTopology,
  type TopologyNodeReference,
  type TopologyObservation,
  type TopologyProfile,
  type TopologyRelationship,
  type TopologyResponse,
  type TopologySource,
} from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  TOPOLOGY_EDGE_COLORS,
  TOPOLOGY_NODE_STYLES,
  TopologyFlow,
} from "@/components/TopologyFlow";
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
  displayWorktreeLabel,
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

const ACCESSIBLE_RELATIONSHIPS_PER_PAGE = 100;

interface TopologyState {
  readonly data: TopologyResponse | null;
  readonly error: string | null;
  readonly loading: boolean;
}

interface TopologyRequestState {
  readonly token: number;
  readonly generationPins: Readonly<Record<string, string>>;
}

const INITIAL_STATE: TopologyState = {
  data: null,
  error: null,
  loading: true,
};

const INITIAL_TOPOLOGY_REQUEST: TopologyRequestState = {
  token: 0,
  generationPins: {},
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
    case "exact":
      return "border-graph-exact/50 bg-graph-exact/10 text-graph-exact";
    case "stale":
    case "partial":
    case "candidate":
    case "shared":
      return "border-graph-symbol/50 bg-graph-symbol/10 text-graph-symbol";
    case "missing":
    case "unavailable":
    case "unresolved":
    case "conflict":
      return "border-graph-symbol/60 bg-graph-symbol/15 text-graph-symbol";
    default:
      return "border-rule-strong bg-raise text-gray-400";
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
  if (applicableProfiles.length === 0)
    return "/api/v1/topology?relationships=grouped";

  const query = new URLSearchParams();
  for (const profile of applicableProfiles) query.append("profile", profile.id);
  for (const profile of applicableProfiles) {
    query.append("generation", `${profile.id}:${profile.generationId}`);
  }
  query.set("relationships", "grouped");
  return `/api/v1/topology?${query.toString()}`;
}

export function topologyGenerationPins(
  profiles: readonly Pick<TopologyProfile, "id" | "generationId">[],
): Readonly<Record<string, string>> {
  return Object.fromEntries(
    [...profiles]
      .sort((left, right) => left.id.localeCompare(right.id))
      .map((profile) => [profile.id, profile.generationId]),
  );
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
  formatOption,
}: {
  readonly label: string;
  readonly value: string;
  readonly options: readonly string[];
  readonly onChange: (value: string) => void;
  readonly disabled?: boolean;
  readonly formatOption?: (value: string) => string;
}): React.ReactElement {
  const labelID = topologyFilterLabelID(label);
  return (
    <div className="grid gap-1.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
      <span id={labelID}>{label}</span>
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger
          className="h-9 w-full rounded-none border-rule bg-raise text-xs font-normal normal-case tracking-normal text-gray-200"
          aria-labelledby={labelID}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL_TOPOLOGY_FILTER}>all</SelectItem>
          {options.map((option) => (
            <SelectItem key={option} value={option}>
              {formatOption ? formatOption(option) : option}
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
    <div className="grid gap-0.5 border-b border-rule py-2 last:border-b-0">
      <dt className="font-mono text-[10px] uppercase tracking-wide text-gray-400">
        {label}
      </dt>
      <dd className="break-words text-xs text-gray-200">{children}</dd>
    </div>
  );
}

function ObservedSourceState({
  label,
  observation,
}: {
  readonly label: "indexed" | "current";
  readonly observation: TopologyObservation | undefined;
}): React.ReactElement {
  const state = observation
    ? observation.dirty
      ? "dirty"
      : "clean"
    : "not observed";
  return (
    <div className="grid gap-1 border border-rule bg-shell p-2">
      <span className="font-mono text-[9px] font-semibold uppercase tracking-[0.14em] text-gray-400">
        {label} observation
      </span>
      <span className="text-[10px] text-gray-400">
        {observation?.branch ?? "branch not observed"} ·{" "}
        {observation?.commit ?? "commit not observed"}
      </span>
      <span className="text-[10px] text-gray-400">{state}</span>
      <span
        className="truncate text-[10px] text-gray-400"
        title={observation?.contentDigest}
      >
        digest · {observation?.contentDigest ?? "not observed"}
      </span>
    </div>
  );
}

function Observation({
  source,
}: {
  readonly source: TopologySource;
}): React.ReactElement {
  return (
    <div className="grid gap-2 rounded-none border border-rule bg-raise p-2">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{source.profile}</span>
        <Badge className={statusClass(source.status)} variant="outline">
          {source.status}
        </Badge>
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        <ObservedSourceState label="indexed" observation={source.indexed} />
        <ObservedSourceState label="current" observation={source.current} />
      </div>
      {source.reason ? (
        <span className="text-[10px] text-gray-400">{source.reason}</span>
      ) : null}
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
      <aside className="flex min-h-0 flex-col justify-center rounded-none border border-rule bg-panel p-5 text-sm text-gray-400">
        <span className="mb-3 font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
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
  const relationshipCount = relationships.reduce(
    (total, edge) => total + (edge.relationship.occurrences ?? 1),
    0,
  );
  const topologyURL = pinnedTopologyURL(node.profileIds, data.profiles);
  const graphURL = `/api/v1/search?name=${encodeURIComponent(node.id)}&mode=prefix`;

  return (
    <aside
      className="flex min-h-0 flex-col gap-4 overflow-y-auto rounded-none border border-rule bg-panel p-4"
      aria-label="Topology details"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
            {TOPOLOGY_NODE_STYLES[node.type].label}
          </p>
          <h2 className="mt-1 truncate font-mono text-base font-semibold text-gray-100">
            {node.label}
          </h2>
          <p className="truncate text-[10px] text-gray-400">{node.subtitle}</p>
        </div>
        <Badge className={statusClass(node.status)} variant="outline">
          {node.status}
        </Badge>
      </div>

      <dl className="rounded-none border border-rule bg-raise px-3">
        {profile ? (
          <>
            <DetailsRow label="generation">{profile.generationId}</DetailsRow>
            <DetailsRow label="worktrees">
              {profile.worktrees.length > 0
                ? profile.worktrees.map(displayWorktreeLabel).join(", ")
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
          <h3 className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
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
        <h3 className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
          evidence · {relationshipCount.toLocaleString()}
        </h3>
        {relationships.length === 0 ? (
          <p className="text-xs text-gray-400">
            No emitted relationships touch this node.
          </p>
        ) : (
          <div className="grid gap-1.5">
            {relationships.slice(0, 12).map((edge) => (
              <div
                key={edge.key}
                className="rounded-none border border-rule bg-raise p-2.5 text-[10px]"
              >
                <div className="flex items-center justify-between gap-2">
                  <span>
                    {topologyEdgeKind(edge.relationship)}
                    {(edge.relationship.occurrences ?? 1) > 1
                      ? ` ×${edge.relationship.occurrences?.toLocaleString()}`
                      : ""}
                  </span>
                  <Badge
                    className={statusClass(edge.relationship.status)}
                    variant="outline"
                  >
                    {edge.relationship.status}
                  </Badge>
                </div>
                <p className="mt-1 break-words text-gray-400">
                  {edge.relationship.confidence} ·{" "}
                  {edge.relationship.provenance}
                  {edge.relationship.evidence
                    ? ` · ${edge.relationship.evidence}`
                    : ""}
                </p>
                {edge.relationship.reason ? (
                  <p className="mt-1 break-words text-gray-400">
                    {edge.relationship.reason}
                  </p>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="flex flex-wrap gap-2 border-t border-rule pt-3 text-[10px]">
        <a
          className="text-graph-cross underline-offset-2 hover:underline"
          href={topologyURL}
        >
          open pinned topology API
        </a>
        <a
          className="text-graph-cross underline-offset-2 hover:underline"
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
    <div className="grid gap-2 text-[10px] text-gray-400">
      <p className="font-mono font-semibold uppercase tracking-[0.14em] text-gray-400">
        how to read the map
      </p>
      <div className="grid gap-1.5">
        <span className="flex items-center gap-1.5">
          <span
            className="h-2.5 w-2.5 rounded-none"
            style={{ backgroundColor: TOPOLOGY_NODE_STYLES.profile.color }}
          />
          purple card · profile
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-2.5 w-2.5 rounded-none"
            style={{ backgroundColor: TOPOLOGY_NODE_STYLES.repository.color }}
          />
          blue card · repository or repository group
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 border-t border-dashed"
            style={{ borderColor: TOPOLOGY_EDGE_COLORS.structural }}
          />
          dashed arrow · contains
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 border-t border-dashed"
            style={{ borderColor: TOPOLOGY_EDGE_COLORS.overlay }}
          />
          violet arrow · worktree overlay
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 border-t border-dashed"
            style={{ borderColor: TOPOLOGY_EDGE_COLORS.invalidation }}
          />
          blue arrow · invalidates stale generation
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 rounded-none"
            style={{ backgroundColor: TOPOLOGY_EDGE_COLORS.exact }}
          />
          green arrow · exact relationship
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 rounded-none"
            style={{ backgroundColor: TOPOLOGY_EDGE_COLORS.candidate }}
          />
          orange arrow · candidate relationship
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 rounded-none"
            style={{ backgroundColor: TOPOLOGY_EDGE_COLORS.unresolved }}
          />
          yellow arrow · unresolved relationship
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="h-0.5 w-4 rounded-none"
            style={{ backgroundColor: TOPOLOGY_EDGE_COLORS.conflict }}
          />
          red arrow · conflicting relationship
        </span>
      </div>
    </div>
  );
}

function RelationshipTable({
  relationships,
  firstRowIndex,
}: {
  readonly relationships: readonly TopologyRelationship[];
  readonly firstRowIndex: number;
}): React.ReactElement {
  const visibleRelationships = relationships.map((relationship, index) => {
    const baseKey = `${relationship.profile ?? ""}:${relationship.generationId ?? ""}:${relationship.type}:${referenceKey(relationship.source)}:${referenceKey(relationship.target ?? { type: "", id: "" })}:${relationship.kind ?? ""}:${relationship.evidence ?? relationship.reason ?? relationship.provenance}`;
    return { key: `${baseKey}:${firstRowIndex + index}`, relationship };
  });

  return (
    <div className="overflow-x-auto rounded-none border border-rule">
      <table className="w-full min-w-[44rem] text-left text-[11px]">
        <caption className="sr-only">
          Visible topology relationships and evidence
        </caption>
        <thead className="bg-raise font-mono text-[10px] uppercase tracking-[0.14em] text-gray-400">
          <tr>
            <th className="px-3 py-2 font-medium">profile</th>
            <th className="px-3 py-2 font-medium">relationship</th>
            <th className="px-3 py-2 font-medium">source → target</th>
            <th className="px-3 py-2 font-medium">evidence</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-rule">
          {visibleRelationships.map(({ key, relationship }) => (
            <tr key={key}>
              <td className="px-3 py-2 text-slate-400">
                {relationship.profile ?? "all"}
                {relationship.generationId
                  ? ` · ${relationship.generationId}`
                  : ""}
              </td>
              <td className="px-3 py-2">
                <span className="font-medium">
                  {topologyEdgeKind(relationship)}
                </span>
                {(relationship.occurrences ?? 1) > 1 ? (
                  <span className="ml-2 text-slate-500">
                    ×{relationship.occurrences?.toLocaleString()}
                  </span>
                ) : null}
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
                {relationship.evidence ? (
                  <span>{relationship.evidence}</span>
                ) : null}
                {relationship.reason ? (
                  <span>
                    {relationship.evidence ? " · " : ""}
                    {relationship.reason}
                  </span>
                ) : null}
                {relationship.evidence || relationship.reason ? (
                  <span> · </span>
                ) : null}
                <span>{relationship.provenance}</span>
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
  const [request, setRequest] = useState<TopologyRequestState>(
    INITIAL_TOPOLOGY_REQUEST,
  );
  const [filters, setFilters] = useState<TopologyFilters>(INITIAL_FILTERS);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [relationshipPage, setRelationshipPage] = useState(0);
  const [showWorktrees, setShowWorktrees] = useState(true);
  const [showInternalRelationships, setShowInternalRelationships] =
    useState(false);
  const [expandedProfiles, setExpandedProfiles] = useState<readonly string[]>(
    [],
  );

  useEffect(() => {
    const controller = new AbortController();
    setState((previous) => ({ ...previous, loading: true, error: null }));
    void fetchTopology(
      {
        generationPins:
          Object.keys(request.generationPins).length > 0
            ? request.generationPins
            : undefined,
      },
      controller.signal,
    )
      .then((data) => {
        if (!controller.signal.aborted)
          setState({ data, error: null, loading: false });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState((previous) => ({
            ...previous,
            error: describe(error),
            loading: false,
          }));
      });
    return () => controller.abort();
  }, [request]);

  const refreshPinnedTopology = useCallback(() => {
    setRequest((previous) => ({
      token: previous.token + 1,
      generationPins: state.data
        ? topologyGenerationPins(state.data.profiles)
        : previous.generationPins,
    }));
  }, [state.data]);
  const loadLatestTopology = useCallback(() => {
    setRequest((previous) => ({
      token: previous.token + 1,
      generationPins: {},
    }));
  }, []);

  const model = useMemo(
    () => (state.data ? createTopologyModel(state.data) : null),
    [state.data],
  );
  const filteredModel = useMemo(
    () => (model ? filterTopology(model, filters) : null),
    [filters, model],
  );
  const scopeModel = useMemo(
    () =>
      model
        ? filterTopology(model, {
            ...INITIAL_FILTERS,
            profile: filters.profile,
          })
        : null,
    [filters.profile, model],
  );
  const scopedData = scopeModel?.response ?? state.data;
  const selectedNode = filteredModel?.nodes.find(
    (node) => node.key === selectedKey,
  );
  const selectedFlowKey = filteredModel?.nodes.some(
    (node) => node.key === selectedKey,
  )
    ? selectedKey
    : null;
  const updateFilter = (key: keyof TopologyFilters, value: string): void => {
    setFilters((previous) => ({ ...previous, [key]: value }));
    setRelationshipPage(0);
  };
  const toggleProfile = useCallback((profileID: string): void => {
    setSelectedKey(null);
    setExpandedProfiles((previous) =>
      previous.includes(profileID)
        ? previous.filter((id) => id !== profileID)
        : [...previous, profileID],
    );
  }, []);

  const profiles = state.data?.profiles.map((profile) => profile.id) ?? [];
  const worktrees = scopeModel
    ? scopeModel.nodes
        .filter((node) => node.type === "worktree")
        .map((node) => node.id)
    : [];
  const repositories = scopeModel
    ? scopeModel.nodes
        .filter((node) => node.type === "repository")
        .map((node) => node.id)
    : [];
  const languages = scopeModel
    ? [...new Set(scopeModel.nodes.flatMap((node) => node.languages))].sort()
    : [];
  const edgeKinds = scopeModel
    ? [
        ...new Set(
          scopeModel.relationships.map((relationship) =>
            topologyEdgeKind(relationship),
          ),
        ),
      ].sort()
    : [];
  const isRepositoryMap = expandedProfiles.length > 0 || showWorktrees;
  const relationshipRowCount = filteredModel?.relationships.length ?? 0;
  const relationshipCount =
    filteredModel?.relationships.reduce(
      (total, relationship) => total + (relationship.occurrences ?? 1),
      0,
    ) ?? 0;
  const totalRelationshipCount =
    model?.relationships.reduce(
      (total, relationship) => total + (relationship.occurrences ?? 1),
      0,
    ) ?? 0;
  const relationshipPageCount = Math.max(
    1,
    Math.ceil(relationshipRowCount / ACCESSIBLE_RELATIONSHIPS_PER_PAGE),
  );
  const visibleRelationshipPage = Math.min(
    relationshipPage,
    relationshipPageCount - 1,
  );
  const firstRelationshipRow =
    visibleRelationshipPage * ACCESSIBLE_RELATIONSHIPS_PER_PAGE;
  const visibleRelationships = filteredModel?.relationships.slice(
    firstRelationshipRow,
    firstRelationshipRow + ACCESSIBLE_RELATIONSHIPS_PER_PAGE,
  );
  const lastRelationshipRow =
    firstRelationshipRow + (visibleRelationships?.length ?? 0);

  useEffect(() => {
    setRelationshipPage((page) => Math.min(page, relationshipPageCount - 1));
  }, [relationshipPageCount]);

  return (
    <div
      className="h-full min-h-0 overflow-hidden bg-shell text-gray-100"
      data-testid="topology-explorer"
    >
      <div className="mx-auto flex h-full min-h-0 w-full max-w-none flex-col gap-2 overflow-hidden p-2 md:p-3">
        <header className="flex shrink-0 flex-wrap items-center justify-between gap-2 pr-28 sm:pr-36">
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <div className="flex items-center gap-2 font-mono text-[10px] font-semibold uppercase tracking-[0.2em] text-graph-package">
              <span className="h-2 w-2 rounded-full bg-graph-package" />
              <span>Kivgraph / topology</span>
              <span className="rounded-none border border-rule-strong px-2 py-0.5 text-[9px] tracking-[0.14em] text-gray-400">
                read only
              </span>
            </div>
            <h1 className="font-mono text-lg font-semibold tracking-tight text-gray-100 md:text-xl">
              Profile topology
            </h1>
            <p className="hidden text-xs text-gray-400 2xl:block">
              A profile is the set of repositories Kivgraph resolves together.
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
              onClick={refreshPinnedTopology}
            >
              check for update
            </Button>
          </div>
        </header>

        {state.loading ? (
          <div className="shrink-0 rounded-none border border-rule bg-panel px-3 py-2 text-xs text-gray-300">
            loading topology…
          </div>
        ) : null}
        {state.error ? (
          <div
            className="flex shrink-0 flex-wrap items-center justify-between gap-2 rounded-none border border-graph-symbol/40 bg-graph-symbol/10 px-3 py-2 text-xs text-gray-200"
            role={
              state.error.startsWith("GENERATION_CHANGED:") ? "status" : "alert"
            }
          >
            <span>
              {state.error.startsWith("GENERATION_CHANGED:")
                ? "A newer generation was published. This map remains pinned to the generation already shown."
                : state.error}
            </span>
            <Button
              type="button"
              size="xs"
              variant="outline"
              onClick={
                state.error.startsWith("GENERATION_CHANGED:")
                  ? loadLatestTopology
                  : refreshPinnedTopology
              }
            >
              {state.error.startsWith("GENERATION_CHANGED:")
                ? "load latest"
                : "retry"}
            </Button>
          </div>
        ) : null}

        {state.data && scopedData && model && filteredModel && scopeModel ? (
          <>
            <div className="grid shrink-0 gap-2 sm:grid-cols-2 xl:grid-cols-4">
              <div className="flex items-center justify-between gap-3 rounded-none border border-rule bg-panel px-3 py-1.5">
                <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
                  visible nodes
                </span>
                <span className="font-mono text-sm font-semibold tabular-nums text-gray-100">
                  {filteredModel.nodes.length}/{model.nodes.length}
                </span>
              </div>
              <div className="flex items-center justify-between gap-3 rounded-none border border-rule bg-panel px-3 py-1.5">
                <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
                  relationships
                </span>
                <span className="font-mono text-sm font-semibold tabular-nums text-gray-100">
                  {relationshipCount.toLocaleString()}/
                  {totalRelationshipCount.toLocaleString()}
                </span>
              </div>
              <div className="flex items-center justify-between gap-3 rounded-none border border-rule bg-panel px-3 py-1.5">
                <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
                  profiles
                </span>
                <span className="font-mono text-sm font-semibold tabular-nums text-gray-100">
                  {
                    scopeModel.nodes.filter((node) => node.type === "profile")
                      .length
                  }
                </span>
              </div>
              <div className="flex min-w-0 items-center justify-between gap-3 rounded-none border border-rule bg-panel px-3 py-1.5">
                <span className="shrink-0 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
                  pinned generation
                </span>
                <span
                  className="min-w-0 truncate text-xs font-medium text-gray-200"
                  title={scopedData.profiles
                    .map((profile) => `${profile.id} ${profile.generationId}`)
                    .join(" · ")}
                >
                  {scopedData.profiles
                    .map((profile) => `${profile.id} ${profile.generationId}`)
                    .join(" · ")}
                </span>
              </div>
            </div>

            {!state.data.completeness.complete ||
            state.data.completeness.truncated ? (
              <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-none border border-graph-symbol/40 bg-graph-symbol/10 px-3 py-2 text-[11px] text-gray-200">
                <span className="font-mono font-semibold uppercase tracking-[0.14em] text-graph-symbol">
                  data quality
                </span>
                {!state.data.completeness.complete ? (
                  <span>
                    incomplete ·{" "}
                    {state.data.completeness.reason ?? "reason not supplied"}
                  </span>
                ) : null}
                {state.data.completeness.truncated ? (
                  <span>API response relationship list truncated</span>
                ) : null}
              </div>
            ) : null}

            <div
              className={`grid min-h-0 min-w-0 flex-1 gap-2 overflow-y-auto lg:overflow-hidden ${
                selectedNode
                  ? "lg:grid-cols-[12rem_minmax(0,1fr)_15rem]"
                  : "lg:grid-cols-[12rem_minmax(0,1fr)]"
              }`}
            >
              <aside className="min-h-0 overflow-y-auto rounded-none border border-rule bg-panel p-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
                      explore
                    </p>
                    <h2 className="mt-1 font-mono text-sm font-semibold text-gray-100">
                      Filters
                    </h2>
                  </div>
                  <Button
                    type="button"
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setFilters(INITIAL_FILTERS);
                      setSelectedKey(null);
                      setExpandedProfiles([]);
                      setShowWorktrees(true);
                      setShowInternalRelationships(false);
                      setRelationshipPage(0);
                    }}
                  >
                    reset
                  </Button>
                </div>

                <div className="mt-4 grid gap-3">
                  <div className="grid gap-1.5 font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
                    <span>search topology</span>
                    <Input
                      className="h-9 rounded-none border-rule bg-raise text-xs font-sans text-gray-200 placeholder:text-gray-500"
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
                    formatOption={displayWorktreeLabel}
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

                <div className="mt-5 grid gap-2 border-t border-rule pt-4">
                  <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">
                    diagram
                  </p>
                  <label className="flex cursor-pointer items-center gap-2 text-xs text-gray-300">
                    <input
                      className="accent-graph-package"
                      type="checkbox"
                      checked={showWorktrees}
                      onChange={(event) => {
                        setShowWorktrees(event.target.checked);
                        if (!event.target.checked) setExpandedProfiles([]);
                      }}
                    />
                    show worktrees
                  </label>
                  <label className="flex cursor-pointer items-center gap-2 text-xs text-gray-300">
                    <input
                      className="accent-graph-symbol"
                      type="checkbox"
                      checked={showInternalRelationships}
                      onChange={(event) =>
                        setShowInternalRelationships(event.target.checked)
                      }
                    />
                    show internal edges
                  </label>
                  <p className="text-[10px] leading-4 text-gray-400">
                    Open a repository group, then choose a repository to see its
                    direct relationships. The canvas never draws the full
                    dependency web at once.
                  </p>
                </div>

                <div className="mt-5 border-t border-rule pt-4">
                  <TopologyLegend />
                </div>
              </aside>

              <section
                className="flex min-h-[36rem] min-w-0 flex-col overflow-hidden rounded-none border border-rule bg-panel lg:min-h-0"
                aria-label="Topology map"
              >
                <div className="flex shrink-0 items-center justify-between gap-3 border-b border-rule bg-panel px-3 py-1.5">
                  <div>
                    <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
                      canvas
                    </p>
                    <p className="mt-1 text-xs text-gray-300">
                      {isRepositoryMap
                        ? "Choose a repository to highlight its direct relationships"
                        : "Open a repository group to explore this profile"}
                    </p>
                  </div>
                  <div className="text-right font-mono text-[10px] tabular-nums text-gray-400">
                    <div className="flex items-center justify-end gap-2">
                      {filteredModel.boundaries.length > 0 ? (
                        <span
                          className="border border-graph-cross/40 bg-graph-cross/10 px-1.5 py-0.5 text-graph-cross"
                          title={`${filteredModel.boundaries
                            .map(
                              (boundary) =>
                                `${boundary.leftProfile} ↔ ${boundary.rightProfile}`,
                            )
                            .join(
                              " · ",
                            )}. Cross-profile code relationships are not evaluated.`}
                        >
                          <span aria-hidden="true">profiles isolated</span>
                          <span className="sr-only">
                            Profiles isolated:{" "}
                            {filteredModel.boundaries
                              .map(
                                (boundary) =>
                                  `${boundary.leftProfile} and ${boundary.rightProfile}`,
                              )
                              .join(", ")}
                            . Cross-profile code relationships are not
                            evaluated.
                          </span>
                        </span>
                      ) : null}
                      <span className="text-gray-200">
                        {isRepositoryMap
                          ? "repository map"
                          : "profile overview"}
                      </span>
                    </div>
                    <span>
                      {isRepositoryMap
                        ? "selection highlights direct links"
                        : "start with composition"}
                    </span>
                  </div>
                </div>
                <div className="min-h-0 flex-1 p-0.5">
                  <TopologyFlow
                    model={filteredModel}
                    selectedKey={selectedFlowKey}
                    onSelect={setSelectedKey}
                    onToggleProfile={toggleProfile}
                    showWorktrees={showWorktrees}
                    showInternalRelationships={showInternalRelationships}
                    expandedProfiles={expandedProfiles}
                  />
                </div>
              </section>

              {selectedNode ? (
                <DetailsPanel
                  node={selectedNode}
                  data={scopedData}
                  model={filteredModel}
                />
              ) : null}
            </div>

            <details className="shrink-0 overflow-hidden rounded-none border border-rule bg-panel">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-3 py-1.5 [&::-webkit-details-marker]:hidden">
                <div className="flex items-baseline gap-3">
                  <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
                    accessibility
                  </span>
                  <span className="font-mono text-xs font-semibold text-gray-100">
                    Relationship list
                  </span>
                </div>
                <div className="flex items-center gap-3 text-right font-mono text-[10px] text-gray-400">
                  <span className="tabular-nums text-gray-200">
                    {filteredModel.relationships.length.toLocaleString()} rows
                  </span>
                  <span>keyboard-friendly detail view</span>
                </div>
              </summary>
              <div className="max-h-72 overflow-auto border-t border-rule p-3">
                {filteredModel.unrenderedRelationships.length > 0 ? (
                  <p className="mb-3 text-[10px] text-graph-symbol">
                    {filteredModel.unrenderedRelationships.length}{" "}
                    relationship(s) omitted because an endpoint is not present.
                  </p>
                ) : null}
                {filteredModel.relationships.length > 0 ? (
                  <>
                    <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-[10px] text-gray-400">
                      <p aria-live="polite">
                        Showing rows {firstRelationshipRow + 1}–
                        {lastRelationshipRow} of{" "}
                        {relationshipRowCount.toLocaleString()} returned
                        relationships.
                      </p>
                      {relationshipPageCount > 1 ? (
                        <nav
                          aria-label="Relationship pagination"
                          className="flex items-center gap-2"
                        >
                          <Button
                            type="button"
                            size="xs"
                            variant="outline"
                            aria-label="Previous relationship page"
                            disabled={visibleRelationshipPage === 0}
                            onClick={() =>
                              setRelationshipPage((page) =>
                                Math.max(0, page - 1),
                              )
                            }
                          >
                            previous
                          </Button>
                          <span aria-current="page">
                            page {visibleRelationshipPage + 1} of{" "}
                            {relationshipPageCount}
                          </span>
                          <Button
                            type="button"
                            size="xs"
                            variant="outline"
                            aria-label="Next relationship page"
                            disabled={
                              visibleRelationshipPage ===
                              relationshipPageCount - 1
                            }
                            onClick={() =>
                              setRelationshipPage((page) =>
                                Math.min(relationshipPageCount - 1, page + 1),
                              )
                            }
                          >
                            next
                          </Button>
                        </nav>
                      ) : null}
                    </div>
                    <RelationshipTable
                      relationships={visibleRelationships ?? []}
                      firstRowIndex={firstRelationshipRow}
                    />
                  </>
                ) : (
                  <p className="rounded-none border border-rule px-3 py-4 text-xs text-gray-400">
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
