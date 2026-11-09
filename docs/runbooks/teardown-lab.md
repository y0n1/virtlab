# SOP: Full teardown

> **Purpose:** remove the lab (cluster, disk, network) and its host-side
> artifacts from the host.
> **Risk:** high — `pulumi destroy` deletes the **running cluster** and its
> 500 GiB disk. There is no undo; the host snapshot is the rollback.
> **Success:** no `demo*` domain, network or pool; no enabled
> `demo-dns` unit; `/home/libvirt/demo` gone.

## Prereqs
- Pulumi access to stack `demo`.
- SSH as root to the host.
- The stack being removed is described by [`bring-up-lab.md`](bring-up-lab.md).

## Steps

1. **Take a host snapshot.** In VMware, snapshot the host VM. This is the only
   rollback once destroy starts.

2. **Select the stack and review the plan** (from the repo root on the laptop):

   ```bash
   pulumi stack select demo
   pulumi preview --diff
   ```

   `Expected:` the plan deletes the domain, volume, pool, network, and the
   `command` resources. Stop if it proposes deleting anything else.

3. **Destroy the Pulumi-managed resources:**

   ```bash
   pulumi destroy --stack demo
   ```

   Answer the confirmation prompt. `Expected:` every resource deleted,
   `demo-net` removed, no errors.

   The provider removes the domain, the `demo.qcow2` volume, the
   `demo` pool and the network. It does **not** remove the host-side
   artifacts in step 4: the `dns-bridge-*` remote commands have no `Delete`
   action, and the install tree is written by scripts, not declared as a
   Pulumi resource.

4. **Remove the host DNS bridge** (unit + four files created by
   `host-dns-bridge`):

   ```bash
   ssh root@hypervisor.example.com 'bash -s' <<'EOF'
   systemctl disable --now demo-dns
   rm -f /etc/demo-dns.conf \
         /etc/systemd/system/demo-dns.service \
         /etc/NetworkManager/conf.d/00-demo-dnsmasq.conf \
         /etc/NetworkManager/dnsmasq.d/demo.conf
   systemctl daemon-reload
   systemctl reload NetworkManager
   EOF
   ```

   `Expected:` no output, exit 0.

5. **Remove the install tree** (ISO, agent configs, kubeconfig, logs, serial
   log — all under the lab-owned pool directory):

   ```bash
   ssh root@hypervisor.example.com 'rm -rf /home/libvirt/demo'
   ```

   `Expected:` no output, exit 0.

## Verify

```bash
ssh root@hypervisor.example.com '
  virsh list --all
  virsh net-list --all
  virsh pool-list --all
  systemctl is-enabled demo-dns || echo "unit gone"
  test ! -e /home/libvirt/demo && echo "install tree gone"
'
```

- `virsh list --all` → no `demo` domain.
- `virsh net-list --all` → `demo-net` absent (`default` may remain).
- `virsh pool-list --all` → `demo` absent.
- `systemctl is-enabled …` → fails with "No such file or directory"; `unit gone`.
- `systemctl is-enabled virtlab-demo-haproxy` → also gone; `virtlab down`
  stops and disables the ingress LB unit and removes
  `/etc/virtlab/demo-haproxy.cfg`, its socket, its PID file, and the
  `80/tcp` `443/tcp` firewall openings.
- `firewall-cmd --zone=libvirt --list-ports` → neither `80/tcp` nor `443/tcp`.
- `test ! -e …` → `install tree gone`.

Reclaim check: `ssh root@hypervisor.example.com 'df -h /home'` shows ~500 GiB
returned.

## Troubleshooting

- **Destroy hangs or is interrupted** → `pulumi cancel --stack demo`,
  re-run step 3. Never hand-delete libvirt objects while destroy is running.
- **Domain or volume remains after a clean destroy** → re-run step 3; if it
  persists, inspect `pulumi stack --show-urns` before touching `virsh`.
- **Laptop DNS still resolves the cluster** → expected until the laptop
  resolver is removed (see *Never do*); the host-side zone is gone, so
  queries will fail rather than resolve.

## Never do

- **Never delete the `default` libvirt network or pool** — stock libvirt,
  unrelated to this lab.
- **Never delete `/root/pull-secret.json`** — a host prerequisite, not stack
  state; other installs reuse it.
- **Never remove Tailscale or `/etc/resolver/example.com` as part of teardown**
  unless decommissioning laptop access entirely; removing the resolver just
  makes `api.…` lookups fail without freeing anything.
- **Never run `pulumi refresh` while the cluster is up** — the guest is
  declared `Running: false`, so refresh + `up` would power the VM off.

## Related

- Values quoted above: `config.yaml` (`lab.vm.*`, `lab.dnsBridge.*`,
  `lab.cluster.installDir`, `lab.cluster.pullSecretPath`).
- Why the DNS files survive: `internal/host/prepare.go` (remote
  commands define `Create`/`Update`, no `Delete`).
- Why the pool autostart needs no cleanup step: `internal/infrastructure/storagepool.go`
  (flag dies with the pool).
- Install artifacts: `internal/cluster/openshift.go`.
