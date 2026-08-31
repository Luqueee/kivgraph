---
name: merging-pull-requests
description: >-
  Merge a Kivgraph PR only after current-head CI and a clean CodeRabbit review;
  then delete its exact head branch. Use for explicit merge requests only.
---

# Merging a Kivgraph pull request

This skill performs the final, destructive PR operation. A request to merge a
specific PR authorizes both the merge and deletion of that PR's head branch.
Do not use this skill for requests to prepare a PR, check whether it is ready,
or enable auto-merge. The workflow below is self-contained and must not load
another skill's `SKILL.md` at runtime.

## Preconditions

1. Treat the PR as review-ready only when the intended changes are committed
   and pushed, relevant local gates have passed, required GitHub checks have
   completed successfully, CodeRabbit has reviewed the latest head, every
   actionable finding is addressed or explained, and GitHub reports no merge
   conflict or other known blocker. Stop if any condition is missing; do not
   merge around an unresolved finding.
2. Resolve exactly one PR. Use the PR number or URL supplied by the user; if
   it is absent, use the current branch's PR only when there is exactly one.
   Stop if the target is ambiguous.
3. Fetch live PR metadata from GitHub. Confirm that the PR is open, not a draft,
   mergeable, and has a concrete `headRefOid`, `headRefName`,
   `headRepository.nameWithOwner`, base branch, and repository. Never infer
   these values from a stale local checkout.
4. Check required status checks and review state. Wait for pending checks and
   CodeRabbit; stop on a failure, conflict, missing required check, or a review
   decision that still requests changes.

## CodeRabbit final gate

The final gate is about the current head SHA, not merely whether CodeRabbit has
reviewed the PR at some point.

1. Before waiting, record the current head SHA and a snapshot of all existing
   CodeRabbit reviews, inline comments, issue comments, and thread states. Keep
   each artifact's ID, commit or timestamp fields, body digest, and resolution
   state where available. Set `reviewCycleStartedAt` immediately after this
   snapshot. Identify the bot by its GitHub account/login returned by the API,
   not by a string in the comment body. The usual login is `coderabbitai[bot]`,
   but do not assume it.
2. Wait until the CodeRabbit check or review for that exact SHA has completed.
   A queued, in-progress, absent, or API-inaccessible review is a blocker.
3. Fetch all three surfaces again:
   - pull-request reviews;
   - pull-request review comments, including replies and threads;
   - issue comments on the PR, which cover comments outside the diff.
4. Compare the second snapshot with the first and save the second as the
   `postReviewSnapshot`. Any new CodeRabbit finding that requests a change
   blocks the merge, even if CI is green. A new clean summary is not a finding,
   but it must be reported.
5. Check existing findings as well. Every CodeRabbit comment concerning the
   current head must be resolved, obsolete with an evidence-based explanation,
   or already fixed. Do not treat an old comment as harmless just because it
   was posted on an earlier SHA. Unresolved review threads or a
   `CHANGES_REQUESTED` review block the merge.
6. Issue comments do not expose `commit_id`. Treat an issue comment as part of
   the current review cycle when its ID is new after `reviewCycleStartedAt`, or
   its body or `updated_at` changed after that checkpoint. Evaluate its body and
   ID even without a commit field. Pre-existing unchanged issue comments remain
   historical, but their unresolved actionable content still blocks the merge;
   missing `commit_id` alone is not uncertainty. If identity, timing, or status
   cannot otherwise be established, stop and report the uncertainty instead of
   guessing. After any fix and push, restart this gate from the new head SHA.

When using the GitHub CLI, the relevant read-only API surfaces are equivalent
to:

```text
gh api --paginate repos/{owner}/{repo}/pulls/{number}/reviews
gh api --paginate repos/{owner}/{repo}/pulls/{number}/comments
gh api --paginate repos/{owner}/{repo}/issues/{number}/comments
```

Use structured fields such as `commit_id`, `user.login`, `state`, `created_at`,
`submitted_at`, and thread resolution state. Do not rely on the abbreviated
output of `gh pr view` alone. Use the GraphQL `reviewThreads` connection when
the REST response does not expose whether an inline thread is resolved.

## Merge and branch deletion

Immediately before mutating anything, refresh the PR and take a fresh snapshot
of all three CodeRabbit surfaces: reviews, review comments, and issue comments,
including body digests, timestamps, commit IDs, states, and thread resolution
states where available. In the same live refresh, revalidate `state=OPEN`,
`isDraft=false`, `mergeable=MERGEABLE`, all required checks as complete and
passing, the review state, and the absence of a merge conflict or other known
blocker. Compare the CodeRabbit data with `postReviewSnapshot`. If any
CodeRabbit surface changed, any prerequisite is no longer merge-ready, or the
head SHA differs from the SHA that passed the final CodeRabbit gate, repeat the
entire gate. Otherwise capture the verified SHA as `gatedHeadSha` and retain
the PR's `headRepository.nameWithOwner` and `headRefName` as the cleanup target.
Never use the base repository or a same-named branch as a fallback.

Before requiring `--delete-branch`, determine whether the base branch requires
a merge queue using live GitHub branch-protection or repository metadata. The
GitHub CLI does not support combining a merge queue with `--delete-branch`.
When a queue is required, report a blocker and do not drop `--delete-branch`,
delete the branch before merging, or use `--auto`. Proceed only if GitHub
confirms a supported direct-merge path, the user explicitly authorizes any
required bypass (such as administrator privileges), and that path still
supports deletion after the merge.

For a supported direct merge, use the repository's configured or user-selected
method and include both `--delete-branch` and
`--match-head-commit "$gatedHeadSha"` in the `gh pr merge` command. Do not use
`--auto`: it can merge later after the verified review state is stale. Do not
close the PR separately; a successful merge closes it.

After the command:

1. Refetch the PR and verify `state=MERGED`, a merge timestamp, and a merge
   commit or equivalent merge result.
2. Resolve the remote repository from the saved
   `headRepository.nameWithOwner`, then check whether
   `refs/heads/<headRefName>` exists there. The preferred path is the CLI's
   `--delete-branch` behavior. If that exact ref still exists after a
   successful merge, including for a cross-repository PR, delete only that ref
   through GitHub and verify it is absent afterward. URL-encode branch names as
   needed; never check or delete a same-named ref in the base repository.
3. A protected branch or a deletion failure is not success. Report the PR as
   merged but the cleanup as failed, including the exact branch name; never
   force-delete another ref.
4. If another actor merged the PR during the final refresh, do not merge again.
   Verify the same post-merge and branch-deletion conditions, then report the
   race.

## Handoff

Report the PR URL and number, final head SHA, checks, CodeRabbit result (including
whether any new comments appeared), merge method and merge commit, exact branch
deletion result, and any skipped or blocked condition. Say explicitly if the PR
was already merged or if the branch cleanup still needs intervention.
