// What the AI crawler reporter sends to Umami's collector -- the headers and
// the payloads -- in its own module for one reason: `landing/server.mjs` cannot
// be imported without a built `dist/server/entry.mjs`, so anything inline there
// is code no test can reach. The header below is the proof: it lived inline, it
// was wrong for a day, and nothing anywhere said so.
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

/**
 * The events one detected request produces, in the order they are sent.
 *
 * This lives here rather than inline in `server.mjs` for the same reason the
 * header above does: `server.mjs` needs a built `dist/server/entry.mjs` to be
 * imported at all, so anything written inline there is code no test can reach.
 * The header that used to be inline was wrong for a day without a symptom.
 *
 * **Two events, always both or neither.** The first is the rich one and it is
 * unchanged: it carries every field, and it is what you filter and cross by
 * hand. The second is the same request under the agent's own event name, and it
 * exists only because Umami's *Events* chart plots one series per event name
 * and cannot split a single event by one of its properties -- so "which agent
 * caused that spike" is a question the rich event cannot draw, however complete
 * it is.
 *
 * The second carries `provider`, `category` and `verified` and stops there.
 * `path`, `method` and `status` are already on the row it pairs with, and a
 * second copy is a second place for them to be wrong.
 *
 * The caller is responsible for the dedupe decision, and it must apply to the
 * pair: reporting one without the other would let the two charts disagree about
 * how many requests there were, which is worse than not drawing the second.
 *
 * The event name comes from the registry and never from the request. Anything
 * the registry does not recognise fails `detectAiAgent` and reaches no caller
 * here, so a `User-Agent` of someone's choosing cannot mint an event name.
 *
 * @param {{
 *   detected: { provider: string, agent: string, category: string,
 *               event: string },
 *   website: string, hostname: string, pathname: string,
 *   method: string | undefined, status: number,
 * }} request
 * @returns {Array<object>} payloads for Umami's `/api/send`
 */
export function crawlerEventPayloads({
  detected,
  website,
  hostname,
  pathname,
  method,
  status,
}) {
  const envelope = { website, hostname, url: pathname };
  return [
    {
      type: "event",
      payload: {
        ...envelope,
        name: "ai_crawler_request",
        data: {
          provider: detected.provider,
          agent: detected.agent,
          category: detected.category,
          path: pathname,
          method,
          status,
          verified: false,
        },
      },
    },
    {
      type: "event",
      payload: {
        ...envelope,
        name: detected.event,
        data: {
          provider: detected.provider,
          category: detected.category,
          verified: false,
        },
      },
    },
  ];
}
