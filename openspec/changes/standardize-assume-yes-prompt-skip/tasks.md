# Tasks

## 1. Confirm the pattern

- [ ] 1.1 Inspect the exemplars (`internal/vm/lifecycle.go:132-146`,
      `internal/cmd/tailscale.go:38-43`) and the three drifted sites
      (`internal/vm/lifecycle.go:44-55`, `internal/vm/lifecycle.go:57-67`,
      `internal/vm/disk.go:39-49`).
- [ ] 1.2 Confirm `reconcile-stale-openspec-changes` (archive of
      `improve-architecture`) has landed so the vm-lifecycle spec baseline
      exists for the MODIFIED delta.

## 2. Pilot the transformation

- [ ] 2.1 Apply the Shape A rewrite to `internal/vm/disk.go` only, preserving
      prompt strings and the "aborted" error verbatim.
- [ ] 2.2 Run `go test ./internal/vm/...`.
- [ ] 2.3 Stop and report if any test asserts that a declined prompt plus
      `assumeYes` proceeds (see design stop conditions).

## 3. Apply across the scoped set

- [ ] 3.1 Apply the same rewrite to both `Up` confirmations in
      `internal/vm/lifecycle.go`.
- [ ] 3.2 Extend `internal/vm/up_test.go` / `internal/vm/disk_test.go`:
      with `assumeYes=true`, the flow proceeds without consulting the prompt;
      an interactive decline (simulated via the existing test seams) aborts
      even when combined with `assumeYes=false` as today.
- [ ] 3.3 Keep `Down`, `tailscale cleanup`, and non-`--yes` confirmations out
      of the diff.

## 4. Verify

- [ ] 4.1 Run `go test ./...` and `just lint`; expect green.
- [ ] 4.2 Manual terminal check: `rover up --yes` and `rover disk 64 --yes`
      show no confirmation prompt.
- [ ] 4.3 Review the diff against `specs/vm-lifecycle/spec.md` in this change;
      behavior matches the two modified requirements exactly.
