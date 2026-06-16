## Context

Rover provisions and manages a single Azure VM so a user can SSH in and run Dune. The CLI is Cobra-based (`internal/cmd`). Each subcommand is a thin Cobra file that loads an `appContext` (loaded `config.State` + an `azureProvider` + materialized asset dir) and calls a `do*` function. All of those `do*` functions, plus their helpers, live in `internal/cmd/actions.go` (~750 lines); their tests live in `internal/cmd/actions_test.go` (~1780 lines).

The provider layer is already partly separated: `internal/azure` (script-backed `az`/Bicep wrapper), `internal/tailscale` (CLI + control-plane API), `internal/ansible` (playbook runner), `internal/config`, `internal/sizes`, `internal/ui`. The seams that are *missing* are between the workflows themselves and between those workflows and their external dependencies — today the latter are global function vars patched by tests.

This change introduces workflow boundaries. It does **not** change Azure scripting, Tailscale verification semantics, or any user-visible behavior.

## Goals / Non-Goals

**Goals:**
- Replace `internal/cmd/actions.go` with focused service packages: `internal/connectivity`, `internal/vm`, `internal/provision`, and a pure `internal/shellsafe`.
- Make every workflow testable through injected providers, removing all global function-var and poll-knob seams.
- Keep `package cmd` as thin adapters: parse → load context → call one service → format the top-level error.
- Enforce a soft file-size budget so no source or test file is a God file (see D7).
- Split `actions_test.go` by behavior and relocate each test next to the code it exercises.
- Preserve every user-facing behavior, output string, prompt, poll timing, and exit code exactly.

**Non-Goals:**
- `rover diagnose` / `rover status --health` (belongs to `vm-tailscale-reliability`).
- Changing Tailscale verification *semantics* — e.g. requiring `tailscale ping` + SSH smoke test before lockdown (also `vm-tailscale-reliability`).
- A generic multi-cloud provider abstraction, multi-VM orchestration, or background daemons.
- Rewriting the Azure shell scripts or giving `internal/azure` typed errors / an SDK migration.
- Converting `ui` side effects into structured return values (services keep printing through `ui`; see D9).

## Guiding Principles

When an implementing agent hits an unforeseen decision, apply these in order:

1. **Behavior is frozen.** Output strings, prompt text, default answers, poll counts/waits, lockdown decisions, fallback ordering, and exit codes are externally observable. Moving code must not alter them. When in doubt, diff the behavior against the pre-refactor `actions.go`.
2. **Deep modules, narrow interfaces.** A package should expose a small `Service` surface and hide substantial implementation behind it. If a new exported symbol is not needed by another package, it should be unexported.
3. **Inject, don't patch.** Cross-package dependencies are constructor parameters typed as interfaces. No `var fn = pkg.Fn` test seams; no package-level mutable knobs.
4. **No cycles.** The dependency direction is `cmd → vm → {provision, connectivity} → {azure, tailscale, ansible, shellsafe}`. `connectivity` and `provision` must not import `vm`; nothing in a service package imports `cmd`.
5. **Progressive disclosure.** A reader entering at a Cobra file should reach a bounded unit of behavior in at most two hops (Cobra file → `Service` method → helper). Keep files within the D7 budget.

## Architecture Overview

```
internal/
  cmd/                ← thin Cobra adapters; appContext composes the services
  vm/                 ← lifecycle service (up/down/restart/disk/status, teardown cleanup)
  provision/          ← Ansible provisioning service
  connectivity/       ← DEEP MODULE: Tailscale verify/repair/route + public-SSH fallback
  shellsafe/          ← pure AuthKey / ShellArg sanitizers
  azure/              ← unchanged (script-backed); concrete *Client satisfies consumer interfaces
  tailscale/          ← + Client interface and default CLI implementation
  ansible/ config/ sizes/ ui/   ← unchanged
```

Dependency graph (acyclic):

```
cmd ──▶ vm ──▶ provision ──▶ ansible, tailscale, shellsafe, azure, config, ui
   │     │
   │     └──▶ connectivity ──▶ tailscale, azure, shellsafe, config, ui
   └──────────▶ connectivity        (cmd also builds connectivity directly for `command`/`connect`)
```

