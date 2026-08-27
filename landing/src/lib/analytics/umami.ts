import type { AnalyticsEvent, AnalyticsProperties } from "./events";

/**
 * The Umami half of the analytics layer.
 *
 * Umami's own script is loaded by the two shells -- `Layout.astro` and the
 * Starlight `Head` override -- with `defer`, and it puts `track` on `window`
 * when it runs. Nothing here loads it: this file only speaks to it if it is
 * there.
 *
 * That is why every call reads `window.umami` at call time rather than
 * capturing it once. The script is deferred and served by another host, so it
 * may arrive late, or never -- blocked, an instance that is down, or a build
 * with no tracker configured.
 */

interface UmamiGlobal {
  track: (name: string, data?: unknown) => void;
}

function umami(): UmamiGlobal | undefined {
  return (globalThis as unknown as { umami?: UmamiGlobal }).umami;
}

/** Whether Umami's script has run and exposed its API. */
export function isUmamiReady(): boolean {
  return typeof umami()?.track === "function";
}

/**
 * Sends one event to Umami, or does nothing.
 *
 * Never throws. An analytics failure is not a page failure, and this runs
 * inside a click handler for an action the reader actually asked for.
 */
export function umamiTrack(
  event: AnalyticsEvent,
  properties?: AnalyticsProperties,
): void {
  const client = umami();
  if (client === undefined) {
    return;
  }
  try {
    if (properties === undefined) {
      client.track(event);
    } else {
      client.track(event, properties);
    }
  } catch {
    // Swallowed on purpose.
  }
}
