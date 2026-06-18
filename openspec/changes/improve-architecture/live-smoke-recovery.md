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

## Re-confirmation (lap work-0d22 rerun, 2026-06-18 UTC)

This lap was reassigned again with the same instruction that operator-provided
Azure and Tailscale credentials had been injected. The current runtime still
does not reflect that injection:

- The expected Azure and Tailscale credential names remain unset in the process
  environment: `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`,
  `AZURE_CLIENT_SECRET`, `AZURE_SUBSCRIPTION_ID`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `TS_AUTHKEY`, `TAILSCALE_AUTHKEY`,
  `TAILSCALE_CLIENT_ID`, and `TAILSCALE_CLIENT_SECRET`.
- `az account show --output json` still fails with `ERROR: Please run 'az login'
  to setup account.`
- `tailscale status --json` still fails with `failed to connect to local
  tailscaled; it doesn't appear to be running`.

Classification remains `needs_user`. The current container still lacks the
operator-side Azure login and Tailscale auth/state required to start the 9.6
live smoke, so task 9.6 remains unchecked. No code or test assertions were
changed in this rerun.

## Re-confirmation (lap work-0d22 rerun 2, 2026-06-18 UTC)

This lap was reassigned yet again with the same instruction that operator-side
Azure and Tailscale credentials had been injected. The runtime still does not
show that injection:

- The expected Azure and Tailscale credential names remain unset:
  `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`,
  `AZURE_SUBSCRIPTION_ID`, `ARM_*`, `AZURE_FEDERATED_TOKEN_FILE`,
  `TS_AUTHKEY`, `TAILSCALE_AUTHKEY`, `TAILSCALE_CLIENT_ID`, and
  `TAILSCALE_CLIENT_SECRET`.
- `az account show --output json` still fails with `ERROR: Please run 'az login'
  to setup account.`
- `tailscale status --json` still fails with `failed to connect to local
  tailscaled; it doesn't appear to be running`.

Classification remains `needs_user`. The required operator-provided Azure login
and Tailscale auth/state are still absent from this container runtime, so the
9.6 live smoke cannot start and task 9.6 remains unchecked. No code or test
assertions were changed in this rerun.

## Re-confirmation (lap work-0d22 rerun 4, 2026-06-18 UTC)

This lap was reassigned again with the same instruction that operator-side
Azure and Tailscale credentials had been injected. The runtime still does not
show that injection:

- The expected Azure and Tailscale credential names remain unset:
  `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`,
  `AZURE_SUBSCRIPTION_ID`, `ARM_*`, `AZURE_FEDERATED_TOKEN_FILE`,
  `TS_AUTHKEY`, `TAILSCALE_AUTHKEY`, `TAILSCALE_CLIENT_ID`, and
  `TAILSCALE_CLIENT_SECRET`.
- `az account show --output json` still fails with `ERROR: Please run 'az login'
  to setup account.`
- `tailscale status --json` still fails with `failed to connect to local
  tailscaled; it doesn't appear to be running`.

Classification remains `needs_user`. The required operator-provided Azure login
and Tailscale auth/state are still absent from this container runtime, so the
9.6 live smoke cannot start and task 9.6 remains unchecked. No code or test
assertions were changed in this rerun.

## Re-confirmation (lap work-0d22 rerun 3, 2026-06-18 UTC)

This lap was reassigned again with the same instruction that operator-side
Azure and Tailscale credentials had been injected. The runtime still does not
show that injection:

- The expected Azure and Tailscale credential names remain unset:
  `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`,
  `AZURE_SUBSCRIPTION_ID`, `ARM_*`, `AZURE_FEDERATED_TOKEN_FILE`,
  `TS_AUTHKEY`, `TAILSCALE_AUTHKEY`, `TAILSCALE_CLIENT_ID`, and
  `TAILSCALE_CLIENT_SECRET`.
- `az account show --output json` still fails with `ERROR: Please run 'az login'
  to setup account.`