## Decisions

### D1: Domain packages, not `internal/app/*` (resolves Open Question 1)

Services live as top-level domain packages (`internal/connectivity`, `internal/vm`, `internal/provision`), matching the existing flat convention (`internal/azure`, `internal/tailscale`, `internal/ansible`). An `internal/app/connectivity` nesting would add a layer that carries no meaning. The package name is the unit of progressive disclosure: "where does connectivity repair live?" → `internal/connectivity`.

### D2: Eliminate global seams via injected interfaces (resolves Open Question 1's testing concern)

The current seams — `tsFindPeer`, `tsGetAuthKey`, `tsConnect`, `tsPingPeer`, `runRemoteCommandFn`, and the `restoreConnectivityPollCount/Wait` vars — are removed. Each service struct carries its dependencies as interface-typed fields populated by its constructor. Tests construct a service with fakes; production code constructs it with default adapters. The doubles become visible at the call site instead of hiding in package state.

### D3: `connectivity` is the flagship deep module

Connectivity is where the real complexity lives and where it has the most callers (`up`, `restart`, `connect`, `command`). It owns:

- `Ready()` — local Tailscale readiness gate (was `tailscaleReady`).
- `Reauthenticate(ctx)` — generate/read an auth key, run `tailscale up` via Azure Run Command, poll until the peer is reachable (was `reauthenticateTailscale`).
- `Restore(ctx)` — if public SSH is locked down, prefer re-auth, otherwise open public SSH as fallback (was `restoreConnectivity`).
- `Connect(ctx, extra...)` — `rover connect`: peer lookup, offline/unpingable handling, online-but-unpingable repair, then `tailscale ssh` (was `doConnect`).
- `RunCommand(ctx, args)` — `rover command` routing: Tailscale-first, repair-when-locked-down, public-SSH fallback (was `doCommand`).

`vm` and `provision` consume `connectivity` rather than reimplementing any of it.

### D4: `vm` and `provision` are thinner orchestrators but still extracted

`vm` and `provision` methods mostly have a single Cobra caller, so a package boundary is not justified by "≥2 callers" alone. They are extracted anyway for two concrete reasons that satisfy the project's own bar: (a) file-size / separation — they are the bulk of `actions.go`; (b) they share `connectivity` as a dependency, and keeping them in `package cmd` would force `connectivity` to either live in `cmd` too (defeating the move) or be imported by `cmd` while the orchestration that uses it stays tangled with Cobra. Extracting them makes the composition explicit and keeps each file small.

`vm` depends on its sub-services through small **consumer-side interfaces** (`connRestorer`, `provisioner`; see Service Surfaces), not the concrete `*connectivity.Service`/`*provision.Service`. This is required by Principle 3: several `vm` lifecycle tests assert connectivity behavior (e.g. `TestDoRestart_RestoresConnectivityWhenPublicSSHClosed` asserts the `tailscale up` Run Command script). With concrete fields, those `vm` tests would have to build a real `connectivity.Service` wired with its own fake `tailscale.Client` + fake `AzureControl` + fast `PollConfig` — reintroducing exactly the "doubles not visible at the call site" problem this change removes. With interfaces, the `vm` test injects a recording fake and asserts "`Restore` was invoked"; the `tailscale up` string assertion moves to `connectivity/repair_test.go` where it belongs. The composition root (`cmd`) still wires the concrete services together.

### D5: `cmd` stays a thin adapter layer; `appContext` is the composition root

`appContext` keeps `state` and `assetDir` and gains constructed services. During the migration, `conn` is introduced as soon as `internal/connectivity` lands so the legacy `doConnect`/`doCommand` wrappers and restore helpers can delegate to it before the global Tailscale seams are deleted. After the full extraction, `loadContext` builds the default providers once and injects them:

```go
type appContext struct {
    state    *config.State
    assetDir string

    vm   *vm.Service
    conn *connectivity.Service   // also reachable as vm.Conn; held directly for command/connect
}
```

