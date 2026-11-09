# SOP: Configure workstation split DNS

> **Purpose:** make the Workstation resolve the cluster zone through the Lab
> Gateway instead of public DNS.
> **Risk:** low — reversible workstation-only change; an incorrect entry only
> affects the lab cluster domain.
> **Success:** `dscacheutil` (macOS) or `resolvectl query` (Linux) resolves
> `api.<clusterName>.<baseDomain>` to the address the preset publishes: the
> Control Plane Node lease for `sno`, the API VIP for `compact` and `ha`.

## Prereqs
- [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md) complete
  and tunnel active.
- Workstation running macOS or Fedora Linux.
- Run the commands from the directory holding `config.yaml`, in one shell
  session. The variables from step 1 do not persist across shells.

## Steps

1. **Resolve the addressing values from the lab configuration.**

   `virtlab up` applies split-DNS automatically in Phase 4. Run this SOP only
   to diagnose or repair a missing or incorrect entry.

   ```bash
   CLUSTER="$(sed -n 's/^clusterName:[[:space:]]*//p' config.yaml | head -1 | tr -d '"')"
   DOMAIN="$(sed -n 's/^baseDomain:[[:space:]]*//p' config.yaml | head -1 | tr -d '"')"
   : "${DOMAIN:=example.com}"

   CIDR="$(sed -n 's/^[[:space:]]*cidr:[[:space:]]*//p' config.yaml | head -1 | tr -d '"')"
   if [ -z "$CIDR" ]; then
     # Auto-allocated /24: use the "Network subnet resolved to" line that
     # virtlab up printed, or the gateway recorded in the state file.
     STATE="${XDG_STATE_HOME:-$HOME/.local/state}/virtlab/state.json"
     GW="$(sed -n 's/.*"gatewayIP":[[:space:]]*"\([^"]*\)".*/\1/p' "$STATE" 2>/dev/null)"
     if [ -n "$GW" ]; then CIDR="$GW/24"; fi
   fi

   eval "$(awk -v cidr="$CIDR" '
   function octets(v) {
     return int(v / 16777216) "." int(v / 65536) % 256 "." int(v / 256) % 256 "." v % 256
   }
   BEGIN {
     if (cidr == "") { print "echo \"Set network.cidr or run virtlab up first\" >&2; exit 1"; exit }
     split(cidr, o, "[./]")
     base = ((o[1] * 256 + o[2]) * 256 + o[3]) * 256 + o[4]
     net = int(base / 2 ^ (32 - o[5])) * 2 ^ (32 - o[5])
     printf "export LAB_CIDR=%s\n", cidr
     printf "export GATEWAY=%s\n", octets(net + 1)
     printf "export API_VIP=%s\n", octets(net + 5)
     printf "export INGRESS_VIP=%s\n", octets(net + 6)
     printf "export INGRESS_LB_VIP=%s\n", octets(net + 7)
     printf "export CP_LEASE=%s\n", octets(net + 10)
   }
   ')"
   ```

   `Expected:` no output. Print the resolved values with:

   ```bash
   printf 'cidr=%s gateway=%s lease=%s apiVip=%s appsVip=%s\n' \
     "$LAB_CIDR" "$GATEWAY" "$CP_LEASE" "$API_VIP" "$INGRESS_LB_VIP"
   ```

   If `network.apiVip`, `network.ingressVip`, or `network.ingressLbVip` is set
   in `config.yaml`, use that explicit address instead of the derived default.

