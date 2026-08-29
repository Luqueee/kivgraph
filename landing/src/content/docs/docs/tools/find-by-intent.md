---
title: find_by_intent
description: Which symbols a plain-language description likely names, and the files to open, when you do not know what anything is called.
---

> Which symbols a plain-language description likely names, and the files to open. Start here when you have no name.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `intent` | string | none | The question, in plain language. Required: an empty or whitespace-only value is rejected with `INVALID_ARGUMENT`. It is a question and not a document, so more than 400 characters is rejected with the instruction to shorten it and pass the vocabulary as `keywords`. |
| `keywords` | array of string | none | Extra terms that extend the question rather than replacing it, and where you supply the vocabulary the code uses when it differs from the vocabulary the question used. At most 16; more is rejected with `INVALID_ARGUMENT`, and so is an empty or whitespace-only entry. There is no thesaurus and no embedding here: the model asking already knows more synonyms than a table would hold. |
| `kind` | string | none | Keeps only candidates of this symbol kind. Compared exactly; surrounding whitespace is rejected. |
| `limit` | integer | `10` | Rows in this page. Must be between 1 and 50. A row is whatever the view spells, so under `view: "files"` it counts files. |
| `path_prefix` | string | none | Keeps only candidates whose repository-relative path starts with this prefix. Compared exactly; surrounding whitespace is rejected. |
| `repo` | string | none | Keeps only candidates belonging to this repository, by name, compared exactly. Surrounding whitespace is rejected. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived `stable_key` to the rows of the `full` view. Anything else is rejected with `INVALID_ARGUMENT`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` lifts into a header what every row shares. `full` is the field-per-row shape. `files` answers only which files hold candidates and how many each holds. Any other value is rejected with `INVALID_ARGUMENT`. |

`repo`, `path_prefix` and `kind` narrow which candidates are *considered*, so
they change the answer rather than trimming it. That is the opposite of the
traversals on this surface: a retrieval has no reachability to preserve, so a
narrower corpus is simply a narrower question.

## Answers

A ranked page of candidates and an account of the terms that produced them. Each
row names a symbol the way every row of this surface does -- repository,
repository-relative path, qualified name and line range -- so the next call is
built from the answer just received, without a key ever appearing.

Two things this answer deliberately withholds. **No score travels**: it orders
candidates inside one answer and means nothing on its own, since scaling every
weight leaves the order identical, so publishing it would invite a reader to
treat it as a confidence this layer cannot claim. And **`match` is on every
row**, because these rows are not like the others on this surface: every other
row this server returns is an edge an analyser resolved, and these are text that
looked alike. They must not be read with the same authority, nor counted in the
same coverage.

## Example

The question, with a page of five. Nothing asks for a `view`, so this is the
`compact` answer a caller gets by default:

```json
{
  "intent": "retry a failed request with exponential backoff",
  "limit": 5
}
```

```json
{
  "snapshot_id": 87,
  "total": 4000,
  "returned": 5,
  "truncated": true,
  "next_cursor": "AlcFcqX-7dppmbTFEY7BKdLJjBhyncQ",
  "guidance": "showing 5 of 4000; narrow with keywords with the identifier words you would guess this code uses, or repo, kind or path_prefix, or pass the cursor for the next page",
  "results": {
    "unmatched_terms": ["exponential"],
    "symbols": [
      {
        "qualified_name": "withRetry",
        "kind": "function",
        "repository": "workspace",
        "file_path": "packages/shared/src/retry.ts",
        "start_line": 135,
        "end_line": 163,
        "terms": 2,
        "match": "lexical+calls"
      },
      {
        "qualified_name": "expBackoff",
        "kind": "function",
        "repository": "workspace",
        "file_path": "packages/shared/src/retry.ts",
        "start_line": 112,
        "end_line": 114,
        "terms": 2,
        "match": "lexical"
      },
      {
        "qualified_name": "expBackoffJitter",
        "kind": "function",
        "repository": "workspace",
        "file_path": "packages/shared/src/retry.ts",
        "start_line": 124,
        "end_line": 128,
        "terms": 2,
        "match": "lexical"
      },
      {
        "qualified_name": "withRetry",
        "kind": "function",
        "repository": "runtime-env",
        "file_path": "src/retry.ts",
        "start_line": 41,
        "end_line": 63,
        "terms": 2,
        "match": "lexical"
      },
      {
        "qualified_name": "connectWithRetry",
        "kind": "function",
        "repository": "media-service",
        "file_path": "src/shared/connect-with-retry.ts",
        "start_line": 40,
        "end_line": 78,
        "terms": 2,
        "match": "lexical"
      }
    ]
  }
}
```

`match` stayed on the rows here because they disagreed: the first row was also
credited for what it calls, and the rest matched text alone. That is the same
unanimous-or-nothing hoist the other compact views use -- one row disagreeing is
enough to push a column back down to every row.

`exponential` is in `unmatched_terms`, and reading it is the point: the index
holds names, qualified names, kinds and paths, not prose, so a word the code
never spells reached nothing. The answer is still good, because the other terms
carried it.

### The `files` view

When the question is which files to open, the symbol rows are noise. The same
question at that granularity:

```json
{
  "intent": "retry a failed request with exponential backoff",
  "view": "files",
  "limit": 5
}
```

```json
{
  "snapshot_id": 87,
  "total": 507,
  "returned": 5,
  "truncated": true,
  "next_cursor": "AlcHcqX-7dppmbTFEY7BKFwcCJNAwr8",
  "guidance": "showing 5 of 507; narrow with keywords with the identifier words you would guess this code uses, or repo, kind or path_prefix, or pass the cursor for the next page",
  "results": {
    "unmatched_terms": ["exponential"],
    "files": [
      { "file": "packages/shared/src/retry.ts", "repo": "workspace", "symbols": 3 },
      { "file": "src/retry.ts", "repo": "runtime-env", "symbols": 1 },
      { "file": "src/shared/connect-with-retry.ts", "repo": "media-service", "symbols": 1 },
      { "file": "src/sdk/types/ModuleResult.ts", "repo": "sdk-types", "symbols": 1 },
      { "file": "app/lib/request-context.server.ts", "repo": "proxy-ui", "symbols": 1 }
    ]
  }
}
```

`total` moved from `4000` to `507` between the two answers and nothing about the
question changed. It counts in the unit the view spells: 4000 candidates, which
sit in 507 files. `limit` counts in that unit too, so a page of five files walks
the same ranking until it has five *files* rather than stopping at five symbols
that turn out to be three.

### Keywords, and the shared header

`keywords` is where you supply the vocabulary you would guess the code uses:

```json
{
  "intent": "where do we cache a value with an expiry",
  "keywords": ["ttl", "evict"],
  "limit": 4
}
```

```json
{
  "snapshot_id": 87,
  "total": 4000,
  "returned": 4,
  "truncated": true,
  "next_cursor": "AlcEkVbPdNyJ1HLFEY7BQKua4YuHibQ",
  "guidance": "showing 4 of 4000; narrow with repo, kind or path_prefix, or ask with view=files first, or pass the cursor for the next page",
  "results": {
    "unmatched_terms": ["we"],
    "match": "lexical",
    "symbols": [
      {
        "qualified_name": "cache::impl::Store::set_value",
        "kind": "method",
        "repository": "media-service",
        "file_path": "src/cache/mod.rs",
        "start_line": 117,
        "end_line": 125,
        "terms": 2,
        "match": ""
      },
      {
        "qualified_name": "cache::codec::serialize_value",
        "kind": "function",
        "repository": "media-service",
        "file_path": "src/cache/codec.rs",
        "start_line": 44,
        "end_line": 74,
        "terms": 2,
        "match": ""
      },
      {
        "qualified_name": "cache::impl::Store::get_value",
        "kind": "method",
        "repository": "media-service",
        "file_path": "src/cache/mod.rs",
        "start_line": 204,
        "end_line": 209,
        "terms": 2,
        "match": ""
      },
      {
        "qualified_name": "cache::impl::Store::hgetall_value",
        "kind": "method",
        "repository": "media-service",
        "file_path": "src/cache/mod.rs",
        "start_line": 261,
        "end_line": 271,
        "terms": 2,
        "match": ""
      }
    ]
  }
}
```

Here all four rows matched text alone, so `match` hoisted into the header and
the rows carry an empty one. A field missing from a row is not a field nobody
knows: the header states it.

`we` is unmatched, and that is the question's grammar rather than a defect. Only
two kinds of term earn a line of their own in `terms`: the one that matched
nothing, which is in `unmatched_terms`, and the one carried by so much of the
corpus that it separated nothing. A term that matched four symbols and produced
the answer needs no line, because the rows *are* the line.

The three answers above come from snapshot `87` of a fifty-repository graph.
That graph is the private benchmark corpus, so repository names, paths and
symbol names are substituted, exactly as the ambiguity refusal on
[`find_references`](/docs/tools/find-references/#one-call-instead-of-two) is.
Everything else -- the counts, the order, `terms`, `match`, `unmatched_terms`,
`guidance` and the cursors -- is what was measured.

## Reading the result

`terms` on a row is how many of the question's terms that candidate carries. It
is a count and not a score: two rows with the same `terms` are ordered by the
ranking, which weights how rare each term is across the corpus, and ties break
on symbol id -- stable-key order, the order every page of this surface uses.
That tie-break is not cosmetic: the cursor pages over this sequence, so two
calls of one question must produce the same sequence or a second page would skip
and repeat rows.

`match` is `lexical` when the candidate matched text alone, and `lexical+calls`
when it was also credited for the terms its callees carry. Neither is an edge
this tool resolved. A row here is a *candidate*, in the sense this project uses
that word everywhere else: plausible and not proven. Confirm it with a tool that
answers from resolved edges -- [`find_symbol`](/docs/tools/find-symbol/) to
locate the declaration, [`find_references`](/docs/tools/find-references/) to
learn who calls it, [`get_source`](/docs/tools/get-source/) to read it.

`unmatched_terms` lists the words of the question that appear in no name,
qualified name, kind or path. On a good answer it is usually the grammar. On an
empty one it is the whole diagnosis.

## The three empty answers, which are not the same

An empty page from this tool means one of three different things, and the
`guidance` says which. Confusing them is how a caller concludes that code does
not exist when the question simply missed.

**Nothing matched.** No word of the question appears anywhere in the index:

```json
{
  "snapshot_id": 2,
  "total": 0,
  "returned": 0,
  "guidance": "no word of this question appears in any name, qualified name, kind or path of the graph; the index holds no prose, so rephrase with the vocabulary the code would use, or pass keywords",
  "results": {
    "unmatched_terms": ["read", "the", "configuration", "file"],
    "symbols": []
  }
}
```

Every term is in `unmatched_terms`, which is the instruction: the index holds
identifiers, not prose. Rephrase in the vocabulary the code would use, or pass
`keywords`.

**The terms matched, and the narrowing excluded every candidate.** A different
answer with the same row count:

```json
{
  "snapshot_id": 87,
  "total": 0,
  "returned": 0,
  "guidance": "the terms matched symbols, but every one of them was excluded by repo, kind or path_prefix; widen the narrowing",
  "results": { "symbols": [] }
}
```

Nothing is unmatched here. The question was fine and the filter was wrong.

**And the answer no retrieval can give: a proven absence.** This tool never
gives one. A page of zero rows is "I found nothing that looks like this", never
"this does not exist" -- text that did not match is not evidence. The tools that
can prove an absence are the ones backed by an analyser;
[`find_references`](/docs/tools/find-references/) with a `COMPLETE` verdict is
the one that answers "nobody calls this".

## Limits

`total` saturates at `4000`. That is the candidate bound, not a count of the
graph: a question whose terms are carried by more of the corpus than that stops
accumulating, and both example pages above sit exactly on it. A `total` of
`4000` should be read as "at least this many, and the question is too broad to
rank well" -- narrow it with `keywords`, `repo`, `kind` or `path_prefix`, or ask
with `view: "files"` first, which is what the `guidance` on those pages says.

`limit` is capped at 50, well under the 500 of the traversal tools. Ranked text
candidates past the first page are rarely the answer; the fix for a page that
did not contain what you wanted is a better question, not a longer page.

A question with more than 32 terms still answers. Its later terms simply earn no
credit for what a candidate's callees carry, which is the cheap end of a bound
that keeps one question from walking the graph.

`truncated` is `true` when rows remain, and `next_cursor` then carries the token
to continue. It is opaque, about 31 characters, and pages over the ranking
described above. `limit`, `view` and `response_format` are not part of the
cursor identity, so changing one of them mid-pagination is accepted; changing
the `intent`, the `keywords` or any of the three filters is not, and yields
`CURSOR_INVALID`. A cursor minted against a generation that has since been
replaced yields `CURSOR_SNAPSHOT_EXPIRED`.

Like every tool on this surface, it answers from the published HotSnapshot: if
the tree moved since it was indexed, the answer describes the code that was
indexed. [`graph_status`](/docs/tools/graph-status/) reports that.

## Where it loses

Everywhere the name is already known. If you can spell the symbol,
[`find_symbol`](/docs/tools/find-symbol/) resolves it and costs no ranking; if
you can spell a rare string, `grep` is cheaper than any call here. This tool
earns its place on exactly one question -- *I do not know what this is called* --
and its answer is the input to a resolved one, never the conclusion.
