// The registry of AI agents this site recognises, and the one function that
// classifies a request against it.
//
// Plain ESM and not TypeScript on purpose, for the same reason `site.mjs` is:
// the consumer is `landing/server.mjs`, a bare Node process that runs the built
// output and is never transformed by Vite. A `.ts` file could not be imported
// there without adding a build step for one module.
//
// **Every identifier below was read from its operator's own documentation**, and
// the source is beside it with the date it was checked. A user agent copied
// from a blog post or an aggregated list does not go in: the whole value of this
// file is that a row can be re-verified, and a row nobody can verify is a guess
// that reports itself as a fact.
//
// The shape of a real user agent matters more than it looks. OpenAI's agents
// carry a **complete Chrome user agent** with the distinguishing token appended:
//
//   Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML,
//   like Gecko) Chrome/131.0.0.0 Safari/537.36; compatible; OAI-SearchBot/1.4;
//   +https://openai.com/searchbot
//
// so a rule that tests for `Chrome` before testing for the token would file
// every one of them as a human, and a rule anchored at the start of the string
// would match none of them. The patterns therefore match the token wherever it
// appears, and nothing else about the string is load-bearing.

/**
 * What an agent is for, which is the question a report actually asks. The four
 * are deliberately few: a category nobody filters by is a column nobody reads.
 *
 * - `search`   indexes the site so it can be cited in an AI answer engine.
 * - `user_fetch` fetches a URL **because a person asked their assistant about
 *   it**. This is the interesting one: it is a human intent arriving through a
 *   machine, and it is the closest thing to a referral that a crawler produces.
 * - `training` collects content for model training.
 * - `unknown_ai` is an agent that self-identifies as an AI operator this file
 *   knows, in a form it does not yet have a row for.
 */
export const CATEGORIES = Object.freeze({
  SEARCH: "search",
  USER_FETCH: "user_fetch",
  TRAINING: "training",
  UNKNOWN: "unknown_ai",
});

/**
 * The operators, each with the page its rows were read from and the date they
 * were checked. Re-verifying is opening these five URLs.
 *
 * `verification` is what that operator publishes for proving an agent is
 * genuine rather than someone spoofing the user agent, which anybody can do:
 *
 * - `ip_ranges` -- a JSON file of CIDR blocks the agent connects from.
 * - `none` -- the operator publishes no mechanism, so a claim stays a claim.
 */
export const PROVIDERS = Object.freeze({
  openai: {
    id: "openai",
    label: "OpenAI",
    source: "https://developers.openai.com/api/docs/bots",
    checkedOn: "2026-08-27",
    verification: "ip_ranges",
    // One file per agent, which is why the row carries its own.
    ipRanges: {
      "oai-searchbot": "https://openai.com/searchbot.json",
      gptbot: "https://openai.com/gptbot.json",
      "chatgpt-user": "https://openai.com/chatgpt-user.json",
    },
  },
  anthropic: {
    id: "anthropic",
    label: "Anthropic",
    source:
      "https://support.claude.com/en/articles/8896518-does-anthropic-crawl-data-from-the-web-and-how-can-site-owners-block-the-crawler",
    checkedOn: "2026-08-27",
    // The article documents the three agents and what each is for; it publishes
    // no address list, so a Claude row cannot be verified beyond its own claim.
    verification: "none",
    ipRanges: {},
  },
  perplexity: {
    id: "perplexity",
    label: "Perplexity",
    source: "https://docs.perplexity.ai/docs/resources/perplexity-crawlers",
    checkedOn: "2026-08-27",
    verification: "ip_ranges",
    ipRanges: {
      perplexitybot: "https://www.perplexity.com/perplexitybot.json",
      "perplexity-user": "https://www.perplexity.com/perplexity-user.json",
    },
  },
});

/**
 * The agents themselves. Adding one is adding a row here and a case to
 * `ai-agents.test.mjs`; nothing else in the codebase names an agent.
 *
 * `pattern` matches the token anywhere in the user agent, case-insensitively.
 * Order matters in exactly one place and it is written down where it does.
 */
