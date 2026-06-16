# Accepted OpenSpec Plan-Tightening Patterns

This reference captures repo-learned examples for `openspec-plan-review`. Use it to recognize plan-tightening moves the user has repeatedly accepted, and update it only when the user explicitly says "yes and remember" or asks to improve the skill.

## High-Signal History Search Terms

Use these terms when mining history for more examples:

- `tighten`, `tightened`
- `clarify`, `correct`, `align`, `disambiguate`
- `refine`, `revise`, `strengthen`
- `address review feedback`, `per review`
- `first-pass`, `second-pass`, `round 2`, `second-opinion deep review findings`
- `encode pitfall guidance`
- `record ... review follow-ups`, `record ... verify report`
- `route QA items`, `coordinate`, `owned by`, `subsumed`, `rejected`
- `update plans to ...`, especially remove/defer/re-add scope changes

Lower-signal subjects usually need filtering: `add/draft openspec plan` is often initial scaffolding; `mark task complete`, `archive`, `feat`, `test`, and `rally: run` are usually progress or implementation.

## Representative Commits

- `Prayer-app-2@2904b0a7` `2026-03-31` `chore: address review feedback on post-launch fix plans`
  Review-response pass across auth/list/prayer-duration/prayer-daily-targets/queue-state-sync design/spec/tasks.

- `Prayer-app-2@ac5e5cfe` `2026-04-01` `refine openspec expiry auth and queue sync plans`
  Converts broad auth refresh into a shared session-aware request layer and adds canonical daily queue snapshot semantics.

- `Prayer-app-2@bfce0519` `2026-04-01` `strengthen openspec plans for agent reliability`
  Adds concrete tooling mechanics, changed-file detection, a queue merge contract spec, and implementation-level task detail.

- `Prayer-app-2@92a8f4ad` `2026-05-04` `openspec(prayer-daily-targets): encode pitfall guidance for queue cap and dot semantics`
  Adds explicit Implementation Pitfalls and fixtures for known regressions: finite queue cap and action-based progress dots.

- `Prayer-app-2@6b419b8c` `2026-05-29` `docs(offline-first-startup): tighten plan for clarity and risk`
  Adds exact timeout, distinguishes `verified` from `isAuthenticated`, names concrete code references, cross-tab handling, and deterministic tests.

- `Prayer-app-2@c53b4578` `2026-05-29` `docs(offline-first-startup): correct malformed /auth/me error classification`
  Corrects a wrong plan assumption by tracing the real parsing path before locking status/error semantics.

- `Prayer-app-2@8b68ecc5` `2026-06-01` `docs: record offline startup review follow-ups`
  Stores review-report artifacts without mixing them into implementation.

- `rally-2@3b8e541` `2026-03-30` `Refine consolidate-rally-gry change: naming, store, hooks, resilience`
  Tightens vocabulary, removes SQLite, changes hooks to resume/report strategy, and scopes future mock CLI work.

- `rally-2@9bcd6c6` `2026-04-26` `Update plans to remove TUI and re-add later`
  Strong de-scoping example: removes TUI from the core change and creates `build-new-tui` as a later change.

- `rally-2@eff3ede` `2026-04-26` `Update plans to be rebased from main`
  Re-aligns the plan with mainline reality: hooks removed, inline parsing chosen, `session` renamed to `try`, config/log paths adjusted.

- `rally-2@3cc83f4` `2026-05-27` `refine harden-relay-run-lifecycle plans per review`
  Converts review findings into explicit failure classes, probation state, per-harness-model resilience, and concrete tasks.

- `rally-2@1465169` `2026-05-27` `incorporate second-opinion deep review findings`
  Deepens edge cases: incomplete runs leave changes uncommitted, `ResilienceKey`, one-shot probation, truncation preservation.

- `rally-2@7357d02` `2026-05-29` `docs: tightened openspec plans further`
  Corrects migration/data-safety choices: do not convert `progress.yaml`, do not touch `config.toml.bak`, append-only logs remain unbounded.

