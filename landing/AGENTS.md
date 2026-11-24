# Landing and documentation instructions (`landing/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

It enters no published bundle; the payload's allowlist lives in
`scripts/build-bundle.sh`.

## Scope and project

- `landing/` is the public landing and the user documentation, in Astro with
  Starlight, and it is its own pnpm project like `ts-worker/` and `web/`. It
  enters no bundle: the payload of `scripts/build-bundle.sh` is an allowlist and
  the release workflow checks that neither the directory nor a line of
  `SHA256SUMS` names it. It is verified with `make landing-check` and `make
  landing-build`, and served with `pm2 start landing/ecosystem.config.cjs` on
  port `6767`.
- The landing is not a Starlight page: its shell is
  `landing/src/components/landing/Layout.astro` and its components live in that
  same directory; only the documentation carries Starlight's chrome
  -sidebar, Pagefind, table of contents-. It is dark by construction, like the
  viewer: the overrides in `landing/src/components/starlight/` set
  `data-theme="dark"` and mount no selector, because Starlight's light palette
  lives entirely under `[data-theme='light']` and it is enough never to select
  it.

## Layers, palette and rhythm

- `global.css` imports the Tailwind utilities inside the `utilities` layer and
  does not import preflight, so a page outside Starlight has no reset and **a
  component rule with no layer beats any utility**, whatever its specificity. The
  landing's reset is therefore an `@layer base` with `:global()` inside the
  shell: without the layer, a rule like `p { margin: 0 }` silently overrode every
  `mt-*` on the page. A grid that must be able to shrink also declares its base
  column -`grid-cols-1`-, because the implicit column is sized by the widest
  child and a `<pre>` overflowed the viewport.
- The corollary has its own measured victim: a `margin: 0` put in a component's
  `<style>` to reset the `<fieldset>` of the home page's tabs ate the `mt-10`
  that separated it from its own text. A margin reset **may only** live in
  `Layout.astro`'s `@layer base`; what stays with the component is what no
  utility covers, and in that case it is `min-width: 0`, because the UA gives
  the `<fieldset>` a `min-width: min-content` that breaks any grid it enters.
- The two halves of the site wear the same skin and **a single palette
  declaration**: the tokens live in `global.css`'s `@theme` and nobody writes a
  colour literal anywhere else. The surfaces are `--color-shell`,
  `--color-panel`, `--color-raise`, `--color-rule` and `--color-rule-strong`, and
  their names avoid a `t-` prefix on purpose: `border-t-<color>` is Tailwind's
  `border-top-color` utility, so a `--color-t-line` would make `border-t-line`
  mean two things. Prose is `--color-gray-300`, labels `--color-gray-400`, and
  nothing dimmer than that carries text. It is not a preference:
  `--color-gray-500` over `--color-panel` measures `3.96:1` and AA asks for
  `4.5:1` for normal text, so a caption in `gray-500` is a contrast failure. The
  home page had 34 when first measured, all of that same pair; with the labels
  in `gray-400` the count is `0` on the home page, on `/comparison/`, on
  `/install/` and on `/kivgraph-faq/`.
- And **a single typography rule**, for the same reason. `docs.css` already
  stated it for its half: mono for what is a headline, a label, an identifier, a
  command, a table header or a figure; sans for prose. The landing carried
  `font-mono` on `<body>`, so it dragged every paragraph into the terminal face,
  left unused the Geist Sans the page downloads anyway, and made the `h1` break
  into three lines. Now `<body>` is `font-sans` and `font-mono` is set where it
  means something. A new component that writes prose declares no font; one that
  writes a label or a readout declares `font-mono`.
- The home page's headline is the case that rule forces you to measure: a
  monospaced face sets much wider than a proportional one at the same size, and
  the 52 letters of the sentence only fit on two lines when the column exceeds
  `52rem`. Hence `max-w-4xl` instead of prose's `max-w-3xl`, and a body that
  steps twice -- `text-3xl sm:text-4xl lg:text-5xl`-- because at the `720px` a
  tablet leaves, 48px split it into three again with `it.` alone on the last.
  Measured at `1728`, `1440`, `1024`, `768` and `390`: two lines in the first
  four, three with no orphan in the last, and `0` horizontal overflow in all.
- `landing/src/styles/docs.css` is Starlight's skin and **only** enters through
  `customCss` after `global.css`: the landing does not load it. It reassigns the
  `--sl-*` properties -- all of which exist in
  `node_modules/@astrojs/starlight/style/props.css`, a name is never invented --
  and Expressive Code's `--ec-*` to those tokens. Every radius is `0` and there
  are no shadows.
- The heading hierarchy is the same in both halves and is carried by a monospaced
  marker, not by size: the landing puts a dim index (`01`, `#`) in front of a
  `--color-gray-100` title, and the docs put `##` on `h2` and `#` on `h3`. A
  section `h2` outranks every subtitle. What is a label -- a column header, a
  figure caption, the key of a row -- stays in `--color-gray-400` and **outside**
  the hierarchy: the footer's column labels were `h2` with subtitle styling and
  now the footer contributes no heading.
- The rule over a documentation `h2` goes on `.sl-heading-wrapper`, not on the
  `h2`: Starlight leaves it `display: inline` so the anchor fits at the end of
  the last line, so a border on the `h2` itself would hug the words instead of
  crossing the column. Measuring `main h2` returns `0px` and does not mean the
  rule is dead.
