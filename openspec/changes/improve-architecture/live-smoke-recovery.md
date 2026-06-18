# Live Smoke Recovery Note

Date: 2026-06-18 UTC
Lap: work-6de6
Recovery classification: needs_user

## Scope

This lap attempted to recover the runtime prerequisites for improve-architecture
task 9.6, the optional real Azure/Tailscale smoke:

- `rover up`
- `rover provision`
- `rover connect`
- `rover command`
- `rover restart`
- `rover down`

## Findings

- `az account show --output json` fails with `ERROR: Please run 'az login' to setup account.`
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- No non-interactive Azure credential environment variables were present by name
  (`AZURE_*`, `ARM_*`, managed-identity variables, subscription/tenant/client
  variables).
- `~/.azure` contains only CLI config/log/cache metadata, not usable account or
  token cache files.
- `~/.config/rover/state.json` is absent, so Rover loads defaults.
- `tailscale status` initially fails because local `tailscaled` is not running.
- A userspace `tailscaled` can start on the default socket in this container, but
  with no persisted state it reports `BackendState: NeedsLogin` / `Logged out`.
- No Tailscale credential environment variables were present by name
  (`TS_*`, `TAILSCALE_*`), and no persisted Tailscale state was found under the
  normal user-state paths or `/persist`.

## Command Results

- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`: pass.
- `go build ./...`: pass.
- `go run ./cmd/rover up --no-provision -y`: fails at Azure login.
- `go run ./cmd/rover provision`: fails at Azure login.
- `go run ./cmd/rover command true`: fails at Azure login.
- `go run ./cmd/rover restart`: fails at Azure login.
- `go run ./cmd/rover down -y`: fails at Azure login.
- `go run ./cmd/rover connect`: fails with `local Tailscale is not connected; run 'tailscale up'`.

## Missing Prerequisites

The full task 9.6 smoke cannot run in this container without user-provided
runtime credentials:

1. A non-interactive Azure login with a selected subscription and permission to
   create/manage the Rover resource group and VM. MSI is not usable here because
   the metadata token request returns 403.
2. An authenticated local Tailscale session, or non-interactive Tailscale auth
   material sufficient to create one (`TS_AUTHKEY` or Tailscale OAuth client ID
   and secret with the required tag grants).

Rover state can remain absent if the next run intentionally uses defaults, but a
state file or environment variables are needed if the smoke should target a
specific subscription/resource group/region/VM name/Tailscale configuration.

## Re-confirmation (lap work-f1e5, 2026-06-18 UTC)

Re-ran the full 9.6 smoke probe in this container; runtime is unchanged from the
original findings, so the smoke still cannot execute:

- `az account show` → `Please run 'az login'`; `az login --identity` → MSI 403;
  no `AZURE_*`/`ARM_*`/SDK auth-chain env; no `/etc/azure`, `/run/secrets`, or
  token cache under `~/.azure`.
- `tailscale status` → local tailscaled not running; no `TS_*`/`TAILSCALE_*`
  env; no persisted state under `/var/lib/tailscale`, `~/.config/tailscale`,
  `~/.local/share/tailscale`, or `/persist`.
- `go build ./...` clean.
- `rover up --no-provision -y` / `rover down -y` → `not logged in to Azure`.
- `rover connect` → `rover: tailscale status: exit status 1` (local TS gate).

Classification remains `needs_user`. Task 9.6 stays unchecked per the "only if it
passes" contract. To unblock, a non-interactive Azure login (SP: tenant/client
ID + secret, or federated token) with a selected subscription and a Tailscale
auth source (`TS_AUTHKEY` or OAuth client + secret) must be injected into the
container environment.

## Re-confirmation (lap work-f659, 2026-06-18 UTC)

Re-ran the 9.6 prerequisite and smoke checks in the current container. The
runtime is still not capable of executing the live smoke, and this attempt is
blocked earlier than the previous recovery notes:

- No credential environment variable names were present for Azure or Tailscale:
  no `AZURE_*`, `ARM_*`, `TS_*`, or `TAILSCALE_*` keys.
- No matching secret, credential, `.env`, Azure, or Tailscale material was found
  in the workspace, Rally current task context, `/run`, `/var/run`, `/etc`, or
  `/home/agent` using filename/key-name scans that avoided printing secrets.
- `az`, `tailscale`, and `tailscaled` are not installed or present on `PATH`.
- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `rover up --no-provision -y` failed at `required command not found: az`.
- `rover provision` failed at `required command not found: az`.
- `rover connect` failed at `tailscale CLI not found; install it from
  https://tailscale.com/download and run 'tailscale up'`.
