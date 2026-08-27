// The IndexNow key is one fact stored in two places -- a constant and a file
// whose *name* is that constant -- and nothing at build or request time
// compares them. When they drift, submissions are answered `403`, which is the
// same answer a key that was never valid gets, so the failure says nothing
// about its own cause.
//
// These assertions are the comparison nobody else makes.

import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

import { INDEXNOW_KEY, indexNowKeyPath } from "./indexnow.mjs";

const publicDir = fileURLToPath(new URL("../public/", import.meta.url));

describe("the IndexNow key", () => {
  it("is 8 to 128 hexadecimal characters, which is all the protocol accepts", () => {
    assert.match(INDEXNOW_KEY, /^[0-9a-f]{8,128}$/);
  });

  it("is served by a file named after itself", () => {
    const file = new URL(`../public/${INDEXNOW_KEY}.txt`, import.meta.url);
    assert.ok(
      existsSync(file),
      `landing/public/${INDEXNOW_KEY}.txt does not exist`,
    );
  });

  it("is the entire contents of that file", () => {
    const contents = readFileSync(
      new URL(`../public/${INDEXNOW_KEY}.txt`, import.meta.url),
      "utf8",
    );
    // Not `trim()`: a trailing newline is content, and the verifier compares
    // the body to the key. Asserting the exact bytes is the point.
    assert.equal(contents, INDEXNOW_KEY);
  });

  it("is the only key file, so a rotated key leaves nothing behind", () => {
    const keyFiles = readdirSync(publicDir).filter((name) =>
      /^[0-9a-f]{8,128}\.txt$/.test(name),
    );
    assert.deepEqual(keyFiles, [`${INDEXNOW_KEY}.txt`]);
  });

  it("derives its served path rather than writing it twice", () => {
    assert.equal(indexNowKeyPath(), `/${INDEXNOW_KEY}.txt`);
  });
});
