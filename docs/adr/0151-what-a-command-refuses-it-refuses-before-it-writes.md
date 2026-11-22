# ADR 0151: What a command refuses, it refuses before it writes

## Status

Accepted. Changes no schema, stable key or generation layout. Changes what
some commands and tools accept and print; each change is listed under
"Compatibility".

## Context

`v0.10.0` was run by hand on a dozen real repositories. Most of what went wrong
was not a wrong edge. It was a command that accepted something, wrote state,
and failed later somewhere else -- or one that knew something and said it only
to whoever ran a second command.

- `init --repository a=/missing` created a configuration and a registry with
  the entry in it; every later command failed on that entry.
- `kivgraph index` in a directory that is not a Git checkout created
  `.kivgraph/` and then failed in the pass.
- `start_index_project` returned an operation for a path `index_project`
  refuses; the refusal was one poll away.
- A new installation got a `state.pre-profiles` directory: a recovery point of
  state that never existed.
- `kivgraph mcp --help` was `unknown command "mcp"`.
- `doctor repositories` knew that `cargo metadata` had failed, that a package
  had no project and that files were claimed by none. `index` said `PASS`.
- A bare `name` in `find_references` resolved only the kinds on a list written
  before most loaders. A Go `var`, a Rust `macro`, a Dart `extension` were
  "only imported or re-exported here, never declared".

## Decision

1. **A refusal comes before the first write.** `init` reads every
   `--repository` before it creates anything, and refuses a path that is not a
   directory and a name in the reserved `rust:` namespace. `kivgraph index`
   asks the registry whether the directory can be registered before it
   creates `.kivgraph/`. `start_index_project` runs the checks a pass runs on
   one project -- name, path, languages -- before it hands out an operation.
   What needs the registry or the pass still fails in the operation.
2. **One entry that cannot be read is named, and the rest are served.**
   ADR 0147 said it for `index --full`; it holds now for the watcher, the
   daemon's pass and `doctor repositories`. With no entry left it is an error.
3. **A recovery point is of state that was there.** The migration to profiles
   keeps `state.pre-profiles` only when it copied a legacy artifact.
4. **`index` points at the audit.** After a pass, one line --
   `index.audit: N finding(s), M blocking` -- when `doctor repositories` would
   report something. The findings are not repeated. And the project form names
   the source files of languages no loader reads: `not read: 70 .swift`.
5. **The first word of a family of commands answers.** `kivgraph mcp`,
   `kivgraph mcp --help`, and the same for `hook` and `profile`, list the
   commands of the family and exit `0`.
6. **A bare name resolves whatever declares.** The list is of the kinds that
   do not: `import`, `export` and `reexport` mention a name, and `parameter`,
   `self_parameter`, `type_parameter`, `local_variable`, `prefix` and `file`
   declare one that means nothing outside its declaration. Every other kind,
   one no list names included, is a declaration.
7. **`get_file_outline` takes `.`** for the repository's own directory, and
   `./x` for `x`.
8. **Another checkout registers beside the repository above it.** ADR 0122
   stopped walking a clone or a linked worktree below a repository. Nothing of
   it is read with the parent, so nothing is read twice, and the "nested
   repositories" refusal no longer applies to it. A plain directory and a
   submodule are still refused. The watcher, the reconciler and the audit skip
   one as the indexers do.
9. **`impact` follows a linked worktree to its repository.** When the checkout
   is not registered and exactly one registered repository shares its Git
   common directory, the graph of that one answers, with a warning that the
   two trees may differ.
10. **Dart: `Extension(receiver).member` is a use of the extension.** The
    Analysis Server resolves the member and gives no navigation region for the
    name. When the resolved member belongs to an extension and the text before
    the receiver is that extension's name, a `REFERENCES` edge is recorded at
    the name. The member is the evidence; the name alone creates nothing.

Also, without a decision behind them: `doctor repositories` reported every
TypeScript finding twice for a repository registered as `typescript` and
`javascript`; `${configDir}` in an inherited `tsconfig.json` was read as a
literal, which made a project the pass indexed "claim nothing"; a line the
analyzer's log repeats with a different internal id filled the twenty
`index.rust.diagnostic` lines, and is one line with a count now; the search
gate did not read `--include`, `-g`, `--glob` and `-t` of a shell command as
where the search looks.

## Rejected

- **Refusing a `repo` filter that names no repository.** `find_references`,
  `find_by_intent`, `find_cross_repo_consumers` and the unresolved listing
  accept a repository key there, and answer an unknown one with an empty page
  and guidance that names the narrowing. Tests hold both. An error would
  break a caller that passes a key.
- **Running the audit inside the pass and printing its findings.** They are
  many lines on a large registry and they are the audit's. One line says
  there is something to read.
- **Indexing a directory that is not a Git checkout.** A repository's identity
  and its freshness are read from Git. The comment that promised it for
  unpacked trees described something the registry never allowed.

## Compatibility

- `init` exits `2` for a `--repository` whose path is not a directory. It
  used to exit `0` and write the entry.
- `start_index_project` answers `INDEXING_FAILED` for a project it used to
  accept and fail later. The words are the ones `index_project` uses.
- A bare `name` that used to resolve to its one `function` can now be
  ambiguous with a `property` or a `constructor` of the same name. The refusal
  names the candidates, as it does for any two declarations.
- `index` prints two lines it did not: `index.audit:` and, in the project
  form, `not read:`. `index --full --json` prints neither on `stdout`.
- The schema of `get_file_outline` describes `path` with different words.
- A Dart graph gains `REFERENCES` edges to extensions on the next index.

## Not measured

- The audit runs once more per `index --full`; on a Rust repository that is
  one `cargo metadata` per workspace. Its time was not measured.
- The Dart change was checked on one repository (`6324` to `6325` edges, the
  one missing use) and a fixture.
- No test runs `kivgraph index` end to end outside a checkout with a real
  pass; the refusal is tested, and that it leaves the directory as it was.
