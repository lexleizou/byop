import assert from "node:assert/strict";
import test from "node:test";

import { agentSource, pageSource, seededStore } from "./support.mjs";

test("P15b query supports search, typed filters, sorting, exact totals, and fixed contexts", async () => {
  const { store } = await seededStore("p15b-query");
  try {
    const searched = store.query({
      search: "BETA",
      limit: 100,
      sum_column_id: "principal",
    });
    assert.equal(searched.matched_count, 1);
    assert.equal(searched.rows[0].values.name, "Synthetic product beta");
    assert.equal(searched.sum.value, "0.00");

    const sorted = store.query({
      sort: [{ column_id: "principal", direction: "desc" }],
      limit: 100,
      sum_column_id: "principal",
    });
    assert.deepEqual(
      sorted.rows.map((row) => row.values.name),
      [
        "Synthetic product alpha",
        "Synthetic product beta",
        "Synthetic product gamma",
      ],
    );
    assert.equal(sorted.sum.value, "1200.50");

    const captured = store.query({
      filters: [
        {
          column_id: "repayment_status",
          op: "not_null",
          value: null,
        },
      ],
      limit: 1,
      capture_targets: true,
    });
    assert.equal(captured.matched_count, 3);
    assert.equal(captured.rows.length, 1);
    assert.equal(captured.context.captured_count, 3);
    assert.ok(captured.context_id);

    const fromContext = store.query({
      context_id: captured.context_id,
      limit: 100,
    });
    assert.equal(fromContext.matched_count, 3);
    assert.equal(fromContext.context.changed_count, 0);
  } finally {
    store.close();
  }
});

test("P15b page command IDs replay once and fixed context versions reject later changes", async () => {
  const { store, summary } = await seededStore("p15b-page");
  try {
    const rowID = summary.row_ids[0];
    const command = {
      action: "update",
      expected_schema_revision: 2,
      targets: [{ row_id: rowID, expected_revision: 1 }],
      values: { repayment_status: "repaying" },
    };
    const source = pageSource("page-update-1");
    const committed = store.change(command, source);
    const replay = store.change(command, source);
    assert.equal(replay.operation_id, committed.operation_id);
    assert.equal(replay.replayed, true);

    assert.throws(
      () =>
        store.change(
          {
            ...command,
            values: { repayment_status: "settled" },
          },
          source,
        ),
      (error) => error.code === "command_id_conflict",
    );

    const context = store.query({
      limit: 100,
      capture_targets: true,
    });
    const secondRowID = summary.row_ids[1];
    store.change(
      {
        action: "update",
        expected_schema_revision: 2,
        targets: [{ row_id: secondRowID, expected_revision: 1 }],
        values: { repayment_status: "settled" },
      },
      pageSource("page-update-2"),
    );

    assert.throws(
      () =>
        store.change(
          {
            action: "update",
            expected_schema_revision: 2,
            context_id: context.context_id,
            values: { repayment_status: "overdue" },
          },
          pageSource("page-update-3"),
        ),
      (error) =>
        error.code === "version_conflict" &&
        error.details?.row_id === secondRowID,
    );
    const unchanged = store.query({
      row_ids: [rowID],
      limit: 1,
    });
    assert.equal(unchanged.rows[0].values.repayment_status, "repaying");
  } finally {
    store.close();
  }
});

test("P15b undo is atomic, unique per operation, and rejects later edits", async () => {
  const { store, summary } = await seededStore("p15b-undo");
  try {
    const rowID = summary.row_ids[0];
    const original = store.change(
      {
        action: "update",
        expected_schema_revision: 2,
        targets: [{ row_id: rowID, expected_revision: 1 }],
        values: { repayment_status: "repaying" },
      },
      agentSource("undo-source-1"),
    );
    const undoSource = pageSource("undo-command-1");
    const undone = store.undo(
      { operation_id: original.operation_id },
      undoSource,
    );
    assert.equal(undone.undo_of, original.operation_id);
    assert.equal(undone.affected_count, 1);
    const repeated = store.undo(
      { operation_id: original.operation_id },
      pageSource("undo-command-2", "view-instance-2"),
    );
    assert.equal(repeated.operation_id, undone.operation_id);
    assert.equal(repeated.replayed, true);

    const restored = store.query({ row_ids: [rowID], limit: 1 });
    assert.equal(restored.rows[0].values.repayment_status, "not_started");
    assert.equal(restored.rows[0].revision, 3);

    const second = store.change(
      {
        action: "update",
        expected_schema_revision: 2,
        targets: [{ row_id: summary.row_ids[1], expected_revision: 1 }],
        values: { repayment_status: "overdue" },
      },
      agentSource("undo-source-2"),
    );
    store.change(
      {
        action: "update",
        expected_schema_revision: 2,
        targets: [{ row_id: summary.row_ids[1], expected_revision: 2 }],
        values: { repayment_status: "settled" },
      },
      agentSource("undo-source-3"),
    );
    assert.throws(
      () =>
        store.undo(
          { operation_id: second.operation_id },
          agentSource("undo-source-4"),
        ),
      (error) => error.code === "version_conflict",
    );

    const history = store.history({ limit: 20 });
    const originalHistory = history.operations.find(
      (operation) => operation.operation_id === original.operation_id,
    );
    assert.equal(originalHistory.undo.operation_id, undone.operation_id);
    assert.equal(originalHistory.undoable, false);
  } finally {
    store.close();
  }
});

test("P15b undo reverses insert and delete without restoring old revision numbers", async () => {
  const { store, summary } = await seededStore("p15b-undo-kinds");
  try {
    const inserted = store.change(
      {
        action: "insert",
        expected_schema_revision: 2,
        rows: [{ name: "Synthetic undo insert" }],
      },
      pageSource("insert-command-1"),
    );
    store.undo(
      { operation_id: inserted.operation_id },
      pageSource("insert-undo-1"),
    );
    assert.equal(
      store.query({
        filters: [
          {
            column_id: "name",
            op: "eq",
            value: "Synthetic undo insert",
          },
        ],
        limit: 100,
      }).matched_count,
      0,
    );

    const deleted = store.change(
      {
        action: "delete",
        expected_schema_revision: 2,
        targets: [{ row_id: summary.row_ids[2], expected_revision: 1 }],
      },
      pageSource("delete-command-1"),
    );
    store.undo(
      { operation_id: deleted.operation_id },
      pageSource("delete-undo-1"),
    );
    const restored = store.query({
      row_ids: [summary.row_ids[2]],
      limit: 1,
    });
    assert.equal(restored.matched_count, 1);
    assert.equal(restored.rows[0].revision, 3);
  } finally {
    store.close();
  }
});
