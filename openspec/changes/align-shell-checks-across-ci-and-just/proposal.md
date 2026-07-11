# Proposal: Align shell checks across CI and just

## Intent

CI treats shell quality as a first-class gate (dedicated `shellcheck` and
`bash-test` jobs in `.github/workflows/ci.yml`), but two gaps opened up as
v0.6.0–v0.7.0 features landed quickly. First, the CI shellcheck job uses a
hardcoded file list that was not updated when `scripts/swapfile` shipped in
v0.7.0 — a script Ansible installs on every VM as `/usr/local/sbin/rover-swapfile`
is the only shipped shell script with no lint gate. Second, the `justfile`
(the documented local workflow: `just test`, `just lint`) has no recipe for
either shell job, so contributors cannot reproduce the CI shell gates locally
before pushing. This batch closes both gaps without changing any script.

## Observed convention

- `.github/workflows/ci.yml` `shellcheck` job lints every shipped shell entry
  point by explicit path, with a comment explaining inclusion/exclusion
  rationale ("All Azure scripts (extensionless ones carry shebangs) plus
  install.sh. Exclude test_common.sh which is not a standalone script").
- Scripts themselves are shellcheck-clean and use targeted directives where
  needed (`scripts/azure/ssh` line 39: `# shellcheck disable=SC2016` with a
  reason; `558c7af` fixed exactly this class of issue).
- The `justfile` mirrors CI's Go jobs one-to-one: `just build`/`just test`/
  `just lint` correspond to the `build`/`test`/`lint` CI jobs, and README's
  Development section documents the recipes.

## Representative evidence

- `.github/workflows/ci.yml:47` — hardcoded shellcheck list ends at
  `scripts/azure/common.sh ansible/roles/dune/files/rover-halt install.sh`;
  `scripts/swapfile` is absent.
- `scripts/swapfile:1` — `#!/usr/bin/env bash` + `set -euo pipefail`; a shipped
  script (installed by `ansible/roles/swapfile/tasks/main.yml:4-5`) with no
  lint coverage.
- `justfile` — recipes `build`, `test`, `fmt`, `lint`, `clean`; no shellcheck
  or bash-test recipe, while CI has both jobs.
- `.github/workflows/ci.yml:56-61` — `bash-test` job runs
  `bash scripts/azure/test_common.sh`; not reproducible via any just recipe.
- `README.md:536-541` — Development section lists `just test` / `just lint`
  only; a contributor following it never runs the shell gates.

## Batch rule

Every shipped shell script (a file with a shell shebang that is executed on a
user machine or VM) appears in exactly one shellcheck target list, and every
CI quality job has a same-named local `just` recipe that runs the same command.
Concretely: add `scripts/swapfile` to the CI shellcheck list; add
`just shellcheck` and `just bash-test` recipes whose commands match the CI
jobs; have CI invoke the just recipes (or keep commands byte-identical) so the
list lives in one place; mention the new recipes in README's Development
section.

## Scope

In scope:
- `.github/workflows/ci.yml` (shellcheck job file list; optionally switch the
  shellcheck/bash-test jobs to call the new just recipes)
- `justfile` (new `shellcheck` and `bash-test` recipes)
- `README.md` Development section (one or two lines listing the new recipes)

Out of scope:
- Any edit to the shell scripts themselves (they are already shellcheck-clean;
  verified with shellcheck 0.9.0)
- Adding new shellcheck directives, changing severity, or adding `.shellcheckrc`
- Go lint configuration, golangci-lint versions, or other CI jobs
- Ansible task changes

## Safety and validation

No runtime code changes. `shellcheck scripts/swapfile` already exits 0 (checked
against shellcheck 0.9.0 at commit `ed145df`), so adding it to CI cannot break
the build. Validation: run `just shellcheck` and `just bash-test` locally and
confirm both pass; confirm the CI job commands are identical to (or invoke) the
recipes; `just --list` shows the new recipes.

## Spec impact

Behavior-preserving tooling tidy-up; no product spec delta expected.
