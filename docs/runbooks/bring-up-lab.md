# SOP: Bring up the lab

> **Purpose:** create or update the Pulumi stack so the guest boots and the
> single-node cluster installs.
> **Risk:** high — an `up` against the wrong stack or config builds or
> reconciles the wrong lab; the install writes a large disk.
> **Success:** `pulumi up` completes and every `make test` acceptance
> target passes.

## Prereqs
- [`prepare-host-vm.md`](prepare-host-vm.md),
  [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md),
  [`grant-laptop-host-access.md`](grant-laptop-host-access.md),
  [`install-host-tools.md`](install-host-tools.md),
  [`store-pull-secret.md`](store-pull-secret.md) complete.
- [`prepare-laptop.md`](prepare-laptop.md) and [`configure-laptop-dns.md`](configure-laptop-dns.md) complete.
- A VMware snapshot of the host VM — the rollback if the install goes wrong.

## Steps

1. **Snapshot the host VM** in VMware and note the snapshot name.

   `Expected:` the snapshot completes before any stack command runs.

2. **Run the offline checks** from the repo root:

   ```bash
   make check
   ```

   `Expected:` build, vet, tests, goldens and self-checks pass; no host access
   is needed.

3. **Review the plan:**

   ```bash
   pulumi preview --stack demo
   ```

   `Expected:` the network, the guest (pool, volume, domain), the DNS bridge
   and the cluster resources. Stop if it proposes deleting anything
   unexpected.

4. **Apply:**

   ```bash
   pulumi up --stack demo
   ```

   `Expected:` the image build, boot, install and smoke commands succeed; the
   stack exports `apiUrl`, `consoleUrl`, `installDir`, `kubeconfigPath`,
   `kubeadminPasswordPath`, `networkBridge` and `serialLogPath`.

The module is resumable: if the update is interrupted, the next `up` re-runs
the command and the resume guard adopts an already installed cluster
(kubeconfig plus `:6443/healthz` → smoke only) instead of reinstalling it.

5. **Run the acceptance gate:**

   ```bash
   make test
   ```

   `Expected:` all four pass. `verify-bootstrap` runs laptop rows; without the
   resolver from [`configure-laptop-dns.md`](configure-laptop-dns.md) those rows print `SKIP` loudly, and
   a skipped row is not a pass.

## Verify

- `pulumi stack output apiUrl --stack demo` → `https://api.demo.example.com:6443`.
- `pulumi stack output consoleUrl --stack demo` → `https://console-openshift-console.apps.demo.example.com`.
- `make test` → node Ready, all cluster operators available,
  `sda` source-less, ISO retained, host and laptop API `ok`, console `200/302`.
- `make test` → no pull-secret bytes in the stack export.

### VIP and ingress checks (`compact` / `ha` only)

`sno` uses none of these: `api`/`api-int`/`*.apps` resolve to the node lease and
there is no host load balancer.

On the Host, after Phase 2:

```bash
ip -4 addr show <bridge> | grep -F 192.168.100.7      # Ingress LB VIP present
cat /sys/class/net/<bridge>/bridge/multicast_snooping # → 0 (VRRP can cross)
systemctl is-active virtlab-<cluster>-haproxy         # → active
firewall-cmd --zone=libvirt --list-ports              # → includes 80/tcp 443/tcp
```

On the Workstation, after Phase 3:

```bash
export KUBECONFIG=~/.kube/config.<cluster>
oc get infrastructure cluster -o jsonpath='{.status.platform}' # → BareMetal
oc get ingresscontroller default -n openshift-ingress-operator \
  -o jsonpath='{.spec.replicas}'                               # → one per ingress node
```

`platform` is immutable: a cluster installed as `None` cannot be upgraded to
`BareMetal` in place. If `virtlab` reports a platform mismatch, run
`virtlab down` and then `virtlab up` — see
[`reinstall-cluster.md`](reinstall-cluster.md).

## Troubleshooting

- **`pulumi preview` shows deletions on a fresh stack** → wrong stack or a
  stale state; stop and inspect `pulumi stack --show-urns`.
- **`up` interrupted** → re-run step 4; artifacts under the install directory
  are reused.
- **A `verify-*` row prints `SKIP`** → the prerequisite it needs is missing;
  fix it, never read `SKIP` as success.
- **`up` failed during image or bootstrap** → [`recover-failed-install.md`](recover-failed-install.md).
- **`up` failed after the guest disk was created** → the resume path still
  applies; do not destroy without a snapshot.

## Never do

- **Never run `pulumi destroy` unless you mean to delete the running cluster**
  — the disk is a Pulumi resource and destroy removes it (see
  [`teardown-lab.md`](teardown-lab.md)).
- **Never run `pulumi refresh` while the cluster is up** — `guest-vm` declares
  the domain stopped, so refresh plus `up` powers the VM off.
- **Never `virsh destroy` the guest during an update** unless recovering;
  use the documented resume path.
- **Never `up` without the host snapshot** — it is the only rollback.

## Related

- Program and outputs: `main.go`.
- Stack configuration: `config.yaml`.
- Acceptance harness: `Makefile`, `Makefile`.
- Recovery: [`recover-failed-install.md`](recover-failed-install.md), [`reinstall-cluster.md`](reinstall-cluster.md).
