# Rover

Rover provisions and manages a single remote Linux VM on **Azure** so you can SSH
in and run [Dune](https://github.com/mitchell-wallace/dune) there. Dune stays
focused on creating/entering coding environments; Rover is the adjacent tool that
gets a suitable machine online, configures it, and hands you a shell.

This is an MVP optimized for a clean, understandable implementation over broad
provider abstraction. It manages **one active VM at a time**.

```
rover up small        # provision a 2 vCPU / 4 GiB VM
rover provision       # configure it (Docker, dune, zsh, ...) via Ansible
rover ssh             # connect, then run `dune`
rover down            # deallocate (stop compute billing)
rover down --delete   # delete everything (stop all billing)
```

## Install

One-liner (downloads the latest release binary to `~/.local/bin`):

```sh
curl -fsSL https://raw.githubusercontent.com/mitchell-wallace/rover/main/install.sh | bash
```

Or build from source:

```sh
git clone https://github.com/mitchell-wallace/rover && cd rover
just build           # produces ./bin/rover
just install         # installs to ~/.local/bin (override with ROVER_INSTALL_DIR)
```

The binary is self-contained: the Bicep templates, Azure scripts, cloud-init,
and Ansible playbook are embedded and materialized into your cache dir on first
use.

## How it works

```
rover (Go CLI)
  ├─ internal/azure   → shells out to scripts/azure/* (az CLI + Bicep)
  ├─ internal/ansible → runs ansible/playbook.yml against the live VM
  └─ internal/config  → ~/.config/rover/state.json (the only state)

infra/bicep/main.bicep        size profiles, network, VM, cloud-init
infra/cloud-init/             minimal first-boot prep for Ansible
ansible/roles/dune/           Docker, dune, zsh+p10k, gh, lazydocker, gitui, ...
scripts/azure/                up · down · status · ssh · ip (usable standalone)
```

Interactive (`rover` with no args, a `huh` menu) and non-interactive
(subcommands/flags) modes call the exact same service functions, so they stay at
parity. The `internal/azure` boundary is intentionally script-agnostic — a
direct Azure SDK implementation could replace the scripts later without touching
callers.

## Prerequisites

Run `rover doctor` to check all of these at once.

- **Azure CLI** (`az`) — https://learn.microsoft.com/cli/azure/install-azure-cli
- **Bicep** — `az bicep install`
- **OpenSSH client** (`ssh`)
- **Ansible** — `pipx install ansible` or `pip install --user ansible`
- An **SSH key pair**
- An **Azure subscription** with VM core quota in your region (see
  [Quota](#quota) below)

## Azure login

```sh
az login
az account set --subscription "<name-or-id>"   # if you have several
```

Rover uses your active `az` login. Set a specific subscription in Rover config if
you want it pinned (`rover config --edit`).

## SSH key setup

If you don't already have a key:

```sh
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

Point Rover at the **public** key (the private key path is derived by stripping
`.pub`, or set explicitly):

```sh
rover config --edit     # set "SSH public key path" to ~/.ssh/id_ed25519.pub
```

## Creating config

Rover stores everything in `~/.config/rover/state.json`. Defaults are applied on
first run; view or edit them:

```sh
rover config            # show current config
rover config --edit     # interactive editor (region, RG, VM name, user, key, size)
rover config --path     # print the state file path
```

Tracked fields: subscription, resource group, region, VM name, size, admin
username, SSH key paths, last-known connection info, and whether Ansible has been
applied.

## Starting a VM

```sh
rover up small          # or: rover up --size medium / rover up large
```

Size profiles (edit in `infra/bicep/main.bicep`):

| size   | SKU                | vCPU | RAM    | OS disk |
|--------|--------------------|------|--------|---------|
| small  | Standard_B2ls_v2   | 2    | 4 GiB  | 30 GiB  |
| medium | Standard_B2s_v2    | 2    | 8 GiB  | 30 GiB  |
| large  | Standard_B4s_v2    | 4    | 16 GiB | 64 GiB  |

`up` creates the resource group, registers required providers (one-time),
deploys via Bicep, and runs cloud-init for first-boot prep. Re-running `up`
redeploys/resizes the same VM in place — Rover enforces one VM at a time.

## Provisioning with Ansible

```sh
rover provision
```

Runs `ansible/playbook.yml` against the live VM (ad-hoc inventory, no inventory
file needed). Installs and configures: Docker Engine + Compose, lazydocker, git,
gh, gitui, micro, zsh + Powerlevel10k, common CLI tools, and `dune`. Adds your
user to the `docker` group, sets zsh as the default shell, verifies Docker works,
and is fully idempotent (re-run any time).

## Connecting to the VM

```sh
rover ssh                       # interactive shell
rover ssh -- uname -a           # run a one-off remote command
rover status                    # power state + connection info
```

## Running Dune remotely

Once provisioned:

```sh
rover ssh
# on the VM:
dune            # create/enter a coding environment, backed by Docker
```

## Stopping / deallocating the VM

```sh
rover down              # DEALLOCATE: stops compute billing; disk + IP remain
rover down --delete     # DELETE: removes the resource group entirely
```

`down` (deallocate) is the normal way to pause — it stops the most expensive
charge (compute) while preserving the disk so `rover up` resumes quickly.
`down --delete` is the only command that removes persistent resources, and it
asks for confirmation (or `--yes` non-interactively).

## Cost warnings

Rover warns before costly/destructive actions, but you own the bill:

- **Running** a VM incurs **compute charges** for as long as it runs.
- **Deallocating** (`rover down`) stops compute billing, but the **OS disk and
  static public IP still incur small charges**.
- **`rover down --delete`** removes everything and stops all Rover charges.
- Deleting is never part of the normal `down` flow — you must ask for it.

The B-series v2 SKUs are inexpensive, but always check the
[Azure pricing calculator](https://azure.microsoft.com/pricing/calculator/) for
your region.

## Quota

Brand-new subscriptions often have **0 core quota** for the B-series v2 family
(`standardBsv2Family`). If `rover up` fails with `QuotaExceeded`, request an
increase (a few cores is plenty):

- Portal: *Subscriptions → Usage + quotas → request increase* for
  `standardBsv2Family` in your region, **or**
- pick a region/family where you already have quota and edit the SKUs in
  `infra/bicep/main.bicep`.

## Current limitations

- **Azure only.** No multi-cloud, by design.
- **One VM at a time.** No multi-VM orchestration, locking, or daemon.
- **No automatic shutdown / idle detection.** Remember to `rover down`.
- **No remote Dune/Rally session detection.** Future work: warn before bringing
  down a VM with an active session, and cost/cleanup reminders.
- **Not integrated into Dune** yet — Rover is standalone for now.
- Tailscale is not required for the MVP (public IP + SSH). It can be layered on
  later via the Ansible role.

## Development

```sh
just build      # build ./bin/rover
just install    # build + copy to ~/.local/bin
just test       # go test ./...
just lint       # golangci-lint
ROVER_ASSET_DIR=$PWD ./bin/rover status   # run against in-repo assets (no rebuild)
```

Releases: bump `./VERSION`, push to `main`; CI auto-tags `vX.Y.Z` and GoReleaser
publishes binaries (mirrors the `laps` setup).
