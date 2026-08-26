import { type CollectionEntry, getCollection } from "astro:content";
import { PROJECT_TAGLINE } from "../site.mjs";

// Every fact the machine-readable surfaces state lives here once. `robots.txt`,
// `llms.txt`, `llms-full.txt`, the raw markdown endpoint, the Starlight `Head`
// override and the landing shell all read these constants, so the license, the
// repository and the tool list cannot drift into two versions of themselves.
//
// The identity of the project -- name, tagline, summary, repository, license --
// lives one level up in `src/site.mjs`, which is plain ESM with no `astro:*`
// import, because `astro.config.mjs` needs the same name and the same tagline
// and cannot import this file. It is re-exported here so this module stays the
// one every consumer imports from.
//
// The leading underscore keeps Astro from routing this file.

export {
  LICENSE_NAME,
  LICENSE_URL,
  PREVIEW_ALT,
  PROJECT_NAME,
  PROJECT_SUMMARY,
  PROJECT_TAGLINE,
  REPOSITORY_URL,
} from "../site.mjs";

/** A page of the `docs` content collection. */
export type DocEntry = CollectionEntry<"docs">;

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
 * The twelve tools the server registers over stdio, in the order the reference
 * lists them: retrieval first, then lookups, then traversal, then the two
 * whole-graph tools and the one that mutates. `get_unresolved_references` is not
 * among them.
 *
 * `find_by_intent` leads because it is the one that answers without a name, and
 * the surface `internal/mcp/surface_test.go` declares as a contract is what this
 * list has to match: it arrived in v0.8.0 and this list stayed at eleven, which
 * left the tool out of `llms.txt`, out of `llms-full.txt` and out of the sidebar
 * while `mcp/usage.md` already linked to a page that did not exist.
 */
export const MCP_TOOLS = [
  "find_by_intent",
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

/**
 * The collection id of the page documenting one tool.
 *
 * `docs/tools/<tool>`, with underscores spelled as hyphens the way the files
 * under `src/content/docs/docs/tools/` are named. The namespace is `docs/`: it
 * was `reference/` until the rename `astro.config.mjs` still redirects from.
 */
export function toolPageId(tool: McpToolName): string {
  return `docs/tools/${tool.replaceAll("_", "-")}`;
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
 * `/raw/<id>.md`, so `docs/tools/find-references` is served at
 * `/raw/docs/tools/find-references.md`. The `raw/` prefix keeps the
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

/**
 * Whether `/raw/<id>.md` is a route, which is the same question as whether the
 * id is a published member of the `docs` collection.
 *
 * A `<StarlightPage>` -- `/releases/` is one -- is not. Starlight synthesises a
 * route for it whose entry claims `collection: "docs"` and a `filePath` under
 * `src/content/docs/`, so the entry the `Head` override receives is
 * shape-identical to a real page's and only the collection can tell them apart.
 * `src/pages/raw/[...slug].md.ts` generates its paths from the collection, so a
 * `<link rel="alternate" type="text/markdown">` emitted for a synthesised route
 * would advertise a 404.
 */
export async function hasRawMarkdown(id: string): Promise<boolean> {
  const entries = await getCollection("docs", isPublishedDoc);
  return entries.some((entry) => entry.id === id);
}

/** A page's description, falling back to the project one so a link is never bare. */
export function docDescription(entry: DocEntry): string {
  return entry.data.description ?? PROJECT_TAGLINE;
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
 * The pages the sidebar shows under Guides that live at the root of the
 * collection rather than under `guides/`. They are written down because there is
 * no prefix to test: a page whose id is `code-intelligence` is a guide only
 * because the sidebar says so.
 */
const FLAT_GUIDE_IDS: readonly string[] = [
  "code-intelligence",
  "repository-relationships",
  "token-efficient-code-understanding",
  "cross-repository-code-graph",
  "workspace-code-intelligence",
  "kivgraph-faq",
];

/**
 * The sidebar order, expressed as prefixes plus a hint rather than as a list of
 * pages wherever a prefix exists. A page added to `src/content/docs/mcp/` joins
 * the MCP section on its own; a page added somewhere unforeseen lands in
 * `OTHER_GROUP_TITLE`. Neither can vanish from `llms.txt`, which is the point of
 * deriving all of this from `getCollection` instead of writing the pages down
 * twice.
 *
 * The `order` lists are the sidebar's own order, copied from the `sidebar`
 * option in `astro.config.mjs`: a heading that ranked its pages differently
 * would be a second navigation.
 */
const GROUPS: readonly GroupDefinition[] = [
  {
    title: "Start here",
    order: ["install", "quickstart"],
    matches: (id) => id === "install" || id === "quickstart",
  },
  {
    title: "MCP server",
    order: [
      "mcp/clients",
      "mcp/claude-code",
      "mcp/codex",
      "mcp/oh-my-pi",
      "mcp/skills",
      "mcp/usage",
      "mcp/troubleshooting",
    ],
    matches: (id) => id.startsWith("mcp/"),
  },
  {
    title: "Guides",
    order: [
      "guides/indexing",
      "guides/viewer",
      "guides/maintenance",
      ...FLAT_GUIDE_IDS,
    ],
    matches: (id) => id.startsWith("guides/") || FLAT_GUIDE_IDS.includes(id),
  },
  {
    title: "Reference",
    order: [
      "docs/cli",
      "docs/mcp-tools",
      "docs/configuration",
      "docs/resolution",
    ],
    matches: (id) => id.startsWith("docs/") && !id.startsWith("docs/tools/"),
  },
  {
    title: "Tool reference",
    order: MCP_TOOLS.map(toolPageId),
    matches: (id) => id.startsWith("docs/tools/"),
  },
  {
    title: "Benchmark",
    order: ["comparison"],
    matches: (id) => id === "comparison",
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
