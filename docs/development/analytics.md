# Analytics: Umami for SEO

This document describes how the landing site's analytics are wired, what they
record, what they do **not** record, and how to use them to answer SEO
questions. It is not user documentation: `docs/` is internal material and is
not published.

There is one criterion and it is worth stating before anything else: **we do
not optimise for having more data, but for having actionable data**. An event
that does not change a decision is not added.

## What is there

A **self-hosted** Umami at `https://analytics.luqueee.dev`, confirmed by its
own `/api/config`, which returns `"cloudMode": false`. The collection endpoint
is `/api/send`.

The tracker is emitted by **both** halves of the site and by nothing else:

|half|component|
|---|---|
|landing|`landing/src/components/landing/Layout.astro`|
|documentation and `404`|`landing/src/components/starlight/Head.astro`|

Both read `umamiTracker()` from `landing/src/pages/_seo.ts`, so the tag is
identical across the 39 pages and **there is no way for one half to drift from
the other**. Measured over `dist/client`: 39 of 39 pages carry the tracker, and
exactly **one** per page.

The tag that is served:

```html
<script
  defer
  src="https://analytics.luqueee.dev/script.js"
  data-website-id="<uuid>"
  data-domains="kivgraph.dev"
  data-performance="true"
></script>
```

### Why the site does not duplicate pageviews

Because it cannot. The landing is Astro with `output: "server"` and everything
prerendered except `/raw/[...slug]`, and it **mounts neither `ClientRouter` nor
ViewTransitions**: every navigation is a full document load. There is no client
routing that could fire a second pageview, and `auto-pageview` stays at its
default because there is nothing to disable.

## Environment variables

|variable|where|what it does|
|---|---|---|
|`KIVGRAPH_UMAMI_SCRIPT_URL`|host's `landing/.env`|absolute tracker URL|
|`KIVGRAPH_UMAMI_WEBSITE_ID`|host's `landing/.env`|the UUID Umami minted|

**Fail-closed by design:** with only one of the two, `umamiTracker()` returns
`null` and nothing is emitted. That is why `astro dev`, a local build and CI
cannot write to the production dataset.

Both are **baked at build time**, because the pages are prerendered. Exporting
them and restarting `pm2` emits nothing: you have to rebuild.

```bash
cd /root/kivgraph && git pull
make landing-build && pm2 restart kivgraph-landing
curl -s https://kivgraph.dev/ | grep -c analytics.luqueee.dev   # expect 1
```

### The guard the variables cannot be

`data-domains="kivgraph.dev"` comes from `PRODUCTION_HOST` in
`landing/src/site.mjs` and **not** from the host of `Astro.site`. That
distinction is what decides whether it works: a staging deployment pins `site`
to the staging origin, so following `Astro.site` would let it match itself and
write to production. The pair of variables fails closed when one is missing,
but a preview that **inherits both** is exactly the case that pair cannot
catch.

`PRODUCTION_ORIGIN` is also the fallback value of `site` in
`astro.config.mjs`, so the production origin is declared **once**.

## What is recorded

Automatically, by the tracker:

- pageview with `url` **including the query string**, `referrer`, `title`,
  `hostname`, language and screen resolution;
- country and device, derived by the server from the request;
- visit duration and page-to-page navigation, derived from the session;
- **Core Web Vitals** through `data-performance`: LCP, INP, CLS, FCP and TTFB.

Plus the events in the table below.

### What is NOT recorded

- **Nothing personal.** There is no `identify()`, no user id, and no free-text
  fields that could drag along text the visitor typed.
- **No secret.** The `website-id` is public by construction — it travels in the
  HTML of every page — and authorises nothing.
- **Nothing trivial.** No `scroll_25`, no `mouse_move`, no time-on-screen
  buckets. An event that fires without intent distorts the *bounce rate* and
  engagement, which are precisely the two metrics used to decide which page to
  fix.

### Query strings are kept on purpose

`data-exclude-search` is **not** set, and must not be. That is where
`utm_source`, `utm_medium`, `utm_campaign`, `utm_content`, `utm_term`, `gclid`,
`fbclid` and `msclkid` travel. Excluding them would leave campaign attribution
blank.

## Events

Every one comes from an action that expresses **real intent**. Five of the six
go through `CopyButton.astro`, which is what guarantees a consistent name and
**one fire per copy** — it reports in the `then` of the `writeText`, so a copy
the clipboard refused does not count.

|event|when it fires|metadata|
|---|---|---|
|`install_copy`|the one-liner is copied|`where`: `hero`, `final_cta`|
|`prompt_copy`|the agent prompt is copied|—|
|`client_connect_copy`|`kivgraph mcp install` is copied|—|
|`quickstart_copy`|a Quickstart command is copied|`step`: the step's title|
|`mcp_config_copy`|the MCP JSON is copied|—|
|`github_click`|link to the repo|`where`: `topbar`, `footer`, `docs_header`|

Where each one fires, measured over `dist/client`, which is what you need to
know in order to test them and what decides whether a goal can be scoped by
route:

|event|section of the front page|anchor|
|---|---|---|
|`prompt_copy`|hero|`/`|
|`install_copy`|hero and final CTA|`/` and `#install`|
|`client_connect_copy`|06, *works where you already code*|`#agents`|
|`quickstart_copy`|07, *start with your workspace*|`#quickstart`|
|`mcp_config_copy`|07, the JSON box|`#quickstart`|
|`github_click`|topbar, footer and docs header|all 39 pages|

The five copy events live **only on the front page**. If a copy button is ever
added outside it -- `/install/` is the obvious candidate -- the goals have to be
revisited: one scoped to a route silently stops counting, and a conversion that
drops without warning reads as people having stopped installing.

`github_click` uses **no** JavaScript of ours: Umami reads `data-umami-event`
off the click. In `TopBar.astro` the attribute is derived from the `href`
itself, so a new entry cannot forget it, and **only** external links carry it:
an internal one is already a pageview, and counting it twice would put a number
in the report that no visit produced.

### Naming convention

- `snake_case`, lowercase, no spaces and no accents;
- `<object>_<action>`, not the other way round: `install_copy`, not
  `copy_install`;
- metadata is flat and its values are strings, because that is what a report
  groups by;
- a new event is added **here** before it is added to the code.

### If the tracker is not there

Nothing happens. `report()` in `CopyButton.astro` checks `window.umami` and
returns silently if it does not exist — blocked, instance down, or a build
without the variables — and wraps the call in a `try`. Analytics that fail are
not a failure of the page, and the copy the reader asked for happens anyway.

## Recommended goals

This site's conversion **cannot be observed**: it happens in a terminal. The
closest the page gets is the moment the visitor takes the command or the prompt
away. Those are the primary conversions.

|level|events|what it means|
|---|---|---|
|**primary**|`install_copy`, `prompt_copy`|takes what is needed to install|
|secondary|`client_connect_copy`, `mcp_config_copy`|is connecting their agent|
|secondary|`quickstart_copy`|is following the guide step by step|
|micro|`github_click`|interest, not commitment|

