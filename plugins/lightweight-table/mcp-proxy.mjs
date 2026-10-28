#!/usr/bin/env node

import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath, pathToFileURL } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const DEFAULT_POLL_INTERVAL_MS = 250;
const DEFAULT_INITIALIZE_TIMEOUT_MS = 5000;
export const DEFAULT_DRAIN_TIMEOUT_MS = 20000;
const INTERNAL_REQUEST_PREFIX = "byop-proxy-reload-";

export function generationPathFor(databasePath) {
  return join(dirname(resolve(databasePath)), "runtime-generation.json");
}

export function backendLaunchArgs(databasePath, pluginRoot = HERE) {
  const resolvedDatabase = resolve(databasePath);
  const dataDirectory = dirname(resolvedDatabase);
  return [
    "--permission",
    `--allow-fs-read=${pluginRoot}`,
    `--allow-fs-read=${dataDirectory}`,
    `--allow-fs-write=${dataDirectory}`,
    join(pluginRoot, "cli.mjs"),
    "serve-mcp",
    "--db",
    resolvedDatabase,
  ];
}

function parseArgs(argv) {
  const options = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg !== "--db" && arg !== "--generation") {
      throw new Error(`Unexpected argument ${JSON.stringify(arg)}.`);
    }
    const value = argv[index + 1];
    if (value === undefined || value.startsWith("--")) {
      throw new Error(`Missing value for ${arg}.`);
    }
    options[arg.slice(2)] = value;
    index += 1;
  }
  if (!options.db) {
    throw new Error("--db is required.");
  }
  return options;
}

function requestKey(id) {
  return id === undefined ? null : JSON.stringify(id);
}

function isClientRequest(message) {
  return typeof message?.method === "string" && message?.id !== undefined;
}

class BackendSession {
  constructor(proxy, child) {
    this.proxy = proxy;
    this.child = child;
    this.internal = new Map();
    this.nextInternalID = 1;
    this.stopping = false;
    this.exited = false;
    this.exitPromise = new Promise((resolveExit) => {
      child.once("exit", () => {
        this.exited = true;
        resolveExit();
        if (!this.stopping && proxy.current === this) {
          proxy.fail(new Error("lightweight-table MCP backend exited"));
        }
      });
    });

    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => {
      proxy.error.write(chunk);
    });
    const lines = createInterface({
      input: child.stdout,
      crlfDelay: Infinity,
    });
    lines.on("line", (line) => {
      proxy.handleBackendLine(this, line);
    });
  }

  sendLine(line) {
    if (this.exited || !this.child.stdin.writable) {
      throw new Error("MCP backend is not running.");
    }
    this.child.stdin.write(`${line}\n`);
  }

  request(method, params, timeoutMs = DEFAULT_INITIALIZE_TIMEOUT_MS) {
    const id = `${INTERNAL_REQUEST_PREFIX}${this.nextInternalID++}`;
    return new Promise((resolveRequest, rejectRequest) => {
      const timeout = setTimeout(() => {
        this.internal.delete(id);
        rejectRequest(new Error(`${method} timed out.`));
      }, timeoutMs);
      this.internal.set(id, {
        resolve(value) {
          clearTimeout(timeout);
          resolveRequest(value);
        },
        reject(error) {
          clearTimeout(timeout);
          rejectRequest(error);
        },
      });
      try {
        this.sendLine(
          JSON.stringify({
            jsonrpc: "2.0",
            id,
            method,
            params,
          }),
        );
      } catch (error) {
        clearTimeout(timeout);
        this.internal.delete(id);
        rejectRequest(error);
      }
    });
  }

  internalResponse(message) {
    const pending = this.internal.get(message.id);
    if (!pending) {
      return false;
    }
    this.internal.delete(message.id);
    if (message.error) {
      pending.reject(new Error(message.error.message ?? "Backend initialize failed."));
    } else {
      pending.resolve(message.result);
    }
    return true;
  }

  async stop() {
    if (this.exited) {
      return;
    }
    this.stopping = true;
    this.child.stdin.end();
    const graceful = await Promise.race([
      this.exitPromise.then(() => true),
      new Promise((resolveWait) => setTimeout(() => resolveWait(false), 1000)),
    ]);
    if (graceful || this.exited) {
      return;
    }
    this.child.kill("SIGTERM");
    const terminated = await Promise.race([
      this.exitPromise.then(() => true),
      new Promise((resolveWait) => setTimeout(() => resolveWait(false), 500)),
    ]);
    if (!terminated && !this.exited) {
      this.child.kill("SIGKILL");
      await this.exitPromise;
    }
  }
}

