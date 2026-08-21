import { type CollectionEntry, getCollection } from "astro:content";

// Every fact the machine-readable surfaces state lives here once. `robots.txt`,
// `llms.txt`, `llms-full.txt`, the raw markdown endpoint, the Starlight `Head`
// override and the landing shell all read these constants, so the license, the
// repository and the tool list cannot drift into two versions of themselves.
//
// The leading underscore keeps Astro from routing this file.

/** A page of the `docs` content collection. */
export type DocEntry = CollectionEntry<"docs">;

export const PROJECT_NAME = "Kivgraph";

/**
 * One sentence, reused as the `description` of the site, of the software and of
 * any page that carries no `description` of its own. It matches the `description`
 * passed to the Starlight integration so the two never disagree.
 */
export const PROJECT_TAGLINE =
  "A canonical code graph for Go, TypeScript, Rust, Python and Dart, served over MCP.";

/**
 * The paragraph an agent needs before it reads anything else: what the thing is,
 * and the one property that separates it from a grep.
 */
export const PROJECT_SUMMARY =
  'Kivgraph is a local MCP server. It indexes Go, TypeScript, Rust, Python and Dart repositories into a canonical code graph and answers "what breaks if I change this" from that graph. Edges come from language analyzers or are marked CANDIDATE/UNRESOLVED; they are never invented from matching names.';

export const REPOSITORY_URL = "https://github.com/Luqueee/kivgraph";

export const LICENSE_NAME = "Apache-2.0";

/** The SPDX page for the license, which is what `schema.org/license` wants. */
export const LICENSE_URL = "https://spdx.org/licenses/Apache-2.0.html";

/**
 * The token Search Console hands out for HTML-tag verification of the
 * URL-prefix property. It is a literal because it belongs to the property, not
 * to the machine serving it, and Google fetches it from the property root, so
 * only the landing shell emits it; the documentation pages do not.
 */
export const GOOGLE_SITE_VERIFICATION =
  "6Fs8IePpUHnOCsQg8mlX_ADhWLEmTI8MRm41hPztRvI";

/** The self-hosted analytics tracker a deployment carries, if it carries one. */
export interface UmamiTracker {
  /** Absolute URL of the tracker script, served by the Umami instance. */
  readonly src: string;
  /** The id Umami minted for this site, which the script reports against. */
  readonly websiteId: string;
}

/**
 * Reads the analytics deployment out of the environment, the way `site` does:
 * the instance host and the website id belong to the machine that serves the
 * page, not to the source, and the id is a UUID Umami mints at runtime.
 *
 * Both halves of the site call this, and both emit nothing when either half of
 * the pair is missing -- so `astro dev`, a local `astro build` and CI never
 * report a page view into the production dataset.
 */
export function umamiTracker(): UmamiTracker | null {
  const src = import.meta.env.KIVGRAPH_UMAMI_SCRIPT_URL;
  const websiteId = import.meta.env.KIVGRAPH_UMAMI_WEBSITE_ID;
  return src && websiteId ? { src, websiteId } : null;
}

/**
 * The eleven tools the server registers over stdio, in the order the reference
 * lists them: lookups first, then traversal, then the two whole-graph tools and
 * the one that mutates. `get_unresolved_references` is not among them.
 */
export const MCP_TOOLS = [
  "find_symbol",
  "get_symbol",
  "get_source",
  "get_file_outline",
  "find_references",
  "find_cross_repo_consumers",
  "trace_dependencies",
  "get_blast_radius",
  "list_repositories",
  "graph_status",
  "index_project",
] as const;

/** A tool name as it appears in the tool list. */
export type McpToolName = (typeof MCP_TOOLS)[number];

/** The collection id of the reference page documenting one tool. */
export function toolPageId(tool: McpToolName): string {
  return `reference/tools/${tool.replaceAll("_", "-")}`;
}

/**
 * Joins a path onto the configured `site`.
 *
 * `Astro.site` is `URL | undefined` because a project may leave `site` unset;
 * this one always sets it, but the type is honest and so is this function. A
 * missing `site` degrades to a root-relative URL rather than inventing a host.
 * Fragments and query strings survive, and the join never doubles a slash.
 */
export function absoluteUrl(site: URL | undefined, pathname: string): string {
  const origin = (site?.href ?? "/").replace(/\/+$/, "");
  const path = pathname.startsWith("/") ? pathname : `/${pathname}`;
  return `${origin}${path}`;
}

/**
 * The HTML path of a documentation page.
 *
 * Trailing slash, because that is what Starlight writes into `rel="canonical"`:
 * `build.format` is `directory` and `trailingSlash` is `ignore`, and its
 * canonical formatter appends a slash for that pair. A link that disagrees with
 * the canonical is a second URL for one page.
 */
export function docPathname(id: string): string {
  return id === "" ? "/" : `/${id}/`;
}

