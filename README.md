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
rover restart         # reboot the running VM
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

## Observability (New Relic)

Rover can report best-effort custom events to New Relic when
`ROVER_NEW_RELIC_LICENSE_KEY` is set. `ROVER_NEW_RELIC_APP_NAME` optionally
overrides the default application name (`Rover CLI`). The `RoverUp`,
`RoverProvision`, and `RoverDiagnostic` events describe aggregate compute
selection and provisioning outcomes; diagnostics contain stable classifications
only, never raw Azure/provider errors. With no license key, Rover constructs no
New Relic agent and every command follows the same no-telemetry behavior.

## How it works

```
rover (Go CLI)
  ├─ internal/azure   → shells out to scripts/azure/* (az CLI + Bicep)
  ├─ internal/ansible → runs ansible/playbook.yml against the live VM
  └─ internal/config  → ~/.config/rover/state.json + isolated Azure CLI state

infra/bicep/main.bicep        size profiles, network, VM, cloud-init
infra/cloud-init/             minimal first-boot prep for Ansible
ansible/roles/dune/           Docker Sandboxes, dune, thenn, mise, agent CLIs, shell tools, ...
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
offers to fix what it can — generate a missing SSH key, run `rover login`, or
install Bicep in Rover's isolated Azure context.

- **Azure CLI** (`az`) — https://learn.microsoft.com/cli/azure/install-azure-cli
- **Bicep** — `rover doctor` can install it in Rover's Azure context
- **OpenSSH client** (`ssh`)
- **Ansible** — `pipx install ansible` or `pip install --user ansible`
- An **SSH key pair**
- An **Azure subscription** with VM core quota in your region (see
  [Quota](#quota) below)

## Azure login

```sh
rover login                    # device-code flow (default)
rover login --browser          # optional browser-based az login
rover status                   # show Rover's identity + active subscription
rover logout
```

Rover deliberately does **not** use the host machine's normal `~/.azure` login.
Every Azure CLI process Rover starts receives an `AZURE_CONFIG_DIR` pointing to
Rover's isolated directory (by default `~/.config/rover/azure`). This lets Rover
use a work or personal Azure identity independently of other `az` CLI use on the
same machine. `rover logout` clears only that isolated context.

The Azure section of `state.json` supports an optional directory, subscription,
and tenant:

```json
{
  "azure": {
    "config_dir": "/home/me/.config/rover/azure",
    "subscription": "name-or-id",
    "tenant": "tenant-id"
  }
}
```

Blank fields select the isolated default directory and Azure CLI defaults. A
configured tenant is passed to `rover login`; a configured subscription is
selected after login and passed to Rover's Azure resource commands. Set these
with `rover config --edit` or by editing `rover config --path`.

If `AZURE_CONFIG_DIR` is already explicitly set in the environment, it wins over
`azure.config_dir` and Rover prints a warning. This override is intended for
deliberate advanced use; unset it to restore config-file control.

> **Upgrading:** Existing `state.json` files continue to load, including the old
> top-level `subscription` field, which Rover migrates into the Azure section.
> Because credentials now default to the isolated directory, run `rover login`
> once after upgrading. Your existing host `az` login remains untouched.

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

Tracked fields include the Azure config directory/subscription/tenant, resource
group, region, VM name, size, admin username, SSH key paths, last-known
connection info, and whether Ansible has been applied. The Azure credential and
token files themselves live under `azure.config_dir`, not in `state.json`.

The scripts under `scripts/azure/` remain usable standalone. To make a direct
script invocation use Rover's default isolated identity, export the directory
first (use your configured `azure.config_dir` instead when customized):

```sh
export AZURE_CONFIG_DIR="$(dirname "$(rover config --path)")/azure"
scripts/azure/status
```

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

On a **fresh create** (no VM yet), `up` then **provisions automatically** and, if
Tailscale is set up, **locks the VM down to Tailscale-only SSH** (see
[Tailscale](#tailscale-optional)). Pass `--no-provision` to skip the automatic
provision. Re-running `up` on an existing VM does **not** run the full playbook.
When the compute size changes, Rover runs only the targeted swapfile playbook
so swap tracks the new RAM size; other redeploys leave provisioning untouched.

If Tailscale isn't configured/connected when you create a VM, `up` warns that
lockdown can't engage and asks before creating a VM that stays reachable on the
public SSH port.

### SSH port

Rover does not use the default SSH port 22. It listens on a non-default high port
(default **29472**, configurable via `rover config --edit`), set at first boot and
allowed through the Azure NSG. `rover ssh`/`rover provision` use it automatically;
a manual `ssh` needs `-p 29472`. Tailscale SSH (`rover connect`) is unaffected by
this port.

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
rover provision --swapfile-only  # update only swap after a manual size change
```

