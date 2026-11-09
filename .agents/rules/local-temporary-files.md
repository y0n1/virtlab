---
name: local-temporary-files
description: Use ./.tmp for scratch files and the Go build cache; keep GOMODCACHE outside the repository.
---

# Local temporary files and Go caches

- Put scratch files, logs, and generated output in `./.tmp/`. The directory is
  gitignored. Do not use `/tmp` or the system temp directory for repository
  work.
- Point the Go build cache at the same place:
  `GOCACHE="$PWD/.tmp/gocache"`.
- Keep `GOMODCACHE` outside the repository, for example
  `GOMODCACHE="${TMPDIR:-/tmp}/virtlab-gomodcache"`.

## Why

The sandbox denies writes to `.gitmodules` and `.vscode/` files inside the
workspace. Module archives contain those files, so a module cache under
`./.tmp` fails to extract and breaks the build. The build cache contains no
such files and can stay in the project.

## Example

```bash
GOCACHE="$PWD/.tmp/gocache" \
GOMODCACHE="${TMPDIR:-/tmp}/virtlab-gomodcache" \
  go test ./internal/...
```
