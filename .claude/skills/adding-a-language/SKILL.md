---
name: adding-a-language
description: How a new language enters Kivgraph's graph - the two routes and why one of them is almost always wrong, the four decisions that cannot be migrated later, the branches that decide what your language is without asking, and the gates that fail closed. Use when adding or extending language support, when touching `internal/facts/semantic.go`, a `*loader` package, `SupportedLanguages()`, or the provenance codes.
---

# Adding a language to Kivgraph

## The rule that costs the most to discover

`internal/facts/semantic.go:483`:

```go
func definitionProvenance(language Language) Provenance {
	if language == LanguageDart {
		return DartAnalyzerDefinition
	}
	return PythonIndexerDefinition
}
```

That was the shape until Java arrived: two functions, both `if Dart, else
Python`. A language added through the payload route and not added *here*
published **every** definition as `PYTHON_INDEXER_DEF` and **every** use as
`PYTHON_INDEXER_USE`. Nothing failed. The provenance is legal,
`canonicalProvenanceValues` accepts it, `stage.integrity` passes with `0
invariant violations`, the golden probes pass, `doctor` goes green. The graph
is wrong in the one field that says where a fact came from, and nothing but
reading it would have told you.

**It is a table with no default now**, and `NormalizeSemantic` refuses a
payload whose language has no provenance. That check is not decoration:
`Set.Validate` would not have caught it, because it only rejects an empty
provenance under an `EXACT` confidence and `DEFINES` is `StructuralCertain`.
Add your two constants, or your first test fails at the door.

The same shape repeated in the indexer, where the fallthrough was TypeScript,
and that one is fixed too -- see *The branch that used to decide what you are*.

So the work is not writing a loader. **The work is finding the branches that
already decide what your language is without being asked.** Two of them are
now failures instead of defaults, which is the only reason this file is
shorter than it was. Assume there are more.

## Two routes, and the one you want

| | Route A: the payload | Route B: your own normalizer |
| --- | --- | --- |
| The producer emits | `facts.SemanticPayload` as JSON | whatever it likes |
| Graph semantics written by | `facts.NormalizeSemantic` | you |
| Who took it | Python, Dart, Java | Go, TypeScript, Rust |
| Cost | `internal/pythonloader/loader.go` is `178` lines, `python-worker/index.py` is `258` | `internal/facts/golang.go` is `747` lines, `typescript.go` `937`, `rust.go` `534` -- each with its own `Input`, `Report`, unit type, cache branch and counters, on top of a loader |
| Contract | `docs/protocol/semantic-facts-v1.md` | none; it is code |

Route A is not the cheap route because it does less. It is cheap because
`NormalizeSemantic` (`internal/facts/semantic.go:193`) already writes the
repository, the package, the files, the symbols, `DEFINES`, the evidence, the
references, the calls, the imports, `PART_OF` and every `UNRESOLVED` -- and it
writes them the same way for every language that uses it, which is the actual
product. A second copy of the graph model is a second place for the model to
drift.

Dart's loader is `2013` lines and that is not a counter-example: it speaks the
Analysis Server protocol. It writes no graph semantics either.

**Take route B only when the payload cannot express what the analyzer knows.**
All three languages that took it did so before the payload existed.

And check `internal/scip` before writing a loader at all. If the language has a
SCIP indexer -- `scip-java`, `scip-python`, `scip-ruby`, `scip-dotnet`,
`scip-clang`, or `rust-analyzer scip` -- the conversion to a payload is already
written, and the language is a loader that runs an indexer and names a package.
Java is `internal/javaloader/loader.go`, and it is `~230` lines. See ADR 0080.

## The four decisions you cannot take back

Make these before writing a line. Each is frozen by a contract in the root
`AGENTS.md`, and each is a data migration once it ships.

### 1. What scopes a symbol's identity

`NormalizeSemantic` builds a `StableKeyIdentity` per symbol. Python leaves
`Module` empty: a qualified name is unique inside a package. Dart sets `Module`
to the file, and on a collision appends `#<start offset>`
(`internal/facts/semantic.go:267`), because Dart lets two files of one package
declare the same name.

