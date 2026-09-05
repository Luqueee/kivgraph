import {
  MAX_VIEWER_PAYLOAD_BYTES,
  VIEWER_BINARY_VERSION,
} from "@/renderer/binary";

/** Root viewport of the published layout, as reported by /api/v1/meta. */
export interface LayoutBounds {
  readonly minX: number;
  readonly minY: number;
  readonly maxX: number;
  readonly maxY: number;
  readonly maxLod: number;
  readonly maxNodes: number;
}

export interface SnapshotMeta {
  readonly snapshotId: number | null;
  readonly status: string;
  readonly counts: {
    readonly repositories: number;
    readonly packages: number;
    readonly files: number;
    readonly symbols: number;
    readonly edges: number;
    readonly unresolved: number;
  };
  readonly layout: LayoutBounds | null;
}

export interface SymbolView {
  readonly stableKey: string;
  readonly canonicalIdentity: string;
  readonly repository: string;
  readonly repositoryPath: string;
  readonly package: string;
  readonly modulePath: string;
  readonly file: string;
  readonly language: string;
  readonly name: string;
  readonly qualifiedName: string;
  readonly kind: string;
  readonly signature: string;
  readonly startLine: number;
  readonly endLine: number;
}

export interface SearchResponse {
  readonly snapshotId: number;
  readonly total: number;
  readonly returned: number;
  readonly truncated: boolean;
  readonly results: readonly SymbolView[];
}

export interface NeighborhoodEdge {
  readonly source: string;
  readonly target: string;
  readonly kind: string;
  readonly confidence: string;
  readonly provenance: string;
  readonly evidence?: string;
}

export interface NeighborhoodResponse {
  readonly snapshotId: number;
  readonly root: string;
  readonly direction: string;
  readonly depth: number;
  readonly truncated: boolean;
  readonly nodes: readonly SymbolView[];
  readonly edges: readonly NeighborhoodEdge[];
}

export interface TopologyProfile {
  readonly id: string;
  readonly generationId: string;
  readonly status: string;
  readonly compositionComplete: boolean;
  readonly reason?: string;
  readonly worktrees: readonly string[];
}

export interface TopologyRepository {
  readonly id: string;
  readonly name?: string;
  readonly languages: readonly string[];
}

export interface TopologyWorktree {
  readonly id: string;
  readonly repository: string;
  readonly path: string;
  readonly git?: {
    readonly gitDirectory?: string;
    readonly commonDirectory?: string;
  };
}

export interface TopologyObservation {
  readonly id: string;
  readonly worktree: string;
  readonly commit: string;
  readonly branch?: string;
  readonly dirty: boolean;
  readonly contentDigest: string;
}

export interface TopologySource {
  readonly profile: string;
  readonly repository: string;
  readonly worktree: string;
  readonly status: string;
  readonly reason?: string;
  readonly indexed?: TopologyObservation;
  readonly current?: TopologyObservation;
}

export interface TopologySharedInput {
  readonly type: string;
  readonly id: string;
  readonly owners: readonly string[];
}

export interface TopologyNodeReference {
  readonly type: string;
  readonly id: string;
}

export interface TopologyRelationship {
  readonly profile?: string;
  readonly type: string;
  readonly source: TopologyNodeReference;
  readonly target?: TopologyNodeReference;
  readonly kind?: string;
  readonly status: string;
  readonly confidence: string;
  readonly provenance: string;
  readonly evidence?: string;
  readonly reason?: string;
}

export interface TopologyCompleteness {
  readonly complete: boolean;
  readonly truncated: boolean;
  readonly reason?: string;
}

export interface TopologyResponse {
  readonly apiVersion: string;
  readonly topologyVersion: number;
  readonly status: string;
  readonly generationId?: string;
  readonly selectedProfiles: readonly string[];
  readonly profiles: readonly TopologyProfile[];
  readonly repositories: readonly TopologyRepository[];
  readonly worktrees: readonly TopologyWorktree[];
  readonly sources: readonly TopologySource[];
  readonly sharedInputs: readonly TopologySharedInput[];
  readonly relationships: readonly TopologyRelationship[];
  readonly completeness: TopologyCompleteness;
}

