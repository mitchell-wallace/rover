## 1. shellsafe (pure sanitizers)

- [ ] 1.1 Create `internal/shellsafe/shellsafe.go` — `AuthKey(key string) (clean string, stripped bool)` and `ShellArg(s string) string`, moving the rune-allowlist logic from `sanitizeAuthKey`, `isSafeAuthKeyChar`, and `sanitizeShellArg`. No `ui` import.
- [ ] 1.2 Create `internal/shellsafe/shellsafe_test.go` — move `TestSanitizeAuthKey`, `TestSanitizeAuthKey_StripsWithWarning` (assert on the `stripped` bool now), `TestIsSafeAuthKeyChar`, and `TestSanitizeShellArg`. Table-driven.
- [ ] 1.3 Update `actions.go` call sites to use `shellsafe.AuthKey`/`shellsafe.ShellArg`, emitting the existing `ui.Warn("Auth key contained unexpected characters ...")` when `stripped` is true (verbatim message). Confirm `go build ./...` and `go test ./...` stay green.

## 2. tailscale.Client seam

- [ ] 2.1 Create `internal/tailscale/client.go` — `Client` interface (`FindPeer`, `PingPeer`, `GetAuthKey`, `Connect`, `CleanupDevices`), a `CLI` struct implementing it by delegating to the existing package funcs, and `NewClient() Client`. Keep the package funcs as-is.
- [ ] 2.2 No behavior change; `go build ./...` and `go test ./...` stay green.

## 3. connectivity package (the deep module)

- [ ] 3.1 Create `internal/connectivity/service.go` — `Service` struct (`State`, `Azure AzureControl`, `TS tailscale.Client`, `Run CommandRunner`, `Poll PollConfig`); `AzureControl` interface (`Status`, `SetPublicSSH`, `RunCommand` — **not** `Info`; only provision uses `Info`); `CommandRunner` func type `func(name string, args ...string) error` (no ctx — matches `runRemoteCommand`) + `defaultCommandRunner` porting `runRemoteCommand` verbatim (own internal 10-min timeout, inherited stdio, `fmt.Errorf("%s: %w", name, err)` wrapping); `PollConfig` + `DefaultPoll{Count:12, Wait:5*time.Second}`; `New(...)` constructor; `Ready()` (port `tailscaleReady`, using the injected `TS`).
- [ ] 3.2 Create `internal/connectivity/repair.go` — `Reauthenticate(ctx)` (port `reauthenticateTailscale`, using `shellsafe`, `s.TS.GetAuthKey`, `s.Azure.RunCommand`, `s.Poll`) and `Restore(ctx)` (port `restoreConnectivity`). Preserve every `ui` string and the fallback ordering exactly.
- [ ] 3.3 Create `internal/connectivity/route.go` — `Connect(ctx, extra...)` (port `doConnect`) and `RunCommand(ctx, args)` (port `doCommand`, using `s.Run` for exec). Preserve Tailscale-first → repair → public-SSH fallback ordering and all messages.
- [ ] 3.4 Add per-package fakes (`fakeAzure`, `fakeTailscale` implementing `AzureControl` / `tailscale.Client`, recording calls) in a `testutil_test.go`.
- [ ] 3.5 Move and re-point connectivity tests: all `TestRestoreConnectivity_*` (including `_FullDownUpCycle` and `_ContextCancelled`) and reauth tests → `repair_test.go`; `TestTailscaleReady_*` → `ready_test.go`; `TestDoConnect_*` and `TestDoCommand_*` → `route_test.go`. Also add the `tailscale up` Run Command string assertion split out of `TestDoRestart_RestoresConnectivityWhenPublicSSHClosed` (see 5.5) here. Replace global-var stubs (`tsFindPeer`, `tsPingPeer`, `tsGetAuthKey`, `tsConnect`, `runRemoteCommandFn`) and poll-var overrides with injected fakes and a fast `PollConfig`. Keep each file ≤400 lines (split if needed). Note `TestDoConnect_TailscaleNotInstalled/NotRunning` assert only the underlying error strings (no extra `ui` guidance) — preserve that.
- [ ] 3.6 Move the "Real-backend verification notes" comment block from `actions_test.go` into `internal/connectivity/testdata/REALBACKEND.md` (or a `doc_test.go`) so the captured `az`/`tailscale` fixtures are preserved.
- [ ] 3.7 Remove the now-unused global vars/poll knobs from `actions.go`. `go test ./internal/connectivity/...` passes.

## 4. provision package

- [ ] 4.1 Create `internal/provision/service.go` — `Service` struct (`State`, `Azure AzureProvisioner`, `TS tailscale.Client`, `AssetDir`, `Ansible func(ansible.Params) error`, `Wait SSHWaiter`); `AzureProvisioner` interface (`Info`, `SetPublicSSH`, `RunCommand`); `SSHWaiter` seam; `Run(ctx)` (port `doProvision`). Default `Ansible = ansible.Provision`.
- [ ] 4.2 Create `internal/provision/wait.go` — default SSH wait (port `waitForSSH`: TCP dial loop, 5-min deadline, the "Waiting for SSH ..."/"SSH is up." messages) behind `SSHWaiter`.
- [ ] 4.3 Create `internal/provision/service_test.go` — fakes for Azure/Tailscale/Ansible/Wait; cover auth-key resolution (env > OAuth > none), provision-over-Tailscale vs public-IP selection, and post-provision verify-and-lockdown (close public SSH only when the peer verifies). Assert exact `ui` strings. The fake `Ansible` MUST record `os.Getenv("TS_AUTHKEY")` at call time; assert it equals the sanitized key during the run and is unset afterward (the key reaches Ansible via env, not `ansible.Params` — see D14).
- [ ] 4.4 Remove `doProvision`/`waitForSSH` from `actions.go`. `go test ./internal/provision/...` passes.

