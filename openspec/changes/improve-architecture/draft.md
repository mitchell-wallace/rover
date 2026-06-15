# Improve Architecture Draft

## Goal

Normalize Rover around smaller, clearer modules with consistent boundaries,
minimal entry points, and tests that are easier to navigate and maintain.

The immediate pressure point is `internal/cmd/actions_test.go`, which has grown
past 1600 lines and mixes command behavior, Azure doubles, Tailscale doubles,
recovery scenarios, command execution, and helper validation in one file.

## Principles

- Keep command entry points thin.
- Keep provider boundaries explicit and narrow.
- Keep side effects behind interfaces that match real workflows.
- Prefer behavior-oriented packages over one large command action file.
- Split tests by behavior and dependency boundary.
- Avoid broad abstractions until there are at least two real callers or a clear
  testing/maintainability win.
- Preserve the current single-VM MVP scope.

## Proposed Shape

### Command Layer

Keep Cobra commands as minimal adapters:

- parse flags and args
- load context
- call one application service
- format top-level errors

Avoid adding provider logic, retries, polling, or repair flows directly in Cobra
files.

### Application Services

Move behavior out of the current large action file into focused services:

- `vm.Service`
  - up/start/restart/down/disk/status workflows
  - Azure power-state and lifecycle decisions
- `provision.Service`
  - Ansible provisioning
  - boot/SSH wait logic
  - post-provision validation
- `connectivity.Service`
  - Tailscale peer lookup
  - data-plane checks
  - remote Tailscale re-auth
  - public SSH fallback decisions
- `diagnostics.Service`
  - local health checks
  - optional guest health collection through Azure Run Command

### Provider Boundaries

Keep provider interfaces small and workflow-shaped:

- Azure provider:
  - status/info
  - up/down/restart/resize
  - set public SSH
  - run command
- Tailscale provider:
  - local status
  - find peer
  - ping peer
  - connect SSH
  - generate auth key
  - cleanup devices
- Provisioner:
  - run Ansible with explicit params

The goal is to make dependencies obvious without creating a generic cloud
provider abstraction prematurely.

## Test Plan Refactor

### Split Large Test Files

Split `internal/cmd/actions_test.go` by behavior:

- `connectivity_test.go`
- `provision_test.go`
- `vm_lifecycle_test.go`
- `command_test.go`
- `disk_test.go`
- `tailscale_cleanup_test.go`
- `sanitize_test.go`

Move shared test doubles and helpers into one small test helper file.

### Test Double Policy

- Use explicit fakes for Azure and Tailscale boundaries.
- Keep fake state transitions visible in each test.
- Avoid global function overrides where a service-local interface would be
  clearer.
- Prefer table tests for pure validation and scenario tests for workflows.

### Coverage Targets

Maintain or improve coverage for:

- public SSH lockdown and restore behavior
- online-but-unpingable Tailscale repair
- no-credential fallback paths
- Azure lifecycle edge cases
- Ansible provisioning failure paths
- command routing between Tailscale and public SSH

## Migration Plan

1. Inventory current command actions and group them by workflow.
2. Introduce `connectivity.Service` around existing Tailscale repair/connect
   logic without changing behavior.
3. Move existing connectivity tests into a dedicated file and replace global
   overrides with fake providers where practical.
4. Introduce `vm.Service` for lifecycle actions while preserving the existing
   Cobra surface.
5. Move provisioning workflow into `provision.Service`.
6. Add diagnostics as a separate service instead of folding more health logic
   into command actions.
7. Remove dead helpers and consolidate duplicated polling/fallback code.

## Non-Goals

- Multi-cloud provider abstraction.
- Multi-VM orchestration.
- Background daemons.
- Large CLI redesign.
- Rewriting Azure shell scripts during the first architecture pass.

## Open Questions

- Should services live under `internal/app/*`, or should they be top-level
  domain packages like `internal/connectivity` and `internal/vm`?
- Should existing `internal/azure` remain script-backed only, or should it gain
  typed errors before any SDK migration?
- Should `rover diagnose` be part of the architecture change, or a separate
  reliability change after the boundaries are clearer?
- What is the acceptable maximum size for individual test files and service
  files?