/** An API response the viewer refuses to interpret, with the server's code. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

interface MetaPayload {
  readonly status?: string;
  readonly snapshot_id?: number;
  readonly counts?: Record<string, number>;
  readonly layout?: {
    readonly min_x: number;
    readonly min_y: number;
    readonly max_x: number;
    readonly max_y: number;
    readonly max_lod: number;
    readonly max_nodes: number;
  };
}

interface SymbolPayload {
  readonly stable_key: string;
  readonly canonical_identity: string;
  readonly repository: string;
  readonly repository_path: string;
  readonly package: string;
  readonly module_path: string;
  readonly file: string;
  readonly language: string;
  readonly name: string;
  readonly qualified_name: string;
  readonly kind: string;
  readonly signature: string;
  readonly start_line: number;
  readonly end_line: number;
}

interface SearchPayload {
  readonly snapshot_id: number;
  readonly total: number;
  readonly returned: number;
  readonly truncated: boolean;
  readonly results: readonly SymbolPayload[];
}

interface NeighborhoodPayload {
  readonly snapshot_id: number;
  readonly root: string;
  readonly direction: string;
  readonly depth: number;
  readonly truncated: boolean;
  readonly nodes: readonly SymbolPayload[];
  readonly edges: readonly NeighborhoodEdge[];
}

interface JsonRecord {
  readonly [key: string]: unknown;
}

function asRecord(value: unknown, path: string, status: number): JsonRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw invalidTopologyResponse(path, status);
  }
  return value as JsonRecord;
}

function requiredString(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): string {
  const value = record[key];
  if (typeof value !== "string") throw invalidTopologyResponse(path, status);
  return value;
}

function optionalString(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): string | undefined {
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw invalidTopologyResponse(path, status);
  return value;
}

function requiredBoolean(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): boolean {
  const value = record[key];
  if (typeof value !== "boolean") throw invalidTopologyResponse(path, status);
  return value;
}

function requiredInteger(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): number {
  const value = record[key];
  if (typeof value !== "number" || !Number.isInteger(value)) {
    throw invalidTopologyResponse(path, status);
  }
  return value;
}

function requiredArray(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): readonly unknown[] {
  const value = record[key];
  if (!Array.isArray(value)) throw invalidTopologyResponse(path, status);
  return value;
}

function requiredStringArray(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): readonly string[] {
  return requiredArray(record, key, path, status).map((value, index) => {
    if (typeof value !== "string") {
      throw invalidTopologyResponse(`${path}[${index}]`, status);
    }
    return value;
  });
}

function optionalRecord(
  record: JsonRecord,
  key: string,
  path: string,
  status: number,
): JsonRecord | undefined {
  const value = record[key];
  if (value === undefined) return undefined;
  return asRecord(value, path, status);
}

function invalidTopologyResponse(path: string, status: number): ApiError {
  return new ApiError(
    "INVALID_RESPONSE",
    status,
    `topology response has an invalid ${path}`,
  );
}

function decodeTopologyProfile(
  value: unknown,
  index: number,
  status: number,
): TopologyProfile {
  const record = asRecord(value, `profiles[${index}]`, status);
  const reason = optionalString(
    record,
    "reason",
    `profiles[${index}].reason`,
    status,
  );
  return {
    id: requiredString(record, "id", `profiles[${index}].id`, status),
    generationId: requiredString(
      record,
      "generation_id",
      `profiles[${index}].generation_id`,
      status,
    ),
    status: requiredString(
      record,
      "status",
      `profiles[${index}].status`,
      status,
    ),
    compositionComplete: requiredBoolean(
      record,
      "composition_complete",
      `profiles[${index}].composition_complete`,
      status,
    ),
    ...(reason === undefined ? {} : { reason }),
    worktrees: requiredStringArray(
      record,
      "worktrees",
      `profiles[${index}].worktrees`,
      status,
    ),
  };
}

function decodeTopologyRepository(
  value: unknown,
  index: number,
  status: number,
): TopologyRepository {
  const record = asRecord(value, `repositories[${index}]`, status);
  const name = optionalString(
    record,
    "name",
    `repositories[${index}].name`,
    status,
  );
  return {
    id: requiredString(record, "id", `repositories[${index}].id`, status),
    ...(name === undefined ? {} : { name }),
    languages: requiredStringArray(
      record,
      "languages",
      `repositories[${index}].languages`,
      status,
    ),
  };
}

function decodeTopologyWorktree(
  value: unknown,
  index: number,
  status: number,
): TopologyWorktree {
  const record = asRecord(value, `worktrees[${index}]`, status);
  const git = optionalRecord(record, "git", `worktrees[${index}].git`, status);
  const gitDirectory = git
    ? optionalString(
        git,
        "git_directory",
        `worktrees[${index}].git.git_directory`,
        status,
      )
    : undefined;
  const commonDirectory = git
    ? optionalString(
        git,
        "common_directory",
        `worktrees[${index}].git.common_directory`,
        status,
      )
    : undefined;
  const decodedGit = git
    ? {
        ...(gitDirectory === undefined ? {} : { gitDirectory }),
        ...(commonDirectory === undefined ? {} : { commonDirectory }),
      }
    : undefined;
  return {
    id: requiredString(record, "id", `worktrees[${index}].id`, status),
    repository: requiredString(
      record,
      "repository",
      `worktrees[${index}].repository`,
      status,
    ),
    path: requiredString(record, "path", `worktrees[${index}].path`, status),
    ...(decodedGit === undefined ? {} : { git: decodedGit }),
  };
}

function decodeTopologyObservation(
  value: unknown,
  path: string,
  status: number,
): TopologyObservation {
  const record = asRecord(value, path, status);
  const branch = optionalString(record, "branch", `${path}.branch`, status);
  return {
    id: requiredString(record, "id", `${path}.id`, status),
    worktree: requiredString(record, "worktree", `${path}.worktree`, status),
    commit: requiredString(record, "commit", `${path}.commit`, status),
    ...(branch === undefined ? {} : { branch }),
    dirty: requiredBoolean(record, "dirty", `${path}.dirty`, status),
    contentDigest: requiredString(
      record,
      "content_digest",
      `${path}.content_digest`,
      status,
    ),
  };
}

function decodeTopologySource(
  value: unknown,
  index: number,
  status: number,
): TopologySource {
  const path = `sources[${index}]`;
  const record = asRecord(value, path, status);
  const reason = optionalString(record, "reason", `${path}.reason`, status);
  const indexed =
    record.indexed === undefined
      ? undefined
      : decodeTopologyObservation(record.indexed, `${path}.indexed`, status);
  const current =
    record.current === undefined
      ? undefined
      : decodeTopologyObservation(record.current, `${path}.current`, status);
  return {
    profile: requiredString(record, "profile", `${path}.profile`, status),
    repository: requiredString(
      record,
      "repository",
      `${path}.repository`,
      status,
    ),
    worktree: requiredString(record, "worktree", `${path}.worktree`, status),
    status: requiredString(record, "status", `${path}.status`, status),
    ...(reason === undefined ? {} : { reason }),
    ...(indexed === undefined ? {} : { indexed }),
    ...(current === undefined ? {} : { current }),
  };
}

function decodeTopologySharedInput(
  value: unknown,
  index: number,
  status: number,
): TopologySharedInput {
  const path = `shared_inputs[${index}]`;
  const record = asRecord(value, path, status);
  return {
    type: requiredString(record, "type", `${path}.type`, status),
    id: requiredString(record, "id", `${path}.id`, status),
    owners: requiredStringArray(record, "owners", `${path}.owners`, status),
  };
}

function decodeTopologyNodeReference(
  value: unknown,
  path: string,
  status: number,
): TopologyNodeReference {
  const record = asRecord(value, path, status);
  return {
    type: requiredString(record, "type", `${path}.type`, status),
    id: requiredString(record, "id", `${path}.id`, status),
  };
}

function decodeTopologyRelationship(
  value: unknown,
  index: number,
  status: number,
): TopologyRelationship {
  const path = `relationships[${index}]`;
  const record = asRecord(value, path, status);
  const profile = optionalString(record, "profile", `${path}.profile`, status);
  const kind = optionalString(record, "kind", `${path}.kind`, status);
  const evidence = optionalString(
    record,
    "evidence",
    `${path}.evidence`,
    status,
  );
  const reason = optionalString(record, "reason", `${path}.reason`, status);
  const target =
    record.target === undefined
      ? undefined
      : decodeTopologyNodeReference(record.target, `${path}.target`, status);
  return {
    ...(profile === undefined ? {} : { profile }),
    type: requiredString(record, "type", `${path}.type`, status),
    source: decodeTopologyNodeReference(
      record.source,
      `${path}.source`,
      status,
    ),
    ...(target === undefined ? {} : { target }),
    ...(kind === undefined ? {} : { kind }),
    status: requiredString(record, "status", `${path}.status`, status),
    confidence: requiredString(
      record,
      "confidence",
      `${path}.confidence`,
      status,
    ),
    provenance: requiredString(
      record,
      "provenance",
      `${path}.provenance`,
      status,
    ),
    ...(evidence === undefined ? {} : { evidence }),
    ...(reason === undefined ? {} : { reason }),
  };
}

/** Decodes and validates the compact, versioned topology read model. */
export function decodeTopology(value: unknown, status = 200): TopologyResponse {
  const record = asRecord(value, "envelope", status);
  const generationId = optionalString(
    record,
    "generation_id",
    "generation_id",
    status,
  );
  const completeness = asRecord(record.completeness, "completeness", status);
  const reason = optionalString(
    completeness,
    "reason",
    "completeness.reason",
    status,
  );
  return {
    apiVersion: requiredString(record, "api_version", "api_version", status),
    topologyVersion: requiredInteger(
      record,
      "topology_version",
      "topology_version",
      status,
    ),
    status: requiredString(record, "status", "status", status),
    ...(generationId === undefined ? {} : { generationId }),
    selectedProfiles: requiredStringArray(
      record,
      "selected_profiles",
      "selected_profiles",
      status,
    ),
    profiles: requiredArray(record, "profiles", "profiles", status).map(
      (item, index) => decodeTopologyProfile(item, index, status),
    ),
    repositories: requiredArray(
      record,
      "repositories",
      "repositories",
      status,
    ).map((item, index) => decodeTopologyRepository(item, index, status)),
    worktrees: requiredArray(record, "worktrees", "worktrees", status).map(
      (item, index) => decodeTopologyWorktree(item, index, status),
    ),
    sources: requiredArray(record, "sources", "sources", status).map(
      (item, index) => decodeTopologySource(item, index, status),
    ),
    sharedInputs: requiredArray(
      record,
      "shared_inputs",
      "shared_inputs",
      status,
    ).map((item, index) => decodeTopologySharedInput(item, index, status)),
    relationships: requiredArray(
      record,
      "relationships",
      "relationships",
      status,
    ).map((item, index) => decodeTopologyRelationship(item, index, status)),
    completeness: {
      complete: requiredBoolean(
        completeness,
        "complete",
        "completeness.complete",
        status,
      ),
      truncated: requiredBoolean(
        completeness,
        "truncated",
        "completeness.truncated",
        status,
      ),
      ...(reason === undefined ? {} : { reason }),
    },
  };
}

