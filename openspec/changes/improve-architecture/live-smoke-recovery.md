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
