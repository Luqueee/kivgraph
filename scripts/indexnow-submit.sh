#!/usr/bin/env bash
#
# indexnow-submit.sh tells the IndexNow endpoint which URLs of kivgraph.dev
# changed, instead of waiting for a crawler to come back and notice.
#
# IndexNow is the closest thing that exists to "submitting a site to AI search":
# ChatGPT's search leans on Bing's index and Perplexity on Bing and Google, so
# what reaches Bing sooner is what an answer engine can cite sooner. It is a
# push, not a ranking signal -- it changes *when* a page is looked at, never
# whether it is liked.
#
# Ownership is proved by a file, not a token: `<key>.txt` at the site root
# containing exactly the key. Anyone can read it, which is the point -- it
# proves control of the host, so it is public by construction and committed
# rather than kept in a secret. `landing/src/indexnow.mjs` holds the key and
# `landing/src/indexnow.test.mjs` asserts the file and the key still agree,
# because the failure mode when they drift is a silent `403`.
#
# Usage:
#   scripts/indexnow-submit.sh                 # every URL in the live sitemap
#   scripts/indexnow-submit.sh /install/ /docs/cli/    # only these paths
#   INDEXNOW_DRY_RUN=1 scripts/indexnow-submit.sh      # print, submit nothing

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
host="kivgraph.dev"
origin="https://$host"
endpoint="https://api.indexnow.org/indexnow"

key="$(node --input-type=module -e "
import { INDEXNOW_KEY } from '$root/landing/src/indexnow.mjs';
process.stdout.write(INDEXNOW_KEY);
")"
if [[ ! "$key" =~ ^[0-9a-f]{8,128}$ ]]; then
	printf 'indexnow: key %q is not 8-128 hexadecimal characters\n' "$key" >&2
	exit 1
fi
key_location="$origin/$key.txt"

# The key file has to be reachable before anything is submitted. IndexNow
# answers a submission whose key it cannot fetch with a `403`, and a `403` here
# looks identical to a key that is simply wrong -- so the fetch is checked
# first, where the difference is still visible.
served="$(curl -fsS --max-time 20 "$key_location" || true)"
if [[ "$served" != "$key" ]]; then
	printf 'indexnow: %s does not serve the key\n' "$key_location" >&2
	printf '  expected: %s\n' "$key" >&2
	printf '  got:      %s\n' "${served:-<nothing>}" >&2
	printf '  the landing has to be rebuilt and restarted after the key file lands\n' >&2
	exit 1
fi

urls=()
if [[ $# -gt 0 ]]; then
	for path in "$@"; do
		urls+=("$origin${path}")
	done
else
	# The sitemap is the site's own answer to "what pages exist", so reading it
	# keeps this script from carrying a second list that would go stale.
	mapfile -t urls < <(
		curl -fsS --max-time 30 "$origin/sitemap-index.xml" |
			grep -o '<loc>[^<]*</loc>' | sed 's|<loc>||;s|</loc>||' |
			while read -r sitemap; do
				curl -fsS --max-time 30 "$sitemap" |
					grep -o '<loc>[^<]*</loc>' | sed 's|<loc>||;s|</loc>||'
			done | LC_ALL=C sort -u
	)
fi

if [[ ${#urls[@]} -eq 0 ]]; then
	printf 'indexnow: no URLs to submit\n' >&2
	exit 1
fi

# IndexNow caps one submission at 10 000 URLs. This site has 39 pages, so the
# cap is documented rather than handled: a site that grew past it would need
# batching, and silently truncating would report success for pages nobody sent.
if [[ ${#urls[@]} -gt 10000 ]]; then
	printf 'indexnow: %d URLs exceeds the 10000 per submission this script does not batch\n' "${#urls[@]}" >&2
	exit 1
fi

payload="$(
	printf '%s\n' "${urls[@]}" | jq -R . | jq -s \
		--arg host "$host" \
		--arg key "$key" \
		--arg keyLocation "$key_location" \
		'{host: $host, key: $key, keyLocation: $keyLocation, urlList: .}'
)"

printf 'indexnow: %d URLs to %s\n' "${#urls[@]}" "$endpoint"
if [[ -n "${INDEXNOW_DRY_RUN:-}" ]]; then
	printf '%s\n' "$payload"
	printf 'indexnow: dry run, nothing submitted\n'
	exit 0
fi

status="$(
	curl -s -o /tmp/indexnow-response -w '%{http_code}' --max-time 30 \
		-X POST "$endpoint" \
		-H 'Content-Type: application/json; charset=utf-8' \
		-d "$payload"
)"

# 200 is accepted; 202 is accepted with the key still to be validated. Both are
# success and the distinction is worth printing, because a 202 that never
# becomes a 200 means the key file stopped being reachable.
case "$status" in
200) printf 'indexnow: accepted (200)\n' ;;
202) printf 'indexnow: accepted, key validation pending (202)\n' ;;
400) printf 'indexnow: bad request (400) -- the payload was malformed\n' >&2 ;;
403) printf 'indexnow: forbidden (403) -- the key was not valid for this host\n' >&2 ;;
422) printf 'indexnow: unprocessable (422) -- a URL does not belong to %s\n' "$host" >&2 ;;
429) printf 'indexnow: rate limited (429) -- too many submissions\n' >&2 ;;
*) printf 'indexnow: unexpected status %s\n' "$status" >&2 ;;
esac
if [[ -s /tmp/indexnow-response ]]; then
	printf '  response: %s\n' "$(head -c 300 /tmp/indexnow-response)"
fi
case "$status" in 200 | 202) exit 0 ;; *) exit 1 ;; esac
