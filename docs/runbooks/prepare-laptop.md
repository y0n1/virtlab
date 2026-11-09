# SOP: Prepare the laptop

> **Purpose:** install the Pulumi and Go toolchains, log in to the local
> Pulumi backend, and select the lab stack on the operator's laptop.
> **Risk:** low — no lab state changes; a wrong stack or backend only blocks
> preview and up.
> **Success:** `pulumi version` and `go version` answer, `pulumi whoami`
> reports the local user, and `demo` is selected on the `file://~`
> backend.

## Prereqs
- A laptop with shell access and this repository checked out.
- User-managed dependencies: `ssh` (OpenSSH >= 8.0) and `go` (>= 1.26).
- Tool-managed dependencies: `sshuttle`, `pulumi`, `ocm` (auto-installed on demand and tracked in `~/.local/state/virtlab/state.json`).
- A passphrase for the local state secrets provider, stored in your password
  manager. Never put it in the repository or in `config.yaml`.

## Steps

1. **Verify user-managed toolchains:**

   ```bash
   ssh -V
   go version
   ```

   `Expected:` OpenSSH >= 8.0 and Go >= 1.26. Missing or outdated binaries must
   be installed by the user. Note that `virtlab` auto-installs `sshuttle`,
   `pulumi`, and `ocm` if missing, recording them in `~/.local/state/virtlab/state.json`.

2. **Log in to the local backend:**

   ```bash
   pulumi login --local
   ```

   `Expected:` exit 0; `pulumi whoami` prints your OS user.

   `--local` is shorthand for `file://~`: state lives under `~/.pulumi` on
   this laptop. There is no shared or cloud state for this lab.

3. **Export the stack passphrase** for the current shell:

   ```bash
   export PULUMI_CONFIG_PASSPHRASE='<passphrase-from-your-password-manager>'
   ```

   `Expected:` no output. The local backend defaults to the passphrase secrets
   provider, so Pulumi asks for this on commands that read or write state; the
   environment variable keeps unattended commands from prompting. Add the
   export to your shell profile to have it in every session.

4. **Select or create the lab stack** (from the repo root):

   ```bash
   pulumi stack select demo || pulumi stack init demo
   ```

   `Expected:` the stack is selected on the local backend.

5. **Confirm the stack sees the committed configuration:**

   ```bash
   pulumi config --stack demo | head
   ```

   `Expected:` values from `config.yaml` (for example
   `libvirt:uri` and `lab:network:bridge`).

6. **Read the pinned Go toolchain** rather than assuming a version:

   ```bash
   grep '^go ' go.mod
   ```

   `Expected:` a version your installed Go satisfies (Go >= 1.26).

## Verify

- `pulumi whoami` → your OS user (local backend, not an organization).
- `pulumi stack` → `demo`, with state showing `current`.
- `go build ./...` from the repo root → exit 0.

## Troubleshooting

- **`OpenSSH version < 8.0` or missing `ssh`** → install or upgrade OpenSSH.
- **`Go version < 1.26` or missing `go`** → install or upgrade Go from https://go.dev/dl/.
- **`pulumi: command not found`** → `virtlab` auto-installs it on `up`, or install manually via Homebrew (`brew install pulumi`) or the official Pulumi installation script.
- **`no stack named 'demo'`** → confirm `pulumi login --local`, then run
  `pulumi stack init demo` from the repo root.
- **A passphrase prompt in an unattended command** → export
  `PULUMI_CONFIG_PASSPHRASE` in that shell.
- **`pulumi stack ls` shows a cloud organization** → you are on the wrong
  backend; re-run `pulumi login --local` (login is per-user and global).
- **`pulumi config` shows missing keys** → you are in the wrong directory or
  stack; re-run step 4 from the repo root.

## Never do

- **Never commit `~/.pulumi` state or the passphrase** — state is local to
  this laptop; back it up securely, never through the repo.
- **Never run bare `pulumi login`** — it switches this user to a cloud
  backend, which this lab does not use.
- **Never `pulumi stack rm demo`** — it deletes the stack state, not the
  lab, and Pulumi loses track of every resource.
- **Never add secrets to `config.yaml`** — the file is committed by
  design; secrets live on the host.
- **Never initialize a second stack casually** — an `up` against the wrong
  stack tries to build a second lab.

## Related

- Project and stack: `Pulumi.yaml`, `config.yaml`.
- Local state and secrets backend: `~/.pulumi` (not in the repo).
- Toolchain: `go.mod`.
- Next: [`configure-laptop-dns.md`](configure-laptop-dns.md).