- The landing's vertical rhythm is declared in `Section.astro` and applied
  without exceptions: `py-14` per band, `mt-10` from header to content and
  between blocks, `mt-5` from subtitle to its block, `mt-3` from block to its
  note. Two contiguous bands end up at `7rem` and two blocks of one band at
  `5rem`, which is what keeps them from reading as one.
- A row of leader dots (`LeaderRow.astro`) cannot overlap its neighbour: key and
  value are `min-w-0` and the row wraps, because with both ends `shrink-0` the
  row's minimum width exceeded its grid track and
  `snapshot_built_at 2026-08-15T11:14:10Z` landed on top of `schema_version`.
  The reading grids use `auto-fit` with `minmax(min(20rem,100%),1fr)`, so a
  column is only added when it fits whole.

## SEO and agent readers

- The site's SEO is written for two readers and the second is an agent. What
  exists for it: `/llms.txt` with the index and `/llms-full.txt` with all the
  documentation in a single request, plus each page's markdown at
  `/raw/<path>.md`, which is what its `<link rel="alternate">` announces. All
  three are generated with `getCollection`, never from a hand-written list: a
  new page cannot leave them stale.
- The blog is the independent `blog` collection, under `src/content/blog/`, and
  its routes are `/blog/` and `/blog/<slug>/`. Only entries without
  `draft: true` are published: the index, `/rss.xml`, `/llms-blog.txt`, the
  sitemap and the Markdown twins must come out of that same collection and not
  from duplicated lists.
- A blog entry carries a `BlogPosting` whose author, dates, title and description
  match what is visible on the page. The template publishes the link to
  `/raw/blog/<slug>.md` for agents and the feed links the canonical HTML URL. New
  editorial content is in English, like the rest of the site's public material.
- Every published page is reached from an internal link, not only from the
  sitemap. A sitemap is discovery, not a recommendation: a URL that only lives
  there is what Search Console reports as "discovered, currently not indexed" and
  never gets crawled. Measured over `dist/client`: the 37 sitemap URLs and the
  internal links pointing at each. Each documentation page gets 37 or more from
  Starlight's sidebar; `/releases/` got `0`, because it is not in the sidebar and
  the landing's footer did not name it. That is why it is in the footer's `start`
  group. When adding a page, count its incoming links before closing.
- The auditor distinguishes what Astro emits from the routes another component
  serves on the same origin. Those routes are passed as `runtimePaths`; `/github`
  belongs to the redirect Worker and that is why it is there. The exception is an
  exact route, not a prefix: another broken local link must still close the gate.
- The documentation namespace was renamed `reference` -> `docs` and the redirect
  in `astro.config.mjs` points at the form **with** a trailing slash, which is the
  only canonical one on the site. Without it the `301` landed on `/docs/<path>`
  -- which answers `200` and declares a canonical different from its own--, one
  more hop than a crawler needs. Verified: `num_redirects=1` and the final URL is
  the canonical.
- `trailingSlash: "always"`. Astro's default is `ignore`, which served both
  forms: `/docs/cli` and `/docs/cli/` answered `200` with the same HTML and the
  same canonical, that is, a second URL per page that a crawler finds and
  discards -- 37 of them. Astro warns that on prerendered pages the trailing
  slash is the host's business and may ignore the configuration, so it was
  measured instead of assumed: with the `node` adapter in `standalone` it **does**
  respect it and the form without the slash returns `301` to the canonical. It was
  also checked that it does not reach what carries an extension: `/llms.txt`,
  `/robots.txt`, `/sitemap-0.xml`, `/raw/<path>.md` and `/og.png` stay at `200`.
- A `BreadcrumbList` `ListItem` carries `item` **always except on the last**, and
  that is Google's rule, not schema.org's, which marks it optional. Both things
  are true at once and that is why the error was easy: this site emitted one link
  per URL segment and left the intermediate ones without `item` -- `Docs`,
  `Tools`, `MCP server`, `Guides`--, which are not pages: the four routes answer
  `404`. Markup valid for schema.org and invalid for Google, which is the
  difference between parsing and appearing. Only the addressable links are
  emitted, which is also what the guide asks for -- "breadcrumbs that represent
  a typical user path to a page, instead of mirroring the URL structure"-- and
  meets the minimum of two.
  https://developers.google.com/search/docs/appearance/structured-data/breadcrumb
- The sitemap carries `lastmod` and it comes from **git**, not from the build's
  `mtime`: the build date is the build's, and it would mark all 37 URLs as changed
  on every deploy. `src/lastmod.mjs` does **one** pass of
  `git log --format=%cI --name-only --no-merges -- landing/src` -- 37 pages touch
  well over 37 files-- and `--no-merges` is there because a merge reports its
  whole diff and would stamp half a branch with its own date.
- Google uses `lastmod` to schedule the recrawl and **discards the field across
  the whole sitemap when it does not believe it**, so the only two acceptable
  answers are an exact date and none. That is why every failure path returns
  `undefined`: without `git`, outside a checkout, in a **shallow** clone -- where
  every file collapses onto the tip commit-- or for a URL whose source file the
  module does not know how to name. Measured in a test clone: `--depth 1` gives
  `undefined`, and the same code after `git fetch --unshallow` gives
  `2026-08-23T13:49:53+02:00`.
- The shallow case is not hypothetical: `ci.yml` cloned at the default depth, so
  its landing build would have passed with a sitemap without a single date --
  verifying something that is not what is served. That is why the `verify` job
  carries `fetch-depth: 0`; `release.yml` already did.
