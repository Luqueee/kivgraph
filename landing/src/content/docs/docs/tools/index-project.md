---
title: index_project
description: Registers projects and rebuilds the graph once, after explicit user approval. The only Kivgraph MCP tool that mutates anything.
---

> Registers projects and rebuilds the graph once, after explicit user approval. Pass every project in one call: a rebuild costs the whole corpus. It never writes inside the source projects.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | string | configured default | Profile to rebuild. A missing profile is created before indexing. |
| `projects` | array of objects | none | The batch to register and index. Each entry requires `name`, `path` and `languages`, and accepts nothing else. This is the form to use. |
| `name` | string | none | Single-project form: the repository identifier. |
| `path` | string | none | Single-project form: the repository directory. Absolute, `~`-prefixed, or relative to the working directory of the server process. |
| `languages` | array of strings | none | Single-project form: the languages to index. |
| `confirmed` | boolean or null | `null` | Approval from a client that cannot use MCP elicitation. Only `true` proceeds. |

Naming both forms in one request is rejected with `INVALID_ARGUMENT`, because two
selectors can disagree and there is nothing to decide between them. Naming
neither is rejected the same way.

The accepted `languages` values are the supported language vocabulary, and
nothing else:

```text
go, typescript, javascript, ts, js, rust, rs
```

Values are lowercased and trimmed. A duplicate in one entry, an empty string, or
a word outside the list is rejected before anything is written.

## Answers

Nothing, in the sense the other ten tools do. It registers the projects it is
given in the repository registry, rebuilds the complete canonical graph once, and
publishes the result as a new generation. On success it reports the generation
and snapshot it published together with the per-language counters of the pass. It
is the only mutating tool on the surface.

## Consent

It refuses without explicit approval. A client that declares the MCP elicitation
capability is asked directly, and the request proceeds only when the user
accepts. A client that does not implement elicitation must obtain approval itself
and then send `confirmed: true`; sending it without having asked is a lie the
server cannot detect.

Called without either, it fails:

```text
PERMISSION_REQUIRED: user approval is required; confirm the operation before setting confirmed=true
```

An elicitation the user declines fails with `PERMISSION_DENIED`. An elicitation
the client cannot deliver fails with `PERMISSION_REQUIRED`.

The prompt names what approval covers: the project and its path for a single
project, or the count and every name for a batch. Approving "11 projects" without
seeing which ones is not approval.

## Example

```json
{
  "name": "index_project",
  "arguments": {
    "name": "kivgraph",
    "path": "/path/to/kivgraph",
    "languages": ["go"]
  }
}
```

```text
PERMISSION_REQUIRED: user approval is required; confirm the operation before setting confirmed=true
```

Corpus: snapshot `30` of two repositories, `kivgraph` and `go-svc-e`. The captured
call sent no `confirmed` and the client declared no elicitation capability, so
the refusal is the whole response.

The batch form, which is the one to send:

```json
{
  "name": "index_project",
  "arguments": {
    "projects": [
      {
        "name": "kivgraph",
        "path": "/path/to/kivgraph",
        "languages": ["go", "typescript"]
      },
      {
        "name": "go-svc-e",
        "path": "/path/to/home/Documents/programacion/projects/go-svc-e",
        "languages": ["go", "typescript"]
      }
    ],
    "confirmed": true
  }
}
```

## Pass every project in one call

This is not a style preference. A rebuild resolves cross-repository edges over
the complete fact set, so there is no cheaper unit than the whole corpus: adding
one project costs exactly what adding eleven costs. Calling the tool once per
project pays that full cost once per project and throws away every result but the
last, because only the newest generation is published.

Eleven projects registered one call at a time cost eleven rebuilds for one useful
graph. Registered together they cost one.

The whole batch is registered before anything is built. If the index or the
rebuild then fails, the prior registry is restored and the previous generation
stays published.

## When it is available

The tool exists only on a configured `serve` route. Without a configured indexer
it is not registered at all, so the read-only server cannot gain filesystem or
storage access by accident.

It is also the only tool registered when no generation has been published yet.
That is how a client with no graph builds its first one: the server completes the
handshake, publishes `index_project` alone, and the other ten appear once a
generation exists. See [Troubleshooting](/mcp/troubleshooting/).

## Registering the same project twice

Indexing a project that is already registered is not a conflict. The caller asked
for that repository to be in the graph, and it is.

- A project already registered with the same directory is reindexed without
  touching the registry. An identical request changes nothing on disk at all.
- A change of `languages` for the same directory replaces the entry, and the
  `exclusions` on file survive it. The request cannot express exclusions, and
  dropping them would silently widen the index to directories that were excluded
  on purpose.
- Only a name already held by a **different** directory is a real conflict,
  because then nothing can decide which of the two repositories the name means.
  The error names the registered path: `project "go-svc-e" is already registered at
  "/repos/go-svc-e": choose another name or remove that entry`.

Names are compared exactly, and a name is an identifier: `.`, `..` and anything
containing a path separator are rejected. The `rust:` namespace is reserved for
the providers Kivgraph derives from the toolchain and cannot be taken.

## Progress

A full rebuild takes minutes on a large registry, while an MCP client applies its
own timeout to the call, thirty seconds in some, and cancels work that is
progressing fine.

A request that carries a `progressToken` gets `notifications/progress` for every
unit of work: the phase, the repository and a detail. A client that honours them
waits for as long as the work reports. Without a token no progress callback is
installed at all, so the index does not pay for a channel nobody reads.

Progress counts up and never repeats a value, as the protocol requires. A
notification that cannot be delivered is dropped rather than failing the index.

The pass itself runs as a child process — `index --full --json` — and the server
forwards the progress it reports. That is what keeps the peak of an index out of
the process answering queries; see [Indexing](/guides/indexing/#where-a-pass-runs).

## Limits

- It never writes inside the source projects. It writes the repository registry
  and the Kivgraph state directory; the checkouts it reads are left untouched.
- One index runs at a time inside a process. An `index_project` and a
  resynchronisation cannot overlap, and across processes a lock elects the single
  writer: the loser does not wait, because a rebuild lasting minutes would look
  like a hang.
- A failure is reported as `INDEXING_FAILED` with the observed cause in the
  message: a module that needs a newer toolchain, a path that is not a
  repository, a dependency the module cache does not hold.
- `path` must exist and be a directory. A relative path is resolved against the
  working directory of the server process, which is the client's choice, not
  yours; prefer an absolute path.
- Rust is indexed through an external `rust-analyzer scip` process and needs it
  configured. See [Configuration](/docs/configuration/).
- It publishes a new generation, so every cursor issued from the previous one
  expires with `CURSOR_SNAPSHOT_EXPIRED`.
