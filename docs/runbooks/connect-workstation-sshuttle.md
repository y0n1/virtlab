# SOP: Connect workstation via sshuttle

> **Purpose:** establish a transparent TCP and split-DNS forwarding tunnel from the Workstation to the Host.
> **Risk:** low — reversible local proxy; stopping the command or running `virtlab down` cleanly restores original routing.
> **Success:** `curl -sk https://192.168.100.1` or the cluster gateway answers across the tunnel.

## Prereqs
- [`prepare-host-vm.md`](prepare-host-vm.md) complete (Host has base packages and forwarding enabled).
- [`grant-laptop-host-access.md`](grant-laptop-host-access.md) complete (SSH key access to Host verified).
- `sshuttle` installed on the Workstation.

## Steps

1. **Install `sshuttle` on the Workstation:**

   On macOS:
   ```bash
   brew install sshuttle
   ```

   On Fedora Linux:
   ```bash
   sudo dnf install -y sshuttle
   ```

   `Expected:` `sshuttle --version` prints the installed version.

2. **Launch the tunnel:**

   Automatic (recommended): run `virtlab up`. Phase 4 configures split-DNS and
   starts the Lab Tunnel. From a source checkout, `make up` builds
   `bin/virtlab` and runs the same command.

   Or explicitly via `sshuttle`:
   ```bash
   sudo sshuttle --ns-hosts=192.168.100.1 --to-ns=192.168.100.1:53 -r root@hypervisor.example.com 192.168.100.0/24
   ```

   `Expected:` Terminal outputs `Connected to server.` and enters forwarding mode.

3. **Confirm the tunnel reaches the Host gateway:**

   ```bash
   curl -skI https://192.168.100.1:6443 2>&1 | head -1
   ```

   `Expected:` connection refused or HTTP response (proves packet routing reaches the remote bridge).

## Verify

- `ps aux | grep [s]shuttle` → active process running.
- In another terminal: `dig @192.168.100.1 api.demo.example.com +short` → returns lease IP once the lab network is up.

## Troubleshooting

- **`sudo: a password is required`** → `sshuttle` modifies local firewall rules (`pf` on macOS, `iptables`/`nftables` on Linux) and requires local `sudo` privileges.
- **Connection refused on SSH** → verify `ssh root@hypervisor.example.com` works independently before launching the tunnel.
- **`pfctl` errors on macOS** → flush packet filter state: `sudo pfctl -d && sudo pfctl -e`.

## Never do

- **Never run `sshuttle --dns`** — `--dns` captures all workstation DNS from `/etc/resolv.conf`, which hijacks and blackholes regular internet and corporate VPN resolution. Always use targeted `--ns-hosts` and `--to-ns`.
- **Never run `sshuttle 0/0`** — routing all workstation traffic hijacks regular internet and corporate connections. Only tunnel the lab CIDR (`192.168.100.0/24`).
- **Never leave a detached background sshuttle running after deleting the lab** — run `virtlab down` to terminate the process and restore state.

## Related

- Architecture: [`../architecture.md`](../architecture.md)
- Domain language: [`../UBIQUITOUS-LANGUAGE.md`](../UBIQUITOUS-LANGUAGE.md)
- Workstation DNS: [`configure-laptop-dns.md`](configure-laptop-dns.md)