The funnel worth measuring:

```text
landing SEO -> hero -> install_copy | prompt_copy
```

`quickstart_copy` with `step` tells who dropped off and where: if step 1 is
copied far more than step 3, the problem is in between.

## UTM convention

**UTMs only go on external links that we control.** Never on an internal link:
it would rewrite the session's own `referrer` and split one visit into two,
which is exactly what ruins an attribution report.

Rules: lowercase, no spaces, names that stay stable over time.

```text
?utm_source=reddit&utm_medium=social&utm_campaign=launch
?utm_source=x&utm_medium=social&utm_campaign=launch
?utm_source=youtube&utm_medium=video&utm_campaign=demo
?utm_source=github&utm_medium=referral&utm_campaign=readme
```

|parameter|vocabulary|
|---|---|
|`utm_source`|`reddit`, `x`, `youtube`, `github`, `hn`, `newsletter`|
|`utm_medium`|`social`, `video`, `referral`, `email`|
|`utm_campaign`|the campaign name, stable: `launch`, `readme`, `v0-8`|
|`utm_content`|the variant, when there are two creatives|
|`utm_term`|only in paid campaigns with a keyword|

## How to analyse organic traffic

The axis is always the same: **referrer -> landing -> behaviour ->
conversion**.

1. In the dashboard, filter by `Referrer` = `google.com` (or `bing.com`,
   `duckduckgo.com`).
2. Sort by `Path` to see **which pages receive the traffic**, not just how many
   visits there are.
3. Cross it with `Bounce rate` and `Visit duration`: lots of traffic with a high
   bounce is a page that ranks for an intent it does not satisfy.
4. Add the goal to see **which landings convert**, not which receive most.
5. Look at the Web Vitals of that same page in the performance tab.

Available segmentations: referrer, path, landing page, device, country,
`utm_source`, `utm_medium`, `utm_campaign`, event/conversion and Web Vitals.

### The four cases you have to know how to find

|pattern|what it means|what to do|
|---|---|---|
|high traffic, high bounce|ranks for another intent|rewrite the intro|
|low traffic, high conversion|good and nobody sees it|internal links to it|
|high traffic, bad p75 LCP|loses whoever arrives|optimise that route|
|good position, low CTR|the snippet does not sell|rewrite `title`/`description`|

The last one **is not visible in Umami**: it comes from Search Console. Hence
the next section.

## Umami and Google Search Console are complementary

Umami does not replace Search Console and must not try to. Each sees one half
and the union is what is actionable:

|Search Console|Umami|
|---|---|
|query, impressions, CTR, position|visitors, visits, bounce, duration|
|landing URL|landing URL, referrer, navigation|
|country, device|country, device, events, conversions, Web Vitals|

**The column that joins them is the landing URL.** The cross is done by
exporting Search Console's pages report and Umami's pages report, and joining
on that column:

```text
Search Console: /docs/cli/  ->  12,400 impressions, CTR 1.2 %, position 8.4
Umami:          /docs/cli/  ->  148 visits, bounce 71 %, p75 LCP 3.9 s
```

That row says three things at once: it ranks, the snippet does not convince,
and whoever enters finds a slow page. Neither tool says it alone.

No more infrastructure is needed for this. An automatic integration between the
two would require a job, Search Console API credentials and a store —
disproportionate for a 39-page site and a cross that is done with two CSVs.

## IndexNow: telling Bing before it comes back

Search Console and Umami both describe what already happened. IndexNow is the
one lever that changes *when* a page is looked at: it pushes a list of changed
URLs instead of waiting for a crawler to return.

It matters here for a reason that is not obvious. **The answer engines do not
crawl much on their own** -- ChatGPT's search leans on Bing's index, Perplexity
on Bing and Google. So this is the closest thing that exists to submitting a
site to AI search, and it is still only a push: it changes when a page is
seen, never whether it is liked. Nothing here is a ranking signal.

### Ownership is a file, not a token

`https://kivgraph.dev/<key>.txt`, containing exactly the key and nothing else.
Anyone may read it, and that is the mechanism rather than a weakness: fetching
it proves whoever submitted the URLs controls the host. So the key is **public
by construction**, committed like the Umami website id, and belongs in no
secret store.

|where|what|
|---|---|
|`landing/src/indexnow.mjs`|the key, and the path derived from it|
|`landing/public/<key>.txt`|the file the verifier fetches|
|`scripts/indexnow-submit.sh`|the submission|

### The failure that says nothing about itself

The key lives in two places -- a constant, and a file whose **name** is that
constant -- and nothing at build or request time compares them. When they
drift, IndexNow answers `403`, which is exactly what a key that was never valid
gets. The response cannot tell you which happened.

So two things check it instead. `landing/src/indexnow.test.mjs` asserts the
file exists, that its bytes are the key with no trailing newline, and that it
is the **only** key file -- a rotated key leaving the old file behind is the
other way this drifts. And the script fetches the key from the live site before
submitting anything, so a `403` cannot be reached without first being told the
file is not being served.

### It is a build artifact

`landing/public/` is copied into `dist/client`, so the key file **is baked at
build time** like everything else here. Adding it and restarting `pm2` serves
the previous bundle and the key stays a `404`; the order is the one the
environment variables need:

```bash
cd /root/kivgraph && git pull
make landing-build && pm2 restart kivgraph-landing
curl -s https://kivgraph.dev/<key>.txt   # the key, or nothing has changed
```

### Submitting

```bash
scripts/indexnow-submit.sh                    # every URL in the live sitemap
scripts/indexnow-submit.sh /install/          # only what changed
INDEXNOW_DRY_RUN=1 scripts/indexnow-submit.sh # print the payload, send nothing
```

The URL list is read from the site's own sitemap rather than kept here, because
a second list of pages is a list that goes stale. `200` is accepted and `202`
is accepted with the key still to be validated -- both are success, and a `202`
that never becomes a `200` means the key file stopped being reachable.

It is deliberately **not** automated. The landing deploys by hand, and a
submission fired from CI would announce URLs the host has not rebuilt yet.

## Core Web Vitals

`data-performance="true"` is what turns them on. Umami collects them from the
visitor's own browser, so they are **field data**, not lab data: they are what
someone arriving from Google actually experiences, and that is why they are
worth more than a local Lighthouse for deciding what to fix.

Always read the **75th percentile**, which is the threshold Google evaluates
against, and not the mean: a good mean hides a quarter of visitors with a bad
experience perfectly well.

Priority: **LCP**, **INP**, **CLS**. `FCP` and `TTFB` are diagnosis, not a
target.

The lab measurement of the hero lives somewhere else and answers another
question: `landing/AGENTS.md`, section *Movimiento*.

## AI agents: the second property

Two distinct phenomena, two datasets. The main property describes **people**;
AI crawlers and agents go to a second property and do not touch visitors,
bounce, duration, journeys or conversion of the first. Umami's bot filter stays
**on** in both.

