# SOP: Full teardown

> **Purpose:** destroy the lab's cluster, VMs, pool, network, host artifacts,
> and workstation access in one command.
> **Risk:** high — `virtlab down` deletes the running cluster and its disk;
> there is no undo.
> **Success:** `virtlab down` exits 0, and no `<clusterName>*` domain, network,
> or pool remains on the host.

## Prereqs
- [`bring-up-lab.md`](bring-up-lab.md) completed at least once (a missing
  stack is treated as already destroyed).
- SSH as root to the host.
- A VMware snapshot of the host VM — the only rollback once teardown starts.

## Steps

1. **Take a host snapshot** in VMware.

   `Expected:` the snapshot completes.

2. **Tear the lab down** from the directory holding `config.yaml`:

   ```bash
   virtlab down
   ```

   Answer the confirmation prompt, or use `virtlab down -y` to skip it.
   `Expected:` these stages run in order:

   - Pulumi destroys the guest domains, volumes, storage pool, and network;
   - the host removes lingering cluster domains, `/home/libvirt/<clusterName>`,
     the NetworkManager split-DNS files, and the ingress load balancer (unit,
     config, socket, PID file, and 80/443 firewall openings);
   - the workstation stops sshuttle, removes split-DNS, and uninstalls tools
     that virtlab installed;
   - `~/.kube/config.<clusterName>` and
     `~/.kube/kubeadmin-password.<clusterName>` are deleted.

3. **Confirm the host is clean:**

   ```bash
   ssh root@<host> '
     virsh list --all
     virsh net-list --all
     virsh pool-list --all
     test ! -e /home/libvirt/<clusterName> && echo "install tree gone"
   '
   ```

   `Expected:` no `<clusterName>*` domain; `<clusterName>-net` absent; the
   pool absent; `install tree gone`.

## Verify

- `virtlab status` → tunnel inactive, kubeconfig and password `Not synced yet`.
- `virsh net-list --all` → the lab network is gone (`default` may remain).
- `test ! -e /home/libvirt/<clusterName>` → `install tree gone`.
- `firewall-cmd --zone=libvirt --list-ports` → neither `80/tcp` nor `443/tcp`.

## Troubleshooting

- **Infrastructure teardown failed** → the Pulumi stack may be unreachable;
  confirm the backend (`PULUMI_BACKEND_URL` or `pulumi login --local`) and
  re-run. The host and workstation stages still run.
- **`Pulumi stack ... was created against Host`** → `config.yaml` targets a
  different Host than the stack. Teardown stops and skips the host and
  workstation stages, because cleanup there would delete unrelated resources.
  Put the original `hostSsh.host` back and re-run
  ([`../state.md`](../state.md#backup-and-restore)).
- **`recorded on backend ... but the active backend is`** → select the backend
  that recorded the stack, then re-run. Do not tear down against a different
  backend.
- **A domain or volume remains** → re-run `virtlab down`; if it persists,
  inspect the stack state before touching `virsh`.
- **Laptop DNS still resolves the cluster** → the workstation stage failed;
  fix its error, then re-run `virtlab down`.

## Never do

- **Never delete the stock `default` libvirt network or pool** — unrelated to
  the lab.
- **Never delete `/root/pull-secret.json`** — a host prerequisite reused by
  other installs.
- **Never hand-delete libvirt objects while `virtlab down` is running** — wait
  for it to finish.
- **Never remove the laptop resolver as part of teardown** unless
  decommissioning access entirely; `virtlab down` already reverts it.

## Related

- Teardown stages: `cmd/virtlab/main.go` (`runTeardownStages`).
- Host cleanup: `internal/host/teardown.go`, `internal/host/ingresslb.go`.
- Configuration: `config.yaml` (`clusterName`, `hostSsh`, `baseDomain`).
