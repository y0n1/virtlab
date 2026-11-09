# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Use GitHub's private vulnerability reporting for this repository:

1. Open the **Security** tab.
2. Choose **Report a vulnerability**.
3. Include the affected version, a description, and reproduction steps.

You will receive an acknowledgement as soon as the report is triaged. Please
allow time for a fix before disclosing the issue publicly.

## Scope

`virtlab` provisions libvirt resources and OpenShift clusters on hosts you
control. Reports about command execution, credential handling, path handling,
or unsafe defaults in this repository are in scope.

## Secrets

This repository never contains pull secrets, kubeconfigs, private keys, or
license archives. If you find one, report it privately and do not copy it.
