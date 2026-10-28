# BYOP Fuyao 0.4.0

Fuyao adds a normal BYOP WebView on top of the P15a Node data layer.
Version 0.4.0 imports a complete Fuyao v4 export into an independent SQLite
snapshot. The plugin does not connect to Fuyao PostgreSQL at runtime.
The plugin uses native HTML and JavaScript, `window.byop.postMessage`, the
existing theme tokens, and an independent SQLite file. It has no build step,
grid dependency, network access, approval flow, clear-all action, CSV, or
XLSX support.

## Included flow

- The ten visible v4 debt columns in display order. Imported or derived values
  are read-only; a missing value displays as `—`.
- A complete one-time v4 import for debts, repayment plans, planned payments,
  payments, payment allocations, and debt events. All related writes run in one
  transaction with foreign keys enabled.
- A read-only row detail drawer with source metadata, the current plan,
  installment status counts, payment totals and recent payments, and recent
  events.
- A single cell click toggles the row selection; double-click or Enter starts
  one inline editor. Escape cancels an editor or clears the row selection.
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
| `plugin.json` | Navigation, WebView, locale, `agentExtension`, and stable `agentProxy` |
| `plugin.mjs` | Long-lived plugin worker and page API |
| `pi-extension.mjs` | Pi MCP registration and plugin-data path resolution |
| `mcp-proxy.mjs` | Stable stdio MCP proxy and generation-based backend reload |
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

The Pi extension registers `mcp-proxy.mjs` as the stable stdio MCP server. The
proxy keeps its process and MCP session alive while it runs the table MCP in a
separate backend child. A successful BYOP plugin restart atomically updates:

```text
<BYOP_PLUGIN_DATA_DIR>/runtime-generation.json
```

On the next poll, the proxy waits for any in-flight request, closes the old
backend, starts the changed backend, and sends
`notifications/tools/list_changed`. The proxy is the only process with
`--allow-child-process`; the backend keeps the prior package/data read scope and
data write scope with no network or child-process permission. Changes to
`pi-extension.mjs` or `mcp-proxy.mjs` itself still require Pi to be refreshed.

## WebView bridge

The page only uses:

```js
window.byop.postMessage({ action, ...payload })
```

There is no product-test RPC. The page API covers locale, bootstrap, query,
debt detail, version, change, context capture, history, undo, and opening chat
with a fixed context.

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
| `table_debt_details` | One debt with its current plan, installment counts, payments, and events |
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

The v4 importer reads only these local files:

```text
debts-api.json
debts.json
repayment_plans.json
planned_payments.json
payments.json
payment_allocations.json
debt_events.json
manifest.json
```

It verifies each file count and SHA-256 digest in `manifest.json`, requires
matching API/raw debt IDs, validates relation foreign keys, and writes no file
under the source directory.

Import into a new database:

```sh
node plugins/lightweight-table/cli.mjs import-v4 \
  --db "$BYOP_PLUGIN_DATA_DIR/tables.sqlite" \
  --input-dir /path/to/fuyao-v4-export
```

To replace an existing seed, pass `--replace-unmodified`. This is accepted only
when the database has zero operations and zero contexts. The CLI validates the
snapshot first, inspects the original database without opening the migrating
`TableStore`, and copies the unmodified SQLite file to:

```text
<db>.pre-v4-import-YYYYMMDD-HHMMSS
```

A database containing saved history is rejected instead of being deleted.
Running the same snapshot again with the replacement flag is idempotent for
rows and related data.

The synthetic seed remains available for generic table tests:

```sh
node plugins/lightweight-table/cli.mjs seed \
  --db /path/to/synthetic.sqlite \
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
- complete synthetic v4 import counts, stable UUID row IDs, exact cents,
  derived rate formatting, foreign keys, backup, replacement guard, and
  idempotency;
- read-only rejection for imported and derived columns;
- insert, update, and delete undo, unique `undo_of`, and later-edit conflicts;
- MCP `table_query`, `table_debt_details`, context reading, and `table_undo`;
- plugin-data path resolution and scoped MCP Node permissions;
- stable proxy identity, backend PID replacement, `tools/list_changed`, and
  in-flight request completion before backend switch;
- worker-to-host `host.openChat` shape;
- `en`/`zh-Hans` key parity, stable QA selectors, ready flag, and query-free
  local resource paths.

These tests do not replace the coordinator's real MyGo WebView acceptance run,
real private seed, or screenshot verification.
