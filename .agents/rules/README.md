# Agent rules

Durable instructions for agents that work in this repository. [`AGENTS.md`](../../AGENTS.md)
is a router: it names the category and points here. The rule bodies live in
this directory.

## Index

| Rule | Description |
|---|---|
| [`local-temporary-files.md`](local-temporary-files.md) | Use `./.tmp` for scratch files and the Go build cache; keep `GOMODCACHE` outside the repository. |

## Authoring a rule

Create a rule when an instruction must survive the current session and apply to
future work.

1. Create `<kebab-case-name>.md` in this directory.
2. Start with frontmatter:
   ```yaml
   ---
   name: <kebab-case-name>
   description: <one-line summary used to decide relevance>
   ---
   ```
3. Write one rule per file. State the instruction in imperative bullets. Add a
   **Why** section when the reason is not obvious.
4. Add a row to the index table above.
5. Add a Navigation row in [`AGENTS.md`](../../AGENTS.md) when the rule covers a
   new top-level category.

Do not put rule bodies in `AGENTS.md`. Keep it a router and index.
