import assert from "node:assert/strict";
import test from "node:test";

import { createMcpHandler } from "../src/mcp-server.mjs";
import { agentSource, seededStore } from "./support.mjs";

test("MCP publishes business schemas without _byop and injects source only at call time", async () => {
  const { store, databasePath } = await seededStore("mcp");
  store.close();
  const handler = createMcpHandler(databasePath);
  try {
    const list = handler.handle({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/list",
      params: {},
    });
    assert.deepEqual(
      list.result.tools.map((tool) => tool.name),
      [
        "table_describe",
        "table_query",
        "table_debt_details",
        "table_change",
        "table_undo",
        "table_history",
      ],
    );
    assert.equal(
      JSON.stringify(list.result.tools).includes("_byop"),
      false,
    );
    assert.match(
      list.result.tools.find((tool) => tool.name === "table_query").description,
      /\[byop-table-context:<contextId>\]/,
    );

    const unavailable = handler.handle({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: {
        name: "table_change",
        arguments: {
          action: "insert",
          expected_schema_revision: 2,
          rows: [{ name: "Synthetic unavailable source" }],
          _byop: { version: 1, source_kind: "unavailable" },
        },
      },
    });
    assert.equal(unavailable.result.isError, true);
    assert.equal(unavailable.result.structuredContent.code, "source_unavailable");

    const changed = handler.handle({
      jsonrpc: "2.0",
      id: 3,
      method: "tools/call",
      params: {
        name: "table_change",
        arguments: {
          action: "insert",
          expected_schema_revision: 2,
          rows: [{ name: "Synthetic direct MCP source" }],
          _byop: agentSource("mcp-message"),
        },
      },
    });
    assert.equal(changed.result.isError, false);
    assert.equal(changed.result.structuredContent.affected_count, 1);

    const history = handler.handle({
      jsonrpc: "2.0",
      id: 4,
      method: "tools/call",
      params: {
        name: "table_history",
        arguments: {},
      },
    });
    assert.equal(history.result.structuredContent.count, 1);
    assert.deepEqual(
      history.result.structuredContent.operations[0].source_message_ids,
      ["mcp-message"],
    );

    const undone = handler.handle({
      jsonrpc: "2.0",
      id: 5,
      method: "tools/call",
      params: {
        name: "table_undo",
        arguments: {
          operation_id: changed.result.structuredContent.operation_id,
          _byop: agentSource("mcp-undo-message"),
        },
      },
    });
    assert.equal(undone.result.isError, false);
    assert.equal(
      undone.result.structuredContent.undo_of,
      changed.result.structuredContent.operation_id,
    );

    const captured = handler.handle({
      jsonrpc: "2.0",
      id: 6,
      method: "tools/call",
      params: {
        name: "table_query",
        arguments: {
          capture_targets: true,
          limit: 100,
          _byop: agentSource("mcp-context-message"),
        },
      },
    });
    assert.equal(captured.result.isError, false);
    assert.ok(captured.result.structuredContent.context_id);

    const contextQuery = handler.handle({
      jsonrpc: "2.0",
      id: 7,
      method: "tools/call",
      params: {
        name: "table_query",
        arguments: {
          context_id: captured.result.structuredContent.context_id,
          limit: 100,
        },
      },
    });
    assert.equal(contextQuery.result.isError, false);
    assert.equal(
      contextQuery.result.structuredContent.context.captured_count,
      3,
    );
  } finally {
    handler.close();
  }
});
