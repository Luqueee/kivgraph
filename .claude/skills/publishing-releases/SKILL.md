---
name: publishing-releases
description: Safe and reproducible procedure to decide on, prepare, tag, verify and close a Kivgraph release. Use when someone asks to cut a version, when touching internal/version/version.go, a release note or a vX.Y.Z tag.
---

# Publishing a Kivgraph release

This skill covers the whole change: deciding whether publishing is worth it,
updating the right sources, passing the gates, creating the tag and checking
what actually reached GitHub and the MCP Registry. It publishes nothing unless
the person has explicitly asked for it.

## Rules that are not negotiable

- The version only goes up. It is never lowered, restarted, reused or rewritten
  as a tag that has already been pushed.
- Stable releases use vX.Y.Z. Development releases use a SemVer suffix, for
  example vX.Y.Z-dev.N; they are prereleases and are not marked as latest.
- The tag must point at the exact commit that is already integrated in main. A
  working branch or a dirty tree is not tagged.
- The release note must exist before the tag. If it is missing, CI burns the time
  of building the artifacts and fails just before publishing them.
- A release is a distribution event, not a progress mark of the backlog. A task
  number, a PASS in TASKS.md or a green suite are not by themselves a reason.

`kivgraph update` selects a channel and compares versions with SemVer. The
`stable` channel queries `/releases/latest`; the `dev` channel enumerates the
first 100 published prereleases and picks the highest valid version. Without
`--channel`, a stable installation uses `stable` and a prerelease installation
uses `dev`. It can also be selected explicitly with `--channel stable|dev` or
`KIVGRAPH_UPDATE_CHANNEL`.

## 1. Decide whether there is a release and which number to use

Publishing only makes sense if whoever updates gets something executable and
observable:

- a fix that affects the published binary;
- a new capability of the CLI, MCP or a supported language;
- a compatibility, schema or migration change;
- a fix of the bundle, installers, checksums, analyzer or web viewer.

Do not publish for isolated documentation, TASKS.md, ADRs, benchmarks, tests,
refactors with no observable change, or for completing a LUQUE-#### task.
Summarize in one sentence what whoever updates gains. If the sentence only talks
about internal work, there is no release.

In the 0.MINOR.PATCH series:

| version | use it for |
| --- | --- |
| PATCH | the default option: bugs and changes that neither widen nor break the observable surface; |
| MINOR | a new MCP tool or command, a persistent schema change, a new compatibility or a removed flag. |

Before choosing the number, check it against GitHub and against all the tags. Do
not use a number remembered from a conversation or a lexicographic comparison:

~~~bash
git fetch origin --tags
gh api repos/Luqueee/kivgraph/releases/latest --jq .tag_name
git tag --list 'v[0-9]*' --sort=-version:refname | head -1
~~~

The new tag has to be semantically greater than the stable release GitHub serves
as latest, greater than any existing tag, and cannot already exist as a tag or as
a release:

~~~bash
TAG=vX.Y.Z
if git show-ref --verify --quiet "refs/tags/$TAG"; then
  echo "tag already exists: $TAG" >&2
  exit 1
fi
if gh release view "$TAG" >/dev/null 2>&1; then
  echo "GitHub release already exists: $TAG" >&2
  exit 1
fi
~~~

If there are doubts about the semantic order, stop and resolve them; do not force
the number. kivgraph update compares versions strictly and a lower version leaves
installations with no update path.

## 2. Prepare the release sources

Work from an up-to-date main. If the changes are still in a PR, they are
integrated first through the repository's normal flow; the preparation commit is
not tagged from the PR branch.

To prepare the commit from a known base:

~~~bash
git switch main
git pull --ff-only origin main
git switch -c chore/release-vX.Y.Z
~~~

The version source and the mandatory note are:

~~~text
internal/version/version.go                 var Value = "X.Y.Z"
landing/src/content/releases/vX.Y.Z.md      name of the note and frontmatter
~~~

The public telemetry page is also part of a release's contract. It carries no
manual copy of "the latest version": it names the first release that introduced
each emitter and describes the collector that is deployed. If a field, an
emitter, deduplication, storage, retention or the opt-out change, update in the
same commit:

~~~text
landing/src/content/docs/telemetry.md
docs/development/analytics.md
docs/adr/0083-a-download-is-not-a-person.md or an ADR that replaces it
~~~

A new note is not enough if the page still describes the previous pipeline.

The rest of the commands pinned with KIVGRAPH_VERSION=v... are derived
documentation. There is no list to maintain by hand: the test discovers them in
README.md, docs/, landing/ and the scripts, excluding histories and ledgers.
Update only the examples that mean to teach the current version; do not do a
global replacement that alters historical references.

~~~bash
go test ./internal/version/ \
  -run 'Test(DocumentedInstallVersionMatchesTheBinary|ReleaseNotes)$' \
  -count=1
~~~

