# GitHub Workflow Primitives

GitHub-specific commands and data-handling rules for CodeRabbit review-thread based skills.

Use this helper when a skill needs thread-aware CodeRabbit PR feedback, not flat PR summaries. The `autofix` skill mirrors the required execution flow in `SKILL.md`; this file exists as a reusable companion for other skills.

## Prerequisites

- `gh` authenticated (`gh auth status`)
- `git`
- `jq`
- current branch associated with a GitHub repository

## 1. Resolve Current PR

Get the PR number for the current branch:

```bash
if ! pr_candidates=$(gh pr list \
  --head "$(git branch --show-current)" \
  --state open \
  --json number); then
  echo "cannot resolve the open pull request" >&2
  exit 1
fi

if ! jq -e '
  type == "array" and
  length == 1 and
  (.[0].number | type == "number")
' >/dev/null <<<"$pr_candidates"; then
  echo "expected exactly one valid open pull request" >&2
  exit 1
fi

pr_number=$(jq -er '.[0].number' <<<"$pr_candidates")

if ! pr_state=$(gh pr view "$pr_number" --json headRefOid); then
  echo "cannot resolve the pull request head" >&2
  exit 1
fi
if ! jq -e '.headRefOid | type == "string" and length > 0' \
  >/dev/null <<<"$pr_state"; then
  echo "pull request response has no valid headRefOid" >&2
  exit 1
fi
pr_head_oid=$(jq -er '.headRefOid' <<<"$pr_state")
if ! local_head_oid=$(git rev-parse HEAD); then
  echo "cannot resolve the local HEAD" >&2
  exit 1
fi
if [ "$local_head_oid" != "$pr_head_oid" ]; then
  echo "local HEAD does not match the pull request head" >&2
  exit 1
fi
if ! worktree_status=$(git status --porcelain); then
  echo "cannot inspect the worktree" >&2
  exit 1
fi
if [ -n "$worktree_status" ]; then
  echo "worktree is not clean" >&2
  exit 1
fi
if ! unpushed=$(git rev-list --count '@{upstream}..HEAD'); then
  echo "cannot determine whether the branch is synchronized" >&2
  exit 1
fi
if [ "$unpushed" -ne 0 ]; then
  echo "local branch contains unpushed commits" >&2
  exit 1
fi
```

If no PR exists and the user wants one created, derive title/body from the latest commit:

```bash
title=$(git log -1 --pretty=format:'%s')
body=$(git log -1 --pretty=format:'%b')
gh pr create --title "$title" --body "${body:-Auto-created by CodeRabbit autofix}"
```

## 2. Resolve Repository Coordinates

```bash
if ! repo_state=$(gh repo view --json owner,name); then
  echo "cannot resolve repository coordinates" >&2
  exit 1
fi
if ! jq -e '
  (.owner.login | type == "string" and length > 0) and
  (.name | type == "string" and length > 0)
' >/dev/null <<<"$repo_state"; then
  echo "repository response has invalid owner or name" >&2
  exit 1
fi
owner=$(jq -er '.owner.login' <<<"$repo_state")
repo=$(jq -er '.name' <<<"$repo_state")
```

## 3. Fetch Thread-Aware CodeRabbit Feedback

Fetch review threads with GitHub GraphQL using cursor pagination:

