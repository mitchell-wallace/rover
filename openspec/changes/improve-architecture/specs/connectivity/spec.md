## ADDED Requirements

### Requirement: Connectivity logic is isolated behind injected providers

All Tailscale verification, repair, fallback, and routing logic SHALL live in `internal/connectivity` and SHALL depend on its external systems only through injected interfaces (`AzureControl`, `tailscale.Client`) and a `CommandRunner` func seam. The package SHALL NOT use package-level mutable function variables or package-level mutable poll knobs as test seams.

#### Scenario: Behavior is tested through injected fakes
- **GIVEN** a `connectivity.Service` constructed with fake `AzureControl` and `tailscale.Client` implementations and a fast `PollConfig`
- **WHEN** any service method runs in a test
- **THEN** no global process state is mutated to redirect behavior, and the fakes record the calls the service made

#### Scenario: No import cycle with tailscale
- **GIVEN** `internal/connectivity` imports `internal/tailscale` for its `Peer`/`CleanupResult` types and `Client` interface
- **THEN** `internal/tailscale` does not import `internal/connectivity`

### Requirement: Local Tailscale readiness gate

`Service.Ready()` SHALL report ready only when a Tailscale credential is available (`TS_AUTHKEY` set or OAuth configured) AND a local peer lookup either succeeds or returns a peer-not-found error (a not-found peer is treated as "Tailscale is usable, the VM just hasn't joined yet"). Any other lookup error SHALL report not ready.

#### Scenario: No credentials configured
- **GIVEN** `TS_AUTHKEY` is unset and OAuth is not configured
- **THEN** `Ready()` returns false

#### Scenario: Credentials present and peer not yet joined
- **GIVEN** OAuth is configured and the peer lookup returns a peer-not-found error
- **THEN** `Ready()` returns true

### Requirement: Public-SSH restore prefers Tailscale re-auth over opening SSH

When public SSH is locked down, `Service.Restore(ctx)` SHALL first attempt Tailscale re-authentication. Only if re-auth does not yield a reachable peer SHALL it open public SSH as a fallback, mark the state public-SSH-open, and persist that state. When public SSH is not locked down, `Restore` SHALL be a no-op.

#### Scenario: Re-auth succeeds
- **GIVEN** public SSH is locked down and remote re-auth makes the peer reachable
- **WHEN** `Restore` runs
- **THEN** public SSH is NOT opened and the state remains public-SSH-closed

#### Scenario: Re-auth fails, fallback opens public SSH
- **GIVEN** public SSH is locked down and re-auth never makes the peer reachable
- **WHEN** `Restore` runs
- **THEN** public SSH is opened, the state is updated to public-SSH-open and saved
- **AND** if opening public SSH errors, `Restore` returns that error

### Requirement: Bounded remote re-authentication

`Service.Reauthenticate(ctx)` SHALL resolve an auth key (preferring `TS_AUTHKEY`, then OAuth-generated), run `tailscale up` inside the VM via Azure Run Command using shell-sanitized arguments, then poll local Tailscale for a reachable peer up to `Poll.Count` times waiting `Poll.Wait` between attempts. It SHALL stop early and return false if the context is cancelled, and return false if no auth key could be obtained.

#### Scenario: Context cancelled during poll
- **GIVEN** a re-auth in progress with a cancelled context
- **THEN** `Reauthenticate` returns false without exhausting the poll budget

#### Scenario: Auth-key characters are sanitized
- **GIVEN** an auth key containing characters outside the safe allowlist
- **THEN** the unsafe characters are stripped before the key is used and the user is warned

### Requirement: Connect repairs an online-but-unpingable peer before failing

`Service.Connect(ctx, extra...)` SHALL connect over Tailscale SSH when the peer is online and reachable. When the peer is online but not reachable on the data plane, it SHALL attempt re-authentication and, if that restores reachability, connect; otherwise it SHALL return an error. Peer-not-found, offline, and Tailscale-not-installed/not-running conditions SHALL each return their existing distinct error; the peer-not-found case additionally prints provisioning guidance, while the not-installed/not-running cases return the underlying error verbatim without extra guidance lines.

#### Scenario: Online but unpingable, repair succeeds
- **GIVEN** the peer is online but `tailscale ping` fails, and re-auth restores reachability
- **WHEN** `Connect` runs
- **THEN** it connects over Tailscale SSH to the admin user at the peer target

#### Scenario: Peer offline
- **GIVEN** the peer is in the tailnet but offline
- **THEN** `Connect` returns an "offline" error and does not attempt to connect

### Requirement: Command routing prefers Tailscale with public-SSH fallback

`Service.RunCommand(ctx, args)` SHALL require an existing, running VM. It SHALL run over Tailscale SSH when the peer is reachable; when the peer is online but unreachable and public SSH is locked down, it SHALL attempt connectivity restore and retry over Tailscale; otherwise it SHALL fall back to public SSH using the configured port, key, and admin user. The remote command's stdio SHALL stream to the terminal and its exit code SHALL propagate.

#### Scenario: Tailscale reachable
- **GIVEN** the peer is reachable
- **THEN** the command runs via `tailscale ssh user@target -- <cmd>`

#### Scenario: Tailscale online but unreachable with public SSH locked down
- **GIVEN** the peer is online but unreachable and public SSH is locked down
- **WHEN** `RunCommand` runs and restore makes the peer reachable
- **THEN** the command runs over Tailscale without opening public SSH

#### Scenario: Fallback to public SSH
- **GIVEN** no reachable Tailscale peer and a non-empty connection host
- **THEN** the command runs over public SSH with the configured port and private key
