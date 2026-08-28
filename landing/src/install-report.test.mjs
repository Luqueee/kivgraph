// Tests for the first-run endpoint.
//
// Two of them assert **wire behaviour** against a server that answers like the
// collector, for the reason `ai-report.test.mjs` gives: the header that broke
// production was right in the constant and wrong on the wire, and the
// collector answers `200` to a request it silently discards.
//
// The rest drive the handler over a real socket, because "every path answers
// `204`" is a property of the endpoint and not of a function.

import assert from "node:assert/strict";
import { createServer } from "node:http";
import { after, before, describe, it } from "node:test";

import {
  clientAddress,
  createFirstRunHandler,
  createSender,
  createWindow,
  dedupeKey,
  FIRST_RUN_EVENT,
  FIRST_RUN_PATH,
  firstRunEvent,
  parseFirstRun,
} from "./install-report.mjs";

const binary = {
  emitter: "binary",
  version: "0.9.1",
  platform: "linux-amd64",
  channel: "mcpb",
  transport: "stdio",
};
const installer = {
  emitter: "installer",
  version: "0.9.1",
  platform: "linux-amd64",
  channel: "installer",
};

describe("what is accepted", () => {
  it("takes a binary ping whole", () => {
    assert.deepEqual(parseFirstRun(JSON.stringify(binary)), binary);
  });

  it("takes an installer ping, which carries no transport", () => {
    assert.deepEqual(parseFirstRun(JSON.stringify(installer)), installer);
  });

  it("refuses a transport on an installer row", () => {
    // An installer has not started a server. A transport there would be a
    // default nobody chose, in the field that exists to measure the choice.
    const forged = { ...installer, transport: "daemon" };
    assert.equal(parseFirstRun(JSON.stringify(forged)), null);
  });

  it("refuses a binary ping with no transport", () => {
    const { transport, ...rest } = binary;
    assert.equal(transport, "stdio");
    assert.equal(parseFirstRun(JSON.stringify(rest)), null);
  });

  it("refuses a sixth field rather than ignoring it", () => {
    const extended = { ...binary, repository: "git@github.com:someone/theirs" };
    assert.equal(parseFirstRun(JSON.stringify(extended)), null);
  });

  it("refuses a value outside each closed set", () => {
    for (const [field, value] of [
      ["emitter", "curl"],
      ["platform", "linux-riscv64"],
      ["channel", "brew"],
      ["transport", "http"],
      ["version", "0.9"],
      ["version", "v0.9.1"],
      ["version", "0.9.1-rc.1"],
    ]) {
      const forged = { ...binary, [field]: value };
      assert.equal(
        parseFirstRun(JSON.stringify(forged)),
        null,
        `${field} = ${value} was accepted`,
      );
    }
  });

  it("refuses anything that is not an object of strings", () => {
    for (const text of ["", "null", "[]", '"binary"', "{", '{"emitter":1}']) {
      assert.equal(parseFirstRun(text), null, `${text} was accepted`);
    }
  });
});

describe("whose install it is", () => {
  it("prefers what the edge says, because the socket is the edge", () => {
    const request = {
      headers: {
        "cf-connecting-ip": "203.0.113.7",
        "x-forwarded-for": "198.51.100.1",
      },
      socket: { remoteAddress: "127.0.0.1" },
    };
    assert.equal(clientAddress(request), "203.0.113.7");
  });

  it("takes the first hop of the forwarded chain otherwise", () => {
    const request = {
      headers: { "x-forwarded-for": "203.0.113.7, 10.0.0.1" },
      socket: { remoteAddress: "127.0.0.1" },
    };
    assert.equal(clientAddress(request), "203.0.113.7");
  });

  it("falls back to the socket when nothing forwarded it", () => {
    const request = { headers: {}, socket: { remoteAddress: "203.0.113.9" } };
    assert.equal(clientAddress(request), "203.0.113.9");
  });
});

describe("the window", () => {
  it("keeps the binary row that follows an installer seconds later", () => {
    // The load-bearing one. Both carry the same address and the same version;
    // a key without `emitter` would discard exactly the row the property
    // exists to collect.
    const window = createWindow();
    const at = 1_000;
    assert.equal(
      window.isDuplicate(dedupeKey("203.0.113.7", installer), at),
      false,
    );
    assert.equal(
      window.isDuplicate(dedupeKey("203.0.113.7", binary), at + 2_000),
      false,
    );
  });

  it("drops a repeat of the same machine, version and emitter", () => {
    const window = createWindow();
    assert.equal(
      window.isDuplicate(dedupeKey("203.0.113.7", binary), 0),
      false,
    );
    assert.equal(
      window.isDuplicate(dedupeKey("203.0.113.7", binary), 60_000),
      true,
    );
  });

  it("keeps two machines apart", () => {
    const window = createWindow();
    assert.equal(
      window.isDuplicate(dedupeKey("203.0.113.7", binary), 0),
      false,
    );
    assert.equal(
      window.isDuplicate(dedupeKey("198.51.100.4", binary), 0),
      false,
    );
  });

  it("forgets after the window", () => {
    const window = createWindow({ windowMs: 1_000 });
    assert.equal(window.isDuplicate("k", 0), false);
    assert.equal(window.isDuplicate("k", 1_001), false);
  });

  it("stays bounded when nothing ever expires", () => {
    const window = createWindow({ windowMs: 60_000, maxEntries: 10 });
    for (let index = 0; index < 100; index += 1) {
      window.isDuplicate(`key-${index}`, 0);
    }
    assert.ok(window.size <= 10, `the window grew to ${window.size}`);
  });
});

