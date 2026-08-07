export const VIEWER_BINARY_VERSION = 2;
export const VIEWER_BINARY_HEADER_SIZE = 64;
export const VIEWER_BINARY_NODE_SIZE = 48;
export const VIEWER_BINARY_EDGE_SIZE = 16;
export const MAX_VIEWER_PAYLOAD_BYTES = 32 * 1024 * 1024;

export const VIEWER_PAYLOAD_TILES = 1;
export const VIEWER_PAYLOAD_NEIGHBORHOOD = 2;

/** Set on edges whose endpoints are package dense IDs, not symbol ones. */
export const VIEWER_EDGE_FLAG_PACKAGE = 1 << 0;

export const NODE_KIND_REPOSITORY = 1;
export const NODE_KIND_PACKAGE = 2;
export const NODE_KIND_FILE = 3;
export const NODE_KIND_SYMBOL = 4;

export type ViewerPayloadKind =
  | typeof VIEWER_PAYLOAD_TILES
  | typeof VIEWER_PAYLOAD_NEIGHBORHOOD;

export interface GraphBinaryHeader {
  readonly version: number;
  readonly kind: ViewerPayloadKind;
  readonly flags: number;
  readonly snapshotId: bigint;
  readonly nodeCount: number;
  readonly edgeCount: number;
  readonly nodeOffset: number;
  readonly nodeBytes: number;
  readonly edgeOffset: number;
  readonly edgeBytes: number;
  readonly level: number;
  readonly totalBytes: number;
  readonly snapshotVersion: number;
  readonly schemaVersion: number;
  readonly labelOffset: number;
  readonly labelBytes: number;
}

export interface GraphPayload {
  /** The caller transfers ownership to the payload; no bytes are copied. */
  readonly buffer: ArrayBuffer;
  readonly header: GraphBinaryHeader;
  readonly nodes: DataView;
  readonly edges: DataView;
  /** Display name of each node, in node order. */
  readonly labels: readonly string[];
}

export interface GraphNodeRecord {
  readonly id: number;
  readonly parentId: number;
  readonly kind: number;
  readonly level: number;
  readonly parentKind: number;
  readonly depth: number;
  readonly minX: bigint;
  readonly minY: bigint;
  readonly maxX: bigint;
  readonly maxY: bigint;
}

export interface GraphEdgeRecord {
  readonly source: number;
  readonly target: number;
  readonly evidence: number;
  readonly kind: number;
  readonly confidence: number;
  readonly provenance: number;
  readonly flags: number;
}

export interface GraphCoordinateBounds {
  readonly minX: bigint;
  readonly minY: bigint;
  readonly maxX: bigint;
  readonly maxY: bigint;
}

export class GraphBinaryError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "GraphBinaryError";
    this.code = code;
  }
}

/**
 * Decodes a versioned viewer payload without copying its ArrayBuffer.
 *
 * The returned DataViews retain the caller-provided buffer. A worker should
 * transfer the buffer into this function and never reuse it after handoff.
 */