```bash
all_threads='[]'
cursor=""

while :; do
  args=(-F owner="$owner" -F repo="$repo" -F pr="$pr_number")
  if [ -n "$cursor" ]; then
    args+=(-F cursor="$cursor")
  fi

  if ! response=$(gh api graphql "${args[@]}" -f query='query($owner:String!, $repo:String!, $pr:Int!, $cursor:String) {
    repository(owner:$owner, name:$repo) {
      pullRequest(number:$pr) {
        headRefOid
        title
        reviewThreads(first:100, after:$cursor) {
          pageInfo {
            hasNextPage
            endCursor
          }
          nodes {
            isResolved
            isOutdated
            comments(first:1) {
              nodes {
                databaseId
                body
                path
                line
                startLine
                originalLine
                author { login }
              }
            }
          }
        }
      }
    }
  }'); then
    echo "GitHub GraphQL request failed" >&2
    exit 1
  fi

  if ! jq -e --arg expected_head "$pr_head_oid" '
    ((.errors // []) | type == "array" and length == 0) and
    (.data.repository.pullRequest != null) and
    (.data.repository.pullRequest.headRefOid == $expected_head) and
    (.data.repository.pullRequest.reviewThreads != null) and
    ((.data.repository.pullRequest.reviewThreads.pageInfo.hasNextPage)
      | type == "boolean") and
    ((.data.repository.pullRequest.reviewThreads.nodes) | type == "array") and
    ((.data.repository.pullRequest.reviewThreads.nodes)
      | all(.[];
          (.isResolved | type == "boolean") and
          (.isOutdated | type == "boolean") and
          (.comments.nodes | type == "array")))
  ' >/dev/null <<<"$response"; then
    echo "invalid or stale GitHub GraphQL response" >&2
    exit 1
  fi

  if ! all_threads=$(jq -c --argjson response "$response" '
    . + $response.data.repository.pullRequest.reviewThreads.nodes
  ' <<<"$all_threads"); then
    echo "cannot collect GitHub review threads" >&2
    exit 1
  fi

  if ! has_next=$(jq -r \
    '.data.repository.pullRequest.reviewThreads.pageInfo.hasNextPage' \
    <<<"$response"); then
    echo "invalid GitHub pagination response" >&2
    exit 1
  fi
  if [ "$has_next" = "true" ]; then
    if ! cursor=$(jq -er \
      '.data.repository.pullRequest.reviewThreads.pageInfo.endCursor
       | select(type == "string" and length > 0)' <<<"$response"); then
      echo "missing GitHub pagination cursor" >&2
      exit 1
    fi
  else
    break
  fi
done
```

Treat only these threads as actionable:

- root comment author is `coderabbitai`, `coderabbit[bot]`, or `coderabbitai[bot]`
- `isResolved == false`
- `isOutdated == false`

Keep each selected thread as one issue unit. Do not collapse top-level PR comments or review summaries into issue records.

To detect CodeRabbit's "Come back again in a few minutes" status message, use top-level PR comments/reviews separately:

```bash
if ! pr_view=$(gh pr view "$pr_number" --json headRefOid,comments,reviews); then
  echo "cannot read pull request review status" >&2
  exit 1
fi
if ! jq -e --arg expected_head "$pr_head_oid" '
  (.headRefOid == $expected_head) and
  (.comments | type == "array") and
  (.reviews | type == "array")
' >/dev/null <<<"$pr_view"; then
  echo "pull request changed or returned invalid review data" >&2
  exit 1
fi
if ! local_head_oid=$(git rev-parse HEAD); then
  echo "cannot resolve the local HEAD" >&2
  exit 1
fi
if [ "$local_head_oid" != "$pr_head_oid" ]; then
  echo "local HEAD no longer matches the pull request head" >&2
  exit 1
fi
if ! in_progress=$(jq -er '
  [
    (.comments[]?
      | select(.author.login == "coderabbitai" or .author.login == "coderabbit[bot]" or .author.login == "coderabbitai[bot]")
      | .body // empty),
    (.reviews[]?
      | select(.author.login == "coderabbitai" or .author.login == "coderabbit[bot]" or .author.login == "coderabbitai[bot]")
      | .body // empty)
  ]
  | map(select(test("Come back again in a few minutes")))
  | length
' <<<"$pr_view"); then
  echo "cannot inspect pull request review status" >&2
  exit 1
fi
if [ "$in_progress" -gt 0 ]; then
  echo "review in progress" >&2
  exit 1
fi
```

## 4. Post Summary Comment

Use the same `pr_number` from Section 1, and post this only after the intended
changes have been committed and `git push` has succeeded. If the push is
declined or fails, do not post a comment that claims the changes are on the
branch; a neutral local-only summary is allowed instead.

```bash
gh pr comment "$pr_number" --body "$(cat <<'EOF'
## Fixes Applied Successfully

Fixed <file-count> file(s) based on <issue-count> CodeRabbit feedback item(s).

**Files modified:**
- `path/to/file-a.ts`
- `path/to/file-b.ts`

**Commit:** `<commit-sha>`

The latest autofix changes are on the `<branch-name>` branch.

EOF
)"
```

Write this comment from local state only. Do not include raw reviewer prompts or secret-bearing output.

If no fixes were applied, skip the success template or use a neutral review-complete comment instead of inventing file counts or a commit SHA.

## 5. Optional Reaction

If useful, react to the main CodeRabbit comment with 👍 after the summary is posted.