export class ReloadingMcpProxy {
  constructor(options) {
    this.input = options.input ?? process.stdin;
    this.output = options.output ?? process.stdout;
    this.error = options.error ?? process.stderr;
    this.databasePath = resolve(options.databasePath);
    this.generationPath =
      options.generationPath ?? generationPathFor(this.databasePath);
    this.pollIntervalMs =
      options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    this.drainTimeoutMs =
      options.drainTimeoutMs ?? DEFAULT_DRAIN_TIMEOUT_MS;
    this.initializeTimeoutMs =
      options.initializeTimeoutMs ?? DEFAULT_INITIALIZE_TIMEOUT_MS;
    this.pluginRoot = options.pluginRoot ?? HERE;
    this.onFatal = options.onFatal ?? null;
    this.current = null;
    this.lastGeneration = "";
    this.inFlight = new Map();
    this.queued = [];
    this.queuedControl = [];
    this.discarded = new Set();
    this.blocking = false;
    this.switching = false;
    this.polling = false;
    this.closed = false;
    this.failing = false;
    this.pollTimer = null;
    this.drainTimer = null;
    this.lines = null;
  }

  async start() {
    this.lastGeneration = await this.readGeneration();
    this.current = await this.spawnBackend();
    this.lines = createInterface({
      input: this.input,
      crlfDelay: Infinity,
    });
    this.lines.on("line", (line) => {
      this.handleClientLine(line);
    });
    this.input.once("end", () => {
      void this.close();
    });
    this.pollTimer = setInterval(() => {
      void this.pollGeneration();
    }, this.pollIntervalMs);
    this.pollTimer.unref?.();
  }

  async readGeneration() {
    try {
      return (await readFile(this.generationPath, "utf8")).trim();
    } catch (error) {
      if (error?.code === "ENOENT") {
        return "";
      }
      throw error;
    }
  }

  async spawnBackend() {
    const child = spawn(
      process.execPath,
      backendLaunchArgs(this.databasePath, this.pluginRoot),
      {
        cwd: this.pluginRoot,
        env: process.env,
        stdio: ["pipe", "pipe", "pipe"],
      },
    );
    const session = new BackendSession(this, child);
    await new Promise((resolveSpawn, rejectSpawn) => {
      child.once("spawn", resolveSpawn);
      child.once("error", rejectSpawn);
    });
    return session;
  }

  handleClientLine(line) {
    if (this.closed || line.trim() === "") {
      return;
    }
    let message = null;
    try {
      message = JSON.parse(line);
    } catch {
      // The backend remains responsible for malformed JSON-RPC responses.
    }
    if (message?.method === "notifications/cancelled") {
      this.handleCancellation(line, message);
      return;
    }
    if (this.blocking || this.switching) {
      if (isClientRequest(message)) {
        this.queued.push(line);
      } else if (this.current && !this.current.exited) {
        this.forwardClientLine(line);
      } else {
        this.queuedControl.push(line);
      }
      return;
    }
    this.forwardClientLine(line);
  }

  handleCancellation(line, message) {
    const key = requestKey(message.params?.requestId);
    if (key !== null) {
      this.inFlight.delete(key);
      this.discarded.add(key);
      this.queued = this.queued.filter((queuedLine) => {
        try {
          const queued = JSON.parse(queuedLine);
          return requestKey(queued?.id) !== key;
        } catch {
          return true;
        }
      });
    }
    if (this.current && !this.current.exited) {
      this.forwardClientLine(line);
    }
    if (this.blocking && !this.switching && this.inFlight.size === 0) {
      void this.reloadBackend();
    }
  }