Each `do*` call in a Cobra file becomes a one-line delegation (e.g. `return a.vm.Up(ctx, family, size, assumeYes, noProvision)`). The interactive menu (`interactive.go`) delegates to the same service methods, preserving CLI/interactive parity.

### D6: Behavior-preserving only — defers Open Questions 2 and 3

- **Open Question 2 (typed Azure errors / SDK):** No. `internal/azure` stays script-backed with its current error surface. Typed errors are deferred until there is a concrete consumer that branches on error kind — none is introduced here.
- **Open Question 3 (`rover diagnose`):** No. Diagnostics is a *new behavior* and belongs in `vm-tailscale-reliability`, which already proposes `rover diagnose` / `rover status --health`. Including it here would mix a refactor with a feature and make the "no behavior change" guarantee unverifiable. The existing `rover doctor` (`internal/cmd/doctor.go`) is left in place, unchanged.

### D7: File-size budget (resolves Open Question 4)

Soft budgets, enforced by review (and optionally a lint rule):

- Any `.go` source file: **≤ 300 lines** (target ~200).
- Any `_test.go` file: **≤ 400 lines**.
- A `Service` type's primary file: aim **≤ 250 lines**; split by sub-behavior (e.g. `repair.go`, `route.go`) before exceeding it.

"Soft" means a file may exceed the budget when splitting would harm cohesion, but doing so requires a one-line comment at the top of the file justifying it. The point is to make a 750-line file impossible to add to by reflex.

### D8: `Tailscale` provider seam (provider-side interface)

`internal/tailscale` gains a `Client` interface and a default `CLI` implementation that delegates to the existing package functions (which stay, so churn is minimal):

```go
// internal/tailscale/client.go
type Client interface {
    FindPeer(host string) (*Peer, error)
    PingPeer(p *Peer) bool
    GetAuthKey(clientID, secret string, tags []string) (string, error)
    Connect(user, host string, extra ...string) error
    CleanupDevices(clientID, secret string, tags []string, hostname string, deleteOnline, dryRun bool) (CleanupResult, error)
}

type CLI struct{}
func NewClient() Client { return CLI{} }
// methods delegate to FindPeer, PingPeer, ... package funcs
```

A provider-side interface (rather than one consumer-side interface per package) is chosen here because three packages need nearly the same set; duplicating the declaration three times would be noise. The default adapter must live in `package tailscale` (not `connectivity`) to avoid an import cycle: `connectivity` imports `tailscale` for the `Peer`/`CleanupResult` types, so `tailscale` cannot import `connectivity`.

### D9: Azure provider seams are consumer-side, satisfied by `*azure.Client`

`internal/azure` is a leaf, so each consuming package declares the minimal Azure interface it needs and `*azure.Client` satisfies all of them by structural typing. This keeps each package's Azure dependency self-documenting and avoids one wide interface that lies about what a package uses.

```go
// connectivity needs (Status for command preconditions, SetPublicSSH/RunCommand for
// fallback + remote re-auth). It does NOT need Info() — only doProvision uses Info().
type AzureControl interface {
    Status() (azure.Info, error)
    SetPublicSSH(allowed bool) error
    RunCommand(script string) error
}
// vm needs the lifecycle subset: Up, Down, Status, Restart, ResizeDisk, SSH, RunCommand
//   (NOT Info — only provision uses Info)
// provision needs: Info, SetPublicSSH
```

The existing `azureProvider` interface in `cmd/root.go` is removed; the per-package interfaces replace it.

### D10: `CommandRunner` replaces the `runRemoteCommandFn` global

Remote command execution (the `ssh`/`tailscale ssh` exec in `doCommand`, and `runRemoteCommand`) becomes a func-typed seam on `connectivity.Service`:

```go
// CommandRunner runs an external command with the user's stdio attached.
type CommandRunner func(name string, args ...string) error
```

