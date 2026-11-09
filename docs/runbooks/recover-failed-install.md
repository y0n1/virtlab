# SOP: Recover a failed install

> **Purpose:** resume and diagnose a `virtlab up` that failed during the
> image build, bootstrap, or first boot.
> **Risk:** medium — a wrong reset can discard a partially installed cluster;
> the resume path is safe.
> **Success:** a re-run of `virtlab up` completes with the access banner, or
> the logs identify a prerequisite that must be fixed first.

## Prereqs
- A failed `virtlab up` and its error message.
- SSH root access to the host.
- The host snapshot taken before the failed run (do not tear down without it).

## Steps

1. **Read the failure class** from the phase prefix in the `virtlab up` error:

   - `phase 1 (workstation)` → missing `ssh`/`go`/pull secret, or a tool
     install failure.
   - `phase 1 (host)` → host packages, services, or toolchain install failed;
     see [`prepare-host-vm.md`](prepare-host-vm.md) and
     [`install-host-tools.md`](install-host-tools.md).
   - `phase 2` → libvirt/Pulumi resource creation failed; check SSH to the
     host and free disk in `/home/libvirt`.
   - `phase 3` → Agent ISO build, node boot, or install wait failed; continue
     with the logs below.
   - `phase 4` → split-DNS, sshuttle, or endpoint verification failed; run
     `virtlab status` first.

2. **Re-run the update** (the command is resumable):

   ```bash
   virtlab up
   ```

   `Expected:` artifacts under the install directory are reused and the
   failed step is retried. If the cluster is already healthy and its applied
   declaration still matches `config.yaml`, the resume guard adopts it and
   skips installation. A mismatch stops the run; see
   [`reinstall-cluster.md`](reinstall-cluster.md).

3. **Read the installer log** for an image or bootstrap failure:

   ```bash
   ssh root@<host> 'tail -n 80 /home/libvirt/<clusterName>/installer/.openshift_install.log'
   ```

   `Expected:` the failing stage and its error. `virtlab up` also streams this
   log while Phase 3 runs.

4. **Read the serial console** when a node booted but never reached the
   installer:

   ```bash
   ssh root@<host> 'ls /home/libvirt/<clusterName>/logs/ && \
     tail -n 80 /home/libvirt/<clusterName>/logs/master-0-serial.log'
   ```

   `Expected:` boot progress or the kernel/agent error.

5. **Escalate when a clean retry fails again** →
   [`reinstall-cluster.md`](reinstall-cluster.md); do not improvise a manual
   reset.

## Verify

- `virtlab up` → completes with the `LAB VERIFICATION SUCCESSFUL` banner.
- `virtlab status` → tunnel active, host reachable, kubeconfig found.
- `export KUBECONFIG=~/.kube/config.<clusterName>` then `oc get nodes` →
  every node `Ready`.
- `ssh root@<host> 'test -f /home/libvirt/<clusterName>/installer/auth/kubeconfig'`
  → exit 0.

## Troubleshooting

- **Phase 1 reports missing or outdated tools** → fix the named user-managed
  dependency; `sshuttle`/`pulumi`/`ocm` are reinstalled automatically.
- **`PREFLIGHT`/tool version mismatch** → fix the host, not the version pin;
  see [`install-host-tools.md`](install-host-tools.md).
- **The same step fails twice with no new log output** → compare the rendered
  manifests for the failure with the current `config.yaml`; a healthy cluster
  short-circuits Phase 3.
- **Node booted but the install never started** → read the serial log from
  step 4; the Agent ISO or boot order is wrong.
- **Not enough disk** → the install cannot resume cleanly; reclaim space and
  reinstall ([`reinstall-cluster.md`](reinstall-cluster.md)).

## Never do

- **Never delete the install directory or the pool while Pulumi owns the
  resources** — remove them through [`teardown-lab.md`](teardown-lab.md).
- **Never hand-run `openshift-install` or edit the rendered manifests on the
  host** — `virtlab up` re-renders them.
- **Never tear down without the snapshot** taken before the failed run.
- **Never treat a skipped install as a failure** — the resume guard adopted a
  healthy cluster that still matches `config.yaml`.

## Related

- Bootstrap flow, resume guard, and logs: `internal/cluster/openshift.go`.
- Install tree: `/home/libvirt/<clusterName>/installer`; serial logs:
  `/home/libvirt/<clusterName>/logs` (`internal/config/config.go`).
- Next: [`reinstall-cluster.md`](reinstall-cluster.md).
