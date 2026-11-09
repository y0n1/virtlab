# SOP: Install the host toolchain

> **Purpose:** ensure the host carries the pinned OpenShift toolchain the
> cluster module preflights, and that the host can reach the registry.
> **Automation Note:** `virtlab up` automatically checks and installs the
> matching `openshift-install` and `oc` binaries matching the remote host's CPU
> architecture and `ocpVersion` from `config.yaml` during Phase 1 Host Preparation.
> This runbook documents the manual verification and fallback procedure.
> **Risk:** low — no lab state changes; a version mismatch fails the image
> build before anything is created.
> **Success:** `openshift-install` reports the pinned version, `oc` matches
> it, and `https://quay.io` answers `200`.

## Prereqs
- Root shell on a host prepared by [`prepare-host-vm.md`](prepare-host-vm.md).
- `virtlab up` installs `openshift-install` and `oc` for the configured
  `ocpVersion` during Phase 1 Host Preparation. This SOP is the manual
  verification and fallback procedure when that install fails.

## Steps

1. **Check `openshift-install`:**

   ```bash
   openshift-install version
   ```

   `Expected:` first line prints `openshift-install <pinned version>`; any
   other version fails the bootstrap preflight.

2. **Check `oc`:**

   ```bash
   oc version --client
   ```

   `Expected:` the same pinned version (the host smoke uses `oc`).

3. **Check registry reachability** (the image build pulls from quay):

   ```bash
   curl -sI -o /dev/null -w '%{http_code}\n' https://quay.io
   ```

   `Expected:` `200`.

4. **Confirm both tools are on the non-interactive PATH** used over SSH:

   ```bash
   ssh root@<host> 'command -v openshift-install && command -v oc'
   ```

   `Expected:` two absolute paths.

The version pin is defined once in `internal/config/config.go` and rendered into
the bootstrap script; do not edit the script or a hand-copied version.

## Verify

- `openshift-install version | head -1` → matches the pin.
- `oc version --client` → matches the pin.
- `curl -sI https://quay.io` → `200`.

## Troubleshooting

- **Version mismatch** → replace the tools with the pinned release, not by
  editing the pin; a mismatch is an intentional hard stop.
- **`command not found` over SSH** → the binary lives outside the
  non-interactive PATH; install it under `/usr/local/bin` or symlink it there.
- **quay returns `403`/timeout** → host egress or proxy problem; the image
  build cannot proceed.
- **Tools installed but the preflight still fails** → re-run `virtlab up`;
  the Phase 1 preflight message names the failing check.

## Never do

- **Never change the pinned version in code to match an installed tool** — the
  pin is the contract; replace the tool instead.
- **Never place the tools in the repo or in the Pulumi program** — they are
  host prerequisites.
- **Never skip the quay check** — it fails cheaply now and expensively during
  the image build.

## Related

- Pin: `internal/config/config.go` (`ocpVersion`) and
  `config.yaml` (`ocpVersion`).
- Preflight: `internal/cluster/openshift.go`.
- Next: [`store-pull-secret.md`](store-pull-secret.md).
