import assert from "node:assert/strict";
import { join } from "node:path";
import test from "node:test";

import { TableStore } from "../src/database.mjs";
import { agentSource, seededStore, syntheticSeed } from "./support.mjs";

test("seed preserves exact amounts and leaves absent values null", async () => {
  const { store, summary, databasePath } = await seededStore("db-seed");
  try {
    assert.equal(summary.row_count, 3);
    const described = store.describe();
    assert.equal(described.table.schema_revision, 1);
    assert.deepEqual(
      described.columns.map((column) => column.column_id),
      [
        "name",
        "institution",
        "category",
        "funding_source",
        "principal",
        "amount_due",
        "due_date",
        "repayment_status",
      ],
    );
    const queried = store.query({
      row_ids: [summary.row_ids[0]],
      sum_column_id: "principal",
    });
    assert.equal(queried.rows[0].values.principal, "1200.50");
    assert.equal(queried.rows[0].values.amount_due, null);
    assert.equal(queried.rows[0].values.due_date, null);
    assert.equal(queried.sum.value, "1200.50");

    const second = new TableStore(databasePath);
    try {
      assert.equal(
        String(
          second.db.prepare("PRAGMA journal_mode").get().journal_mode,
        ).toLowerCase(),
        "wal",
      );
      assert.equal(
        Number(second.db.prepare("PRAGMA busy_timeout").get().timeout),
        3000,
      );
      assert.equal(
        Number(second.db.prepare("PRAGMA synchronous").get().synchronous),
        2,
      );
      assert.equal(
        Number(second.db.prepare("PRAGMA foreign_keys").get().foreign_keys),
        1,
      );
    } finally {
      second.close();
    }
  } finally {
    store.close();
  }
});

test("seed --if-empty never replaces an existing workbook", async () => {
  const { store, summary } = await seededStore("db-if-empty");
  try {
    const second = store.seed(await syntheticSeed(), { ifEmpty: true });
    assert.equal(second.seeded, false);
    assert.equal(second.workbook_id, summary.workbook_id);
    assert.equal(second.row_count, 3);
  } finally {
    store.close();
  }
});

test("insert dedupe, versioned A-B-A updates, dry-run, and conflict receipts", async () => {
  const { store, summary } = await seededStore("db-change");
  try {
    const source = agentSource("message-a");
    const insert = {
      action: "insert",
      expected_schema_revision: 1,
      rows: [
        {
          name: "Synthetic duplicate probe",
          category: "financial",
          funding_source: "bank",
          repayment_status: "not_started",
        },
      ],
    };
    const first = store.change(insert, source);
    const replay = store.change(insert, source);
    assert.equal(replay.operation_id, first.operation_id);
    assert.equal(replay.replayed, true);
    assert.equal(
      store.query({ filters: [{ column_id: "name", op: "eq", value: "Synthetic duplicate probe" }] })
        .matched_count,
      1,
    );

    const rowID = summary.row_ids[0];
    const toRepaying = store.change(
      {
        action: "update",
        expected_schema_revision: 1,
        targets: [{ row_id: rowID, expected_revision: 1 }],
        values: { repayment_status: "repaying" },
      },
      source,
    );
    assert.equal(toRepaying.affected_count, 1);
    const toNotStarted = store.change(
      {
        action: "update",
        expected_schema_revision: 1,
        targets: [{ row_id: rowID, expected_revision: 2 }],
        values: { repayment_status: "not_started" },
      },
      source,
    );
    assert.equal(toNotStarted.affected_count, 1);
    const afterABA = store.query({ row_ids: [rowID] }).rows[0];
    assert.equal(afterABA.revision, 3);
    assert.equal(afterABA.values.repayment_status, "not_started");

    const historyBefore = store.history({}).count;
    const dryRun = store.change(
      {
        action: "update",
        expected_schema_revision: 1,
        targets: [{ row_id: rowID, expected_revision: 3 }],
        values: { repayment_status: "overdue" },
        dry_run: true,
      },
      { version: 1, source_kind: "unavailable" },
    );
    assert.equal(dryRun.dry_run, true);
    assert.equal(dryRun.affected_count, 1);
    assert.equal(store.history({}).count, historyBefore);
    assert.equal(
      store.query({ row_ids: [rowID] }).rows[0].values.repayment_status,
      "not_started",
    );

    assert.throws(
      () =>
        store.change(
          {
            action: "update",
            expected_schema_revision: 1,
            targets: [{ row_id: rowID, expected_revision: 1 }],
            values: { repayment_status: "overdue" },
          },
          source,
        ),
      (error) => error.code === "version_conflict",
    );
    assert.equal(store.history({}).count, historyBefore);
  } finally {
    store.close();
  }
});