Get this wrong and every stable key of the language is wrong. Stable keys are
listed in *Superficies que rompen compatibilidad*: the algorithm, the canonical
identity and the historical `luque-stable-key` namespace do not change without
a data migration, an ADR and an explicit contract update. This is not fixable
in a patch.

Ask it as: *can two files of one package declare the same qualified name?* If
yes, the file is part of the identity.

### 2. Whether the producer is authoritative

`authoritative: true` is reserved for a producer that resolved references with
the language's own semantic analyzer. Absent or false, definitions are still
structural facts but references and calls are `CANDIDATE`.

Python ships both and says which is which: an AST fallback that reports itself
as `python-ast-fallback`, and a Pyright adapter for exact mode. A fallback that
claimed `EXACT` would be the one thing the project does not do -- an `EXACT`
edge is never created by name, text, path, alias or single candidate.

`CANDIDATE` is not a degraded `EXACT`. It is a different answer, and callers
filter on it.

### 3. The provenance codes, which are append-only

`internal/facts/codes.go:60`, and the file says why at line 13: the HotSnapshot
stores kinds, confidences and provenances as bare `uint8` and filters
traversals by them, so a snapshot is only readable by code that agrees on the
numbering.

Append your constants at the end of the block. Do not insert, do not renumber,
do not sort the list to be tidy. Reordering is a format break that requires
bumping `CodeFormatVersion`, which makes every published snapshot unreadable.

Then add the same values to `provenanceCodes` in that file **and** to
`canonicalProvenanceValues` in
`internal/storage/ladybug/canonical_integrity.go:119` -- the second list is
written out by hand on purpose, so it does not follow the first.

### 4. The extension set, and the aliases

`internal/config/languages.go:21` is keyed by **every spelling
`SupportedLanguages()` accepts**, aliases included. The file explains the
defect that made it: a lookup that only knew the canonical name answered
nothing for the alias, and the watcher then did not see a change to a `.py` or
a `.dart` file as a source change at all.

If the language gets an alias in `SupportedLanguages()`
(`internal/config/config.go:1233`), it gets a row here under each spelling.

Separately, `moduleSymbolExtensions` in `internal/facts/module_symbol.go:32`
decides what is stripped from a module symbol's qualified name. It is
readability only -- the stable key disambiguates on the path -- but a language
missing from it names every file's module scope with the extension still on.

## The touchpoints

Do not trust a list written by hand. `publishing-releases` learned that one the
expensive way: its hand-written list of files carrying the version was two
minors out of date on a page nobody had thought to add. Derive the list
instead, from the commit that did this twice in one pass:

```bash
git show --stat fe8308b     # feat: add Python and Dart semantic indexing support
git log --oneline fe8308b..HEAD -- internal/pythonloader internal/dartloader
git grep -il dart -- 'internal/**' 'cmd/**' 'scripts/**' '.github/**'
```

`fe8308b` is `70` files and `4197` insertions for two languages at once. The
commits after it are the ones worth reading twice: they are what the first pass
missed.

What the files want, which the list of names does not say:

