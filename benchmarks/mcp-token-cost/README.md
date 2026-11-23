# mcp-token-cost

What a coding session costs with Kivgraph and what the same session costs
with the host's own `grep` and file reads, for one question: who calls this
symbol, and what do those callers look like. Tokens are `cl100k_base`.

The numbers are in `report.md` and `results.json`. This file is how to
produce them again, because two parts of the harness cannot be run by a
program alone.

## What cannot be generated

The native arm is **the host's own tool output, captured verbatim**: a
`grep` of the symbol, and the range reads the agent then makes. A native arm
written by us would be an imitation, and an imitation is always worse than
the tool it stands in for, which biases the comparison our way. So
`native/*.grep.txt` and `native/reads.json` are produced by an agent running
in the host being measured, with that host's tools. They are tied to the
commit: a range that moved is a range that was not captured, and the run
stops and names every one of them.

## The corpus

The repository is indexed as its own documentation says, and not with the
defaults, because the defaults make its own fixtures unreadable scopes and the
`completeness` block of every answer says so (ADR 0156 measures what that
costs): the `ladybug` build tag, and `testdata/` left out.

```bash
export HOME=$(mktemp -d)   # never your own: this is an index of a scratch copy
kivgraph init --repository kivgraph=/path/to/kivgraph --languages go,typescript
# config.yaml:       go.build_tags: [ladybug]
# repositories.yaml: exclusions: [testdata]   (under the kivgraph repository)
kivgraph index --full
```

Use a binary built with the `ladybug` tag (see the root `AGENTS.md`). Do not run
`kivgraph daemon install` from that `HOME`: it refuses, and for a reason.

## Run

```bash
go run ./benchmarks/mcp-token-cost --server /path/to/kivgraph
```

The first run on a new commit stops with the list of ranges it needs, one
`path:start-end` per line. That list is the whole recapture.

## Recapture

1. Retake the `grep` of each question in `questions.json` with the host, with
   the pattern and path it names, into the `capture` file it names.
2. Read each listed range with the host's `read`, as an agent would
   (`path:start-end`). Keep the text exactly as the host returned it.
3. Count the texts. `count` reads `{"path:start-end": "text", ...}` on stdin
   and prints `{"path:start-end": tokens, ...}`, with the harness's own
   encoding:

   ```bash
   go run ./benchmarks/mcp-token-cost/count < texts.json
   ```

4. Add each as `"path:start-end": {"file": "path", "tokens": N}` to
   `native/reads.json`, and run the harness again until it stops asking.

In Oh My Pi the whole of 1-4 is one cell of `eval`: `tool.grep({pattern, path})`
and `tool.read({path: "file:a-b"})` return the host's text in the `text` of the result; the rest
is the counter above. Retake **every** range and not only the missing ones: a
file that kept its range can still have changed its lines.

## Reading the result

Both factors are published on purpose. The answer factor is the part a graph
server owns, and it flatters the graph. The session factor adds the bodies the
agent then opens, which both arms pay alike, and it flatters `grep`. The
ceiling is what the bodies alone allow: an answer that cost nothing would still
land there.

Neither latency nor money is measured. The cross-repository question lives in
`cross-repo/` and is run separately.
