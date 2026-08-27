import type { AnalyticsEvent, AnalyticsProperties } from "./events";
import { initPostHog, isAnalyticsEnabled, posthogTrack } from "./posthog";
import { umamiTrack } from "./umami";

export {
  ANALYTICS_EVENTS,
  PRIMARY_CONVERSIONS,
  SECONDARY_CONVERSIONS,
} from "./events";
export type { AnalyticsEvent, AnalyticsProperties } from "./events";
export { isAnalyticsEnabled } from "./posthog";

/**
 * The one call the rest of the site makes.
 *
 * No component imports `posthog-js` or touches `window.umami`. They call this,
 * and which providers exist is a decision that lives here -- so adding or
 * removing one is an edit to this directory rather than to every button.
 *
 * Umami and PostHog answer different questions on purpose: Umami is
 * acquisition -- where a visit came from, which page it landed on, what its Web
 * Vitals were -- and PostHog is behaviour -- what happened next, and whether it
 * ended in a conversion. Sending an event to both is what lets the same action
 * be counted in either frame; the numbers will not match exactly, and expecting
 * them to would be a mistake about method rather than a bug.
 */
export function track(
  event: AnalyticsEvent,
  properties?: AnalyticsProperties,
): void {
  umamiTrack(event, properties);
  posthogTrack(event, properties);
}

/**
 * Starts analytics for this document.
 *
 * Called by the two shells. It does nothing outside production or off the real
 * host, which is what keeps `astro dev`, a local build and any preview
 * deployment out of the dataset.
 *
 * PostHog is loaded after the `load` event rather than during it. The site's
 * JavaScript budget is small and its LCP is measured, so a library of this size
 * does not belong on the critical path; nothing here is needed before the page
 * has painted.
 */
export function initAnalytics(): void {
  if (!isAnalyticsEnabled()) {
    return;
  }

  const start = (): void => {
    void initPostHog();
    bindDeclarativeEvents();
  };

  if (document.readyState === "complete") {
    start();
  } else {
    window.addEventListener("load", start, { once: true });
  }
}

/**
 * Forwards the clicks Umami already captures declaratively.
 *
 * A link carrying `data-umami-event` is reported by Umami's own script with no
 * JavaScript of ours, which is why those attributes exist: they survive this
 * bundle failing to load. PostHog has no equivalent, so this listener exists to
 * give it the same clicks -- and it deliberately does **not** go through
 * `track()`, because that would send a second copy to Umami and count one click
 * twice.
 *
 * This is the one place in the codebase allowed to know that a provider already
 * saw something. It is analytics code rather than a component, which is the
 * difference.
 */
function bindDeclarativeEvents(): void {
  document.addEventListener(
    "click",
    (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const node = target.closest<HTMLElement>("[data-umami-event]");
      const name = node?.dataset.umamiEvent;
      if (name === undefined) {
        return;
      }
      const properties: Record<string, string> = {};
      for (const [key, value] of Object.entries(node?.dataset ?? {})) {
        // `data-umami-event-where` arrives as `umamiEventWhere`; the prefix is
        // Umami's convention and the property name is what follows it.
        if (key.startsWith("umamiEvent") && key !== "umamiEvent" && value) {
          const property = key.slice("umamiEvent".length);
          properties[property.charAt(0).toLowerCase() + property.slice(1)] =
            value;
        }
      }
      posthogTrack(
        name,
        Object.keys(properties).length > 0 ? properties : undefined,
      );
    },
    { capture: true },
  );
}
