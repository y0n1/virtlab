# SOP: Reinstall the cluster

> **Purpose:** deliberately reproduce the cluster from scratch, or re-fire the
> image and bootstrap after a configuration change.
> **Risk:** high — the from-scratch path deletes the running cluster and its
> disk before rebuilding it.
> **Success:** a fresh install passes the acceptance gate and `oc get nodes`
> reports `master-0` Ready again.

## Prereqs
- [`bring-up-lab.md`](bring-up-lab.md) has completed at least once.
- A VMware snapshot of the host VM taken immediately before this procedure.
- [`teardown-lab.md`](teardown-lab.md) and [`bring-up-lab.md`](bring-up-lab.md) available for the from-scratch path.

## Steps

1. **Snapshot the host VM** in VMware and note the name.

   `Expected:` the snapshot completes.

2. **Decide which path you need:**

   - **Config change (in place):** editing the rendered configs or the OCP
     version re-fires `cluster-image` → `cluster-bootstrap`; `up` reinstalls
     over the existing guest.
   - **Preset change (from scratch):** switching between `sno` and
     `compact`/`ha` changes the installer `platform` (`none` ↔ `baremetal`),
     which is immutable. Use the from-scratch path.
   - **Reproduce from scratch:** the resume guard adopts a healthy cluster, so
     a no-op `up` never resets it; you must remove the cluster first.

3. **Config-change path:**

   ```bash
   pulumi preview --stack demo
   pulumi up --stack demo
   ```

   `Expected:` the preview shows `cluster-image` and `cluster-bootstrap`
   replaced or updated; `up` re-runs them.

4. **From-scratch path — remove the lab, including host artifacts:**

   Follow [`teardown-lab.md`](teardown-lab.md) (it also removes the install tree and the host DNS
   bridge, which `pulumi destroy` alone leaves behind).

   `Expected:` no domain, network or pool; install tree gone.

5. **Rebuild:**

   Follow [`bring-up-lab.md`](bring-up-lab.md) from step 2 (the snapshot already exists).

   `Expected:` a fresh image build and install, then the acceptance gate.

6. **Re-establish laptop access:**

   The old kubeconfig and admin password were removed with the install tree;
   repeat [`access-cluster.md`](access-cluster.md).

## Verify

- `make test` → all
  pass.
- `oc get nodes` → `master-0` `Ready`.
- `oc get clusterversion` → the pinned version (or the version you changed
  to).
- `pulumi stack output installDir` → repopulated with the ISO retained.

## Troubleshooting

- **`up` finishes without reinstalling** → the resume guard adopted the
  healthy cluster; use the from-scratch path (step 4).
- **`pulumi destroy` hangs or is interrupted** → `pulumi cancel --stack
  demo`, then re-run it; never hand-delete libvirt objects while it
  runs.
- **`up` fails after the teardown** → [`recover-failed-install.md`](recover-failed-install.md); the
  install tree is empty, so recovery starts from the image build.
- **Old cluster artifacts reappear** → you skipped [`teardown-lab.md`](teardown-lab.md); a bare
  `pulumi destroy` does not remove `/home/libvirt/demo`.
- **`virtlab up` refuses to start with a platform mismatch** → the live cluster's
  `platform` differs from what the current preset installs (`None` for `sno`,
  `BareMetal` for `compact`/`ha`). `platform` is immutable, so this is the
  from-scratch path: `virtlab down`, then `virtlab up`.

## Never do

- **Never destroy without a snapshot** — it is the only rollback.
- **Never run `pulumi refresh` while the cluster is up** — refresh plus `up`
  powers the VM off.
- **Never `virsh destroy` the guest during an update** unless recovering.
- **Never delete the stock `default` libvirt network or pool** — unrelated to
  the lab.
- **Never hand-run `openshift-install`** to force a reinstall; change the
  stack or use the documented teardown.
- **Never expect a preset change to apply in place** — `sno` ↔ `compact`/`ha`
  changes `platform`, which the installer refuses to change on an existing
  cluster; tear down first.

## Related

- Teardown and rebuild: [`teardown-lab.md`](teardown-lab.md), [`bring-up-lab.md`](bring-up-lab.md).
- Command keying: `internal/cluster/openshift.go`,
  `internal/host`.
- Recovery: [`recover-failed-install.md`](recover-failed-install.md).
