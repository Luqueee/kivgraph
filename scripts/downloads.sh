#!/usr/bin/env bash
set -euo pipefail

# The release download counters, read through the classification in
# `scripts/downloads.jq`.
#
#   downloads.sh now              what the API says right now, by class
#   downloads.sh snapshot         the record a day of the series stores
#   downloads.sh series <dir>     the daily traffic those records imply
#   downloads.sh selftest         checks the reader against known series
#
# `now` is a view over the same classification the series uses, not a second
# opinion about what a download is.
#
# Requires `jq`, and uses `gh` when it is authenticated: the unauthenticated
# API allows sixty requests an hour per address, which a paginated release
# list can spend on its own.

repository="${KIVGRAPH_REPOSITORY:-Luqueee/kivgraph}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() {
  printf 'downloads: %s\n' "$*" >&2
  exit 1
}

command -v jq >/dev/null 2>&1 || die "jq is required"

# fetchReleases prints the releases API response as a single JSON array.
#
# `gh api --paginate` prints one array per page, so the pages are slurped and
# concatenated here rather than assuming a single page or a gh new enough for
# `--slurp`.
fetchReleases() {
  if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    gh api "repos/$repository/releases" --paginate
  else
    curlReleases
  fi | jq -s 'add // []'
}

# curlReleases walks the pages itself, because the fallback has no `--paginate`.
#
# Stopping at the first hundred releases would not merely truncate the
# photograph: an asset missing from one snapshot and present in the next is
# indistinguishable from an asset published between them, so it contributes its
# whole cumulative total to that day. A truncated read does not lose history,
# it invents traffic.
curlReleases() {
  local page=1 body
  while :; do
    body="$(curl -fsS "https://api.github.com/repos/$repository/releases?per_page=100&page=$page")"
    printf '%s\n' "$body"
    [[ "$(jq 'length' <<<"$body")" -eq 100 ]] || break
    page=$((page + 1))
    # The loop ends on a short page. This is the guard for the day the API
    # stops giving one, so a runaway read fails instead of never returning.
    ((page <= 20)) || die "the releases API returned 20 full pages: refusing to keep asking"
  done
}

runJq() {
  jq -L "$here" "$@"
}

# takeSnapshot prints the cumulative record for one day.
takeSnapshot() {
  local capturedAt
  capturedAt="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  fetchReleases | runJq \
    --arg capturedAt "$capturedAt" \
    --arg repository "$repository" \
    'include "downloads"; snapshot($capturedAt; $repository)'
}

# showNow prints today's totals by class, and the platform split of the two
# classes that carry a binary.
showNow() {
  local snapshotJson
  snapshotJson="$(takeSnapshot)"

  printf 'Downloads for %s, all releases, as of %s\n\n' \
    "$repository" "$(jq -r .captured_at <<<"$snapshotJson")"

  printf '%-12s %10s  %s\n' "CLASS" "DOWNLOADS" "PLATFORMS"
  printf '%-12s %10s  %s\n' "-----" "---------" "---------"
  jq -r '
    [.assets[] | {class, platform, downloads}]
    | group_by(.class)[]
    | {
        class: .[0].class,
        downloads: ([.[].downloads] | add),
        platforms: (
          [.[] | select(.platform != "")]
          | group_by(.platform)
          | map("\(.[0].platform) \([.[].downloads] | add)")
          | join(", ")
        ),
      }
    | [.class, (.downloads | tostring), .platforms]
    | @tsv
  ' <<<"$snapshotJson" | while IFS=$'\t' read -r class downloads platforms; do
    printf '%-12s %10s  %s\n' "$class" "$downloads" "$platforms"
  done

  printf '\n%-12s %10s\n' "total" "$(jq '[.assets[].downloads] | add // 0' <<<"$snapshotJson")"

  local unclassified
  unclassified="$(jq -r '[.assets[] | select(.class == "other") | .asset] | unique | join(", ")' <<<"$snapshotJson")"
  if [[ -n "$unclassified" ]]; then
    printf '\nunclassified assets, counted in `other` and in nothing else: %s\n' "$unclassified" >&2
    printf 'teach scripts/downloads.jq about them before reading the series.\n' >&2
  fi
}