- Two pages are not a collection entry and are dated by hand: `/`, which has no
  content of its own and takes the most recent of the components it **imports** --
  reading the imports and not the directory keeps a component the page no longer
  uses from still dating it--, and `/releases/`, which is a page that renders a
  collection and takes both things. The style sheets and the motion layer are left
  out on purpose: they change how the page looks, not what it says, and `lastmod`
  answers the second question.
- `robots.txt` explicitly allows the AI crawlers and **every token comes from its
  operator's documentation**, with the URL and the date printed in the file
  itself. A token that cannot be verified does not enter. Three of them
  -`ChatGPT-User`, `Perplexity-User`, `Meta-ExternalFetcher`- are published by
  their operators with a warning that they may ignore `robots.txt`: they are named
  because an ignored permission costs nothing, and the comment says so instead of
  suggesting a control that may not bind.
- Every absolute URL is derived from `Astro.site`, never from a literal: the
  deployment sets `KIVGRAPH_LANDING_URL` and a hand-written host would publish
  another machine's canonical. The project's facts -name, tagline, repository,
  licence, the tool list- live once in `landing/src/pages/_seo.ts`, which the
  landing's shell, the `Head` override and the three endpoints consume.
- The deployment environment is declared in `landing/.env`, and
  `astro.config.mjs` reads it with `process.loadEnvFile()` before Vite looks: the
  config is **not** a module Vite transforms, so `import.meta.env` is empty there
  and a bare `process.env` only sees what the shell exported. The fallback value
  of `site` is the production origin, `https://kivgraph.dev`, and the direction
  matters: `site` is baked at build time and CI builds without `.env`, so the
  fallback has to be the correct answer. What is pinned in the file is the
  development case, `KIVGRAPH_LANDING_URL=http://localhost:6767`. The other way
  round -- fallback `localhost`, variable in the deployment-- is what published
  `http://localhost:6767` as the canonical and in the sitemap of every page. An
  already exported variable wins over the file -measured-, and `.env` is in
  `.gitignore`.
- **The origin is the `kivgraph.dev` apex.** It is the form
  `internal/supervisor/supervisor_linux.go` already wrote in the `Documentation=`
  line of every systemd unit it generates, so the site was publishing one host's
  canonical and the daemon documenting another. `www` is not a second origin: the
  host redirects it instead of serving it, because two names answering the same
  HTML are the duplicate every canonical on this site exists to avoid.
- The previous origin, `https://kivgraph.luqueee.dev`, has to keep answering with
  a permanent `301` to the equivalent path on the new one -- path by path, not
  everything to the root, which Google treats as a soft 404 and transfers no
  signals. That lives on the host, not in this repository: `ecosystem.config.cjs`
  starts a Node process and TLS and the reverse proxy are the business of whoever
  serves it. Withdrawing the old host's DNS instead of redirecting it throws away
  the indexing history the 37 URLs already had.
- **Search Console is verified by DNS, not by the HTML.** The site emitted a
  `google-site-verification` in the landing's shell, which is the HTML-tag method
  of a *URL-prefix* property -- and a URL-prefix property is an origin: the token
  minted for `https://kivgraph.luqueee.dev/` verified nothing at
  `https://kivgraph.dev/`, and every move asked for a new literal in `_seo.ts`. A
  domain property with a `TXT` record covers the apex, `www` and every subdomain
  at once, and does not depend on the HTML this repository serves, so a page that
  stops emitting a tag cannot unverify the property.
- The identity facts -- name, tagline, summary, repository, licence, the social
  card's alt -- live in `landing/src/site.mjs`, bare ESM with not a single
  `astro:*` `import`, because `astro.config.mjs` has to be able to import it.
  `_seo.ts` re-exports them, so it is still the module every component and
  endpoint imports from. The only hand-kept copy is
  `landing/public/site.webmanifest`, which is static JSON.
- The analytics are a self-hosted Umami and **the tracker comes from that pair of
  variables or it does not come out**: `KIVGRAPH_UMAMI_SCRIPT_URL` and
  `KIVGRAPH_UMAMI_WEBSITE_ID`, which `umamiTracker()` reads in `_seo.ts`. With only
  one of the two nothing is emitted, so `astro dev`, a local build and CI cannot
  write to the production dataset. The `id` is a UUID that the instance mints at
  run time, not a fact of the repository, and therefore lives in `_seo.ts` like the
  rest. The `<script>` goes `is:inline` -- the file is served by the instance, not
  by this bundle -- and **both** halves of the site emit it, the 404 included, so
  that a visitor who goes from the landing to the documentation remains one
  session.
- The tracker carries `data-performance="true"` and `data-domains`, and both are
  for SEO and not for counting visits. The first switches on the field Core Web
  Vitals -- LCP, INP, CLS, FCP, TTFB -- which is what makes it possible to ask
  which page ranks **and** is slow. The second is the guard the pair of variables
  cannot be: it comes from `PRODUCTION_HOST` in `site.mjs` and **not** from the
  host of `Astro.site`, because a staging build sets `site` to the staging origin
  and following it would let it match itself. The pair fails closed when a
  variable is missing; a preview that inherits **both** is exactly the case it
  does not catch.
- `data-exclude-search` is not set and must not be: that is where `utm_*`,
  `gclid`, `fbclid` and `msclkid` travel, and excluding them leaves attribution
  blank.
