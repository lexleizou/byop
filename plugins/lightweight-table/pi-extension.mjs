import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { LIGHTWEIGHT_TABLE_MCP_PREFIX, SourceTracker } from "./src/source-tracker.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const MCP_PROXY = join(HERE, "mcp-proxy.mjs");
const MCP_SERVER_NAME = "lightweight_table";
const PLUGIN_ID = "local.lightweight-table";
const DEFAULT_READY_WAIT_MS = 1500;
const DEFAULT_READY_POLL_MS = 20;

export function configuredDatabasePath(env = process.env) {
  const override = env.BYOP_LIGHTWEIGHT_TABLE_DB;
  if (typeof override === "string" && override.trim() !== "") {
    return resolve(override);
  }
  const rawDirectories = env.BYOP_PLUGIN_DATA_DIRS;
  if (typeof rawDirectories !== "string" || rawDirectories.trim() === "") {
    throw new Error(
      "BYOP_PLUGIN_DATA_DIRS must include local.lightweight-table.",
    );
  }
  let directories;
  try {
    directories = JSON.parse(rawDirectories);
  } catch (error) {
    throw new Error(`BYOP_PLUGIN_DATA_DIRS is not valid JSON: ${error.message}`);
  }
  const dataDirectory = directories?.[PLUGIN_ID];
  if (typeof dataDirectory !== "string" || dataDirectory.trim() === "") {
    throw new Error(
      "BYOP_PLUGIN_DATA_DIRS must include local.lightweight-table.",
    );
  }
  return join(resolve(dataDirectory), "tables.sqlite");
}

export function dataDirectoryPath(env = process.env) {
  const rawDirectories = env.BYOP_PLUGIN_DATA_DIRS;
  if (typeof rawDirectories !== "string" || rawDirectories.trim() === "") {
    return null;
  }
  let directories;
  try {
    directories = JSON.parse(rawDirectories);
  } catch (error) {
    throw new Error(`BYOP_PLUGIN_DATA_DIRS is not valid JSON: ${error.message}`);
  }
  const dataDirectory = directories?.[PLUGIN_ID];
  if (typeof dataDirectory !== "string" || dataDirectory.trim() === "") {
    throw new Error(
      "BYOP_PLUGIN_DATA_DIRS must include local.lightweight-table.",
    );
  }
  return resolve(dataDirectory);
}

export function controlDirectoryPath(env = process.env) {
  return dataDirectoryPath(env) ?? dirname(configuredDatabasePath(env));
}

export function runtimeStatePath(env = process.env) {
  return join(controlDirectoryPath(env), "runtime-state.json");
}

export function readRuntimeState(env = process.env) {
  try {
    const parsed = JSON.parse(readFileSync(runtimeStatePath(env), "utf8"));
    return { enabled: parsed?.enabled !== false };
  } catch {
    return { enabled: true };
  }
}

export function readinessTimeoutMs(env = process.env) {
  const value = Number(env.BYOP_MCP_READY_MS);
  return Number.isFinite(value) && value > 0 ? value : DEFAULT_READY_WAIT_MS;
}

export function mcpLaunchArgs(databasePath, controlDirectory) {
  const databaseDirectory = dirname(resolve(databasePath));
  const control = resolve(controlDirectory ?? databaseDirectory);
  return [
    "--permission",
    `--allow-fs-read=${HERE}`,
    `--allow-fs-read=${databaseDirectory}`,
    `--allow-fs-write=${databaseDirectory}`,
    `--allow-fs-read=${control}`,
    `--allow-fs-write=${control}`,
    "--allow-child-process",
    MCP_PROXY,
    "--db",
    resolve(databasePath),
    "--generation",
    join(control, "runtime-generation.json"),
  ];
}

export default function lightweightTableExtension(pi) {
  const databasePath = configuredDatabasePath();
  const controlDirectory = controlDirectoryPath();
  const tracker = new SourceTracker();
  const isTableTool = (toolName) =>
    typeof toolName === "string" &&
    toolName.startsWith(LIGHTWEIGHT_TABLE_MCP_PREFIX);
  const serverConfig = {
    type: "stdio",
    command: process.execPath,
    args: mcpLaunchArgs(databasePath, controlDirectory),
    cwd: HERE,
    exposure: "direct",
    description:
      "Query and review the local Fuyao v4 snapshot using stable row and column IDs. This is an imported local SQLite snapshot, not a live v4 connection; call table_describe for the source date. Plugin restarts reload the backend without restarting Pi.",
    timeout: 15,
  };
  let registered = false;
  let readinessPending = false;

  const registerServer = () => {
    if (registered) {
      return;
    }
    pi.registerMcpServer(MCP_SERVER_NAME, serverConfig);
    registered = true;
    readinessPending = true;
  };
  const unregisterServer = () => {
    if (!registered) {
      return;
    }
    pi.unregisterMcpServer(MCP_SERVER_NAME);
    registered = false;
  };
  const syncRegistration = () => {
    if (readRuntimeState().enabled) {
      registerServer();
      return true;
    }
    unregisterServer();
    return false;
  };

  const toolIsReady = () => {
    try {
      return (pi.getAllTools() ?? []).some(
        (tool) =>
          isTableTool(tool?.name) &&
          tool?.exposure === "direct",
      );
    } catch {
      return false;
    }
  };

  async function awaitMcpToolsReady(timeoutMs, signal) {
    const startedAt = Date.now();
    while (Date.now() - startedAt < timeoutMs && !signal?.aborted) {
      if (toolIsReady()) {
        return true;
      }
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_READY_POLL_MS));
    }
    return toolIsReady();
  }

  if (readRuntimeState().enabled) {
    registerServer();
  }

  pi.on("session_start", (_event, _ctx) => {
    tracker.sessionStarted();
    syncRegistration();
  });
  pi.on("before_agent_start", async (event, ctx) => {
    tracker.beforeAgentStart(event, ctx);
    const shouldWait = syncRegistration() && readinessPending;
    if (!shouldWait || !registered) {
      return;
    }
    readinessPending = false;
    const ready = await awaitMcpToolsReady(readinessTimeoutMs(), undefined);
    if (!ready && ctx?.ui?.notify) {
      ctx.ui.notify(
        "lightweight-table MCP tools are still connecting.",
        "info",
      );
    }
  });
  pi.on("turn_start", () => {
    tracker.turnStarted();
    syncRegistration();
  });
  pi.on("agent_end", () => {
    tracker.agentEnd();
  });
  pi.on("agent_settled", () => {
    tracker.agentSettled();
  });
  pi.on("session_shutdown", () => {
    tracker.sessionShutdown();
  });
  pi.on("tool_call", (event, ctx) => {
    if (!readRuntimeState().enabled && isTableTool(event?.toolName)) {
      return {
        block: true,
        reason: "lightweight-table plugin is disabled.",
      };
    }
    tracker.toolCall(event, ctx);
  });
}
