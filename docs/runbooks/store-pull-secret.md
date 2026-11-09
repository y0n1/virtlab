# SOP: Store the pull secret

> **Purpose:** place the OpenShift pull secret at the host path the stack
> expects, readable only by root.
> **Risk:** low — the cluster cannot pull images without it, but nothing is
> destroyed; the real hazard is leaking the secret.
> **Success:** `stat` shows mode `600`, owner `root`, and the path matches the
> stack configuration.

## Prereqs
- Root shell on the host prepared by [`prepare-host-vm.md`](prepare-host-vm.md).
- A Red Hat account able to download the pull secret.

## Steps

1. **Download the pull secret on the host** from
   <https://console.redhat.com/openshift/install/pull-secret>.

   `Expected:` a `pull-secret.json` file under your download path.

2. **Install it at the configured path with restrictive permissions:**

   ```bash
   install -m 600 -o root -g root /path/to/pull-secret.json /root/pull-secret.json
   ```

   `Expected:` exit 0.

3. **Confirm mode and ownership:**

   ```bash
   stat -c '%a %U %n' /root/pull-secret.json
   ```

   `Expected:` `600 root /root/pull-secret.json`.

4. **Confirm the path matches the stack configuration:**

   ```bash
   grep 'pullSecretPath' config.yaml
   ```

   `Expected:` the same path shown by step 3.

The bytes are injected host-side into `install-config.yaml` at image build
time. They never enter the repo, the Pulumi program, or the stack state — only
the path appears in configuration.

## Verify

- `stat -c '%a %U' /root/pull-secret.json` → `600 root`.
- `grep pullSecretPath config.yaml` → matches the path.
- `git ls-files | grep -q pull-secret` → no output.

## Troubleshooting

- **Download returns an error page** → sign in to console.redhat.com first;
  the endpoint requires an authenticated session.
- **`stat` shows `644`** → re-run step 2 with `install -m 600`; do not use
  `chmod` after a copy that already exposed the file.
- **Path mismatch with the stack** → move the file to the configured path
  rather than editing the configuration.
- **Secret already leaked** (committed, pasted, or shared) → revoke and
  regenerate it in the Red Hat console before continuing.

## Never do

- **Never commit the pull secret** — `.gitignore` excludes it, but the check
  is yours; the file belongs on the host only.
- **Never put the bytes in stack config or environment variables** — only the
  path is configuration.
- **Never print or paste the file contents** into logs, tickets or chat.
- **Never reuse a secret copied from an old capture** — it has been revoked.

## Related

- Path and injection: `config.yaml`
  (`lab.cluster.pullSecretPath`), `internal/cluster/openshift.go`.
- Preflight: `internal/cluster/openshift.go`.
- Ignore rule: `.gitignore` (`**/pull-secret.json`).
