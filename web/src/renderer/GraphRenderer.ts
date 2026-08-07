import {
  BufferAttribute,
  BufferGeometry,
  LineSegments,
  NearestFilter,
  OrthographicCamera,
  Points,
  RGBAFormat,
  Scene,
  ShaderMaterial,
  SRGBColorSpace,
  UnsignedByteType,
  Vector2,
  Vector3,
  WebGLRenderTarget,
  WebGLRenderer,
} from "three";

import {
  GraphBinaryError,
  type GraphPayload,
  NODE_KIND_FILE,
  NODE_KIND_PACKAGE,
  NODE_KIND_REPOSITORY,
  NODE_KIND_SYMBOL,
  readCoordinateBounds,
  VIEWER_BINARY_NODE_SIZE,
  readEdge,
} from "./binary";

const MIN_ZOOM = 0.25;
const MAX_ZOOM = 24;
const DEFAULT_EDGE_MIN_ZOOM = 0.7;
const DEFAULT_MAX_LABELS = 48;
const MAX_PICK_COLOR_ID = 0xffffff - 1;

const nodeVertexShader = `
  uniform float uPointScale;
  attribute float aSize;
  attribute vec3 aColor;
  varying vec3 vColor;

  void main() {
    vec4 viewPosition = modelViewMatrix * vec4(position, 1.0);
    gl_PointSize = aSize * uPointScale;
    gl_Position = projectionMatrix * viewPosition;
    vColor = aColor;
  }
`;

const nodeFragmentShader = `
  varying vec3 vColor;

  void main() {
    float distanceFromCenter = distance(gl_PointCoord, vec2(0.5));
    if (distanceFromCenter > 0.5) discard;
    gl_FragColor = vec4(vColor, 1.0);
  }
`;

const pickingVertexShader = `
  uniform float uPointScale;
  attribute float aSize;
  attribute vec3 aPickColor;
  varying vec3 vPickColor;

  void main() {
    vec4 viewPosition = modelViewMatrix * vec4(position, 1.0);
    gl_PointSize = aSize * uPointScale;
    gl_Position = projectionMatrix * viewPosition;
    vPickColor = aPickColor;
  }
`;

const pickingFragmentShader = `
  varying vec3 vPickColor;

  void main() {
    float distanceFromCenter = distance(gl_PointCoord, vec2(0.5));
    if (distanceFromCenter > 0.5) discard;
    gl_FragColor = vec4(vPickColor, 1.0);
  }
`;

const edgeVertexShader = `
  attribute vec3 aColor;
  varying vec3 vColor;

  void main() {
    vColor = aColor;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;

const edgeFragmentShader = `
  varying vec3 vColor;

  void main() {
    gl_FragColor = vec4(vColor, 0.72);
  }
