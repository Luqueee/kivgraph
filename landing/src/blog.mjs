/** Return only blog entries that are safe to publish. */
export function isPublishedBlog(entry) {
  return entry.data.draft !== true;
}

/** Sort entries newest first without mutating the content collection result. */
export function sortBlogEntries(entries) {
  return [...entries].sort(
    (a, b) => b.data.pubDate.valueOf() - a.data.pubDate.valueOf(),
  );
}

/** The canonical HTML path for a blog entry. */
export function blogPathname(id) {
  return `/blog/${id}/`;
}

/** The Markdown twin advertised to agents and feed readers. */
export function blogRawPathname(id) {
  return `/raw/blog/${id}.md`;
}