/**
 * The raw markdown path of a documentation page.
 *
 * `/raw/<id>.md`, so `reference/tools/find-references` is served at
 * `/raw/reference/tools/find-references.md`. The `raw/` prefix keeps the
 * markdown out of the way of Starlight's own `directory`-format routes, which
 * already own `/<id>/`. A page whose id is empty is the collection root, and
 * llmstxt.org asks for `index.md` where a URL has no filename.
 */
export function rawPathname(id: string): string {
  return id === "" ? "/raw/index.md" : `/raw/${id}.md`;
}

/** Starlight's own test for its 404 route, which is not documentation. */
export function isNotFoundPage(id: string): boolean {
  return id === "404" || id.endsWith("/404");
}

/** Pages that belong in the machine-readable listings and the raw endpoint. */
export function isPublishedDoc(entry: DocEntry): boolean {
  return !isNotFoundPage(entry.id) && entry.data.draft !== true;
}

/** A page's description, falling back to the project one so a link is never bare. */
export function docDescription(entry: DocEntry): string {
  return entry.data.description ?? PROJECT_TAGLINE;
}

/**
 * Human-readable names for the path segments a breadcrumb walks through.
 *
 * `reference` reads "Docs" because that is the label the sidebar shows; the URL
 * segment stayed `reference` when the label changed, and a breadcrumb that
 * contradicts the visible navigation is worse than one that repeats it.
 */
const SEGMENT_LABELS: Readonly<Record<string, string | undefined>> = {
  mcp: "MCP server",
  guides: "Guides",
  reference: "Docs",
  tools: "Tools",
};

/** Title-cases an unknown segment rather than leaving `find-references` raw. */
export function segmentLabel(segment: string): string {
  const known = SEGMENT_LABELS[segment];
  if (known !== undefined) return known;
  const spaced = segment.replaceAll("-", " ");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/** A named run of pages, in the order the sidebar presents them. */
export interface DocGroup {
  readonly title: string;
  readonly entries: readonly DocEntry[];
}

interface GroupDefinition {
  readonly title: string;
  /** Ids in sidebar order. An id absent from this list still lands in the group. */
  readonly order: readonly string[];
  readonly matches: (id: string) => boolean;
}

/**
 * The sidebar order, expressed as prefixes plus a hint rather than as a list of
 * pages. A page added to `src/content/docs/mcp/` joins the MCP section on its
 * own; a page added somewhere unforeseen lands in `OTHER_GROUP_TITLE`. Neither
 * can vanish from `llms.txt`, which is the point of deriving all of this from
 * `getCollection` instead of writing the pages down twice.
 */
const GROUPS: readonly GroupDefinition[] = [
  {
    title: "Start here",
    order: ["install", "quickstart"],
    matches: (id) => id === "install" || id === "quickstart",
  },
  {
    title: "MCP server",
    order: ["mcp/clients", "mcp/skills", "mcp/usage", "mcp/troubleshooting"],
    matches: (id) => id.startsWith("mcp/"),
  },
  {
    title: "Guides",
    order: ["guides/indexing", "guides/viewer", "guides/maintenance"],
    matches: (id) => id.startsWith("guides/"),
  },
  {
    title: "Reference",
    order: [
      "reference/cli",
      "reference/mcp-tools",
      "reference/configuration",
      "reference/resolution",
    ],
    matches: (id) =>
      id.startsWith("reference/") && !id.startsWith("reference/tools/"),
  },
  {
    title: "Tool reference",
    order: MCP_TOOLS.map(toolPageId),
    matches: (id) => id.startsWith("reference/tools/"),
  },
  {
    title: "Limits",
    order: ["limits"],
    matches: (id) => id === "limits",
  },
];

const OTHER_GROUP_TITLE = "Other pages";

function sortByOrder(
  entries: readonly DocEntry[],
  order: readonly string[],
): DocEntry[] {
  const rank = (id: string): number => {
    const index = order.indexOf(id);
    return index === -1 ? order.length : index;
  };
  return [...entries].sort(
    (a, b) => rank(a.id) - rank(b.id) || a.id.localeCompare(b.id),
  );
}

/** Every published page, grouped and ordered the way the sidebar presents them. */
export async function loadDocGroups(): Promise<DocGroup[]> {
  const entries = await getCollection("docs", isPublishedDoc);
  const claimed = new Set<string>();
  const groups: DocGroup[] = [];

  for (const definition of GROUPS) {
    const matched = entries.filter(
      (entry) => !claimed.has(entry.id) && definition.matches(entry.id),
    );
    if (matched.length === 0) continue;
    for (const entry of matched) claimed.add(entry.id);
    groups.push({
      title: definition.title,
      entries: sortByOrder(matched, definition.order),
    });
  }

  const rest = entries.filter((entry) => !claimed.has(entry.id));
  if (rest.length > 0) {
    groups.push({ title: OTHER_GROUP_TITLE, entries: sortByOrder(rest, []) });
  }

  return groups;
}

/** Every published page as one flat list, still in sidebar order. */
export async function loadDocsInOrder(): Promise<DocEntry[]> {
  const groups = await loadDocGroups();
  return groups.flatMap((group) => [...group.entries]);
}
