# ADR 0093: Explicit managed analyzer toolchains

Status: accepted

## Context

Some language loaders have a bundled fallback and an optional type-aware
analyzer. Python is the clearest case: Kivgraph can index Python with its
standard-library AST worker, but exact declarations and references require a
Pyright-compatible language server. The old contract required the operator to
install Pyright separately and edit the Python configuration by hand.

The project-local `index` command must stay safe and reproducible. It cannot
silently install npm packages, mutate a repository, or turn a missing optional
analyzer into a failed index. At the same time, asking every user to discover
the package name, pin a version and wire the adapter is unnecessary friction.

## Decision

Add an explicit, language-agnostic `kivgraph toolchain` command family:

- `toolchain status` reports managed analyzers and the active Python analyzer
  mode.
- `toolchain install pyright` installs the pinned stable Pyright version with
  npm, using `--ignore-scripts`, under the installation state directory. The
  installed tree is published atomically with a manifest and SHA-256 digest.
  It then changes only `python.analyzer_command` and `python.analyzer_mode` in
  the selected configuration.
- `toolchain remove pyright --yes` removes only Kivgraph's managed Pyright
  directory and restores the bundled Python fallback when that configuration
  points at the managed analyzer.

The default version is an exact version, not an npm range. The selected
configuration may be passed with `--config`; no repository files are written.
The command family exposes only tools this build can install, so completion
does not promise an unsupported future tool.

`index` and `index --full` never invoke the toolchain manager. Existing host
toolchains remain valid configuration choices, and language-specific SDKs such
as Cargo, Dart and the Python interpreter remain host prerequisites. Managed
toolchains are an explicit convenience, not a hidden installer or a replacement
for the system toolchain.

## Consequences

Python exact mode becomes a one-command opt-in while fallback indexing remains
available offline. The state directory is shared by profiles at installation
scope, matching the existing analyzer-target ownership model. The selected
configuration owns the activation, so profiles behind one configuration see
the same analyzer mode; separate configurations can opt in independently.

The first install still requires npm and network access. Kivgraph records the
exact package version and the digest of the installed tree, so `status` can
detect local tampering or an incomplete installation. Registry provenance is
still npm's responsibility; this feature does not turn npm into a vendored
package mirror.
