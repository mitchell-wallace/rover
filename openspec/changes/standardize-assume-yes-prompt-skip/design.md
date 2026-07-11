# Design: Standardize --yes prompt-skip semantics

## Current pattern

Two coexisting shapes for `--yes`-guarded confirmations:

Shape A (target, used by `Down` at `internal/vm/lifecycle.go:132-146` and
`tailscale cleanup` at `internal/cmd/tailscale.go:38-43`):

```go
ok := assumeYes
if !ok {
    ok, err = ui.Confirm(title, desc, def)
    if err != nil { return err }
}
if !ok { return fmt.Errorf("aborted...") }
```

Shape B (drifted, used by `Up` at `internal/vm/lifecycle.go:44-55` and `:57-67`,
and `Disk` at `internal/vm/disk.go:39-49`):

```go
ok, err := ui.Confirm(title, desc, def)
if err != nil { return err }
if !ok && !assumeYes { return fmt.Errorf("aborted...") }
```

## Target convention

Shape A everywhere a `--yes` flag exists: skip the prompt entirely when
`assumeYes` is true; an interactive "No" always aborts.

## Transformation rules

- Rewrite each Shape B site to Shape A, preserving the exact `ui.Confirm`
  title/description/default strings and the exact abort error message.
- Do not touch confirmations that have no `assumeYes` parameter.
- Update tests that relied on Shape B ordering; add one assertion per command
  that `assumeYes=true` produces no prompt (the vm tests already run
  non-interactively, so assert on outcome: the operation proceeds and the
  abort error does not occur).

## Files and exclusions

- `internal/vm/lifecycle.go` (Up only — leave Down untouched)
- `internal/vm/disk.go`
- `internal/vm/up_test.go`, `internal/vm/disk_test.go`
- Excluded: `internal/ui/ui.go`, `internal/cmd/*` flag definitions,
  `internal/cmd/init.go`, `internal/cmd/doctor.go`,
  `internal/connectivity/route.go` (no `--yes` at those sites).

## Verification strategy

- `go test ./internal/vm/...` and `go test ./...` green.
- `just build`, then in a terminal: `./bin/rover up --yes` against no config /
  fake state shows no confirmation prompt before failing on Azure calls;
  same for `./bin/rover disk 64 --yes`.
- Diff review: prompt strings and abort messages byte-identical; only control
  flow around `ui.Confirm` changed.

## Stop conditions

- If any existing test encodes "--yes overrides an interactive No" as desired
  behavior (rather than incidental), stop and flag it for review instead of
  rewriting the test.
- If the `improve-architecture` change has not been archived and its
  vm-lifecycle delta spec still specs the old decline scenario, coordinate
  ordering (archive first) rather than leaving two conflicting deltas active.