Runs `ansible/playbook.yml` against the live VM (ad-hoc inventory, no inventory
file needed). Installs and configures: Docker Engine + Compose and Docker
Sandboxes, lazydocker, git, gh, gitui, micro, zsh + Powerlevel10k, common CLI
tools, `mise`, Claude Code, `dune`, and the latest `thenn` release. Adds your user
to the `docker` and `kvm` groups, sets zsh as the default shell, verifies Docker
works, and is fully idempotent (re-run any time).

Provisioning also manages `/swapfile` at half of the VM's runtime-detected RAM,
enables it, and persists it in `/etc/fstab`. Existing manual swapfiles are
adopted when already the right size or safely replaced (including `swapoff`
when active) when the VM's memory changes. The swapfile-only command runs
`ansible/swapfile.yml`; it does not re-run the rest of provisioning.

Docker's interactive setup suggests `newgrp kvm` to update the current shell's
supplementary groups. Provisioning does not start that persistent subshell;
Ansible resets its SSH connection after changing group membership, which gives
the remainder of the run a fresh login with both groups immediately available.

## Connecting to the VM

```sh
rover ssh                       # attach/create the "rover" tmux session
rover ssh --no-tmux             # opt out once and open a plain shell
rover ssh -- uname -a           # run a one-off command (never uses tmux)
rover connect                   # connect over Tailscale (see below)
rover status                    # power state + connection info
```

Interactive `rover ssh` sessions run `tmux new-session -A -s rover`, so a
disconnect does not discard the remote terminal. The interactive menu uses the
same default. Tmux is installed by `rover provision`; if it is missing on an
older or unprovisioned VM, Rover prints one warning and opens a plain shell.
To opt out persistently, disable **Attach interactive SSH sessions to tmux** in
`rover config --edit`, which saves `"ssh": {"tmux": false}` in Rover's config.

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
3. Generate an **auth key** (Settings → Keys → *Generate auth key*) OR generate an **OAuth Client** (Settings → **Trust credentials** → *+ Credential* → choose **OAuth**, not OpenID Connect):
   * **OAuth Client (Recommended)**: Grant the **`auth_keys`** scope with **write** access (Rover only mints ephemeral auth keys — it never touches device-management endpoints, so `devices:core` is not needed) and attach the tag **`tag:rover`**. Copy the Client ID and Client Secret immediately. The tag is required — and `tag:rover` must list the client (or `autogroup:admin`) under `tagOwners` in your ACL (step 2), or key minting succeeds but the node can't apply the tag and won't come up.
   * **Auth Key**: Make it **Reusable** + **Ephemeral**, and attach the tag **`tag:rover`**. Ephemeral means deallocated/deleted VMs auto-clean from your tailnet.

**Use it (OAuth Client — recommended):**

Configure your credentials once; Rover mints a single-use key on demand each provision. Make sure local Tailscale is connected (`tailscale up`) so Rover can verify the join and close public SSH.

```sh
rover config --edit       # Enter the OAuth Client ID + Client Secret
rover up small            # fresh create → auto-provisions, joins tailnet, then
                          # auto-closes public SSH once Tailscale is verified
rover connect             # Tailscale SSH to rover-vm
```

(You can still run `rover provision` manually — e.g. after `--no-provision`, or to
re-run config. It performs the same join + lockdown.)

**Use it (Auth Key):**

```sh
export TS_AUTHKEY=tskey-auth-xxxxxxxx     # the key you generated
rover up small                             # fresh create auto-provisions, detects
                                           # TS_AUTHKEY, joins the tailnet, locks down
rover connect                              # Tailscale SSH to rover-vm
```

