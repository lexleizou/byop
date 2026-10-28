import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { SourceTracker } from "./src/source-tracker.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const MCP_PROXY = join(HERE, "mcp-proxy.mjs");
const MCP_SERVER_NAME = "lightweight_table";
const PLUGIN_ID = "local.lightweight-table";

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

export function mcpLaunchArgs(databasePath) {
  const dataDirectory = dirname(resolve(databasePath));
  return [
    "--permission",
    `--allow-fs-read=${HERE}`,
    `--allow-fs-read=${dataDirectory}`,
    `--allow-fs-write=${dataDirectory}`,
    "--allow-child-process",
    MCP_PROXY,
    "--db",
    resolve(databasePath),
    "--generation",
    join(dataDirectory, "runtime-generation.json"),
  ];
}

export default function lightweightTableExtension(pi) {
  const databasePath = configuredDatabasePath();
  const tracker = new SourceTracker();

  pi.registerMcpServer(MCP_SERVER_NAME, {
    type: "stdio",
    command: process.execPath,
    args: mcpLaunchArgs(databasePath),
    cwd: HERE,
    exposure: "direct",
    description:
      "Query, change, review, and undo the local BYOP lightweight table using stable row and column IDs. Plugin restarts reload the backend without restarting Pi.",
    timeout: 15,
  });

  pi.on("session_start", (_event, _ctx) => {
    tracker.sessionStarted();
  });
  pi.on("before_agent_start", (event, ctx) => {
    tracker.beforeAgentStart(event, ctx);
  });
  pi.on("turn_start", () => {
    tracker.turnStarted();
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
    tracker.toolCall(event, ctx);
  });
}