- `rally-2@3cd3fc3` `2026-06-02` `docs(git-hygiene): address second-pass review feedback`
  Second-pass precision: use a `rally:` commit-message prefix instead of author matching, exclude `.rally/` dirtiness, handle no-change finalization.

## Accepted Pattern Categories

### Verify real code paths before semantics

Plans should not lock status codes, parser behavior, storage paths, or auth state meanings until current code is checked.

Use when:
- The plan names a concrete error/status/state contract.
- The behavior depends on a parser, sync mapper, auth store, repository, or service worker path.
- The artifact asserts "current state" without code references.

Examples:
- `Prayer-app-2@6b419b8c`: separated `verified` from `isAuthenticated`.
- `Prayer-app-2@c53b4578`: corrected malformed `/auth/me` classification after tracing the real path.

### Replace vague work with executable contracts

Accepted tightening usually adds exact files, symbols, defaults, ordering rules, acceptance checks, and test files.

Use when:
- A task says "update the queue", "handle errors", "make it robust", or "verify UI" without files/tests.
- Implementation agents could choose different layers.
- A plan describes a behavior but omits how it is observed.

Examples:
- `Prayer-app-2@bfce0519`: added merge-contract spec and concrete task detail.
- `Prayer-app-2@6b419b8c`: added exact timeout and deterministic tests.
- `rally-2@3cd3fc3`: replaced brittle author matching with a commit-message prefix rule.

### Split or route scope explicitly

Review findings should be accepted, rejected, subsumed, or routed to another change. Broad work should be split when it would blur implementation ownership.

Use when:
- A plan contains unrelated UI/infra/core behaviors.
- A review finding is valid but not required for the current change.
- A change includes future-facing functionality without a crisp boundary.

Examples:
- `rally-2@9bcd6c6`: removed TUI from one change and re-added later.
- `rally-2@3b8e541`: scoped future mock CLI work.

### Encode pitfalls and fixtures from known failures

When previous attempts failed in a specific way, add an Implementation Pitfalls section, fixtures, and regression tests.

Use when:
- The user mentions a bug that has recurred.
- There are easy-to-miss semantics like queue caps, progress dots, merge order, or sync conflict states.
- A plan has a "do X" task but not the trap it must avoid.

Example:
- `Prayer-app-2@92a8f4ad`: encoded queue cap and action-based dot semantics.

### Classify failure modes

Auth, retry, sync, runner, and migration plans should define classes of failure and expected behavior per class.

Use when:
- There are transient vs terminal vs malformed cases.
- Retry/escalation/probation behavior is involved.
- Local and remote state can disagree.

Examples:
- `rally-2@3cc83f4`: explicit failure classes and probation state.
- `rally-2@1465169`: resilience keys and one-shot probation.

### Preserve data safety explicitly

Data plans should avoid surprise overwrites and make profile/import/export/migration cleanup explicit.

Use when:
- Work touches import/export, clear all data, profile switching, localStorage, DB migrations, backups, logs, or sync schema.
- The plan proposes migrating or deleting existing user-managed files.

Example:
- `rally-2@7357d02`: do not convert `progress.yaml`, do not touch `config.toml.bak`, append-only logs stay unbounded.

### Clarify overloaded vocabulary

Accepted reviews often disambiguate terms before they become implementation bugs.

Use when:
- A term appears with multiple possible meanings, such as authenticated vs verified, freeze vs stall vs benched, session vs try.
- The term appears in user-facing/operator-facing output or persisted data.

Examples:
- `Prayer-app-2@6b419b8c`: `verified` vs `isAuthenticated`.
- `rally-2@eff3ede`: `session` renamed to `try`.

### Reuse existing machinery before specifying new mechanisms

A plan that introduces new state, persistence, recovery, or wait/retry flow should first be checked against existing subsystems. When an existing persisted state machine or pipeline already implements the desired lifecycle, prefer extending it over a parallel mechanism. Parallel mechanisms force coexistence guards (one path taught not to clobber the other) and duplicate persistence/restore code — both are fragility sources the review should remove at the design stage, not document.