### Why it is not an Astro middleware

Because it sees nothing. Measured over the build: a middleware in
`src/middleware.ts` received **one** of seven routes -- `/raw/docs/cli.md`, the
only non-prerendered one -- and missed `/`, `/docs/cli/`, `/install/`,
`/robots.txt`, `/sitemap-0.xml` and the 404. The adapter's static handler
answers those before the SSR pipeline exists, and they are precisely the ones a
crawler asks for.

What does see everything is `landing/server.mjs`, which is what pm2 starts
instead of the adapter's entry. It reimplements nothing: it imports the
`handler` the adapter itself exports -- `createStandaloneHandler`, with statics,
the trailing-slash `301` and the 404 -- and wraps it in an `http.createServer`.
Verified through the wrapper: `/`, `/docs/cli/`, `/robots.txt` and
`/sitemap-0.xml` at `200`, and `/docs/cli` at `301` to the canonical form.

### The agent registry

`landing/src/ai-agents.mjs`. Plain ESM for the same reason as `site.mjs`: it is
consumed by a bare Node process that Vite never transforms.

**Every identifier came from the operator's documentation**, with the source
and the date beside it. Adding an agent is a row there and a case in
`ai-agents.test.mjs`; no other place in the repository names an agent.

|provider|agents|category|published verification|
|---|---|---|---|
|OpenAI|`OAI-SearchBot`|`search`|IP ranges (`searchbot.json`)|
|OpenAI|`ChatGPT-User`|`user_fetch`|IP ranges (`chatgpt-user.json`)|
|OpenAI|`GPTBot`|`training`|IP ranges (`gptbot.json`)|
|Anthropic|`Claude-SearchBot`|`search`|none published|
|Anthropic|`Claude-User`|`user_fetch`|none published|
|Anthropic|`ClaudeBot`|`training`|none published|
|Perplexity|`PerplexityBot`|`search`|IP ranges (`perplexitybot.json`)|
|Perplexity|`Perplexity-User`|`user_fetch`|IP ranges (`perplexity-user.json`)|

Three things the registry does on purpose and that are best left alone:

- **OpenAI's UAs carry a full Chrome inside.** Their real string starts with
  `Mozilla/5.0 (Macintosh; ...) Chrome/131.0.0.0 Safari/537.36` and the token
  comes at the end. A matcher that checked "is this Chrome?" before the token
  would file all three as human.
- **A generic bot is not AI.** `Googlebot`, `Bingbot`, `Discordbot`, `curl` and
  a browser fall through to `null`. Turning any bot into an AI bot would drain
  the entire second dataset of meaning.
- **`Google-Extended` is not there, and its absence is the point.** It is a
  `robots.txt` token that controls whether Google may use already-crawled
  content to train Gemini; Google's crawler documentation assigns it no HTTP
  user agent, so it never arrives on a request. Inventing Gemini traffic from
  it would be fabricating a figure.

`unknown_ai` is the fourth category: an operator this file knows about sending
an agent it does not. It is a finding, not a classification, and the signal
that a new row is needed.

### Verification

Today it is `user_agent` and the event says so: `verified: false`. Nobody
resolves DNS or compares ranges on the request path, because that would cost
latency.

OpenAI and Perplexity **publish CIDR files** -- they are in the registry -- so
raising this to `ip_range` is possible later and out of band: download the JSON
with caching and compare. Anthropic publishes none, so a Claude row cannot be
more than an assertion. Lying about that would be worse than not verifying.

### What is sent, and what is not

Two events to the crawler property, per request, always both or neither.

`ai_crawler_request` is the rich one and it has not changed: `provider`,
`agent`, `category`, `path`, `method`, `status` and `verified`. It is what every
report below filters, and what gets crossed by hand with the main property.

`ai_crawler_<agent>` is the same request under the agent's own name, with
`provider`, `category` and `verified` and nothing else. It carries no
information the first does not already have, and it exists for one reason:
**Umami's *Events* chart plots one series per event name and cannot split a
single event by one of its properties.** "Which agent caused that spike" is
therefore a question `ai_crawler_request` cannot draw, however complete it is.
`path`, `method` and `status` are deliberately not copied onto it -- they are
already on the row it pairs with, and a second copy is a second place for them
to be wrong.

|agent|event|
|---|---|
|`OAI-SearchBot`|`ai_crawler_oai_searchbot`|
|`ChatGPT-User`|`ai_crawler_chatgpt_user`|
|`GPTBot`|`ai_crawler_gptbot`|
|`Claude-SearchBot`|`ai_crawler_claude_searchbot`|
|`Claude-User`|`ai_crawler_claude_user`|
|`ClaudeBot`|`ai_crawler_claudebot`|
|`Perplexity-User`|`ai_crawler_perplexity_user`|
|`PerplexityBot`|`ai_crawler_perplexitybot`|
|an OpenAI agent with no row|`ai_crawler_openai_unknown`|
|an Anthropic agent with no row|`ai_crawler_anthropic_unknown`|
|a Perplexity agent with no row|`ai_crawler_perplexity_unknown`|

**Eleven names and no twelfth.** The name comes from the registry row, never
from the request: a `User-Agent` of `HolaSoyPepito123` matches nothing, is
classified `null` and emits neither event, so nobody outside `ai-agents.mjs` can
mint an event name and fill the property with one-off events. The three
`_unknown` rows are the only ones an unrecognised agent can reach, and they are
bounded by the three operators.

They are built by `crawlerEventPayloads()` in `landing/src/ai-report.mjs` rather
than inline in `server.mjs`, for the reason the header below already proves:
`server.mjs` needs a built `dist/server/entry.mjs` to be imported, so anything
written inline there is code no test can reach.

The pair goes out on the **same side of the deduplication gate**. That is the
invariant worth stating: a request that reports one reports the other, so the
per-agent chart and the overview can never disagree about how many requests
there were. The cost is that the crawler property's raw event count is twice
the request count -- a number to remember before reading it as traffic.

Adding an agent stays what it was: a row in `ai-agents.mjs`, a case in
`ai-agents.test.mjs`, and a line in the table above. Nothing else names an
agent, and there is no second list to drift out of step -- `event` sits on the
same row as `provider`, `category` and `pattern`.

**Not sent:** the IP, any cookie, any authorisation header, the query string or
the body. The full user agent stays **only** in the local log: it is what makes
it possible to turn an `unknown_ai` into a registry row, and it describes a
robot and not a person.

**The sender's `User-Agent` is the empty string, and that is the finding, not a
detail.** The reasoning that produced the first value was wrong, and it cost
the crawler property a day of silence.

It used to send `kivgraph-landing/1.0 (+https://kivgraph.dev)` -- neutral and
honest -- on the assumption that Umami's collector rejects a `User-Agent` that
*looks like* a crawler. It does not. It runs `isbot` over the sender's own
header, and `isbot` treats **anything that is not a recognisable browser** as a
bot. Measured against the instance on a throwaway property, every honest
identifier was discarded and only one value was recorded:

