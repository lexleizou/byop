import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import test from "node:test";

import {
  configuredDatabasePath,
  controlDirectoryPath,
  runtimeStatePath,
  readRuntimeState,
  mcpLaunchArgs,
  default as lightweightTableExtension,
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

test("disabled marker revokes the MCP server at a turn boundary", async () => {
  const root = await mkdtemp(join(tmpdir(), "byop-pi-ext-"));
  const dataDir = join(root, "data");
  await mkdir(dataDir, { recursive: true });
  const env = {
    ...process.env,
    BYOP_PLUGIN_DATA_DIRS: JSON.stringify({
      "local.lightweight-table": dataDir,
    }),
  };
  const statePath = runtimeStatePath(env);
  const savedEnv = { ...process.env };
  Object.assign(process.env, env);
  try {
    const calls = [];
    const handlers = {};
    const pi = {
      registerMcpServer: (name) => calls.push(["register", name]),
      unregisterMcpServer: (name) => calls.push(["unregister", name]),
      on: (event, handler) => {
        handlers[event] = handler;
      },
    };
    lightweightTableExtension(pi);
    assert.deepEqual(calls[0], ["register", "lightweight_table"]);
    assert.equal(calls.length, 1);

    await writeFile(statePath, JSON.stringify({ enabled: false }), "utf8");
    handlers.turn_start();
    assert.deepEqual(calls[1], ["unregister", "lightweight_table"]);
    assert.equal(calls.length, 2);

    const result = handlers.tool_call(
      { toolName: "mcp__lightweight_table__read_rows", input: {}, toolCallId: "1" },
      {},
    );
    assert.equal(result?.block, true);

    await writeFile(statePath, JSON.stringify({ enabled: true }), "utf8");
    handlers.turn_start();
    assert.deepEqual(calls[2], ["register", "lightweight_table"]);
    assert.equal(calls.length, 3);
  } finally {
    Object.assign(process.env, savedEnv);
    await rm(root, { recursive: true, force: true });
  }
});

test("BYOP_LIGHTWEIGHT_TABLE_DB does not redirect the control directory", async () => {
  const dataDir = "/tmp/byop-plugin-data";
  const control = controlDirectoryPath({
    BYOP_LIGHTWEIGHT_TABLE_DB: "/tmp/synthetic/tables.sqlite",
    BYOP_PLUGIN_DATA_DIRS: JSON.stringify({
      "local.lightweight-table": dataDir,
    }),
  });
  assert.equal(control, dataDir);
  const args = mcpLaunchArgs("/tmp/synthetic/tables.sqlite", control);
  assert.equal(args.filter((arg) => arg === "--db").length, 1);
  assert.ok(args.includes(join(dataDir, "runtime-generation.json")));
});

test("re-register waits for direct MCP tools before the first request", async () => {
  const root = await mkdtemp(join(tmpdir(), "byop-pi-ready-"));
  const dataDir = join(root, "data");
  await mkdir(dataDir, { recursive: true });
  const env = {
    ...process.env,
    BYOP_PLUGIN_DATA_DIRS: JSON.stringify({
      "local.lightweight-table": dataDir,
    }),
    BYOP_MCP_READY_MS: "300",
  };
  const statePath = runtimeStatePath(env);
  const savedEnv = { ...process.env };
  Object.assign(process.env, env);
  try {
    const handlers = {};
    let registered = false;
    const pi = {
      registerMcpServer: () => {
        registered = true;
      },
      unregisterMcpServer: () => {
        registered = false;
      },
      getAllTools: () =>
        registered
          ? [
              {
                name: "mcp__lightweight_table__read_rows",
                exposure: "direct",
              },
            ]
          : [],
      on: (event, handler) => {
        handlers[event] = handler;
      },
    };
    lightweightTableExtension(pi);
    assert.equal(registered, true);

    await writeFile(statePath, JSON.stringify({ enabled: false }), "utf8");
    handlers.turn_start();
    assert.equal(registered, false);

    await writeFile(statePath, JSON.stringify({ enabled: true }), "utf8");
    handlers.turn_start();
    assert.equal(registered, true);

    await handlers.before_agent_start(
      { messageType: "user" },
      { signal: undefined, sessionManager: { getBranch: () => [] } },
    );
    assert.equal(
      pi.getAllTools().some((tool) => tool.exposure === "direct"),
      true,
    );
  } finally {
    Object.assign(process.env, savedEnv);
    await rm(root, { recursive: true, force: true });
  }
});
test("readiness wait runs once per re-register", async () => {
  const root = await mkdtemp(join(tmpdir(), "byop-pi-once-"));
  const dataDir = join(root, "data");
  await mkdir(dataDir, { recursive: true });
  const env = {
    ...process.env,
    BYOP_PLUGIN_DATA_DIRS: JSON.stringify({
      "local.lightweight-table": dataDir,
    }),
  };
  const statePath = runtimeStatePath(env);
  const savedEnv = { ...process.env };
  Object.assign(process.env, env);
  try {
    const handlers = {};
    let registered = false;
    const pi = {
      registerMcpServer: () => {
        registered = true;
      },
      unregisterMcpServer: () => {
        registered = false;
      },
      getAllTools: () =>
        registered
          ? [{ name: "mcp__lightweight_table__read_rows", exposure: "direct" }]
          : [],
      on: (event, handler) => {
        handlers[event] = handler;
      },
    };
    lightweightTableExtension(pi);
    await handlers.before_agent_start(
      { messageType: "user" },
      { signal: undefined, sessionManager: { getBranch: () => [] } },
    );
    // A second prompt while still enabled must not wait again.
    const started = Date.now();
    await handlers.before_agent_start(
      { messageType: "user" },
      { signal: undefined, sessionManager: { getBranch: () => [] } },
    );
    assert.ok(Date.now() - started < 100, "second prompt waited again");
  } finally {
    Object.assign(process.env, savedEnv);
    await rm(root, { recursive: true, force: true });
  }
});
