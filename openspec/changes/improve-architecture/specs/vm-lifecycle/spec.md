## ADDED Requirements

### Requirement: VM lifecycle is a composable service

All single-VM lifecycle workflows (up, down, restart, disk, status, ssh) SHALL live in `internal/vm` as methods on a `Service` that depends on Azure through an injected `AzureLifecycle` interface and composes `connectivity` and `provision` rather than reimplementing their logic. The command layer SHALL invoke these methods as thin adapters.

#### Scenario: Service composes connectivity and provision
- **GIVEN** a `vm.Service`
- **THEN** it restores connectivity via the connectivity service and auto-provisions via the provision service rather than containing duplicate copies of that logic

### Requirement: Up validates, confirms, and conditionally provisions

`Service.Up` SHALL normalize and validate the requested family/size and the admin username before acting. On a fresh create with provisioning enabled, when local Tailscale is not ready it SHALL warn that the VM will stay public-SSH-only and require confirmation. After a successful Azure `up` it SHALL persist family/size and connection info; on a fresh create it SHALL scrub known-hosts entries for the new host; on a fresh provisioning create it SHALL auto-provision (unless `--no-provision`); when starting an existing VM it SHALL restore connectivity.

#### Scenario: Fresh create with Tailscale not ready
- **GIVEN** a fresh create with provisioning enabled and local Tailscale not ready
- **WHEN** the user declines the public-SSH-only confirmation and `--yes` was not passed
- **THEN** `Up` aborts with guidance to configure Tailscale

#### Scenario: Starting an existing VM restores connectivity
- **GIVEN** an existing (non-fresh) VM is started
- **WHEN** `Up` completes the Azure start
- **THEN** it runs connectivity restore

### Requirement: Down distinguishes deallocate from delete

`Service.Down` with delete SHALL require confirmation (or `--yes`), run a best-effort in-VM Tailscale logout while the VM is still running, perform the Azure delete, then (when OAuth is configured) clean up Rover's Tailscale devices and reset connection/provision/public-SSH state. Without delete it SHALL deallocate and sync connection info, preserving disk and IP.

#### Scenario: Delete resets state and cleans up devices
- **GIVEN** delete confirmed and OAuth configured
- **WHEN** `Down` completes
- **THEN** Tailscale devices are cleaned up and connection/AnsibleApplied/public-SSH state are reset and saved

#### Scenario: Deallocate preserves resources
- **GIVEN** a deallocate (no delete)
- **THEN** the VM is deallocated, connection info is synced, and disk/IP are retained

### Requirement: Restart requires a running VM and restores connectivity

`Service.Restart` SHALL fail when no VM exists or the VM is not running. On success it SHALL sync connection info and run connectivity restore.

#### Scenario: Not running
- **GIVEN** the VM exists but is not running
- **THEN** `Restart` returns an error directing the user to `rover up`

### Requirement: Disk enforces minimum and no-shrink, and records size without a VM

`Service.Disk` SHALL reject sizes below 30 GiB and SHALL refuse to shrink an existing OS disk. With no VM yet it SHALL record the requested size for the next `up`. When the size already matches it SHALL be a no-op (still persisting the recorded size). A real resize SHALL require confirmation (or `--yes`) and SHALL sync connection info on success.

#### Scenario: Below minimum
- **GIVEN** a requested size below 30 GiB
- **THEN** `Disk` returns an error and makes no Azure call

#### Scenario: No VM yet
- **GIVEN** no VM exists
- **THEN** the requested size is recorded in state for the next `up`
