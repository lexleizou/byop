#!/usr/bin/env node

import { readFile } from "node:fs/promises";

import { TableStore } from "./src/database.mjs";
import { asTableError } from "./src/errors.mjs";
import { createMcpHandler, serveStdio } from "./src/mcp-server.mjs";
import {
  importV4Snapshot,
  inspectAndBackupV4Database,
  loadV4Snapshot,
} from "./src/v4-import.mjs";

function parseArgs(argv) {
  const [command, ...rest] = argv;
  const options = { command };
  for (let index = 0; index < rest.length; index += 1) {
    const arg = rest[index];
    if (!arg.startsWith("--")) {
      throw new Error(`Unexpected argument ${JSON.stringify(arg)}.`);
    }
    const name = arg.slice(2);
    if (name === "if-empty") {
      options.ifEmpty = true;
      continue;
    }
    if (name === "replace-unmodified") {
      options.replaceUnmodified = true;
      continue;
    }
    const value = rest[index + 1];
    if (value === undefined || value.startsWith("--")) {
      throw new Error(`Missing value for --${name}.`);
    }
    options[name] = value;
    index += 1;
  }
  return options;
}

async function readJSON(path) {
  return JSON.parse(await readFile(path, "utf8"));
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  switch (options.command) {
    case "seed": {
      if (!options.db || !options.input) {
        throw new Error("seed requires --db and --input.");
      }
      const input = await readJSON(options.input);
      const store = new TableStore(options.db);
      try {
        const summary = store.seed(input, { ifEmpty: options.ifEmpty === true });
        process.stdout.write(
          `${JSON.stringify({
            ok: true,
            seeded: summary.seeded,
            workbook_id: summary.workbook_id,
            table_id: summary.table_id,
            schema_revision: summary.schema_revision,
            data_revision: summary.data_revision,
            row_count: summary.row_count,
            row_ids: summary.row_ids ?? [],
          })}\n`,
        );
      } finally {
        store.close();
      }
      return;
    }
    case "import-v4": {
      if (!options.db || !options["input-dir"]) {
        throw new Error("import-v4 requires --db and --input-dir.");
      }
      const snapshot = loadV4Snapshot(options["input-dir"]);
      const prepared = inspectAndBackupV4Database(options.db, {
        replaceUnmodified: options.replaceUnmodified === true,
      });
      const store = new TableStore(options.db);
      try {
        const summary = importV4Snapshot(store, {
          inputDirectory: options["input-dir"],
          replaceUnmodified: options.replaceUnmodified === true,
          snapshot,
          backupBeforeReplace: false,
          preparedBackup: prepared.backup,
        });
        process.stdout.write(`${JSON.stringify(summary)}\n`);
      } finally {
        store.close();
      }
      return;
    }
    case "serve-mcp": {
      if (!options.db) {
        throw new Error("serve-mcp requires --db.");
      }
      await serveStdio(options.db);
      return;
    }
    case "call": {
      if (!options.db || !options.tool || !options.arguments) {
        throw new Error("call requires --db, --tool, and --arguments.");
      }
      const handler = createMcpHandler(options.db);
      try {
        const args = JSON.parse(options.arguments);
        if (options.source) {
          args._byop = JSON.parse(options.source);
        }
        const reply = handler.handle({
          jsonrpc: "2.0",
          id: 1,
          method: "tools/call",
          params: { name: options.tool, arguments: args },
        });
        if (reply?.error) {
          throw new Error(reply.error.message);
        }
        process.stdout.write(`${JSON.stringify(reply.result)}\n`);
      } finally {
        handler.close();
      }
      return;
    }
    default:
      throw new Error(
        "Usage: cli.mjs <seed|import-v4|serve-mcp|call> [--db path] [options]",
      );
  }
}

main().catch((error) => {
  const tableError = asTableError(error);
  process.stderr.write(
    `${JSON.stringify({
      code: tableError.code,
      message: tableError.message,
      details: tableError.details,
    })}\n`,
  );
  process.exitCode = 1;
});
