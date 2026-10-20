# BYOP Design System

This directory makes the public repository self-contained.

| File | Purpose |
|---|---|
| `onevra/tokens.json` | Onevra base colors, typography, spacing, radii, motion and product semantics |
| `pi-workbench/tokens.extensions.json` | BYOP workbench, Chat, plugin, task panel and Loop extensions |

Both files are JSON and can be validated with `jq empty`.

The design source is pinned in each file's metadata. `tokens.extensions.json`
keeps the compatibility namespace `workbench.*`. BYOP product behavior is
encoded under `piWorkbench.*`, `chat.*` and related namespaces.

The current P2 Go implementation consumes a deliberate subset: semantic
colors, the 14 px body size, the full type scale, native spacing and the
expanded/collapsed navigation dimensions. Token-to-CSS synchronization and a
runtime token loader remain future work.