- `tailscale status --json` still fails with `failed to connect to local
  tailscaled; it doesn't appear to be running`.

Classification remains `needs_user`. The required operator-provided Azure login
and Tailscale auth/state are still absent from this container runtime, so the
9.6 live smoke cannot start and task 9.6 remains unchecked. No code or test
assertions were changed in this rerun.

## Re-confirmation (lap work-dce5, 2026-06-18 UTC)

Re-read the OpenSpec apply context for `improve-architecture` and confirmed the
change is at 41/42 tasks complete with only optional live smoke task 9.6 still
unchecked. Rechecked the runtime after this lap was assigned with the requirement
to inject a real non-interactive Azure login and authenticated Tailscale state or
auth material.

Current runtime findings:

- `az` 2.45.0 and `tailscale`/`tailscaled` 1.98.4 are installed.
- No Azure or Tailscale credential variable names are present in exported env,
  shell vars, or scanned process environments: no `AZURE_*`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `TS_AUTHKEY`, `TAILSCALE_AUTHKEY`,
  `TS_OAUTH_CLIENT_*`, or `TAILSCALE_CLIENT_*`.
- `~/.azure/azureProfile.json` contains an empty subscriptions list and there is
  no `~/.azure/accessTokens.json`.
- `~/.config/rover/state.json`, `/var/lib/tailscale/tailscaled.state`, and
  `/run/tailscale/tailscaled.sock` are absent.
- Root-level and user-level filename/content scans for Azure SP/federated-token
  and Tailscale auth-key/OAuth names found only repo docs/tests and prior Rally
  logs quoting the task context, not real credential material.
- `az account show --output json` fails with `Please run 'az login'`.
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- A temporary userspace `tailscaled` starts, but `tailscale status --json`
  reports `BackendState: NeedsLogin`, with no tailnet.

Verification and smoke rerun:

- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `go run ./cmd/rover up -y` failed at `not logged in to Azure`.
- `go run ./cmd/rover provision` failed at `not logged in to Azure`.
- `go run ./cmd/rover connect -- true` failed at `tailscale status: exit status
  1`.
- `go run ./cmd/rover command true` failed at `not logged in to Azure`.
- `go run ./cmd/rover restart` failed at `not logged in to Azure`.
- `go run ./cmd/rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. There is no real Azure service-principal or
federated-token material, no selected subscription, and no authenticated local
Tailscale state/auth material in this container runtime for the agent to inject.
Task 9.6 remains unchecked per the "tick only if it passes" contract; no code or
test assertions were changed.

## Re-confirmation (lap work-d277, 2026-06-18 UTC)

Re-read the OpenSpec apply context for `improve-architecture`: the change is
still at 41/42 tasks complete, with only optional live smoke task 9.6 unchecked.
This lap was assigned with the requirement to inject real non-interactive Azure
credentials and authenticated Tailscale state/auth material, then rerun the live
smoke. The required auth material is still absent from the current container.

Current runtime findings:

- `az` 2.45.0 and `tailscale`/`tailscaled` 1.98.4 are installed.
- No Azure or Tailscale credential variable names are present in exported env,
  shell vars, or scanned process environments: no `AZURE_*`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `ARM_OIDC_TOKEN*`, `TS_AUTHKEY`,
  `TAILSCALE_AUTHKEY`, `TS_OAUTH_CLIENT_*`, or `TAILSCALE_CLIENT_*`.
- No service-principal secret, federated-token, Tailscale auth key, or Tailscale
  OAuth material was found in standard secret/cache locations or targeted
  filename/content scans of the workspace and likely runtime directories. Matches
  in the workspace are only docs/tests/recovery notes that mention the variable
  names, not usable credential values.
- `~/.azure/azureProfile.json` contains zero subscriptions, and
  `~/.azure/accessTokens.json` is absent.
- `~/.config/rover/state.json`, `/var/lib/tailscale/tailscaled.state`,
  `/run/tailscale/tailscaled.sock`, and `/var/run/tailscale/tailscaled.sock` are
  absent.
- The conditional service-principal/federated Azure login path had no env-backed
  material to use; no subscription source was available to select.
- `az account show --output json` fails with `Please run 'az login'`.
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- A temporary userspace `tailscaled` starts, but `tailscale status --json`
  reports `BackendState: NeedsLogin`, `selfOnline: false`, and zero peers.

Verification and smoke rerun:

- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `go run ./cmd/rover up --no-provision -y` failed at `not logged in to Azure`.
- `go run ./cmd/rover provision` failed at `not logged in to Azure`.
- `go run ./cmd/rover connect -- true` failed at `tailscale status: exit status
  1`.
- `go run ./cmd/rover command true` failed at `not logged in to Azure`.
- `go run ./cmd/rover restart` failed at `not logged in to Azure`.
- `go run ./cmd/rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. There is still no real Azure
service-principal or federated-token material, no selected subscription, and no
authenticated local Tailscale state/auth material in this container runtime for
the agent to inject. Task 9.6 remains unchecked per the "tick only if it passes"
contract; no code or test assertions were changed.

