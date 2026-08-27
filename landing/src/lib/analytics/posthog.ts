import { PRODUCTION_HOST } from "../../site.mjs";
import type { AnalyticsEvent, AnalyticsProperties } from "./events";

/**
 * The PostHog half of the analytics layer.
 *
 * Two decisions here are worth stating, because both were the audit's answer
 * rather than a default.
 *
 * **Pageviews are PostHog's own.** This site has no `<ClientRouter />` and no
 * View Transitions -- measured, not assumed -- so every navigation is a full
 * document load. PostHog's automatic capture therefore fires exactly once per
 * page, which is the correct count. Adding an `astro:page-load` listener on top
 * of it is what produces two `$pageview`s per navigation, and adding one
 * *instead* of it would mean disabling the automatic capture to reimplement
 * what it already does. Neither buys anything on an MPA.
 *
 * **The library is imported dynamically, after load.** This is a documentation
 * site with no framework islands: its whole JavaScript budget is 72 KB gzipped,
 * and `posthog-js` is comparable to that on its own. A static import would put
 * it on the critical path of a page whose LCP is measured and written down.
 * Loading it after the `load` event keeps it off that path entirely -- the cost
 * is that an event fired in the first second may find no client, which is
 * `flushQueue` below.
 */

type PostHogClient = {
  capture: (event: string, properties?: Record<string, unknown>) => void;
};

let client: PostHogClient | undefined;
let loading = false;

/**
 * Events that happened before the library finished loading.
 *
 * Bounded on purpose: if PostHog never arrives -- blocked, offline, a bad key --
 * this must not grow with every click for the life of the page.
 */
const queued: Array<{ event: string; properties?: AnalyticsProperties }> = [];
const QUEUE_LIMIT = 20;

function flushQueue(): void {
  if (client === undefined) {
    return;
  }
  for (const item of queued.splice(0, queued.length)) {
    try {
      client.capture(item.event, item.properties);
    } catch {
      // Swallowed on purpose.
    }
  }
}

/**
 * Loads and initialises PostHog exactly once per document.
 *
 * The guard is belt and braces rather than strictly required: navigation here
 * is a full document load, so each page starts with a fresh module instance.
 * It costs one boolean and it is what keeps this correct if a `<ClientRouter />`
 * is ever added, which is the change that would otherwise turn one `init` into
 * one per navigation.
 */
export async function initPostHog(): Promise<void> {
  if (client !== undefined || loading) {
    return;
  }
  const key = import.meta.env.PUBLIC_POSTHOG_KEY;
  const host = import.meta.env.PUBLIC_POSTHOG_HOST;
  if (!key || !host) {
    return;
  }
  loading = true;

  try {
    const { default: posthog } = await import("posthog-js");
    posthog.init(key, {
      api_host: host,
      // The default. Stated rather than omitted because it is the decision the
      // audit made: one document load, one pageview, no manual capture.
      capture_pageview: true,
      // Clicks and form submissions on interactive elements. Conversions stay
      // explicit -- `install_copy` must never depend on PostHog deciding that a
      // button whose text is `copy` means somebody installed something.
      autocapture: true,
      capture_pageleave: true,
      // A documentation site's screen is full of commands and paths rather than
      // personal data, but a recording is still a recording: every input and
      // text field is masked, and the site carries no form that takes personal
      // data anyway.
      session_recording: {
        maskAllInputs: true,
      },
      // A quarter of sessions. Enough to see how a page is read without
      // spending the free tier on a documentation site's traffic; the figure
      // is revisited once real session volume is known rather than guessed.
      disable_session_recording: false,
      persistence: "localStorage+cookie",
    });
    // `sessionRecording.sampleRate` is set through the project's own settings
    // rather than here, because the dashboard is where it can be changed
    // without a deploy.
    client = posthog as unknown as PostHogClient;
    flushQueue();
  } catch {
    // A blocked or failed bundle leaves `client` undefined and the site intact.
  } finally {
    loading = false;
  }
}

/** Whether analytics may run at all: production, on the real host. */
export function isAnalyticsEnabled(): boolean {
  if (!import.meta.env.PROD) {
    return false;
  }
  if (typeof location === "undefined") {
    return false;
  }
  // `PRODUCTION_HOST` and not `location.host` compared against itself: a
  // preview deployment serves the same bundle from another hostname, and this
  // is what keeps its traffic out of the dataset. `www` is deliberately absent
  // because it does not resolve; adding it would be inventing a host.
  return location.hostname === PRODUCTION_HOST;
}

/**
 * Sends one event to PostHog, or queues it until the library arrives.
 *
 * Never throws, for the same reason the Umami side does not.
 */
export function posthogTrack(
  event: AnalyticsEvent | string,
  properties?: AnalyticsProperties,
): void {
  if (client === undefined) {
    if (queued.length < QUEUE_LIMIT) {
      queued.push({ event, properties });
    }
    return;
  }
  try {
    client.capture(event, properties);
  } catch {
    // Swallowed on purpose.
  }
}
