// Tests for the AI agent registry, run by Node's own test runner so the landing
// gains a suite without gaining a dependency: `node --test`.
//
// The negatives come first, deliberately, because they are the ones that decide
// whether the second dataset means anything. A registry that files `Googlebot`
// or `curl` as an AI agent does not produce a slightly noisy report -- it
// produces a report that answers no question, because "AI traffic" would then
// mean "traffic".

import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  AI_AGENTS,
  CATEGORIES,
  PROVIDERS,
  detectAiAgent,
} from "./ai-agents.mjs";

/**
 * User agents copied from the operators' own documentation rather than
 * shortened to the token. The real strings are what arrives, and OpenAI's carry
 * a whole Chrome user agent in front of the part that identifies them -- which
 * is the case a naive matcher gets wrong in the direction that matters, by
 * filing a crawler as a human.
 */
const OFFICIAL = {
  "oai-searchbot":
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36; compatible; OAI-SearchBot/1.4; +https://openai.com/searchbot",
  "oai-searchbot-robots":
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36; compatible; OAI-SearchBot/1.4; robots.txt; +https://openai.com/searchbot",
  "chatgpt-user":
    "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot",
  gptbot:
    "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.4; +https://openai.com/gptbot",
  perplexitybot:
    "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)",
  "perplexity-user":
    "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)",
  claudebot:
    "Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)",
  "claude-user":
    "Mozilla/5.0 (compatible; Claude-User/1.0; +Claude-User@anthropic.com)",
  "claude-searchbot":
    "Mozilla/5.0 (compatible; Claude-SearchBot/1.0; +Claude-SearchBot@anthropic.com)",
};

describe("what must never be classified as an AI agent", () => {
  // A generic crawler is not an AI agent. Googlebot is the one that costs the
  // most to get wrong: it is the highest-volume bot most sites see, and filing
  // it here would drown every real signal in the second dataset.
  const notAi = [
    [
      "Googlebot",
      "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
    ],
    ["Googlebot-Image", "Googlebot-Image/1.0"],
    [
      "Bingbot",
      "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
    ],
    [
      "Discordbot",
      "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)",
    ],
    ["curl", "curl/8.14.1"],
    ["wget", "Wget/1.21.3"],
    [
      "Chrome",
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    ],
    [
      "Safari on iPhone",
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
    ],
    [
      "an unknown scraper",
      "Mozilla/5.0 (compatible; SomeScraper/1.0; +http://example.com)",
    ],
    ["an empty string", ""],
  ];

  for (const [name, ua] of notAi) {
    it(`${name} is not an AI agent`, () => {
      assert.equal(detectAiAgent(ua), null);
    });
  }

  it("a missing user agent is not an AI agent", () => {
    assert.equal(detectAiAgent(undefined), null);
    assert.equal(detectAiAgent(null), null);
  });

  // `Google-Extended` is a robots.txt token and not an HTTP user agent, so it
  // cannot arrive on a request. If it ever does, it is somebody imitating a
  // string Google does not send, and inventing Gemini traffic out of it would
  // be reporting a number that never happened.
  it("Google-Extended is not treated as traffic", () => {
    assert.equal(detectAiAgent("Google-Extended"), null);
  });
});

describe("the documented agents", () => {
  const expected = [
    ["oai-searchbot", "openai", "OAI-SearchBot", CATEGORIES.SEARCH],
    ["oai-searchbot-robots", "openai", "OAI-SearchBot", CATEGORIES.SEARCH],
    ["chatgpt-user", "openai", "ChatGPT-User", CATEGORIES.USER_FETCH],
    ["gptbot", "openai", "GPTBot", CATEGORIES.TRAINING],
    ["claude-searchbot", "anthropic", "Claude-SearchBot", CATEGORIES.SEARCH],
    ["claude-user", "anthropic", "Claude-User", CATEGORIES.USER_FETCH],
    ["claudebot", "anthropic", "ClaudeBot", CATEGORIES.TRAINING],
    ["perplexitybot", "perplexity", "PerplexityBot", CATEGORIES.SEARCH],
    ["perplexity-user", "perplexity", "Perplexity-User", CATEGORIES.USER_FETCH],
  ];

  for (const [key, provider, agent, category] of expected) {
    it(`${agent} is ${provider}/${category}`, () => {
      const got = detectAiAgent(OFFICIAL[key]);
      assert.ok(got, `${key} was not detected`);
      assert.equal(got.provider, provider);
      assert.equal(got.agent, agent);
      assert.equal(got.category, category);
    });
  }

  // The three Anthropic tokens share the word `Claude`, and `ClaudeBot` is the
  // one a looser rule would swallow the other two into. The registry is ordered
  // specific-first; this is the test that fails if somebody reorders it.
  it("Claude-SearchBot is not filed as ClaudeBot", () => {
    assert.equal(
      detectAiAgent(OFFICIAL["claude-searchbot"]).agent,
      "Claude-SearchBot",
    );
    assert.equal(detectAiAgent(OFFICIAL["claude-user"]).agent, "Claude-User");
  });

  // OpenAI's strings contain a complete Chrome user agent. A matcher that
  // checked for a browser first would file all three as human traffic.
  it("an OpenAI agent is not read as Chrome", () => {
    for (const key of ["oai-searchbot", "chatgpt-user", "gptbot"]) {
      assert.ok(
        OFFICIAL[key].includes("AppleWebKit"),
        "fixture lost its browser prefix",
      );
      assert.ok(detectAiAgent(OFFICIAL[key]), `${key} fell through`);
    }
  });
});

describe("an operator this file knows sending an agent it does not", () => {
  const cases = [
    [
      "OpenAI",
      "Mozilla/5.0 (compatible; OAI-FutureBot/1.0; +https://openai.com/futurebot)",
      "openai",
    ],
    [
      "Anthropic",
      "Mozilla/5.0 (compatible; Claude-Something/1.0; +anthropic.com)",
      "anthropic",
    ],
    [
      "Perplexity",
      "Mozilla/5.0 (compatible; PerplexityFuture/2.0)",
      "perplexity",
    ],
  ];

  for (const [name, ua, provider] of cases) {
    it(`${name} lands in unknown_ai, not in a wrong category`, () => {
      const got = detectAiAgent(ua);
      assert.ok(got);
      assert.equal(got.provider, provider);
      assert.equal(got.category, CATEGORIES.UNKNOWN);
      // The raw string is deliberately not carried through: it is unbounded and
      // attacker-controlled, and an analytics dimension is the wrong place for
      // either property.
      assert.equal(got.agent, "unknown");
    });
  }
});

describe("the registry itself", () => {
  it("every agent names a provider that exists", () => {
    for (const entry of AI_AGENTS) {
      assert.ok(
        PROVIDERS[entry.provider],
        `${entry.id} names no known provider`,
      );
    }
  });

  it("every id is unique", () => {
    const ids = AI_AGENTS.map((entry) => entry.id);
    assert.equal(new Set(ids).size, ids.length);
  });

  it("every category is one of the four", () => {
    const known = new Set(Object.values(CATEGORIES));
    for (const entry of AI_AGENTS) {
      assert.ok(
        known.has(entry.category),
        `${entry.id} has category ${entry.category}`,
      );
    }
  });

  // A row that cannot be re-verified is a guess reporting itself as a fact, so
  // the source and the date it was checked are part of the contract.
  it("every provider cites a source and the date it was checked", () => {
    for (const provider of Object.values(PROVIDERS)) {
      assert.match(provider.source, /^https:\/\//);
      assert.match(provider.checkedOn, /^\d{4}-\d{2}-\d{2}$/);
    }
  });
});
