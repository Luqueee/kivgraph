// The process pm2 runs, and the only place on this site that sees every HTTP
// request.
//
// It exists because an Astro middleware does not. Measured on the built output:
// a middleware placed at `src/middleware.ts` saw **one** of seven routes --
// `/raw/docs/cli.md`, the only one that is not prerendered -- and missed `/`,
// `/docs/cli/`, `/install/`, `/robots.txt`, `/sitemap-0.xml` and the 404,
// because the adapter's static handler answers those before the SSR pipeline
// exists. An AI crawler asks for exactly those pages, so a middleware would
// have detected almost nothing.
//
// What this file does instead is delegate. `dist/server/entry.mjs` exports the
// `createStandaloneHandler` the adapter would have run itself, and honours
// `ASTRO_NODE_AUTOSTART=disabled`, so wrapping it costs one `http.createServer`
// and changes nothing about how a page is served: static files, the trailing
// slash `301` and the 404 all come out of the same handler as before. Verified
// through this wrapper: `/` `200`, `/docs/cli/` `200`, `/robots.txt` `200`,
// `/sitemap-0.xml` `200`, `/docs/cli` `301` to the canonical.
//
// Everything analytics-related here is best-effort and off the request path.
// The crawler gets its response first; nothing waits on Umami.

process.env.ASTRO_NODE_AUTOSTART = "disabled";

import { existsSync } from "node:fs";
import { createServer } from "node:http";

// The deployment declares itself in `landing/.env`, and this process reads it
// the same way `astro.config.mjs` does: it is plain Node, so nothing puts that
// file into `process.env` on its own. A variable already exported wins over the
// file, which is what lets pm2 or the shell override it.
if (existsSync(new URL(".env", import.meta.url))) {
  process.loadEnvFile(new URL(".env", import.meta.url).pathname);
}

import { detectAiAgent } from "./src/ai-agents.mjs";

const { handler } = await import("./dist/server/entry.mjs");

const HOST = process.env.HOST ?? "0.0.0.0";
const PORT = Number(process.env.PORT ?? 6767);

// --- configuration ---------------------------------------------------------
//
// Fail-closed on the pair, exactly like the browser tracker in `_seo.ts`: with
// half of it missing nothing is sent, so a development machine and CI cannot
// write into the production dataset by accident. Development is therefore off
// by default, because neither variable is set there.
//
// The website id is the **AI crawlers** property and never the main one. The
// separation is the entire point: a crawler must not move visitors, bounce
// rate, visit duration or a conversion rate that describes people.
const UMAMI_URL = process.env.KIVGRAPH_UMAMI_URL;
const AI_WEBSITE_ID = process.env.KIVGRAPH_UMAMI_AI_WEBSITE_ID;
const TRACKING_ENABLED =
  Boolean(UMAMI_URL) &&
  Boolean(AI_WEBSITE_ID) &&
  process.env.KIVGRAPH_UMAMI_AI_TRACKING !== "off";

/** Milliseconds before a report to Umami is abandoned. */
const SEND_TIMEOUT_MS = 2000;

/**
 * How long the same agent asking for the same path stays deduplicated.
 *
 * A crawler re-fetches a page far more often than the page changes, and one row
 * per fetch buys nothing: the question a report answers is which pages an agent
 * is reading and how that moves over weeks, and ten identical rows an hour
 * apart answer it no better than one. Fifteen minutes keeps a real revisit and
 * drops a burst.
 *
 * This is deduplication and deliberately not sampling. Sampling would throw
 * away rows at random and make a count of a low-volume agent meaningless;
 * nothing here is dropped that a distinct (agent, path) pair did not already
 * report inside the window. Volume is small enough that sampling would be
 * solving a problem this site does not have.
 */
const DEDUPE_WINDOW_MS = 15 * 60 * 1000;

/** Entries above which the dedupe map is swept, so it cannot grow unbounded. */
const DEDUPE_MAX_ENTRIES = 5000;

const recentlySeen = new Map();

/**
 * Whether this (agent, path) pair was already reported inside the window.
 *
 * Sweeping on insert rather than on a timer keeps the process free of an
 * interval that would hold it open and would have to be cleared on shutdown.
 */
function isDuplicate(key, now) {
  const previous = recentlySeen.get(key);
  if (previous !== undefined && now - previous < DEDUPE_WINDOW_MS) {
    return true;
  }
  if (recentlySeen.size >= DEDUPE_MAX_ENTRIES) {
    for (const [entry, at] of recentlySeen) {
      if (now - at >= DEDUPE_WINDOW_MS) {
        recentlySeen.delete(entry);
      }
    }
    // Still full of live entries: drop the oldest rather than grow. Losing a
    // dedupe decision costs one duplicate row; growing without a bound costs
    // the process.
    if (recentlySeen.size >= DEDUPE_MAX_ENTRIES) {
      recentlySeen.delete(recentlySeen.keys().next().value);
    }
  }
  recentlySeen.set(key, now);
  return false;
}

