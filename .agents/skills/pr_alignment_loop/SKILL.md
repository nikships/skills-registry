---
name: pr-alignment-loop
description: Iterate a PR to its final form by running two opposing reviewer droids (reviewer-robustness and reviewer-minimalist) in a bounded back-and-forth loop. The main orchestrator synthesizes their feedback, makes edits, runs tests, and stops when both approve, on stagnation, or after 3 rounds. Use when the user asks to "iterate on a PR", "polish a PR", "have the reviewers go back and forth", "align reviewers on a PR", or otherwise wants a PR brought to merge-ready quality by two opposing critics.
---

# PR Alignment Loop

You orchestrate two read-only reviewer droids over a real PR until it is merge-ready. You own the edits, the tiebreaks, and the stopping decision. The reviewers only critique.

The two droids:
- `reviewer-robustness` — biased toward correctness, edge cases, security, failure modes.
- `reviewer-minimalist` — biased toward simplicity, deletion, anti-speculative-complexity.

They are intentionally biased in opposite directions. The negotiated middle is the goal — not the union of their wishlists.

## Operating principles

- **You are the implementer.** Reviewers never edit code. You synthesize their feedback and edit.
- **Bias to ship.** Default is APPROVE once no `HIGH`-severity issue remains. Do not chase polish.
- **Anti-bloat is co-equal with anti-bug.** Adding code has a cost. Do not action `MED`/`LOW` robustness findings if the minimalist disagrees and there is no concrete failure path.
- **Hard 3-round cap.** No exceptions.
- **Log every conflict resolution.** When the two reviewers disagree, write down which side you took and why, in one sentence, in the final report.

## Inputs

The user will give you one of:
- A PR URL (`https://github.com/<owner>/<repo>/pull/<n>`)
- A PR number with repo context
- A branch name with an implicit "review this branch's diff against base"

If ambiguous, ask once for the PR identifier. Do not start the loop without it.

## Setup (before round 0)

1. Use `gh` CLI for everything PR-related. Never `FetchUrl` GitHub URLs (per global rules).
   - `gh pr view <id> --json title,body,baseRefName,headRefName,files,additions,deletions`
   - `gh pr checkout <id>`
   - `gh pr diff <id>` — capture the unified diff
2. Identify the project's verification commands by inspecting:
   - `package.json` scripts → prefer `test`, `lint`, `typecheck`, `build`
   - `Makefile` → look for `test`, `check`
   - `pyproject.toml` / `pytest.ini` / `tox.ini`
   - `Cargo.toml`, `go.mod`, etc.
   Record the commands you will run after each edit pass. If you cannot find any, note that — you'll skip the verification step but flag it in the final report.
3. Note the PR scope from the description. Anything the PR *intentionally* doesn't do is out of scope for reviewer findings.

## The loop

### Round 0 — initial parallel review

Spawn both reviewer droids **in the same assistant message** via parallel `Task` calls. Pass each:

- PR title, description, base branch, head branch
- The full unified diff
- List of changed file paths
- The project's stated scope (so they don't expand it)
- Round number: 0

Wait for both to return. Each will produce a `Verdict` + structured findings.

### Synthesis

For each finding:

1. **Dedupe**: collapse findings that target the same `file:line` and the same root cause across reviewers.
2. **Tag with reviewer source**: `[ROB]`, `[MIN]`, or `[BOTH]`.
3. **Promote to action list** if any of:
   - Either reviewer flagged `HIGH` *and* the failure path / cost is concretely described.
   - Both reviewers independently flagged `MED` on the same item.
4. **Conflict resolution** (e.g., ROB says "add validation here", MIN says "this branch is unreachable, delete it"):
   - If the code path is **demonstrably reachable** (caller exists, untrusted input, public API), ROB wins.
   - If the "risk" is **speculative** (no current caller, internal-only, framework already guarantees the invariant), MIN wins.
   - Record the decision: `Conflict at file:line — chose <side> because <one sentence>`.
5. **Drop**: everything else (lone `LOW`, cosmetic, style preference, "would be nice").

