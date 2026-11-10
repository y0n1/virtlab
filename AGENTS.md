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

