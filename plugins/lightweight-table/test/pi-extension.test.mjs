import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  configuredDatabasePath,
  mcpLaunchArgs,
} from "../pi-extension.mjs";

test("Pi extension resolves the plugin data map and preserves the test override", () => {
  const mapped = configuredDatabasePath({
    BYOP_PLUGIN_DATA_DIRS: JSON.stringify({
      "local.lightweight-table": "/tmp/byop-plugin-data",
    }),
  });
  assert.equal(mapped, "/tmp/byop-plugin-data/tables.sqlite");

  const overridden = configuredDatabasePath({
    BYOP_LIGHTWEIGHT_TABLE_DB: "/tmp/synthetic/tables.sqlite",
    BYOP_PLUGIN_DATA_DIRS: JSON.stringify({
      "local.lightweight-table": "/tmp/ignored",
    }),
  });
  assert.equal(overridden, "/tmp/synthetic/tables.sqlite");
});

test("MCP child gets scoped Node permissions for package reads and its data directory", () => {
  const databasePath = "/tmp/byop-table/tables.sqlite";
  const args = mcpLaunchArgs(databasePath);
  const dataDirectory = dirname(databasePath);
  assert.equal(args[0], "--permission");
  assert.ok(args.includes(`--allow-fs-read=${dataDirectory}`));
  assert.ok(args.includes(`--allow-fs-write=${dataDirectory}`));
  assert.equal(args.includes("--allow-net"), false);
  assert.equal(args.includes("--allow-child-process"), false);
  assert.ok(
    args.some((arg) =>
      arg.endsWith(join("plugins", "lightweight-table", "cli.mjs")),
    ),
  );
  assert.deepEqual(args.slice(-3), ["serve-mcp", "--db", databasePath]);
});
