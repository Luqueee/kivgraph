import {
  VIEWER_BINARY_EDGE_SIZE,
  VIEWER_BINARY_HEADER_SIZE,
  VIEWER_BINARY_NODE_SIZE,
  VIEWER_BINARY_VERSION,
  VIEWER_PAYLOAD_TILES,
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
} from "./binary";

interface FixtureNode {
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

interface FixtureEdge {
  readonly source: number;
  readonly target: number;
  readonly evidence: number;
  readonly kind: number;
  readonly confidence: number;
  readonly provenance: number;
  readonly flags: number;
}

const fixtureNodes: readonly FixtureNode[] = [
  {
    id: 0,
    parentId: 0,
    kind: NODE_KIND_REPOSITORY,
    level: 0,
    parentKind: 0,
    depth: 0,
    minX: 0n,
    minY: 0n,
    maxX: 640n,
    maxY: 400n,
  },
  {
    id: 0,
    parentId: 0,
    kind: NODE_KIND_PACKAGE,
    level: 1,
    parentKind: NODE_KIND_REPOSITORY,
    depth: 1,
    minX: 40n,
    minY: 40n,
    maxX: 600n,
    maxY: 360n,
  },
  {
    id: 0,
    parentId: 0,
    kind: NODE_KIND_FILE,
    level: 2,
    parentKind: NODE_KIND_PACKAGE,
    depth: 2,
    minX: 80n,
    minY: 80n,
    maxX: 560n,
    maxY: 320n,
  },
  {
    id: 0,
    parentId: 0,
    kind: NODE_KIND_SYMBOL,
    level: 3,
    parentKind: NODE_KIND_FILE,
    depth: 3,
    minX: 120n,
    minY: 120n,
    maxX: 220n,
    maxY: 180n,
  },
  {
    id: 1,
    parentId: 0,
    kind: NODE_KIND_SYMBOL,
    level: 3,
    parentKind: NODE_KIND_FILE,
    depth: 3,
    minX: 260n,
    minY: 120n,
    maxX: 360n,
    maxY: 180n,
  },
  {
    id: 2,
    parentId: 0,
    kind: NODE_KIND_SYMBOL,
    level: 3,
    parentKind: NODE_KIND_FILE,
    depth: 3,
    minX: 400n,
    minY: 120n,
    maxX: 500n,
    maxY: 180n,
  },
  {
    id: 3,
    parentId: 0,
    kind: NODE_KIND_SYMBOL,
    level: 3,
    parentKind: NODE_KIND_FILE,
    depth: 3,
    minX: 260n,
    minY: 230n,
    maxX: 360n,
    maxY: 290n,
  },
];

const fixtureEdges: readonly FixtureEdge[] = [
  {
    source: 0,
    target: 1,
    evidence: 0,
    kind: 1,
    confidence: 1,
    provenance: 1,
    flags: 0,
  },
  {
    source: 1,
    target: 2,
    evidence: 0,
    kind: 2,
    confidence: 1,
    provenance: 1,
    flags: 0,
  },
  {
    source: 2,
    target: 3,
    evidence: 0,
    kind: 3,
    confidence: 2,
    provenance: 1,
    flags: 0,
  },
  {
    source: 0,
    target: 3,
    evidence: 0,
    kind: 4,
    confidence: 1,
    provenance: 1,
    flags: 0,
  },
];

/** Creates a small valid tile payload used only by the renderer preview. */
export function createDemoPayload(): ArrayBuffer {
  const nodeBytes = fixtureNodes.length * VIEWER_BINARY_NODE_SIZE;
  const edgeBytes = fixtureEdges.length * VIEWER_BINARY_EDGE_SIZE;
  const totalBytes = VIEWER_BINARY_HEADER_SIZE + nodeBytes + edgeBytes;
  const buffer = new ArrayBuffer(totalBytes);
  const view = new DataView(buffer);
  view.setUint8(0, 0x4c);
  view.setUint8(1, 0x47);
  view.setUint8(2, 0x56);
  view.setUint8(3, 0x42);
  view.setUint16(4, VIEWER_BINARY_VERSION, true);
  view.setUint8(6, VIEWER_PAYLOAD_TILES);
  view.setUint8(7, 0);
  view.setBigUint64(8, 1n, true);
  view.setUint32(16, fixtureNodes.length, true);
  view.setUint32(20, fixtureEdges.length, true);
  view.setUint32(24, VIEWER_BINARY_HEADER_SIZE, true);
  view.setUint32(28, nodeBytes, true);
  view.setUint32(32, VIEWER_BINARY_HEADER_SIZE + nodeBytes, true);
  view.setUint32(36, edgeBytes, true);
  view.setUint8(40, 3);
  view.setUint32(44, totalBytes, true);
  view.setUint32(48, 1, true);
  view.setUint32(52, 2, true);

  for (let index = 0; index < fixtureNodes.length; index += 1) {
    const node = fixtureNodes[index];
    const offset = VIEWER_BINARY_HEADER_SIZE + index * VIEWER_BINARY_NODE_SIZE;
    view.setUint32(offset, node.id, true);
    view.setUint32(offset + 4, node.parentId, true);
    view.setUint8(offset + 8, node.kind);
    view.setUint8(offset + 9, node.level);
    view.setUint8(offset + 10, node.parentKind);
    view.setUint32(offset + 12, node.depth, true);
    view.setBigInt64(offset + 16, node.minX, true);
    view.setBigInt64(offset + 24, node.minY, true);
    view.setBigInt64(offset + 32, node.maxX, true);
    view.setBigInt64(offset + 40, node.maxY, true);
  }

  for (let index = 0; index < fixtureEdges.length; index += 1) {
    const edge = fixtureEdges[index];
    const offset =
      VIEWER_BINARY_HEADER_SIZE + nodeBytes + index * VIEWER_BINARY_EDGE_SIZE;
    view.setUint32(offset, edge.source, true);
    view.setUint32(offset + 4, edge.target, true);
    view.setUint32(offset + 8, edge.evidence, true);
    view.setUint8(offset + 12, edge.kind);
    view.setUint8(offset + 13, edge.confidence);
    view.setUint8(offset + 14, edge.provenance);
    view.setUint8(offset + 15, edge.flags);
  }

  return buffer;
}