2. **Create the split-DNS entry for the cluster zone.**

   **On macOS:**
   ```bash
   sudo mkdir -p /etc/resolver
   printf 'nameserver %s\n' "$GATEWAY" | sudo tee "/etc/resolver/$DOMAIN"
   ```

   **On Fedora Linux (systemd-resolved):**
   ```bash
   sudo mkdir -p /etc/systemd/resolved.conf.d
   printf '[Resolve]\nDNS=%s\nDomains=~%s\n' "$GATEWAY" "$DOMAIN" \
     | sudo tee "/etc/systemd/resolved.conf.d/lab-$CLUSTER.conf"
   sudo systemctl restart systemd-resolved
   ```

   `Expected:` the configuration file is created with permissions 0644.

   The Linux file name must be `lab-<clusterName>.conf`. `virtlab down` removes
   exactly that name; a differently named file stays behind after teardown and
   keeps sending lab queries to a gateway that no longer exists.

3. **Confirm the zone resolves.**

   **On macOS:**
   ```bash
   dscacheutil -q host -a name "api.$CLUSTER.$DOMAIN"
   ```

   **On Fedora Linux:**
   ```bash
   resolvectl query "api.$CLUSTER.$DOMAIN"
   ```

   `Expected:` `$CP_LEASE` in `sno`; `$API_VIP` in `compact` and `ha`
   (the **API VIP** — keepalived owns it, no single node does).

4. **Confirm the API answers across the tunnel.**

   ```bash
   curl -sk "https://api.$CLUSTER.$DOMAIN:6443/healthz"
   ```

   `Expected:` `ok` once the cluster is installed; connection refused before
   [`bring-up-lab.md`](bring-up-lab.md) is normal.

## Verify

- `api.$CLUSTER.$DOMAIN` resolves to `$CP_LEASE` (`sno`) or `$API_VIP`
  (`compact`/`ha`).
- `console-openshift-console.apps.$CLUSTER.$DOMAIN` resolves to `$CP_LEASE`
  (`sno`) or to the **Ingress LB VIP** (`$INGRESS_LB_VIP`) in `compact`/`ha`.
- `curl -skI "https://console-openshift-console.apps.$CLUSTER.$DOMAIN" | head -1`
  → `HTTP/2 200` once the cluster is up.

The cluster's own **Ingress VIP** (`$INGRESS_VIP`) is required by
`platform: baremetal` but is deliberately not published: external `*.apps`
traffic reaches the **Ingress Load Balancer** on the Host instead, which spreads
it across every router node.

## Troubleshooting

- **No answer from DNS query** → confirm the sshuttle tunnel is running with
  `--ns-hosts=$GATEWAY --to-ns=$GATEWAY:53`.
- **Stale negative cache on macOS** → `sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder`, then retry.
- **systemd-resolved not directing domain on Linux** → verify `resolvectl status`
  shows `~$DOMAIN` under search domains and that `lab-$CLUSTER.conf` exists.
- **Stale `/etc/systemd/resolved.conf.d/lab.conf` after teardown** → earlier
  versions of this SOP used that name; `virtlab down` does not remove it.
  Delete the file and restart `systemd-resolved`.
- **`api` resolves but `*.apps` does not** → in `compact`/`ha` the wildcard points
  at the **Ingress LB VIP**; confirm the Host owns it
  (`ip -4 addr show <bridge>`) and that the HAProxy unit is active. On `sno`,
  confirm the node lease instead.

## Never do

- **Never point the resolver at a public DNS server** — `example.com` is lab-internal.
- **Never commit `/etc/resolver` or drop-in files to git** — they are machine-local.
- **Never add a global search domain for the lab**; it can hijack unrelated lookups.
- **Never rename the Linux drop-in file** — `virtlab down` removes only
  `lab-<clusterName>.conf`, so a renamed file survives teardown.

## Related

- Addressing plan (offsets and ownership): [`../configuration.md`](../configuration.md#addressing-plan).
- Zone configuration: `config.yaml` (`clusterName`, `baseDomain`, `network`)
  and `internal/workstation/splitdns.go`.
- Domain language: [`../UBIQUITOUS-LANGUAGE.md`](../UBIQUITOUS-LANGUAGE.md).
- Tunnel setup: [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md).
- Next: [`bring-up-lab.md`](bring-up-lab.md).