describe("the collector payload", () => {
  it("names the event and carries the fields flat", () => {
    const event = firstRunEvent(binary, {
      websiteId: "an-id",
      hostname: "kivgraph.dev",
    });
    assert.equal(event.type, "event");
    assert.equal(event.payload.website, "an-id");
    assert.equal(event.payload.name, FIRST_RUN_EVENT);
    assert.equal(event.payload.url, FIRST_RUN_PATH);
    assert.deepEqual(event.payload.data, binary);
  });

  it("carries the address in the body, which is the only place read", () => {
    // Measured against the instance: every address-bearing *header* landed in
    // one session, because Cloudflare rewrites them at the edge. `payload.ip`
    // is what separates one machine from another.
    const event = firstRunEvent(binary, {
      websiteId: "an-id",
      hostname: "kivgraph.dev",
      address: "203.0.113.7",
    });
    assert.equal(event.payload.ip, "203.0.113.7");
  });

  it("omits it entirely when there is no address to send", () => {
    const event = firstRunEvent(binary, {
      websiteId: "an-id",
      hostname: "kivgraph.dev",
    });
    assert.equal("ip" in event.payload, false);
  });
});

// --- over a socket ---------------------------------------------------------

let collector;
let collectorOrigin;
const collected = [];

let endpoint;
let endpointOrigin;
let sent = [];

before(async () => {
  collector = createServer((request, response) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      collected.push({
        headers: request.headers,
        body: Buffer.concat(chunks).toString("utf8"),
      });
      // What the real one answers to a request it discards, which is why the
      // assertions below are on what arrived and never on the reply.
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end('{"beep":"boop"}');
    });
  });
  await new Promise((resolve) => collector.listen(0, "127.0.0.1", resolve));
  collectorOrigin = `http://127.0.0.1:${collector.address().port}`;

  const handle = createFirstRunHandler({
    websiteId: "a-throwaway-property",
    send: (payload, address) => sent.push({ payload, address }),
  });
  endpoint = createServer((request, response) => handle(request, response));
  await new Promise((resolve) => endpoint.listen(0, "127.0.0.1", resolve));
  endpointOrigin = `http://127.0.0.1:${endpoint.address().port}`;
});

after(() => {
  collector.close();
  endpoint.close();
});

async function ping(body, headers = {}) {
  return fetch(`${endpointOrigin}${FIRST_RUN_PATH}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...headers },
    body,
  });
}

describe("every path answers 204", () => {
  it("a ping it accepted", async () => {
    sent = [];
    const response = await ping(JSON.stringify(binary), {
      "X-Forwarded-For": "203.0.113.7",
    });
    assert.equal(response.status, 204);
    assert.equal(sent.length, 1);
    assert.equal(sent[0].address, "203.0.113.7");
  });

  it("a ping it refused", async () => {
    sent = [];
    const response = await ping(JSON.stringify({ emitter: "curl" }), {
      "X-Forwarded-For": "203.0.113.8",
    });
    assert.equal(response.status, 204);
    assert.equal(sent.length, 0);
  });

  it("a body that is not JSON", async () => {
    sent = [];
    const response = await ping("not json at all");
    assert.equal(response.status, 204);
    assert.equal(sent.length, 0);
  });

  it("a body far larger than a ping", async () => {
    sent = [];
    const response = await ping("x".repeat(64 * 1024));
    assert.equal(response.status, 204);
    assert.equal(sent.length, 0);
  });

  it("a method that is not POST", async () => {
    sent = [];
    const response = await fetch(`${endpointOrigin}${FIRST_RUN_PATH}`);
    assert.equal(response.status, 204);
    assert.equal(sent.length, 0);
  });

  it("a repeat, which is indistinguishable from the first", async () => {
    sent = [];
    const first = await ping(JSON.stringify(binary), {
      "X-Forwarded-For": "198.51.100.9",
    });
    const second = await ping(JSON.stringify(binary), {
      "X-Forwarded-For": "198.51.100.9",
    });
    assert.equal(first.status, 204);
    assert.equal(second.status, 204);
    assert.equal(sent.length, 1);
  });
});

describe("what the collector receives", () => {
  it("carries the address in the body and an empty user agent", async () => {
    collected.length = 0;
    const send = createSender({ umamiUrl: collectorOrigin });
    send(
      firstRunEvent(binary, {
        websiteId: "an-id",
        hostname: "kivgraph.dev",
        address: "203.0.113.7",
      }),
    );
    await new Promise((resolve) => setTimeout(resolve, 100));

    assert.equal(collected.length, 1);
    const [received] = collected;
    // Without an address every install on earth is one visitor: the landing
    // server. Measured against the instance, no header carries it -- both ends
    // are behind Cloudflare, which rewrites them at the edge -- so it goes in
    // the payload, which is what the collector reads.
    assert.equal(JSON.parse(received.body).payload.ip, "203.0.113.7");
    // And without an empty user agent the collector's `isbot` filter discards
    // the request while answering 200. The same filter reads a `userAgent` in
    // the payload, so there is not one there either.
    assert.equal(received.headers["user-agent"], "");
    assert.equal("userAgent" in JSON.parse(received.body).payload, false);
    assert.equal(JSON.parse(received.body).payload.name, FIRST_RUN_EVENT);
  });

  it("sends no address at all when there is nothing to attribute", async () => {
    collected.length = 0;
    const send = createSender({ umamiUrl: collectorOrigin });
    send(
      firstRunEvent(binary, { websiteId: "an-id", hostname: "kivgraph.dev" }),
    );
    await new Promise((resolve) => setTimeout(resolve, 100));

    assert.equal(collected.length, 1);
    assert.equal("ip" in JSON.parse(collected[0].body).payload, false);
    assert.equal(collected[0].headers["x-forwarded-for"], undefined);
  });
});