`rover connect` checks that the VM is online in your tailnet and connects to its
MagicDNS name (`<hostname>.<tailnet>.ts.net`). If it's offline it tells you to
`rover up`; if it was never provisioned with Tailscale it falls back to a hint to
use `rover ssh`. The node name/tags are configurable via `rover config --edit`
(defaults: hostname = VM name, tags = `tag:rover`). The OAuth client credentials
are saved locally in `~/.config/rover/state.json` (protected with `0600` owner-only
permissions) so you don't need to specify them on every command.

> Note: Public SSH (the non-default port, default 29472) is open during the
> bootstrap provision as a fallback. Once Tailscale is verified online after
> provisioning, Rover **automatically** updates the Azure NSG rule to block public
> SSH, locking the VM down to Tailscale-only — no prompt. Subsequent `rover
> provision` runs route over your tailnet. If Tailscale verification fails, public
> SSH is left open and Rover warns you. Re-running `rover up` on a fresh create
> reopens public SSH for the bootstrap; `rover config --edit` also exposes the
> lockdown flag.

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
rover restart           # RESTART: reboot a running VM, preserving compute allocation
rover down --delete     # DELETE: removes the resource group entirely
```

`down` (deallocate) is the normal way to pause — it stops the most expensive
charge (compute) while preserving the disk so `rover up` resumes quickly.
`down --delete` is the only command that removes persistent resources, and it
asks for confirmation (or `--yes` non-interactively).

`restart` is for recovering a wedged running VM without deallocating it. It uses
Azure VM restart, refreshes Rover's saved connection info, and re-runs the
Tailscale connectivity restoration path when public SSH is locked down.

### Halting from inside the VM

Provisioned VMs include a `rover-halt` command that deallocates the VM from
within — no Azure credentials on the VM, no laptop required. It uses the VM's
system-assigned managed identity and the Azure Instance Metadata Service
(IMDS) to authenticate and self-deallocate.

```sh
rover-halt                    # deallocate now
sleep 2h && rover-halt        # deallocate after 2 hours (e.g. in tmux/zellij)
```

This is useful for scheduling shutdowns after long-running tasks:

```sh
# Inside the VM, in a tmux session:
dune run my-agent-task && rover-halt   # halt when the task finishes
```

The command prints a goodbye message, forks a background deallocate call, and
exits cleanly. Your SSH session drops ~2 seconds later as the VM shuts down.
Resume later with `rover up` from your laptop.

> **Note:** `rover-halt` requires Owner-level permissions on your Azure
> subscription for the initial deployment (to create the managed identity and
> role assignment). If the deployment fails with a permissions error, the VM
> still works — just without `rover-halt`. You can add the role assignment
> manually later.

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
- **No automatic shutdown / idle detection.** Remember to `rover down`, or schedule a halt from inside the VM with `rover-halt`.
- **No remote Dune/Rally session detection.** Future work: warn before bringing
  down a VM with an active session, and cost/cleanup reminders.
- **Not integrated into Dune** yet — Rover is standalone for now.
- **Tailscale is optional** (see above). When set up, a fresh `rover up`
  auto-closes public SSH after verifying the tailnet join; without it the VM
  stays on the public (non-default) SSH port. The OAuth client (mints keys on
  demand) is the recommended hands-off path.

## Updating Rover

```sh
rover update
```

Check the Rover GitHub repository for a newer version. Prints the current and latest versions. If a newer release exists, prompts for Y/n confirmation before downloading and installing it via the official install script. Use `rover update --yes` to install non-interactively.

## Architecture

Rover is layered as **thin command adapters** over a small set of **service
packages**, each a deep module with a narrow `Service` surface. A reader
entering at a Cobra file reaches a bounded unit of behavior in at most two
hops: Cobra file → `Service` method → helper. This section is the navigation
guide; the full decision rationale lives in
[`openspec/changes/improve-architecture/design.md`](openspec/changes/improve-architecture/design.md).

```
cmd/rover                 ← `main` entrypoint
internal/
  cmd/          ← thin Cobra adapters; appContext (root.go) is the composition root
  vm/           ← single-VM lifecycle: up/down/restart/disk/status + teardown cleanup
  provision/    ← Ansible provisioning: auth-key resolution, host select, verify + lockdown
  connectivity/ ← Tailscale verify/repair/route + public-SSH fallback (deepest module)
  stateutil/    ← shared Azure → config.Connection mapping (used by vm + provision)
  shellsafe/    ← pure AuthKey/ShellArg sanitizers (no ui import)
  azure/        ← script-backed az/Bicep leaf; *Client satisfies per-package interfaces
  tailscale/    ← CLI + control-plane API + the provider-side Client interface
  ansible/ config/ sizes/ ui/   ← unchanged leaves
