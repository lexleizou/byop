import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createInterface } from "node:readline";
import test from "node:test";

import { agentSource, McpProcess, seededStore } from "./support.mjs";

test("two MCP processes commit and replay one source command once", async () => {
  const { store, databasePath } = await seededStore("mcp-concurrent");
  store.close();
  const first = await new McpProcess(databasePath).start();
  const second = await new McpProcess(databasePath).start();
  try {
    const args = {
      action: "insert",
      expected_schema_revision: 1,
      rows: [
        {
          name: "Synthetic concurrent insert",
          category: "financial",
          funding_source: "bank",
          repayment_status: "not_started",
        },
      ],
      _byop: agentSource("concurrent-message"),
    };
    const [left, right] = await Promise.all([
      first.callTool("table_change", args),
      second.callTool("table_change", args),
    ]);
    assert.equal(left.isError, false);
    assert.equal(right.isError, false);
    assert.equal(
      left.structuredContent.operation_id,
      right.structuredContent.operation_id,
    );
    assert.equal(
      [left.structuredContent.replayed, right.structuredContent.replayed].filter(Boolean)
        .length,
      1,
    );

    const history = await first.callTool("table_history", {});
    assert.equal(history.structuredContent.count, 1);
    const query = await first.callTool("table_query", {
      filters: [
        {
          column_id: "name",
          op: "eq",
          value: "Synthetic concurrent insert",
        },
      ],
    });
    assert.equal(query.structuredContent.matched_count, 1);
  } finally {
    await Promise.all([first.close(), second.close()]);
  }
});

test("a competing writer fails after the bounded busy wait without a receipt", async () => {
  const { store, databasePath } = await seededStore("mcp-busy");
  store.close();
  const client = await new McpProcess(databasePath).start();
  const blocker = spawn(
    process.execPath,
    [
      "--input-type=module",
      "-e",
      `
        import { DatabaseSync } from "node:sqlite";
        const db = new DatabaseSync(process.argv[1]);
        db.exec("PRAGMA busy_timeout = 3000");
        db.exec("BEGIN IMMEDIATE");
        process.stdout.write("READY\\n");
        setTimeout(() => {
          db.exec("ROLLBACK");
          db.close();
        }, 4500);
      `,
      databasePath,
    ],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  try {
    const lines = createInterface({ input: blocker.stdout, crlfDelay: Infinity });
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error("Lock holder did not become ready.")),
        3000,
      );
      lines.once("line", (line) => {
        clearTimeout(timeout);
        if (line === "READY") {
          resolve();
        } else {
          reject(new Error("Unexpected lock holder output."));
        }
      });
    });

    const startedAt = Date.now();
    const result = await client.callTool("table_change", {
      action: "insert",
      expected_schema_revision: 1,
      rows: [{ name: "Synthetic busy insert" }],
      _byop: agentSource("busy-message"),
    });
    const elapsed = Date.now() - startedAt;
    assert.equal(result.isError, true);
    assert.equal(result.structuredContent.code, "busy");
    assert.ok(elapsed >= 2800, `busy wait ended too early after ${elapsed} ms`);
    assert.ok(elapsed < 6000, `busy wait exceeded the bounded window: ${elapsed} ms`);
    const history = await client.callTool("table_history", {});
    assert.equal(history.structuredContent.count, 0);
  } finally {
    await client.close();
    if (blocker.exitCode === null) {
      blocker.kill("SIGTERM");
      await once(blocker, "exit");
    }
  }
});
