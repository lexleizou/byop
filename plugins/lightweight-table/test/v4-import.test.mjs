import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { promisify } from "node:util";
import test from "node:test";

import { TableStore } from "../src/database.mjs";
import { createMcpHandler } from "../src/mcp-server.mjs";
import { importV4Snapshot } from "../src/v4-import.mjs";
import {
  CLI_PATH,
  agentSource,
  makeScratch,
  seededStore,
  syntheticSeed,
  syntheticV4Records,
  writeSyntheticV4Snapshot,
} from "./support.mjs";

const execFileAsync = promisify(execFile);
const FIRST_DEBT_ID = "11111111-1111-4111-8111-111111111111";

function digestTree(directory, filenames) {
  return Object.fromEntries(
    filenames.map((filename) => [
      filename,
      createHash("sha256")
        .update(readFileSync(join(directory, filename)))
        .digest("hex"),
    ]),
  );
}

test("v4 import stores exact synthetic snapshot values, relations, and read-only detail", async () => {
  const scratch = await makeScratch("v4-import");
  const input = join(scratch, "source");
  const databasePath = join(scratch, "tables.sqlite");
  await writeSyntheticV4Snapshot(input);
  const before = digestTree(input, [
    "manifest.json",
    "debts-api.json",
    "debts.json",
    "repayment_plans.json",
    "planned_payments.json",
    "payments.json",
    "payment_allocations.json",
    "debt_events.json",
  ]);
  const store = new TableStore(databasePath);
  try {
    const summary = importV4Snapshot(store, {
      inputDirectory: input,
      replaceUnmodified: true,
    });
    assert.equal(summary.replaced, false);
    assert.equal(summary.backup, null);
    assert.deepEqual(summary.counts, {
      debts: 2,
      repayment_plans: 1,
      planned_payments: 2,
      payments: 1,
      payment_allocations: 1,
      debt_events: 2,
    });

    const queried = store.query({ row_ids: [FIRST_DEBT_ID] });
    assert.equal(queried.rows.length, 1);
    assert.equal(queried.rows[0].row_id, FIRST_DEBT_ID);
    assert.equal(queried.rows[0].values.principal, "1000.00");
    assert.equal(queried.rows[0].values.actual_received, "990.00");
    assert.equal(queried.rows[0].values.remaining_due, "700.00");
    assert.equal(queried.rows[0].values.annualized_rate, "12.50%");
    assert.equal(queried.rows[0].values.next_due, "2026-11-15");
    assert.equal(queried.rows[0].values.next_due_amount, "345.67");
    assert.equal(queried.rows[0].values.source_record_id, FIRST_DEBT_ID);

    const second = store.query({
      row_ids: ["22222222-2222-4222-8222-222222222222"],
    });
    assert.equal(second.rows[0].values.actual_received, null);
    assert.equal(second.rows[0].values.annualized_rate, "0.80%／月");

    const relationCounts = Object.fromEntries(
      [
        "fuyao_repayment_plans",
        "fuyao_planned_payments",
        "fuyao_payments",
        "fuyao_payment_allocations",
        "fuyao_debt_events",
        "fuyao_import_meta",
      ].map((table) => [
        table,
        Number(
          store.db.prepare(`SELECT COUNT(*) AS count FROM ${table}`).get().count,
        ),
      ]),
    );
    assert.deepEqual(relationCounts, {
      fuyao_repayment_plans: 1,
      fuyao_planned_payments: 2,
      fuyao_payments: 1,
      fuyao_payment_allocations: 1,
      fuyao_debt_events: 2,
      fuyao_import_meta: 1,
    });
    assert.deepEqual(store.db.prepare("PRAGMA foreign_key_check").all(), []);

    const detail = store.detail({ row_id: FIRST_DEBT_ID });
    assert.equal(detail.current_plan.id, "33333333-3333-4333-8333-333333333333");
    assert.equal(detail.current_plan.installment_rows.length, 2);
    assert.deepEqual(detail.installment_counts, { paid: 1, pending: 1 });
    assert.equal(detail.payment_summary.count, 1);
    assert.equal(detail.payments.length, 1);
    assert.equal(detail.payment_summary.total, "510.00");
    assert.equal(detail.event_summary.count, 1);
    assert.equal(detail.events.length, 1);
    assert.equal(detail.recent_payments[0].amount, "510.00");
    assert.equal(detail.data_source.live_connected, false);

    store.db.exec(`
      CREATE TEMP TABLE replaced_plan AS
      SELECT * FROM fuyao_repayment_plans
      WHERE id = '33333333-3333-4333-8333-333333333333';
      UPDATE replaced_plan
         SET id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
             status = 'replaced',
             effective_on = '2099-01-01',
             created_at = '2099-01-01T00:00:00.000Z',
             updated_at = '2099-01-01T00:00:00.000Z';
      INSERT INTO fuyao_repayment_plans SELECT * FROM replaced_plan;
      DROP TABLE replaced_plan;
    `);
    const detailAfterReplacement = store.detail({ row_id: FIRST_DEBT_ID });
    assert.equal(detailAfterReplacement.plan_count, 2);
    assert.equal(
      detailAfterReplacement.current_plan.id,
      "33333333-3333-4333-8333-333333333333",
    );

    assert.throws(
      () =>
        store.change(
          {
            action: "update",
            expected_schema_revision: 2,
            targets: [{ row_id: FIRST_DEBT_ID, expected_revision: 1 }],
            values: { actual_received: "1.00" },
          },
          agentSource("read-only-message"),
        ),
      (error) => error.code === "read_only_column",
    );
    assert.equal(store.history({}).count, 0);

    const handler = createMcpHandler(databasePath);
    try {
      const listed = handler.handle({
        jsonrpc: "2.0",
        id: 10,
        method: "tools/list",
        params: {},
      });
      const detailTool = listed.result.tools.find(
        (tool) => tool.name === "table_debt_details",
      );
      assert.ok(detailTool);
      const changeTool = listed.result.tools.find(
        (tool) => tool.name === "table_change",
      );
      assert.equal(
        changeTool.inputSchema.properties.values.properties.actual_received
          .readOnly,
        true,
      );
      const mcpDetail = handler.handle({
        jsonrpc: "2.0",
        id: 11,
        method: "tools/call",
        params: {
          name: "table_debt_details",
          arguments: { row_id: FIRST_DEBT_ID },
        },
      });
      assert.equal(mcpDetail.result.isError, false);
      assert.equal(
        mcpDetail.result.structuredContent.payment_summary.total,
        "510.00",
      );
    } finally {
      handler.close();
    }
  } finally {
    store.close();
  }
  const after = digestTree(input, Object.keys(before));
  assert.deepEqual(after, before);
});