The note landing/src/content/releases/vX.Y.Z.md must have this frontmatter:

~~~yaml
---
version: vX.Y.Z
date: YYYY-MM-DD
requires_reindex: false
---
~~~

Choose requires_reindex by behaviour, not by habit:

- true if loaders, resolution, identities, edges or the schema change in a way
  that an existing graph no longer correctly represents the sources;
- false if the existing graph is still valid.

A version with no tag in the repository the page links to carries
`tagged: false`, and the page does not show its comparison link, which would be a
404. That line is removed when the tag is created.

If the persistent schema or a compatibility surface changes, review the ADR and
the migration or full-rebuild path before writing the note. The note must explain
the impact for the user, the incompatible changes and the action needed to
update. Its body is published on the landing and as the GitHub Release note, so
it is written in English.

server.json is a metadata template for the MCP Registry, not another source of
the binary's version. The workflow generates its version and its packages from
the artifacts of that run. A packages list is not added and no checksums are
invented; only the template is edited if its stable metadata changes.

Do not hand-edit dist/, .tooling/, web/dist, landing/dist, ts-worker/dist, pinned
manifests or lockfiles.

## 3. Gates before creating the tag

The tag triggers the pipeline, but CI cannot decide whether the number or the
note were right. Run the relevant gates before publishing and keep the result:

~~~bash
git diff --check
test -z "$(find . -type f -name '*.go' -not -path './.git/*' -exec gofmt -l {} +)"
go vet ./...
go test ./...
make build
make test-ladybug
make lint-ladybug
make coverage
make semantic-coverage
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 \
  -checks='all,-U1000' ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
scripts/check-docs.sh origin/main
go test -race \
  ./cmd/kivgraph/... \
  ./internal/hotsnapshot/... \
  ./internal/indexing/... \
  ./internal/daemon/... \
  ./internal/storage/generation/...
~~~

make test-ladybug is the supported way to run the native suite; do not replace it
with go test -tags ladybug without the library and the CGO_* that the Makefile
prepares. make build produces the normal Go binary, not the native bundle that is
distributed.

Repeat the checks of the three Node packages, the same as CI:

~~~bash
(cd ts-worker && pnpm install --frozen-lockfile && pnpm format:check && \
  pnpm lint && pnpm typecheck && pnpm test && pnpm build)
(cd web && pnpm install --frozen-lockfile && pnpm check && pnpm build)
(cd landing && pnpm install --frozen-lockfile && pnpm check && pnpm build)
~~~

make semantic-coverage and the analyzers must not turn into skips for lack of
tools. Install the toolchain CI requires and leave the failure visible if it
cannot be completed.

CI also cross-compiles for windows/amd64 and parses the two PowerShell scripts.
If Windows-specific code or an installer is modified, reproduce those two checks
with the workflow's tools; that Linux compiles does not prove that the Windows
installer is valid.

If you are on a publishing host, also verify the native bundle of that platform
and its reproducibility:

~~~bash
make build-linux-amd64       # only on Linux amd64
make build-darwin-arm64      # only on macOS arm64
scripts/check-reproducible-bundle.sh
~~~

The release workflow is what builds the three platforms -Linux amd64, macOS
arm64 and Windows amd64- on native hosts. Never treat a local bundle of another
platform as evidence for that platform.

## 4. Commit, integration and tag

After updating Value, the note and the pinned documentation, check:

~~~bash
go test ./internal/version/ \
  -run 'Test(DocumentedInstallVersionMatchesTheBinary|ReleaseNotes)$' \
  -count=1
git diff --check
git status --short
~~~

Create a preparation commit with a message in English:

~~~bash
git add internal/version/version.go landing/src/content/releases/vX.Y.Z.md
# Explicitly add, if they changed, the files with installation examples.
git commit -m "chore(release): prepare vX.Y.Z"
~~~

Integrate that commit into main through the repository's usual protection. When
the commit is already on the remote, create the annotated tag on the exact SHA
of origin/main:

~~~bash
git fetch origin main --tags
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
test -z "$(git status --porcelain)"
git tag -a vX.Y.Z -m "Kivgraph vX.Y.Z"
git push origin vX.Y.Z
~~~

For a development release, replace the tag and the note's name with
`vX.Y.Z-dev.N`. The workflow publishes the GitHub Release as a prerelease and
does not mark it as latest; it does not publish it to the MCP Registry either.
The note is called exactly `landing/src/content/releases/vX.Y.Z-dev.N.md`.

Do not manually create a GitHub Release before the workflow: the tag push
triggers .github/workflows/release.yml and gh release create is the publish job's
responsibility.

## 5. What the workflow really does

.github/workflows/release.yml runs ci.yml again on the tagged commit and then:

1. builds and verifies the complete bundle on each native host;
2. checks version, provenance, checksums, rust-analyzer, web/index.html, the help
   of ui and the absence of landing/ in the payload;