- Events come out of `CopyButton.astro` -- which declares them `event` and
  `eventData`, optional, and reports **in the `then` of `writeText`**, so a
  rejected copy does not count -- and of `data-umami-event` on links that leave
  the site, which Umami reads from the click with no JavaScript of ours. An
  internal link carries **no** event: it is already a pageview, and counting it
  twice puts in the report a number no visit produced. And **leaving the site is
  not enough either**: the event is written by each row of `TopBar.astro`, not
  derived from the `href`, because since the bar also links Glama's listing there
  are two external destinations and they are not the same click. `github_click`
  means a click to the repository; sharing the name would leave one number meaning
  two things with no way back, so the listing goes with no event until it deserves
  one of its own. The vocabulary, the goals and the UTM convention live in
  `docs/development/analytics.md`; a new event is added there before it is added
  in code.
- **Nobody calls a provider: `track()` is called.** The layer is in
  `src/lib/analytics/`, the event catalogue is typed and a name that is not in it
  does not compile. Umami and PostHog answer different questions -- acquisition
  and behaviour -- and their figures will not add up; expecting them to is an
  error of method and not a failure.
- **There is no `astro:page-load` and there must not be while there is no
  `<ClientRouter />`.** This site navigates with full loads, so PostHog's
  automatic capture gives one `$pageview` per page; adding a listener on top gives
  two. Measured: one on load, one on navigation, one on `back`, one on `reload`.
  If `ClientRouter` ever comes in, that measurement has to be redone.
- PostHog is imported **dynamically after `load`** and without
  `PUBLIC_POSTHOG_KEY` it **disappears from the bundle** by tree-shaking. With a
  key it is `+82.8 KB` gzip over the site's `72.1`, which is more than all the
  rest of its JavaScript together: it is not free, it is off the critical path,
  and there is no lighter build of the package.
- Verifying PostHog headless requires hiding `navigator.webdriver`: its bot
  filter drops those events **without a single error**, and the `/e/` payload is
  gzip. It is recorded in `analytics.md` because it costs an afternoon to
  discover.
- **What pm2 starts is `server.mjs`, not the adapter's entry**, and the reason is
  measurable: an Astro middleware sees **one** of seven routes -- only the
  non-prerendered one-- because the static handler answers the others before the
  SSR pipeline exists. `server.mjs` imports the `handler` the adapter itself
  exports and wraps it, so statics, the trailing-slash `301` and the 404 come out
  of the same place as always; the only thing it adds is seeing every request.
  The AI-agent detector lives there.
- **`server.mjs` does not receive the first-run telemetry.** The exact public
  route `POST /api/telemetry/first-run` belongs to the edge Worker that also serves
  `/install.sh`, `/install.ps1` and `/github`; it validates and aggregates the
  facts in D1 for the internal dashboard. The module `src/install-report.mjs` keeps
  the historical implementation and its tests, but it is neither imported from the
  server nor a production route. Wiring it back would create two owners and two
  datasets with no join, the failure ADR 0092 documents.
- The AI crawlers go to a **second Umami property** and never to the main one: a
  bot cannot move visitors, bounce, duration or a conversion rate that describes
  people. Umami's bot filter stays on in both, and that is why the sender
  identifies itself as `kivgraph-landing/1.0` instead of faking the header. The
  agent registry is `src/ai-agents.mjs`, each row with its official source and its
  date, and the rest is in `docs/development/analytics.md`.
- A detected request emits **two** events and both go on the same side of the
  deduplication: `ai_crawler_request` with all the fields, and
  `ai_crawler_<agent>` so that the dashboard's *Events* chart can separate by
  agent -- one series per event name is all it knows how to draw, and it does not
  filter by a property. They are built by `crawlerEventPayloads()` in
  `src/ai-report.mjs` and not by `server.mjs`, which cannot be imported without a
  built `dist/`: what is written in there is code no test reaches. The event name
  comes from the registry row and **never** from the request, so an invented
  `User-Agent` does not mint a new event.
- The site's JSON-LD is **one** graph: the landing publishes `SoftwareApplication`
  at `<site>/#software` and `WebSite` at `<site>/#website`, and each documentation
  page emits a `TechArticle` that references them by `@id` instead of duplicating
  them. The `BreadcrumbList` is a sibling node, not a property of the article:
  schema.org puts `breadcrumb` only on `WebPage`. `Faq.astro`'s `FAQPage` is a
  second `<script>` -- being a different page type requires it -- but it is not an
  orphan: it carries `@id` at `<site>/#faq` and points to the other two through
  `isPartOf` and `about`.
- The `Head` override adds only what Starlight does not emit. Starlight already
  writes the title, the description, the canonical and `twitter:card`, so
  repeating any of the four is a defect, not an innocent redundancy. What it does
  **not** emit and therefore goes there: the preview image with its dimensions,
  its type and its `alt`. The `alt` describes the image, does not repeat the
  tagline already in the `og:description` next to it, and comes from `PREVIEW_ALT`
  so that the two halves do not describe the same card in two ways.
- The `<link rel="alternate" type="text/markdown">` is conditioned on the page
  being a member of the `docs` collection. `/releases/` is a `StarlightPage`, so
  it inherits the override with the synthetic id `releases`, and `raw/[...slug]`
  only serves routes of the collection: announcing it unconditionally published a
  `/raw/releases.md` that **returned 500**, not 404 -- what this file said before
  was an assumption and the measurement refutes it. And the cause was not the
  announcement: `raw/[...slug]` was prerendered under `output: "server"`, so the
  router still **matched** any `/raw/**.md` against a route with no component
  instance for the paths `getStaticPaths` never emitted, and Astro threw inside
  its own pipeline before any handler of ours could answer. An `if` inside the
  `GET` there is dead code: checked. That is why the route is `prerender = false`
  and resolves the entry at request time -- a collection read and a `join`--,
  which is what lets it return `404` with `text/plain` for what it does not
  publish and `text/markdown` for what it does. Verified: `/raw/releases.md`,
  `/raw/nonsense.md` and an invented nested slug give `404`; the site's 37
  `alternate` and the 73 links of `llms.txt` and `llms-full.txt` give `200`.

