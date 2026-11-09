# SOP: Grant the laptop host access

> **Purpose:** let the laptop run `virsh` against the host over SSH as root
> without a password prompt.
> **Risk:** low — no lab state changes; a failure only blocks Pulumi and the
> verify scripts.
> **Success:** `virsh -c qemu+ssh://root@<host>/system list --all` runs from
> the laptop and prints the host's domain table.

## Prereqs
- [`prepare-host-vm.md`](prepare-host-vm.md) complete.
- An SSH keypair on the laptop under `~/.ssh/`.

## Steps

1. **Install your public key on the host:**

   ```bash
   ssh-copy-id root@<host>
   ```

   `Expected:` the key is appended to `/root/.ssh/authorized_keys`.

2. **Prove non-interactive root access:**

   ```bash
   ssh root@<host> 'virsh version'
   ```

   `Expected:` libvirt/hypervisor versions, no password prompt.

3. **Confirm the stack targets the same host and user:**

   ```bash
   grep 'libvirt:uri' config.yaml
   ```

   `Expected:` a `qemu+ssh://root@<host>/system` URI matching step 2.

4. **Reach libvirt from the laptop:**

   ```bash
   virsh -c qemu+ssh://root@<host>/system list --all
   ```

   `Expected:` the domain table (empty, or `demo` after a bring-up).

Either the LAN name/hosts entry or the tailnet name is acceptable; the repo
only requires that `root@<host>` resolves and the key is authorized.

## Verify

- `ssh -o BatchMode=yes -o ConnectTimeout=8 root@<host> true` → exit 0.
- `virsh -c qemu+ssh://root@<host>/system list --all` → exit 0.
- The CLI connects to the host configured in `config.yaml`.

## Troubleshooting

- **`Permission denied (publickey)`** → re-run `ssh-copy-id` with the same
  user the stack uses (`root`), or the key is not the one loaded.
- **`Host key verification failed`** → the host was rebuilt; remove the stale
  entry with `ssh-keygen -R <host>` and re-accept it.
- **`virsh: failed to connect to the hypervisor`** → SSH works but libvirtd
  does not: `systemctl status libvirtd` on the host.
- **Name does not resolve** → use the tailnet name or add a hosts entry; both
  are operator-specific.

## Never do

- **Never enable password or `PermitRootLogin yes` to make this work** — use
  keys; the stack's URI assumes key auth.
- **Never commit a private key** or copy it into the repo.
- **Never disable host-key checking globally** in `~/.ssh/config`; fix the
  stale entry instead.

## Related

- Provider URI and host pair: `config.yaml` (`libvirt:uri`,
  `lab.hostSSH`).
- Host: `config.yaml` (`lab.hostSSH`, `libvirt:uri`).
- Next: [`install-host-tools.md`](install-host-tools.md) and [`prepare-laptop.md`](prepare-laptop.md).
