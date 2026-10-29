import { mkdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

import { TableStore } from "./src/database.mjs";
import { TableError, asTableError } from "./src/errors.mjs";

const input = createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

const HERE = dirname(fileURLToPath(import.meta.url));
const LOCALE_FILES = new Map([
  ["en", join(HERE, "l10n", "en.json")],
  ["zh-Hans", join(HERE, "l10n", "zh-Hans.json")],
]);
const localeCache = new Map();
const pendingHostCalls = new Map();
let nextHostRequestID = 1;
let store = null;

function send(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function dataDirectory() {
  const directory = process.env.BYOP_PLUGIN_DATA_DIR;
  if (typeof directory !== "string" || directory.trim() === "") {
    throw new TableError(
      "data_directory_unavailable",
      "BYOP_PLUGIN_DATA_DIR is required.",
    );
  }
  mkdirSync(directory, { recursive: true });
  return directory;
}

function tableStore() {
  if (!store) {
    store = new TableStore(join(dataDirectory(), "tables.sqlite"));
  }
  return store;
}

function localeMessages(locale) {
  const selected = LOCALE_FILES.has(locale) ? locale : "en";
  if (!localeCache.has(selected)) {
    localeCache.set(
      selected,
      JSON.parse(readFileSync(LOCALE_FILES.get(selected), "utf8")),
    );
  }
  return {
    locale: selected,
    messages: localeCache.get(selected),
  };
}

function requestHost(method, params = {}) {
  const id = `lightweight-table-host-${nextHostRequestID++}`;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      pendingHostCalls.delete(id);
      reject(new TableError("host_timeout", `${method} timed out.`));
    }, 8000);
    pendingHostCalls.set(id, { resolve, reject, timeout });
    send({ jsonrpc: "2.0", id, method, params });
  });
}

function requiredText(value, label) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new TableError("invalid_arguments", `${label} is required.`);
  }
  return value;
}

function pageSource(viewInstanceId, commandId) {
  return {
    version: 1,
    source_kind: "page",
    view_instance_id: requiredText(viewInstanceId, "viewInstanceId"),
    client_command_id: requiredText(commandId, "commandId"),
  };
}

function selectedQuery(query, selectedRowIds) {
  const next = { ...(query ?? {}) };
  if (selectedRowIds !== undefined) {
    if (
      !Array.isArray(selectedRowIds) ||
      selectedRowIds.some((rowID) => typeof rowID !== "string" || rowID === "")
    ) {
      throw new TableError(
        "invalid_arguments",
        "selectedRowIds must be an array of row IDs.",
      );
    }
    if (selectedRowIds.length > 0) {
      next.row_ids = selectedRowIds;
    }
  }
  return next;
}

async function handlePageMessage(message) {
  if (!message || typeof message !== "object" || Array.isArray(message)) {
    throw new TableError("invalid_arguments", "message must be an object.");
  }
  switch (message.action) {
    case "table.locale":
      return localeMessages(message.locale);
    case "table.bootstrap": {
      const current = tableStore();
      return {
        description: current.describe(),
        summary: current.summary(),
      };
    }
    case "table.query":
      return tableStore().query(message.query ?? {});
    case "table.dashboard":
      return tableStore().dashboard();
    case "table.detail":
      return tableStore().detail({
        row_id: message.rowId ?? message.row_id,
      });
    case "table.version":
      return tableStore().summary();
    case "table.change":
      return tableStore().change(
        message.change,
        pageSource(message.viewInstanceId, message.commandId),
      );
    case "table.capture_context":
      return tableStore().query({
        ...selectedQuery(message.query, message.selectedRowIds),
        capture_targets: true,
      });
    case "table.history":
      return tableStore().history(message.query ?? {});
    case "table.undo":
      return tableStore().undo(
        {
          operation_id: message.operationId,
          dry_run: message.dryRun === true,
        },
        pageSource(message.viewInstanceId, message.commandId),
      );
    case "selection.open_chat": {
      const context = tableStore().query({
        ...selectedQuery(message.query, message.selectedRowIds),
        capture_targets: true,
      });
      if (!context.context_id || context.context.captured_count === 0) {
        throw new TableError(
          "empty_context",
          "Select at least one record before opening chat.",
        );
      }
      const hostResult = await requestHost("host.openChat", {
        text: requiredText(message.text, "text"),
        contextId: context.context_id,
        label: requiredText(message.label, "label"),
      });
      return {
        context: context.context,
        host: hostResult ?? {},
      };
    }
    default:
      throw new TableError(
        "unknown_action",
        `Unknown lightweight-table action ${JSON.stringify(message.action)}.`,
      );
  }
}

async function handleMessage(message) {
  if (!message || typeof message !== "object") {
    return;
  }
  if (!message.method && Object.hasOwn(message, "id")) {
    const pending = pendingHostCalls.get(message.id);
    if (!pending) {
      return;
    }
    pendingHostCalls.delete(message.id);
    clearTimeout(pending.timeout);
    if (message.error) {
      pending.reject(
        new TableError(
          "host_call_failed",
          message.error.message || "Host call failed.",
        ),
      );
    } else {
      pending.resolve(message.result);
    }
    return;
  }
  if (typeof message.method !== "string") {
    return;
  }

  const reply = { jsonrpc: "2.0", id: message.id };
  try {
    switch (message.method) {
      case "initialize":
      case "activate":
        reply.result = {};
        break;
      case "deactivate":
        reply.result = {};
        break;
      case "webviewMessage":
        reply.result = await handlePageMessage(message.params?.message);
        break;
      default:
        reply.error = {
          code: -32601,
          message: `Unknown method: ${message.method}`,
        };
    }
  } catch (error) {
    const tableError = asTableError(error);
    reply.error = {
      code: -32000,
      message: JSON.stringify({
        code: tableError.code,
        message: tableError.message,
        details: tableError.details,
      }),
    };
  }
  if (reply.id !== undefined) {
    send(reply);
  }
}

function close() {
  if (store) {
    store.close();
    store = null;
  }
}

input.on("line", (line) => {
  try {
    void handleMessage(JSON.parse(line));
  } catch {
    // Invalid lines cannot be correlated with a host request.
  }
});

input.on("close", close);
process.on("SIGTERM", () => {
  close();
  process.exit(0);
});
process.on("SIGINT", () => {
  close();
  process.exit(0);
});
