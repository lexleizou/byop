import { spawn } from "node:child_process";
import { readFile, mkdir, mkdtemp } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createInterface } from "node:readline";

import { TableStore } from "../src/database.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
export const PLUGIN_DIR = dirname(HERE);
export const FIXTURE_PATH = join(HERE, "fixtures", "synthetic-bank-seed.json");
export const CLI_PATH = join(PLUGIN_DIR, "cli.mjs");

export async function makeScratch(prefix) {
  const root =
    process.env.BYOP_P15A_SCRATCH ??
    join(homedir(), "AgentScratch", "20261028-byop-table-link");
  await mkdir(root, { recursive: true });
  return mkdtemp(join(root, `${prefix}-`));
}

export async function syntheticSeed() {
  return JSON.parse(await readFile(FIXTURE_PATH, "utf8"));
}

export async function seededStore(prefix = "node-test") {
  const directory = await makeScratch(prefix);
  const databasePath = join(directory, "tables.sqlite");
  const store = new TableStore(databasePath);
  const summary = store.seed(await syntheticSeed());
  return { directory, databasePath, store, summary };
}

export function agentSource(messageID = "message-1", sessionID = "session-1") {
  return {
    version: 1,
    source_kind: "agent",
    session_id: sessionID,
    message_ids: [messageID],
    message_time: "2026-10-28T08:00:00.000Z",
  };
}

export class McpProcess {
  constructor(databasePath) {
    this.databasePath = databasePath;
    this.nextID = 1;
    this.pending = new Map();
    this.stderr = "";
  }

  async start() {
    this.child = spawn(process.execPath, [CLI_PATH, "serve-mcp", "--db", this.databasePath], {
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stderr.setEncoding("utf8");
    this.child.stderr.on("data", (chunk) => {
      this.stderr += chunk;
    });
    const lines = createInterface({
      input: this.child.stdout,
      crlfDelay: Infinity,
    });
    lines.on("line", (line) => {
      const message = JSON.parse(line);
      const pending = this.pending.get(message.id);
      if (!pending) {
        return;
      }
      this.pending.delete(message.id);
      if (message.error) {
        pending.reject(new Error(message.error.message));
      } else {
        pending.resolve(message.result);
      }
    });
    await this.request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "p15a-test", version: "1" },
    });
    this.notify("notifications/initialized", {});
    return this;
  }

  request(method, params) {
    const id = this.nextID;
    this.nextID += 1;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`);
    });
  }

  notify(method, params) {
    this.child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", method, params })}\n`);
  }

  callTool(name, args) {
    return this.request("tools/call", { name, arguments: args });
  }

  async close() {
    if (!this.child || this.child.exitCode !== null) {
      return;
    }
    this.child.stdin.end();
    await new Promise((resolve) => {
      const timeout = setTimeout(() => {
        this.child.kill("SIGTERM");
      }, 1500);
      this.child.once("exit", () => {
        clearTimeout(timeout);
        resolve();
      });
    });
  }
}