|sender `User-Agent`|collector|
|---|---|
|`kivgraph-landing/1.0 (+https://kivgraph.dev)`|`{"beep":"boop"}`, discarded|
|`kivgraph-landing/1.0`, `kivgraph-landing`, `kivgraph`|discarded|
|`node` -- what `undici` sends on its own|discarded|
|**the empty string**|**recorded**|
|a browser user agent|recorded, and it would be spoofing|

It fails **invisibly**, which is the part worth remembering. Umami answers
`200`, so the reporter's `catch` has no error to catch. The local detection log
is written either way. And `ai_tracking` on the startup line still says `true`,
because the variables really are set -- that flag reports configuration, not
delivery. Eighty detections were logged and nothing arrived, with no signal
anywhere. **A detection in the log and `ai_tracking: true` together still do
not prove Umami received anything.** Only the property does.

Deleting the header is not the same fix as emptying it: `undici` substitutes
its own `User-Agent: node`, which `isbot` matches. The empty value has to be
written down, and `landing/src/ai-report.test.mjs` asserts both halves on the
wire rather than on the constant.

An empty `User-Agent` claims nothing and impersonates nobody; the agent being
reported travels as normalised metadata in the payload, where it belongs.
`DISABLE_BOT_CHECK` on the instance was the other way out and it is the wrong
one: it is global, so it would drop the filter on the main property too, which
is exactly the contamination all of this avoids.

### Noise

- **Assets out.** `/_astro/`, `/pagefind/`, the favicon, the manifest and
  everything ending in an asset extension produce no row.
- **In on purpose:** `robots.txt`, `llms.txt`, `llms-full.txt`, the sitemaps and
  `/raw/**.md`. They are not pages, but they are **how** an agent discovers and
  reads this site, and a crawler that starts at `llms.txt` behaves differently
  from one that walks the sitemap.
- **15-minute deduplication** per (agent, path) pair. Measured: four requests to
  the same path produce **one** event. It is not sampling -- which would drop
  rows at random and make a rare agent's count meaningless -- and the local log
  still has all four.

### None of this blocks a response

`fire-and-forget` with a 2 s timeout, inside `response.on("finish")`, and every
failure is swallowed. Measured with Umami pointed at a dead port: `/docs/cli/`
answers in **0.6-0.9 ms**. If Umami goes down, `kivgraph.dev` serves anyway.

### Local log

One JSON line per detection on `stdout`, which is where pm2 already collects:

```json
{"ai_agent":true,"ai_provider":"openai","ai_agent_name":"OAI-SearchBot",
 "ai_category":"search","ai_verification":"user_agent","verified":false,
 "method":"GET","path":"/docs/cli/","status":200,"at":"..."}
```

It is the source of truth that survives Umami being down or an event being
deduplicated, and it does not add a second logging system.

## First runs: the third property

A download is not a person, and an install is not a visit. Counting the
downloads is Layer 0 of `docs/adr/0083-a-download-is-not-a-person.md`: it
touches no client, reads GitHub's own counters and is deployed. This section
is Layer 1 -- the one ping that says a version arrived on a machine and ran
there.

**The endpoint receives; nothing emits yet.** The receiving half is deployed
and tested -- `landing/src/install-report.mjs`, wired in `landing/server.mjs`
-- and the two emitters are not written. So every rule below about validation,
deduplication and what reaches the collector is observed behaviour, and every
rule about the marker and the notice is still a design. `LUQUE-2232` carries
the emitters and their gates.

### The event

One event, `first_run`, on a third property, `kivgraph FIRST RUNS`, with five
flat string fields:

- **`emitter`** -- `installer` or `binary`. Which of two different facts this
  row is;
- **`version`** -- the version that arrived, `0.9.1`, validated against the
  published version pattern;
- **`platform`** -- `linux-amd64`, `darwin-arm64` or `windows-amd64`. The same
  vocabulary the release assets use, so Layer 0 and Layer 1 can be read side
  by side instead of being joined by hand;
- **`channel`** -- `installer`, `mcpb`, `archive` or `source`. How the binary
  got there. It is what makes the `.mcpb` share of the volume visible from the
  client side, where the download counters cannot see it;
- **`transport`** -- `stdio` or `daemon` on a `binary` row, and **absent** on
  an `installer` one, because an installer has not started a server and
  reporting a default it did not choose would be inventing data. It is
  required on the rows that have it: a `binary` row without a transport is
  refused rather than defaulted.

One machine installing one version therefore produces **at most two rows**,
one per emitter, and they are never added together.

`first_run` is the one event name that does not obey `<object>_<action>`. The
object *is* the event, and `run_first` would satisfy the shape at the cost of
being unsearchable by the name everyone will use.

### Why two emitters, and why the field cannot be dropped

An installer that finished and a binary that started are different facts, and
the second does not follow from the first: a bundle can be installed and never
launched. Without `emitter` the property would report an installer's success
as a first run, which is exactly the claim ADR 0083 spends its length
refusing. Only the `binary` rows answer *how many machines ran it*.

And a `.mcpb` never runs `install.sh` -- the MCP client unpacks the bundle and
launches the binary -- so instrumenting the installer alone would give clean
data about a twentieth of the volume and nothing about the largest share of
it. The split is counted in `LUQUE-2232` over the releases API from `v0.1.0`
to `v0.9.1`: `39 %` of downloads were `.mcpb` and `5 %` were the installers,
and Layer 0 now keeps that number current instead of quoting it.

### What is measured is the first run of a version

**The marker governs the `binary` emitter and nothing else.** An installer has
no marker: it reports once per successful run, and a reader who runs it three
times sends three, which the endpoint's window collapses. Sharing one marker
between the two emitters would be the bug the dedupe key already avoids --
whichever emitter got there first would suppress the other's row, and the two
rows are the two different facts this property exists to keep apart.

The marker lives under the state directory, not the bundle root: an update
replaces the bundle, so a marker there would fire again on every update.
Calling the number *installations* would be a claim the marker cannot support.

It is created with `O_CREATE|O_EXCL` and only the process that created it
sends -- this paragraph being the design, since the marker is not written
yet. Reading the marker and then writing it would let a whole burst find it
absent and report before any of them had created it -- and stdio starts
bursts: `docs/adr/0069-el-demonio-es-el-defecto.md` measured `69` starts of
`serve` with `8` alive at once, over one session of one client. That is one
install turning into as many pings as the client happened to spawn.

### What is not sent

No identifier of ours, no repository name, no path, no hostname, no user name,
nothing about the code that was indexed, and no address stored by us.

**Identity is Umami's, and this repository mints none.** Umami derives a
visitor from a daily-rotating hash of website id, hostname, address and user
agent, so *unique visitors per day* is already distinct machines and the
address itself is never stored.

That has one load-bearing consequence: **the endpoint has to give the
collector the caller's address**. Without it every install on earth collapses
into one visitor -- the landing server itself. And since `REPORTER_HEADERS`
forces `User-Agent: ""` to survive the collector's `isbot` filter, the address
is the *only* discriminator left, so a corporate NAT counts as one person.
That is the bias every web analytics carries, and it is written here rather
than discovered in a report.