### Implement

1. Make the edits indicated by the action list. Keep diffs minimal — do not refactor surrounding code that wasn't flagged.
2. Run the verification commands you identified at setup. If anything fails, fix it before continuing. Test failures are not optional.
3. Commit with a message summarizing the round, e.g. `pr-align: round 1 — addressed 3 high, 2 med findings`. Do not push yet.

### Round N (N = 1, 2)

Re-spawn both reviewers in parallel. Pass them:

- The same PR context
- The diff **since the last round's commit** (most relevant) plus full PR diff for context
- Their own prior-round output (so they can mark Addressed / Still open / New)
- Round number: N

There are at most 3 rounds total: Round 0 (initial) plus Round 1 and Round 2 (iterative). Round 2 is the last review pass.

### Convergence check (after each post-Round-0 review)

Stop the loop if **any** of these hold:

- Both verdicts are `APPROVE`.
- No `HIGH` findings remain across either reviewer, and the `MED` items either (a) are not duplicated across both reviewers, or (b) are conflicts where you've already documented your decision.
- **Stagnation**: Zero new findings introduced this round *and* zero prior `HIGH` findings remain open. (If a reviewer keeps voting `REQUEST_CHANGES` with no new substantive justification, treat it as `APPROVE` and note "stagnation override" in the report.)
- **Hard cap**: Round 2 completed. Do not start a Round 3.

### Anti-sycophancy guard

Between rounds, before treating a reviewer's `APPROVE` or `REQUEST_CHANGES` at face value, sanity-check:

- If round N output is largely a restatement of round N-1 with no Round Delta section filled in, re-prompt the reviewer once with: *"You did not produce a Round Delta. Mark each prior finding Addressed / Still open / New. Do not introduce new findings to justify your verdict."*
- If a reviewer suddenly drops all findings in a single round with no edits explaining it, ask once: *"You previously flagged X. The code at file:line still appears the same. Confirm Addressed or restore the finding."*
- After one re-prompt, accept the response as final for that round.

### Reviewer failure handling

If a reviewer's Task call fails, times out, or returns output without a parseable `## Verdict:` line plus `## Findings` block:

1. Retry that reviewer once with the same prompt.
2. If it fails again, proceed with the surviving reviewer's findings for that round. Note "reviewer-X unavailable in round N" in the final report.
3. If both reviewers fail in the same round, stop the loop and surface the failure to the user. Do not silently approve.

### Tiebreak after Round 2

If you exit on the hard cap with unresolved disagreements:

- For each remaining open finding, decide using the same conflict rules (reachability > speculation, real cost > stylistic, concrete fix > vague concern).
- Document each decision in the final report.

## Final report

Once the loop terminates, produce a markdown summary back to the user:

```
# PR alignment loop — final report

**PR:** <id / url>
**Rounds:** <0 - N>
**Exit reason:** both-approve | stagnation | hard-cap

## Changes made
- <file:line — what changed and why, grouped by round>

## Conflicts resolved
- <file:line — ROB said X, MIN said Y, chose Z because …>

## Deferred (won't fix now)
- <file:line — finding, reviewer, reason it's deferred>

## Verification
- lint: <pass/fail/skipped>
- tests: <pass/fail/skipped>
- typecheck: <pass/fail/skipped>
- build: <pass/fail/skipped>

## Next step
<one sentence: push the branch, ask the user to push, or flag a blocker>
```

If the PR's scope/functionality changed during the loop, also propose an updated PR description via `gh pr edit --body` (per global instructions about keeping PR descriptions in sync). Confirm with the user before editing the description.

## Do not

- Do not run more than 3 rounds.
- Do not action `LOW` findings.
- Do not action `MED` findings unless both reviewers raised them.
- Do not push to the remote unless the user explicitly asks.
- Do not give either reviewer edit tools. Their job is to critique.
- Do not let either reviewer dictate scope expansion. If a finding pulls in unrelated refactors, defer it.
- Do not auto-approve a `HIGH` finding away because the reviewer hedged. If it has a real failure path, address it or document why deferred.
