# ADR-001: Ingress addressing for compact clusters

## Status
Accepted

## Date
2026-11-08

## Context

The compact preset installs three control-plane nodes (192.168.100.10/.11/.12)
on `lab-net`, with DNS served by the libvirt network's dnsmasq at
192.168.100.1. `install-config.yaml` used `platform: none: {}` (user-managed
networking), so virtlab owned every DNS record and there was no API or ingress
VIP.

`BuildNetwork` published:

- `api.<fqdn>` and `api-int.<fqdn>` for every control-plane node (libvirt
  `<dns><host>` entries);
- one `address=/apps.<fqdn>/<ip>` dnsmasq option per ingress node.

During an earlier verification run, Phase 3 hit the installer's
40-minute limit with:

```
Cluster operators authentication, console are not available: timed out
waiting for the condition
```

All failing checks named the same endpoint:

```
dial tcp 192.168.100.10:443: connect: connection refused
```

Observed state on the installed cluster:

| Check | Result |
|---|---|
| `api` / `api-int` resolution | three A records (.10, .11, .12) |
| `*.apps` resolution | **one** A record (.10) |
| IngressController `default` replicas | 2 |
| `router-default` pods | master-1 (.11), master-2 (.12) |
| Per-node `oauth` route probe | .10 refused; .11 and .12 HTTP 200 |
| Console pods | crashloop: `failed to construct OAuth endpoint cache: context deadline exceeded` after 5 minutes |

Two independent defects combined:

1. **Advertised set is not the serving set.** The wildcard advertised every
   control-plane node, but the default IngressController ran two router
   replicas, so .10 served no ingress.
2. **The wildcard was not round-robin.** On the host's dnsmasq 2.85, three
   repeated `address=` options for `apps.<fqdn>` produce a single record (the
   first, .10). The same resolver returns all three addresses for `api` because
   those use libvirt host entries. The comment on `buildDnsmasqOptions` claimed
   the wildcard "follows the full ingress node set"; the resolver did not.

Console was a downstream symptom: it exits fatally when its OAuth endpoint
cache cannot be built within five minutes, which is the same unreachable route.

The cluster converged on its own later: later the ingress operator had
rescheduled a router onto master-0 (`router-default-…-mqll2`, running 7h52m at
the time of the check), after which `authentication` and `console` became
Available and both routes returned HTTP 200. That confirmed the failure depended
on router placement relative to the single advertised wildcard address — and it
did not remove the fragility.

`auth/kubeconfig` and `auth/kubeadmin-password` were written at 20:05, before
the ISO build completed, so a re-run of `virtlab up` after the ingress fault is
corrected skips Phase 3 through the resume guard and proceeds to Phase 4.

## Decision

Adopt **Option C, augmented with a host-side ingress load balancer**, for the
**compact** and **HA** presets. **SNO keeps `platform: none`**; VIPs are
overkill for a single node.

| Preset | Platform | `api` / `api-int` | `*.apps` |
|---|---|---|---|
| sno | `none` | the single node IP | the single node IP |
| compact | `baremetal` | API VIP (offset 5) | HAProxy VIP (offset 7) |
| ha | `baremetal` | API VIP (offset 5) | HAProxy VIP (offset 7) |

- **API:** `install-config.yaml` uses `platform.baremetal` with `apiVIPs` and
  `ingressVIPs`. Assisted-service writes `/etc/keepalived/lease-api` and
  `lease-ingress` into node ignition, so the control-plane nodes own the VIPs
  through keepalived (active/passive failover). `provisioningNetwork` is set to
  `Disabled` because the guests have no provisioning network or BMCs.
- **Cluster ingress VIP:** required by assisted-service (it rejects a length
  mismatch between `apiVIPs` and `ingressVIPs`) but **not used for external
  traffic**; it is reserved at offset 6.
- **Ingress scale:** an HAProxy instance on the hypervisor binds the ingress LB
  VIP on the lab bridge and fronts every ingress node on 80/443 with
  `balance source` and `tcp-check`. Adding router nodes extends the backend
  pool, so `*.apps` scales horizontally — which keepalived alone cannot do.
