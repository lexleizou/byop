import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  configuredDatabasePath,
  mcpLaunchArgs,
} from "../pi-extension.mjs";
import { backendLaunchArgs } from "../mcp-proxy.mjs";

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

test("MCP proxy gets scoped permissions and starts the reloadable backend", () => {
  const databasePath = "/tmp/byop-table/tables.sqlite";
  const args = mcpLaunchArgs(databasePath);
  const dataDirectory = dirname(databasePath);
  assert.equal(args[0], "--permission");
  assert.ok(args.includes(`--allow-fs-read=${dataDirectory}`));
  assert.ok(args.includes(`--allow-fs-write=${dataDirectory}`));
  assert.equal(args.includes("--allow-net"), false);
  assert.equal(args.includes("--allow-child-process"), true);
  assert.ok(
    args.some((arg) =>
      arg.endsWith(join("plugins", "lightweight-table", "mcp-proxy.mjs")),
    ),
  );
  assert.deepEqual(args.slice(-4), [
    "--db",
    databasePath,
    "--generation",
    join(dataDirectory, "runtime-generation.json"),
  ]);

  const backendArgs = backendLaunchArgs(databasePath);
  assert.equal(backendArgs.includes("--allow-child-process"), false);
  assert.equal(backendArgs.includes("--allow-net"), false);
  assert.ok(
    backendArgs.some((arg) =>
      arg.endsWith(join("plugins", "lightweight-table", "cli.mjs")),
    ),
  );
  assert.deepEqual(backendArgs.slice(-3), [
    "serve-mcp",
    "--db",
    databasePath,
  ]);
});
