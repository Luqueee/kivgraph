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
