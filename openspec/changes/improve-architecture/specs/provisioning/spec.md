## ADDED Requirements

### Requirement: Provisioning is a service with injectable Ansible and SSH-wait seams

Ansible provisioning SHALL live in `internal/provision` as a `Service` that depends on Azure through an injected `AzureProvisioner` interface, on Tailscale through `tailscale.Client`, and on the playbook runner and SSH-wait through injectable seams (defaulting to `ansible.Provision` and a TCP-dial wait). This SHALL allow provisioning behavior to be tested without running Ansible or opening real sockets.

#### Scenario: Provision tested without real Ansible
- **GIVEN** a `provision.Service` constructed with a fake Ansible runner and a no-op SSH waiter
- **WHEN** `Run` executes
- **THEN** the fake runner receives the expected `ansible.Params` and no real playbook is invoked

### Requirement: Provisioning requires a running VM

`Service.Run` SHALL fail when no VM exists or the VM is not running, directing the user to `rover up`.

#### Scenario: VM not running
- **GIVEN** the VM exists but is not running
- **THEN** `Run` returns an error and does not invoke Ansible

### Requirement: Auth-key resolution and host selection

`Service.Run` SHALL resolve the Tailscale auth key preferring `TS_AUTHKEY`, then an OAuth-generated key, then proceeding without Tailscale. The resolved key SHALL be shell-sanitized and exported to the playbook process via the `TS_AUTHKEY` environment variable for the duration of the run (it is NOT passed through `ansible.Params`), and SHALL be unset after the run completes. When a Tailscale peer is already online it SHALL provision over the Tailscale target; otherwise it SHALL provision over the public IP. It SHALL wait (bounded) for SSH before running the playbook.

#### Scenario: OAuth key generation fails
- **GIVEN** no `TS_AUTHKEY` and OAuth configured but key generation fails
- **THEN** `Run` returns the key-generation error and does not provision

#### Scenario: Provision over an online Tailscale peer
- **GIVEN** the peer is online before provisioning
- **THEN** the Ansible host target is the Tailscale target rather than the public IP

#### Scenario: Auth key reaches the playbook via environment
- **GIVEN** a resolved, sanitized auth key
- **WHEN** the playbook runner is invoked
- **THEN** `TS_AUTHKEY` is set in the process environment to the sanitized key at invocation time
- **AND** it is unset after `Run` returns

### Requirement: Post-provision verify and lockdown

After a successful playbook run with Tailscale in use, `Service.Run` SHALL mark Ansible applied, sync connection info, verify the Tailscale peer is online, and — only when verified — close public SSH and persist the locked-down state. When verification fails it SHALL leave public SSH open and warn. When public SSH is already closed it SHALL not attempt to reclose.

#### Scenario: Verified peer triggers lockdown
- **GIVEN** provisioning used Tailscale and the peer verifies online afterward
- **AND** public SSH is currently open
- **THEN** public SSH is closed and the state is saved as locked down

#### Scenario: Verification fails leaves SSH open
- **GIVEN** provisioning used Tailscale but the peer does not verify online afterward
- **THEN** public SSH is left open and the user is warned
