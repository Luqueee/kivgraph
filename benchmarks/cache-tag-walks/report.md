# What skipping tagged cache directories saves in a pass

> **Result.** On six repositories, one of which holds a Rust `target/` of `49.976`
> entries with a `CACHEDIR.TAG`, the pass after an edit takes a median `14,12 s`
> with [ADR 0122](../../docs/adr/0122-skip-directories-a-tool-tags-as-caches.md)
> applied and `16,91 s` without it: `-2,79 s`, `-16,5 %`, over `24` passes each,
> with the three arms interleaved in one window. Analysis falls `7,97` to `6,01 s`.
> A first batch under a heavier load, without the control arm, gives `18,36` to
> `13,83 s`. The rule also beats the manual `exclusions: [target]` that the
> earlier measurement used (`15,14 s`), by about a second. It is one tagged
> directory in one repository, on macOS.

`benchmarks/publication-profile` found that the walks of a pass enter a Rust
`target/`, and that excluding one of `47.000` entries saved `0,9`-`1,1 s`. ADR 0122
proposes to skip any directory a tool tags as a cache, in every walk that decides
what the sources are. This is the measurement its Verification section asks for.

## Method

`benchmarks/edit-frequency`, built twice and run as two binaries because the rule
has no switch:

- **A**: built from `main` at `2fad171`, before the change.
- **B**: the same tree with the rule applied.
- **C** (second batch only): A with `exclusions: [target]` on that repository, the
  setting the earlier measurement used.

Corpus: the six repositories of `publication-profile`, `53.7xx` symbols, a
`repositories.yaml` with **no** `target` exclusion for A and B. Each run is eight
edits after a warm-up pass; the fact cache, state directory and scratch directory
are removed before every run. A round runs one run of each arm back to back, and
the arms alternate so a load that drifts does not belong to one of them. `results.json`
holds every pass: total, analysis, publication and the `verify sources` step.

|batch|arm|passes|pass, median|analysis|publication|`verify sources`|
|---|---|---|---|---|---|---|
|second|B, rule|24|`14,12 s`|`6,01 s`|`7,96 s`|`1,28 s`|
|second|A, before|24|`16,91 s`|`7,97 s`|`8,41 s`|`1,45 s`|
|second|C, manual exclusion|24|`15,14 s`|`7,09 s`|`8,21 s`|`1,45 s`|
|first|B, rule|24|`13,83 s`|`6,00 s`|`7,88 s`|`1,18 s`|
|first|A, before|24|`18,36 s`|`8,91 s`|`9,16 s`|`1,97 s`|

- **B is faster than A in all six rounds**, by `1,9` to `5,8 s` of the run median
  (`13,55`/`15,10`/`13,90` against `17,51`/`20,52`/`19,71` in the first batch;
  `13,96`/`13,94`/`15,63` against `15,92`/`15,82`/`18,91` in the second).
- **The first batch ran at a load average of `22` falling to `9`**, and A's worst
  pass there took `64 s`. Its figure is larger because the walks are I/O and the
  machine was busy; the second batch ran at `6`-`9` and is the one to cite.
- **The rule beats the manual exclusion by about `1 s`.** The exclusion is applied
  by the inventory and the discovery walks; `TreeDigest` takes no exclusions and
  still entered `target/`, twice per pass. That gap is consistent with the
  difference, but this run does not isolate it.
- **The `verify sources` step moves by `0,17 s`** (`1,45` to `1,28 s`) in the second
  batch, which is its share of the tree walked once more. The rest of the
  saving is in analysis, where the inventory, the discovery and the first
  observation walk.

## Did the result change?

The harness reports the symbol count of each pass. It is `53.337` to `53.344` in
every pass of every arm and the same in A and B, so the rule did not remove a
symbol the earlier code produced. That is a coarse check: the harness does not
compare the graph with a clean rebuild, and the passes' own `integrity` and probe
verification ran in both. The only tagged directory in this corpus holds no
source any provider reads, which is what ADR 0122 argues in general.

## Entries

Counted with a throwaway directory walk applying the digest's skip rule
(`.git` and `node_modules` skipped): `113.525` entries over the six repositories,
`105.022` in one of them, and `49.976` of those under the one tagged directory
(`44 %` of all entries). **Not measured: the entries each of the five walks visits
in a pass.** The ADR asks for that and this run does not provide it; the
profile counted CPU, not entries.

## Limitations

- One tagged directory, in one repository, on one macOS volume. Linux, a smaller
  `target/`, and a repository with several tagged directories are not measured.
- `fuzz/target/` (`1.926` entries) and the `49.057`-entry test-output directory of
  the same repository carry no tag and are still walked in every arm.
- The two binaries are separate builds. The `commit` field in `results.json`
  files names the repository commit the harness ran in, not the binary.
- Load changed between and within the batches; only the interleaved comparison
  and its round-by-round direction are evidence, not the absolute seconds.

## Reproduce

```bash
git worktree add /tmp/base 2fad171
go build -tags ladybug -o /tmp/ef-base /tmp/base/benchmarks/edit-frequency
go build -tags ladybug -o /tmp/ef-rule ./benchmarks/edit-frequency
# one repositories.yaml with no exclusion for the tagged directory, then per round:
/tmp/ef-rule -config <config> -edits 8 -grep-repeats 1 -root <r> -fact-cache <c> -scratch <s> -output out.json
/tmp/ef-base -config <config> -edits 8 -grep-repeats 1 -root <r> -fact-cache <c> -scratch <s> -output out.json
```

Remove the root, the fact cache and the scratch directory before each run, build
with the native LadybugDB library as `AGENTS.md` describes, and keep
`GOMODCACHE` pointing at the real module cache: without it the graph comes out
hollow, which the harness rejects.