/**
 * Paths worth a row.
 *
 * A crawler pulls the stylesheet, the fonts and every hashed asset of a page it
 * reads, and each of those would be a row saying nothing: the question is which
 * **document** an agent read. `/_astro/` is Astro's own bundle directory and is
 * where the bulk of that noise lives.
 *
 * `robots.txt`, `sitemap-index.xml`, `sitemap-0.xml`, `llms.txt`,
 * `llms-full.txt` and `/raw/**.md` are kept on purpose even though they are not
 * pages. They are how an agent discovers and reads this site, and a crawler
 * fetching `llms.txt` before anything else is a different behaviour from one
 * walking the sitemap -- which is a thing worth being able to see.
 */
const IGNORED_EXTENSIONS =
  /\.(css|js|mjs|map|png|jpe?g|gif|svg|webp|avif|ico|woff2?|ttf|eot|json)$/i;

function isInterestingPath(pathname) {
  if (pathname.startsWith("/_astro/") || pathname.startsWith("/pagefind/")) {
    return false;
  }
  if (pathname === "/favicon.ico" || pathname === "/site.webmanifest") {
    return false;
  }
  // The agent-facing surfaces keep their extension and are the exception the
  // rule above would otherwise swallow.
  if (
    pathname === "/robots.txt" ||
    pathname === "/llms.txt" ||
    pathname === "/llms-full.txt" ||
    pathname.startsWith("/sitemap") ||
    pathname.endsWith(".md")
  ) {
    return true;
  }
  return !IGNORED_EXTENSIONS.test(pathname);
}

/**
 * Reports one request to the AI crawlers property.
 *
 * Fire-and-forget with a timeout, and every failure is swallowed: if Umami is
 * down, slow or unreachable, this site serves exactly as it did before. The
 * response to the crawler has already been written by the time this runs.
 *
 * Umami's collector rejects a request whose own `User-Agent` looks like a bot,
 * which is precisely what the string we are reporting about is. So the sender
 * identifies itself neutrally and honestly -- it is this server, not a browser
 * and not the crawler -- and the real agent travels as normalised metadata
 * rather than as a spoofed header. Disabling Umami's bot filter globally would
 * have been the other way to solve it, and it is the wrong one: it protects the
 * main property from exactly the contamination this whole design avoids.
 */
function report(payload) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), SEND_TIMEOUT_MS);
  fetch(new URL("/api/send", UMAMI_URL), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "User-Agent": "kivgraph-landing/1.0 (+https://kivgraph.dev)",
    },
    body: JSON.stringify(payload),
    signal: controller.signal,
  })
    .catch(() => {})
    .finally(() => clearTimeout(timer));
}

/**
 * One line per detected request, on stdout, where pm2 already collects it.
 *
 * This is the source of truth that survives Umami being down or a report being
 * deduplicated, and it is a single line of JSON so it can be grepped or fed to
 * anything later without a second logging system.
 *
 * What is deliberately absent: the client address, any cookie, any header, the
 * query string and the request body. The full user agent is kept because it is
 * the one field that lets an unrecognised agent be turned into a registry row,
 * and it identifies a robot rather than a person.
 */
function logDetection(entry) {
  process.stdout.write(`${JSON.stringify(entry)}\n`);
}

const server = createServer((request, response) => {
  let detected = null;
  let pathname = "";

  // Nothing in here may throw into the request path. A classification bug must
  // cost a missing row, never a page.
  try {
    detected = detectAiAgent(request.headers["user-agent"]);
    if (detected !== null) {
      pathname = new URL(request.url ?? "/", "http://localhost").pathname;
    }
  } catch {
    detected = null;
  }

  if (detected !== null && isInterestingPath(pathname)) {
    // The status is only known once the response is finished, and the status is
    // half the story: an agent walking a list of 404s is a different finding
    // from one reading the documentation.
    response.on("finish", () => {
      try {
        const now = Date.now();
        const record = {
          ai_agent: true,
          ai_provider: detected.provider,
          ai_agent_name: detected.agent,
          ai_category: detected.category,
          // `user_agent` verification only: this server does not resolve DNS or
          // match address ranges on the request path, so a claim is a claim.
          // The providers that publish CIDR files are named in `ai-agents.mjs`
          // for whoever wants to raise this to `ip_range` later, out of band.
          ai_verification: "user_agent",
          verified: false,
          method: request.method,
          path: pathname,
          status: response.statusCode,
          at: new Date(now).toISOString(),
        };
        logDetection(record);

        if (!TRACKING_ENABLED) {
          return;
        }
        if (isDuplicate(`${detected.id} ${pathname}`, now)) {
          return;
        }
        report({
          type: "event",
          payload: {
            website: AI_WEBSITE_ID,
            hostname: request.headers.host ?? "kivgraph.dev",
            url: pathname,
            name: "ai_crawler_request",
            data: {
              provider: detected.provider,
              agent: detected.agent,
              category: detected.category,
              path: pathname,
              method: request.method,
              status: response.statusCode,
              verified: false,
            },
          },
        });
      } catch {
        // Reporting never breaks serving.
      }
    });
  }

  handler(request, response);
});

server.listen(PORT, HOST, () => {
  process.stdout.write(
    `${JSON.stringify({
      msg: "kivgraph-landing listening",
      host: HOST,
      port: PORT,
      ai_tracking: TRACKING_ENABLED,
    })}\n`,
  );
});