export function decodeGraphPayload(buffer: ArrayBuffer): GraphPayload {
  if (!(buffer instanceof ArrayBuffer)) {
    throw new GraphBinaryError(
      "INVALID_BUFFER",
      "viewer payload must be an ArrayBuffer",
    );
  }
  if (buffer.byteLength < VIEWER_BINARY_HEADER_SIZE) {
    throw new GraphBinaryError(
      "TRUNCATED_PAYLOAD",
      "viewer payload is shorter than its fixed header",
    );
  }

  const view = new DataView(buffer);
  if (
    view.getUint8(0) !== 0x4c ||
    view.getUint8(1) !== 0x47 ||
    view.getUint8(2) !== 0x56 ||
    view.getUint8(3) !== 0x42
  ) {
    throw new GraphBinaryError(
      "INVALID_MAGIC",
      "viewer payload magic is invalid",
    );
  }

  const version = view.getUint16(4, true);
  if (version !== VIEWER_BINARY_VERSION) {
    throw new GraphBinaryError(
      "UNSUPPORTED_VERSION",
      `viewer payload version ${version} is unsupported`,
    );
  }

  const kind = view.getUint8(6);
  if (kind !== VIEWER_PAYLOAD_TILES && kind !== VIEWER_PAYLOAD_NEIGHBORHOOD) {
    throw new GraphBinaryError(
      "INVALID_KIND",
      `viewer payload kind ${kind} is invalid`,
    );
  }

  const flags = view.getUint8(7);
  const snapshotId = view.getBigUint64(8, true);
  const nodeCount = view.getUint32(16, true);
  const edgeCount = view.getUint32(20, true);
  const nodeOffset = view.getUint32(24, true);
  const nodeBytes = view.getUint32(28, true);
  const edgeOffset = view.getUint32(32, true);
  const edgeBytes = view.getUint32(36, true);
  const level = view.getUint8(40);
  const totalBytes = view.getUint32(44, true);
  const snapshotVersion = view.getUint32(48, true);
  const schemaVersion = view.getUint32(52, true);

  if (totalBytes > MAX_VIEWER_PAYLOAD_BYTES) {
    throw new GraphBinaryError(
      "PAYLOAD_TOO_LARGE",
      `viewer payload exceeds ${MAX_VIEWER_PAYLOAD_BYTES} bytes`,
    );
  }
  if (totalBytes !== buffer.byteLength) {
    throw new GraphBinaryError(
      "TRUNCATED_PAYLOAD",
      `viewer payload declares ${totalBytes} bytes but contains ${buffer.byteLength}`,
    );
  }
  if (nodeOffset !== VIEWER_BINARY_HEADER_SIZE) {
    throw new GraphBinaryError(
      "INVALID_OFFSETS",
      "viewer node section does not start after the fixed header",
    );
  }
  if (nodeBytes !== nodeCount * VIEWER_BINARY_NODE_SIZE) {
    throw new GraphBinaryError(
      "INVALID_LENGTHS",
      "viewer node section length does not match its count",
    );
  }
  if (edgeOffset !== nodeOffset + nodeBytes) {
    throw new GraphBinaryError(
      "INVALID_OFFSETS",
      "viewer edge section does not follow the node section",
    );
  }
  if (edgeBytes !== edgeCount * VIEWER_BINARY_EDGE_SIZE) {
    throw new GraphBinaryError(
      "INVALID_LENGTHS",
      "viewer edge section length does not match its count",
    );
  }
  const labelOffset = view.getUint32(56, true);
  const labelBytes = view.getUint32(60, true);
  if (labelOffset !== edgeOffset + edgeBytes) {
    throw new GraphBinaryError(
      "INVALID_OFFSETS",
      "viewer label section does not follow the edge section",
    );
  }
  if (labelOffset + labelBytes !== totalBytes) {
    throw new GraphBinaryError(
      "INVALID_OFFSETS",
      "viewer sections do not cover the complete payload",
    );
  }
  if (snapshotVersion === 0) {
    throw new GraphBinaryError(
      "INVALID_SNAPSHOT",
      "viewer payload has no snapshot version",
    );
  }

  return {
    buffer,
    header: {
      version,
      kind,
      flags,
      snapshotId,
      nodeCount,
      edgeCount,
      nodeOffset,
      nodeBytes,
      edgeOffset,
      edgeBytes,
      level,
      totalBytes,
      snapshotVersion,
      schemaVersion,
      labelOffset,
      labelBytes,
    },
    nodes: new DataView(buffer, nodeOffset, nodeBytes),
    edges: new DataView(buffer, edgeOffset, edgeBytes),
    labels: readLabels(buffer, labelOffset, labelBytes, nodeCount),
  };
}

/**
 * Reads one label per node: a uint16 byte length followed by UTF-8 bytes. The
 * section must describe exactly the nodes the header declares, so a payload
 * that names fewer nodes than it renders is rejected instead of rendered with
 * blank captions.
 */
