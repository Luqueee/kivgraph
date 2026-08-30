## What changed

<!-- Summarize the observable change and name the affected packages or workflows. -->

## Why

<!-- Explain the problem, motivation, and any linked issue (for example, fixes #123). -->

## Testing

<!-- List the exact commands you ran and mention relevant platform or fixture details. -->

- [ ] `go test ./...` passes, or the reason it does not apply is documented.
- [ ] `go vet ./...` passes when Go code is affected.
- [ ] `make build` passes when the executable or bundle is affected.
- [ ] `make test-ladybug` passes when LadybugDB code is affected.
- [ ] Worker, web, or landing checks pass when those packages are affected.
- [ ] User-facing documentation or release notes are updated when needed.

## Compatibility and scope

- [ ] CLI, MCP, configuration, schema, or payload changes are documented.
- [ ] No indexed repository or benchmark input was modified.
- [ ] Generated directories and pinned manifests or lockfiles were not edited by hand.
