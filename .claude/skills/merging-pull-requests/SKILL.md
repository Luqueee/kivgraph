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
or enable auto-merge. Those requests stop at the handoff described by
`opening-pull-requests`.

## Preconditions

1. Read the repository instructions and
   `.claude/skills/opening-pull-requests/SKILL.md`. If the PR is not already
   review-ready, finish that workflow first; do not merge around an unresolved
   finding.
2. Resolve exactly one PR. Use the PR number or URL supplied by the user; if
   it is absent, use the current branch's PR only when there is exactly one.
   Stop if the target is ambiguous.
3. Fetch live PR metadata from GitHub. Confirm that the PR is open, not a draft,
   mergeable, and has a concrete `headRefOid`, `headRefName`, base branch, and
   repository. Never infer these values from a stale local checkout.
4. Check required status checks and review state. Wait for pending checks and
   CodeRabbit; stop on a failure, conflict, missing required check, or a review
   decision that still requests changes.

## CodeRabbit final gate

The final gate is about the current head SHA, not merely whether CodeRabbit has
reviewed the PR at some point.

1. Before waiting, record the current head SHA and a snapshot of all existing
   CodeRabbit review, inline-comment, and issue-comment IDs. Identify the bot by
   its GitHub account/login returned by the API, not by a string in the comment
   body. The usual login is `coderabbitai[bot]`, but do not assume it.
2. Wait until the CodeRabbit check or review for that exact SHA has completed.
   A queued, in-progress, absent, or API-inaccessible review is a blocker.
3. Fetch all three surfaces again:
   - pull-request reviews;
   - pull-request review comments, including replies and threads;
   - issue comments on the PR, which cover comments outside the diff.
4. Compare the second snapshot with the first. Any new CodeRabbit finding that
   requests a change blocks the merge, even if CI is green. A new clean summary
   is not a finding, but it must be reported.
5. Check existing findings as well. Every CodeRabbit comment concerning the
   current head must be resolved, obsolete with an evidence-based explanation,
   or already fixed. Do not treat an old comment as harmless just because it
   was posted on an earlier SHA. Unresolved review threads or a
   `CHANGES_REQUESTED` review block the merge.
6. If a comment cannot be tied to the current head or its status cannot be
   established, stop and report the uncertainty instead of guessing. After any
   fix and push, restart this gate from the new head SHA.

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

Immediately before mutating anything, refresh the PR and compare its head SHA
with the SHA that passed the final CodeRabbit gate. If it changed, repeat the
entire gate. Also confirm that the branch to delete is exactly the PR's
`headRefName`, never the base branch or a similarly named branch.

Use GitHub's normal, immediate merge command with the repository's configured
or user-selected merge method, and include `--delete-branch` in that command.
Do not use `--auto`: it can merge later after the verified review state is
stale. Do not close the PR separately; a successful merge closes it.

After the command:

1. Refetch the PR and verify `state=MERGED`, a merge timestamp, and a merge
   commit or equivalent merge result.
2. Verify that the exact remote head ref no longer exists. The preferred path is
   the CLI's `--delete-branch` behavior; if it reports that deletion did not
   happen, delete only that exact remote ref through GitHub and verify again.
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
