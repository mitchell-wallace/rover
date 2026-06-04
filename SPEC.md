You are working on a new experimental Go CLI tool tentatively named Rover.

Rover’s purpose is to provision and manage remote Linux VM compute for running Dune remotely. Dune itself should remain focused on creating/entering coding environments. Rover is the adjacent tool for getting a suitable machine envelope online, then letting the user SSH/Tailscale into it and run Dune there.

This is an MVP. Optimize for a clean, understandable implementation over broad provider abstraction.

Primary target:
- Azure VMs
- Ubuntu Linux
- one active Rover-managed VM at a time
- size-aware start flow: small | medium | large
- explicit up/down/status commands
- usable both interactively via `rover` and non-interactively via Cobra commands/flags
- interactive prompts via `huh`
- infrastructure defined with Bicep
- bootstrap via cloud-init or equivalent first-boot prep
- host configuration via Ansible

Repository expectations:
- Create a fresh repo layout if needed.
- Use Go with Cobra for CLI parsing.
- Use `huh` for interactive prompts.
- Shell scripts can wrap `az` CLI operations where appropriate, but keep the Go CLI as the primary entrypoint.
- Keep state simple and explicit. Prefer a local Rover state/config file over implicit magic.
- Do not attempt multi-cloud support.
- Do not integrate into Dune yet.

MVP functional requirements:

1. Bicep infrastructure

Create Bicep config for Azure VM provisioning.

Support at least three size profiles:

- small
- medium
- large

Each profile should map to:
- VM SKU
- OS disk size
- optional data disk size if appropriate
- region default
- admin username
- SSH key config
- basic network/security config

Use sane defaults but keep them easy to edit.

The Bicep output should expose enough information for scripts/CLI to retrieve:
- VM name
- resource group
- public IP or DNS name, if used
- private IP, if relevant
- SSH connection target

Assume the user may later use Tailscale, but do not make Tailscale mandatory for MVP unless it is clearly simpler.

2. Cloud-init / first boot preparation

Add cloud-init or equivalent first-boot setup that prepares the machine for Ansible.

It should:
- update package metadata
- ensure Python is available for Ansible
- ensure the configured admin user can be reached via SSH
- install minimal prerequisites only
- avoid duplicating the full Ansible role

3. Ansible provisioning

Add Ansible playbook/roles to configure the VM for Dune usage.

Install and configure:
- Docker Engine
- lazydocker
- git
- gh
- gitui
- micro
- zsh
- Powerlevel10k
- common shell quality-of-life packages as needed
- `dune` via `curl -fsSL https://raw.githubusercontent.com/mitchell-wallace/dune/main/install.sh | bash`

Also:
- add user to docker group if appropriate
- configure zsh as the default shell if safe
- verify Docker works for the user
- avoid storing secrets in the repo
- keep the playbook idempotent

4. Azure shell scripts

Create shell scripts under something like `scripts/azure/`.

Required scripts:
- `up`
- `down`
- `status`
- `resize` or size-aware `up`
- `ssh`
- `ip` or connection-info helper

The scripts should:
- use Azure CLI
- enforce one active Rover-managed VM at a time for MVP
- support small | medium | large
- avoid deleting persistent disks by default unless explicitly requested
- clearly distinguish stop/deallocate from delete
- print useful next-step commands

Be careful with cost semantics:
- `stop/deallocate` should stop compute billing where Azure supports it
- persistent disks/IPs may still cost money
- scripts should make that visible

VM compute is chosen by family × size (default family: burstable):
- burstable (Ba, CPU-credit): xsmall B2als_v2 2/4, small B2as_v2 2/8, medium B4als_v2 4/8, large B4as_v2 4/16.
- balanced (D, sustained CPU): small D2as_v7 2/8, medium D4as_v7 4/16, large D8as_v7 8/32.
- ramheavy (E, memory-optimized): small E2as_v7 2/16, medium E4as_v7 4/32, large E8as_v7 8/64.
- xsmall is burstable-only (no sub-2-vCPU SKU in the D/E families).

5. Go CLI

Create a Go CLI using Cobra and huh.

It should support both:

Interactive mode:

```sh
rover
````

Non-interactive mode:

```sh
rover up small
rover up --size medium
rover down
rover status
rover ssh
rover provision
rover doctor
rover update
```

Cobra-parsed commands and interactive prompts should have parity. Interactive mode should call the same underlying command/service functions as non-interactive mode.

Suggested commands:

* `rover up [small|medium|large]`
* `rover down`
* `rover status`
* `rover ssh`
* `rover provision`
* `rover doctor`
* `rover config`
* `rover version`
* `rover update`

The CLI may invoke shell scripts for Azure operations initially, but the boundary should be clean enough that direct Azure SDK integration could replace scripts later.

6. State/config

Create a simple Rover config/state model.

Track:

* selected Azure subscription/resource group
* active VM name
* selected size
* region
* admin username
* SSH key path
* last known connection info
* whether Ansible has been run successfully

Do not overbuild locking or multi-VM orchestration. MVP is one active VM at a time.

7. Safety and UX

Rover should warn before destructive or costly operations.

Examples:

* starting a VM may incur Azure charges
* stopping/deallocating may leave disk/IP costs
* deleting resources is not part of normal down flow
* one active VM already exists, so starting another should fail or ask to stop the current one first

For MVP, do not attempt to detect active Dune/Rally sessions remotely. Add a placeholder note in docs for future cleanup/reminder integration.

8. Docs

Add README sections for:

* prerequisites
* Azure login
* SSH key setup
* creating config
* starting a VM
* provisioning with Ansible
* connecting to the VM
* running Dune remotely
* stopping/deallocating the VM
* cost warnings
* current limitations

9. Deliverables

Return:

* summary of files created
* exact commands to test locally
* known gaps
* recommended next hardening steps

Implementation priorities:

1. Clean repo structure.
2. Bicep + scripts usable manually.
3. Ansible playbook idempotent.
4. Go CLI wraps the flows.
5. Interactive and non-interactive modes share logic.
6. Docs make the workflow clear.

Do not over-engineer provider abstraction, multi-user support, daemon processes, background scheduling, or automatic VM shutdown in this MVP.

10. Packaging

- Use GoReleaser + ./VERSION file + CI auto-tagging setup similar to ../laps
- Include install.sh script and document copy-paste one-liner in README.md
