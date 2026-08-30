// The endpoint that receives a first-run ping, and the rules that decide
// whether one becomes a row.
//
// It lives beside `ai-report.mjs` and not in a route of its own for the reason
// that module gives: the reporter, the `User-Agent` finding it depends on and
// the fail-closed configuration pair are already in `landing/server.mjs`, and
// an Astro route would need a second copy of all three. What is here is
// everything that can be tested without a listening socket; `server.mjs` wires
// it to one.
//
// ADR 0083 designed this. `docs/development/analytics.md` documents the event
// and its fields, and `/telemetry/` publishes them.

import { REPORTER_HEADERS } from "./ai-report.mjs";

/** The path the installers, the binary and `daemon install` post to. */
export const FIRST_RUN_PATH = "/api/telemetry/first-run";

/** The event name on the first-runs property. */
export const FIRST_RUN_EVENT = "first_run";

// The closed sets. Validation against them is most of what the number is
// worth: the endpoint is public, so without it a forged `platform` invents a
// row no release ever produced, and every report built on the field inherits
// the invention.
export const EMITTERS = Object.freeze(["installer", "binary", "supervisor"]);
export const PLATFORMS = Object.freeze([
  "linux-amd64",
  "darwin-arm64",
  "windows-amd64",
]);
// `source` is deliberately not here. The binary declines to report when it is
// not running from a release layout, because nothing distinguishes a
// developer's `go build` from a CI job's -- so no emitter can ever send that
// value, and a closed set with an unreachable member invites a row nobody can
// explain.
export const CHANNELS = Object.freeze(["installer", "mcpb", "archive"]);
export const TRANSPORTS = Object.freeze(["stdio", "daemon"]);

/** The published version pattern, which is what a release tag carries. */
export const VERSION_PATTERN = /^\d+\.\d+\.\d+$/;

/**
 * The last release that shipped without a first-run emitter.
 *
 * Validating the shape of a ping does not validate its origin, and the gap is
 * not theoretical: within a day of publishing `/telemetry/`, 25 well-formed
 * pings arrived from 25 datacentre addresses, every one claiming `0.9.2` -- a
 * tag whose tree contains no `internal/telemetry/firstrun.go`, so no binary of
 * it has code to emit with. Those rows were impossible rather than unlikely,
 * and the only reason they became rows is that nothing here read the version
 * as a fact the project already knows.
 *
 * A version at or below this one cannot have sent a ping, and that is a truth
 * about a tag already cut: it never needs revising. A bound kept at the
 * *current* release would refuse a stale replay for longer, and would also
 * discard every real ping of a new release the first time someone forgot to
 * raise it -- silently, on exactly the data this property exists to collect.
 * Losing the real number is the worse failure, so the bound stays where it can
 * only ever be right.
 *
 * It is not authentication and nothing here can be: the emitter is open
 * source, so any secret it carried would ship inside it. This refuses a claim
 * the project knows to be false, which is what a public endpoint can honestly
 * do.
 */
export const LAST_VERSION_WITHOUT_EMITTER = "0.9.2";

/**
 * Whether a version is one that could have sent the ping claiming it.
 *
 * The triple is compared numerically, because `0.10.0` is above `0.9.2` and a
 * string comparison puts it below.
 */
export function canEmit(version) {
  const parse = (value) => value.split(".").map(Number);
  const claimed = parse(version);
  const floor = parse(LAST_VERSION_WITHOUT_EMITTER);
  for (let part = 0; part < 3; part += 1) {
    if (claimed[part] !== floor[part]) {
      return claimed[part] > floor[part];
    }
  }
  return false;
}

/**
 * The most a ping may weigh.
 *
 * The whole payload is five short strings, so anything larger is not a client
 * of this endpoint. Reading it to find that out is the part worth refusing.
 */
export const MAX_BODY_BYTES = 1024;

/**
 * How long one machine's report of one version stays deduplicated.
 *
 * A day, because that is the period of the collector's own visitor hash: two
 * pings from one address on one day are already one visitor, so a shorter
 * window would let repetition inflate the event count without ever moving the
 * number anyone reads.
 *
 * The key carries `emitter` and it is the term that is easy to leave out. An
 * installer that has just finished and the first run that follows it arrive
 * seconds apart with the same address and the same version, so a key of those
 * two alone would discard the second -- which is exactly the `binary` row this
 * property exists to collect.
 */
