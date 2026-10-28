import { createInterface } from "node:readline";

import { TableStore, columnDefinitions } from "./database.mjs";
import { TableError, asTableError } from "./errors.mjs";

const SUPPORTED_PROTOCOL_VERSIONS = new Set([
  "2025-11-25",
  "2025-06-18",
  "2025-03-26",
  "2024-11-05",
]);

const columnProperties = Object.fromEntries(
  columnDefinitions.map((column) => [
    column.column_id,
    column.type === "number"
      ? {
          type: ["string", "null"],
          description: `${column.label}; decimal string with ${column.config.scale} places.`,
        }
      : column.type === "date"
        ? {
            type: ["string", "null"],
            pattern: "^\\d{4}-\\d{2}-\\d{2}$",
            description: `${column.label}; calendar date or null.`,
          }
        : {
            type: ["string", "null"],
            description: column.label,
          },
  ]),
);

const tools = [
  {
    name: "table_describe",
    description:
      "Describe the fixed local bank table, its stable column IDs, revisions, and timezone.",
    inputSchema: {
      type: "object",
      properties: {
        table_id: { type: "string" },
      },
      additionalProperties: false,
    },
  },
  {
    name: "table_query",
    description:
      "Read rows from the local bank table by filters or a fixed context. Use stable row and column IDs; no arbitrary SQL. When a user prompt ends with [byop-table-context:<contextId>], pass that value as context_id.",
    inputSchema: {
      type: "object",
      properties: {
        table_id: { type: "string" },
        row_ids: {
          type: "array",
          items: { type: "string" },
          minItems: 1,
        },
        context_id: {
          type: "string",
          description:
            "Fixed context ID, including the value from [byop-table-context:<contextId>].",
        },
        filters: {
          type: "array",
          items: {
            type: "object",
            properties: {
              column_id: { type: "string" },
              op: {
                type: "string",
                enum: [
                  "eq",
                  "neq",
                  "contains",
                  "not_contains",
                  "is_null",
                  "not_null",
                  "gt",
                  "gte",
                  "lt",
                  "lte",
                  "between",
                ],
              },
              value: {},
            },
            required: ["column_id", "op"],
            additionalProperties: false,
          },
        },
        search: { type: "string" },
        sort: {
          type: "array",
          items: {
            type: "object",
            properties: {
              column_id: { type: "string" },
              direction: { type: "string", enum: ["asc", "desc"] },
            },
            required: ["column_id", "direction"],
            additionalProperties: false,
          },
        },
        projection: {
          type: "array",
          items: { type: "string" },
          minItems: 1,
        },
        limit: { type: "integer", minimum: 1, maximum: 100 },
        offset: { type: "integer", minimum: 0 },
        sum_column_id: { type: "string" },
        capture_targets: { type: "boolean" },
      },
      additionalProperties: false,
    },
  },
  {
    name: "table_change",
    description:
      "Preview or commit one insert, update, or delete command. Updates and deletes require expected row revisions.",
    inputSchema: {
      type: "object",
      properties: {
        action: { type: "string", enum: ["insert", "update", "delete"] },
        table_id: { type: "string" },
        rows: {
          type: "array",
          items: {
            type: "object",
            properties: columnProperties,
            required: ["name"],
            additionalProperties: false,
          },
          minItems: 1,
        },
        targets: {
          type: "array",
          items: {
            type: "object",
            properties: {
              row_id: { type: "string" },
              expected_revision: { type: "integer", minimum: 1 },
            },
            required: ["row_id", "expected_revision"],
            additionalProperties: false,
          },
          minItems: 1,
        },
        context_id: { type: "string" },
        values: {
          type: "object",
          properties: columnProperties,
          additionalProperties: false,
        },
        expected_schema_revision: { type: "integer", minimum: 1 },
        dry_run: { type: "boolean" },
      },
      required: ["action", "expected_schema_revision"],
      additionalProperties: false,
    },
  },
  {
    name: "table_undo",
    description:
      "Undo one committed data operation. Later changes to any target cause a version conflict.",
    inputSchema: {
      type: "object",
      properties: {
        operation_id: { type: "string" },
        dry_run: { type: "boolean" },
      },
      required: ["operation_id"],
      additionalProperties: false,
    },
  },
  {
    name: "table_history",
    description:
      "Read successful change receipts, including their Pi source session and message IDs.",
    inputSchema: {
      type: "object",
      properties: {
        operation_id: { type: "string" },
        source_session_id: { type: "string" },
        message_id: { type: "string" },
        limit: { type: "integer", minimum: 1, maximum: 200 },
        offset: { type: "integer", minimum: 0 },
      },
      additionalProperties: false,
    },
  },
];

