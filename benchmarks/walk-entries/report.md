# How many directory entries a pass looks at

> **Result.** On six repositories, the pass after one edit makes the walks of the
> kivgraph process examine **`446.857` directory entries with
> [ADR 0122](../../docs/adr/0122-skip-directories-a-tool-tags-as-caches.md)
> applied and `803.626` without it** (`-44 %`), for a tree of `63.548` entries
> outside tagged directories (`114.516` with them). That is **`7,0` entries
> examined per entry in the tree, with the rule on and off alike**: five walks
> read the same tree, two of them twice. The counts are identical in all six edit
> passes of each arm, so they are a property of the code and not of the load.

ADR 0122 listed five walks and said their entries per pass were not counted. This
counts them, every walk in the process and not only the five, with the real code.

## Method

`instrument.py` takes a throwaway worktree of `main` (`e6b2c93`) and replaces
every `filepath.WalkDir` and `os.ReadDir` in non-test code under `internal/` and
`cmd/` (28 files) with the counting versions of `walkcount.go.txt`. Each counts the
entries it is given and names the walk by the function that called it, so a walk
nobody listed is still counted. It makes the `edit-frequency` harness reset the
counters before a pass and print them after it, and adds `KIVGRAPH_NO_CACHETAG`,
which turns the ADR 0122 rule off, so one binary measures both sides.

Two rounds per arm, each a warm-up pass and three edit passes, `-edits 3`, the fact
cache removed before every run, the same corpus as `benchmarks/cache-tag-walks`
(no `exclusions` for the tagged directory). `results.json` holds every count.

## What each walk examines in an edit pass

|walk|rule off|rule on|saved|directories read|
|---|---|---|---|---|
|`sourceobservation.treeDigest`|`238.798`|`136.864`|`101.934`|`73` walks|
|`freshness.Capture`|`228.252`|`126.318`|`101.934`|`12` walks|
|`workspace.walkGoFiles` (Go discovery)|`216.306`|`114.372`|`101.934`|`1.910`|
|`workspace.walkTypeScriptFiles` (TypeScript discovery)|`114.085`|`63.118`|`50.967`|`1.410`|
|`indexer.countSourceFiles`|`6.185`|`6.185`|`0`|`36` walks|
|**total**|**`803.626`**|**`446.857`**|**`356.769`**|

- **The tagged `target/` is `50.967` entries and every walk that reaches it paid
  that.** The digest and the inventory paid it twice (two observations of each
  repository per pass: `12` inventory walks for `6` repositories), Go discovery
  twice, TypeScript discovery once. The rule removes exactly those multiples and
  nothing else: `countSourceFiles` roots at the analysis units and never enters it.
- **`treeDigest` is `73` walks, not two per repository.** Twelve are the two
  observations; the other `61` match the per-unit tree fingerprint the fact cache
  takes through the same function (`indexer.treeFingerprint`). By arithmetic that
  is consistent and not isolated: two observations of `63.548` entries are `127.096`,
  and the remaining `9.768` are the unit fingerprints.
- **Go discovery runs twice per pass.** `walkGoFiles` examines `216.306` entries,
  twice the roughly `108.000` that the trees of the Go repositories hold. Which two callers do
  it is not attributed (the counter names the walk, not who called it); `DiscoverGo`
  has call sites in `goworkspace/synthetic.go` and `goloader/crossrepo.go`, which are
  the likely two. [INFERENCE]
- **ADR 0122's list of five was short by one.** `indexer.countSourceFiles` is a
  walk of the same kind; it is small (`6.185`) and unaffected by the rule. The
  others in the code (`scratchtree`, the C# and Dart loaders, `semantic.go`,
  `rust_unit.go`) made no call in this corpus and are not measured.

## What it says about a shared traversal

Seven examinations per entry is the figure ADR 0122 weighed against "one shared
traversal that produces a listing the walks consume", and it is the same with the
rule on and off: the rule shrinks the tree, not the multiple. If one listing served
the digest, the inventory and both discoveries, the entries examined in a pass would
fall toward `63.548`, an upper bound of about `86 %` fewer. It is a bound on
directory entries, not on time: hashing the files, the part of `TreeDigest` that
reads bytes, would still be done, and ADR 0121 measured traversal as `62 %` of a
digest and hashing as `38 %` on a warm tree. Which share a pass spends here was
not measured.

## Limitations

- **Entries examined, not time.** The instrumented binary was not timed, so no
  cost per entry follows from this report.
- **Only walks in the kivgraph process.** `go list` (the Go loader), the
  TypeScript worker and `rust-analyzer` read directories too and are not counted.
- **`DiscoverCargo`, the TypeScript glob expansion and the unclaimed TypeScript
  walk made no call** in this corpus (no Rust language registered, no
  `include_unclaimed_sources`). They are not measured.
- `WalkDir` counts every callback, including a pruned directory itself; `ReadDir`
  counts the entries listed in each directory it enters. Both are entries
  examined and are comparable, not identical.
- One macOS volume, one corpus, one repository holding the only tagged directory;
  that directory held `49.976` entries when first counted (`cache-tag-walks`) and
  `50.967` here, because its owner builds in it.
- The worktree is instrumented, so `commit` in `results.json` says so. The
  committed code is not changed by this benchmark.

## Reproduce

```bash
git worktree add --detach /tmp/wc main
python3 benchmarks/walk-entries/instrument.py /tmp/wc
(cd /tmp/wc && go build -tags ladybug -o /tmp/ef-walks ./benchmarks/edit-frequency)
# per arm, fact cache and root removed first; the second arm sets KIVGRAPH_NO_CACHETAG=1
/tmp/ef-walks -config <config> -edits 3 -grep-repeats 1 -root <r> -fact-cache <c> -scratch <s> -output out.json 2> run.log
grep '^WALKCOUNT' run.log
```

Build with the native LadybugDB library as `AGENTS.md` describes, and keep
`GOMODCACHE` pointing at the real module cache: without it the graph comes out
hollow and the harness rejects it. A `PATH` containing a space must be quoted when
it is passed through `env`.
