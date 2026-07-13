# Tasks

## 1. Confirm the pattern

- [ ] 1.1 Inspect `.github/workflows/ci.yml` (`shellcheck` and `bash-test`
      jobs) and the `justfile`; confirm the shellcheck list still omits
      `scripts/swapfile` and no just recipe covers either job.
- [ ] 1.2 Run `shellcheck scripts/swapfile` and
      `bash scripts/azure/test_common.sh` at the current commit; both must
      pass before wiring them into gates.

## 2. Pilot the transformation

- [ ] 2.1 Add the `shellcheck` recipe to `justfile` covering:
      `scripts/azure/up down status ip ssh ssh-access disk restart common.sh`,
      `scripts/swapfile`, `ansible/roles/dune/files/rover-halt`, `install.sh`.
      Exclude `scripts/azure/test_common.sh` (sourced helper, keep the comment
      explaining why).
- [ ] 2.2 Add the `bash-test` recipe (`bash scripts/azure/test_common.sh`).
- [ ] 2.3 Run `just shellcheck` and `just bash-test`; both exit 0.

## 3. Apply across the scoped set

- [ ] 3.1 Point the CI `shellcheck` job at the recipe (add
      `extractions/setup-just@v2`, run `just shellcheck`) or add
      `scripts/swapfile` to the inline list with a keep-in-sync comment;
      do the same choice consistently for `bash-test`.
- [ ] 3.2 Add the two recipes to README's Development snippet.
- [ ] 3.3 Keep unrelated cleanup (script edits, other CI jobs) out of the diff.

## 4. Verify

- [ ] 4.1 Run `just --list`, `just shellcheck`, `just bash-test`, `just test`;
      expect all to succeed.
- [ ] 4.2 Review the diff: no changes under `scripts/`, `ansible/`, or to Go
      sources; CI and just commands are in lockstep.
