## Why

Rover's behavior has accreted into two monolithic files in `package cmd`:

- `internal/cmd/actions.go` (~880 lines) holds every workflow — VM lifecycle, Ansible provisioning, Tailscale verification/repair, public-SSH fallback, command routing, device cleanup, and shell sanitizers — in one flat namespace.
- `internal/cmd/actions_test.go` (~1925 lines) mixes Azure doubles, Tailscale doubles, recovery scenarios, command-execution tests, and pure validation in a single file.

Two structural problems follow. First, the test seams are global mutable package vars (`tsFindPeer`, `tsGetAuthKey`, `tsConnect`, `tsPingPeer`, `runRemoteCommandFn`) and package-level timing knobs (`restoreConnectivityPollCount/Wait`, `connectReconnect*`). Tests mutate process-global state, so behavior cannot be exercised through an explicit, injected boundary and the doubles are not visible at the call site. Second, there is no separation between *deciding what should happen* (lifecycle/connectivity decisions) and *the Cobra plumbing that triggers it*; a reader or agent cannot navigate from a command down to a bounded unit of behavior — everything lives in one file.

This change normalizes Rover around small, deep modules with narrow interfaces, so the command layer reads top-to-bottom as thin adapters and each workflow is a self-contained service that is testable through injected providers. Shared state-mapping helpers move out of `cmd` so services can sync persisted connection state without depending on each other. It is a behavior-preserving refactor: no user-visible command, flag, output, or connectivity decision changes.

## What Changes

- **NEW** `internal/connectivity` package — the deepest module. Owns Tailscale readiness gating, remote re-auth via Azure Run Command, the public-SSH fallback decision, online-but-unpingable repair, and `rover command` / `rover connect` routing. Depends on injected `Tailscale`, `AzureControl`, and `CommandRunner` seams — no global function vars.
- **NEW** `internal/vm` package — VM lifecycle service (`up` / `down` / `restart` / `disk` / `status`), Azure power-state decisions, and teardown-time Tailscale device cleanup. Composes `connectivity` and `provision`.
- **NEW** `internal/provision` package — Ansible provisioning service: auth-key resolution, provision-over-Tailscale-or-public-IP selection, bounded SSH wait, and post-provision verify-and-lockdown.
- **NEW** `internal/stateutil` package — shared conversion/sync helpers for persisted `config.Connection` state, used by both `vm` and `provision` without either package importing the other.
- **NEW** `internal/shellsafe` package — pure `AuthKey` and `ShellArg` sanitizers, decoupled from `ui` (they return whether characters were stripped instead of printing warnings).
- **MODIFIED** `internal/tailscale` — add a small provider-side `Client` interface plus a default `CLI` implementation wrapping the existing package functions, so consumers depend on an interface instead of global vars.
- **MODIFIED** `internal/cmd` — Cobra files stay thin adapters; `appContext` composes the new services and wires the default providers. `actions.go` and `actions_test.go` are deleted; their behavior moves into the service packages and their tests are split per behavior and relocated next to the code they exercise.
- **REMOVED** global test seams (`tsFindPeer`, `tsGetAuthKey`, `tsConnect`, `tsPingPeer`, `runRemoteCommandFn`) and package-level timing vars, replaced by injected interfaces plus explicit `PollConfig` / reconnect policy config.

## Capabilities

### New Capabilities
- `connectivity`: Tailscale verification, remote repair, public-SSH fallback, and SSH command routing, behind an injected provider boundary.
- `vm-lifecycle`: Single-VM lifecycle workflows (up/down/restart/disk/status) as a composable service.
- `provisioning`: Ansible provisioning with Tailscale-aware host selection, bounded SSH wait, and post-provision lockdown.

These capabilities document behavior that already exists but was never specified. The deltas are written to be behavior-preserving — they capture the current contract so the refactor can be verified against it.

## Impact

- **Internal architecture**: One God file becomes focused service/helper packages plus a thin command layer. Each workflow is a deep module with a narrow `Service` surface; providers cross package boundaries through small interfaces.
- **Testability**: Global mutable seams are removed. Tests inject fakes at the constructor, so the doubles are visible at each call site and tests no longer mutate process-global state. Connectivity tests move into `internal/connectivity` and run without `package cmd`.
- **Navigability (progressive disclosure)**: A reader starts at a one-screen Cobra file, follows to a `Service` method, and finds bounded behavior — no file over the size budget in §D7 of `design.md`.
- **User-facing behavior**: None. All commands, flags, prompts, output strings, lockdown decisions, poll timings, and exit codes are preserved exactly.
- **Out of scope (deferred to `vm-tailscale-reliability`)**: `rover diagnose` / `rover status --health` and any change to Tailscale verification *semantics* (e.g. `tailscale ping` / SSH smoke tests before lockdown). This change only relocates and seams the existing logic.
