# Spike 1: VM and Tailscale Reliability Signals

## Objective

Identify the minimum reliable set of Azure, Tailscale, SSH, and guest-side
signals Rover should use before choosing one of these outcomes:

- connect over Tailscale
- retry remote Tailscale re-auth
- fall back to public SSH
- tell the user the VM is not running
- tell the user diagnostics are needed

## Tasks

1. Reproduce the online-but-unpingable Tailscale failure.
   - Capture local `tailscale status --json`.
   - Capture `tailscale ping --timeout=3s --c 1 <target>`.
   - Capture whether Tailscale SSH succeeds.
   - Record whether Azure reports the VM as running.

2. Test remote repair through Azure Run Command.
   - Run the same `tailscale up --authkey ... --ssh --hostname ... --advertise-tags ...` command Rover uses.
   - Measure Run Command latency.
   - Record Run Command failure modes when the VM is deallocated, booting, or has a broken Azure agent.
   - Verify whether `sudo systemctl restart tailscaled` is ever needed before `tailscale up`.

3. Compare verification signals.
   - Determine when `Peer.Online` is stale or optimistic.
   - Determine whether `tailscale ping` is enough before Tailscale SSH.
   - Determine whether a Tailscale SSH smoke test catches failures that ping misses.

4. Investigate guest-side health output.
   - Prototype `/usr/local/bin/rover-health --json`.
   - Include cloud-init, Azure agent, tailscaled, Tailscale backend state, Tailscale IP, SSH listener, disk pressure, and memory pressure.
   - Keep output under Azure Run Command's practical output limit.
   - Redact hostnames, users, IPs, and raw logs by default.

5. Define telemetry categories.
   - Map observed failures into stable categories.
   - Decide which fields are useful and safe.
   - Confirm that no auth keys, command strings, IPs, FQDNs, resource group names, or usernames are emitted.

## Acceptance Criteria

- A short findings note lists which signals are trustworthy, weak, or noisy.
- A proposed connection decision tree is ready for implementation.
- A draft guest health JSON schema exists.
- A recommended telemetry event list and failure category list exists.
- At least one real VM run documents timings for start, repair, Tailscale ping,
  and Tailscale SSH smoke test.