### The address goes in the body, and that was measured

The design said *forward the header*. Against this instance no header works,
and it fails the way the `User-Agent` failed: `200`, a session token, events
stored, and one visitor.

Both `kivgraph.dev` and `analytics.luqueee.dev` sit behind Cloudflare, which
rewrites the address headers at its edge. Measured on a throwaway property,
`13` events:

|what was sent|what the collector recorded|
|---|---|
|`X-Forwarded-For: 203.0.113.7`|one session, country `ES` -- the sender's|
|`X-Real-IP: 198.18.0.1`|the same session|
|`X-Client-IP: 198.18.0.3`|the same session|
|`CF-Connecting-IP: 198.18.0.2`|`403`, Cloudflare error `1000`|
|`payload.ip: 8.8.8.8`, twice|**one session of its own**, country `US`|
|`payload.ip: 1.1.1.1`|**another session**, country `AU`|
|`payload.id: <a uuid>`|ignored; the event joined the sender's session|
|`payload.userAgent: kivgraph-first-run`|`{"beep":"boop"}`, discarded|

So `payload.ip` is what the collector reads, and it gives exactly the property
the layer needs: the same address twice is one visitor, two addresses are two.
`payload.id` being ignored matters for the opposite reason -- a visitor cannot
be named from outside, so identity stays the collector's daily-rotating hash
rather than something a caller can set.

The last row is the crawler reporter's finding arriving a second time: the
`isbot` filter reads a `userAgent` in the payload as well as the header, so
there is neither.

Two consequences for the endpoint, both in `landing/src/install-report.mjs`:
it reads `CF-Connecting-IP` first when deciding whose install it is, because
that is the header the edge sets and the socket address is the edge's; and it
puts that address in `payload.ip` rather than in a header of its own.

### The endpoint, and the bounds on the number

`POST /api/telemetry/first-run`, in `landing/server.mjs` because the reporter,
the `User-Agent` finding it depends on and the fail-closed configuration pair
are already there; an Astro route would need a second copy of all three.

It is public, so the number is worth exactly its bounds: validation against
the closed sets above and the published version pattern, a dedupe window per
address, version **and `emitter`**, and `204` on every path so that probing it
teaches nothing.

The `emitter` in that key is the easy one to leave out. An installer that has
just finished and the first run that follows it carry the same address and the
same version seconds apart, so a window keyed on those two alone would discard
the second -- precisely the `binary` row the property exists to collect.

The window does **not** buy the headline number. Under a daily-rotating hash
one address reinstalling in a loop is already **one** unique visitor, so
repetition inflates the event count and never the visitor count. The window
bounds events and write volume; validation is what stops a forged `version`
from inventing a row no release ever produced.

### Nothing may reach stdout, and stdio decides that

`kivgraph serve` runs the MCP surface over stdio, where one stray byte on
stdout corrupts the session; the daemon serves the same surface over HTTP,
where it does not. The ping is shared code, so it obeys the stricter of the
two everywhere: a goroutine with a short timeout, failures dropped, and the
first-run notice on stderr. A test that does not try to write to stdout will
not notice the day this stops being true.

### Off by a variable, on both ends

`KIVGRAPH_TELEMETRY=0` in the environment stops the client sending. The server
half fails closed the way the other two properties do: without its website id
nothing is forwarded, so a development landing cannot write to the dataset.

### What the collector stores, which is not nothing

A session row carries a **country** derived from the address -- `US` for
`8.8.8.8`, `AU` for `1.1.1.1`, empty for a reserved range -- and a device
class that defaults to `desktop` for a request with no screen. The address
itself is not stored. Coarse geography is therefore part of what this property
knows, and `/telemetry/` says so rather than leaving it to be discovered.

One more thing a report has to know: the stats endpoint answers `visitors: 0`
for this property, because it counts sessions with **pageviews** and a first
run is an event. The machine count is read from sessions and events, not from
the overview.

## PostHog: the behaviour, not the acquisition

Umami and PostHog **do not overlap and do not compete**. Each answers questions
the other cannot, and expecting their numbers to agree is a method error, not a
bug:

|tool|what it answers|fields|
|---|---|---|
|Search Console|before the click|query, impressions, CTR, position|
|Umami|the acquisition|referrer, landing, country, device, Web Vitals|
|PostHog|the behaviour|funnels, paths, replay, heatmaps, conversion|

The column that joins the three is the **landing URL**.

### A single call

No component knows a provider. They all call the same thing:

```ts
import { track, ANALYTICS_EVENTS } from "../../lib/analytics";

track(ANALYTICS_EVENTS.INSTALL_COPY, { where: "hero" });
```

`src/lib/analytics/` is `events.ts` (the typed catalogue), `umami.ts`,
`posthog.ts` and `index.ts` with the facade. An event name that is not in
`ANALYTICS_EVENTS` **does not compile**: an event that exists in one call and
nowhere else is a column nobody can explain six weeks later.

### Pageviews: why there is no `astro:page-load`

Because this site **has neither `<ClientRouter />` nor View Transitions** --
measured, not assumed. Every navigation is a full document load, so PostHog's
automatic capture gives exactly one `$pageview` per page. Adding a listener on
top is what produces **two**; adding it instead of the automatic one would mean
disabling that to reimplement what it already does.

If `<ClientRouter />` is ever added, this changes and you have to come back
here: the init guard in `posthog.ts` is already there for that reason.

Measured over the build, intercepting the ingest:

|action|events|
|---|---|
|initial load|`$pageview`|
|navigate A -> B|`$pageleave` + `$pageview`|
|back|`$pageleave` + `$pageview`|
|reload|`$pageleave` + `$pageview`|
|click to copy|`$autocapture`, `prompt_copy`, `$$heatmap`, `$web_vitals`|

One per load, none duplicated, and `prompt_copy` **exactly once** despite also
going to Umami.

### The cost, which is not small

|build|total JS|gzip|
|---|---|---|
|without PostHog|214.1 KB|**72.1 KB**|
|with PostHog|467.5 KB|**154.9 KB**|

PostHog **more than doubles** the JavaScript of a site that does not have a
single framework island. There is no lighter build: `module` (85 KB) is already
the smallest in the package and `module.no-external` is bigger.

Two things make it acceptable, and both are decisions:

- **It is imported dynamically after the `load` event.** The chunk is not among
  the scripts the front page requests, so it does not compete with the LCP,
  which in this repository is measured and written down.
- **Without `PUBLIC_POSTHOG_KEY` it disappears entirely.** Vite substitutes the
  variable at build time, the early `return` becomes constant and tree-shaking
  takes the library away: a build without a key grows `0.9 KB`. An unconfigured
  deployment pays nothing.

### Production and nothing else