## 5. vm package

- [ ] 5.1 Create `internal/vm/service.go` — `Service` struct (`State`, `Azure AzureLifecycle`, `TS tailscale.Client`, `Conn connRestorer`, `Provision provisioner`); consumer-side seams `connRestorer` (`Ready() bool`, `Restore(ctx) error`) and `provisioner` (`Run(ctx) error`), satisfied by `*connectivity.Service`/`*provision.Service` (see D4 — concrete types here would force `vm` tests to wire real sub-services); `AzureLifecycle` interface (`Up`, `Down`, `Status`, `Restart`, `ResizeDisk`, `SSH`, `RunCommand` — **not** `Info`; only provision uses `Info`); `syncConnection`, `printInfo`, `scrubKnownHosts`.
- [ ] 5.2 Create `internal/vm/lifecycle.go` — `Up` (port `doUp`, using `Conn.Ready` for the gate, `Conn.Restore` for existing-VM restore, `Provision.Run` for auto-provision), `Down` (port `doDown`), `Restart` (port `doRestart`).
- [ ] 5.3 Create `internal/vm/disk.go` — `Disk` (port `doDisk`), `Status` (port `doStatus`), `SSH` (port `doSSH`).
- [ ] 5.4 Create `internal/vm/cleanup.go` — teardown Tailscale cleanup (`doTailscaleCleanup`, `printTailscaleCleanupResult`, `tailscaleLogoutScript`) using `s.TS.CleanupDevices`.
- [ ] 5.5 Create `internal/vm/*_test.go` — move `TestDoDown_*`, `TestDoDisk_*`, `TestSyncConnection_SavesState`, `TestInfoRunning`, and the Azure-only `TestDoRestart_NoVM/_VMNotRunning/_RestartsAndSyncsConnection`; re-point onto injected fakes (`fakeAzure`, recording `connRestorer`/`provisioner`). For `TestDoRestart_RestoresConnectivityWhenPublicSSHClosed`, the `vm` test asserts the recording `connRestorer.Restore` was invoked; the `tailscale up` Run Command string assertion moves to `connectivity/repair_test.go` (see 3.5). Do NOT move `TestRestoreConnectivity_FullDownUpCycle`/`_ContextCancelled` here — they are connectivity tests (see 3.5). Split into `lifecycle_test.go` / `disk_test.go` / `cleanup_test.go` to stay ≤400 lines.
- [ ] 5.6 Remove the lifecycle `do*` functions and helpers from `actions.go`. `go test ./internal/vm/...` passes.

## 6. cmd rewiring

- [ ] 6.1 Update `internal/cmd/root.go` — `appContext` holds `state`, `assetDir`, `vm *vm.Service`, `conn *connectivity.Service`. Remove the `azureProvider` interface. `loadContext` builds `azure.New(...)`, `tailscale.NewClient()`, then composes `connectivity.New`, `provision.Service`, and `vm.Service` (injecting defaults).
- [ ] 6.2 Update each Cobra file (`up.go`, `down.go`, `restart.go`, `disk.go`, `status.go`, `ssh.go`, `provision.go`, `command.go`, `connect.go`) to delegate to `a.vm.*` / `a.conn.*`, constructing a `context.Context` where the old `do*` did. No flag, arg, or message changes.
- [ ] 6.3 Update `internal/cmd/interactive.go` to call the same service methods (preserve CLI/interactive parity).
- [ ] 6.4 Delete `internal/cmd/actions.go`. `go build ./...` passes.

## 7. Test cleanup

- [ ] 7.1 Confirm every test in `actions_test.go` has an equivalent in a service package; then delete `internal/cmd/actions_test.go`.
- [ ] 7.2 Remove now-dead test helpers (`mockAzureClient`, `newTestAppContext`, `stubTSPing`) — they are superseded by per-package fakes.
- [ ] 7.3 Sanity-check test counts: `go test ./... -run . -count=1 -v | grep -c '^=== RUN'` is ≥ the pre-refactor count (no behavior coverage lost).

## 8. Verification

- [ ] 8.1 `go build ./...` and `go vet ./...` clean.
- [ ] 8.2 `go test ./...` green.
- [ ] 8.3 `golangci-lint run` clean.
- [ ] 8.4 File-size budget check: no `.go` source file > 300 lines and no `_test.go` file > 400 lines, except files carrying a top-of-file justification comment. (`find internal -name '*.go' | xargs wc -l | sort -n`)
- [ ] 8.5 Behavior diff spot-check against pre-refactor `git show HEAD:internal/cmd/actions.go`: prompts, default answers, poll count/wait, lockdown ordering, and fallback messages are byte-identical.
- [ ] 8.6 Live smoke (optional, per `azure-quota-gotcha` memory): `rover up`/`provision`/`connect`/`command`/`restart`/`down` paths behave as before on a real VM if quota allows.

## 9. Documentation

- [ ] 9.1 Add a short "Architecture" section to `README.md` (or `docs/architecture.md`): command layer = thin adapters; `vm`/`provision`/`connectivity` services; provider seams (`tailscale.Client`, per-package Azure interfaces); the file-size budget. Frame it as the navigation/progressive-disclosure guide for future agents.
- [ ] 9.2 Update `SPEC.md` if it references `internal/cmd/actions.go` or the old structure. (Current `SPEC.md` has no such reference — likely a no-op; confirm during impl.)