3. creates a reproducible archive and an MCPB (.mcpb) per platform;
4. assembles SHA256SUMS over all the assets and attaches provenance
   attestations;
5. reads the body of the note corresponding to the tag and creates the GitHub
   Release; a stable one is marked as latest and a prerelease leaves `latest`
   untouched;
6. downloads from the freshly published release each file of the three
   platforms, checks its digest, extracts it and runs scripts/verify-bundle.sh
   again;
7. only then validates and publishes the three MCPB to the MCP Registry using
   OIDC.

The expected publication contains exactly these kinds of assets:

~~~text
kivgraph-linux-amd64.tar.gz
kivgraph-darwin-arm64.tar.gz
kivgraph-windows-amd64.zip
kivgraph-linux-amd64.mcpb
kivgraph-darwin-arm64.mcpb
kivgraph-windows-amd64.mcpb
install.sh
install.ps1
uninstall.sh
uninstall.ps1
SHA256SUMS
~~~

The tar.gz/zip are for manual installation and installers; the mcpb are the
packages the Registry understands. The server.json that is published to the
Registry is generated during the job with the hashes of those MCPB, not taken
from the committed file without packages.

## 6. Check and close the publication

Wait for the tag's run and review all the jobs, not only verify:

~~~bash
gh run list --workflow release.yml --limit 10
RUN_ID="$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
test -n "$RUN_ID"
gh run view "$RUN_ID" --log-failed
gh release view vX.Y.Z --json tagName,isDraft,isPrerelease,isLatest,assets,url
~~~

The run is only closed when verify, the three build rows, publish and the three
post-publish-smoke rows pass. On a stable one registry must also pass and the
GitHub Release must be public, not draft, not prerelease, marked as latest and
have the 11 assets above. On a prerelease the registry job and the latest mark
are not expected.

Download the published assets and verify the full manifest from outside the
workspace that built them:

~~~bash
tmp_dir="$(mktemp -d)"
gh release download vX.Y.Z --repo Luqueee/kivgraph --dir "$tmp_dir"
(
  cd "$tmp_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c SHA256SUMS
  else
    shasum -a 256 -c SHA256SUMS
  fi
)
~~~

If post-publish-smoke, registry or the external verification fail, the release is
not considered good even if GitHub shows the tag. Record which asset, job, digest
or metadata failed before deciding on the repair.

Publication does not end at GitHub either. Update the deployed landing from the
`main` that contains the release, build it with `make landing-check` and
`make landing-build`, reload `kivgraph-landing` and check from outside:

~~~bash
curl -fsS https://kivgraph.dev/releases/ | grep -F "vX.Y.Z"
curl -fsS https://kivgraph.dev/telemetry/ | grep -F "/api/telemetry/first-run"
test "$(curl -sS -o /dev/null -w '%{http_code} %{redirect_url}' \
  https://kivgraph.dev/install.sh)" = \
  "302 https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh"
~~~

The first-run collector and the D1 dashboard are part of the distribution smoke.
Check that the exact route answers `204`, that the deployed Worker has its D1
binding and its daily hash secret, and that the migration that adds the version
is applied **before** publishing the emitter. Do not send a synthetic event to
put a version on the chart: if the smoke really runs an installer or a published
binary, its row must appear in the dashboard; if it runs neither, the absence is
still the honest result. A real installation that was observed and does not
appear is an incomplete release, not something deferred to the next cron. The
authoritative commands, with the PASS criteria for the deployed version, the
binding, the secret, the migration, the route and the dashboard, are in section
**1i. The first-run Worker and D1** of `docs/development/analytics.md`; run
them, do not replace them with a visual inspection of the panel.

## 7. Failures and recovery

- Before pushing the tag: fix it in the same preparation and repeat all the
  gates.
- After pushing the tag but before creating the GitHub Release: the tag is
  already consumed. Fix forward with the next number; do not delete or reuse the
  failed tag.
- After creating the GitHub Release: do not retag, delete the release or replace
  artifacts without an explicit decision. The workflow uses --clobber; repeating
  it on an existing release can replace assets and reset their download
  counters.
- If only the MCP Registry fails, keep the GitHub Release and separate the
  diagnosis from the already published artifact. Do not attribute to the
  registry a success that does not appear in the registry job.

The normal fix is a new release with a higher number. An installation that has
been published is never repaired by silently changing the contents of a tag that
a user has already seen.

## Sources of truth

When a claim in this skill seems not to match the behaviour, consult in this
order:

1. .github/workflows/release.yml, which defines the real delivery;
2. internal/version/documented_test.go, which discovers the documented versions
   and the mandatory note;
3. scripts/verify-bundle.sh, scripts/build-mcpb.sh and
   scripts/build-server-json.sh, which define the artifact contracts;
4. docs/development/mcp-registry.md and
   docs/release/production-qualification.md, for the restrictions and the
   qualification evidence.
