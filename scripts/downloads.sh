#!/usr/bin/env bash
set -euo pipefail

# Retrieves and aggregates the download counts from GitHub releases.
# Requires 'jq' and optionally 'gh' for authentication (avoids rate limits).

owner="Luqueee"
repo="kivgraph"

echo "Fetching release data for $owner/$repo..."

if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  json=$(gh api "repos/$owner/$repo/releases" --paginate)
else
  json=$(curl -sS "https://api.github.com/repos/$owner/$repo/releases")
fi

total=0
printf "%-15s %-20s %-10s\n" "RELEASE" "ASSET" "DOWNLOADS"
printf "%-15s %-20s %-10s\n" "-------" "-----" "---------"

while IFS=$'\t' read -r tag name count; do
  printf "%-15s %-20s %-10s\n" "$tag" "$name" "$count"
  total=$((total + count))
done < <(
  jq -r '
    .[] |
    .tag_name as $tag |
    .assets[] |
    [$tag, .name, .download_count] | @tsv
  ' <<<"$json"
)

echo ""
echo "Total downloads across all releases: $total"