`isAnalyticsEnabled()` requires `import.meta.env.PROD` **and**
`location.hostname === PRODUCTION_HOST`. Measured: from `localhost`, zero
events. `www` is not in the guard because `www.kivgraph.dev` does not resolve,
and adding it would be inventing a host.

### Replay privacy

`maskAllInputs` is on. What gets recorded is a documentation screen: commands,
paths and public code. There is no form collecting personal data, no token, no
header and no secret in the HTML -- `PUBLIC_POSTHOG_KEY` is a capture key and is
public by design. **No administrative PostHog key may appear in client code**,
and that includes the MCP's.

### The trap that costs an afternoon: the bot filter

**PostHog discards events from an automated browser silently.** There is no
error, `init` finishes, the config downloads, `capture` exists -- and not a
single request goes out. The discard is triggered by `navigator.webdriver`,
which Playwright leaves at `true`.

Verifying PostHog in headless requires hiding it:

```js
await ctx.addInitScript(() => {
  Object.defineProperty(navigator, "webdriver", { get: () => false });
});
```

Without that you waste time reviewing the key, the region, the network and the
code itself, which is exactly what happened. And the `/e/` payload is **gzip**:
`request.postDataBuffer()` and `zlib.gunzipSync`, because `postData()` returns
unreadable binary.

A browser driven straight over CDP sidesteps the whole thing, because
`navigator.webdriver` is only set by the automation flags an automation library
passes. Launching the Chromium that Playwright's cache already has, with
`--remote-debugging-port` and nothing else, reports `navigator.webdriver` as
`false` on its own -- verified, and no property had to be redefined. Under
`xvfb-run` it needs no display of its own.

### Grepping the HTML for `posthog` proves nothing

`curl -s https://kivgraph.dev/ | grep -c posthog` returns **`0`** on a working
deployment, and that is not a symptom. The boot is bundled, so the only thing
the HTML carries is the module that reaches it. The chain is two hops:

```bash
# 1. the shell's second module script is the analytics entry
curl -s https://kivgraph.dev/ | grep -o 'src="/_astro/Layout[^"]*"'

# 2. it imports the chunk that carries the key and the host
curl -s https://kivgraph.dev/_astro/analytics.<hash>.js | grep -o 'phc_[A-Za-z0-9]*'
```

Grepping the served page for the vendor name is the Umami habit -- there the tag
really is in the HTML, `is:inline` -- and it does not transfer. What proves the
deployment is the chunk carrying the `phc_` key and `https://eu.i.posthog.com`,
and after that only the events do.

### A copy event needs a focused window and a real click

`CopyButton.astro` reports in the `then` of `writeText`, so the event only
exists if the clipboard write resolved. A driven browser fails that twice over:
the page has no focus, and a `.click()` from `Runtime.evaluate` is not a user
gesture. The button reads `failed`, no event is sent, and nothing anywhere says
why -- it looks exactly like analytics that do not work.

Three things fix it, and all three are needed:

|what|why|
|---|---|
|`Browser.grantPermissions` with `clipboardReadWrite`|the write is permissioned|
|`Emulation.setFocusEmulationEnabled`|`writeText` rejects without focus|
|`Input.dispatchMouseEvent`|a synthetic `.click()` is not a user gesture|

The check that the copy actually happened is the clipboard, not the label: the
label resets after `1500 ms` and a slow probe reads `copy` again either way.

### Regions: they are not changed

US Cloud and EU Cloud are separate deployments with distinct accounts. Changing
an existing project's region requires a Scale or Enterprise plan and their team
to do it. For a project with no data, the answer is to create it in the EU and
be done.

The way to tell which region a key belongs to, without entering the panel:

```bash
curl -o /dev/null -w '%{http_code}\n' \
  https://eu-assets.i.posthog.com/array/<key>/config.js   # 200 if it is EU
```

### The key that was not this project's

That check is in this document because the host ran the wrong key for a while,
and the wrong key was **valid** -- which is the only reason it cost anything.
`landing/.env` held `phc_BQNTzBtbbV5F...` next to
`PUBLIC_POSTHOG_HOST=https://eu.i.posthog.com`, and the pair answers the
question on its own:

|probe|result|
|---|---|
|`us-assets.i.posthog.com/array/<key>/config.js`|`200`|
|`eu-assets.i.posthog.com/array/<key>/config.js`|`404`|

It is a **US Cloud** key, and it was deployed against the **EU** ingest host.
Nothing in the build, the browser or the panel says so: the library boots, the
key is well formed, and the events go to a deployment that has never heard of
that token. A key being real is not the same as a key being *this project's*,
and only the region probe separates the two.

What the panel cannot answer, and this document will not guess: **which project
owns it.** The MCP authenticates against the EU organisation `kivgraph`, which
holds exactly one project -- `259316`, `phc_rYqAazYSMhxX...`. US Cloud is a
separate deployment with separate accounts, so an EU credential cannot
enumerate it. The owning project is not identifiable from here.

Whether it *received* anything from `kivgraph.dev` is a different question and
it does have an answer: **no**, because the events were never addressed to the
deployment it lives on. They were posted to `eu.i.posthog.com`, which returns
`404` for that token. The US project cannot have been given data this
deployment never sent it. The confirming half -- reading zero events in that
project -- is the half that cannot be run without access to it.

The EU project's own history agrees. Its first event ever is
`2026-08-27T14:51:59Z`, three minutes after the corrected key was written
(`14:48:18`) and rebuilt (`14:48:53`). Before the fix, neither project was
collecting: one was not being written to and the other was not being reached.

The lesson is the ordering, not the incident. **Check the region before
accepting a key, not after wondering why the dashboard is empty** -- an empty
dashboard looks identical to an unvisited site.

## MANUAL SETUP REQUIRED

What follows **cannot be done from the repository**. These are changes in the
Umami dashboard and on the host.

### 1. Goals in the dashboard

Umami does not define goals on the client. On the site, *Settings -> Websites ->
kivgraph -> Goals*, create one per event:

|name|action|value|
|---|---|---|
|`install_copy`|Triggered event|`install_copy`|
|`prompt_copy`|Triggered event|`prompt_copy`|
|`client_connect_copy`|Triggered event|`client_connect_copy`|
|`quickstart_copy`|Triggered event|`quickstart_copy`|
|`mcp_config_copy`|Triggered event|`mcp_config_copy`|

`github_click` is deliberately left **out** of the goals: it is interest, and
counting it as a conversion would inflate the rate with clicks that lead to
installing nothing. It is visible in *Events* all the same without being a goal.

**The goals have to be created after the first event arrives, not before**, and
the reason costs an afternoon if you discover it on your own. With *Triggered
event* the form asks for a second value, and that value is the **event name** --
not the route, even though it looks like it. Umami fills that dropdown with the
events the site has already recorded, so on a site with none it offers only `/`,
which is the only thing in the table. Choosing it creates six goals looking for
an event named `/` that **never count**, while *Events* does add up: the `Name`
field is only the card's label, so the goals look well named and sit at zero. It
has already happened once.

