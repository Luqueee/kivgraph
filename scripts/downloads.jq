# The classification of release assets, and the reader that turns a series of
# cumulative snapshots into daily traffic.
#
# It lives in one file because there are two readers -- the scheduled workflow
# that writes the series, and `scripts/downloads.sh` that shows today -- and two
# definitions of what a `bundle` is would make them a disagreement rather than
# two views.

# classify maps an asset name to its class and the platform it serves.
#
# The four classes are the four things a download can mean. A `bundle` is the
# archive `install.sh` unpacks; an `mcpb` is the same binary wrapped for a
# client that installs extensions itself and never runs the installer; the
# `checksums` and the `installer` are fetched by the install path and by anyone
# verifying it.
#
# `other` is deliberate: a release that publishes an asset shape nobody taught
# this file about must show up as unclassified, because the alternative is a
# class that quietly stops counting part of itself.
def classify:
  if . == "SHA256SUMS" then
    {class: "checksums", platform: ""}
  elif . == "install.sh" or . == "install.ps1" then
    # An installer serves whatever it can run on -- `install.sh` covers Linux
    # and macOS -- so it has no platform, and giving it one would invent a
    # split the asset does not have.
    {class: "installer", platform: ""}
  elif test("^kivgraph-[a-z0-9]+-[a-z0-9]+\\.mcpb$") then
    {class: "mcpb", platform: capture("^kivgraph-(?<p>[a-z0-9]+-[a-z0-9]+)\\.mcpb$").p}
  elif test("^kivgraph-[a-z0-9]+-[a-z0-9]+\\.(tar\\.gz|zip)$") then
    {class: "bundle", platform: capture("^kivgraph-(?<p>[a-z0-9]+-[a-z0-9]+)\\.(tar\\.gz|zip)$").p}
  else
    {class: "other", platform: ""}
  end;

# snapshot turns the releases API response into the record stored for a day.
#
# It is the cumulative total and not the delta since yesterday. A scheduled
# workflow is disabled by GitHub after sixty days without repository activity
# and a run can simply fail, so a missed day is expected rather than
# exceptional: the next cumulative photograph still carries the traffic of the
# day that was lost, and a stored delta would have lost it for good.
#
# `id` is stored because it is what tells a counter reset from a lie. Publishing
# with `--clobber` replaces the asset, and a replaced asset is a different asset
# whose counter starts at zero; its id changes with it.
def snapshot($capturedAt; $repository):
  {
    schema: 1,
    repository: $repository,
    captured_at: $capturedAt,
    assets: [
      .[]
      | .tag_name as $tag
      | .assets[]
      | (.name | classify) as $classified
      | {
          tag: $tag,
          asset: .name,
          id: .id,
          class: $classified.class,
          platform: $classified.platform,
          downloads: .download_count
        }
    ] | sort_by(.tag, .asset)
  };

def assetKey: "\(.tag)/\(.asset)";

def indexed: reduce (.assets[]) as $asset ({}; .[$asset | assetKey] = $asset);

# dayOf reads the calendar day a snapshot was captured on.
def dayOf: .captured_at | split("T")[0];

# deltaFor reports the traffic one asset took since the previous snapshot.
#
# Three of the four answers are not a subtraction:
#
#   - an asset nobody has seen before contributes everything it has. Its
#     counter started at zero after the previous snapshot, so the whole number
#     is traffic -- except in the first row of the series, where every asset is
#     unseen and the row is therefore the all-time total. That row carries
#     `first_snapshot`, and a reader computing a rate drops it;
#   - an asset whose id changed was replaced by `--clobber`, so the count on
#     screen belongs to the new asset and the old total is not a floor;
#   - a count that fell without the id changing is a reset this file cannot
#     explain. It contributes zero, and it is recorded, because silently
#     clamping an anomaly is how a broken source keeps looking healthy.
def deltaFor($previous):
  . as $asset
  | ($previous[$asset | assetKey]) as $before
  | if $before == null then
      {delta: $asset.downloads, reset: null}
    elif $before.id != $asset.id then
      {
        delta: $asset.downloads,
        reset: {tag: $asset.tag, asset: $asset.asset, kind: "replaced",
                previous: $before.downloads, now: $asset.downloads},
      }
    elif $asset.downloads < $before.downloads then
      {
        delta: 0,
        reset: {tag: $asset.tag, asset: $asset.asset, kind: "unexplained",
                previous: $before.downloads, now: $asset.downloads},
      }
    else
      {delta: ($asset.downloads - $before.downloads), reset: null}
    end;

def sumBy($rows; keyExpression):
  reduce $rows[] as $row ({}; .[$row | keyExpression] += $row.delta);

# series derives one row per snapshot from snapshots given in any order.
#
# The first row is the baseline: with nothing to subtract from, its `total` is
# every download the repository has ever served. `first_snapshot` marks it, and
# `days_covered` is null there because the row covers no measured interval.
def series:
  sort_by(.captured_at)
  | . as $snapshots
  | [
      range(0; $snapshots | length) as $index
      | $snapshots[$index] as $today
      | (if $index == 0 then {assets: []} else $snapshots[$index - 1] end) as $yesterday
      | ($yesterday | indexed) as $previous
      | [$today.assets[] | . + (deltaFor($previous))] as $rows
      | {
          date: ($today | dayOf),
          captured_at: $today.captured_at,
          # The gap is published because a delta that covers three days is not
          # a spike, and nothing else in the row says so.
          days_covered: (
            if $index == 0 then null
            else ((($today.captured_at | fromdateiso8601)
                   - ($snapshots[$index - 1].captured_at | fromdateiso8601)) / 86400
                  | . * 100 | round / 100)
            end
          ),
          first_snapshot: ($index == 0),
          total: ([$rows[].delta] | add // 0),
          by_class: sumBy($rows; .class),
          bundle_by_platform: sumBy([$rows[] | select(.class == "bundle")]; .platform),
          mcpb_by_platform: sumBy([$rows[] | select(.class == "mcpb")]; .platform),
          resets: [$rows[].reset | select(. != null)]
        }
    ];