## Re-confirmation (lap work-d277 rerun, 2026-06-18 UTC)

Re-read the OpenSpec apply context for `improve-architecture`: the change remains
at 41/42 tasks complete, with only optional live smoke task 9.6 unchecked. This
rerun again attempted to use any real non-interactive Azure and Tailscale auth
material present in the container before running the live smoke.

Current runtime findings:

- `az` 2.45.0 and `tailscale`/`tailscaled` 1.98.4 are installed.
- No Azure or Tailscale credential variable names are present in exported env,
  shell vars, or scanned process environments: no `AZURE_*`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `ARM_OIDC_TOKEN*`, `TS_AUTHKEY`,
  `TAILSCALE_AUTHKEY`, `TS_OAUTH_CLIENT_*`, or `TAILSCALE_CLIENT_*`.
- Targeted filename scans of standard workspace/user/runtime locations found no
  service-principal secret, federated token, Tailscale auth key, or Tailscale
  OAuth material. The only matches remain package source/docs/tests/logs that
  mention credential names, not usable auth values.
- `~/.azure/azureProfile.json` still contains zero subscriptions, and
  `~/.azure/accessTokens.json` is absent.
- `~/.config/rover/state.json`, `/var/lib/tailscale/tailscaled.state`,
  `/run/tailscale/tailscaled.sock`, and `/var/run/tailscale/tailscaled.sock` are
  absent.
- The conditional service-principal/federated Azure login path again had no
  env-backed material to use; no subscription source was available to select.
- `az account show --output json` fails with `Please run 'az login'`.
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- A temporary userspace `tailscaled` starts, but `tailscale status --json`
  reports `BackendState: NeedsLogin`, `selfOnline: false`, and zero peers.

Verification and smoke rerun:

- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `go run ./cmd/rover up --no-provision -y` failed at `not logged in to Azure`.
- `go run ./cmd/rover provision` failed at `not logged in to Azure`.
- `go run ./cmd/rover connect -- true` failed at `tailscale status: exit status
  1`.