function decodeSymbol(payload: SymbolPayload): SymbolView {
  return {
    stableKey: payload.stable_key,
    canonicalIdentity: payload.canonical_identity,
    repository: payload.repository,
    repositoryPath: payload.repository_path,
    package: payload.package,
    modulePath: payload.module_path,
    file: payload.file,
    language: payload.language,
    name: payload.name,
    qualifiedName: payload.qualified_name,
    kind: payload.kind,
    signature: payload.signature,
    startLine: payload.start_line,
    endLine: payload.end_line,
  };
}

async function apiError(response: Response): Promise<ApiError> {
  let code = `HTTP_${response.status}`;
  let message = response.statusText || "request failed";
  try {
    const body = (await response.json()) as {
      error?: { code?: string; message?: string };
    };
    code = body.error?.code ?? code;
    message = body.error?.message ?? message;
  } catch {
    // A non-JSON body is reported by status alone; never invented.
  }
  return new ApiError(code, response.status, message);
}

export async function fetchMeta(signal?: AbortSignal): Promise<SnapshotMeta> {
  const response = await fetch("/api/v1/meta", {
    signal,
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw await apiError(response);
  const payload = (await response.json()) as MetaPayload;
  const counts = payload.counts ?? {};
  return {
    snapshotId: payload.snapshot_id ?? null,
    status: payload.status ?? "unknown",
    counts: {
      repositories: counts.repositories ?? 0,
      packages: counts.packages ?? 0,
      files: counts.files ?? 0,
      symbols: counts.symbols ?? 0,
      edges: counts.edges ?? 0,
      unresolved: counts.unresolved ?? 0,
    },
    layout: payload.layout
      ? {
          minX: payload.layout.min_x,
          minY: payload.layout.min_y,
          maxX: payload.layout.max_x,
          maxY: payload.layout.max_y,
          maxLod: payload.layout.max_lod,
          maxNodes: payload.layout.max_nodes,
        }
      : null,
  };
}

export type SearchMode = "exact" | "qualified_exact" | "prefix";
export type NeighborhoodDirection = "incoming" | "outgoing" | "both";

function addSnapshotQuery(
  query: URLSearchParams,
  snapshotId: number | null,
): void {
  if (snapshotId !== null) query.set("snapshot_id", String(snapshotId));
}

export async function searchSymbols(
  name: string,
  mode: SearchMode,
  snapshotId: number | null,
  signal?: AbortSignal,
): Promise<SearchResponse> {
  const query = new URLSearchParams({
    name,
    mode,
    limit: "50",
  });
  addSnapshotQuery(query, snapshotId);
  const response = await fetch(`/api/v1/search?${query.toString()}`, {
    signal,
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw await apiError(response);
  const payload = (await response.json()) as SearchPayload;
  return {
    snapshotId: payload.snapshot_id,
    total: payload.total,
    returned: payload.returned,
    truncated: payload.truncated,
    results: (payload.results ?? []).map(decodeSymbol),
  };
}

export async function fetchSymbol(
  stableKey: string,
  snapshotId: number | null,
  signal?: AbortSignal,
): Promise<{ readonly snapshotId: number; readonly symbol: SymbolView }> {
  const query = new URLSearchParams({ stable_key: stableKey });
  addSnapshotQuery(query, snapshotId);
  const response = await fetch(`/api/v1/symbol?${query.toString()}`, {
    signal,
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw await apiError(response);
  const payload = (await response.json()) as {
    readonly snapshot_id: number;
    readonly symbol: SymbolPayload;
  };
  return {
    snapshotId: payload.snapshot_id,
    symbol: decodeSymbol(payload.symbol),
  };
}

export async function fetchNeighborhood(
  stableKey: string,
  depth: number,
  direction: NeighborhoodDirection,
  snapshotId: number | null,
  signal?: AbortSignal,
): Promise<NeighborhoodResponse> {
  const query = new URLSearchParams({
    stable_key: stableKey,
    depth: String(depth),
    direction,
    max_nodes: "200",
  });
  addSnapshotQuery(query, snapshotId);
  const response = await fetch(`/api/v1/neighborhood?${query.toString()}`, {
    signal,
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw await apiError(response);
  const payload = (await response.json()) as NeighborhoodPayload;
  return {
    snapshotId: payload.snapshot_id,
    root: payload.root,
    direction: payload.direction,
    depth: payload.depth,
    truncated: payload.truncated,
    nodes: (payload.nodes ?? []).map(decodeSymbol),
    edges: payload.edges ?? [],
  };
}

export interface TopologyRequest {
  readonly profiles?: readonly string[];
  readonly generationId?: string;
  readonly generationPins?: Readonly<Record<string, string>>;
}

/** Fetches one generation-consistent topology read model. */
export async function fetchTopology(
  request: TopologyRequest = {},
  signal?: AbortSignal,
): Promise<TopologyResponse> {
  const query = new URLSearchParams();
  for (const profile of request.profiles ?? []) {
    query.append("profile", profile);
  }
  if (request.generationId !== undefined) {
    query.set("generation_id", request.generationId);
  }
  for (const profile of Object.keys(request.generationPins ?? {}).sort()) {
    query.append(
      "generation",
      `${profile}:${(request.generationPins as Record<string, string>)[profile]}`,
    );
  }
  const encodedQuery = query.toString();
  const response = await fetch(
    `/api/v1/topology${encodedQuery.length > 0 ? `?${encodedQuery}` : ""}`,
    {
      signal,
      headers: { Accept: "application/json" },
    },
  );
  if (!response.ok) throw await apiError(response);
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    throw new ApiError(
      "INVALID_RESPONSE",
      response.status,
      "topology response is not valid JSON",
    );
  }
  return decodeTopology(payload, response.status);
}

export interface TileRequest {
  readonly bounds: LayoutBounds;
  readonly lod: number;
  readonly maxNodes: number;
}

/**
 * Fetches one binary tile. The response body is transferred to the caller as
 * an ArrayBuffer and never copied; `decodeGraphPayload` takes ownership.
 */
export async function fetchTile(
  request: TileRequest,
  signal?: AbortSignal,
): Promise<ArrayBuffer> {
  const query = new URLSearchParams({
    min_x: String(request.bounds.minX),
    min_y: String(request.bounds.minY),
    max_x: String(request.bounds.maxX),
    max_y: String(request.bounds.maxY),
    lod: String(request.lod),
    max_nodes: String(request.maxNodes),
    format_version: String(VIEWER_BINARY_VERSION),
  });
  const response = await fetch(`/api/v1/tiles?${query.toString()}`, {
    signal,
    headers: { Accept: "application/octet-stream" },
  });
  if (!response.ok) throw await apiError(response);
  const buffer = await response.arrayBuffer();
  if (buffer.byteLength > MAX_VIEWER_PAYLOAD_BYTES) {
    throw new ApiError(
      "PAYLOAD_TOO_LARGE",
      response.status,
      `tile payload is ${buffer.byteLength} bytes`,
    );
  }
  return buffer;
}
