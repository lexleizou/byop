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
      ["table_describe", "table_query", "table_change", "table_history"],
    );
    assert.equal(
      JSON.stringify(list.result.tools).includes("_byop"),
      false,
    );

    const unavailable = handler.handle({
      jsonrpc: "2.0",
      id: 2,
      method: "tools/call",
      params: {
        name: "table_change",
        arguments: {
          action: "insert",
          expected_schema_revision: 1,
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
          expected_schema_revision: 1,
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
  } finally {
    handler.close();
  }
});
