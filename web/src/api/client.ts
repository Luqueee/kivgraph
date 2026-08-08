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