## Build warnings

- The build admits no warnings. `scripts/build.mjs` keeps all the output and
  fails if Astro prints one, even though the command itself ends at zero. The 404
  route is the case that closed that gate: Starlight injected an explicit route
  and its catch-all also claimed the collection's `404` entry. Now
  `disable404Route` removes the injected one, `src/pages/404.astro` is the only
  error route and keeps the previous status, copy, chrome and `noindex`.
- The other two there were got fixed, and one deserves to be written down because
  it will bite again. Astro decides whether to warn that a dynamic route ignores
  its static-paths list **by searching for that function's name as a substring in
  the file's text** -- `vite-plugin-routes`, not a reading of the exports--, so
  mentioning it in a comment is enough to trigger it. It happened to
  `src/pages/raw/[...slug].md.ts`, which exports none, and the advice printed
  -- add `prerender = true`-- is exactly the `500` defect that file documents. The
  comment now names it with a detour, on purpose.
- The markdown pipeline is declared with an explicit `processor`, `unified({...})`
  from `@astrojs/markdown-remark`, which is therefore a direct dependency of
  `landing/`. The old form -- bare `markdown.rehypePlugins`-- still works because
  Astro hands it over to the configured processor, but it is deprecated and warns
  on every build. Naming it is also what keeps Starlight standing: it does **not
  set** the processor, it **mutates** it, pushing its asides, its heading anchors
  and its RTL code support into `remarkPlugins` and `rehypePlugins`, and it only
  recognizes two engines. One it does not recognize is not an error: it warns and
  silently drops those transformations. Measured before and after the migration
  over `dist/client`: `88` external anchors all with `target` and `noopener`, `280`
  `sl-anchor-link`, `280` `sl-heading-wrapper` and `175` Expressive Code blocks,
  identical.

## Icons

- The icons are committed assets, not a build step: `landing/scripts/icons.sh`
  generates them from a square image with ImageMagick or, failing that, `sips`,
  and fails closed with neither. CI runs on Linux and never runs it. The maskable
  keeps the mark inside the central 80 % because the launcher crops to a circle.
- `og.png` does **not** come from `icons.sh`. The social card carries the
  wordmark, the headline and the tagline composed in the Geist the site serves,
  and composing typography is what that script cannot do. The source is
  `landing/scripts/social-card.html`: it is opened in a browser, the `#card`
  element is captured -- which measures exactly 1200x630, so no viewport scales
  it-- and the PNG is committed. Its `@font-face` point at `node_modules`, which is
  where the site's exact fonts are. The background mesh is pushed to the right on
  purpose: the mark is an opaque raster and on top of the pattern it reads as a
  square cut out of it.
- The canvas of a filled icon is `--bg`, and it has to be **the background colour
  of the source image itself**, not the page's: the current mark brings `#0f1117`
  and filling with `#0a0b0d` left a visible box inside `og.png` and the maskable.
  That is why the manifest's `background_color` is `#0f1117` -- it is the field the
  launcher paints behind the icon -- while `theme_color` stays the site's
  `#0a0b0d`.
- `--zoom` crops the source to its centre before scaling, because an app icon
  wants the mark at ~80 % of the canvas and this one took the measured 60.9 %: at
  16 px that is ten pixels of mark in a tab. It is measured, not estimated.
- There is no vector mark: the source is a raster, and `favicon.svg` is that same
  raster -- the 32 px PNG in base64-- wrapped in a `clipPath` with rounded
  corners, `rx=7` on a canvas of 32. It is the only icon with a radius: the PNGs
  come out of `icons.sh` square and stay as a fallback for a client that does not
  accept an SVG icon. **Both** halves of the site link it -- Starlight through the
  `favicon` option of `astro.config.mjs` and the landing through its own
  `<link rel="icon" type="image/svg+xml">`--, because the browser prefers the
  vector and having it in one half only is what left the documentation's tab
  rounded and the home page's with square corners.
- That same "the vector wins" is what makes a stale `favicon.svg` dangerous: when
  it wrapped the old mark, the old mark was what showed in the tab even though the
  PNGs were up to date. When the icons are regenerated, the base64 this file
  carries inside is replaced too; `icons.sh --drop-svg` removes it, and then
  Starlight's `favicon` points at the 32 px PNG, which it emits as
  `rel="shortcut icon"`, and the landing loses the radius.

## Content

- The documentation in `landing/` is written for whoever uses Kivgraph and comes
  out of the repository's sources -`cmd/kivgraph/help.go`, `internal/config`,
  `internal/mcp/tools`, `README.md`-, copied literally when they are a contract.
  `docs/` remains internal engineering material -ADRs, qualification reports- and
  is not published.