The signature matches `runRemoteCommand` verbatim (no `ctx` parameter): `defaultCommandRunner` wraps `exec.CommandContext` exactly as `runRemoteCommand` does today — it creates its **own** 10-minute timeout context internally, inherits stdio, and preserves the `fmt.Errorf("%s: %w", name, err)` error wrapping (the connectivity command-failure test asserts the wrapped message). Threading an injected `ctx` into this exec seam (for caller-driven cancellation) is deliberately out of scope here and deferred to `vm-tailscale-reliability`. Tests inject a fake matching the same 2-arg shape that records `name` and `args`. This removes the `runRemoteCommandFn` package var (today a nil-checked optional override at `actions.go:675-678`).

### D11: `shellsafe` is pure and UI-free

`sanitizeAuthKey`, `isSafeAuthKeyChar`, and `sanitizeShellArg` move to `internal/shellsafe` as:

```go
func AuthKey(key string) (clean string, stripped bool)
func ShellArg(s string) string
```

`AuthKey` returns whether characters were stripped instead of calling `ui.Warn`, so the package has no UI dependency and is trivially table-testable. Callers (`connectivity`, `provision`) emit the existing warning when `stripped` is true, preserving the current message verbatim.

### D12: `PollConfig` replaces package-level poll knobs

`restoreConnectivityPollCount` (12) and `restoreConnectivityPollWait` (5s) become a `connectivity.PollConfig{ Count int; Wait time.Duration }` field on the service. Production uses `DefaultPoll = PollConfig{Count: 12, Wait: 5 * time.Second}`; tests pass a fast config (e.g. `Count: 2, Wait: time.Millisecond`) to keep them quick, exactly as the current tests do by overriding the vars.

### D13: Teardown-time Tailscale cleanup lives in `vm`

`doTailscaleCleanup`, `printTailscaleCleanupResult`, and `tailscaleLogoutScript` are part of VM teardown and the existing `rover tailscale cleanup` maintenance command. They move into `internal/vm` (the teardown owner) and use the `tailscale.Client` seam's `CleanupDevices`. `connectivity` does not own control-plane device cleanup — that is a lifecycle concern, not a reachability concern. `vm.Service` exposes a narrow cleanup method for the maintenance command:

```go
func (s *Service) CleanupTailscaleDevices(deleteOnline, dryRun bool) (tailscale.CleanupResult, error)
```

`internal/cmd/tailscale.go` delegates to that method after preserving its existing confirmation prompts.

### D14: Provisioning preserves the `TS_AUTHKEY` env round-trip

`doProvision` resolves and sanitizes the auth key, then does `os.Setenv("TS_AUTHKEY", authKey)` with a deferred `os.Unsetenv` (`actions.go:548-553`), because `ansible.Provision` reads `TS_AUTHKEY` from the process environment — the key is *not* passed through `ansible.Params`. This is load-bearing and easy to drop accidentally: a test that injects a fake `Ansible` and only asserts on `ansible.Params` will stay green even if the `Setenv` is removed, silently breaking real provisioning. The provision service MUST preserve this env round-trip, and `service_test.go` MUST assert it — the fake `Ansible` records `os.Getenv("TS_AUTHKEY")` at call time and the test checks it equals the sanitized key (and is unset afterward).

### D15: State sync helpers are shared outside `cmd`

`syncConnection` is currently a method on `appContext`, but both VM lifecycle and provisioning use it (`doProvision` marks Ansible applied, then syncs the latest Azure info). Service packages cannot import `cmd`, and putting the helper only in `vm` would force `provision` to depend on `vm` or duplicate state-mapping logic. Introduce `internal/stateutil` with:

```go
func ConnectionFromAzure(info azure.Info) config.Connection
func ZeroConnection() config.Connection
func SyncConnection(st *config.State, info azure.Info) error
```

`internal/cmd/conv.go` is removed. `vm` and `provision` both use `stateutil.SyncConnection`; `vm.Down(delete=true)` uses `stateutil.ZeroConnection`. This keeps the dependency graph acyclic: service packages depend on `stateutil`, while `stateutil` depends only on `azure` and `config`.

## Service Surfaces