And that same empty dropdown blows the page up with
`TypeError: Cannot read properties of null (reading 'value')`. It is a UI bug,
not a configuration one: the select does
`items?.map(e => typeof e === "object" ? e : {label: e, value: e})` and then
`.map(e => e.value)`, and since `typeof null === "object"` the `null` passes the
guard intact and blows up in the second `map`.

So the order is: deploy, press each copy button once on
`https://kivgraph.dev/`, check in *Events* that the names appear, and only then
create the goals by choosing them from the list.

**Verified on 2026-08-27.** The five goals exist on the main property and all
five carry `{"type": "event", "value": "<the event name>"}` -- not a route. The
trap above did fire when they were first created and was corrected in place:
every one has an `updatedAt` later than its `createdAt`. What the card is
called is not evidence, so the check is the parameters and not the name:

```text
umami_list_reports(websiteId=<main property>, type=goal)
-> parameters.type == "event" and parameters.value == the event name
```

A goal reading `{"type": "url", "value": "/"}` is the failure, and it is
invisible from the dashboard.

A sixth goal existed that this document says should not: **`github_click`**,
created the same afternoon. It pointed at its event correctly, so it counted --
which is the problem, because counting interest as conversion is what inflates
the rate. It was **deleted on 2026-08-27** and the five above are what remains.
No data was lost: the event is still recorded and still visible in *Events*,
which is exactly what the micro level in the table above asks for.

### 1b. The AI crawler property and its variables

*Settings -> Websites -> Add website*, with name `kivgraph.dev - AI Crawlers`
and domain `kivgraph.dev`. Copy the **Website ID** and put it in the **host's**
`landing/.env`, next to the two that are already there:

```env
KIVGRAPH_UMAMI_URL=https://analytics.luqueee.dev
KIVGRAPH_UMAMI_AI_WEBSITE_ID=<the crawler property's id>
```

Never the main property's id: mixing them is exactly what this separation exists
to prevent. Like the site's pair, it **fails closed**: with either one missing
nothing is sent, so in development it is off. `KIVGRAPH_UMAMI_AI_TRACKING=off`
disables it without deleting anything.

These two are read by `server.mjs` at startup; they are **not** build variables,
so there is no need to rebuild in order to change them.

But the first time **`pm2 restart` is not enough, and it fails silently**.
`restart` reuses the process definition pm2 has saved, including the script
path, so it keeps starting the adapter's entry and not `server.mjs`. The site
works, there is no error at all, and not a single agent is detected. The signal
is the startup line: if it says `[@astrojs/node] Server listening on`, the old
entry is running.

```bash
pm2 delete kivgraph-landing
pm2 start landing/ecosystem.config.cjs
pm2 save
pm2 logs kivgraph-landing --lines 5
```

What has to appear is `server.mjs`'s line, in JSON:

```json
{"msg":"kivgraph-landing listening","host":"0.0.0.0","port":6767,"ai_tracking":true}
```

A plain `restart` is fine for everything else; the `delete` is only needed when
the ecosystem's `script` changes, which is exactly what happened when the
detector was added.

And a distinction that costs you if ignored: **the detection log is written
whether or not anything is sent**. Seeing an `ai_agent` line proves the detector
runs, not that Umami received anything; that is decided by `ai_tracking` on the
startup line.

### 1c. The crawler property's reports

Every **report** comes from a single event, `ai_crawler_request`, filtered by
its fields.

|report|filter|what it answers|
|---|---|---|
|**AI Crawlers Overview**|none|breakdown by provider, agent, category and path|
|**Search AI Crawlers**|`category = search`|who is indexing you to cite you|
|**AI User Fetches**|`category = user_fetch`|**what someone asks their assistant**|
|**Training Crawlers**|`category = training`|who collects for training|
|**Provider x page**|`provider = openai` + `path`|what each provider reads|

`user_fetch` says the most: it is human intent arriving through a machine, and
it is what gets compared with the main property's AI referrals.

The **dashboard** is where the per-agent events earn their place, because a
dashboard chart is not a report: *Events* plots one series per event name and
takes no filter on a property. Two charts, and each answers a different
question:

|chart|events|what it answers|
|---|---|---|
|*AI crawler requests*|`ai_crawler_request`|how much AI traffic there is|
|*AI crawler requests by agent*|the eight per-agent names|**who is causing it**|

Dashboard -> Edit -> add an *Events* chart, select the eight agent events, title
it `AI crawler requests by agent`. Typing `ai_crawler_` in the picker lists
every one of them, which is what the shared prefix is for. The `_unknown` three
are left off it on purpose: a series that is flat at zero for months buries the
ones that are not, and an unrecognised agent is a **finding** -- it wants a
registry row, not a line on a chart.

Keep both charts. The first says how much; the second says from whom. And
`Events -> Properties` on `ai_crawler_request` still answers the third, which
neither chart can: *which pages* is that agent reading.

### 1d. AI referrals, in the main property

Nothing new is needed: the referrer is already collected and so are the query
strings. In *Referrers*, filter by `chatgpt.com`, `perplexity.ai`, `claude.ai`,
`gemini.google.com` and `copilot.microsoft.com`; and in the parameters, by
`utm_source=chatgpt.com`, which is what ChatGPT appends to the links it shows.

The correlation being looked for is not computed by Umami, and it does not need
to be:

```text
OAI-SearchBot  ->  /docs/tools/find-by-intent/     (crawler property)
        ...weeks later...
referrer chatgpt.com  ->  /docs/tools/find-by-intent/   (main property)
```

Both halves share the **path**, which is the column they are crossed on by
hand, just as with Search Console.

### 1e. PostHog: project, variables and MCP

The project lives on **Cloud EU**, `https://eu.posthog.com`. Its capture key
goes in the **host's** `landing/.env`:

```env
PUBLIC_POSTHOG_KEY=<the EU project's phc_...>
PUBLIC_POSTHOG_HOST=https://eu.i.posthog.com
```

`PUBLIC_` is not decorative: Astro only exposes variables with that prefix to
the client, and the capture key has to run in the browser. It is a build
variable, so **you have to rebuild**; restarting is not enough.

Before accepting a key, check its region -- a US one on the EU host captures
nothing and gives no visible error:

```bash
curl -o /dev/null -w '%{http_code}\n' \
  https://eu-assets.i.posthog.com/array/<key>/config.js
```

**The MCP authenticates separately, over OAuth, and never with this key.**

```bash
claude mcp add --transport http posthog https://mcp.posthog.com/mcp -s user
```

Check the official documentation before running it in case the command changed.

### 1f. The dashboards

**Umami — SEO** (main property): visitors, organic, referrers, landing pages,
Google, Bing, AI referrals, UTM campaigns, country, device, Web Vitals.

**Umami — AI Crawlers** (second property): provider, agent, category, path,
requests and how they evolve.

**PostHog — Growth**, in four blocks:

|block|what it carries|
|---|---|
|acquisition|traffic by source, top landing pages|
|conversion|primary conversion, by source and by landing|
|product|`install_copy`, `prompt_copy`, `github_click`, `quickstart_copy`|
|UX|funnels, paths, heatmaps, replay, rage and dead clicks|

Funnels that deserve to exist:

```text
landing -> docs -> install_copy
Google  -> landing -> docs -> install_copy
ChatGPT / Claude / Perplexity -> landing -> docs -> install_copy
```

### 1g. AI referrals versus AI crawlers

**They are not the same and must not be mixed.** A *referral* is a person
arriving from `chatgpt.com`, `perplexity.ai`, `claude.ai`, `gemini.google.com`
or `copilot.microsoft.com`, and lives in the main property and in PostHog. A
*crawler* is a machine, and lives only in the crawler property.

The correlation that matters is not computed automatically by anyone, and it
does not need to be:

```text
OAI-SearchBot -> /docs/tools/find-by-intent/     (crawlers)
        ...weeks later...
referrer chatgpt.com -> /docs/tools/find-by-intent/ -> install_copy
```

Both halves share the **path**. That is correlation, not causation, and a report
that confuses them is inventing.

### 1h. An analysis prompt for the MCP

```text
Analyse kivgraph.dev over the last 30 days. Compare Google, Bing, ChatGPT,
Claude, Perplexity, GitHub, Reddit and Direct. For each source: sessions,
landing pages, conversions, install_copy, github_click, paths and device.

Find pages with lots of traffic and poor conversion, pages with little
traffic and high conversion, the highest-intent sources, the drop-off
points and the SEO opportunities.

Use funnels, paths and replays as evidence. Do not confuse correlation with
causation, do not infer Google keywords from PostHog, do not assume that
`direct` is direct access, and do not compare absolute Umami and PostHog
figures without considering that they measure in different ways.
```

### 1i. The first-runs property and its variables

*Settings -> Websites -> Add website*, name `kivgraph FIRST RUNS`, domain
`kivgraph.dev`. Its id goes in the **host's** `landing/.env` beside the others:

```env
KIVGRAPH_UMAMI_FIRST_RUN_WEBSITE_ID=<the first-runs property's id>
```

Never the main property's id, and never the crawler property's: three
questions, three datasets, and an install landing in the site's property moves
visitors, bounce and the conversion rate that describes people. It fails
closed like the other pair -- without the id nothing is forwarded -- and
`KIVGRAPH_UMAMI_FIRST_RUN_TRACKING=off` disables it without deleting anything.

Read by `server.mjs` at startup, so no rebuild is needed to change it. The pm2
warning in **1b** applies unchanged: `restart` reuses the saved process
definition, and the startup line is what says which entry is running.

The reports worth having are one per emitter, because the two must never be
added up: *first runs by platform* filtered to `emitter = binary`, which is
the machines number, and *installs by channel* filtered to
`emitter = installer`, which is the installer's own success rate. A third,
`transport` on the `binary` rows, is the only measurement of how often the
stdio entry ends up serving in process rather than relaying.

### 2. The site's domain in Umami

**Done.** The entry reads `kivgraph.dev`; it used to read
`kivgraph.luqueee.dev`, which is a host that today only redirects. The change
was cosmetic in the sense that matters here -- the tracker does not filter on
that field, so collection never depended on it.

Both properties are distinct entries and their ids are the two the host holds,
which is the part worth re-checking rather than assuming:

```bash
grep -E '^KIVGRAPH_UMAMI(_AI)?_WEBSITE_ID=' landing/.env
```

`KIVGRAPH_UMAMI_WEBSITE_ID` is `kivgraph` and `KIVGRAPH_UMAMI_AI_WEBSITE_ID` is
`kivgraph AI CRAWLERS`. The same id in both is the mistake the separation exists
to prevent, and nothing in the code can catch it.

### 3. Search Console

The domain property's `TXT` record is in place, which is checkable without the
panel:

```bash
dig +short TXT kivgraph.dev | grep google-site-verification
```

`https://kivgraph.dev/sitemap-index.xml` answers `200`. Whether it was
**submitted** is only visible inside Search Console, so it is not asserted
here.

### 4. First-party tracking, if wanted

Today the tracker is served from `analytics.luqueee.dev`. A blocker that
recognises that pattern eats it, and the data is lost entirely. Serving it from
the site's own domain avoids that:

```text
https://kivgraph.dev/u.js      instead of  https://analytics.luqueee.dev/script.js
https://kivgraph.dev/api/u     instead of  https://analytics.luqueee.dev/api/send
```

Umami supports this natively, without patches, with two variables **on the
instance**:

```env
TRACKER_SCRIPT_NAME=u.js
COLLECT_API_ENDPOINT=/api/u
```

plus a reverse proxy on the landing's host that forwards those two routes to the
instance. With Cloudflare in front, an origin rule or a Worker does the same
without touching the host.

Once that is done, the repository does not change: it is enough to point
`KIVGRAPH_UMAMI_SCRIPT_URL` at `https://kivgraph.dev/u.js`. That is the reason
the URL is an environment variable and not a literal.

**It is not done** because it requires touching the instance and the proxy, and
because a half-finished proxy is worse than none: if `/api/u` does not arrive,
100 % of the data is lost instead of the percentage an adblocker blocks today.

## Verification

```bash
# the tracker is emitted, once per page, with both options
curl -s https://kivgraph.dev/ | grep -o '<script[^>]*analytics[^>]*>'

# the events are in the served HTML
curl -s https://kivgraph.dev/ | grep -o 'data-copy-event="[a-z_]*"' | sort -u
curl -s https://kivgraph.dev/ | grep -o 'data-umami-event="[a-z_]*"' | sort -u
```

In the browser, with the console open: copying the install command must produce
**one** request to `/api/send`, not two. And on `localhost` it must produce
none, because `data-domains` does not match.

PostHog is not checked the same way, for the reason two sections above: nothing
of it is in the HTML. What answers the question is the chunk and then the
events.

```bash
# the deployed key is the one intended, and it is an EU key
curl -s https://kivgraph.dev/_astro/analytics.<hash>.js | grep -o 'phc_[A-Za-z0-9]*'
curl -o /dev/null -w '%{http_code}\n' \
  https://eu-assets.i.posthog.com/array/<key>/config.js   # 200 if it is EU
```

Then, in a browser that PostHog will not discard, one load and one copy should
produce `$pageview`, `$autocapture`, `$web_vitals`, `$snapshot`, `$$heatmap` and
the copy event, each answered `200`. `$snapshot` goes to `/s/` and the rest to
`/e/` or `/i/v0/e/`, so a filter on one path alone will miss replay.

The count that matters is one `$pageview` per load. It is worth reading back out
of PostHog rather than off the wire, which also confirms ingestion:

```sql
SELECT event, count() FROM events
WHERE timestamp > now() - INTERVAL 1 DAY GROUP BY event
```

`$snapshot` and `$$heatmap` will not appear there. They land in the replay and
heatmap stores, not in `events`, and their absence from that query is not a
failure to ingest them.