- **The tool count comes from the contract, not from memory.** The live list is
  `allowedTools` in `internal/mcp/surface_test.go`, which that file declares a
  contract and not a snapshot, plus the three indexing controls that are only
  registered on the configured `serve` path. `MCP_TOOLS` in `_seo.ts` has to match
  it: the *Tool reference* group of `llms.txt` and `llms-full.txt` comes from
  there. `find_by_intent` arrived in v0.8.0 and must stay first because it is the
  entry door when the visitor does not know a name. Keeping the landing in sync
  touches five places: the page, the `astro.config.mjs` sidebar, `MCP_TOOLS`,
  `docs/mcp-tools.md` and the count in prose in this file. The runtime contract is
  also checked in `internal/mcp/surface_test.go` and `internal/mcp/server.go`.
- The tool reference documents the surface that `internal/mcp/server.go`
  registers, not the `internal/mcp/tools` package: there are fifteen tools,
  `get_source` among them, and `get_unresolved_references` is not published --
  that question is answered by the CLI. With no published generation only the
  three indexing controls are registered, and the documentation says so where it
  is installed.

## The home page

- The order of the bands **is the argument** and goes in one direction: it starts
  with a question with no name -> `find_by_intent` names the files to open -> this
  is what a resolved graph says about a symbol -> this is the measurement -> that
  is why a text search cannot say the same -> what a name really is -> the
  relations cross repositories -> it plugs into the agent you already use ->
  install it. `index.astro` composes nothing else.
- The `find_by_intent` demo goes **before** the measurement so that the reader
  first sees what is measured and then the comparable result. If the order is
  changed, four things change: this sentence, `index.astro`'s header comment, its
  sections and `TopBar.astro`'s links, which carry the same order as anchors.
- What is **not** on the home page is the list of the fifteen tools. The visitor
  asks what their agent will be able to understand, not which functions the server
  exports; the tool reference is a page and each band links to it where the
  question appears. A component that enumerated them disappeared because of that,
  together with the band of three cards that only served to link three SEO pages:
  those links now live inside the text of the section they belong to, which is
  also the contextual anchor a search engine rewards.
- **Every published figure comes from a single pass, and the pass is named.** The
  home page cites `benchmarks/graph-tools-comparison/results-all.json`, commit
  `954b9eb`, `o200k_base`, kivgraph `0.5.0`, 29 questions over 37 repositories:
  `35,961` tokens against `267,980` for `grep` plus reading, 28 of 29 exact in both
  arms. Mixing passes is the concrete defect there was: a token total from one, an
  accuracy from another and indexing times from a third produced a `6,200 / 7 of
  7` that **appeared in no committed file**, and a block of multipliers -`28.3x`,
  `2.1x`, `0.2x` over a monorepo of `41` repositories and `100,118` symbols- that
  did not exist in the harness it was attributed to. If a new table needs rival
  arms, they go in their own table with their own JSON: the only pass where the six
  arms really measured is `results.json`, and there kivgraph is `0.3.2`.
- The counterweight is part of the evidence, not a disclaimer underneath. `grep`
  comes out cheaper on 5 of the 29 questions and the home page counts them; the
  product section itself publishes the `I1_go_depth2` case, where we lose.
- The product demonstration **is not illustrative**. Every path, line, hop and
  figure comes from a verbatim capture in `raw-all/`, and the component names the
  file. A diagram with plausible names would argue against the only property this
  page sells, like the synthetic node field that was withdrawn from the hero.
- The three tabs of that section are a radio group and **zero JavaScript**: a
  radio group is already navigable with the keyboard and a screen reader announces
  it, the three panels go in the served HTML -- so a crawler sees all three-- and a
  page whose CSS does not arrive shows all three instead of none. The `<input>`s
  go before the tabs and the panels because the whole control is a sibling
  selector; they are `sr-only` and never `display: none`, which is what would take
  focus away from them.
- **The install command is a platform selector, not a box.** Three bundles are
  published -- `linux/amd64`, `darwin/arm64` and `windows/amd64`-- and two
  installers, because `install.sh` does not run where there is no POSIX shell. The
  control lives once in `InstallCommand.astro` and the hero and the final CTA use
  it; the commands and the three architecture notes live in `platforms.ts`, which
  is the only place they are written: the hero and `FinalCta.astro` each had their
  own copy of the `curl`, which is how two boxes on the same page end up
  publishing different commands.
- macOS and Linux show **the same line** on purpose. `install.sh` reads `uname`
  and picks its archive, so the answer for a Mac is the answer for Linux; what a
  box with no labels does not answer is the question the visitor brings, which is
  whether their machine is on the list. Windows is indeed another command:
  `irm ... | iex`, and its `#Requires -Version 5.1` becomes a comment when piped
  into `Invoke-Expression`. That is said in `/install/`, not on the home page.
- The control is a radio group and **zero JavaScript**, like the tabs of
  `ProductProof.astro`, with one difference that matters: the rules hook onto
  `data-platform` and not onto an `id`, which is what lets the same component
  appear **twice** on a page without the two boxes sharing a selection. Each panel
  carries its own `CopyButton`, because the value it copies is rendered on the
  server and a single button could not follow the selection without the script
  this control exists to avoid. Nothing is detected: the page is prerendered, so
  the first tab is a default and not an assumption about the reader.
- Measured with the same protocol as the badge, two builds at once and 15
  interleaved loads per arm at `1440x900`: before the tabs
  `min 77 / median 89 / max 194 ms`, after `min 74 / median 85 / max 227 ms`. The
  LCP element was the `h1` in **all 30 loads** and its painted surface the same,
  `90132`. It was also verified on the served build that the panel changes with the
  pointer and with the keyboard, that moving one box does not move the other, that
  the `data-copy-data` of the visible button follows the tab
  -- `{"where":"hero","platform":"windows"}`-- and that **with the style sheets
  disabled the three commands are seen** in each box, which is the honest
  degradation for a control whose job is to name a command.