test("v4 replacement backs up once per run, is idempotent, and preserves unmodified history guard", async () => {
  const scratch = await makeScratch("v4-replace");
  const input = join(scratch, "source");
  await writeSyntheticV4Snapshot(input);
  const { store, summary: legacySummary } = await seededStore("v4-legacy");
  try {
    const first = importV4Snapshot(store, {
      inputDirectory: input,
      replaceUnmodified: true,
    });
    assert.equal(first.replaced, true);
    assert.ok(existsSync(first.backup));
    assert.match(first.backup, /\.pre-v4-import-\d{8}-\d{6}/);
    const backupStore = new TableStore(first.backup);
    try {
      assert.equal(backupStore.summary().row_count, legacySummary.row_count);
    } finally {
      backupStore.close();
    }
    const firstValues = store.query({
      row_ids: [FIRST_DEBT_ID],
      projection: [
        "name",
        "principal",
        "actual_received",
        "remaining_due",
        "annualized_rate",
      ],
    }).rows[0].values;

    const second = importV4Snapshot(store, {
      inputDirectory: input,
      replaceUnmodified: true,
    });
    assert.ok(existsSync(second.backup));
    assert.notEqual(second.backup, first.backup);
    assert.deepEqual(second.counts, first.counts);
    assert.ok(second.data_revision > first.data_revision);
    assert.deepEqual(
      store.query({
        row_ids: [FIRST_DEBT_ID],
        projection: [
          "name",
          "principal",
          "actual_received",
          "remaining_due",
          "annualized_rate",
        ],
      }).rows[0].values,
      firstValues,
    );
    assert.equal(
      Number(
        store.db
          .prepare("SELECT COUNT(*) AS count FROM fuyao_import_meta")
          .get().count,
      ),
      1,
    );
    assert.equal(
      Number(
        store.db
          .prepare("SELECT COUNT(*) AS count FROM fuyao_debt_events")
          .get().count,
      ),
      2,
    );
  } finally {
    store.close();
  }

  const guarded = await seededStore("v4-guard");
  try {
    guarded.store.change(
      {
        action: "update",
        expected_schema_revision: 2,
        targets: [
          {
            row_id: guarded.summary.row_ids[0],
            expected_revision: 1,
          },
        ],
        values: { repayment_status: "repaying" },
      },
      agentSource("guard-message"),
    );
    assert.throws(
      () =>
        importV4Snapshot(guarded.store, {
          inputDirectory: input,
          replaceUnmodified: true,
        }),
      (error) =>
        error.code === "import_history_present" &&
        error.details.operations === 1 &&
        error.details.contexts === 0,
    );
    assert.deepEqual(guarded.store.summary().row_count, legacySummary.row_count);
  } finally {
    guarded.store.close();
  }

  const contextGuard = await seededStore("v4-context-guard");
  try {
    contextGuard.store.query({ capture_targets: true });
    assert.throws(
      () =>
        importV4Snapshot(contextGuard.store, {
          inputDirectory: input,
          replaceUnmodified: true,
        }),
      (error) =>
        error.code === "import_history_present" &&
        error.details.operations === 0 &&
        error.details.contexts === 1,
    );
  } finally {
    contextGuard.store.close();
  }
});

