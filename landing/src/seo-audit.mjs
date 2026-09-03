const REQUIRED_FILES = [
  "robots.txt",
  "sitemap-index.xml",
  "sitemap-0.xml",
  "llms.txt",
  "llms-full.txt",
  "llms-blog.txt",
  "rss.xml",
];

const EDGE_RUNTIME_PATHS = Object.freeze(["/github"]);

const HTML_ENTITIES = Object.freeze({
  amp: "&",
  quot: '"',
  "#39": "'",
  apos: "'",
  lt: "<",
  gt: ">",
});

/** Decode HTML entities once, without recursively unescaping the result. */
function decodeHtml(value) {
  return value.replace(
    /&(amp|quot|#39|apos|lt|gt);/g,
    (_, entity) => HTML_ENTITIES[entity],
  );
}

/** Read one HTML attribute without depending on attribute ordering. */
function attribute(tag, name) {
  const match = tag.match(new RegExp(`\\b${name}\\s*=\\s*(["'])(.*?)\\1`, "i"));
  return match === null ? undefined : decodeHtml(match[2]);
}

/** Return all tags with a given name. */
function tags(html, name) {
  return [...html.matchAll(new RegExp(`<${name}\\b[^>]*>`, "gi"))].map(
    (match) => match[0],
  );
}

/** Make a browser URL path comparable to an Astro trailing-slash route. */
export function normalisePath(pathname) {
  if (pathname === "/" || pathname.endsWith("/")) {
    return pathname;
  }
  return /\.[a-z0-9]+$/i.test(pathname) ? pathname : `${pathname}/`;
}

/** Convert one built client filename into the public route it represents. */
export function routeFromClientFile(relativeFile) {
  const normalised = relativeFile.replaceAll("\\", "/");
  if (normalised === "index.html") {
    return "/";
  }
  if (normalised.endsWith("/index.html")) {
    return `/${normalised.slice(0, -"/index.html".length)}/`;
  }
  return `/${normalised}`;
}

/** Return the raw Markdown route paired with a published HTML route. */
export function markdownPathForRoute(pathname) {
  const path = normalisePath(pathname);
  return path === "/" ? undefined : `/raw${path.slice(0, -1)}.md`;
}

/** Build the non-emitted routes that remain valid on the production origin. */
export function runtimePathsForBuild(documents, rawMarkdownRoute) {
  const markdownPaths = rawMarkdownRoute
    ? documents
        .filter(({ html }) => extractPageSignals(html).markdownAlternate)
        .map(({ pathname }) => markdownPathForRoute(pathname))
        .filter((pathname) => pathname !== undefined)
    : [];
  return [...EDGE_RUNTIME_PATHS, ...markdownPaths];
}

/** Extract the signals the audit can verify without a browser or paid API. */
export function extractPageSignals(html) {
  const titleMatch = html.match(/<title\b[^>]*>([\s\S]*?)<\/title>/i);
  const descriptionTag = tags(html, "meta").find(
    (tag) => attribute(tag, "name")?.toLowerCase() === "description",
  );
  const canonicalTag = tags(html, "link").find(
    (tag) => attribute(tag, "rel")?.toLowerCase() === "canonical",
  );
  const markdownTag = tags(html, "link").find(
    (tag) =>
      attribute(tag, "rel")?.toLowerCase().split(/\s+/).includes("alternate") &&
      attribute(tag, "type")?.toLowerCase() === "text/markdown",
  );
  const structuredData = [];
  const structuredDataErrors = [];
  for (const match of html.matchAll(
    /<script\b[^>]*type=["']application\/ld\+json["'][^>]*>([\s\S]*?)<\/script>/gi,
  )) {
    try {
      structuredData.push(JSON.parse(match[1]));
    } catch {
      structuredDataErrors.push("invalid JSON-LD");
    }
  }

  const structuredTypes = new Set();
  for (const value of structuredData) {
    const graph = Array.isArray(value?.["@graph"]) ? value["@graph"] : [value];
    for (const item of graph) {
      const types = Array.isArray(item?.["@type"])
        ? item["@type"]
        : [item?.["@type"]];
      for (const type of types) {
        if (typeof type === "string") {
          structuredTypes.add(type);
        }
      }
    }
  }

  const links = [];
  for (const match of html.matchAll(/<a\b[^>]*\bhref=["']([^"']+)["']/gi)) {
    const href = decodeHtml(match[1]);
    if (href.startsWith("/")) {
      links.push(normalisePath(href.split(/[?#]/, 1)[0]));
    }
  }

  return {
    h1Count: tags(html, "h1").length,
    title:
      titleMatch === null ? "" : decodeHtml(titleMatch[1]).replace(/[<>]/g, ""),
    description:
      descriptionTag === undefined
        ? ""
        : (attribute(descriptionTag, "content") ?? ""),
    canonical:
      canonicalTag === undefined ? undefined : attribute(canonicalTag, "href"),
    markdownAlternate:
      markdownTag === undefined ? undefined : attribute(markdownTag, "href"),
    structuredTypes,
    structuredDataErrors,
    startsWithParagraph:
      /<div\b[^>]*class=["'][^"']*blog-prose[^"']*["'][^>]*>\s*<p\b/i.test(
        html,
      ),
    links,
  };
}

function issue(severity, path, code, message) {
  return { severity, path, code, message };
}

function emittedPaths(files) {
  return new Set(
    [...files].map((file) =>
      file.endsWith(".html") ? routeFromClientFile(file) : `/${file}`,
    ),
  );
}

function localPath(value, siteOrigin) {
  try {
    const url = new URL(value, siteOrigin);
    const origin = new URL(siteOrigin).origin;
    return url.origin === origin ? normalisePath(url.pathname) : undefined;
  } catch {
    return undefined;
  }
}

/** Audit built documents and shared discovery files. */
export function auditBuiltSite({
  documents,
  files,
  runtimePaths = [],
  siteOrigin = "https://kivgraph.dev",
}) {
  const issues = [];
  const knownRoutes = new Set(
    documents.map(({ pathname }) => normalisePath(pathname)),
  );
  const availablePaths = new Set([
    ...emittedPaths(files),
    ...runtimePaths.map((pathname) => normalisePath(pathname)),
  ]);
  const inbound = new Map([...knownRoutes].map((pathname) => [pathname, 0]));

  for (const required of REQUIRED_FILES) {
    if (!files.has(required)) {
      issues.push(
        issue("error", "/", "missing-discovery-file", `Missing ${required}`),
      );
    }
  }

  for (const { pathname, html } of documents) {
    const path = normalisePath(pathname);
    const signals = extractPageSignals(html);
    if (signals.h1Count !== 1) {
      issues.push(
        issue(
          "error",
          path,
          "h1-count",
          `Expected exactly one H1, found ${signals.h1Count}`,
        ),
      );
    }
    if (signals.title.length === 0) {
      issues.push(issue("error", path, "missing-title", "Missing title"));
    }
    if (signals.description.length === 0) {
      issues.push(
        issue("error", path, "missing-description", "Missing meta description"),
      );
    }
    if (signals.canonical === undefined) {
      issues.push(
        issue("error", path, "missing-canonical", "Missing canonical URL"),
      );
    } else {
      try {
        const canonicalUrl = new URL(signals.canonical);
        const expectedOrigin = new URL(siteOrigin).origin;
        const canonicalPath = normalisePath(canonicalUrl.pathname);
        if (canonicalUrl.origin !== expectedOrigin) {
          issues.push(
            issue(
              "error",
              path,
              "canonical-origin",
              `Canonical uses ${canonicalUrl.origin}; expected ${expectedOrigin}`,
            ),
          );
        }
        if (canonicalPath !== path) {
          issues.push(
            issue(
              "error",
              path,
              "canonical-mismatch",
              `Canonical points to ${canonicalPath}`,
            ),
          );
        }
      } catch {
        issues.push(
          issue(
            "error",
            path,
            "invalid-canonical",
            "Canonical is not an absolute URL",
          ),
        );
      }
    }
    if (signals.structuredDataErrors.length > 0) {
      issues.push(
        issue("error", path, "invalid-json-ld", "Invalid JSON-LD block"),
      );
    }

    if (signals.markdownAlternate !== undefined) {
      const alternatePath = localPath(signals.markdownAlternate, siteOrigin);
      const expectedPath = markdownPathForRoute(path);
      if (alternatePath === undefined) {
        issues.push(
          issue(
            "error",
            path,
            "invalid-markdown-alternate",
            "Markdown alternate is not a local URL",
          ),
        );
      } else if (expectedPath !== alternatePath) {
        issues.push(
          issue(
            "error",
            path,
            "markdown-alternate-mismatch",
            `Markdown alternate points to ${alternatePath}; expected ${expectedPath}`,
          ),
        );
      } else if (!availablePaths.has(alternatePath)) {
        issues.push(
          issue(
            "error",
            path,
            "missing-markdown-target",
            `Markdown alternate target ${alternatePath} is not emitted`,
          ),
        );
      }
    }

    if (path.startsWith("/blog/") && path !== "/blog/") {
      if (!signals.structuredTypes.has("BlogPosting")) {
        issues.push(
          issue(
            "error",
            path,
            "missing-blogposting",
            "Missing BlogPosting JSON-LD",
          ),
        );
      }
      if (!signals.structuredTypes.has("BreadcrumbList")) {
        issues.push(
          issue(
            "error",
            path,
            "missing-breadcrumblist",
            "Missing BreadcrumbList JSON-LD",
          ),
        );
      }
      if (signals.markdownAlternate === undefined) {
        issues.push(
          issue(
            "error",
            path,
            "missing-markdown-alternate",
            "Missing text/markdown alternate",
          ),
        );
      }
      if (!signals.startsWithParagraph) {
        issues.push(
          issue(
            "warning",
            path,
            "missing-direct-answer",
            "The article does not start with a plain-text paragraph",
          ),
        );
      }
      if (signals.title.length < 45 || signals.title.length > 60) {
        issues.push(
          issue(
            "warning",
            path,
            "title-length",
            `Title has ${signals.title.length} characters; target is 45–60`,
          ),
        );
      }
      if (
        signals.description.length < 120 ||
        signals.description.length > 160
      ) {
        issues.push(
          issue(
            "warning",
            path,
            "description-length",
            `Description has ${signals.description.length} characters; target is 120–160`,
          ),
        );
      }
    }

    for (const link of signals.links) {
      if (!knownRoutes.has(link) && !availablePaths.has(link)) {
        issues.push(
          issue(
            "error",
            path,
            "broken-internal-link",
            `Local link target ${link} is not emitted`,
          ),
        );
        continue;
      }
      if (knownRoutes.has(link)) {
        inbound.set(link, inbound.get(link) + 1);
      }
    }
  }

  for (const [path, count] of inbound) {
    if (path !== "/" && count === 0) {
      issues.push(
        issue(
          "error",
          path,
          "orphan-page",
          "No published HTML page links to this route",
        ),
      );
    }
  }

  return issues;
}

export { REQUIRED_FILES };
