// The headers the AI crawler reporter sends to Umami's collector, in their own
// module for one reason: the value below is a load-bearing invariant that used
// to live inline in `landing/server.mjs`, where nothing could test it, and it
// was wrong for a day without a single symptom.
//
// Plain ESM for the same reason as `ai-agents.mjs`: the consumer is
// `landing/server.mjs`, a bare Node process Vite never transforms.
//
// **The `User-Agent` must be the empty string, and it must be present.** Both
// halves of that sentence are the finding, and each one fails differently.
//
// Umami's collector runs `isbot` over the *sender's* own `User-Agent` and
// silently discards the request when it matches. The inline value used to be
// `kivgraph-landing/1.0 (+https://kivgraph.dev)`, chosen to be neutral and
// honest, on the assumption that the filter rejects strings that look like a
// crawler. It does not: `isbot` treats anything that is not a recognisable
// browser as a bot. Measured against the instance itself, on a throwaway
// property, every honest identifier was dropped -- `kivgraph-landing/1.0`,
// `kivgraph-landing`, `KivgraphLanding`, `Kivgraph Landing Server`, `kivgraph`
// -- and the empty string was the only value that was recorded.
//
// And it fails **invisibly**, which is why it survived. Umami answers `200`
// with `{"beep":"boop"}` and writes nothing, so the reporter's `catch` has no
// error to catch, the local detection log is written either way, and
// `ai_tracking` on the startup line still reports `true`. Nothing anywhere says
// the events are being thrown away.
//
// Deleting this header instead of emptying it looks like the same fix and is
// not: `undici` substitutes its own `User-Agent: node` when a request carries
// none, and `node` is a string `isbot` matches. The empty value has to be
// written down. `ai-report.test.mjs` asserts the wire behaviour of both.
//
// The alternative was `DISABLE_BOT_CHECK` on the Umami instance, and it is the
// wrong one: it is global, so it would drop the filter on the main property
// too, which is the contamination this whole two-property design exists to
// avoid. An empty `User-Agent` claims nothing and impersonates nobody; the
// agent being reported travels as normalised metadata in the event payload,
// where it belongs.
export const REPORTER_HEADERS = Object.freeze({
  "Content-Type": "application/json",
  "User-Agent": "",
});
