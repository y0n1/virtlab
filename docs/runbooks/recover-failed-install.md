# SOP: Recover a failed install

> **Purpose:** resume and diagnose a `pulumi up` that failed during the image
> build, bootstrap, or first boot.
> **Risk:** medium — the async commands need manual retry; a wrong reset can
> discard a partially installed cluster.
> **Success:** a re-run of `pulumi up` completes, or the logs identify a
> prerequisite that must be fixed first.

## Prereqs
- A failed `pulumi up --stack demo` and its error message.
- SSH root access to the host.
- The host snapshot taken before the failed run (do not destroy without it).

## Steps

1. **Read the failure class** from the `pulumi up` error:

   - `PREFLIGHT FAIL` → a host prerequisite is missing or mismatched; fix it
     with [`install-host-tools.md`](install-host-tools.md) or [`store-pull-secret.md`](store-pull-secret.md).
   - image command failure → `openshift-install agent create image`.
   - install/bootstrap failure → `openshift-install agent wait-for
     install-complete`.
   - `install completion/pivot failed` → first boot or pivot.

2. **Re-run the update** (the command is resumable):

   ```bash
   pulumi up --stack demo
   ```

   `Expected:` artifacts under the install directory are reused and the failed
   step is retried. The resume guard adopts the cluster (kubeconfig plus
   `:6443/healthz` → smoke only) if it is already healthy.

3. **Read the install log** for an image or bootstrap failure:

   ```bash
   ssh root@<host> 'tail -n 80 /home/libvirt/demo/cluster/.openshift_install.log'
   ```

   `Expected:` the failing stage and its error.

4. **Read the first-boot fix log** for an early install or pivot failure:

   ```bash
   ssh root@<host> 'tail -n 80 /home/libvirt/demo/cluster/.bootstrap-fix.log'
   ```

   `Expected:` the script setting `overwrite: true` on the installed
   `/boot/ignition/config.ign` before first boot.

5. **Read the serial console** when the guest booted but never reached the
   installer:

   ```bash
   ssh root@<host> 'tail -n 80 /home/libvirt/demo/serial.log'
   ```

   `Expected:` boot progress or the kernel/agent error.

   `make test` tails the last two logs automatically when it
   fails.

6. **Escalate when a clean retry fails again** → [`reinstall-cluster.md`](reinstall-cluster.md); do
   not improvise a manual reset.

## Verify

- `pulumi up --stack demo` → completes with no errors.
- `make test` → node Ready and all operators available.
- `pulumi stack output apiUrl --stack demo` → the API URL is exported.
- `ssh root@<host> 'test -f /home/libvirt/demo/cluster/auth/kubeconfig'`
  → exit 0.

## Troubleshooting

- **`PREFLIGHT FAIL: openshift-install <version> required`** → wrong tool
  version; fix the host, not the pin.
- **`PREFLIGHT FAIL: pull-secret missing`** → [`store-pull-secret.md`](store-pull-secret.md).
- **The same command fails twice with no new log output** → the command may be
  pinned to a stale config; compare `pulumi preview` against the stack config.
- **Guest powers off unexpectedly** → someone ran `pulumi refresh` while the
  cluster was up; restart the guest and re-run the update, never refresh.
- **Not enough disk** → the install cannot resume cleanly; reclaim space and
  reinstall.

## Never do

- **Never delete the install directory or the pool while Pulumi owns the
  resources** — remove them through [`teardown-lab.md`](teardown-lab.md).
- **Never hand-run `openshift-install` or edit the rendered configs on the
  host** — the stack re-renders them.
- **Never destroy without the snapshot** taken before the failed run.
- **Never treat a `SKIP` in `verify-bootstrap` as a pass.**

## Related

- Bootstrap flow, preflight and logs: `internal/cluster/openshift.go`.
- Install tree and log paths: `config.yaml`
  (`lab.cluster.installDir`, `lab.vm.serialLogPath`).
- Acceptance: `Makefile`.
- Next: [`reinstall-cluster.md`](reinstall-cluster.md).