export const AI_AGENTS = Object.freeze([
  // --- OpenAI ---------------------------------------------------------------
  // `OAI-SearchBot` must be tested before any looser OpenAI rule: its string
  // contains neither `GPTBot` nor `ChatGPT-User`, but keeping the three ordered
  // by specificity is what stops a future looser row from swallowing it.
  {
    id: "oai-searchbot",
    provider: "openai",
    agent: "OAI-SearchBot",
    category: CATEGORIES.SEARCH,
    pattern: /OAI-SearchBot/i,
  },
  {
    // Fetches a page because someone asked ChatGPT about it. OpenAI's own
    // documentation is explicit that it "is not used for crawling the web in an
    // automatic fashion", which is exactly why it belongs in `user_fetch` and
    // not in `search`.
    id: "chatgpt-user",
    provider: "openai",
    agent: "ChatGPT-User",
    category: CATEGORIES.USER_FETCH,
    pattern: /ChatGPT-User/i,
  },
  {
    id: "gptbot",
    provider: "openai",
    agent: "GPTBot",
    category: CATEGORIES.TRAINING,
    pattern: /GPTBot/i,
  },

  // --- Anthropic ------------------------------------------------------------
  // `Claude-SearchBot` and `Claude-User` both contain `Claude`, and `ClaudeBot`
  // is a prefix of neither, so the three are unambiguous. They are still
  // ordered specific-first for the same reason as OpenAI's.
  {
    id: "claude-searchbot",
    provider: "anthropic",
    agent: "Claude-SearchBot",
    category: CATEGORIES.SEARCH,
    pattern: /Claude-SearchBot/i,
  },
  {
    id: "claude-user",
    provider: "anthropic",
    agent: "Claude-User",
    category: CATEGORIES.USER_FETCH,
    pattern: /Claude-User/i,
  },
  {
    id: "claudebot",
    provider: "anthropic",
    agent: "ClaudeBot",
    category: CATEGORIES.TRAINING,
    pattern: /ClaudeBot/i,
  },

  // --- Perplexity -----------------------------------------------------------
  // `Perplexity-User` before `PerplexityBot`: the two are distinct tokens, but
  // a `/Perplexity/i` rule added later would match both, and the order decides
  // which wins when that happens.
  {
    id: "perplexity-user",
    provider: "perplexity",
    agent: "Perplexity-User",
    category: CATEGORIES.USER_FETCH,
    pattern: /Perplexity-User/i,
  },
  {
    id: "perplexitybot",
    provider: "perplexity",
    agent: "PerplexityBot",
    category: CATEGORIES.SEARCH,
    pattern: /PerplexityBot/i,
  },
]);

/**
 * Operators whose name in a user agent means an AI agent of some kind, even
 * when the specific agent has no row above. A match here is `unknown_ai`: it
 * says "OpenAI sent something this file does not recognise", which is a
 * finding rather than a classification, and it is the signal that the registry
 * needs a new row.
 *
 * These are deliberately narrow. A generic crawler is **not** AI: `Googlebot`,
 * `Bingbot`, `Discordbot`, `curl` and a browser all fall through to `null`, and
 * turning every bot into an AI bot is the failure that would make the whole
 * second dataset meaningless.
 *
 * `Google-Extended` is absent on purpose and the absence is the point. It is a
 * `robots.txt` token that controls whether Google may use already-crawled
 * content for Gemini training; Google's crawler documentation lists no HTTP
 * user agent for it, so it never appears in a request and inventing Gemini
 * traffic from it would be fabricating a number.
 * https://developers.google.com/crawling/docs/crawlers-fetchers/google-common-crawlers
 */
const UNKNOWN_AI_HINTS = Object.freeze([
  { provider: "openai", pattern: /\bOAI-|\bOpenAI\b/i },
  { provider: "anthropic", pattern: /\bAnthropic\b|\bClaude[-\s]/i },
  // No trailing `\b`: it would refuse `PerplexityFuture`, which is exactly the
  // shape an unrecognised agent from a known operator takes.
  { provider: "perplexity", pattern: /\bPerplexity/i },
]);

/**
 * Classifies a user agent string.
 *
 * Returns `null` for everything this file does not recognise, which is the
 * common case and the one that must stay cheap: a browser, `curl`, `Googlebot`
 * and an unknown scraper all return `null` and cost one pass over eight
 * regular expressions.
 *
 * @param {string | undefined | null} userAgent
 * @returns {{
 *   id: string, provider: string, agent: string, category: string,
 *   verification: string,
 * } | null}
 */
export function detectAiAgent(userAgent) {
  if (typeof userAgent !== "string" || userAgent.length === 0) {
    return null;
  }

  for (const entry of AI_AGENTS) {
    if (entry.pattern.test(userAgent)) {
      return {
        id: entry.id,
        provider: entry.provider,
        agent: entry.agent,
        category: entry.category,
        verification: PROVIDERS[entry.provider].verification,
      };
    }
  }

  for (const hint of UNKNOWN_AI_HINTS) {
    if (hint.pattern.test(userAgent)) {
      return {
        id: `${hint.provider}-unknown`,
        provider: hint.provider,
        // The agent is unknown by definition, and putting the raw user agent
        // here would push an unbounded attacker-controlled string into an
        // analytics dimension. The provider is what a reader can act on.
        agent: "unknown",
        category: CATEGORIES.UNKNOWN,
        verification: PROVIDERS[hint.provider].verification,
      };
    }
  }

  return null;
}