| File | What it needs |
| --- | --- |
| `internal/config/config.go` | the name and aliases in `SupportedLanguages()`, a `<Lang>Config` struct, its field on `Config`, its defaults, and its validation |
| `internal/config/languages.go` | one row per accepted spelling |
| `internal/facts/facts.go` | `Language<X>` and the provenance constants |
| `internal/facts/codes.go` | the codes, appended, and the `provenanceCodes` rows |
| `internal/facts/module_symbol.go` | the extension |
| `internal/facts/semantic.go` | `definitionProvenance`, `useProvenance`, and the identity branch if the language is file-scoped |
| `internal/<lang>loader/` | the new package: run the producer, decode, check version and language, set `Authoritative` |
| `internal/scip/` | nothing, if the producer emits SCIP: `Convert` already writes the payload. Give it the language, the repository, the package and a file reader |
| `internal/indexer/semantic.go` | `semanticSourceExtensions`, the `indexSemantic` switch, `semanticRequestedPackage` if an import prefix has to be stripped |
| `internal/indexer/full.go` | the partition, the `units` append, the report counters, `semanticSchedule`, `semanticBudget`, a progress phase and a worker limit |
| `internal/indexer/factcache.go` | three separate duties -- see below |
| `internal/indexing/full.go` | the options, twice: from configuration and from the caller |
| `internal/indexing/service.go`, `document.go` | the JSON report counters |
| `internal/storage/ladybug/canonical_integrity.go` | `canonicalProvenanceValues` |
| `internal/syntax/parser_manager.go`, `grammars/manifest.json` | only if a tree-sitter grammar is pinned; the manifest is digest-pinned and never edited by hand |
| `internal/mcp/instructions.go` | the sentence users read, and `surface_test.go` asserts it |
| `internal/integrations/assets/kivgraph/SKILL.md` | the shipped skill names the languages |
| `cmd/kivgraph/main.go` | the `index.<lang>` counter line and the `doctor` toolchain branch |
| `scripts/verify-semantic-coverage.sh` | a hardcoded set -- see the gates |
| `testdata/semantic-coverage/manifest.json` | the entry, with at least ten capabilities |
| `testdata/<lang>/` | the fixtures the manifest names |
| `scripts/build-bundle.sh` | only if a worker script ships |
| `.github/workflows/ci.yml` | the toolchain, or the tests skip and the suite still goes green |
| `docs/adr/00XX-*.md` | a new analyzer boundary is an architecture change; the next number is `0081` |

`internal/agenthook/languages.go` needs nothing. It is a thin name over
`config.SourceExtensions`, and it is thin precisely because it used to be the
second copy of that table and the two disagreed.

## The branch that used to decide what you are

This section used to list eleven `switch { case unit.isPython: ... }` sites
whose default was TypeScript, and told you to find all eleven by hand. Adding
Java did the refactor it recommended instead. What is there now:

```bash
git grep -n 'unit\.kind' -- 'internal/**/*.go' | grep -v _test
```

`analysisUnit` carries one `kind` (`internal/indexer/full.go`), whose zero value
`unitUnspecified` is deliberately not a language. `analyseUnit` refuses it and
names the repository, so a unit built without its kind stops the pass instead of
being analysed as a TypeScript package.

Every semantic language shares `unitSemantic` and differs only by `language`.
So a language added through the payload route touches **none** of the nine
dispatch sites. What it does touch is a set of tables, all in
`internal/indexer/full.go` and `internal/indexer/semantic.go`:

```bash
git grep -n 'LanguageJava' -- internal/indexer/ | grep -v _test
```

`semanticSourceExtensions`, the `indexSemantic` switch, `addSemantic`,
`semanticSchedule`, `semanticBudget`, `semanticRequestedPackage` if an import
prefix has to be stripped, plus the partition and the `units` append in `Full`,
a `ProgressPhase` and a worker limit. Call it eight or nine places rather than
four -- the win is not that there are fewer, it is that every one of them is a
table or a switch with an explicit default, so a miss is a compile error, an
empty count or a named failure instead of another language's facts.

Two things the refactor turned up, which say what the old shape was costing:

- **`unitIdentity` had no branch for Rust.** Every Rust workspace was keyed
  `typescript\x00<repository>\x00` with an empty package name. One workspace
  per repository hid it -- wrong but unique. Two in one repository shared a
  fact-cache entry, and the second was served the first's facts.
- The counters, `weight()`, `detail()`, `describeInputs` and the scheduler
  queue all fell through to TypeScript too. Nothing errored; the graph was
  simply about the wrong thing.

The lesson generalises past this repository: **when a dispatch's default is a
real case rather than a failure, adding a member is a silent bug waiting for
whoever forgets one site.**

## The cache has three duties, and each fails silently

`internal/indexer/factcache.go`:

1. **Identity** -- `unitIdentity` at line `380`, what the entry is about.
2. **Inputs** -- `describeInputs` at line `395`, the source tree plus every
   manifest whose content changes the answer. The Python/Dart branch lists
   twelve filenames by hand; a manifest yours reads and that list does not name
   is a file you can edit without invalidating anything.
3. **Analyzer fingerprint** -- `analyzerFingerprint` at line `664`, every
   option that changes what the producer does.

And a fourth that is not obvious and cost a defect. `e0d8d52`
(`fix(indexing): fingerprint the python producer the pass runs`): the options
were fingerprinted, the binary was fingerprinted, and the *script the loader
actually resolves and runs* was not. Editing `python-worker/index.py` changed
no cache key, so a rebuild reused the previous producer's facts and published a
generation the current code would not produce.

The fix is `pythonloader.ProducerFile`, and the shape to copy is in its
comment: it resolves with **the same rules** as `resolveCommand`, because two
resolution rules is how a cache ends up keyed on a file nobody runs. If your
producer is a file this repository ships, fingerprint the file, resolved the
way you run it. If its identity is unknown, say so with a value that cannot
match -- unknown identity is not a licence to reuse anything.

## The gates that fail closed on a language they do not know

`scripts/verify-semantic-coverage.sh` opens with:

```python
expected = {"go", "typescript", "python", "dart"}
```

`make semantic-coverage` fails until the set names your language *and*
`testdata/semantic-coverage/manifest.json` has an entry whose `fixture` and
`test` both exist on disk and whose `capabilities` list has no duplicates and
at least ten entries. This one is honest: it fails loudly and immediately.

Note what it also says: **Rust is not in that set.** The coverage gate covers
four languages, not five. Do not read a green `semantic-coverage` as a
statement about Rust, and do not treat its absence as permission to skip yours.

`internal/mcp/surface_test.go:333` asserts the server instructions mention
`"Python"` and `"Dart"` by name, and that the whole string stays under `2048`
bytes. Adding a language to that sentence is a change to the MCP surface, which
`AGENTS.md` lists under *Superficies que rompen compatibilidad*: the tool
descriptions and the skill that names them do not change in silence.

## An analyzer that builds must not build in the repository

`AGENTS.md` says it under *Nunca modificar* with no exception, and it is the
easiest rule in the project to break by accident: a build-based indexer --
`scip-java` driving Maven, `scip-dotnet` running `dotnet restore` -- writes
`target/`, `obj/` and `bin/` into the directory it builds, and no flag moves
them. They are the build tool's output, not Kivgraph's.

`internal/scratchtree` is the answer: it materialises the working tree
somewhere else, the build runs there, and the tree dies with everything it
produced. Measured on this repository -- `1652` tracked files, a `3.8 GB`
working tree:

| strategy | time | size | writes inside the repository |
| --- | --- | --- | --- |
| copy of the working tree | `8154 ms` | `4.5 GB` | none |
| `git worktree add` | `107 ms` | `16 MB` | **yes**, `.git/worktrees/` |
| `git archive` + dirty overlay | `76 ms` | `16 MB` | none |

Two things to take from it. **`git worktree add` is the obvious answer and the
wrong one** -- it registers metadata inside the repository, which is the rule
you came to keep. And **a materialisation reproduces the working tree, not
`HEAD`**: a user editing code expects the graph to describe what is on disk.

Then the defect it introduces, which is worth knowing before you write it: the
scratch tree is a fresh temporary directory per pass, and if the loader derives
its **package name** from where it read the files, every stable key changes on
every pass. A loader carries two roots and they are not interchangeable --
`sources` is where files are read, `repository` is what the facts are about.
See ADR 0082.

And the test that catches it has to index **in place**. Every end-to-end test
here copies the fixture first, which is allowed and is also exactly what hid
this for two languages.

## Fixtures, and what a fixture has to be

`testdata/<lang>/basic` and `testdata/<lang>/advanced` is the shape Dart uses;
Python uses `basic` and `coverage`. The manifest names one of them and the test
that reads it.