export const DEDUPE_WINDOW_MS = 24 * 60 * 60 * 1000;

/** Entries above which the window is swept, so it cannot grow unbounded. */
export const DEDUPE_MAX_ENTRIES = 5000;

/**
 * Validates a ping and returns its fields, or `null`.
 *
 * Everything about this function is a refusal. Unknown keys are rejected
 * rather than ignored, because a client sending a sixth field is a client this
 * endpoint has not agreed to, and silently dropping it would let a field
 * appear in production that nothing here or in the documentation describes.
 *
 * `transport` belongs to `binary` alone, checked by exclusion rather than by
 * naming `installer` and `supervisor`: a fourth emitter that also has nothing
 * serving yet inherits the right refusal instead of needing this function
 * edited to know about it too.
 */
export function parseFirstRun(text) {
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    return null;
  }
  if (body === null || typeof body !== "object" || Array.isArray(body)) {
    return null;
  }

  const allowed = new Set([
    "emitter",
    "version",
    "platform",
    "channel",
    "transport",
  ]);
  for (const key of Object.keys(body)) {
    if (!allowed.has(key)) {
      return null;
    }
  }

  const { emitter, version, platform, channel, transport } = body;
  if (!EMITTERS.includes(emitter)) return null;
  if (typeof version !== "string" || !VERSION_PATTERN.test(version))
    return null;
  if (!canEmit(version)) return null;
  if (!PLATFORMS.includes(platform)) return null;
  if (!CHANNELS.includes(channel)) return null;

  // Neither an installer nor a supervisor registration has started a server,
  // so a transport on either row would be a default nobody chose, and the
  // field exists precisely to measure which arrangement actually served.
  if (emitter !== "binary") {
    if (transport !== undefined) return null;
    return { emitter, version, platform, channel };
  }
  if (!TRANSPORTS.includes(transport)) return null;
  return { emitter, version, platform, channel, transport };
}

/**
 * The address to attribute the ping to.
 *
 * The process listens behind Cloudflare, so the socket address is the edge's:
 * trusting it would attribute every install on earth to one machine, which is
 * the failure ADR 0083 names. `CF-Connecting-IP` is what the edge sets to the
 * real client and is preferred; `X-Forwarded-For` is the fallback for a
 * deployment without it, and its first hop is the client.
 *
 * Either is a **claim** -- the endpoint is public and anyone can send the
 * header. That is the right trade here: forging one moves an install into
 * someone else's bucket and inflates a count, and reveals nothing, because
 * every path answers `204` and reads nothing back.
 */
export function clientAddress(request) {
  const first = (value) => {
    const header = Array.isArray(value) ? value[0] : value;
    return typeof header === "string" ? header.split(",")[0].trim() : "";
  };
  return (
    first(request.headers["cf-connecting-ip"]) ||
    first(request.headers["x-forwarded-for"]) ||
    request.socket?.remoteAddress ||
    ""
  );
}

/** The window key: one machine, one version, one kind of report. */
export function dedupeKey(address, fields) {
  return `${address} ${fields.version} ${fields.emitter}`;
}

/**
 * The collector payload for one validated ping.
 *
 * **The address travels in the body, and that is a measured decision.** The
 * design said to forward it as a header, and against this instance no header
 * works: both `kivgraph.dev` and the collector sit behind Cloudflare, which
 * rewrites `X-Forwarded-For` at the edge, ignores `X-Real-IP`, and answers
 * `403` to a request that carries `CF-Connecting-IP` itself. Measured on a
 * throwaway property, nine events sent with each of those headers landed in
 * **one** session, carrying the country of the machine that sent them.
 *
 * `payload.ip` is what the collector reads. The same three events sent that
 * way became one session per address -- `8.8.8.8` twice into one session with
 * country `US`, `1.1.1.1` into another with `AU` -- which is the property the
 * whole layer depends on.
 *
 * A `payload.id` is ignored, so a visitor cannot be named from outside; the
 * collector derives it from a daily-rotating hash, and the address is what
 * feeds it.
 */