function readLabels(
  buffer: ArrayBuffer,
  offset: number,
  length: number,
  nodeCount: number,
): readonly string[] {
  const decoder = new TextDecoder();
  const view = new DataView(buffer, offset, length);
  const labels: string[] = [];
  let cursor = 0;
  for (let index = 0; index < nodeCount; index += 1) {
    if (cursor + 2 > length) {
      throw new GraphBinaryError(
        "TRUNCATED_LABELS",
        `viewer label ${index} is missing from the payload`,
      );
    }
    const size = view.getUint16(cursor, true);
    cursor += 2;
    if (cursor + size > length) {
      throw new GraphBinaryError(
        "TRUNCATED_LABELS",
        `viewer label ${index} claims ${size} bytes past the payload`,
      );
    }
    labels.push(decoder.decode(new Uint8Array(buffer, offset + cursor, size)));
    cursor += size;
  }
  if (cursor !== length) {
    throw new GraphBinaryError(
      "INVALID_LENGTHS",
      "viewer label section has trailing bytes",
    );
  }
  return labels;
}

export function readNode(
  payload: GraphPayload,
  index: number,
): GraphNodeRecord {
  assertNodeIndex(payload, index);
  const offset = index * VIEWER_BINARY_NODE_SIZE;
  return {
    id: payload.nodes.getUint32(offset, true),
    parentId: payload.nodes.getUint32(offset + 4, true),
    kind: payload.nodes.getUint8(offset + 8),
    level: payload.nodes.getUint8(offset + 9),
    parentKind: payload.nodes.getUint8(offset + 10),
    depth: payload.nodes.getUint32(offset + 12, true),
    minX: payload.nodes.getBigInt64(offset + 16, true),
    minY: payload.nodes.getBigInt64(offset + 24, true),
    maxX: payload.nodes.getBigInt64(offset + 32, true),
    maxY: payload.nodes.getBigInt64(offset + 40, true),
  };
}

export function readEdge(
  payload: GraphPayload,
  index: number,
): GraphEdgeRecord {
  assertEdgeIndex(payload, index);
  const offset = index * VIEWER_BINARY_EDGE_SIZE;
  return {
    source: payload.edges.getUint32(offset, true),
    target: payload.edges.getUint32(offset + 4, true),
    evidence: payload.edges.getUint32(offset + 8, true),
    kind: payload.edges.getUint8(offset + 12),
    confidence: payload.edges.getUint8(offset + 13),
    provenance: payload.edges.getUint8(offset + 14),
    flags: payload.edges.getUint8(offset + 15),
  };
}

export function readCoordinateBounds(
  payload: GraphPayload,
): GraphCoordinateBounds {
  if (payload.header.nodeCount === 0) {
    return { minX: 0n, minY: 0n, maxX: 1n, maxY: 1n };
  }

  const first = readNode(payload, 0);
  let minX = first.minX;
  let minY = first.minY;
  let maxX = first.maxX;
  let maxY = first.maxY;
  validateBounds(first, 0);

  for (let index = 1; index < payload.header.nodeCount; index += 1) {
    const node = readNode(payload, index);
    validateBounds(node, index);
    if (node.minX < minX) minX = node.minX;
    if (node.minY < minY) minY = node.minY;
    if (node.maxX > maxX) maxX = node.maxX;
    if (node.maxY > maxY) maxY = node.maxY;
  }

  if (minX >= maxX || minY >= maxY) {
    throw new GraphBinaryError(
      "INVALID_BOUNDS",
      "viewer payload has no positive world bounds",
    );
  }
  return { minX, minY, maxX, maxY };
}

function validateBounds(node: GraphNodeRecord, index: number): void {
  if (node.minX >= node.maxX || node.minY >= node.maxY) {
    throw new GraphBinaryError(
      "INVALID_BOUNDS",
      `viewer node ${index} has non-positive bounds`,
    );
  }
}

function assertNodeIndex(payload: GraphPayload, index: number): void {
  if (
    !Number.isInteger(index) ||
    index < 0 ||
    index >= payload.header.nodeCount
  ) {
    throw new RangeError(`viewer node index ${index} is out of range`);
  }
}

function assertEdgeIndex(payload: GraphPayload, index: number): void {
  if (
    !Number.isInteger(index) ||
    index < 0 ||
    index >= payload.header.edgeCount
  ) {
    throw new RangeError(`viewer edge index ${index} is out of range`);
  }
}