test("v4 CLI imports a synthetic snapshot without network and rejects invalid relation data", async () => {
  const scratch = await makeScratch("v4-cli");
  const input = join(scratch, "source");
  const databasePath = join(scratch, "cli.sqlite");
  await writeSyntheticV4Snapshot(input);
  const { stdout } = await execFileAsync(process.execPath, [
    CLI_PATH,
    "import-v4",
    "--db",
    databasePath,
    "--input-dir",
    input,
    "--replace-unmodified",
  ]);
  const result = JSON.parse(stdout);
  assert.equal(result.ok, true);
  assert.equal(result.counts.debts, 2);
  const cliStore = new TableStore(databasePath);
  try {
    assert.equal(cliStore.summary().data_source.source_instance, "synthetic-v4-test-instance");
  } finally {
    cliStore.close();
  }

  const records = syntheticV4Records();
  records["planned_payments.json"][0].plan_id =
    "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const invalidInput = join(scratch, "invalid-source");
  await writeSyntheticV4Snapshot(invalidInput, records);
  const invalidDatabasePath = join(scratch, "invalid.sqlite");
  const invalidStore = new TableStore(invalidDatabasePath);
  try {
    assert.throws(
      () =>
        importV4Snapshot(invalidStore, {
          inputDirectory: invalidInput,
          replaceUnmodified: true,
        }),
      (error) => error.code === "invalid_v4_snapshot",
    );
    assert.equal(invalidStore.initialized(), false);
  } finally {
    invalidStore.close();
  }
});

test("v4 CLI backs up the original pre-migration database before replacing it", async () => {
  const scratch = await makeScratch("v4-pre-migration-backup");
  const input = join(scratch, "source");
  const databasePath = join(scratch, "legacy.sqlite");
  await writeSyntheticV4Snapshot(input);
  const legacy = new TableStore(databasePath);
  try {
    legacy.seed(await syntheticSeed());
    legacy.db
      .prepare("UPDATE workbook_meta SET format_version = 1")
      .run();
    legacy.db
      .prepare("UPDATE tables SET schema_revision = 1")
      .run();
  } finally {
    legacy.close();
  }

  const { stdout } = await execFileAsync(process.execPath, [
    CLI_PATH,
    "import-v4",
    "--db",
    databasePath,
    "--input-dir",
    input,
    "--replace-unmodified",
  ]);
  const imported = JSON.parse(stdout);
  assert.ok(imported.backup);
  const backup = new DatabaseSync(imported.backup);
  try {
    assert.equal(
      backup.prepare(
        "SELECT format_version FROM workbook_meta WHERE singleton = 1",
      ).get().format_version,
      1,
    );
    assert.equal(
      backup.prepare(
        "SELECT schema_revision FROM tables WHERE table_id = 'tbl_default'",
      ).get().schema_revision,
      1,
    );
    assert.equal(
      backup.prepare(
        "SELECT COUNT(*) AS count FROM rows WHERE deleted_at IS NULL",
      ).get().count,
      (await syntheticSeed()).rows.length,
    );
  } finally {
    backup.close();
  }
});

test("v4 importer rejects a non-empty database without the explicit replacement flag", async () => {
  const scratch = await makeScratch("v4-no-replace");
  const input = join(scratch, "source");
  await writeSyntheticV4Snapshot(input);
  const { store } = await seededStore("v4-no-replace-db");
  try {
    assert.throws(
      () => importV4Snapshot(store, { inputDirectory: input }),
      (error) => error.code === "database_not_empty",
    );
    assert.equal(store.summary().row_count, (await syntheticSeed()).rows.length);
  } finally {
    store.close();
  }
});