- `rover command true` failed at `required command not found: az`.
- `rover restart` failed at `required command not found: az`.
- `rover down -y` failed at `required command not found: az`.

Classification remains `needs_user`. Task 9.6 stays unchecked because the live
smoke did not pass. To unblock a future run, the container needs Azure CLI and
Tailscale CLI/daemon available, a non-interactive Azure login plus subscription
selection, and a Tailscale auth source (`TS_AUTHKEY` or OAuth client credentials
with the required tag grants).

## Re-run After CLI Install (lap work-f659, 2026-06-18 UTC)

This run removed the missing-binary blocker but is still blocked on missing
non-interactive credentials:

- Installed Azure CLI from Debian apt (`az` 2.45.0).
- Installed Tailscale via the official apt installer (`tailscale`/`tailscaled`
  1.98.4).
- No credential environment variable names were present for Azure or Tailscale:
  no `AZURE_*`, `ARM_*`, `TS_*`, `TAILSCALE_*`, or `ROVER_*` keys.
- Filename/key-name scans in `/workspace`, `/home/agent`, `/persist`, `/run`,
  `/var/run`, and `/etc` did not find Azure or Tailscale auth material. The only
  credential-looking persisted files found were unrelated agent/OAuth files.
- `az account show --output json` failed with `Please run 'az login'`.
- `az account list --output json` returned `[]` with the Azure CLI login
  warning.
- `az login --identity --output json` failed with MSI 403 Forbidden.
- A temporary userspace `tailscaled` started successfully on the default socket,
  but `tailscale status --json` reported `BackendState: NeedsLogin` and
  `tailscale status` reported `Logged out.`
- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `rover up --no-provision -y` failed at `not logged in to Azure`.
- `rover provision` failed at `not logged in to Azure`.
- `rover connect` failed at `local Tailscale is not connected; run 'tailscale up'`.
- `rover command true` failed at `not logged in to Azure`.
- `rover restart` failed at `not logged in to Azure`.
- `rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. Task 9.6 stays unchecked because the live
smoke did not pass. To unblock, inject a usable non-interactive Azure login
(service principal secret or federated token) and select a subscription with
`az account set --subscription <id>`, then authenticate local Tailscale with
`TS_AUTHKEY` or configured OAuth credentials with the required tag grants.

## Re-confirmation (lap work-e733, 2026-06-18 UTC)

Re-ran the 9.6 prerequisite and smoke checks. The runtime now has the binaries
installed, but no credentials were ever injected, so the smoke still cannot
execute. This is the same blocker, re-confirmed with the full search surface
below.

- `az` 2.45.0, `tailscale`/`tailscaled` 1.98.4 are present on `PATH`
  (the prior "command not found" blocker is resolved).
- No credential environment variable names present: no `AZURE_*`, `ARM_*`,
  `TS_*`, `TAILSCALE_*`, or `ROVER_*` keys in the exported environment, the
  non-exported shell variables, or `/proc/self/environ`.
- Content + filename scans (run with passwordless `sudo` across the whole
  filesystem) for `tskey-`, `AZURE_CLIENT_SECRET`, `AZURE_TENANT_ID`,
  `AZURE_SUBSCRIPTION_ID`, and `ARM_CLIENT_ID` found matches ONLY in rally
  try-logs, codex/gemini session transcripts, opencode logs, `.rally/state/*`,
  `.laps/laps.json` (all of which quote this task text), and unrelated nvidia
  plugin skill docs. No real Azure SP or Tailscale auth material exists
  anywhere, including `~/.azure` (config/log only), `/var/lib/tailscale` (empty
  `tailscaled.log*` only), `/run/tailscale`, `/persist/agent`, `/root`,
  `/run/secrets`, `/etc/azure`, and `/tmp/rover-tailscaled.state` (2 bytes).
- `az account show` -> `Please run 'az login'`; `az login --identity` not
  retried (prior MSI 403 documented above).
- `tailscale status` -> local `tailscaled` not running; no persisted state.
- `go build ./...` passed.
- `rover up --no-provision -y` -> `not logged in to Azure`.
- `rover provision` -> `not logged in to Azure`.
- `rover connect` -> `tailscale status: exit status 1` (local TS gate).
- `rover command true` -> `not logged in to Azure`.
- `rover restart` -> `not logged in to Azure`.
- `rover down -y` -> `not logged in to Azure`.

Classification remains `needs_user`. The task's "inject real non-interactive
Azure credentials and Tailscale auth material into the container" step cannot
be performed by the agent itself — those credentials are not present anywhere
in the container to inject, and they cannot be fabricated (real Azure SP / a
real tailnet authkey are required). Task 9.6 stays unchecked per the "tick 9.6
only if it passes" contract; no test assertions were changed. To unblock, a
real service-principal secret (or federated token) with tenant + subscription
IDs and a `TS_AUTHKEY` (or Tailscale OAuth client credentials with the required
tag grants) must be provided into the container environment by the operator.

## Re-confirmation (lap work-2d49, 2026-06-18 UTC)

Re-ran the 9.6 prerequisite and smoke checks for the claimed lap `work-2d49`. Despite the task instruction indicating that the operator injects the real Azure credentials and Tailscale auth source, no such credentials were found in the container environment or filesystem:

- Inspected exported environment variables (`env`) and shell variables (`set`) for `AZURE_*`, `ARM_*`, `TS_*`, or `TAILSCALE_*` keys; none were found.
- Searched filesystem with passwordless `sudo grep` for keys like `AZURE_CLIENT_SECRET` (excluding transcripts, git, laps, and caches) and found no occurrences of real credential values.
- Checked `/proc/self/environ` and the environments of running parent/adjacent processes (e.g. `rally` process environment); no credential variables were set.
- Checked `~/.azure` config files and `/run/tailscale`; they do not contain valid logins or configuration profiles.
- `az account show` continues to report `Please run 'az login'`.
- Local `tailscaled` is not running, and starting it in userspace shows a `NeedsLogin` / `Logged out` state.

Since credentials are not available, the live smoke cannot be executed. Per the contract, task 9.6 remains unchecked, and no code or test assertions were changed. The task remains classified as `needs_user`.

## Re-confirmation (lap work-0d22, 2026-06-18 UTC)

Re-checked the improve-architecture 9.6 live-smoke prerequisites after this lap
was reassigned with the instruction that the operator had injected real Azure
and Tailscale credentials. In the current container runtime, that injection is
still not present:

- Azure and Tailscale CLIs are still installed (`az` 2.45.0,
  `tailscale`/`tailscaled` 1.98.4).
- Checked the expected credential environment-variable names without printing
  secret values: `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`,
  `AZURE_CLIENT_SECRET`, `AZURE_SUBSCRIPTION_ID`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `TS_AUTHKEY`, `TAILSCALE_AUTHKEY`,
  `TAILSCALE_CLIENT_ID`, and `TAILSCALE_CLIENT_SECRET` are all unset.
- `az account show --output json` still fails with `ERROR: Please run 'az login'
  to setup account.`
- `tailscale status --json` still fails because local `tailscaled` is not
  running.

Classification remains `needs_user`. The operator-side credential injection
described by the task has not landed in the current runtime, so the agent still
cannot execute the optional 9.6 live smoke. Task 9.6 remains unchecked per the
"tick 9.6 only if it passes" contract; no code or test assertions were changed.
To unblock, inject a real non-interactive Azure login with a selected
subscription and authenticated local Tailscale state or auth material into this
container before reassigning the smoke again.
