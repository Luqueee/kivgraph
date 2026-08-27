/**
 * The event vocabulary, in one place and typed.
 *
 * These are not invented: they are the six actions this site already had
 * instrumented for Umami, which were chosen by looking at what a visitor can
 * actually do here. A documentation site has no signup and no checkout, so the
 * strongest intent it can observe is somebody taking a command away with them --
 * the next step happens in a terminal this site cannot see.
 *
 * Adding an event means adding a member here first. A string literal passed to
 * `track()` will not compile, which is the point: an event name that exists in
 * one call site and nowhere else is a column in a report that nobody can
 * explain six weeks later.
 */
export const ANALYTICS_EVENTS = {
  /** The install one-liner was copied. `where`: `hero` | `final_cta`. */
  INSTALL_COPY: "install_copy",
  /** The setup prompt for a coding agent was copied, from the hero. */
  PROMPT_COPY: "prompt_copy",
  /** `kivgraph mcp install` was copied from the agents band. */
  CLIENT_CONNECT_COPY: "client_connect_copy",
  /** A Quickstart step was copied. `step`: the step's own title. */
  QUICKSTART_COPY: "quickstart_copy",
  /** The MCP client JSON was copied. */
  MCP_CONFIG_COPY: "mcp_config_copy",
  /** A link to the repository was followed. `where`: where on the page. */
  GITHUB_CLICK: "github_click",
} as const;

export type AnalyticsEvent =
  (typeof ANALYTICS_EVENTS)[keyof typeof ANALYTICS_EVENTS];

/**
 * Properties an event may carry.
 *
 * Strings and numbers only, and deliberately so. A report groups by a property,
 * so an unbounded value -- a URL, a search term, anything a visitor typed -- is
 * both a privacy problem and a dimension with one row per visit, which answers
 * nothing. Every property in use here is drawn from a small closed set.
 */
export type AnalyticsProperties = Readonly<Record<string, string | number>>;

/**
 * The conversions this site recognises, which is what a funnel is built from.
 *
 * `install_copy` and `prompt_copy` are primary because they are the last thing
 * this site can observe before adoption happens somewhere it cannot: one takes
 * the command, the other takes the whole setup addressed to an agent.
 *
 * The rest are secondary and mean something weaker but still real -- somebody
 * already installing, or following the guide. `github_click` is deliberately
 * **not** here: it is interest, and counting it as a conversion would inflate
 * the rate with clicks that lead nobody to install anything.
 */
export const PRIMARY_CONVERSIONS: readonly AnalyticsEvent[] = [
  ANALYTICS_EVENTS.INSTALL_COPY,
  ANALYTICS_EVENTS.PROMPT_COPY,
];

export const SECONDARY_CONVERSIONS: readonly AnalyticsEvent[] = [
  ANALYTICS_EVENTS.CLIENT_CONNECT_COPY,
  ANALYTICS_EVENTS.QUICKSTART_COPY,
  ANALYTICS_EVENTS.MCP_CONFIG_COPY,
];
