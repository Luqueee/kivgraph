/**
 * Wire codec for the Kivgraph TypeScript worker protocol, version 1.
 *
 * The contract lives in docs/protocol/ts-worker-v1.md and must stay byte for
 * byte compatible with internal/tsworker in Go. The shared fixtures under
 * testdata/protocol/ts-worker-v1 are the executable proof of that compatibility.
 */

/** Only protocol version this codec speaks. */
export const PROTOCOL_VERSION = 1;
/** Largest body accepted or produced, per the protocol. */
export const MAX_FRAME_BYTES = 16 << 20;
/** Size of the big-endian length prefix. */
export const FRAME_HEADER_BYTES = 4;

/**
 * Transport failure classification. Every kind except INVALID_PAYLOAD ends the
 * session, because the byte stream can no longer be trusted; the protocol
 * forbids resynchronising a corrupted stream.
 */
export type FramingErrorKind =
  | "FRAME_TOO_LARGE"
  | "FRAME_EMPTY"
  | "FRAME_TRUNCATED"
  | "INVALID_PAYLOAD"
  | "VERSION_MISMATCH"
  | "IO_FAILURE";

/** Classified transport failure, mirroring the Go FramingError. */
export class FramingError extends Error {
  readonly kind: FramingErrorKind;
  readonly op: string;

  constructor(kind: FramingErrorKind, op: string, detail?: string) {
    super(
      detail ? `tsworker ${op}: ${kind}: ${detail}` : `tsworker ${op}: ${kind}`,
    );
    this.name = "FramingError";
    this.kind = kind;
    this.op = op;
  }

  /** Reports whether the failure invalidates the session. */
  get fatal(): boolean {
    return this.kind !== "INVALID_PAYLOAD";
  }
}

/** Outer object carried by every frame. */
export interface Envelope {
  readonly v: number;
  readonly id: number;
  readonly type: string;
  readonly payload: unknown;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Serialises a value the way Go's encoding/json serialises a map: object keys
 * sorted, no insignificant whitespace. Payloads travel as Go maps, so sorting
 * is what reproduces the same bytes on both sides.
 */
export function canonicalJSON(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value) ?? "null";
  }
  if (Array.isArray(value)) {
    return `[${value.map((entry) => canonicalJSON(entry)).join(",")}]`;
  }
  const record = value as Record<string, unknown>;
  const entries = Object.keys(record)
    .sort()
    .filter((key) => record[key] !== undefined)
    .map((key) => `${JSON.stringify(key)}:${canonicalJSON(record[key])}`);
  return `{${entries.join(",")}}`;
}

/** Checks the invariants the protocol requires of every envelope. */
export function validateEnvelope(envelope: Envelope): void {
  if (envelope.v !== PROTOCOL_VERSION) {
    throw new FramingError(
      "INVALID_PAYLOAD",
      "encode",
      `protocol version ${envelope.v} is not supported`,
    );
  }
  if (typeof envelope.type !== "string" || envelope.type === "") {
    throw new FramingError(
      "INVALID_PAYLOAD",
      "encode",
      "message type must not be empty",
    );
  }
  if (!Number.isInteger(envelope.id) || envelope.id < 0) {
    throw new FramingError(
      "INVALID_PAYLOAD",
      "encode",
      "id must be a non-negative integer",
    );
  }
  if (envelope.payload === undefined || envelope.payload === null) {
    throw new FramingError(
      "INVALID_PAYLOAD",
      "encode",
      "payload must be present",
    );
  }
}

/** Builds a validated envelope with the current protocol version. */
export function newEnvelope(
  id: number,
  type: string,
  payload: unknown,
): Envelope {
  const envelope: Envelope = { v: PROTOCOL_VERSION, id, type, payload };
  validateEnvelope(envelope);
  return envelope;
}

/**
 * Encodes one envelope as a complete frame. Envelope keys are emitted in the
 * order the Go struct declares them, and the payload uses canonical ordering.
 */
export function encodeFrame(envelope: Envelope): Uint8Array {
  validateEnvelope(envelope);
  const body = new TextEncoder().encode(
    `{"v":${envelope.v},"id":${envelope.id},"type":${JSON.stringify(envelope.type)},"payload":${canonicalJSON(envelope.payload)}}`,
  );
  if (body.length > MAX_FRAME_BYTES) {
    throw new FramingError(
      "FRAME_TOO_LARGE",
      "encode",
      `length ${body.length} exceeds ${MAX_FRAME_BYTES}`,
    );
  }
  const frame = new Uint8Array(FRAME_HEADER_BYTES + body.length);
  new DataView(frame.buffer).setUint32(0, body.length, false);
  frame.set(body, FRAME_HEADER_BYTES);
  return frame;
}

