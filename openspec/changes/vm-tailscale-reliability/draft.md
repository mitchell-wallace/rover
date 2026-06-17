# VM and Tailscale Reliability Draft

## Goal

Improve Rover's ability to detect, repair, and explain Azure VM and Tailscale
connectivity failures without asking users to guess whether Azure, SSH,
tailscaled, MagicDNS, or Tailscale SSH is the failing layer.

## Current Behavior

- `rover up` and `rover restart` can restore Tailscale when public SSH is locked
  down by using Azure Run Command to execute `tailscale up` inside the VM.
- `rover command` prefers Tailscale and can repair a closed-public-SSH VM before
  falling back to public SSH.
- `rover connect` checks the local tailnet peer and data-plane reachability.
  When the peer reports online but `tailscale ping` fails, it now attempts
  remote Tailscale re-auth before returning an error.

## Suggested Code Changes

### Tailscale Verification

- Treat `Peer.Online` as a weak control-plane signal only.
- Before closing public SSH, require:
  - peer found in `tailscale status --json`
  - peer reports online
  - `tailscale ping` succeeds
  - Tailscale SSH smoke test succeeds, e.g. `tailscale ssh user@target -- true`
- Replace one-shot verification after provisioning with a bounded poll loop that
  records the latest state: `not_found`, `offline`, `online_unpingable`,
  `tailscale_ssh_failed`, or `ready`.

### Remote Repair

- Keep remote repair centered on Azure Run Command because it works when public
  SSH is closed.
- Reuse one helper for all remote Tailscale re-auth flows:
  - generate or read an auth key
  - run `sudo tailscale up --authkey=... --ssh --hostname=... --advertise-tags=...`
  - poll local Tailscale until data-plane reachability is proven
- Use that helper in:
  - `rover connect`
  - `rover command`
  - `rover restart`
  - `rover up` after starting an existing locked-down VM
- Keep public SSH fallback separate from remote repair so commands can choose
  whether opening public SSH is appropriate.

### Status and Diagnostics

- Add `rover status --health` or `rover diagnose`.
- Include local checks:
  - Azure VM existence and power state
  - public SSH NSG state
  - local Tailscale CLI availability and backend state
  - Rover peer match result
  - Tailscale data-plane ping result
  - Tailscale SSH smoke-test result
- Include guest checks via Azure Run Command when requested:
  - cloud-init status
  - Azure Linux Agent status
  - `tailscaled` systemd status
  - `tailscale status --json` backend state
  - Tailscale IP presence
  - SSH service/socket state and configured port listener
  - disk pressure and basic memory pressure

## Suggested Telemetry

Telemetry should be opt-in or clearly documented, and must avoid secrets,
resource names, IPs, FQDNs, usernames, auth keys, command strings, and raw logs.

### Events

- `command.started`
- `command.finished`
- `azure.operation.finished`
- `tailscale.peer_check.finished`
- `tailscale.reauth.started`
- `tailscale.reauth.finished`
- `tailscale.lockdown.finished`
- `connect.route_selected`
- `diagnose.finished`

### Tags and Fields

- Rover version
- OS and architecture
- command name
- Azure region
- VM family and t-shirt size
- VM power-state category
- public SSH closed boolean
- Tailscale local backend state category
- peer state category
- repair attempted boolean
- repair result category
- fallback route category
- duration buckets
- sanitized error category and exit code

### Error Categories

- `azure_cli_missing`
- `azure_not_logged_in`
- `azure_run_command_failed`
- `azure_vm_not_running`
- `tailscale_cli_missing`
- `tailscale_local_not_running`
- `tailscale_peer_not_found`
- `tailscale_peer_offline`
- `tailscale_peer_unpingable`
- `tailscale_ssh_failed`
- `tailscale_authkey_generation_failed`
- `ansible_failed`
- `ssh_port_unreachable`

## Open Questions

- Should `rover connect` ever open public SSH automatically, or should it only
  attempt Tailscale repair and leave public SSH fallback to `rover restart` or a
  future explicit command?
- Should Rover add an explicit `rover repair tailscale` command for users who
  want to trigger Azure Run Command re-auth directly?
- Should guest health checks be on-demand only, or should provisioning install a
  small local `rover-health --json` command for diagnostics?
- What is the right timeout budget for Tailscale repair after deallocation,
  restart, and resize?

## Incident & Fix Log

### 2026-06-17 — Run Command conflict + wedged tailscaled (`rover connect` stuck)

