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
ansible/roles/tailscale/      optional tailnet join + Tailscale SSH (opt-in)
scripts/azure/                up · down · status · ssh · ip (usable standalone)
```

Interactive (`rover` with no args, a `huh` menu) and non-interactive
(subcommands/flags) modes call the exact same service functions, so they stay at
parity. The `internal/azure` boundary is intentionally script-agnostic — a
direct Azure SDK implementation could replace the scripts later without touching
callers.

## Prerequisites

Run `rover doctor` to check all of these at once. When run in a terminal, it
offers to fix what it can — generate a missing SSH key, run `az login`, or
`az bicep install`.

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

Rover stores everything in `~/.config/rover/state.json`. On a fresh install the
quickest path is the guided setup:

```sh
rover init              # walk through region, size, admin user, SSH key
```

`rover init` validates the admin username (Azure rejects reserved names like
`admin`/`root`) and offers to generate an ed25519 key pair if the chosen public
key doesn't exist yet. Running bare `rover` on a brand-new install launches the
same flow automatically before showing the menu.

Defaults are applied on first run; view or edit them directly any time:

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
rover up small                          # burstable (default family), small
rover up --family ramheavy medium       # memory-optimized, medium
rover up --family balanced --size large # general-purpose, large
```

Compute is chosen along two axes — **family** (the hardware tier) and **size**
(a t-shirt size within that family). Defaults to `burstable`. Edit the mapping
in `infra/bicep/main.bicep` (mirrored in `internal/sizes/sizes.go`):

| size   | burstable (Ba) — cheap, CPU-credit | balanced (D) — sustained CPU | ramheavy (E) — memory-optimized |
|--------|------------------------------------|------------------------------|---------------------------------|
| xsmall | Standard_B2als_v2 · 2 / 4 GiB      | —                            | —                               |
| small  | Standard_B2as_v2 · 2 / 8 GiB       | Standard_D2as_v7 · 2 / 8 GiB | Standard_E2as_v7 · 2 / 16 GiB   |
| medium | Standard_B4als_v2 · 4 / 8 GiB      | Standard_D4as_v7 · 4 / 16 GiB| Standard_E4as_v7 · 4 / 32 GiB   |
| large  | Standard_B4as_v2 · 4 / 16 GiB      | Standard_D8as_v7 · 8 / 32 GiB| Standard_E8as_v7 · 8 / 64 GiB   |

`xsmall` is burstable-only — Azure's balanced/ramheavy families have no
sub-2-vCPU SKU. Each family needs its own core quota in your region.

Disk size is **independent of compute size** (see [Disk](#disk-storage) below) —
changing size only swaps the CPU/RAM envelope and **preserves your disk and its
data**.

`up` creates the resource group, registers required providers (one-time),
deploys via Bicep, and runs cloud-init for first-boot prep. Re-running `up`
redeploys/resizes the same VM in place — Rover enforces one VM at a time.

## Disk / storage

The OS disk is a single persistent disk whose size is **decoupled from the
compute size**, so you can scale CPU/RAM up and down without ever touching your
data. The size is tracked in Rover config (default 30 GiB) and applied on every
`up`.

```sh
rover disk 64        # grow the OS disk to 64 GiB
rover status         # shows current disk size
```

Notes:
- Azure OS disks can **grow but never shrink**.
- Resizing deallocates the VM briefly, then restarts it if it was running;
  Ubuntu's cloud-init grows the root filesystem automatically on the next boot.
- The new size is persisted, so later `rover up` (incl. compute resizes) keeps
  it. Running `rover disk <gb>` before the first `up` just records the size.

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
rover ssh                       # interactive shell (public IP + SSH key)
rover ssh -- uname -a           # run a one-off remote command
rover connect                   # connect over Tailscale (see below)
rover status                    # power state + connection info
```

## Tailscale (optional)

Rover can join each VM to your [Tailscale](https://tailscale.com) tailnet so you
connect over an encrypted mesh with `rover connect` — independent of the Azure
public IP, using **Tailscale SSH** (no SSH keypair to manage). It's strictly
opt-in: if you don't set `TS_AUTHKEY`, provisioning skips Tailscale entirely.

**One-time setup:**

1. Create a free account at [login.tailscale.com](https://login.tailscale.com)
   and install the Tailscale client on **your laptop** (`tailscale up`).
2. In the admin console, edit the **ACL policy** to define the `tag:rover` owner
   and allow Tailscale SSH to it, e.g.:

   ```jsonc
   {
     "tagOwners": { "tag:rover": ["autogroup:admin"] },
     "ssh": [
       {
         "action": "accept",
         "src":    ["autogroup:member"],
         "dst":    ["tag:rover"],
         "users":  ["autogroup:nonroot"]
       }
     ]
   }
   ```
3. Generate an **auth key** (Settings → Keys → *Generate auth key*): make it
   **Reusable** + **Ephemeral**, and attach the tag **`tag:rover`**. Ephemeral
   means deallocated/deleted VMs auto-clean from your tailnet.

**Use it:**

```sh
export TS_AUTHKEY=tskey-auth-xxxxxxxx     # the key you generated
rover up small
rover provision                            # detects TS_AUTHKEY, joins the tailnet
rover connect                              # Tailscale SSH to rover-vm
```

`rover connect` checks that the VM is online in your tailnet and connects to its
MagicDNS name (`<hostname>.<tailnet>.ts.net`). If it's offline it tells you to
`rover up`; if it was never provisioned with Tailscale it falls back to a hint to
use `rover ssh`. The node name/tags are configurable via `rover config --edit`
(defaults: hostname = VM name, tags = `tag:rover`). The auth key is **never**
stored on disk — it's read from `TS_AUTHKEY` at provision time only.

> Note: Rover keeps public SSH (port 22) open as well, so `rover ssh` always
> works as a fallback. Locking the NSG down to Tailscale-only is a future option.

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

Brand-new subscriptions often have **0 core quota** for the AMD B-series v2
family (`standardBasv2Family`). If `rover up` fails with `QuotaExceeded`, request
an increase (a few cores is plenty):

- Portal: *Subscriptions → Usage + quotas → request increase* for
  `standardBasv2Family` in your region, **or**
- pick a region/family where you already have quota and edit the SKUs in
  `infra/bicep/main.bicep`.

## Current limitations

- **Azure only.** No multi-cloud, by design.
- **One VM at a time.** No multi-VM orchestration, locking, or daemon.
- **No automatic shutdown / idle detection.** Remember to `rover down`.
- **No remote Dune/Rally session detection.** Future work: warn before bringing
  down a VM with an active session, and cost/cleanup reminders.
- **Not integrated into Dune** yet — Rover is standalone for now.
- **Tailscale is optional** (see above) and additive — public SSH stays open.
  Auth keys expire (max 90 days); for fully hands-off automation, a Tailscale
  OAuth client (mint keys on demand) is a future upgrade.

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