The rules from `AGENTS.md` and `running-tests` that bite here:

- **An indexing pass must leave the fixture byte-identical.** The Rust fixture
  is the precedent: no `target/`, no `Cargo.lock` after a run, and the test
  checks it. If your analyzer writes into the project it analyses -- a
  `.dart_tool/`, a `__pycache__`, a lockfile -- it runs in a scratch tree; see
  the section above. A test that copies the fixture first proves the fixture is
  fine and says nothing about a user's repository.
- **Start with the negatives.** What the producer *cannot* resolve is the
  contract, not an omission: dynamic dispatch, a decorator that rewrites a
  name, a conditional import, generated code. Each becomes an `UNRESOLVED` with
  a reason, and the test asserts it stays unresolved.
- Use `internal/testsupport.TempDir(t)`, never `t.TempDir()`, when the path
  feeds the workspace layer.

## What ships and what only runs here

A worker script that the bundle does not carry works on your machine and
nowhere else. `scripts/build-bundle.sh` installs `python-worker/*.py`
explicitly, by name, into `worker/python-worker/`; `pythonloader.resolveCommand`
then looks in three places in order -- the working directory, the source tree
relative to `runtime.Caller`, and `../worker/` beside the executable. Copy all
three or the bundled binary cannot find its own producer.

`doctor` reports each toolchain separately (`cmd/kivgraph/main.go:1615`, `runDoctorToolchains`), and
the rule it implements is worth keeping: **a toolchain nobody installed is a
fact about this machine, not about the code.** `analyzerNotInstalled`
(`internal/indexer/semantic.go:90`) isolates the repository with `not_loaded=1`
and lets the others index. A language that aborts the whole pass because its
analyzer is missing is a defect -- that one already happened, five repositories
deep, four of which had nothing to do with Dart.

## Verifying

```bash
gofmt -l <changed-go-files>
go vet ./...
go test ./...
make test-ladybug                # the loader touches indexing, so this is not optional
make semantic-coverage
go test -race ./cmd/kivgraph/...
make build
```

Then check the suite did not skip the thing you came to prove -- a missing
toolchain is a `SKIP`, and a `SKIP` is green:

```bash
go test ./internal/<lang>loader/ -v -count=1 2>&1 | grep -E 'SKIP|FAIL'
```

And prove it end to end on a real fixture with a native binary, because the
`make build` binary has no storage layer and dies at publish. The full recipe
is in `running-tests`, under *Smoke test del binario*; the language-specific
part is:

```bash
env HOME="$home" /tmp/kivgraph-native init --repository "fixture=$repo" --languages <lang>
env HOME="$home" /tmp/kivgraph-native doctor
env HOME="$home" /tmp/kivgraph-native index --full
```

A healthy pass prints `index.<lang>: repositories=1 not_loaded=0 symbols=N
references=N unresolved=N`, `stage.integrity: PASS (0 invariant violations)`
and `stage.golden probes: PASS`. `symbols=0` with `not_loaded=0` means the
partition never selected the repository: the language name reached
`SupportedLanguages()` but not the `repositoriesFor...` call in
`internal/indexer/full.go`.

Then read the provenance of what you published. It is the one thing every gate
above passes without checking, and the defect at the top of this file is
invisible until someone looks.

## Not part of adding a language

- **A release.** A new supported language is a capability a user can run, so it
  does qualify -- but only when someone asks. See `publishing-releases`, and
  the twenty-one `chore(release)` commits in two days that section exists to
  prevent.
- **The landing.** `landing/src/components/landing/Languages.astro` and the
  pages around it are user-facing copy for a language that works. They come
  after the coverage gate is green, not before, and they are written in English
  like everything the project publishes.
- **Removing a language.** Nothing here is symmetric. A configuration key that
  is retired is accepted, ignored and **named** -- `retiredConfigKeys` and the
  `config.retired` line of `doctor` -- because the decoder rejects unknown keys
  and deleting the field turns every existing config file into a startup
  failure. See ADR 0062.
