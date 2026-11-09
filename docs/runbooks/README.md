# Operator documentation

Everything in this directory is for the operator.

## Where to start

- **Building the lab from nothing?** Start with
[`prepare-host-vm.md`](prepare-host-vm.md) and follow this order:

  1. **Host:** [`prepare-host-vm.md`](prepare-host-vm.md) →
   [`grant-laptop-host-access.md`](grant-laptop-host-access.md) →
   [`install-host-tools.md`](install-host-tools.md) →
   [`store-pull-secret.md`](store-pull-secret.md).
  2. **Workstation:** [`prepare-laptop.md`](prepare-laptop.md) →
   [`bring-up-lab.md`](bring-up-lab.md). Phase 4 of `virtlab up` configures
   split-DNS and sshuttle automatically; use
   [`configure-laptop-dns.md`](configure-laptop-dns.md) and
   [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md) only
   to diagnose connectivity.
  3. **Access:** [`access-cluster.md`](access-cluster.md).

- **Fixing a specific problem?** Find the row below whose **Purpose** matches
your symptom. 
- **Operating a running cluster?** Go straight to
[`access-cluster.md`](access-cluster.md),
[`reinstall-cluster.md`](reinstall-cluster.md),
[`recover-failed-install.md`](recover-failed-install.md) or
[`teardown-lab.md`](teardown-lab.md).

## Procedures

One procedure per document, named `<verb>-<object>.md` (no prefix). Each
document is self-contained and lists what it depends on under **Prereqs**;
complete those prerequisites first, but no other reading is required.

| Document | Purpose | Risk | Status |
|---|---|---|---|
| [`prepare-host-vm.md`](prepare-host-vm.md) | A fresh host VM that needs libvirt, NTP, forwarding and disk headroom | medium | available |
| [`connect-workstation-sshuttle.md`](connect-workstation-sshuttle.md) | Workstation connects to lab subnet and DNS via sshuttle | low | available |
| [`grant-laptop-host-access.md`](grant-laptop-host-access.md) | Laptop has no SSH/libvirt access to the host | low | available |
| [`install-host-tools.md`](install-host-tools.md) | Host lacks the pinned OpenShift toolchain | low | available |
| [`store-pull-secret.md`](store-pull-secret.md) | Host lacks the pull secret at the configured path | low | available |
| [`prepare-laptop.md`](prepare-laptop.md) | Laptop lacks prerequisites or a valid `config.yaml` | low | available |
| [`configure-laptop-dns.md`](configure-laptop-dns.md) | Laptop cannot resolve the cluster zone | low | available |
| [`bring-up-lab.md`](bring-up-lab.md) | Provision or reconcile the lab with `virtlab up` | high | available |
| [`access-cluster.md`](access-cluster.md) | Get `oc` access to the running cluster | low | available |
| [`reinstall-cluster.md`](reinstall-cluster.md) | Reproduce the cluster from scratch or re-fire a config change | high | available |
| [`recover-failed-install.md`](recover-failed-install.md) | `virtlab up` failed during image or bootstrap | medium | available |
| [`teardown-lab.md`](teardown-lab.md) | Remove the lab and its host/workstation artifacts | high | available |
| [`release-virtlab.md`](release-virtlab.md) | Package and publish a new semantic version release | low | available |

New procedures follow the `sop-authoring` skill and its template. Project
conventions: this directory, `<verb>-<object>.md` names, and this file as the
index.

## Reference material (not procedures)

| Document | Contents | Status |
|---|---|---|
| [`../UBIQUITOUS-LANGUAGE.md`](../UBIQUITOUS-LANGUAGE.md) | Formal domain terminology and ubiquitous language | available |
| [`reference/architecture.md`](reference/architecture.md) | Topology diagram, what Pulumi owns, what stays manual | available |
| [`reference/known-unknowns.md`](reference/known-unknowns.md) | Deferred items and tracked gaps | available |
