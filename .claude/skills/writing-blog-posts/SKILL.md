---
name: writing-blog-posts
description: "Create or improve English Kivgraph blog posts with search-intent research, SEO/AEO metadata, authoritative sources and verified landing builds."
---

# Writing Blog Posts

Use this skill when creating or substantially editing an article under
`landing/src/content/blog/`, or when improving the Kivgraph blog's search
visibility. It does not apply to product documentation, release notes or
generic copy outside the landing blog.

## Goal

Produce a useful article that answers one search intent clearly, earns a click
with an accurate title and description, and gives both search crawlers and AI
answer engines enough structure and evidence to quote it safely.

## Before writing

- Read `landing/AGENTS.md` and inspect the existing blog components, content
  schema and related articles. Reuse the current route and metadata system.
- Identify one primary question or task and the supporting questions that
  naturally follow it. Use current SERP research when the user asks for search
  intent, competition or recommendations. SERP patterns are evidence of
  intent, not monthly search volume; never invent volume or difficulty numbers.
- Choose an angle Kivgraph can support with product behavior, documentation,
  benchmarks or an authoritative external source. Do not pad a post with
  generic AI or SEO claims.

## Article contract

Every published article is English and uses the existing collection frontmatter:

```yaml
title: A clear search-facing title
description: A direct 120–160 character answer or promise
pubDate: 2026-08-31
author: Kivgraph
category: One useful topic label
tags:
  - primary topic
featured: false
```

Aim for a natural title of roughly 45–60 characters. Put the primary topic in
the title when it reads naturally; avoid repeated brand suffixes, keyword
lists and clickbait. The `description` is also shown immediately below the
generated H1, so it must answer or frame the query directly rather than merely
describe the company.

The first one or two sentences of the body must answer the primary question in
plain text. State the definition, command, decision or result before the
background. Do not make the answer depend on bold text, a graphic, a code block
or a link being opened.

Use question-style H2s when they match real follow-up searches. Keep paragraphs
short, use ordered lists for procedures, and use fenced code blocks for exact
commands. Do not add another H1: the blog route renders the article title.

Link deliberately:

- Add relevant internal links to the closest Kivgraph guide, tool reference,
  quickstart or related article.
- Add one or two authoritative external sources when the post makes protocol,
  platform, language or tool claims. Verify unfamiliar URLs before publishing.
- Do not add links solely to satisfy a count or link to competitors without a
  clear reader benefit.

## FAQs and structured data

The blog route emits `BlogPosting` and breadcrumbs automatically. When an
article contains a visible FAQ section, declare the exact same questions and
plain-text answers in the optional `faq` frontmatter field:

```yaml
faq:
  - question: Does this change the indexed code?
    answer: No. Registration and indexing are separate operations.
```

The route turns that field into `FAQPage` JSON-LD. Keep the frontmatter answers
aligned with the visible Markdown answers; never publish FAQ schema for
questions that the page does not visibly answer. Do not hand-write canonical,
Open Graph, sitemap, RSS or `llms.txt` markup in an article.

## Verification

From `landing/`, run:

```bash
pnpm check
pnpm test
pnpm build
```

Also run `git diff --check`. Confirm the generated article has one H1, a
120–160 character description, a canonical URL, `BlogPosting` JSON-LD, valid
FAQ JSON-LD when applicable, internal links and a raw Markdown alternate. If a
preview server is available, request the article as HTML and check it returns
`200`; preview canonical URLs should still point at the production origin.

BabyLoveGrowth's public readability checker can be used as a spot check, one
URL at a time. Its public endpoint is rate-limited per IP and its H1/source
checks can disagree with the actual HTML, so treat it as feedback rather than
the sole acceptance gate. Inspect the page source and the build output before
changing correct markup to satisfy a false positive.

Do not edit generated directories such as `landing/dist` or `landing/.astro`.