```go
// internal/connectivity
type Service struct {
    State *config.State
    Azure AzureControl
    TS    tailscale.Client
    Run   CommandRunner
    Poll  PollConfig
}
func New(st *config.State, az AzureControl, ts tailscale.Client) *Service // Run=default, Poll=DefaultPoll
func (s *Service) Ready() bool
func (s *Service) Reauthenticate(ctx context.Context) bool
func (s *Service) Restore(ctx context.Context) error
func (s *Service) Connect(ctx context.Context, extra ...string) error
func (s *Service) RunCommand(ctx context.Context, args []string) error

// internal/provision
type Service struct {
    State    *config.State
    Azure    AzureProvisioner
    TS       tailscale.Client
    AssetDir string
    Ansible  func(ansible.Params) error // default: ansible.Provision; injectable for tests
    Wait     SSHWaiter                  // default: TCP dial loop; injectable for tests
}
type SSHWaiter func(ctx context.Context, host string, port int)
func (s *Service) Run(ctx context.Context) error  // was doProvision

// internal/vm
type Service struct {
    State     *config.State
    Azure     AzureLifecycle
    TS        tailscale.Client
    Conn      connRestorer  // satisfied by *connectivity.Service
    Provision provisioner   // satisfied by *provision.Service
}

// Consumer-side seams so vm lifecycle tests inject recording fakes instead of
// constructing real sub-services with nested fakes (see D4).
type connRestorer interface {
    Ready() bool
    Restore(ctx context.Context) error
}
type provisioner interface {
    Run(ctx context.Context) error
}
func (s *Service) Up(ctx context.Context, family, size string, assumeYes, noProvision bool) error
func (s *Service) Down(ctx context.Context, del, assumeYes bool) error
func (s *Service) Restart(ctx context.Context) error
func (s *Service) Disk(gb int, assumeYes bool) error
func (s *Service) Status() error
func (s *Service) SSH(extra ...string) error
func (s *Service) CleanupTailscaleDevices(deleteOnline, dryRun bool) (tailscale.CleanupResult, error)
```

`SSHWaiter` deliberately returns no error. The default preserves current `waitForSSH` behavior: it waits up to 5 minutes, returns early on context cancellation, and if SSH never opens it silently returns so Ansible produces the user-visible connection failure exactly as it does today.

## Package File Layout (after change)

```
internal/connectivity/
  service.go         ← Service, New, PollConfig, DefaultPoll, CommandRunner, defaultCommandRunner,
                       AzureControl interface, Ready
  repair.go          ← Reauthenticate, Restore
  route.go           ← Connect, RunCommand
  *_test.go          ← repair_test.go, route_test.go, ready_test.go (each ≤400 lines)

internal/provision/
  service.go         ← Service, AzureProvisioner, SSHWaiter, Run
  wait.go            ← default SSH wait (TCP dial loop)
  service_test.go

internal/vm/
  service.go         ← Service, AzureLifecycle, printInfo, scrubKnownHosts
  lifecycle.go       ← Up, Down, Restart
  disk.go            ← Disk, Status, SSH
  cleanup.go         ← Tailscale device cleanup + logout script (teardown)
  lifecycle_test.go, disk_test.go, cleanup_test.go

internal/stateutil/
  stateutil.go       ← ConnectionFromAzure, ZeroConnection, SyncConnection
  stateutil_test.go

internal/shellsafe/
  shellsafe.go       ← AuthKey, ShellArg
  shellsafe_test.go

internal/tailscale/
  client.go          ← Client interface, CLI default impl, NewClient   (new file)
  tailscale.go       ← unchanged package funcs (CLI delegates to these)

internal/cmd/
  root.go            ← appContext with vm + conn; loadContext wires defaults
  up.go down.go restart.go disk.go status.go ssh.go command.go connect.go provision.go tailscale.go
                     ← unchanged thin Cobra files, now delegating to a.vm.* / a.conn.*
  interactive.go     ← delegates to the same service methods
  (actions.go, actions_test.go, and conv.go DELETED)
```

## Test Migration Map

