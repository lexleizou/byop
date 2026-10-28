import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { SourceTracker } from "./src/source-tracker.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const CLI = join(HERE, "cli.mjs");
const MCP_SERVER_NAME = "lightweight_table";

function configuredDatabasePath() {
  const databasePath = process.env.BYOP_LIGHTWEIGHT_TABLE_DB;
  if (!databasePath || databasePath.trim() === "") {
    throw new Error(
      "BYOP_LIGHTWEIGHT_TABLE_DB must point to an independent lightweight-table SQLite file.",
    );
  }
  return databasePath;
}

export default function lightweightTableExtension(pi) {
  const databasePath = configuredDatabasePath();
  const tracker = new SourceTracker();

  pi.registerMcpServer(MCP_SERVER_NAME, {
    type: "stdio",
    command: process.execPath,
    args: [CLI, "serve-mcp", "--db", databasePath],
    cwd: HERE,
    exposure: "direct",
    description:
      "Query and change the local BYOP lightweight table using stable row and column IDs.",
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
