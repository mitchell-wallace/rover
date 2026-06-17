# Rover Release

This skill documents the release process for the `rover` project.

## Workflow

1. **Verify locally:**
   ```
   just lint
   just test
   just build
   ```

2. **Check if bump is needed:** If `VERSION` contents already exceed the latest tag (e.g. `0.6.0` vs `v0.5.6`), skip the bump — it was already done.

3. **Bump version** (patch by default) if needed:
   ```
   echo "0.X.Y" > VERSION
   ```

4. **Commit and push to main:**
   ```
   git add VERSION
   git commit -m "chore: release vX.Y.Z"
   git push origin main
   ```

5. **Monitor CI:** `auto-tag.yml` detects VERSION bump, creates tag `vX.Y.Z`, dispatches `release.yml` which runs GoReleaser. Check workflow runs and fix any failures.

## Notes

- Single source of truth for version is `./VERSION`; GoReleaser derives `{{.Version}}` from the Git tag.
- `just build` injects version via ldflags from `VERSION` file.
- To redo a release: delete tag locally and on origin, increment `VERSION` again, re-push.
- Push directly to `main` (no PR workflow for this repo).