- **Router count:** virtlab reconciles `IngressController/default` replicas to
  the number of ingress leases so every advertised node runs a router.
- **DNS:** `api`/`api-int` resolve to the API VIP; `*.apps` resolves to the
  HAProxy VIP; per-node records are retained.
- **VRRP readiness:** IGMP snooping is disabled on the lab bridge in Phase 2
  (the bridge has `mcast_snooping=1`, `mcast_querier=0`).
- **Day-2 host addition:** the HAProxy backends and ingress replica count are
  derived from the topology, so adding workers later only requires refreshing
  both. The node-add flow itself is a separate follow-up feature request.

Horizontal scaling of the **API** is explicitly out of scope; the API remains
active/passive behind the cluster VIP.

## Alternatives Considered

### Option A — Match router replicas to the advertised ingress set

Set `IngressController/default.spec.replicas` to `len(ingressLeases)` and keep
the per-node DNS.

- Pros: smallest change; no new components; every advertised IP serves.
- Cons: on dnsmasq 2.85 the wildcard still resolves to a single node, so all
  ingress traffic lands on one node; added hosts run routers that receive no
  traffic. No failover and no horizontal scale.
- Rejected: does not satisfy the availability and scaling requirements.

### Option B — Make the wildcard return the full ingress set

Keep `platform: none` and publish a wildcard record set resolving to all
ingress nodes.

- Pros: no new infrastructure; client-side failover using the same model as
  `api`.
- Cons: the installed dnsmasq (2.85) does not return multiple addresses for one
  domain; its man page directs multi-address answers to `--addn-hosts`, which
  cannot express a wildcard. Upstream 2.86 added multiple addresses per domain
  via repeated `address=` flags, but the host (CentOS Stream 9) ships only
  2.85-x in `appstream` (checked during implementation). Option B would require building or
  packaging dnsmasq ≥ 2.86, running it in a container, or replacing the gateway
  resolver.
- Rejected: the resolver dependency is a larger, riskier change than the VIP +
  HAProxy path, and it still lacks health checking.

### Option C alone — `platform: baremetal` without an ingress LB

- Pros: cluster-managed VIPs, keepalived failover, no external process.
- Cons: keepalived is active/passive; added hosts would never receive ingress
  traffic.
- Rejected as insufficient on its own; adopted together with the HAProxy layer.

## Consequences

- `platform` is immutable after install: every existing SNO/compact/HA cluster
  must be torn down and reinstalled.
- Compact and HA now depend on keepalived VRRP crossing `virbr-labN`; disabling
  IGMP snooping is required and must be re-applied after a host reboot (the next
  `virtlab up` does so in Phase 2).
- The host becomes part of the external ingress path: HAProxy, its unit, the
  bridge VIP, and the 80/443 firewall openings are created by `virtlab up` and
  removed by `virtlab down`. Host failure already takes down every VM, so the
  added single point of failure is accepted for a lab.
- Three addresses are reserved: API VIP, cluster ingress VIP (installer
  requirement, unused externally), and HAProxy VIP.
- The `buildDnsmasqOptions` claim of a per-node wildcard is replaced by a single
  LB address; the old per-node regression tests are superseded.
- Phase 4 gains strict checks (VIP DNS answers, HTTP 200 from `/healthz`, the
  console redirect host, and a required `oc get nodes` with every expected node
  `Ready`), so a partial install cannot pass.
- Day-2 node addition is not implemented; a follow-up feature request captures
  the agent add-nodes flow plus backend/replica refresh.

## Open Questions

1. **VRRP on the lab bridge — mitigated, verify at runtime.** Snooping is
   disabled; if VRRP still fails, enable the bridge multicast querier, then
   investigate keepalived unicast peers.
2. **Day-2 host addition — follow-up.** The design keeps the backend set and
   replica count derived from topology, but the node-add flow and refresh logic
   are not part of this change.
3. **HAProxy availability — accepted risk.** A single HAProxy on the hypervisor
   is a single point of failure for external ingress; acceptable while the host
   itself is a dependency, but a paired LB would be required if the lab must
   survive host loss.