function writeMessage(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function response(id, result) {
  return { jsonrpc: "2.0", id, result };
}

function errorResponse(id, code, message, data = undefined) {
  const error = { code, message };
  if (data !== undefined) {
    error.data = data;
  }
  return { jsonrpc: "2.0", id, error };
}

function toolResult(value) {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(value),
      },
    ],
    structuredContent: value,
    isError: false,
  };
}

function toolError(error) {
  const tableError = asTableError(error);
  const payload = {
    code: tableError.code,
    message: tableError.message,
    details: tableError.details,
  };
  return {
    content: [{ type: "text", text: JSON.stringify(payload) }],
    structuredContent: payload,
    isError: true,
  };
}

function extractSource(args) {
  if (!args || typeof args !== "object" || Array.isArray(args)) {
    return { business: {}, source: null };
  }
  const { _byop: source = null, ...business } = args;
  return { business, source };
}

export function createMcpHandler(databasePath) {
  const store = new TableStore(databasePath);
  return {
    handle(message) {
      if (!message || message.jsonrpc !== "2.0" || typeof message.method !== "string") {
        return errorResponse(message?.id ?? null, -32600, "Invalid JSON-RPC request.");
      }
      if (message.id === undefined) {
        return null;
      }
      switch (message.method) {
        case "initialize": {
          const requested = message.params?.protocolVersion;
          const protocolVersion = SUPPORTED_PROTOCOL_VERSIONS.has(requested)
            ? requested
            : "2025-11-25";
          return response(message.id, {
            protocolVersion,
            capabilities: { tools: { listChanged: false } },
            serverInfo: { name: "lightweight_table", version: "0.2.1" },
          });
        }
        case "ping":
          return response(message.id, {});
        case "tools/list":
          return response(message.id, { tools });
        case "tools/call": {
          const name = message.params?.name;
          const { business, source } = extractSource(message.params?.arguments);
          try {
            switch (name) {
              case "table_describe":
                return response(message.id, toolResult(store.describe(source)));
              case "table_query":
                return response(message.id, toolResult(store.query(business, source)));
              case "table_change":
                return response(message.id, toolResult(store.change(business, source)));
              case "table_undo":
                return response(message.id, toolResult(store.undo(business, source)));
              case "table_history":
                return response(message.id, toolResult(store.history(business)));
              default:
                return response(
                  message.id,
                  toolError(
                    new TableError(
                      "unknown_tool",
                      `Unknown tool ${JSON.stringify(name)}.`,
                    ),
                  ),
                );
            }
          } catch (error) {
            return response(message.id, toolError(error));
          }
        }
        default:
          return errorResponse(message.id, -32601, "Method not found.");
      }
    },
    close() {
      store.close();
    },
  };
}

export async function serveStdio(databasePath) {
  const handler = createMcpHandler(databasePath);
  const lines = createInterface({
    input: process.stdin,
    crlfDelay: Infinity,
  });
  for await (const line of lines) {
    if (line.trim() === "") {
      continue;
    }
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      writeMessage(errorResponse(null, -32700, "Parse error."));
      continue;
    }
    const reply = handler.handle(message);
    if (reply) {
      writeMessage(reply);
    }
  }
  handler.close();
}