export function firstRunEvent(fields, { websiteId, hostname, address = "" }) {
  return {
    type: "event",
    payload: {
      website: websiteId,
      hostname,
      // A first run is not a page, and the url is what a report groups by when
      // nothing else is asked of it. One constant path keeps the property from
      // growing a page list it has no pages for.
      url: FIRST_RUN_PATH,
      name: FIRST_RUN_EVENT,
      ...(address === "" ? {} : { ip: address }),
      data: { ...fields },
    },
  };
}

/**
 * A window that forgets, without an interval that would hold the process open.
 *
 * Sweeping on insert is what `server.mjs` already does for the crawler
 * reporter, for the same reason.
 */
export function createWindow({
  windowMs = DEDUPE_WINDOW_MS,
  maxEntries = DEDUPE_MAX_ENTRIES,
} = {}) {
  const seen = new Map();
  return {
    /** Whether this key was already reported inside the window. */
    isDuplicate(key, now) {
      const previous = seen.get(key);
      if (previous !== undefined && now - previous < windowMs) {
        return true;
      }
      if (seen.size >= maxEntries) {
        for (const [entry, at] of seen) {
          if (now - at >= windowMs) {
            seen.delete(entry);
          }
        }
        if (seen.size >= maxEntries) {
          seen.delete(seen.keys().next().value);
        }
      }
      seen.set(key, now);
      return false;
    },
    get size() {
      return seen.size;
    },
  };
}

/**
 * Reads at most `MAX_BODY_BYTES` of a request, or answers `null`.
 *
 * Over the limit it stops keeping the bytes and lets the rest run through
 * without storing them, rather than destroying the socket. Destroying would
 * answer an oversized ping with a connection reset, which is a different
 * answer from the `204` everything else gets -- and a difference in the answer
 * is the one thing this endpoint is not supposed to have.
 */
export function readBody(request, limit = MAX_BODY_BYTES) {
  return new Promise((resolve) => {
    const chunks = [];
    let length = 0;
    let tooLarge = false;
    request.on("data", (chunk) => {
      length += chunk.length;
      if (length > limit) {
        tooLarge = true;
        chunks.length = 0;
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () =>
      resolve(tooLarge ? null : Buffer.concat(chunks).toString("utf8")),
    );
    request.on("error", () => resolve(null));
  });
}

/**
 * The sender: one collector request, fire and forget.
 *
 * It is here rather than in `server.mjs` so that a test can assert what the
 * collector receives instead of what the code meant to send. That distinction
 * is not theoretical: the crawler reporter's `User-Agent` was wrong in
 * production for a day, with a passing suite, because the assertion was on the
 * constant and the collector answers `200` to a request it discards. Measured
 * again here: a `payload.userAgent` of `kivgraph-first-run` came back
 * `{"beep":"boop"}` -- the filter reads the payload too.
 */
export function createSender({ umamiUrl, timeoutMs = 2000 }) {
  return function send(payload) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    fetch(new URL("/api/send", umamiUrl), {
      method: "POST",
      headers: REPORTER_HEADERS,
      body: JSON.stringify(payload),
      signal: controller.signal,
    })
      .catch(() => {})
      .finally(() => clearTimeout(timer));
  };
}

/**
 * Builds the request handler.
 *
 * **Every path answers `204`**, including the ones that refused. A probe that
 * could tell a rejected `platform` from an accepted one would be a description
 * of the closed sets, served to whoever asked; a probe that could tell a
 * deduplicated ping from a fresh one would say whether an address had reported
 * today. Neither is worth the debugging convenience, and the `send` side has
 * the local log for that.
 *
 * `send` and `now` are injected so the tests drive the whole handler without a
 * collector and without waiting a day for the window.
 */
export function createFirstRunHandler({
  websiteId,
  hostname = "kivgraph.dev",
  send,
  now = () => Date.now(),
  window = createWindow(),
}) {
  return async function handleFirstRun(request, response) {
    try {
      if (request.method !== "POST") {
        return;
      }
      const text = await readBody(request);
      if (text === null) {
        return;
      }
      const fields = parseFirstRun(text);
      if (fields === null) {
        return;
      }
      const address = clientAddress(request);
      if (window.isDuplicate(dedupeKey(address, fields), now())) {
        return;
      }
      send(firstRunEvent(fields, { websiteId, hostname, address }), address);
    } catch {
      // A ping never breaks the process that serves the site.
    } finally {
      if (!response.writableEnded) {
        response.writeHead(204).end();
      }
    }
  };
}