Use when:
- The plan adds a new flag/field/state to express "out of rotation", "paused", "deadline", "retry-after", or "restore on restart".
- The plan enumerates several plumbing steps where some exist only to keep the new mechanism from fighting an existing one (a guard scoped to "only this branch", a separate restoration scanner that re-reads what an existing replay already loads).
- The artifact treats a bit/field as an independent source of truth without tracing who actually owns it — it is often a per-cycle projection of another layer's state.

How to tighten:
- Trace the existing concept to its owner and lifecycle before specifying a sibling.
- If the existing lifecycle is missing only a parameter (e.g. a variable deadline) or a key dimension (e.g. scope), add that to the existing state rather than cloning the whole flow.
- Keep distinct domain concepts as distinct *states* sharing one mechanism, so the conceptual separation survives without duplicating the plumbing.

Example:
- `rally-2 improve-error-categorisation` (design Decision 5/6): a proposed `BenchUntil` field on the in-memory scheduler `EntryState`, plus a separate persisted reset event and a bespoke restoration scanner, was collapsed into a new `StateBenched` resilience state reusing the existing persist → `GetState` → recovery-sync → selection-wait pipeline. The scheduler's `Benched` bit turned out to be a per-cycle projection of resilience state, not an independent axis; the parallel design's `StateActive`-only unbench guard and restoration scanner became unnecessary (cross-relay persistence is free via event replay). Net: one fewer implementation lap and the fragile guard removed.

## Automatable Checks

- Search current artifacts for vague verbs without file/test anchors.
- Verify referenced paths/symbols with `rg`.
- Compare proposal/design/spec/tasks for scope drift.
- Detect extra behavior introduced "for consistency" and move it to a product call.
- Require classification tables for auth/error/retry/sync behavior.
- Require explicit review item routing: accepted, rejected, subsumed, owned by another change.
- Check data plans for profile isolation, import replacement, clear-all-data cleanup, no-overwrite, and rollback.
- For plans adding new state/persistence/recovery, grep the codebase for an existing state machine or pipeline covering the same lifecycle (bench/pause/freeze/retry/restore) and confirm the plan extends it rather than paralleling it.
- Run `openspec validate <change> --strict` after edits.

## Human/Product Judgment

Escalate these instead of deciding silently:

- Correct user-facing semantics and naming.
- Whether to split scope or keep a bundled change.
- Whether a behavior is product polish or architecture policy.
- Data-retention, migration, sync-schema, and backup tradeoffs.
- Which failure classes deserve alerts, retries, or escalation.
- Which code path is authoritative when plan intent and implementation reality disagree.

## Accepted Rules Log

Append future "yes and remember" entries here.

Format:

```markdown
### YYYY-MM-DD - Short Rule Name

- Rule:
- Accepted because:
- Use when:
- Example:
```

### 2026-06-09 - Reuse existing machinery before specifying new mechanisms

- Rule: When a plan adds new state/persistence/recovery/wait flow, trace whether an existing persisted state machine or pipeline already implements that lifecycle and extend it, instead of standing up a parallel mechanism that then needs coexistence guards and duplicate restore code. Watch for the "N plumbing changes, several of which only stop the new thing fighting the old thing" smell, and for a field treated as an independent source of truth when it is really a projection of another layer.
- Accepted because: The user flagged that the plan-review workflow had solidified usage-limit benching as brand-new machinery (`BenchUntil` on `EntryState` + separate persisted event + restoration scanner) when the resilience pause pipeline (`StatePaused` → bench/wait/decay, persisted via `agent_status.jsonl` and replayed by `GetState`) already did 90% of it. Reusing it via a new `StateBenched` state removed an unbench guard, a restoration scanner, and one implementation lap.
- Use when: a plan introduces "out of rotation"/"paused"/"deadline"/"retry-after"/"restore on restart" state; or enumerates plumbing steps where some exist only to keep the new mechanism from clobbering an existing one.
- Example: `rally-2 improve-error-categorisation` design Decision 5/6; see the "Reuse existing machinery before specifying new mechanisms" pattern above.
