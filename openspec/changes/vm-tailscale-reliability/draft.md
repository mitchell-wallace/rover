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