# showSeries reads a directory of daily snapshots and prints the traffic they
# imply. `--json` prints the rows instead of the table, for anything that wants
# to compute rather than read.
showSeries() {
  local directory="${1:-}" format="${2:-table}"
  [[ -n "$directory" ]] || die "series needs the directory holding the daily snapshots"
  [[ -d "$directory" ]] || die "$directory: not a directory"

  local files=()
  while IFS= read -r -d '' file; do files+=("$file"); done \
    < <(find "$directory" -type f -name '*.json' -print0)
  ((${#files[@]})) || die "$directory: no snapshots to read"

  # The order the files arrive in does not matter: `series` sorts by the
  # instant each snapshot was captured, which is the only ordering that is
  # true of the data rather than of the filesystem.
  local rows
  rows="$(jq -s '.' "${files[@]}" | runJq 'include "downloads"; series')"

  if [[ "$format" == "--json" ]]; then
    printf '%s\n' "$rows"
    return
  fi

  printf '%-12s %5s %8s %8s %8s %10s %12s %10s %7s\n' \
    "DATE" "DAYS" "TOTAL" "BUNDLE" "MCPB" "INSTALLER" "UNINSTALLER" "CHECKSUMS" "RESETS"
  printf '%-12s %5s %8s %8s %8s %10s %12s %10s %7s\n' \
    "----" "----" "-----" "------" "----" "---------" "-----------" "---------" "------"
  jq -r '
    .[]
    | [
        .date,
        (if .first_snapshot then "base" else (.days_covered | tostring) end),
        (.total | tostring),
        (.by_class.bundle // 0 | tostring),
        (.by_class.mcpb // 0 | tostring),
        (.by_class.installer // 0 | tostring),
        (.by_class.uninstaller // 0 | tostring),
        (.by_class.checksums // 0 | tostring),
        (.resets | length | tostring)
      ]
    | @tsv
  ' <<<"$rows" | while IFS=$'\t' read -r date days total bundle mcpb installer uninstaller checksums resets; do
    printf '%-12s %5s %8s %8s %8s %10s %12s %10s %7s\n' \
      "$date" "$days" "$total" "$bundle" "$mcpb" "$installer" "$uninstaller" "$checksums" "$resets"
  done

  printf '\nThe first row is the baseline: everything downloaded before the series\n'
  printf 'started. DAYS is the interval a row covers, so a row after a missed run\n'
  printf 'is not a spike.\n'

  jq -r '
    [.[] | .date as $date | .resets[] | "\($date) \(.tag)/\(.asset): \(.kind), \(.previous) -> \(.now)"]
    | select(length > 0)
    | "\nCounter resets, which `--clobber` causes and which cost the traffic\nbetween the last snapshot and the replacement:\n" + join("\n")
  ' <<<"$rows"
}

# selftest drives the reader over a series built to contain each thing that is
# not a subtraction. It runs before every scheduled snapshot, because the
# workflow is the only consumer and a reader that has started clamping real
# traffic to zero has no other way to say so.
selftest() {
  local work
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' RETURN

  cat >"$work/day1.json" <<'JSON'
{"schema":1,"repository":"o/r","captured_at":"2026-08-01T03:00:00Z","assets":[
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.tar.gz","id":1,"class":"bundle","platform":"linux-amd64","downloads":10},
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.mcpb","id":2,"class":"mcpb","platform":"linux-amd64","downloads":4},
  {"tag":"v1.0.0","asset":"install.sh","id":3,"class":"installer","platform":"","downloads":1},
  {"tag":"v1.0.0","asset":"uninstall.sh","id":4,"class":"uninstaller","platform":"","downloads":1}
]}
JSON
  cat >"$work/day2.json" <<'JSON'
{"schema":1,"repository":"o/r","captured_at":"2026-08-02T03:00:00Z","assets":[
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.tar.gz","id":1,"class":"bundle","platform":"linux-amd64","downloads":13},
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.mcpb","id":9,"class":"mcpb","platform":"linux-amd64","downloads":2},
  {"tag":"v1.0.0","asset":"install.sh","id":3,"class":"installer","platform":"","downloads":1},
  {"tag":"v1.0.0","asset":"uninstall.sh","id":4,"class":"uninstaller","platform":"","downloads":2}
]}
JSON
  # Day 3 is two days later: the run of the third of August never happened.
  cat >"$work/day4.json" <<'JSON'
{"schema":1,"repository":"o/r","captured_at":"2026-08-04T03:00:00Z","assets":[
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.tar.gz","id":1,"class":"bundle","platform":"linux-amd64","downloads":20},
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.mcpb","id":9,"class":"mcpb","platform":"linux-amd64","downloads":5},
  {"tag":"v1.0.0","asset":"install.sh","id":3,"class":"installer","platform":"","downloads":1},
  {"tag":"v1.0.0","asset":"uninstall.sh","id":4,"class":"uninstaller","platform":"","downloads":3},
  {"tag":"v1.1.0","asset":"kivgraph-darwin-arm64.mcpb","id":10,"class":"mcpb","platform":"darwin-arm64","downloads":7}
]}
JSON

  local rows
  rows="$(showSeries "$work" --json)"

  expect() {
    local description="$1" filter="$2" want="$3" got
    got="$(jq -r "$filter" <<<"$rows")"
    [[ "$got" == "$want" ]] || die "selftest: $description: want $want, got $got"
  }

  expect "the baseline row is every download so far" '.[0].total' 16
  expect "the baseline row is marked" '.[0].first_snapshot' true
  expect "the baseline row covers no interval" '.[0].days_covered' null
  expect "a counter that grew is a subtraction" '.[1].by_class.bundle' 3
  expect "a replaced asset contributes what the new one has" '.[1].by_class.mcpb' 2
  expect "and the replacement is recorded" '[.[1].resets[].kind] | sort | join(",")' replaced
  expect "an installer nobody fetched is zero" '.[1].by_class.installer' 0
  expect "the uninstaller is counted separately" '.[1].by_class.uninstaller' 1
  expect "the day is the sum of its classes" '.[1].total' 6
  expect "and a replacement is the only thing it had to explain" '.[1].resets | length' 1
  expect "a missed run leaves a row covering two days" '.[2].days_covered' 2
  expect "and the traffic of the day it missed is still there" '.[2].by_class.bundle' 7
  expect "uninstaller traffic survives a missed run" '.[2].by_class.uninstaller' 1
  expect "an asset seen for the first time contributes all of it" '.[2].mcpb_by_platform["darwin-arm64"]' 7
  expect "a platform that did not move is absent, not zero" '.[2].bundle_by_platform["darwin-arm64"] // "absent"' absent

  # A count that falls with the same id is the case the source should never
  # produce. It must reach the reader as a recorded anomaly and not as traffic.
  cat >"$work/day5.json" <<'JSON'
{"schema":1,"repository":"o/r","captured_at":"2026-08-05T03:00:00Z","assets":[
  {"tag":"v1.0.0","asset":"kivgraph-linux-amd64.tar.gz","id":1,"class":"bundle","platform":"linux-amd64","downloads":2}
]}
JSON
  rows="$(showSeries "$work" --json)"
  expect "an unexplained drop contributes nothing" '.[3].by_class.bundle' 0
  expect "and it is reported rather than smoothed" '.[3].resets[0].kind' unexplained

  printf 'selftest: ok\n'
}

case "${1:-now}" in
  now) showNow ;;
  snapshot) takeSnapshot ;;
  series) shift; showSeries "$@" ;;
  selftest) selftest ;;
  -h | --help | help)
    sed -n '3,18p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    ;;
  *) die "unknown command: $1" ;;
esac
