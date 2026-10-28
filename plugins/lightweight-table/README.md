# BYOP Lightweight Table

P15b adds a normal BYOP Tables WebView on top of the P15a Node data layer.
The plugin uses native HTML and JavaScript, `window.byop.postMessage`, the
existing theme tokens, and an independent SQLite file. It has no build step,
grid dependency, network access, approval flow, clear-all action, CSV, or
XLSX support.

## Included flow

- Compact business table with horizontal scrolling and a narrow-window layout.
- Server-side search, one typed filter, sorting, 100-row pages, and an exact
  principal total for every matching row.
- IME-safe inline cell editing with Enter, Tab, and Escape handling.
- Add and delete rows.
- Select records or use every filtered record, then set one field in a single
  version-checked operation.
- Create an immutable row-ID/version context and call `host.openChat` with
  `{text, contextId, label}`. The host appends the machine-readable context
  marker and renders its removable chip.
- History drawer with explicit undo. An undo is one inverse transaction and
  fails if any target changed after the original operation.
- One-second version polling only while the page is visible. Active drafts are
  never replaced by a poll refresh.

All data in `test/fixtures/` is synthetic. Real records, private databases, QA
databases, screenshots, and logs must stay outside the repository.

## Plugin files

| Path | Purpose |
|---|---|
| `plugin.json` | Navigation, WebView, locale, and `contributes.agentExtension` |
| `plugin.mjs` | Long-lived plugin worker and page API |
| `pi-extension.mjs` | Pi MCP registration and plugin-data path resolution |
| `web/` | Native HTML, CSS, and JavaScript WebView |
| `l10n/` | Complete `en` and `zh-Hans` copy |
| `src/` | Shared SQLite, context, MCP, and validation modules |
| `test/` | Direct Node tests using synthetic fixtures only |

The manifest declares:

```json
"contributes": {
  "agentExtension": "pi-extension.mjs"
}
```

BYOP discovery supplies `BYOP_PLUGIN_DATA_DIR` to `plugin.mjs`. The worker uses:

```text
<BYOP_PLUGIN_DATA_DIR>/tables.sqlite
```

Pi receives `BYOP_PLUGIN_DATA_DIRS` as a JSON map. Unless the test override
`BYOP_LIGHTWEIGHT_TABLE_DB` is set, the extension reads:

```text
BYOP_PLUGIN_DATA_DIRS["local.lightweight-table"]/tables.sqlite
```

The MCP child is launched with Node permissions scoped to the package directory
for reads and its own data directory for reads and writes. It does not receive
network or child-process permission.

## WebView bridge

The page only uses:

```js
window.byop.postMessage({ action, ...payload })
```

There is no product-test RPC. The page API covers locale, bootstrap, query,
version, change, context capture, history, undo, and opening chat with a fixed
context.

For automated real-WebView QA:

- wait for `document.body.dataset.ready === "true"`;
- use the stable element IDs documented in `web/index.html`;
- use the stable `data-action` value on each interactive control;
- drive the actual MyGo `Page.Eval` bridge and capture the actual page;
- use only a synthetic fixture database.

The page loads locale copy through `table.locale`. The Node worker reads only
these fixed package paths:

```text
../l10n/en.json
../l10n/zh-Hans.json
```

The WebView does not use `fetch` for locale files. The `byop-plugin` custom
scheme does not support query strings or URL fragments, and this bridge keeps
locale loading independent of those parameters.

## Data and write identity

Agent writes keep the P15a source object and Pi session/message IDs. Page writes
use:

```json
{
  "version": 1,
  "source_kind": "page",
  "view_instance_id": "stable page instance UUID",
  "client_command_id": "stable command UUID"
}
```

The command UUID is created before the first submit and reused if the response
is unknown. A successful retry returns the original receipt. Reusing the same
page command ID for different values returns `command_id_conflict`.

Contexts store sorted row IDs, their captured revisions, schema/data revisions,
and the query description. `table_query(context_id)` reads current values and
marks changed or missing targets. `table_change(context_id)` expands the fixed
IDs and captured versions; it never re-runs the original filter to discover a
new target set.

Undo checks the post-operation revision of every target. If any row changed,
the whole undo rolls back. The `undo_of` unique index permits one inverse
operation per original operation, including across processes.

## MCP tools

The model-visible schemas do not expose `_byop`; the Pi extension injects it at
the tool-call hook.

| Tool | Purpose |
|---|---|
| `table_describe` | Workbook, columns, revisions, timezone, and filter options |
| `table_query` | Search/filter/sort/page, exact sum, capture/read a fixed context |
| `table_change` | Insert, versioned update/delete, batch field update, dry run |
| `table_undo` | One atomic inverse operation with later-edit conflict checks |
| `table_history` | Successful operations, source, undo status, and pagination |

When a normal BYOP user prompt ends with:

```text
[byop-table-context:<contextId>]
```

the model should pass that value as `table_query.context_id`. The host hides the
machine-readable line in the product UI and shows the preceding label as a
removable chip.

## Seed and local checks

Seed a private database only when it is empty:

```sh
node plugins/lightweight-table/cli.mjs seed \
  --db "$BYOP_PLUGIN_DATA_DIR/tables.sqlite" \
  --input /path/to/synthetic-seed.json \
  --if-empty
```

Run all directly related plugin tests:

```sh
cd plugins/lightweight-table
npm run check
```

The check covers:

- syntax for the plugin worker, Pi extension, and WebView JavaScript;
- page command replay and command-ID conflict;
- immutable context targets and version conflicts;
- exact search/filter/sort/total behavior;
- insert, update, and delete undo, unique `undo_of`, and later-edit conflicts;
- MCP `table_query`, context reading, and `table_undo`;
- plugin-data path resolution and scoped MCP Node permissions;
- worker-to-host `host.openChat` shape;
- `en`/`zh-Hans` key parity, stable QA selectors, ready flag, and query-free
  local resource paths.

These tests do not replace the coordinator's real MyGo WebView acceptance run,
real private seed, or screenshot verification.
