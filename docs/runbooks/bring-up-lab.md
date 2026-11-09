# SOP: Bring up the lab

> **Purpose:** provision or reconcile the lab (storage pool, network, guest
> VMs, OpenShift install) from `config.yaml` with one command.
> **Risk:** high — creates or updates a running cluster and a large disk; a
> wrong `clusterName` or host targets the wrong lab.
> **Success:** `virtlab up` exits 0 and prints the access banner with the
> console URL, kubeconfig path, and kubeadmin password path.

## Prereqs
- [`prepare-laptop.md`](prepare-laptop.md) complete (`config.yaml` valid).
- Host prepared and reachable: [`prepare-host-vm.md`](prepare-host-vm.md),
  [`grant-laptop-host-access.md`](grant-laptop-host-access.md), and
  [`store-pull-secret.md`](store-pull-secret.md).
- A VMware snapshot of the host VM — the rollback if the install goes wrong.

## Steps

1. **Snapshot the host VM** in VMware and note the snapshot name.

   `Expected:` the snapshot completes before any lab command runs.

2. **Run the offline checks** (source checkout only):

   ```bash
   make check
   ```

   `Expected:` build, vet, and unit tests pass; no host access is needed.

3. **Apply the configuration** from the directory holding `config.yaml`:

   ```bash
   virtlab up
   ```

   `Expected:` four phases run in order:

   - Phase 1 verifies `ssh`/`go`, installs missing `sshuttle`/`pulumi`/`ocm`,
     prepares the host, and installs matching `openshift-install`/`oc`;
   - Phase 2 creates the storage pool, network, and guest VMs;
   - Phase 3 builds the Agent ISO, boots the nodes, and waits for install;
   - Phase 4 configures split-DNS and sshuttle, probes DNS/API/console, and
     writes `~/.kube/config.<clusterName>` and
     `~/.kube/kubeadmin-password.<clusterName>`.

   Use `virtlab up -y` to skip confirmation prompts.

4. **Confirm the lab state:**

   ```bash
   virtlab status
   ```

   `Expected:` tunnel active, host reachable, kubeconfig and kubeadmin
   password found.

## Verify

- `virtlab up` → final banner `🎉 LAB VERIFICATION SUCCESSFUL!`.
- `export KUBECONFIG=~/.kube/config.<clusterName>` then `oc get nodes` →
  every node `Ready`.
- `oc get clusterversion` → the configured `ocpVersion`.

## Troubleshooting

- **`platform is immutable after install`** → the live cluster was installed
  with a different preset; run `virtlab down` then `virtlab up`
  ([`reinstall-cluster.md`](reinstall-cluster.md)).
- **`co-existing lab ... detected`** → another lab is active on the host or
  workstation; tear it down first.
- **Phase 3 fails during image build or bootstrap** →
  [`recover-failed-install.md`](recover-failed-install.md).
- **`up` skips installation because the cluster is already healthy** → the
  resume guard adopted it; this is expected. A healthy cluster is not
  reinstalled when config or version changes.
- **A re-run after an interruption** → install artifacts are reused; re-run
  `virtlab up`.

## Never do

- **Never run `virtlab down` without a host snapshot** — it deletes the
  cluster disk and there is no undo.
- **Never hand-run `openshift-install` or edit rendered manifests on the
  host** — `virtlab up` owns them.
- **Never modify Pulumi stack state out of band** — the Automation API state
  is the source of truth.

## Related

- Command flow and phases: `cmd/virtlab/main.go`.
- Configuration: `config.yaml`, `examples/config.example.yaml`.
- Recovery: [`recover-failed-install.md`](recover-failed-install.md),
  [`reinstall-cluster.md`](reinstall-cluster.md).
