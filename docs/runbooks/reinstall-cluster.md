# SOP: Reinstall the cluster

> **Purpose:** deliberately rebuild the cluster from scratch after a preset,
> version, or topology change.
> **Risk:** high — deletes the running cluster and its disk before rebuilding
> it.
> **Success:** a fresh `virtlab up` finishes with the access banner and
> `oc get nodes` reports every node `Ready`.

## Prereqs
- [`bring-up-lab.md`](bring-up-lab.md) completed at least once.
- A VMware snapshot of the host VM taken immediately before this procedure.
- `config.yaml` edited for the target `preset`, `ocpVersion`, or overrides.

## Steps

1. **Snapshot the host VM** in VMware and note the name.

   `Expected:` the snapshot completes.

2. **Confirm the change cannot apply in place:**

   - `sno` ↔ `compact`/`ha` changes the installer `platform`
     (`None` ↔ `BareMetal`), which is immutable.
   - `ocpVersion` and rendered manifests are not re-applied by `virtlab up`
     while the cluster is healthy. `virtlab up` stops with a mismatch error
     instead.
   - VM sizing and count changes still require a fresh install to converge.

   `Expected:` any of the above means the from-scratch path below.

3. **Tear the lab down completely:**

   ```bash
   virtlab down -y
   ```

   `Expected:` every stage succeeds; see [`teardown-lab.md`](teardown-lab.md)
   to verify the host and workstation are clean.

4. **Rebuild from the updated configuration:**

   ```bash
   virtlab up
   ```

   `Expected:` Phase 2 recreates the VMs and Phase 3 performs a fresh image
   build and install; the run ends with the access banner.

## Verify

- `virtlab status` → tunnel active, host reachable, kubeconfig and password
  found.
- `export KUBECONFIG=~/.kube/config.<clusterName>` then `oc get nodes` →
  every node `Ready`.
- `oc get clusterversion` → the version you changed to.
- `oc get infrastructure cluster -o jsonpath='{.status.platform}'` →
  `None` for `sno`, `BareMetal` for `compact`/`ha`.

## Troubleshooting

- **`up` fails with `config.yaml no longer matches its applied declaration`**
  → the cluster is healthy but the declaration changed; you skipped step 3.
  Run `virtlab down -y` first.
- **`platform is immutable after install`** → same cause; the from-scratch
  path is required.
- **Teardown failed** → fix the reported stage and re-run `virtlab down`
  before running `virtlab up`.
- **Old artifacts reappear** → the host teardown stage did not finish; check
  `/home/libvirt/<clusterName>` and the ingress LB unit.
- **`up` fails after the teardown** →
  [`recover-failed-install.md`](recover-failed-install.md); the install tree
  is empty, so recovery starts at the image build.

## Never do

- **Never destroy without a snapshot** — it is the only rollback.
- **Never expect a preset change to apply in place** — `platform` is
  immutable; tear down first.
- **Never hand-run `openshift-install`** to force a reinstall; change the
  configuration and use the documented teardown.
- **Never delete the stock `default` libvirt network or pool** — unrelated to
  the lab.

## Related

- Teardown and rebuild: [`teardown-lab.md`](teardown-lab.md),
  [`bring-up-lab.md`](bring-up-lab.md).
- Platform guard: `internal/cluster/platform.go`.
- Resume guard: `internal/cluster/openshift.go`.