```

**Dependency direction (acyclic — verified against shipped imports):**

```
cmd ──▶ vm · provision · connectivity        (loadContext wires the concrete services)
vm           ──▶ azure, tailscale, sizes, stateutil, config, ui
provision    ──▶ ansible, azure, tailscale, shellsafe, stateutil, config, ui
connectivity ──▶ azure, tailscale, shellsafe, config, ui
stateutil    ──▶ azure, config
shellsafe    ──▶ (pure)
```

No service package imports `cmd`; `connectivity` and `provision` do not import
`vm`. `vm` consumes connectivity/provision **behavior** through narrow
consumer-side interfaces (`connRestorer`, `provisioner`) rather than importing
those packages — so its tests inject recording fakes instead of building real
sub-services.

**Provider seams — inject, don't patch.** Every cross-package dependency is a
constructor-injected interface; there are no global function vars or
package-level timing knobs used as test seams.

- `tailscale.Client` (`internal/tailscale/client.go`) — the one **provider-side**
  interface, shared by `connectivity`, `provision`, and `vm`. It lives in
  package `tailscale` (not a consumer) so the `Peer`/`CleanupResult` types do
  not form an import cycle. `NewClient()` returns the default `CLI` adapter;
  tests inject fakes.
- **Per-package Azure interfaces** (consumer-side, each satisfied by
  `*azure.Client` via structural typing, declaring only what that package uses):
  `connectivity.AzureControl` (`Status`/`SetPublicSSH`/`RunCommand`),
  `provision.AzureProvisioner` (`Info`/`SetPublicSSH`),
  `vm.AzureLifecycle` (`Up`/`Down`/`Status`/`Restart`/`ResizeDisk`/`SSH`/`RunCommand`).
- **Func seams:** `connectivity.CommandRunner` (replaces the former
  `runRemoteCommandFn` global) and `provision.SSHWaiter` decouple remote exec
  and the SSH-wait loop. `connectivity.PollConfig` / `ReconnectConfig` carry the
  repair/reconnect timing that used to live in package-level vars.

**Composition root.** `loadContext` (`internal/cmd/root.go`) builds one
`azure.Client` and one `tailscale.Client`, then composes `connectivity.New`,
`provision.New`, and `vm.Service`, wiring the concrete services together.
Interactive (`rover`) and non-interactive (subcommand) modes call the same
service methods, so they stay at parity.

**Where does X live?**

| Looking for… | Go to |
| --- | --- |
| A command's flow | `internal/cmd/<cmd>.go` → the `a.vm.*` / `a.conn.*` / `a.provision.*` call |
| Tailscale ready / reauth / repair / connect / command routing | `internal/connectivity` (`repair.go`, `route.go`) |
| VM up/down/restart/disk/status, device cleanup | `internal/vm` (`lifecycle.go`, `disk.go`, `cleanup.go`) |
| Ansible run, auth-key + host selection, lockdown | `internal/provision` (`service.go`, `wait.go`) |
| Persisted connection-state mapping | `internal/stateutil` |
| Auth-key / shell-arg sanitization | `internal/shellsafe` |

**File-size budget (soft, review-enforced).** Source files ≤ 300 lines
(target ~200), `_test.go` files ≤ 400, a `Service` primary file ~250 — split by
sub-behavior before exceeding. A file may exceed the budget only with a
top-of-file justification. Pre-existing `internal/config/config.go`,
`internal/tailscale/tailscale.go`, and `internal/azure/azure.go` predate the
budget and are not retrofitted.

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
