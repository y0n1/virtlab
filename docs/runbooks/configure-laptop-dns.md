# SOP: Configure workstation split DNS

> **Purpose:** make the Workstation resolve the cluster zone through the Lab Gateway (`192.168.100.1`) instead of public DNS.
> **Risk:** low — reversible workstation-only change; an incorrect entry only affects the lab cluster domain.
> **Success:** `dscacheutil` (macOS) or `resolvectl query` (Linux) resolves `api.demo.example.com` to the address the preset publishes: the node lease (`192.168.100.10`) for `sno`, the API VIP (`192.168.100.5`) for `compact` and `ha`.

## Prereqs
- [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md) complete and tunnel active.
- Workstation running macOS or Fedora Linux.

## Steps

1. **Create the split-DNS entry for the cluster zone:**

   *Note: `make up` or `lab up` applies this automatically with explicit microcopy.*

   **On macOS:**
   ```bash
   sudo mkdir -p /etc/resolver
   printf 'nameserver 192.168.100.1\n' | sudo tee /etc/resolver/example.com
   ```

   **On Fedora Linux (systemd-resolved):**
   ```bash
   sudo mkdir -p /etc/systemd/resolved.conf.d
   printf '[Resolve]\nDNS=192.168.100.1\nDomains=~example.com\n' | sudo tee /etc/systemd/resolved.conf.d/lab.conf
   sudo systemctl restart systemd-resolved
   ```

   `Expected:` the configuration file is created with permissions 0644.

2. **Confirm the zone resolves to the preset's published address:**

   On macOS:
   ```bash
   dscacheutil -q host -a name api.demo.example.com
   ```

   On Fedora Linux:
   ```bash
   resolvectl query api.demo.example.com
   ```

   `Expected:` `192.168.100.10` in `sno`, `192.168.100.5` in `compact`/`ha`
   (the **API VIP** — no single node owns it, keepalived does).

3. **Confirm the API answers across the tunnel:**

   ```bash
   curl -sk https://api.demo.example.com:6443/healthz
   ```

   `Expected:` `ok` once the cluster is installed; connection refused before
   [`bring-up-lab.md`](bring-up-lab.md) is normal.

## Verify

- `api.demo.example.com` resolves to `192.168.100.10` (`sno`) or `192.168.100.5` (`compact`/`ha`).
- `console-openshift-console.apps.demo.example.com` resolves to the same
  `sno` node, or to the **Ingress LB VIP** (`192.168.100.7`) in `compact`/`ha`.
- `curl -skI https://console-openshift-console.apps.demo.example.com | head -1` → `HTTP/2 200` once the cluster is up.

The cluster's own **Ingress VIP** (`192.168.100.6`) is required by
`platform: baremetal` but is deliberately not published: external `*.apps`
traffic reaches the **Ingress Load Balancer** on the Host instead, which spreads
it across every router node.

## Troubleshooting

- **No answer from DNS query** → confirm the sshuttle tunnel is running with `--ns-hosts=192.168.100.1 --to-ns=192.168.100.1:53`.
- **Stale negative cache on macOS** → `sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder`, then retry.
- **systemd-resolved not directing domain on Linux** → verify `resolvectl status` shows `~example.com` under search domains.
- **`api` resolves but `*.apps` does not** → in `compact`/`ha` the wildcard points
  at the **Ingress LB VIP**; confirm the Host owns it
  (`ip -4 addr show <bridge>`) and that the HAProxy unit is active. On `sno`,
  confirm the node lease instead.

## Never do

- **Never point the resolver at a public DNS server** — `example.com` is lab-internal.
- **Never commit `/etc/resolver` or drop-in files to git** — they are machine-local.
- **Never add a global search domain for the lab**; it can hijack unrelated lookups.

## Related

- Zone configuration: `config.yaml` (`lab.cluster.baseDomain`) and `config.yaml`.
- Domain language: [`../UBIQUITOUS-LANGUAGE.md`](../UBIQUITOUS-LANGUAGE.md).
- Tunnel setup: [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md).
- Next: [`bring-up-lab.md`](bring-up-lab.md).
