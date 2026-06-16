---
name: openspec-plan-review
description: Review and tighten OpenSpec change plans before implementation. Use when the user asks whether an OpenSpec plan/proposal/design/spec/tasks is solid, asks for gaps or holes in an OpenSpec change, wants a subagent-backed plan review, says "go again" during plan review, or says "yes and remember" to encode an accepted plan-review pattern.
---

# OpenSpec Plan Review

Review OpenSpec change artifacts for implementation readiness, then tighten clear plan/spec issues directly. Keep product-direction calls visible for the user.

## Reference File

Before reviewing, read [references/accepted-tightening-patterns.md](references/accepted-tightening-patterns.md) in full (~190 lines) to calibrate on repo-accepted patterns. Skim the pattern categories and the most recent commits — this keeps reviews consistent with what the user has already approved.

## Workflow

Two passes are always expected: first pass finds issues, second pass catches what tightening introduced or missed. Both passes are subagent-backed unless the change is trivial.

### 1. Identify the Change

- If the user named a change, use it.
- If ambiguous, run `openspec list --json` and inspect likely active changes before asking.
- Load `proposal.md`, `design.md`, `tasks.md`, and all `specs/**/spec.md`.

### 2. First Pass (Subagent)

Spawn a read-only reviewer subagent. Prompt it to read all artifacts, verify every referenced file/symbol/contract against the real codebase, and return findings in the standard format. While the subagent runs, the main thread can do spot-checks on areas of known risk (sync, auth, data-safety).

Skip the subagent only for trivial changes: single-file edits, no architecture/sync/auth/data-safety, no new specs. The main thread does the first pass directly in that case.

### 3. First-Pass Triage

Main thread reviews the subagent's findings:

- **Clear tighten-ups**: apply them immediately — patch plans/specs/tasks for factual corrections, missing test tasks, sharper acceptance criteria, safer scope boundaries, and clearer sequencing.
- **Product calls**: do not silently decide product semantics, UX direction, migration policy, naming that shapes user/operator understanding, or scope splits with real tradeoffs. Present product calls to the user and wait for decisions before proceeding.
- **Auto-fixable-only**: if all findings are clear tighten-ups with no human-judgment needed, proceed directly to the second pass.

### 4. Second Pass (Subagent)

Spawn a fresh subagent against the updated artifacts and codebase. Instruct it to compare new findings with the first-pass report — only patch newly discovered issues (facts that changed during tightening, gaps the first pass missed, or issues introduced by tightening itself). Second-pass findings should be fewer and sharper than first-pass.

### 5. Apply and Validate

- Apply any remaining second-pass tighten-ups.
- Run `openspec validate <change> --strict`.
- Inspect `git diff` and verify no unrelated changes were touched.

### 6. Report and Commit

Summarize the review in this order:

1. **Product calls** (if any remain unresolved) — tradeoff, default recommendation.
2. **What was tightened** — grouped by category, never ending with "etc":
   - "Architecture decisions" (design rewrites, approach changes)
   - "Spec clarifications" (scenario additions, requirement refinements)
   - "Task additions" (new subtasks, test coverage, migration steps)
   - "Code-reference corrections" (wrong file paths, wrong function names, stale line numbers)
   - "Naming and type alignment fixes" (vocabulary disambiguation, type field changes)
3. **Validation results** — output of `openspec validate` and `git diff --check`.
4. **What's solid** — areas that needed no changes (keep brief, after findings).

Then commit the tightened artifacts with a concise message.

## "Go Again"

If the user says "go again" after providing decisions or feedback:

1. Apply the user's decisions to artifacts first.
2. Run a fresh full dual-pass review (steps 2-5) against the updated artifacts.
3. Compare findings with the previous pass to avoid re-reporting already-fixed items.
4. Report remaining product calls and commit.

## "Yes And Remember"

If the user says "yes and remember" after accepting a review recommendation:

1. Apply the accepted change to the current OpenSpec artifacts if not already done.
2. Update [references/accepted-tightening-patterns.md](references/accepted-tightening-patterns.md) with a concise accepted rule.
3. Include:
   - `Rule`: the reusable review heuristic.
   - `Accepted because`: why the user accepted it.
   - `Use when`: trigger conditions.
   - `Example`: change name, file, or commit if available.
4. Keep this skill concise; put detailed examples in the reference file, not `SKILL.md`.

Only update the skill/reference when the user explicitly says to remember it or clearly asks to improve the skill.

## Review Heuristics

Look especially for:

- Reinvented machinery: a plan that adds new state, persistence, recovery, or wait/retry flow should be checked against existing subsystems first. If an existing persisted state machine or pipeline already implements the lifecycle the plan wants (e.g. bench-until-deadline → wait → decay → restore-across-restarts), prefer extending it over standing up a parallel mechanism that then needs coexistence guards. A tell: the plan itself enumerates "N plumbing changes" where several exist only to stop the new mechanism from fighting an existing one. Trace where the existing concept is actually owned (often it is a projection of another layer's state, not an independent source of truth) before specifying a sibling.
- Current-code mismatches: plan says a control/path/symbol behaves one way, but code says another.
- Vague tasks: missing files, symbols, module ownership, acceptance checks, or test files.
- Hidden product decisions: "for consistency" changes, data-retention choices, UX affordances, naming, or scope splits.
- Cross-change contracts: queues, sync, auth, migrations, retries, and shared state need explicit boundaries.
- Failure classifications: auth/retry/sync/error plans should classify transient, terminal, malformed, local-only, remote-applied, and rollback paths.
- Data safety: import/export/clear/migration plans need no-overwrite, idempotency, profile isolation, backup behavior, and rollback notes.
- Prior failure traps: add "Implementation Pitfalls", fixtures, or regression tests when past attempts failed on a specific behavior.
- Review routing: every review item should be accepted into scope, rejected, subsumed, or routed to another change.

## Subagent Findings Format

Subagents should return findings in this format:

```
Findings
- [severity] file:line - Concrete issue and why it can break implementation.
  Recommendation: exact artifact edit or product decision needed.

What's Solid
- Areas that are well-specified (keep brief).

Product Calls
- Question with the tradeoff and default recommendation.
```

Severity labels:
- `Critical`: plan likely sends implementation in the wrong direction or violates a contract.
- `Major`: likely implementation gap, missing test, stale assumption, or ambiguous behavior.
- `Minor`: wording, sequencing, or test specificity improvement.