  forwardClientLine(line) {
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      message = null;
    }
    const key = requestKey(message?.id);
    if (key !== null && typeof message?.method === "string") {
      this.inFlight.set(key, message);
    }
    try {
      this.current.sendLine(line);
    } catch (error) {
      if (key !== null) {
        this.inFlight.delete(key);
        this.writeErrorResponse(message.id, error.message);
      }
    }
  }

  handleBackendLine(session, line) {
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      if (session === this.current) {
        this.output.write(`${line}\n`);
      }
      return;
    }
    if (session.internalResponse(message)) {
      return;
    }
    if (session !== this.current) {
      return;
    }
    const key = requestKey(message?.id);
    if (key !== null && this.discarded.has(key)) {
      this.discarded.delete(key);
      return;
    }
    if (key !== null && typeof message?.method !== "string") {
      this.inFlight.delete(key);
    }
    this.output.write(`${line}\n`);
    if (this.blocking && this.inFlight.size === 0) {
      void this.reloadBackend();
    }
  }

  async pollGeneration() {
    if (this.polling || this.closed || this.switching) {
      return;
    }
    this.polling = true;
    try {
      const generation = await this.readGeneration();
      if (generation === this.lastGeneration || this.switching) {
        return;
      }
      this.lastGeneration = generation;
      this.beginDrain();
    } catch (error) {
      void this.fail(error);
    } finally {
      this.polling = false;
    }
  }

  beginDrain() {
    this.blocking = true;
    clearTimeout(this.drainTimer);
    this.drainTimer = setTimeout(() => {
      this.drainTimer = null;
      this.expireDrain();
    }, this.drainTimeoutMs);
    this.drainTimer.unref?.();
    if (this.inFlight.size === 0) {
      void this.reloadBackend();
    }
  }

  expireDrain() {
    if (this.closed || !this.blocking || this.switching) {
      return;
    }
    for (const [key, request] of this.inFlight) {
      this.discarded.add(key);
      this.writeErrorResponse(request.id, "MCP request did not drain before reload.");
    }
    this.inFlight.clear();
    void this.reloadBackend();
  }

  clearDrainDeadline() {
    clearTimeout(this.drainTimer);
    this.drainTimer = null;
  }

  async reloadBackend() {
    if (this.switching || !this.blocking || this.inFlight.size > 0 || this.closed) {
      return;
    }
    this.switching = true;
    this.clearDrainDeadline();
    const old = this.current;
    let next = null;
    try {
      await old?.stop();
      this.current = null;
      this.discarded.clear();
      next = await this.spawnBackend();
      await next.request("initialize", {
        protocolVersion: "2025-11-25",
        capabilities: {},
        clientInfo: { name: "byop-plugin-proxy", version: "0.4.0" },
      }, this.initializeTimeoutMs);
      this.current = next;
      next = null;
      const controls = this.queuedControl;
      this.queuedControl = [];
      for (const line of controls) {
        this.current.sendLine(line);
      }
      this.output.write(
        `${JSON.stringify({
          jsonrpc: "2.0",
          method: "notifications/tools/list_changed",
          params: {},
        })}\n`,
      );
      this.blocking = false;
      this.switching = false;
      const queued = this.queued;
      this.queued = [];
      for (const line of queued) {
        this.handleClientLine(line);
      }
    } catch (error) {
      await next?.stop();
      await old?.stop();
      await this.fail(error);
    }
  }

  writeErrorResponse(id, message) {
    this.output.write(
      `${JSON.stringify({
        jsonrpc: "2.0",
        id,
        error: { code: -32000, message },
      })}\n`,
    );
  }

  async fail(error) {
    if (this.closed || this.failing) {
      return;
    }
    this.failing = true;
    this.error.write(
      `${JSON.stringify({
        code: "mcp_proxy_failed",
        message: error instanceof Error ? error.message : String(error),
      })}\n`,
    );
    for (const request of this.inFlight.values()) {
      this.writeErrorResponse(request.id, "MCP backend restart failed.");
    }
    for (const line of this.queued) {
      try {
        const request = JSON.parse(line);
        if (request?.id !== undefined) {
          this.writeErrorResponse(request.id, "MCP backend restart failed.");
        }
      } catch {
        // Parse errors are handled by the active backend during normal traffic.
      }
    }
    this.inFlight.clear();
    this.queued = [];
    this.queuedControl = [];
    await this.close();
    if (this.onFatal) {
      this.onFatal(error);
    }
  }

  async close() {
    if (this.closed) {
      return;
    }
    this.closed = true;
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
    this.clearDrainDeadline();
    this.lines?.close();
    const backend = this.current;
    this.current = null;
    await backend?.stop();
  }
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const numericEnv = (name, fallback) => {
    const value = Number(process.env[name]);
    return Number.isFinite(value) && value > 0 ? value : fallback;
  };
  const proxy = new ReloadingMcpProxy({
    databasePath: options.db,
    generationPath: options.generation ?? generationPathFor(options.db),
    drainTimeoutMs: numericEnv(
      "BYOP_MCP_PROXY_DRAIN_MS",
      DEFAULT_DRAIN_TIMEOUT_MS,
    ),
    initializeTimeoutMs: numericEnv(
      "BYOP_MCP_PROXY_INITIALIZE_MS",
      DEFAULT_INITIALIZE_TIMEOUT_MS,
    ),
    onFatal: () => {
      process.exitCode = 1;
      process.stdin.pause();
      const exit = () => process.exit(1);
      if (process.stdout.writable) {
        process.stdout.write("", exit);
      } else {
        exit();
      }
    },
  });
  const shutdown = () => {
    void proxy.close().finally(() => {
      process.exit(0);
    });
  };
  process.once("SIGTERM", shutdown);
  process.once("SIGINT", shutdown);
  try {
    await proxy.start();
  } catch (error) {
    process.stderr.write(
      `${JSON.stringify({
        code: "mcp_proxy_start_failed",
        message: error instanceof Error ? error.message : String(error),
      })}\n`,
    );
    process.exitCode = 1;
    await proxy.close();
  }
}

const invokedPath = process.argv[1] ? pathToFileURL(resolve(process.argv[1])).href : "";
if (invokedPath === import.meta.url) {
  void main();
}