- `go run ./cmd/rover command true` failed at `not logged in to Azure`.
- `go run ./cmd/rover restart` failed at `not logged in to Azure`.
- `go run ./cmd/rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. There is still no real Azure
service-principal or federated-token material, no selected subscription, and no
authenticated local Tailscale state/auth material in this container runtime for
the agent to inject. Task 9.6 remains unchecked per the "tick only if it passes"
contract; no code or test assertions were changed.

## Re-confirmation (lap work-d277 rerun 2, 2026-06-18 UTC)

Re-read the OpenSpec apply context for `improve-architecture`: the change is
still at 41/42 tasks complete, with only optional live smoke task 9.6 unchecked.
This rerun again attempted to discover and use real non-interactive Azure and
Tailscale auth material before running the live smoke.

Current runtime findings:

- `az` 2.45.0 and `tailscale`/`tailscaled` 1.98.4 are installed.
- No Azure or Tailscale credential variable names are present in exported env,
  shell vars, or scanned process environments: no `AZURE_*`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `ARM_OIDC_TOKEN*`, `TS_AUTHKEY`,
  `TAILSCALE_AUTHKEY`, `TS_OAUTH_CLIENT_*`, or `TAILSCALE_CLIENT_*`.
- Targeted filename and content scans of standard workspace/user/runtime
  locations found no service-principal secret, federated token, Tailscale auth
  key, or Tailscale OAuth material. Matches were limited to Rover source/docs,
  recovery notes, unrelated plugin setup docs, and prior agent transcripts that
  mention credential names, not usable auth values.
- `~/.azure/azureProfile.json` still contains zero subscriptions, and
  `~/.azure/accessTokens.json` is absent.
- `~/.config/rover/state.json`, `/var/lib/tailscale/tailscaled.state`,
  `/run/tailscale/tailscaled.sock`, and `/var/run/tailscale/tailscaled.sock` are
  absent.
- The conditional service-principal/federated Azure login path again had no
  env-backed material to use; no subscription source was available to select.
- `az account show --output json` fails with `Please run 'az login'`.
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- `tailscale status --json` fails because local `tailscaled` is not running.
- A temporary userspace `tailscaled` starts, but `tailscale status --json`
  reports `BackendState: NeedsLogin`, `selfOnline: false`, and zero peers.

Verification and smoke rerun:

- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `go run ./cmd/rover up --no-provision -y` failed at `not logged in to Azure`.
- `go run ./cmd/rover provision` failed at `not logged in to Azure`.
- `go run ./cmd/rover connect -- true` failed at `tailscale status: exit status
  1`.
- `go run ./cmd/rover command true` failed at `not logged in to Azure`.
- `go run ./cmd/rover restart` failed at `not logged in to Azure`.
- `go run ./cmd/rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. There is still no real Azure
service-principal or federated-token material, no selected subscription, and no
authenticated local Tailscale state/auth material in this container runtime for
the agent to inject. Task 9.6 remains unchecked per the "tick only if it passes"
contract; no code or test assertions were changed.

## Re-confirmation (lap work-d277 rerun 3, 2026-06-18 UTC)

Re-read the OpenSpec apply context for `improve-architecture`: the change is
still at 41/42 tasks complete, with only optional live smoke task 9.6 unchecked.
This rerun again attempted to discover and use real non-interactive Azure and
Tailscale auth material before running the live smoke.

Current runtime findings:

- `az` 2.45.0 and `tailscale`/`tailscaled` 1.98.4 are installed.
- No Azure or Tailscale credential variable names are present in exported env,
  shell vars, or scanned process environments: no `AZURE_*`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `ARM_OIDC_TOKEN*`, `TS_AUTHKEY`,
  `TAILSCALE_AUTHKEY`, `TS_OAUTH_CLIENT_*`, or `TAILSCALE_CLIENT_*`.
- Targeted filename and content scans of standard workspace/user/runtime
  locations found no service-principal secret, federated token, Tailscale auth
  key, or Tailscale OAuth material. Matches were limited to Rover source/docs,
  recovery notes, unrelated plugin setup docs, and prior agent transcripts that
  mention credential names, not usable auth values.
- `~/.azure/azureProfile.json` still contains zero subscriptions, and
  `~/.azure/accessTokens.json` is absent.
- `~/.config/rover/state.json`, `/var/lib/tailscale/tailscaled.state`,
  `/run/tailscale/tailscaled.sock`, and `/var/run/tailscale/tailscaled.sock` are
  absent.
- The conditional service-principal/federated Azure login path again had no
  env-backed material to use; no subscription source was available to select.
- `az account show --output json` fails with `Please run 'az login'`.
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- `tailscale status --json` fails because local `tailscaled` is not running.
- A temporary userspace `tailscaled` starts, but `tailscale status --json`
  reports `BackendState: NeedsLogin`, `selfOnline: false`, and zero peers.