`;

export interface GraphNodeHit {
  readonly index: number;
  readonly id: number;
  readonly kind: number;
}

export interface GraphRendererOptions {
  readonly labelContainer?: HTMLElement;
  readonly labelProvider?: (id: number, kind: number) => string | null;
  readonly maxLabels?: number;
  readonly edgeMinZoom?: number;
  readonly onHover?: (hit: GraphNodeHit | null) => void;
}

export class GraphRendererUnavailableError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "GraphRendererUnavailableError";
  }
}

/**
 * Owns one Three.js scene for a decoded payload. The scene contains one
 * Points object for all nodes and one LineSegments object for all edges.
 * Large payloads and geometry arrays never enter React state.
 */
export class GraphRenderer {
  private readonly canvas: HTMLCanvasElement;
  private readonly renderer: WebGLRenderer;
  private readonly scene = new Scene();
  private readonly pickingScene = new Scene();
  private readonly camera = new OrthographicCamera(-1, 1, 1, -1, 0.1, 100);
  private readonly pickingTarget: WebGLRenderTarget;
  private readonly pickingPixel = new Uint8Array(4);
  private readonly projectedLabel = new Vector3();
  private readonly options: Required<
    Pick<GraphRendererOptions, "maxLabels" | "edgeMinZoom">
  > &
    Omit<GraphRendererOptions, "maxLabels" | "edgeMinZoom">;

  private payload: GraphPayload | null = null;
  private nodePoints: Points | null = null;
  private pickingPoints: Points | null = null;
  private edgeLines: LineSegments | null = null;
  private nodeGeometry: BufferGeometry | null = null;
  private pickingGeometry: BufferGeometry | null = null;
  private edgeGeometry: BufferGeometry | null = null;
  private nodeMaterial: ShaderMaterial | null = null;
  private pickingMaterial: ShaderMaterial | null = null;
  private edgeMaterial: ShaderMaterial | null = null;
  private nodePositions = new Float32Array();
  private nodeIds = new Uint32Array();
  private nodeKinds = new Uint8Array();
  private labelIndices: number[] = [];
  private labelElements: HTMLSpanElement[] = [];
  private edgePairCount = 0;
  private drawingWidth = 1;
  private drawingHeight = 1;
  private cssWidth = 1;
  private cssHeight = 1;
  private centerX = 0;
  private centerY = 0;
  private zoom = 1;
  private dragging = false;
  private lastPointerX = 0;
  private lastPointerY = 0;
  private animationFrame: number | null = null;
  private disposed = false;

  private readonly onPointerDown = (event: PointerEvent): void => {
    if (event.button !== 0) return;
    this.dragging = true;
    this.lastPointerX = event.clientX;
    this.lastPointerY = event.clientY;
    this.canvas.setPointerCapture(event.pointerId);
  };

  private readonly onPointerMove = (event: PointerEvent): void => {
    if (this.dragging) {
      this.panBy(
        event.clientX - this.lastPointerX,
        event.clientY - this.lastPointerY,
      );
      this.lastPointerX = event.clientX;
      this.lastPointerY = event.clientY;
      this.render();
      return;
    }

    const hit = this.pick(event.clientX, event.clientY);
    this.options.onHover?.(hit);
  };

  private readonly onPointerUp = (event: PointerEvent): void => {
    this.dragging = false;
    if (this.canvas.hasPointerCapture(event.pointerId)) {
      this.canvas.releasePointerCapture(event.pointerId);
    }
  };

  private readonly onWheel = (event: WheelEvent): void => {
    event.preventDefault();
    const nextZoom = clamp(
      this.zoom * Math.exp(-event.deltaY * 0.001),
      MIN_ZOOM,
      MAX_ZOOM,
    );
    if (nextZoom === this.zoom) return;
    this.zoom = nextZoom;
    this.updateCamera();
    this.render();
  };

  constructor(canvas: HTMLCanvasElement, options: GraphRendererOptions = {}) {
    this.canvas = canvas;
    this.options = {
      ...options,
      maxLabels: options.maxLabels ?? DEFAULT_MAX_LABELS,
      edgeMinZoom: options.edgeMinZoom ?? DEFAULT_EDGE_MIN_ZOOM,
    };

    try {
      this.renderer = new WebGLRenderer({
        canvas,
        antialias: true,
        alpha: true,
        powerPreference: "high-performance",
      });
    } catch (error) {
      throw new GraphRendererUnavailableError(
        "WebGL is unavailable for the graph renderer",
        { cause: error },
      );
    }

    this.renderer.setPixelRatio(Math.min(globalThis.devicePixelRatio || 1, 2));
    this.renderer.setClearColor(0x000000, 0);
    this.renderer.outputColorSpace = SRGBColorSpace;
    this.camera.position.z = 10;

    this.pickingTarget = new WebGLRenderTarget(1, 1, {
      depthBuffer: false,
      stencilBuffer: false,
      format: RGBAFormat,
      minFilter: NearestFilter,
      magFilter: NearestFilter,
      type: UnsignedByteType,
    });

    canvas.addEventListener("pointerdown", this.onPointerDown);
    canvas.addEventListener("pointermove", this.onPointerMove);
    canvas.addEventListener("pointerup", this.onPointerUp);
    canvas.addEventListener("pointercancel", this.onPointerUp);
    canvas.addEventListener("wheel", this.onWheel, { passive: false });
    this.resize();
  }

  load(payload: GraphPayload): void {
    this.assertNotDisposed();
    this.disposeGraph();
    this.payload = payload;
    this.buildGraph(payload);
    this.zoom = 1;
    this.centerX = 0;
    this.centerY = 0;
    this.updateCamera();
    this.updateLabels();
    this.render();
  }

  start(): void {
    this.assertNotDisposed();
    if (this.animationFrame !== null) return;

    const frame = (): void => {
      this.animationFrame = requestAnimationFrame(frame);
      this.render();
    };
    this.animationFrame = requestAnimationFrame(frame);
  }

  stop(): void {
    if (this.animationFrame === null) return;
    cancelAnimationFrame(this.animationFrame);
    this.animationFrame = null;
  }

  resize(): void {
    this.assertNotDisposed();
    const bounds = this.canvas.getBoundingClientRect();
    this.cssWidth = Math.max(
      1,
      Math.round(bounds.width || this.canvas.clientWidth || 1),
    );
    this.cssHeight = Math.max(
      1,
      Math.round(bounds.height || this.canvas.clientHeight || 1),
    );
    this.renderer.setSize(this.cssWidth, this.cssHeight, false);

    const drawingBuffer = new Vector2();
    this.renderer.getDrawingBufferSize(drawingBuffer);
    this.drawingWidth = Math.max(1, drawingBuffer.x);
    this.drawingHeight = Math.max(1, drawingBuffer.y);
    this.pickingTarget.setSize(this.drawingWidth, this.drawingHeight);
    this.updateCamera();
    this.render();
  }

  render(): void {
    if (this.disposed) return;
    this.updateLOD();
    this.renderer.render(this.scene, this.camera);
    this.updateLabels();
  }

  pick(clientX: number, clientY: number): GraphNodeHit | null {
    if (!this.payload || !this.pickingPoints) return null;
    const bounds = this.canvas.getBoundingClientRect();
    const scaleX = this.drawingWidth / Math.max(bounds.width, 1);
    const scaleY = this.drawingHeight / Math.max(bounds.height, 1);
    const pixelX = clampInt(
      Math.floor((clientX - bounds.left) * scaleX),
      0,
      this.drawingWidth - 1,
    );
    const pixelY = clampInt(
      Math.floor((bounds.bottom - clientY) * scaleY),
      0,
      this.drawingHeight - 1,
    );

    const previousTarget = this.renderer.getRenderTarget();
    this.renderer.setRenderTarget(this.pickingTarget);
    this.renderer.setViewport(0, 0, this.cssWidth, this.cssHeight);
    this.renderer.clear(true, true, false);
    this.renderer.render(this.pickingScene, this.camera);
    this.renderer.readRenderTargetPixels(
      this.pickingTarget,
      pixelX,
      pixelY,
      1,
      1,
      this.pickingPixel,
    );
    this.renderer.setRenderTarget(previousTarget);

    const encoded =
      this.pickingPixel[0] |
      (this.pickingPixel[1] << 8) |
      (this.pickingPixel[2] << 16);
    if (encoded === 0) return null;

    const index = encoded - 1;
    if (index < 0 || index >= this.nodeIds.length) return null;
    return {
      index,
      id: this.nodeIds[index],
      kind: this.nodeKinds[index],
    };
  }

  dispose(): void {
    if (this.disposed) return;
    this.stop();
    this.canvas.removeEventListener("pointerdown", this.onPointerDown);
    this.canvas.removeEventListener("pointermove", this.onPointerMove);
    this.canvas.removeEventListener("pointerup", this.onPointerUp);
    this.canvas.removeEventListener("pointercancel", this.onPointerUp);
    this.canvas.removeEventListener("wheel", this.onWheel);
    this.disposeGraph();
    this.pickingTarget.dispose();
    this.renderer.dispose();
    this.options.labelContainer?.replaceChildren();
    this.disposed = true;
  }

  private buildGraph(payload: GraphPayload): void {
    if (payload.header.nodeCount > MAX_PICK_COLOR_ID) {
      throw new GraphBinaryError(
        "PICKING_LIMIT",
        `GPU picking supports at most ${MAX_PICK_COLOR_ID} nodes`,
      );
    }

    const worldBounds = readCoordinateBounds(payload);
    const spanX = Number(worldBounds.maxX - worldBounds.minX);
    const spanY = Number(worldBounds.maxY - worldBounds.minY);
    if (
      !Number.isFinite(spanX) ||
      !Number.isFinite(spanY) ||
      spanX <= 0 ||
      spanY <= 0
    ) {
      throw new GraphBinaryError(
        "INVALID_BOUNDS",
        "viewer coordinates exceed the renderer numeric range",
      );
    }

    const nodeCount = payload.header.nodeCount;
    this.nodePositions = new Float32Array(nodeCount * 3);
    this.nodeIds = new Uint32Array(nodeCount);
    this.nodeKinds = new Uint8Array(nodeCount);
    const nodeSizes = new Float32Array(nodeCount);
    const nodeColors = new Float32Array(nodeCount * 3);
    const pickColors = new Uint8Array(nodeCount * 3);
    const symbolNodeIndices = new Map<number, number>();

    for (let index = 0; index < nodeCount; index += 1) {
      const offset = index * VIEWER_BINARY_NODE_SIZE;
      const id = payload.nodes.getUint32(offset, true);
      const kind = payload.nodes.getUint8(offset + 8);
      const minX = payload.nodes.getBigInt64(offset + 16, true);
      const minY = payload.nodes.getBigInt64(offset + 24, true);
      const maxX = payload.nodes.getBigInt64(offset + 32, true);
      const maxY = payload.nodes.getBigInt64(offset + 40, true);
      const centerX = (minX + maxX) / 2n;
      const centerY = (minY + maxY) / 2n;

      this.nodeIds[index] = id;
      this.nodeKinds[index] = kind;
      this.nodePositions[index * 3] = normalizeCoordinate(
        centerX,
        worldBounds.minX,
        spanX,
      );
      this.nodePositions[index * 3 + 1] = -normalizeCoordinate(
        centerY,
        worldBounds.minY,
        spanY,
      );
      nodeSizes[index] = nodeSizeForKind(kind);
      const color = nodeColorForKind(kind);
      nodeColors[index * 3] = color[0];
      nodeColors[index * 3 + 1] = color[1];
      nodeColors[index * 3 + 2] = color[2];
      const pickColor = encodePickColor(index);
      pickColors[index * 3] = pickColor[0];
      pickColors[index * 3 + 1] = pickColor[1];
      pickColors[index * 3 + 2] = pickColor[2];
      if (kind === NODE_KIND_SYMBOL) symbolNodeIndices.set(id, index);
    }

    const positions = new BufferAttribute(this.nodePositions, 3);
    const sizes = new BufferAttribute(nodeSizes, 1);
    const colors = new BufferAttribute(nodeColors, 3);
    const pickColorAttribute = new BufferAttribute(pickColors, 3, true);

    this.nodeGeometry = new BufferGeometry();
    this.nodeGeometry.setAttribute("position", positions);
    this.nodeGeometry.setAttribute("aSize", sizes);
    this.nodeGeometry.setAttribute("aColor", colors);
    this.nodeGeometry.setAttribute("aPickColor", pickColorAttribute);

    this.nodeMaterial = new ShaderMaterial({
      depthTest: false,
      depthWrite: false,
      fragmentShader: nodeFragmentShader,
      transparent: true,
      uniforms: { uPointScale: { value: 1 } },
      vertexShader: nodeVertexShader,
    });
    this.nodePoints = new Points(this.nodeGeometry, this.nodeMaterial);
    this.nodePoints.frustumCulled = false;
    this.scene.add(this.nodePoints);

    this.pickingGeometry = new BufferGeometry();
    this.pickingGeometry.setAttribute("position", positions);
    this.pickingGeometry.setAttribute("aSize", sizes);
    this.pickingGeometry.setAttribute("aPickColor", pickColorAttribute);
    this.pickingMaterial = new ShaderMaterial({
      depthTest: false,
      depthWrite: false,
      fragmentShader: pickingFragmentShader,
      uniforms: { uPointScale: { value: 1 } },
      vertexShader: pickingVertexShader,
    });
    this.pickingPoints = new Points(this.pickingGeometry, this.pickingMaterial);
    this.pickingPoints.frustumCulled = false;
    this.pickingScene.add(this.pickingPoints);

    const edgePositions = new Float32Array(payload.header.edgeCount * 6);
    const edgeColors = new Float32Array(payload.header.edgeCount * 6);
    let edgePairCount = 0;
    for (
      let edgeIndex = 0;
      edgeIndex < payload.header.edgeCount;
      edgeIndex += 1
    ) {
      const edge = readEdge(payload, edgeIndex);
      const sourceIndex = symbolNodeIndices.get(edge.source);
      const targetIndex = symbolNodeIndices.get(edge.target);
      if (sourceIndex === undefined || targetIndex === undefined) continue;

      const destinationOffset = edgePairCount * 6;
      const sourceOffset = sourceIndex * 3;
      const targetOffset = targetIndex * 3;
      edgePositions[destinationOffset] = this.nodePositions[sourceOffset];
      edgePositions[destinationOffset + 1] =
        this.nodePositions[sourceOffset + 1];
      edgePositions[destinationOffset + 2] = 0;
      edgePositions[destinationOffset + 3] = this.nodePositions[targetOffset];
      edgePositions[destinationOffset + 4] =
        this.nodePositions[targetOffset + 1];
      edgePositions[destinationOffset + 5] = 0;
      const color = edgeColorForConfidence(edge.confidence);
      for (let vertex = 0; vertex < 2; vertex += 1) {
        const colorOffset = destinationOffset + vertex * 3;
        edgeColors[colorOffset] = color[0];
        edgeColors[colorOffset + 1] = color[1];
        edgeColors[colorOffset + 2] = color[2];
      }
      edgePairCount += 1;
    }
    this.edgePairCount = edgePairCount;
    this.edgeGeometry = new BufferGeometry();
    this.edgeGeometry.setAttribute(
      "position",
      new BufferAttribute(edgePositions, 3),
    );
    this.edgeGeometry.setAttribute(
      "aColor",
      new BufferAttribute(edgeColors, 3),
    );
    this.edgeGeometry.setDrawRange(0, edgePairCount * 2);
    this.edgeMaterial = new ShaderMaterial({
      depthTest: false,
      depthWrite: false,
      fragmentShader: edgeFragmentShader,
      transparent: true,
      vertexShader: edgeVertexShader,
    });
    this.edgeLines = new LineSegments(this.edgeGeometry, this.edgeMaterial);
    this.edgeLines.frustumCulled = false;
    this.scene.add(this.edgeLines);

    this.createLabels();
  }

  private createLabels(): void {
    const container = this.options.labelContainer;
    if (!container) return;
    container.replaceChildren();
    this.labelIndices = [];
    this.labelElements = [];

    const nodeCount = this.nodeIds.length;
    const limit = Math.min(nodeCount, this.options.maxLabels);
    for (let index = 0; index < limit; index += 1) {
      const label = this.options.labelProvider?.(
        this.nodeIds[index],
        this.nodeKinds[index],
      );
      if (!label) continue;
      const element = document.createElement("span");
      element.textContent = label;
      element.style.position = "absolute";
      element.style.pointerEvents = "none";
      element.style.transform = "translate3d(-50%, -120%, 0)";
      element.style.whiteSpace = "nowrap";
      element.style.fontSize = "10px";
      element.style.lineHeight = "1";
      element.style.color = "rgba(15, 23, 42, 0.72)";
      element.style.textShadow = "0 1px 2px rgba(255,255,255,0.9)";
      container.append(element);
      this.labelIndices.push(index);
      this.labelElements.push(element);
    }
  }

  private updateLabels(): void {
    const container = this.options.labelContainer;
    if (!container || this.labelElements.length === 0) return;
    for (let index = 0; index < this.labelElements.length; index += 1) {
      const nodeIndex = this.labelIndices[index];
      this.projectedLabel
        .set(
          this.nodePositions[nodeIndex * 3],
          this.nodePositions[nodeIndex * 3 + 1],
          0,
        )
        .project(this.camera);
      const visible =
        this.projectedLabel.z >= -1 &&
        this.projectedLabel.z <= 1 &&
        this.projectedLabel.x >= -1 &&
        this.projectedLabel.x <= 1 &&
        this.projectedLabel.y >= -1 &&
        this.projectedLabel.y <= 1;
      const element = this.labelElements[index];
      element.style.display = visible ? "block" : "none";
      if (!visible) continue;
      const left = ((this.projectedLabel.x + 1) / 2) * this.cssWidth;
      const top = ((1 - this.projectedLabel.y) / 2) * this.cssHeight;
      element.style.left = `${left}px`;
      element.style.top = `${top}px`;
    }
  }

  private updateLOD(): void {
    const showEdges =
      this.edgePairCount > 0 &&
      edgeVisibilityForZoom(this.zoom, this.options.edgeMinZoom);
    if (this.edgeLines) this.edgeLines.visible = showEdges;
    const pointScale = Math.max(0.8, Math.min(1.8, Math.sqrt(this.zoom)));
    if (this.nodeMaterial)
      this.nodeMaterial.uniforms.uPointScale.value = pointScale;
    if (this.pickingMaterial) {
      this.pickingMaterial.uniforms.uPointScale.value = pointScale;
    }
  }

  private updateCamera(): void {
    const aspect = this.cssWidth / Math.max(this.cssHeight, 1);
    const halfHeight = 1 / this.zoom;
    const halfWidth = aspect / this.zoom;
    this.camera.left = this.centerX - halfWidth;
    this.camera.right = this.centerX + halfWidth;
    this.camera.top = this.centerY + halfHeight;
    this.camera.bottom = this.centerY - halfHeight;
    this.camera.updateProjectionMatrix();
  }

  private panBy(deltaX: number, deltaY: number): void {
    const worldWidth = this.camera.right - this.camera.left;
    const worldHeight = this.camera.top - this.camera.bottom;
    this.centerX -= (deltaX / this.cssWidth) * worldWidth;
    this.centerY += (deltaY / this.cssHeight) * worldHeight;
    this.updateCamera();
  }

  private disposeGraph(): void {
    if (this.nodePoints) this.scene.remove(this.nodePoints);
    if (this.pickingPoints) this.pickingScene.remove(this.pickingPoints);
    if (this.edgeLines) this.scene.remove(this.edgeLines);
    this.nodeGeometry?.dispose();
    this.pickingGeometry?.dispose();
    this.edgeGeometry?.dispose();
    this.nodeMaterial?.dispose();
    this.pickingMaterial?.dispose();
    this.edgeMaterial?.dispose();
    this.nodePoints = null;
    this.pickingPoints = null;
    this.edgeLines = null;
    this.nodeGeometry = null;
    this.pickingGeometry = null;
    this.edgeGeometry = null;
    this.nodeMaterial = null;
    this.pickingMaterial = null;
    this.edgeMaterial = null;
    this.nodePositions = new Float32Array();
    this.nodeIds = new Uint32Array();
    this.nodeKinds = new Uint8Array();
    this.edgePairCount = 0;
    this.labelIndices = [];
    this.labelElements = [];
    this.options.labelContainer?.replaceChildren();
  }

  private assertNotDisposed(): void {
    if (this.disposed) throw new Error("graph renderer is already disposed");
  }
}

export function encodePickColor(index: number): [number, number, number] {
  if (!Number.isInteger(index) || index < 0 || index > MAX_PICK_COLOR_ID) {
    throw new RangeError(`GPU picking index ${index} is out of range`);
  }
  const encoded = index + 1;
  return [encoded & 0xff, (encoded >> 8) & 0xff, (encoded >> 16) & 0xff];
}

export function edgeVisibilityForZoom(
  zoom: number,
  minimumZoom: number,
): boolean {
  return (
    Number.isFinite(zoom) && Number.isFinite(minimumZoom) && zoom >= minimumZoom
  );
}

function normalizeCoordinate(
  value: bigint,
  origin: bigint,
  span: number,
): number {
  return (Number(value - origin) / span) * 2 - 1;
}

function nodeSizeForKind(kind: number): number {
  switch (kind) {
    case NODE_KIND_REPOSITORY:
      return 14;
    case NODE_KIND_PACKAGE:
      return 11;
    case NODE_KIND_FILE:
      return 9;
    case NODE_KIND_SYMBOL:
      return 7;
    default:
      return 6;
  }
}

function nodeColorForKind(kind: number): [number, number, number] {
  switch (kind) {
    case NODE_KIND_REPOSITORY:
      return [0.09, 0.42, 0.68];
    case NODE_KIND_PACKAGE:
      return [0.13, 0.58, 0.62];
    case NODE_KIND_FILE:
      return [0.85, 0.47, 0.18];
    case NODE_KIND_SYMBOL:
      return [0.25, 0.22, 0.72];
    default:
      return [0.35, 0.35, 0.4];
  }
}

function edgeColorForConfidence(confidence: number): [number, number, number] {
  if (confidence === 1) return [0.18, 0.32, 0.55];
  if (confidence === 2) return [0.82, 0.46, 0.16];
  return [0.45, 0.48, 0.55];
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function clampInt(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}
