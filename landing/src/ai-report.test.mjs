// Tests for the headers the crawler reporter sends, run by Node's own test
// runner like the registry's suite beside it.
//
// These assert **wire behaviour**, not the constant. Asserting the object would
// have passed for the value that was broken in production, and it would pass
// again for the one refactor most likely to reintroduce the bug -- deleting a
// header that looks redundant, which is exactly when `undici` substitutes its
// own. What has to hold is what the collector actually receives.

import assert from "node:assert/strict";
import { createServer } from "node:http";
import { after, before, describe, it } from "node:test";

import { REPORTER_HEADERS } from "./ai-report.mjs";

/** Captures the headers of one request and answers, like the collector does. */
let server;
let origin;
const received = [];

before(async () => {
  server = createServer((request, response) => {
    received.push(request.headers);
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end("{}");
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
});

after(() => server.close());

describe("the reporter's user agent", () => {
  it("arrives as the empty string, and arrives at all", async () => {
    received.length = 0;
    await fetch(`${origin}/api/send`, {
      method: "POST",
      headers: REPORTER_HEADERS,
      body: "{}",
    });

    assert.equal(received.length, 1);
    // Present is half of it: an absent header is not an empty one.
    assert.ok("user-agent" in received[0]);
    assert.equal(received[0]["user-agent"], "");
  });

  it("is not something Umami's filter would discard", () => {
    // The strings measured against the live instance, every one dropped. This
    // is the list a future edit has to stay off, not a style preference.
    const dropped = [
      "kivgraph-landing/1.0 (+https://kivgraph.dev)",
      "kivgraph-landing/1.0",
      "kivgraph-landing",
      "KivgraphLanding",
      "Kivgraph Landing Server",
      "kivgraph",
      "node",
    ];
    assert.ok(!dropped.includes(REPORTER_HEADERS["User-Agent"]));
  });

  it("cannot be fixed by omitting the header, which is why it is written down", async () => {
    received.length = 0;
    await fetch(`${origin}/api/send`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });

    // `undici` supplies its own when the caller sends none, and `isbot` matches
    // it. This test exists so that deleting the header fails here rather than
    // in production, where it produces no error at all.
    assert.equal(received[0]["user-agent"], "node");
  });

  it("still declares the payload as JSON", () => {
    assert.equal(REPORTER_HEADERS["Content-Type"], "application/json");
  });
});
