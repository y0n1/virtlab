# Project guidelines

## Overview
Declarative provisioning of the virtlab test infrastructure: a Go CLI
(`virtlab`, entry point `cmd/virtlab/main.go`) structured into domain
packages (`internal/`) that manages libvirt resources (networks, storage pools,
guest VMs) and provisions OpenShift clusters on remote KVM/libvirt hypervisor
hosts.
Infrastructure resources are declared via the Pulumi Automation API using
the vendored `dmacvicar/libvirt` Pulumi SDK in `sdks/libvirt/`.

## Navigation
This file is a router. Classify the user request into one or more categories in
the table below. For each matching category, read only the file associated with
it to acquire more context.

| Category | Location |
|----|----|
| Domain ubiquitous language (shared domain vocabulary) | docs/UBIQUITOUS-LANGUAGE.md |
| Operator documentation (runbooks + procedures for humans, user-facing) | docs/runbooks/README.md |
| Create, change, or read a durable agent rule | .agents/rules/README.md |
| Local temporary files and Go caches | .agents/rules/local-temporary-files.md |

Durable agent rules live under `.agents/rules/`. Do not add rule bodies to this
file. Add a Navigation row here only when a rule covers a new top-level
category, and list every rule in the `.agents/rules/README.md` index.
