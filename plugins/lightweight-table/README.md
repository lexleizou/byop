# BYOP Lightweight Table

P15a is the smallest executable link from BYOP's existing Pi RPC client to an
explicitly loaded Pi extension, a local stdio MCP server, and an independent
`node:sqlite` database. It does not include a table UI, import/export, column
management, contexts, or undo.

All files in `test/fixtures/` are synthetic. Do not place a real bank export in
the repository. Real validation data and databases must stay under
`~/AgentScratch/`.

## Seed JSON

The seed format is already normalized. It does not read a source database and
does not derive an outstanding balance or repayment date.

```json
{
  "workbook_name": "Synthetic bank fixture - not a real ledger",
  "timezone": "Asia/Singapore",
  "rows": [
    {
      "name": "Synthetic product",
      "institution": "Synthetic institution",
      "category": "financial",
      "funding_source": "bank",
      "principal": "1234.56",
      "amount_due": null,
      "due_date": null,
      "repayment_status": "not_started"
    }
  ]
}
```

Rules:

- `principal` and `amount_due` are decimal strings. They are parsed directly to
  safe integer cents without using floating point.
- When no direct source field exists, `amount_due` and `due_date` stay `null`.
- `category`, `funding_source`, and `repayment_status` are retained as source
  strings.
- Unknown row fields are rejected so an export cannot silently map inferred
  financial fields.

Seed a new independent local database:

```sh
node plugins/lightweight-table/cli.mjs seed \
  --db /path/under/AgentScratch/tables.sqlite \
  --input /path/to/seed.json
```

Use `--if-empty` for validation against an existing copy. It returns a count
and IDs without replacing the workbook:

```sh
node plugins/lightweight-table/cli.mjs seed \
  --db /path/under/AgentScratch/tables.sqlite \
  --input /path/to/seed.json \
  --if-empty
```

## BYOP and Pi startup

The product integration is opt-in. Without these environment variables,
`startPiClient` keeps its existing `--mode rpc` arguments.

```sh
BYOP_PI_PATH=/path/to/pi \
BYOP_PI_EXTENSIONS="$PWD/plugins/lightweight-table/pi-extension.mjs" \
BYOP_LIGHTWEIGHT_TABLE_DB=/path/under/AgentScratch/tables.sqlite \
go run .
```

`BYOP_PI_EXTENSIONS` is a platform path list; each entry is appended as an
explicit Pi `--extension` argument. The bridge registers the session-scoped
`lightweight_table` MCP server. It does not edit the global Pi `mcp.json`.

For the controlled validation wrapper:

```sh
BYOP_P15A_E2E=1 \
BYOP_PI_PATH=/Users/lexleizou/AgentScratch/20261028-byop-table-link/pi-link-wrapper.sh \
BYOP_P15A_SEED_INPUT=/Users/lexleizou/AgentScratch/20261028-byop-table-link/bank-seed.json \
BYOP_LIGHTWEIGHT_TABLE_DB=/Users/lexleizou/AgentScratch/20261028-byop-table-link/bank-copy.sqlite \
go test . -run '^TestP15aLightweightTableBridgeE2E$' -count=1 -v
```

The E2E runs `seed --if-empty`, so an already seeded database is not reset. It
then uses the real BYOP `startPiClient` with the controlled Pi wrapper:

1. the direct MCP prompt sends the same insert twice in one Pi user turn and
   checks that intent dedupe leaves one receipt;
2. the nested test probe queries one existing seed row by stable ID, changes
   only its `repayment_status`, restores the original value with the next
   expected revision, and checks that the row revision advanced by two.

History must contain two distinct Pi user message groups. The probe returns no
name, institution, principal, or amount-due values, and the test logs counts and
IDs only.

## MCP surface

The model-visible schemas omit `_byop`. The bridge injects that source object in
Pi's `tool_call` hook after Pi validates model arguments.

| Tool | Purpose |
|---|---|
| `table_describe` | Workbook/table revisions, timezone, and stable column IDs |
| `table_query` | Row-ID/filter/projection query and exact numeric sum |
| `table_change` | Insert, versioned update/delete, or dry-run preview |
| `table_history` | Successful receipts and Pi source attribution |

Updates and deletes require an expected table revision and an expected revision
for every target row. An insert is deduplicated by intent within one Pi user
message group. Update and delete commands include the expected versions. A
replay checks the successful receipt before checking current row versions.

## SQLite contract

- one business row change and its successful receipt commit in the same
  `BEGIN IMMEDIATE` transaction;
- WAL, `synchronous=FULL`, `foreign_keys=ON`, and `busy_timeout=3000` are checked
  on every connection;
- failures, version conflicts, and busy errors do not create receipts;
- `dry_run` never writes rows or receipts;
- row and column IDs are stable, and deleted rows are soft deleted.

## Focused tests

```sh
node --test \
  plugins/lightweight-table/test/database.test.mjs \
  plugins/lightweight-table/test/source-tracker.test.mjs \
  plugins/lightweight-table/test/mcp.test.mjs

node --test plugins/lightweight-table/test/concurrency.test.mjs

go test . -run 'TestPiClientArgsAddsOnlyExplicitExtensions|TestP15aLightweightTableBridgeE2E' -count=1
```

Runtime test databases are created below
`~/AgentScratch/20261028-byop-table-link/`; set `BYOP_P15A_SCRATCH` to use a
different local scratch root.