- The final CTA is its own band and not a `Section`: its heading **is** the call
  to action, and putting it inside would put two `h2` in one band. It repeats
  `Section`'s geometry -- `border-t`, `max-w-6xl`, `py-14`, `data-reveal`-- so the
  rhythm is identical from outside.

## The hero

- The hero is centred and its background is CSS: a static `72px` mesh in
  `--color-rule-strong` and a single diagonal sheen that sweeps it, declared in
  `Hero.astro`'s `<style>`. There is no canvas, no `requestAnimationFrame`, no
  observers, no reading of the palette from the cascade: the synthetic canvas
  there was before cost 415 lines and drew a field whose positions encoded no
  relation of the graph.
- The sheen is the mesh's `::after`, not a sibling, so that the mesh's mask crops
  it: a sibling would light the edges of the band that the mask exists to hide. It
  goes back and forth with `alternate` instead of wrapping around, because a
  one-way sweep jumps to its starting position on every cycle.
  `prefers-reduced-motion: reduce` stops it.
- The veil is a seam, not a scrim. It only closes the pattern at the band's edges;
  a wash opaque enough to "protect" the text erases the mesh in its place, which is
  exactly how a background ends up invisible.
- The band carries no `border-bottom`: the first `Section` draws its own
  `border-top`, and two contiguous rules read as a 2px rule that no other seam on
  the page has.
- The bars of `TokenSaving.astro` are derived from their own figures and are never
  sized by hand: a chart whose geometry contradicts its numbers argues against
  what it is about to demonstrate. The card no longer lives in the hero: it is in
  the measurement band, next to the three big figures it summarizes, and both read
  the same two totals of the same pass.
- `global.css` omits Tailwind's preflight on purpose, because Starlight brings its
  own reset. The landing's lives in `Layout.astro`'s `@layer base` and is the one
  that strips the native chrome from `<button>` (`appearance`, `background`,
  `font`). A page that does not use `Layout` -- a loose preview route, for
  example -- draws system controls: a light `ButtonFace` box with the system
  typography. No component duplicates it.

## Motion

- `landing/src/lib/motion.ts` is the only animation layer with JavaScript, and it
  is loaded only from `Layout.astro`: Starlight's documentation does not receive
  it. There are four pieces: the hero's entrance, the background's switch-on, the
  halo that follows the pointer and the two magnetic CTAs, plus the reveals on
  scroll and the background's parallax.
- The hero's markup declares the anchors and the layer does not search by
  structure: `data-hero` on the band, `data-hero-item` on the eight blocks in
  reading order, `data-hero-field` on the plane and `data-hero-halo` on the halo.
  The first name of `HERO_ORDER` is `title` and not the block that goes first on
  the page: that name receives a tween of its own with zero delay, and the `h1` is
  the LCP candidate. Putting the `eyebrow` first queues the `h1`, which is the
  `1112 ms` case in the table below.
- The halo lives in the markup, the JS does not create it: an element added at
  runtime carries no Astro scope attribute and the component's scoped CSS would
  not reach it. It rests at `opacity: 0` because it holds nothing -- if the module
  does not run, there is nothing to reveal.
- The sheen is an `::after` with a CSS loop and **no tween reaches a
  pseudo-element**. The hero declares its duration as `--sheen-duration` and the
  entrance shortens it for one pass and then **removes** the property, which is
  what returns the element to the style sheet's value instead of pinning a copy of
  its own.
- Measured on the served home page, five loads: LCP between `64` and `72 ms` with
  median `72 ms`, and the element is the `H1` in all five -- the same as without
  animation, and without moving when the `eyebrow` was added or when the body
  switched to Geist Sans, which are the two checks those edits demanded. The
  hero's eight blocks end at `opacity: 1` and the 21 blocks with `data-reveal`
  too; with `prefers-reduced-motion: reduce` the eight come out at `1`, the halo at
  `0`, the sheen with `animation-name: none` and the bars at their final width.
- **The eighth block is the Glama badge, and it is the only image in the hero and
  the only third-party mark on the page.** It is served from `glama.ai` instead of
  being committed because the rating is theirs and can change: a copy here would
  keep claiming what it claimed the day it was saved. What that costs is bounded
  -- `4 KB` of SVG with `max-age=300`-- and the `width` and `height` are the SVG's
  own `110x20`, so the box is reserved before the response arrives. The `alt`
  names what the badge is and **never** what it says, for the same reason the image
  is remote. It goes with no Umami event, like the same link in the bar.
- It was measured as the rule below demands, two builds served at once on two ports
  and the loads interleaved, 15 per arm at `1440x900`: without the badge
  `min 75 / median 98 / max 236 ms`, with it `min 74 / median 79 / max 133 ms`. The
  badge costs nothing -- it comes out below, within a noise that in this case
  reaches the `160 ms` of the arm without it -- and what was really being checked
  is the other thing: the LCP element was the `h1` in **all 30 loads** and its
  painted surface the same, `90132`. It was also verified that the box holds
  without the image: with the request to `glama.ai` **aborted**, the badge still
  measures `110x20` at `top 718`, the facts row is still at `top 670` and the
  document measures the same `11998px` as with the image served. A badge that does
  not load moves nothing, which is the only thing a third party in the hero has to
  guarantee.
