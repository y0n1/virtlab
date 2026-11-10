# SOP: Release virtlab

> **Purpose:** Build, package, and publish a new semantic version of the `virtlab` CLI to GitHub Releases using automated multi-platform GoReleaser workflows.
> **Risk:** low — Tagging and publishing releases does not alter running libvirt or cluster infrastructure.
> **Success:** GitHub Actions Release workflow finishes green; GitHub Releases displays the new tag `vX.Y.Z` with release notes, `.tar.gz` packages for `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`, and `checksums.txt`.

## Prereqs
- Git write access to the `y0n1/virtlab` repository.
- GitHub CLI (`gh`) installed and authenticated.
- Local repository on `main` branch with clean working tree.

## Steps

1. **Verify local codebase health:**
   Ensure all tests, linting, and compilation checks pass locally before tagging:

   ```bash
   make check
   ```

   `Expected:` Clean build, vet passes, and all unit tests pass without error.

2. **Optional: Test dry-run packaging snapshot:**
   Run GoReleaser in snapshot mode to verify packaging locally without publishing:

   ```bash
   make release-snapshot
   ```

   `Expected:` Builds all 4 architecture targets into `dist/` and generates `dist/checksums.txt`.

3. **Determine the semantic version bump:**
   Follow Semantic Versioning (`vMAJOR.MINOR.PATCH`):
   - **Patch** (`v0.1.1`): Bug fixes, documentation, internal refactoring.
   - **Minor** (`v0.2.0`): New features, CLI flags, preset additions.
   - **Major** (`v1.0.0`): Breaking changes to configuration format or CLI semantics.

4. **Create and annotate the git release tag:**

   ```bash
   VERSION="v0.1.0"
   git tag -a "$VERSION" -m "Release $VERSION"
   ```

   `Expected:` Annotated tag created pointing to the HEAD commit.

5. **Push the tag to GitHub:**

   ```bash
   git push origin "$VERSION"
   ```

   `Expected:` Git pushes tag to `origin`. This triggers the GitHub Actions `.github/workflows/release.yml` pipeline.

6. **Monitor the automated release pipeline:**

   ```bash
   gh run watch
   ```

   `Expected:` The `Verify Tests and Lint` job passes, followed by the `Publish Release` job running GoReleaser to publish the GitHub release.

## Verify

1. **Inspect published release:**

   ```bash
   gh release view "$VERSION"
   ```

   `Expected:` Release title matches `$VERSION`, changelog displays grouped commit notes, and assets include:
   - `virtlab_<version>_darwin_arm64.tar.gz` and `virtlab_darwin_arm64.tar.gz`
   - `virtlab_<version>_darwin_amd64.tar.gz` and `virtlab_darwin_amd64.tar.gz`
   - `virtlab_<version>_linux_arm64.tar.gz` and `virtlab_linux_arm64.tar.gz`
   - `virtlab_<version>_linux_amd64.tar.gz` and `virtlab_linux_amd64.tar.gz`
   - `checksums.txt`

2. **Verify downloaded asset:**

   ```bash
   gh release download "$VERSION" -p "*darwin_arm64.tar.gz" -D /tmp/test-release
   tar -xzf /tmp/test-release/*.tar.gz -C /tmp/test-release
   /tmp/test-release/virtlab version
   ```

   `Expected:` Executable outputs `virtlab version vX.Y.Z (commit: <sha>, built: <date>, ...)`.

3. **Verify build provenance attestation:**

   ```bash
   gh attestation verify /tmp/test-release/virtlab_<version>_darwin_arm64.tar.gz --owner y0n1
   ```

   `Expected:` Attestation verification succeeds against repository `y0n1/virtlab`.

## Troubleshooting

- **Release workflow fails on pre-release verification:**
  Check workflow logs:
  ```bash
  gh run view --log-failed
  ```
  Fix the underlying test or linting issue on `main`, delete the remote tag, and re-tag once fixed.

- **Rollback a faulty release tag:**
  If a release was published accidentally or needs retraction:
  ```bash
  TAG="vX.Y.Z"
  gh release delete "$TAG" --yes
  git push origin ":refs/tags/$TAG"
  git tag -d "$TAG"
  ```
  `Expected:` Remote release and tag removed.

## Never do
- Never push a release tag from a dirty working tree or unverified branch.
- Never manually edit or delete assets in GitHub Releases without updating or deleting the corresponding git tag.

## Related
- `.goreleaser.yaml` — Multi-platform compilation and packaging definitions.
- `.github/workflows/release.yml` — Automated release CI workflow.
- `.github/workflows/ci.yml` — PR and commit verification pipeline.
- [`../../AGENTS.md`](../../AGENTS.md) — Production pipeline guidelines.
