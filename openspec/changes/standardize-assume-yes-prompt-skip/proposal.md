# Proposal: Standardize --yes prompt-skip semantics

## Intent

Every confirming command defines `--yes` identically ("skip confirmation
prompts"), but two implementation patterns coexist. `Down` and
`tailscale cleanup` check `assumeYes` first and never prompt when it is set.
`Up` and `Disk` always call `ui.Confirm` and only consult `assumeYes`
afterward (`if !ok && !assumeYes`), which has two user-visible consequences in
an interactive terminal: `rover up --yes` still shows confirmation prompts,
and if the user answers **No**, the `--yes` flag silently overrides the
refusal and the operation proceeds. This batch converges all call sites on the
prompt-skip pattern that `Down` already uses, making the flag match its own
help text.

## Observed convention

The flag help is uniform across all four commands: `"skip confirmation
prompts"` (`internal/cmd/up.go:71`, `internal/cmd/down.go:33`,
`internal/cmd/disk.go:33`, `internal/cmd/tailscale.go:57`). The correct
implementation precedent exists in two places:

- `internal/vm/lifecycle.go:132-146` (`Down`): `ok := assumeYes; if !ok { ok, err = ui.Confirm(...) }`
- `internal/cmd/tailscale.go:38-43` (`cleanup`): `if !assumeYes { ... ui.Confirm ... }`

## Representative evidence

- `internal/vm/lifecycle.go:44-55` — `Up` fresh-create Tailscale warning:
  prompts even with `--yes`; a "No" answer is overridden by `!ok && !assumeYes`.
- `internal/vm/lifecycle.go:57-67` — `Up` start/redeploy confirmation: same
  prompt-then-override shape.
- `internal/vm/disk.go:39-49` — `Disk` resize confirmation: same shape.
- `internal/vm/lifecycle.go:132-146` — `Down` delete confirmation: the
  prompt-skip precedent to converge on.
- `internal/cmd/tailscale.go:38-43` — second exemplar of the precedent.
- `internal/ui/ui.go:20-23` — `Confirm` already auto-returns the default in
  non-interactive contexts, so the drift only manifests in terminals — which
  is why it survived CI and non-interactive smoke runs.

## Batch rule

At every confirmation guarded by an `assumeYes`/`--yes` parameter, short-circuit
before prompting: `ok := assumeYes; if !ok { ok, err = ui.Confirm(...) }`, then
treat `!ok` as abort. Never call `ui.Confirm` when `assumeYes` is true, and
never let `assumeYes` override an explicit interactive "No".

## Scope

In scope:
- `internal/vm/lifecycle.go` `Up` (both confirmations)
- `internal/vm/disk.go` `Disk` (resize confirmation)
- Matching test updates in `internal/vm/up_test.go` / `internal/vm/disk_test.go`
  (e.g. `TestUp_FreshCreateTailscaleNotReadyDeclinedConfirmAborts`), plus new
  cases asserting no prompt occurs when `assumeYes` is true

Out of scope:
- `ui.Confirm` itself and its non-interactive default behavior
- `Down` and `tailscale cleanup` (already correct)
- Confirmations without a `--yes` parameter (`internal/cmd/init.go:99`,
  `internal/cmd/doctor.go:45`, `internal/connectivity/route.go:54`)
- Prompt wording, defaults, or flag names

## Safety and validation

Small, mechanical, and confined to three confirmation sites; the target shape
already runs in production in `Down`. Non-interactive behavior is unchanged
(`Confirm` already returns the default without prompting there). The only
behavior deltas are the documented ones: `--yes` now truly skips prompts, and
an interactive "No" is now honored even when `--yes` was passed (strictly safer).
Verify with `go test ./internal/vm/...` and a manual `rover up --yes` /
`rover disk <gb> --yes` check that no prompt appears.

## Spec impact

Documented CLI behavior changes; see
`specs/vm-lifecycle/spec.md` (MODIFIED requirements for Up and Disk
confirmation semantics, relative to the `improve-architecture` vm-lifecycle
spec). Note: the `improve-architecture` change (complete, pending archive)
currently specs the drifted behavior for Up's decline scenario; land this batch
after that change is archived (see `reconcile-stale-openspec-changes`).
