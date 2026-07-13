# Design: Align shell checks across CI and just

## Current pattern

- `.github/workflows/ci.yml` has five jobs: `build`, `test`, `lint` (Go,
  golangci-lint), `shellcheck` (explicit file list), `bash-test`
  (`bash scripts/azure/test_common.sh`).
- The `justfile` provides `build`, `test`, `lint` mirroring the first three
  jobs; there is no local equivalent for the last two.
- The shellcheck list predates v0.7.0 and names: all `scripts/azure/*` entry
  points, `scripts/azure/common.sh`, `ansible/roles/dune/files/rover-halt`,
  and `install.sh`. `scripts/swapfile` (added in `4de8dce`, v0.7.0) is missing.

## Target convention

One authoritative shell-lint target list, exercised identically by CI and by a
local `just` recipe; every CI quality gate is locally reproducible via just.

## Transformation rules

- Add a `shellcheck` recipe to `justfile` that runs shellcheck over the full
  script list including `scripts/swapfile`. Keep the existing exclusion
  (`test_common.sh` is sourced, not standalone) and carry over the explanatory
  comment.
- Add a `bash-test` recipe running `bash scripts/azure/test_common.sh`.
- Update the CI `shellcheck` and `bash-test` jobs to run the just recipes
  (the CI jobs already install just via `extractions/setup-just@v2` in other
  jobs; add that step) — or, if keeping CI standalone is preferred, update the
  CI list to include `scripts/swapfile` and add a comment in both files noting
  they must stay in sync. Prefer the single-source-of-truth option.
- Append the two recipes to the Development snippet in `README.md`
  (around lines 536-541).

## Files and exclusions

- `justfile` — add recipes only; do not reorder or rename existing recipes.
- `.github/workflows/ci.yml` — touch only the `shellcheck`/`bash-test` jobs.
- `README.md` — Development section only.
- Do not modify any file under `scripts/` or `ansible/` — the scripts are
  already clean and behavior must not change.

## Verification strategy

- `just shellcheck` exits 0 (shellcheck 0.9.0 verified clean on all listed
  scripts at `ed145df`, including `scripts/swapfile`).
- `just bash-test` prints `PASSED: ... tests passed`.
- `just --list` shows the new recipes with one-line docs.
- Diff review: CI job commands and recipe commands are identical or CI invokes
  the recipe.

## Stop conditions

- shellcheck reports any finding on `scripts/swapfile` or any other script
  with the CI-pinned or local shellcheck version: stop and report instead of
  adding directives or "fixing" scripts — script edits are out of scope.
- If the CI runner's shellcheck version would differ from what the recipe
  installs/assumes and produces different results, stop and surface the
  version question rather than pinning ad hoc.
