# Go and Bazel build-system cost

## Result

Keep Bazel as an optional proof of concept. Do not add it to ordinary CI,
replace a Make target, or expand its smoke suite. Go was faster in every
measured scenario on both the developer machine and the representative GitHub
Actions runner.

| environment | phase | Go median | Bazel median | Bazel / Go |
| --- | --- | ---: | ---: | ---: |
| macOS arm64 | clean total | 16.836 s | 59.402 s | 3.53x |
| macOS arm64 | warm no-op | 0.127 s | 0.143 s | 1.13x |
| macOS arm64 | one-file edit | 0.147 s | 0.154 s | 1.05x |
| Ubuntu 24.04 amd64 | clean total | 31.383 s | 86.858 s | 2.77x |
| Ubuntu 24.04 amd64 | warm no-op | 0.088 s | 0.314 s | 3.56x |
| Ubuntu 24.04 amd64 | one-file edit | 0.116 s | 0.490 s | 4.23x |

The clean total includes launcher setup, dependency acquisition, and the clean
build. Separating those phases shows the same outcome:

| environment | phase | Go median | Bazel median |
| --- | --- | ---: | ---: |
| macOS arm64 | setup | 0.006 s | 8.363 s |
| macOS arm64 | dependencies | 8.431 s | 30.340 s |
| macOS arm64 | clean build | 8.652 s | 20.698 s |
| Ubuntu 24.04 amd64 | setup | 0.003 s | 8.593 s |
| Ubuntu 24.04 amd64 | dependencies | 3.654 s | 29.271 s |
| Ubuntu 24.04 amd64 | clean build | 27.484 s | 48.994 s |

## Method

The corpus was Kivgraph's ordinary Go binary at
`//cmd/kivgraph:kivgraph` / `./cmd/kivgraph`, without the LadybugDB native tag,
pnpm projects, packaging, or release bundles. Both runs used Go `1.26.6`, Bazel
`9.2.0`, 1,828 tracked files, and the same `go.mod`, `MODULE.bazel`, and
`MODULE.bazel.lock` digests recorded in `results.json`.

The command was:

```bash
go run ./benchmarks/build-system-cost --trials 3 --output <output>
```

Each trial copied tracked files into separate Go and Bazel source trees and
started every cache empty. Go received private `GOCACHE`, `GOMODCACHE`, and
`GOPATH` directories. Bazel received a private `BAZELISK_HOME`,
`output_user_root`, repository cache, and disk/action cache, and ran with
`--ignore_all_rc_files`. No remote cache was configured. The arm order
alternated Go/Bazel, Bazel/Go, Go/Bazel.

The clean scenario timed setup, dependency acquisition, and build separately.
The resulting build warmed that arm; the next unchanged build was the warm
no-op sample. The harness then appended a unique comment to
`internal/version/version.go`, verified that exactly that tracked file changed,
and timed the rebuild. All reported values are medians of three trials; every
raw sample is in `results.json`.

The developer run used macOS arm64 with 10 logical CPUs at commit
`d955462b263de143b7afff39f7da74e079c32965`. The CI run used the GitHub-hosted
`ubuntu24` amd64 image with four logical CPUs at merge commit
`8269772f679566921c120050185b3c45271bf6db`, in [Actions run
33330648106](https://github.com/Luqueee/kivgraph/actions/runs/33330648106).

## Excluded runs

Two earlier CI runs are excluded. They showed implausible second and third
clean Bazel builds with `221 action cache hit` because
`bazel-contrib/setup-bazel` wrote a shared `startup --output_base` to the
runner's home rc file. That contradicted the declared empty-cache policy. The
final harness ignores all host rc files; all three accepted macOS clean builds
executed 217 processes and all three accepted Linux clean builds executed 223.

## Limitations

- GitHub's Go toolchain setup is outside the harness. Bazel launcher download
  is inside its setup phase because each trial has an empty `BAZELISK_HOME`.
- `bazel fetch` combines Go SDK and external dependency acquisition; no stable
  observed boundary separates them here.
- Hosted runners and developer machines have uncontrolled background load.
  Medians describe this corpus and are not timing guarantees.
- Remote caching was deliberately absent. A future remote-cache deployment or
  materially larger Go graph would require a new benchmark and decision.

The permanent workflow is manual because its accepted run occupied a hosted
runner for `7m42s`. It has no timing threshold and fails only when a build arm
cannot be measured.
