// The IndexNow key for kivgraph.dev.
//
// IndexNow proves ownership with a **file**, not a token: `<key>.txt` at the
// site root, containing exactly the key and nothing else. Anyone may read it,
// which is the whole mechanism -- fetching it proves whoever submitted URLs
// controls the host. So this is public by construction, and committed rather
// than kept in a secret, for the same reason the Umami website id is.
//
// Plain ESM like `site.mjs` beside it: the consumer is
// `scripts/indexnow-submit.sh`, driving a bare Node process that Vite never
// transforms.
//
// **The file name and this value have to stay equal.** They are two facts in
// two places, and when they drift IndexNow answers a submission with a `403`
// that is indistinguishable from a key that was simply never valid.
// `indexnow.test.mjs` is what keeps them from drifting.
export const INDEXNOW_KEY =
  "46cedbe0753aa0e731e78d72276b1a663104e90af6776ceeded8f9fe18731a45";

// Where the key file is served from, derived rather than written twice.
export function indexNowKeyPath() {
  return `/${INDEXNOW_KEY}.txt`;
}