Verification and smoke rerun:

- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `go run ./cmd/rover up --no-provision -y` failed at `not logged in to Azure`.
- `go run ./cmd/rover provision` failed at `not logged in to Azure`.
- `go run ./cmd/rover connect -- true` failed at `tailscale status: exit status
  1`.
- `go run ./cmd/rover command true` failed at `not logged in to Azure`.
- `go run ./cmd/rover restart` failed at `not logged in to Azure`.
- `go run ./cmd/rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. There is still no real Azure
service-principal or federated-token material, no selected subscription, and no
authenticated local Tailscale state/auth material in this container runtime for
the agent to inject. Task 9.6 remains unchecked per the "tick only if it passes"
contract; no code or test assertions were changed.

## Re-confirmation (lap work-d277 rerun 4, 2026-06-18 UTC)

Re-read the OpenSpec apply context for `improve-architecture`: the change is
still at 41/42 tasks complete, with only optional live smoke task 9.6 unchecked.
This rerun again attempted to discover and use real non-interactive Azure and
Tailscale auth material before running the live smoke.

Current runtime findings:

- `az` 2.45.0 and `tailscale`/`tailscaled` 1.98.4 are installed.
- No Azure or Tailscale credential variable names are present in exported env,
  shell vars, or scanned process environments: no `AZURE_*`, `ARM_*`,
  `AZURE_FEDERATED_TOKEN_FILE`, `ARM_OIDC_TOKEN*`, `TS_AUTHKEY`,
  `TAILSCALE_AUTHKEY`, `TS_OAUTH_CLIENT_*`, or `TAILSCALE_CLIENT_*`.
- Targeted filename and content scans of standard workspace/user/runtime
  locations found no service-principal secret, federated token, Tailscale auth
  key, or Tailscale OAuth material. Matches were limited to Rover source/docs,
  recovery notes, unrelated plugin setup docs, and prior agent transcripts that
  mention credential names, not usable auth values.
- `~/.azure/azureProfile.json` still contains zero subscriptions, and
  `~/.azure/accessTokens.json` is absent.
- `~/.config/rover/state.json`, `/var/lib/tailscale/tailscaled.state`,
  `/run/tailscale/tailscaled.sock`, and `/var/run/tailscale/tailscaled.sock` are
  absent.
- The conditional service-principal/federated Azure login path again had no
  env-backed material to use; no subscription source was available to select.
- `az account show --output json` fails with `Please run 'az login'`.
- `az account list --output json` returns `[]` with the Azure CLI login warning.
- `az login --identity --output json` fails with MSI 403 Forbidden.
- `tailscale status --json` fails because local `tailscaled` is not running.
- A temporary userspace `tailscaled` starts, but `tailscale status --json`
  reports `BackendState: NeedsLogin`, `selfOnline: false`, and zero peers.

Verification and smoke rerun:

- `go build ./...` passed.
- `go test ./internal/connectivity/... ./internal/vm/... ./internal/provision/... ./internal/cmd`
  passed.
- `go run ./cmd/rover up --no-provision -y` failed at `not logged in to Azure`.
- `go run ./cmd/rover provision` failed at `not logged in to Azure`.
- `go run ./cmd/rover connect -- true` failed at `tailscale status: exit status
  1`.
- `go run ./cmd/rover command true` failed at `not logged in to Azure`.
- `go run ./cmd/rover restart` failed at `not logged in to Azure`.
- `go run ./cmd/rover down -y` failed at `not logged in to Azure`.

Classification remains `needs_user`. There is still no real Azure
service-principal or federated-token material, no selected subscription, and no
authenticated local Tailscale state/auth material in this container runtime for
the agent to inject. Task 9.6 remains unchecked per the "tick only if it passes"
contract; no code or test assertions were changed.

## RESOLVED — Live smoke passed (lap work-6afb, 2026-06-18 UTC)

The runtime prerequisites that every prior lap was blocked on are now present in
this container, so the optional 9.6 smoke executed end-to-end and **passed**.
Task 9.6 is now ticked.

Runtime state (verified before the run):

- Azure CLI authenticated: `az account show` returns subscription
  `Azure subscription 1` (`3202355a-d485-4072-99fe-36956d349691`), state
  `Enabled`, user `movedbytheword@outlook.com`. Explicitly re-selected with
  `az account set --subscription 3202355a-...`.
- Tailscale authenticated locally: `tailscale status --json` ->
  `BackendState: Running`, self `MINTAERO` (`100.121.215.18`), online.
- `rover doctor`: all checks pass (az, login, Bicep, ssh, Ansible, Tailscale CLI,
  SSH key).
- Rover state targets `rover-rg` / `rover-vm`, `burstable xsmall`
  (`Standard_B2als_v2`) in australiaeast -- the Basv2 family that is deployable on
  this sub per the `azure-quota-gotcha` memory. VM pre-existed and was
  `deallocated`.

Smoke sequence (built `/tmp/rover` from `./cmd/rover`; each exited 0):

- `rover up -y` -- started the deallocated VM, locked down public SSH, regenerated
  a Tailscale auth key, re-authenticated the VM's tailscaled, peer came online.
- `rover provision` -- Ansible `PLAY RECAP ... failed=0 unreachable=0`
  (`ok=37 changed=4 skipped=12`); Tailscale verified; public SSH confirmed closed.
- `rover connect -- 'echo connect-ok && hostname && whoami'` -- ran over Tailscale
  SSH; returned `rover-vm` / `mitchell`.
- `rover command 'echo command-ok && uname -a'` -- ran over Tailscale; returned the
  VM `uname`.
- `rover restart` -- restarted the running VM, re-locked public SSH, re-auth'd
  Tailscale, peer back online; a follow-up `rover command` confirmed reachability.
- `rover down -y` -- deallocated the VM (disk + static IP retained), returning the
  VM to its original `deallocated` state. No resources were deleted.

No code or test assertions were changed; only `tasks.md` (9.6 ticked) and this
note were edited.

## Re-confirmation (lap work-0d22, relay-2 run-3, 2026-06-18 UTC)

This lap (`work-0d22`: operator injects real Azure/Tailscale credentials before
reassigning the smoke) was re-claimed in relay-2 run-3. The operator-side
credential injection described by the task is now verifiably present in the
current container runtime, and the 9.6 smoke it gates has already passed:

- Azure CLI is authenticated non-interactively: `az account show` returns
  subscription `3202355a-d485-4072-99fe-36956d349691` (`Azure subscription 1`,
  `Enabled`), tenant `cc66fc7e-54fe-4e6d-8f7b-91ef0b284b16`, user
  `movedbytheword@outlook.com`. No `AZURE_*`/`ARM_*`/`TS_*` env names are needed
  because the login is materialised in the Azure CLI token cache.
- Tailscale is authenticated locally: `tailscale status --json` ->
  `BackendState: Running`, `HaveNodeKey: true`, self `MINTAERO`
  (`100.121.215.18`), online.
- The optional 9.6 live smoke already executed end-to-end and **passed** in the
  preceding laps (see "RESOLVED — Live smoke passed (lap work-6afb)" above, and
  relay-2 runs 1 & 2): all six paths (`up`/`provision`/`connect`/`command`/
  `restart`/`down`) exited 0, provision `failed=0`, and the VM was returned to
  `deallocated`.
- Task 9.6 is ticked `[x]` in `tasks.md` (commit `3dd112f`), and the working
  tree is clean.

The live smoke was **not** re-executed in this run. Per the "tick 9.6 only if it
passes" contract it is already ticked on the strength of the documented passing
run; re-running would only needlessly cycle a real Azure VM (start → provision →
deallocate) at real cost with no assertion change. The lap's stated objective —
operator credential injection plus smoke re-assignment — is satisfied:
credentials are injected and the smoke has been reassigned and has passed.

Classification: resolved / lap complete. No code or test assertions were changed;
only this note was edited.
