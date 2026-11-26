# Agent run record

These are session measurements, not timings from `mcp-token-cost --routes`.
That executable takes `--server`, `--dir` and `--repository`; it does not run
an agent or produce these reports. Corpus checkouts and indexes are separate.
All three measured repositories are public. Frozen revisions:

- `cobra`: `88b30ab89da2d0d0abb153818746c5a2d30eccec`
- `ky`: `3541888878c4d303a8bc2ad61815f4e048f55a5d`
- `ripgrep`: `4649aa9700619f94cf9c66876e9549d83420e16c`

## Headless trials

The retained OMP launchers ran on macOS arm64. In a fresh directory per
session, `prompt` held the assigned question and guide; `.omp/mcp.json`
registered `bench` for MCP arms. The stdio adapter launched the measured
`kivgraph serve` with `HOME` set to the indexed corpus home and exposed only
`find_symbol`, `find_references` and `get_source`. Native controls enabled
`grep,read`; MCP arms enabled `read,write` for MCP access, with the prompt
restricting allowed operations. Explicit hybrid arms had different permissions.
A native control ran with `KIVGRAPH_DISABLE_HOOK=1` to avoid graph routing.
The following is the retained **session** command with variable paths and
settings (not a command for recreating every historical adapter):

```sh
omp -p --model "$model" --thinking "$effort" --no-title --no-context-files \
  --no-rules --no-skills --no-extensions --no-lsp --mode json \
  --tools "$tools" --session-dir "$PWD/sessions" "$(cat prompt)" \
  >output.jsonl 2>stderr
```

Use each artifact's recorded prompt, arm order, restrictions, model and
reasoning setting where provided. Do not substitute `--hide-thinking` for
`--thinking off`. No model seed was supplied in this command. Fixed or
recorded session order is not a provider sampling seed; where a randomized
schedule has no recorded generator seed, the order is the replay input.
The six-question global runs used concurrent corpus lanes, serial within
each corpus. Session seconds are summed, not batch wall time. Provider cache
state and concurrent load were not controlled. A matching schedule or binary
does not imply deterministic answers or costs.

`six-concise-guide-ten.json` used Python `random.Random(7241)` to shuffle
the starting permutations before reversing each adjacent pair. The paired
Rust-only selector run retained that order after removing the old guide arm;
`six-narrow-rust-vs-grep-ten.json` reused the resulting two-arm order. The
captured concise-guide adapter starts `/tmp/kv-selector-v2` with SHA-256
`e0cba1151a12528e434e4a26e73ffc5c0be1122e29edfc58ffd480cf50c1824b`.
Its build metadata identifies a dirty `43d1a11c` base, not a source patch.

The native server build used `scripts/fetch-ladybug.sh`, `CGO_ENABLED=1`,
`CGO_CFLAGS=-I$LIB`, `CGO_LDFLAGS=-L$LIB`, `go build -tags ladybug`, and
`-ldflags="-extldflags=-Wl,-rpath,$LIB"`. A corpus revision is not the
Kivgraph source revision. Digests identify binaries rather than the dirty
source that produced them. Do not use the publishing commit as a substitute.
Exact historical patches, per-run host/tool versions and provider hardware
were not recorded for every run. The live-option and automatic-edge trials
include their experimental source diff. For identical-binary guide trials,
use the recorded binary digest for both arms.
The [build inventory](agent-builds.json) maps the reported SHA-256 values to
`go version -m` on the retained executable bytes: 18 distinct binaries and
46 matching reports. An empty match list means no digest match was found,
not that the artifact was produced by the last known build.

## Earlier adapters

`selective-grep.json` used OMP scouts and `rg -n` plus range reads versus a
fresh graph server per call. `persistent-selective-grep.json` used one stdio
server per agent through a local bridge. The historical adapters and launch
functions are in `replay/scouts.py`; load them in OMP Python eval, not in
standalone Python (`tool`, `agent` and `wait` come from the host). They expect
the frozen checkouts at `/tmp/kivgraph-corpora/{cobra,ky,ripgrep}/src`, a
published index in each sibling `home`, the native binary from `de7a74e0`
at `/tmp/kv-main`, and `/opt/homebrew/bin/rg`. From the repository root:

```python
%load benchmarks/mcp-token-cost/replay/scouts.py
# Restart per call. Blocks 4 and 5 were the two retained blocks.
for block in (4, 5):
    await wait(list(runCleanN(block).values()))
# Persistent stdio. Wait for the first repetition before starting the second.
try:
    for rep, kinds in [(1, ("control", "typed")), (2, ("typed", "control"))]:
        for kind in kinds:
            launchP(rep, kind)
        await wait([h for (i, _, _, _), h in hP.items() if i == rep])
finally:
    for broker in procsP.values():
        broker.terminate()
        broker.wait()
```

`ky-native-tools.json` exposes three named tools or a single generic `query`
on the same server. Set OMP's `bench` MCP server command to `python3` and
its arguments to `replay/ky_schema.py`, `native` or `generic`, and a fresh
RPC log path (absolute paths). Use the report's question and arm order.
The wrapper is part of the experiment, not a product feature. No provider
seed is recorded for the scouts.

## Artifact index

The records below carry the captured metrics, order, inputs and limitations
where available. Missing historical values are unknown, not inferred from
later sessions. Workstation paths in published transcripts are redacted as
`<home>` and `<checkout>` without changing calls, answers' substantive text,
classification, or usage accounting.

- [`cobra-body-first-guide-ab-ten.json`](cobra-body-first-guide-ab-ten.json)
- [`cobra-doc-context-guide-ab-ten.json`](cobra-doc-context-guide-ab-ten.json)
- [`cobra-go-package-direct-stop-off-ab-ten.json`](cobra-go-package-direct-stop-off-ab-ten.json)
- [`cobra-outgoing-guide-ab-ten.json`](cobra-outgoing-guide-ab-ten.json)
- [`ky-get-content-length-caller-guide-ab-ten.json`](ky-get-content-length-caller-guide-ab-ten.json)
- [`ky-get-content-length-caller-guide-vs-grep-ten.json`](ky-get-content-length-caller-guide-vs-grep-ten.json)
- [`ky-get-content-length-context-lines-ab-ten.json`](ky-get-content-length-context-lines-ab-ten.json)
- [`ky-get-content-length-context-schema-vs-grep-ten.json`](ky-get-content-length-context-schema-vs-grep-ten.json)
- [`ky-get-content-length-final-mcp-vs-grep-ten.json`](ky-get-content-length-final-mcp-vs-grep-ten.json)
- [`ky-get-content-length-line-schema-ab-ten.json`](ky-get-content-length-line-schema-ab-ten.json)
- [`ky-get-content-length-position-ab-ten.json`](ky-get-content-length-position-ab-ten.json)
- [`ky-get-content-length-predicate-guide-off-ab-ten.json`](ky-get-content-length-predicate-guide-off-ab-ten.json)
- [`ky-get-content-length-predicate-guide-off-replication-ab-ten.json`](ky-get-content-length-predicate-guide-off-replication-ab-ten.json)
- [`ky-get-content-length-predicate-preview-off-ab-ten.json`](ky-get-content-length-predicate-preview-off-ab-ten.json)
- [`ky-get-content-length-predicate-preview-off-replication-ab-ten.json`](ky-get-content-length-predicate-preview-off-replication-ab-ten.json)
- [`ky-get-content-length-reference-evidence-ab-ten.json`](ky-get-content-length-reference-evidence-ab-ten.json)
- [`ky-get-content-length-shown-range-ab-ten.json`](ky-get-content-length-shown-range-ab-ten.json)
- [`ky-get-content-length-shown-range-vs-grep-ten.json`](ky-get-content-length-shown-range-vs-grep-ten.json)
- [`ky-get-content-length-subject-definition-ab-ten.json`](ky-get-content-length-subject-definition-ab-ten.json)
- [`ky-get-content-length-subject-definition-vs-grep-ten.json`](ky-get-content-length-subject-definition-vs-grep-ten.json)
- [`ky-get-content-length-whole-callable-ab-ten.json`](ky-get-content-length-whole-callable-ab-ten.json)
- [`ky-get-content-length-whole-callable-vs-grep-ten.json`](ky-get-content-length-whole-callable-vs-grep-ten.json)
- [`ky-native-tools.json`](ky-native-tools.json)
- [`persistent-selective-grep.json`](persistent-selective-grep.json)
- [`rust-checked-short-name-off-ab-ten.json`](rust-checked-short-name-off-ab-ten.json)
- [`rust-inverse-caller-guide-ab-ten.json`](rust-inverse-caller-guide-ab-ten.json)
- [`rust-inverse-caller-result-ab-ten.json`](rust-inverse-caller-result-ab-ten.json)
- [`rust-narrow-selectors-ten.json`](rust-narrow-selectors-ten.json)
- [`rust-next-impl-automatic-local-edges-off-ab-ten.json`](rust-next-impl-automatic-local-edges-off-ab-ten.json)
- [`rust-next-impl-frozen-edge-off-ab-ten.json`](rust-next-impl-frozen-edge-off-ab-ten.json)
- [`rust-next-impl-live-caller-guide-off-ab-ten.json`](rust-next-impl-live-caller-guide-off-ab-ten.json)
- [`rust-next-impl-live-caller-option-off-ab-ten.json`](rust-next-impl-live-caller-option-off-ab-ten.json)
- [`rust-outgoing-evidence-schema-ab-ten.json`](rust-outgoing-evidence-schema-ab-ten.json)
- [`rust-receiver-selector-ab-ten.json`](rust-receiver-selector-ab-ten.json)
- [`rust-required-inverse-wrappers-off-ab-ten.json`](rust-required-inverse-wrappers-off-ab-ten.json)
- [`rust-required-inverse-wrappers-off-replication-ab-ten.json`](rust-required-inverse-wrappers-off-replication-ab-ten.json)
- [`rust-required-scoped-hybrid-off-ab-ten.json`](rust-required-scoped-hybrid-off-ab-ten.json)
- [`rust-scoped-hybrid-off-ab-ten.json`](rust-scoped-hybrid-off-ab-ten.json)
- [`rust-selectors-ab-ten.json`](rust-selectors-ab-ten.json)
- [`rust-short-name-first-off-ab-ten.json`](rust-short-name-first-off-ab-ten.json)
- [`selective-grep.json`](selective-grep.json)
- [`selector-schema-ab.json`](selector-schema-ab.json)
- [`selector-vs-stop-ten.json`](selector-vs-stop-ten.json)
- [`six-body-first-direction-vs-grep-ten.json`](six-body-first-direction-vs-grep-ten.json)
- [`six-checked-short-name-off-vs-grep-replication-ten.json`](six-checked-short-name-off-vs-grep-replication-ten.json)
- [`six-checked-short-name-off-vs-grep-ten.json`](six-checked-short-name-off-vs-grep-ten.json)
- [`six-concise-guide-ten.json`](six-concise-guide-ten.json)
- [`six-concise-guide.json`](six-concise-guide.json)
- [`six-current-short-name-guide-off-vs-grep-ten.json`](six-current-short-name-guide-off-vs-grep-ten.json)
- [`six-explicit-router-vs-grep-mcp-ten.json`](six-explicit-router-vs-grep-mcp-ten.json)
- [`six-final-mcp-vs-grep.json`](six-final-mcp-vs-grep.json)
- [`six-guided-mcp-vs-grep.json`](six-guided-mcp-vs-grep.json)
- [`six-held-out-caller-guide-vs-grep-ten.json`](six-held-out-caller-guide-vs-grep-ten.json)
- [`six-hybrid-vs-grep-mcp-ten.json`](six-hybrid-vs-grep-mcp-ten.json)
- [`six-manual-inverse-doc-context-vs-grep-ten.json`](six-manual-inverse-doc-context-vs-grep-ten.json)
- [`six-narrow-rust-vs-grep-ten.json`](six-narrow-rust-vs-grep-ten.json)
- [`six-native-vs-grep.json`](six-native-vs-grep.json)
- [`six-new-explicit-router-vs-grep-mcp-ten.json`](six-new-explicit-router-vs-grep-mcp-ten.json)
- [`six-output-format-ab-ten.json`](six-output-format-ab-ten.json)
- [`six-output-thinking-ab-ten.json`](six-output-thinking-ab-ten.json)
- [`six-rust-receiver-outgoing-vs-grep-ten.json`](six-rust-receiver-outgoing-vs-grep-ten.json)
- [`six-rust-selectors-vs-grep-ten.json`](six-rust-selectors-vs-grep-ten.json)
- [`six-thinking-off-mcp-vs-grep-ten.json`](six-thinking-off-mcp-vs-grep-ten.json)