**Symptom.** `rover connect` reported the peer online-but-unreachable, attempted
remote re-auth via Azure Run Command, and failed with:

    (Conflict) Run command extension execution is in progress. Please wait for
    completion before invoking a run command.

…then waited through a blind 60s poll and gave up. Re-running `rover connect`
hit the same conflict repeatedly.

**Root causes (three compounding).**

1. **Server-side LRO outlives the local `az` process.** `azure.RunCommand`
   wrapped `az vm run-command invoke` in a 10-minute `context.WithTimeout`, but
   cancelling the local `az` does NOT stop the Azure-side Run Command extension.
   An earlier repair invoke (activity-log correlation `53557815…`) ran for
   ~1h30m server-side (started 02:48:30Z, Failed 04:18:42Z). Every subsequent
   invoke during that window was rejected with HTTP 409 Conflict — the exact
   message above — and the old code merely `ui.Warn`-ed it and proceeded to the
   pointless 60s poll.
2. **Wedged tailscaled inside the VM.** The live Tailscale node
   (`100.118.177.35`, tagged) was control-plane Online but data-plane dead:
   `tx 158KB / rx 0`, no handshake, DERP `syd` only, `tailscale ping` timed out.
   `tailscale up` (and even `tailscale status`) block on a wedged daemon, which
   is why the repair invoke hung for ~90 minutes — the script had no internal
   bound, and Azure's script ceiling is ~90 minutes.
3. **Swallowed failures + no conflict handling.** The repair script ended with
   `… 2>&1 || true`, so a failing `tailscale up` reported success and its error
   text was discarded. There was no detection of, retry on, or bounded backoff
   for the 409 Conflict.

**Latent finding.** A stale duplicate Tailscale node (`100.88.25.46`,
personally-owned, offline 12d) owned the hostname `rover-vm`, so the live tagged
node was auto-suffixed to `rover-vm-1` — a source of peer-matching ambiguity and
the "ghost" pattern that ephemeral-key re-auth can create over time.

**Recovery (live).** No local "stuck process" exists to kill — the stuck work is
the Azure-side extension. A `az vm restart` (queued behind the extension, then
processed) restarted `tailscaled`, which cleared the wedge; the data plane
returned (`pong … via 4.196.123.16:41641 in 25ms`, direct, not relayed). The
stale ghost node was deleted via the Tailscale device API.

**Landed fix (v0.5.5).** Behavior-additive; touches only `internal/azure` and
`internal/cmd` (no architecture refactor — that stays in `improve-architecture`).

- `azure.RunCommand` now classifies failures (`KindConflictBusy |
  KindTransient | KindGuestScriptFailed | KindFatal`) and retries with bounded
  exponential backoff (4 attempts, 10/20/40/60s, per-attempt 5min timeout,
  caller-context-bounded). Captures az stdout/stderr for diagnostics. Only
  `KindGuestScriptFailed` short-circuits; every other kind (including ambiguous
  contention errors that don't match the clean 409, and throttles) is retried,
  because a spurious give-up is worse than a bounded retry during the exact
  contention this layer exists to ride through.
- `buildReauthScript` bounds every guest call with `timeout(1)`
  (`systemctl restart tailscaled` then `tailscale up`) so a wedged daemon can
  no longer pin the extension for ~90 minutes; drops `|| true` so real
  `tailscale up` failures propagate; restarts `tailscaled` first (reloads
  existing node creds, unwedges the daemon, and deliberately does NOT use
  `--force-reauth`, which combined with ephemeral keys mints duplicate nodes).
- `rover connect` now offers an in-process "Restart the VM to repair
  Tailscale?" prompt when re-auth is exhausted, then reconnects automatically
  (the documented escape hatch, automated).
- Classified errors surface the captured guest output so users see the real
  cause (invalid auth key, unauthorized tag, tailscaled down) instead of a bare
  exit code.

**Verified live** against the real VM: happy-path RunCommand ~33s; under
deliberate contention (occupying extension) the new code classifies and retries
through the conflict and recovers in ~1m45s; guest-script failures are surfaced
without retry. Classifier + retry are unit-tested with the exact production 409
string as a fixture.

**Remaining scope here (not addressed by v0.5.5).** Stronger Tailscale
verification (peer + online + ping + Tailscale-SSH smoke test, bounded poll with
explicit states), `rover diagnose`, telemetry/error categories, and deciding
whether `rover connect` should ever auto-open public SSH. The Remote Repair
helper unification is now partially realized (`buildReauthScript` is shared by
connect/restart/up/command via `reauthenticateTailscale`/`restoreConnectivity`).
