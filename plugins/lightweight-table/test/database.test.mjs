import assert from "node:assert/strict";
import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";
import test from "node:test";

import { TableStore } from "../src/database.mjs";
import {
  agentSource,
  makeScratch,
  seededStore,
  syntheticSeed,
} from "./support.mjs";

test("seed preserves exact amounts and leaves absent values null", async () => {
  const { store, summary, databasePath } = await seededStore("db-seed");
  try {
    assert.equal(summary.row_count, 3);
    const described = store.describe();
    assert.equal(described.table.schema_revision, 2);
    assert.deepEqual(
      described.columns.slice(0, 12).map((column) => column.column_id),
      [
        "institution",
        "category",
        "name",
        "principal",
        "actual_received",
        "remaining_due",
        "annualized_rate",
        "repayment_status",
        "plan_status",
        "next_due",
        "next_due_amount",
        "funding_source",
      ],
    );
    assert.equal(
      described.columns.find((column) => column.column_id === "actual_received")
        .config.read_only,
      true,
    );
    const queried = store.query({
      row_ids: [summary.row_ids[0]],
      sum_column_id: "principal",
    });
    assert.equal(queried.rows[0].values.principal, "1200.50");
    assert.equal(queried.rows[0].values.actual_received, null);
    assert.equal(queried.rows[0].values.remaining_due, null);
    assert.equal(queried.rows[0].values.annualized_rate, null);
    assert.equal(queried.rows[0].values.plan_status, null);
    assert.equal(queried.rows[0].values.next_due, null);
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

test("v1 tables migrate to the v2 imported-column schema in place", async () => {
  const directory = await makeScratch("db-migrate-v1");
  const databasePath = join(directory, "tables.sqlite");
  const legacy = new DatabaseSync(databasePath);
  legacy.exec(`
    CREATE TABLE workbook_meta (
      singleton INTEGER PRIMARY KEY,
      workbook_id TEXT NOT NULL UNIQUE,
      format_version INTEGER NOT NULL,
      name TEXT NOT NULL,
      timezone TEXT NOT NULL,
      created_at TEXT NOT NULL
    );
    CREATE TABLE tables (
      table_id TEXT PRIMARY KEY,
      name TEXT NOT NULL,
      schema_revision INTEGER NOT NULL,
      data_revision INTEGER NOT NULL
    );
    CREATE TABLE columns (
      column_id TEXT PRIMARY KEY,
      table_id TEXT NOT NULL,
      label TEXT NOT NULL,
      type TEXT NOT NULL,
      required INTEGER NOT NULL,
      config TEXT NOT NULL,
      position INTEGER NOT NULL,
      hidden INTEGER NOT NULL
    );
    CREATE TABLE rows (
      row_id TEXT PRIMARY KEY,
      table_id TEXT NOT NULL,
      revision INTEGER NOT NULL,
      values_json TEXT NOT NULL,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      deleted_at TEXT
    );
    INSERT INTO workbook_meta VALUES (
      1, 'legacy-workbook', 1, 'Legacy synthetic', 'Asia/Singapore',
      '2026-10-01T00:00:00.000Z'
    );
    INSERT INTO tables VALUES ('tbl_default', '银行负债', 1, 1);
    INSERT INTO columns VALUES (
      'name', 'tbl_default', '名称', 'text', 1,
      '{"source_field":"product_name"}', 0, 0
    );
    INSERT INTO rows VALUES (
      'legacy-row', 'tbl_default', 1,
      '{"name":"Synthetic legacy row"}',
      '2026-10-01T00:00:00.000Z',
      '2026-10-01T00:00:00.000Z',
      NULL
    );
  `);
  legacy.close();

  const migrated = new TableStore(databasePath);
  try {
    const described = migrated.describe();
    assert.equal(described.workbook.format_version, 2);
    assert.equal(described.table.schema_revision, 2);
    assert.ok(
      described.columns.some(
        (column) => column.column_id === "actual_received",
      ),
    );
    assert.equal(
      described.columns.find((column) => column.column_id === "name")
        .config.read_only,
      undefined,
    );
    assert.ok(
      migrated.db
        .prepare("PRAGMA table_info(rows)")
        .all()
        .some((column) => column.name === "source_record_id"),
    );
  } finally {
    migrated.close();
  }
});

test("insert dedupe, versioned A-B-A updates, dry-run, and conflict receipts", async () => {
  const { store, summary } = await seededStore("db-change");
  try {
    const source = agentSource("message-a");
    const insert = {
      action: "insert",
      expected_schema_revision: 2,
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
        expected_schema_revision: 2,
        targets: [{ row_id: rowID, expected_revision: 1 }],
        values: { repayment_status: "repaying" },
      },
      source,
    );
    assert.equal(toRepaying.affected_count, 1);
    const toNotStarted = store.change(
      {
        action: "update",
        expected_schema_revision: 2,
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
        expected_schema_revision: 2,
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
            expected_schema_revision: 2,
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