function decodeBody(body: Uint8Array): Envelope {
  let parsed: unknown;
  try {
    parsed = JSON.parse(
      new TextDecoder("utf-8", { fatal: true }).decode(body),
    ) as unknown;
  } catch (error: unknown) {
    throw new FramingError(
      "INVALID_PAYLOAD",
      "decode",
      error instanceof Error ? error.message : "body is not valid JSON",
    );
  }
  if (!isPlainObject(parsed)) {
    throw new FramingError(
      "INVALID_PAYLOAD",
      "decode",
      "frame body is not a JSON object",
    );
  }
  if (parsed.v !== PROTOCOL_VERSION) {
    throw new FramingError(
      "VERSION_MISMATCH",
      "decode",
      `protocol version ${String(parsed.v)} is not supported`,
    );
  }
  const envelope: Envelope = {
    v: parsed.v as number,
    id: typeof parsed.id === "number" ? parsed.id : Number.NaN,
    type: typeof parsed.type === "string" ? parsed.type : "",
    payload: parsed.payload,
  };
  try {
    validateEnvelope(envelope);
  } catch (error: unknown) {
    if (error instanceof FramingError) {
      throw new FramingError("INVALID_PAYLOAD", "decode", error.message);
    }
    throw error;
  }
  return envelope;
}

/**
 * Incremental frame decoder. Bytes are pushed as they arrive and complete
 * frames are pulled out; a partial frame simply yields nothing until the rest
 * of it arrives.
 */
export class FrameDecoder {
  private pending: Uint8Array = new Uint8Array(0);
  private ended = false;

  /** Appends transport bytes to the decode buffer. */
  push(chunk: Uint8Array): void {
    if (chunk.length === 0) {
      return;
    }
    const combined = new Uint8Array(this.pending.length + chunk.length);
    combined.set(this.pending, 0);
    combined.set(chunk, this.pending.length);
    this.pending = combined;
  }

  /** Signals end of input. A partial frame then becomes a truncation. */
  end(): void {
    this.ended = true;
  }

  /** Reports bytes buffered but not yet forming a complete frame. */
  get buffered(): number {
    return this.pending.length;
  }

  /**
   * Returns the next complete frame, or undefined when more bytes are needed.
   * Throws FramingError for any protocol violation.
   */
  next(): Envelope | undefined {
    if (this.pending.length < FRAME_HEADER_BYTES) {
      if (this.ended && this.pending.length !== 0) {
        throw new FramingError(
          "FRAME_TRUNCATED",
          "read header",
          "incomplete length prefix",
        );
      }
      return undefined;
    }

    const view = new DataView(
      this.pending.buffer,
      this.pending.byteOffset,
      this.pending.byteLength,
    );
    const length = view.getUint32(0, false);
    if (length === 0) {
      throw new FramingError("FRAME_EMPTY", "read header");
    }
    // The length is validated before any allocation, so a hostile prefix cannot
    // make the process reserve 4 GiB.
    if (length > MAX_FRAME_BYTES) {
      throw new FramingError(
        "FRAME_TOO_LARGE",
        "read header",
        `length ${length} exceeds ${MAX_FRAME_BYTES}`,
      );
    }

    const total = FRAME_HEADER_BYTES + length;
    if (this.pending.length < total) {
      if (this.ended) {
        throw new FramingError(
          "FRAME_TRUNCATED",
          "read body",
          `announced ${length} bytes, received ${this.pending.length - FRAME_HEADER_BYTES}`,
        );
      }
      return undefined;
    }

    const body = this.pending.subarray(FRAME_HEADER_BYTES, total);
    // The frame boundary is honoured before decoding, so an invalid body leaves
    // the stream aligned and the session usable.
    this.pending = this.pending.subarray(total);
    return decodeBody(body);
  }
}

/** Decodes exactly one frame from a complete buffer. */
export function decodeFrame(frame: Uint8Array): Envelope {
  const decoder = new FrameDecoder();
  decoder.push(frame);
  decoder.end();
  const envelope = decoder.next();
  if (envelope === undefined) {
    throw new FramingError(
      "FRAME_TRUNCATED",
      "read",
      "buffer does not contain a whole frame",
    );
  }
  return envelope;
}

/** Streams envelopes from a chunked transport such as process.stdin. */
export async function* readFrames(
  source: AsyncIterable<Uint8Array | string>,
): AsyncGenerator<Envelope> {
  const decoder = new FrameDecoder();
  for await (const chunk of source) {
    decoder.push(
      typeof chunk === "string" ? new TextEncoder().encode(chunk) : chunk,
    );
    for (;;) {
      const envelope = decoder.next();
      if (envelope === undefined) {
        break;
      }
      yield envelope;
    }
  }
  decoder.end();
  for (;;) {
    const envelope = decoder.next();
    if (envelope === undefined) {
      return;
    }
    yield envelope;
  }
}
