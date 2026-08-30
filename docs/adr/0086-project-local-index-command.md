# ADR 0086: Project-local `kivgraph index`

## Context

The explicit `kivgraph index --full` command is the stable pipeline entry
point. It reads the repositories already registered in the selected
configuration, which is the right behavior for a shared workspace and for
automation.

That workflow is unnecessarily verbose for a developer standing in one
project. It also requires the developer to know the supported language names
before the first index. A project-local workflow needs to discover its source
languages and keep its state separate from the user's shared registry.

## Decision

`kivgraph index` is a convenience form that:

1. resolves the current working directory as the project root;
2. detects supported languages from source extensions and project manifests;
3. creates or reuses `.kivgraph/config.yaml` and
   `.kivgraph/repositories.yaml`;
4. adds or updates a repository named `project` for that directory; and
5. runs the same complete, validated rebuild as `kivgraph index --full`.

Language detection ignores Git metadata, dependency directories, generated
output and the `.kivgraph` directory. It is deterministic and writes canonical
language names, not aliases. An empty or unreadable project is an error and
does not publish a graph.

`kivgraph index --full` keeps its existing meaning and flags. The short form
accepts the same configuration, repository, resolver-version and JSON output
overrides, but its default configuration is local to the current project.
JSON event output remains on stdout only; discovery messages go to stderr.

The command does not install Go, Cargo, Dart, Java or .NET toolchains. Those
tools are platform prerequisites for the corresponding language pass, and
`doctor` reports a missing prerequisite without silently changing the host.

## Alternatives

- Keep requiring `--full` and document a shell wrapper. This duplicates the
  command's configuration and language-detection rules outside the binary.
- Add a global `--repository .` shortcut. This still writes to shared state
  and makes a local project depend on the user's registry.
- Install language toolchains automatically. This is host mutation outside
  Kivgraph's ownership and is not reproducible or safe for a project command.

## Consequences

The first local index creates a `.kivgraph` directory in the project. It must
be added to that project's ignore rules when the project tracks unignored
files. Subsequent invocations re-detect languages and update only the matching
repository entry, preserving its manifests, roots and exclusions.

The CLI has two index entry points, but both execute the same full rebuild and
publication pipeline. This preserves the no-incremental-indexing contract
while making the common current-directory workflow one command.

## Risks

Autodetection can identify a language from a manifest whose source files are
not currently present. The index report still exposes what was loaded and
what was not, so that absence is not presented as successful semantic data.

## Status

Accepted.