| Current test (in `actions_test.go`) | Moves to |
| --- | --- |
| `TestRestoreConnectivity_*` (incl. `_FullDownUpCycle` and `_ContextCancelled`), `TestReauthenticate*` | `connectivity/repair_test.go` |
| `TestTailscaleReady_*` | `connectivity/ready_test.go` |
| `TestDoConnect_*`, `TestDoCommand_*` | `connectivity/route_test.go` |
| `TestSanitizeAuthKey*`, `TestIsSafeAuthKeyChar`, `TestSanitizeShellArg` | `shellsafe/shellsafe_test.go` |
| `TestSyncConnection*` plus `configConnFrom`/`stateZeroConn` coverage | `stateutil/stateutil_test.go` |
| `TestInfoRunning` | `azure/azure_test.go` |
| `TestDoDown_*`, `TestDoDisk_*` | `vm/*_test.go` |
| `TestDoRestart_NoVM`, `TestDoRestart_VMNotRunning`, `TestDoRestart_RestartsAndSyncsConnection` | `vm/lifecycle_test.go` (Azure-only assertions) |
| `TestDoRestart_RestoresConnectivityWhenPublicSSHClosed` | **split**: `vm/lifecycle_test.go` asserts `Conn.Restore` was invoked (recording fake `connRestorer`); the `tailscale up` Run Command string assertion moves to `connectivity/repair_test.go` |
| `mockAzureClient`, `newTestAppContext`, `stubTSPing` | replaced by per-package fakes (`fakeAzure`, `fakeTailscale`, recording `connRestorer`/`provisioner`) + small constructors |

Note: `TestRestoreConnectivity_FullDownUpCycle` and `TestRestoreConnectivity_ContextCancelled` are connectivity tests (they exercise `restoreConnectivity`/reauth and the poll-cancellation path) — they belong in `connectivity/repair_test.go`, not `vm`, despite touching the down/up cycle.

The "Real-backend verification notes" comment block at the top of `actions_test.go` (captured `az`/`tailscale` responses, last verified 2026-06-10) is preserved — move it to a `doc_test.go` or a `testdata/REALBACKEND.md` in the package whose fakes it documents (`connectivity`), so the live-Azure fixtures are not lost.

## Migration Sequence (keep tests green at each stage)

1. `shellsafe` (pure, no deps) — extract + test; update `actions.go` to call it.
2. `tailscale.Client` interface + `CLI` — add; no behavior change.
3. `connectivity` — extract using the seams from 1–2; introduce `appContext.conn`/default `tailscale.Client`; update legacy connectivity entry points (`tailscaleReady`, `restoreConnectivity`, `reauthenticateTailscale`, `doConnect`, `doCommand`) to delegate to `a.conn` so existing VM/provision wrappers keep compiling; move connectivity tests in; then delete global vars/poll knobs once no legacy code references them.
4. `stateutil` — move connection-state conversion/sync helpers out of `cmd` before both `vm` and `provision` need them.
5. `provision` — extract; move provisioning behavior; inject `Ansible`/`Wait`; introduce `appContext.provision` before any legacy wrapper delegates to it.
6. `vm` — extract lifecycle + teardown cleanup; compose `connectivity` + `provision`; expose cleanup for `rover tailscale cleanup`.
7. `cmd` — rewire `appContext`/`loadContext`/`interactive.go`/`tailscale.go`; delete `actions.go` and `conv.go`.
8. Delete `actions_test.go` once every test has a new home; verify counts.
9. Verification: `go build ./...`, `go test ./...`, `golangci-lint run`, behavior diff spot-check.

## Risks / Mitigations

- **Behavior drift during the move.** Mitigation: move code verbatim first (same strings, same ordering), then seam it; rely on the relocated tests (which assert exact strings and call ordering) as the safety net. Do not "improve" messages in this change.
- **Import cycle between `connectivity` and `tailscale`.** Mitigation: D8 — the default adapter lives in `package tailscale`.
- **`ui` coupling inside services.** Accepted (D9, non-goal). Services print through `ui` exactly as today; converting to structured results is future work.
- **Scope creep into diagnostics / verification semantics.** Mitigation: D6 hard line — those belong to `vm-tailscale-reliability`.