- The `h1` is the home page's LCP element, and what that metric penalizes is the
  **delay**, not the duration or the fade. Measured by injecting each rule before
  the first paint, with a `PerformanceObserver` on `largest-contentful-paint`:

  |rule on the `h1`|LCP|element|
  |---|---|---|
  |none|`76 ms`|`H1`|
  |`transform` only, `.6s`|`48 ms`|`H1`|
  |`opacity` + `transform`, `.6s`, no delay|`88 ms`|`H1`|
  |`opacity`, `.6s`, with `.6s` of delay|`1,112 ms`|`H1`|
  |permanent `opacity: 0`|`40 ms`|`P`|

  So the `h1` **can** enter animated if its delay is zero; what it cannot do is
  wait for its turn in a cascade. The last row is the trap: a hidden `h1` stops
  being a candidate, Chrome measures a smaller paragraph and the metric *improves*
  while the page cannot be read.
- Any new animation over the hero is measured the same way before it is taken as
  free. The probe that injects the CSS has to insert the `<style>` as soon as
  `document.head` exists: in `evaluateOnNewDocument` neither `head` nor
  `documentElement` exist yet, and a `MutationObserver` registered there never
  gets to observe anything -- it returns the baseline disguised as a result.
- **A new control in the hero is measured like an animation**, and the button that
  copies the agent's prompt is the first that went through there. Measuring one LCP
  against the other build is the only thing that answers the question: the figure
  alone says nothing, because the measuring machine and the installed versions move
  the median more than the change. **Both builds** are served at once, on two
  ports, and the loads are **interleaved** alternating which goes first in each
  round; otherwise the arm that always runs second inherits an already warm machine
  and the difference that gets published is that.
- Result of that pass, 15 loads per arm at `1440x900`: without the button
  `min 75 / median 85 / max 104 ms`, with it `min 71 / median 82 / max 99 ms`. The
  button costs nothing -- it comes out 3 ms below, within a noise of ~30 ms -- and
  what was really being checked is the other thing: the LCP element was the `h1`
  (`data-hero-item="title"`) in **all 30 loads** and its painted surface the same,
  `90132`. A control added to the actions row did not displace the candidate, which
  is the failure this section exists to avoid.
- The probe does not live in the repository and does not need to: `playwright-core`
  in a throwaway directory outside the tree, pointing with `executablePath` at the
  Chromium that Playwright's cache already has. `-core` on purpose, because it
  downloads no browsers. `addInitScript` is the `evaluateOnNewDocument` this file
  names above, and for LCP there is no need to wait for a `head` to exist: a
  `PerformanceObserver` on `largest-contentful-paint` with `buffered: true` does
  not touch the DOM. From bare Chromium's CLI -- `--dump-dom` and
  `--virtual-time-budget`-- the measurement comes out empty or unrepeatable; it
  was tried, and that is why it is written here.
- The initial state is set by GSAP with `gsap.from`, **never by CSS**. No element
  rests at `opacity: 0` -- the only such declaration in the style sheets is the
  keyframe that blinks the transcript's cursor--, so a blocked bundle leaves the
  whole page visible instead of hiding it behind a script.
- GSAP core plus `ScrollTrigger` weigh `112.1 KB` uncompressed and `43.5 KB`
  gzipped in the home page's bundle, measured over `dist/client/_astro`. It does
  not affect indexing -- the content is in the served HTML-- but it is unused
  JavaScript in Lighthouse's eyes. Change the number when touching the layer.
- Only `opacity` and `transform` are animated. Neither feeds back into layout, so
  a reveal cannot contribute to CLS.
- `prefers-reduced-motion: reduce` returns before registering the plugin: nothing
  is instrumented, instead of instrumenting it and then skipping it.
- The markup contract is `data-reveal` on a container, whose direct children enter
  staggered, and `data-hero-field` on the hero's plane. They are declared by
  `Section.astro`, so a new section inherits it.
- A load animation does not go in deferred JavaScript: the module may run after the
  first paint, and then `TokenSaving`'s bar would be seen complete before shrinking
  to grow. That one grows in CSS.

## External links

- Every link that leaves the site opens in a new tab, with `target="_blank"` and
  `rel="noopener noreferrer"`. `noopener` is the reason for the `rel`: without it
  the opened tab gets a handle on `window.opener` and can navigate this one.
- There are three surfaces and none covers the other two:
  - The documentation's markdown is resolved by `rehype-external-links` in
    `astro.config.mjs`. An author cannot remember the attributes on every link.
  - The landing's literal anchors carry them written out.
  - The generated navigations -- `TopBar.astro`, `Footer.astro`-- derive it from
    the `href` itself with `/^https?:\/\//`, so a new entry cannot forget the rule.
  - Starlight's social icon is a component and no rehype step reaches it: it is
    overridden in `src/components/starlight/SocialIcons.astro`, which keeps the
    original `rel="me"` -- it is a statement of identity and removing it would
    break rel-me verification.
- It is checked over the generated HTML, not by reading templates: `dist/client`
  has `97` external anchors and `2,805` internal ones, and the correct count is
  `0` external without the rule and `0` internal with `target`. The totals grow
  with every page and are only context for the pass that measured them; what is
  verified are the two zeros.

## Verification

```bash
make landing-check
make landing-build
```

`pnpm --dir landing check` is `format:check`, `lint` and `typecheck`
(`astro check`).
It is served with `pm2 start landing/ecosystem.config.cjs` on port `6767`.
