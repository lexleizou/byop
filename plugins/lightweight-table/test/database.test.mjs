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

test("dashboard counts match the query for the same filter", async () => {
  const { store } = await seededStore("db-dashboard");
  try {
    const dashboard = store.dashboard();
    assert.equal(dashboard.row_count, 3);
    assert.equal(dashboard.scope.matched_count, 3);
    assert.equal(dashboard.scope.total_count, 3);
    assert.equal(dashboard.totals.principal.value, "1200.50");
    assert.equal(dashboard.totals.principal.currency, "CNY");
    assert.equal(dashboard.totals.remaining_due.value, "0.00");
    assert.equal(dashboard.totals.actual_paid.value, "0.00");
    assert.equal(dashboard.due_window_days, 30);

    const byID = new Map(
      dashboard.presets.map((preset) => [preset.preset_id, preset]),
    );
    assert.equal(byID.get("all").row_count, 3);
    assert.deepEqual(byID.get("all").filters, []);
    assert.equal(byID.get("overdue").row_count, 1);
    assert.equal(byID.get("repaying").row_count, 1);

    for (const preset of dashboard.presets) {
      // One filter per preset keeps a button and the filter bar in agreement.
      assert.ok(preset.filters.length <= 1, preset.preset_id);
      const queried = store.query({ filters: preset.filters, limit: 50 });
      assert.equal(queried.matched_count, preset.row_count, preset.preset_id);
    }

    assert.equal(byID.get("bank").amount.column_id, "remaining_due");
    assert.equal(byID.get("all").amount.column_id, "principal");
    assert.equal(byID.get("all").amount.value, "1200.50");

    const statusFilter = [
      {
        column_id: "repayment_status",
        op: "eq",
        value: "overdue",
      },
    ];
    const scoped = store.dashboard({
      search: "Synthetic",
      filters: statusFilter,
    });
    const query = store.query({
      search: "Synthetic",
      filters: statusFilter,
      limit: 50,
    });
    assert.equal(scoped.scope.matched_count, query.matched_count);
    assert.equal(scoped.scope.search, "Synthetic");
    assert.deepEqual(scoped.scope.filters, statusFilter);
    for (const columnID of [
      "principal",
      "remaining_due",
      "actual_paid",
    ]) {
      const summed = store.query({
        search: "Synthetic",
        filters: statusFilter,
        limit: 50,
        sum_column_id: columnID,
      });
      assert.equal(scoped.totals[columnID].value, summed.sum.value);
    }
    assert.deepEqual(
      scoped.status.map((entry) => entry.value),
      ["overdue", "repaying", "not_started"],
    );
    assert.equal(
      scoped.status.find((entry) => entry.value === "overdue").focus,
      true,
    );
    assert.equal(
      scoped.status.find((entry) => entry.value === "repaying").row_count,
      1,
    );
    assert.throws(
      () => store.dashboard({ unexpected: true }),
      (error) => error.code === "invalid_arguments",
    );
  } finally {
    store.close();
  }
});

test("dashboard keeps the active dimension and bounds attention by date", async () => {
  const directory = await makeScratch("db-dashboard-scoped");
  const databasePath = join(directory, "tables.sqlite");
  const store = new TableStore(databasePath);
  try {
    store.seed({
      workbook_name: "Synthetic dashboard scope",
      timezone: "Asia/Singapore",
      rows: [
        {
          name: "Synthetic overdue",
          institution: "Synthetic creditor A",
          category: "financial",
          funding_source: "bank",
          principal: "1000.00",
          repayment_status: "overdue",
        },
        {
          name: "Synthetic soon",
          institution: "Synthetic creditor B",
          category: "friend",
          funding_source: "non_bank",
          principal: "500.00",
          repayment_status: "repaying",
        },
        {
          name: "Synthetic settled",
          institution: "Synthetic creditor C",
          category: "high_interest",
          funding_source: "online_financial_platform",
          principal: "300.00",
          repayment_status: "settled",
        },
        {
          name: "Synthetic later",
          institution: "Synthetic creditor D",
          category: "financial",
          funding_source: "bank",
          principal: "200.00",
          repayment_status: "repaying",
        },
      ],
    });
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: "Asia/Singapore",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).formatToParts(new Date());
    const values = Object.fromEntries(
      parts.map((part) => [part.type, part.value]),
    );
    const today = `${values.year}-${values.month}-${values.day}`;
    const shift = (days) => {
      const date = new Date(`${today}T00:00:00Z`);
      date.setUTCDate(date.getUTCDate() + days);
      return date.toISOString().slice(0, 10);
    };
    const rows = store.db
      .prepare("SELECT row_id, values_json FROM rows")
      .all()
      .map((row) => ({
        row_id: row.row_id,
        values: JSON.parse(row.values_json),
      }));
    const rawByName = {
      "Synthetic overdue": {
        plan_status: "generated",
        remaining_due: 80000,
        actual_paid: 20000,
        next_due: shift(-2),
        next_due_amount: 5000,
      },
      "Synthetic soon": {
        plan_status: "not_created",
        remaining_due: 60000,
        actual_paid: 0,
        next_due: shift(10),
        next_due_amount: null,
      },
      "Synthetic settled": {
        plan_status: "completed",
        remaining_due: 0,
        actual_paid: 30000,
        next_due: null,
        next_due_amount: null,
      },
      "Synthetic later": {
        plan_status: "not_created",
        remaining_due: 10000,
        actual_paid: 0,
        next_due: shift(31),
        next_due_amount: 1000,
      },
    };
    const update = store.db.prepare(
      "UPDATE rows SET values_json = ? WHERE row_id = ?",
    );
    for (const row of rows) {
      update.run(
        JSON.stringify({
          ...row.values,
          ...rawByName[row.values.name],
        }),
        row.row_id,
      );
    }

    const categoryFilter = [
      { column_id: "category", op: "eq", value: "financial" },
    ];
    const scoped = store.dashboard({ filters: categoryFilter });
    const queried = store.query({ filters: categoryFilter, limit: 50 });
    assert.equal(scoped.scope.matched_count, queried.matched_count);
    assert.deepEqual(
      scoped.breakdowns.category.map((entry) => entry.value).sort(),
      ["financial", "friend", "high_interest"],
    );
    assert.equal(
      scoped.breakdowns.category.find(
        (entry) => entry.value === "financial",
      ).focus,
      true,
    );
    assert.deepEqual(
      scoped.breakdowns.funding_source.map((entry) => entry.value).sort(),
      ["bank"],
    );
    assert.equal(scoped.no_plan_count, 1);

    const dashboard = store.dashboard();
    assert.deepEqual(
      dashboard.attention.map((item) => item.days),
      [-2, 10],
    );
    assert.equal(dashboard.attention[1].next_due_amount, null);
    assert.equal(dashboard.attention_total, "50.00");
    assert.ok(dashboard.attention.every((item) => item.next_due < shift(31)));

    const noPlan = store.dashboard({
      search: "Synthetic",
      filters: [
        { column_id: "plan_status", op: "eq", value: "not_created" },
      ],
    });
    assert.equal(noPlan.scope.search, "Synthetic");
    assert.equal(noPlan.no_plan_count, 2);
  } finally {
    store.close();
  }
});
