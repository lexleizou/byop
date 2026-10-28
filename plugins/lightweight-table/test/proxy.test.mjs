import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { copyFile, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { DEFAULT_DRAIN_TIMEOUT_MS } from "../mcp-proxy.mjs";
import { makeScratch } from "./support.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const PLUGIN_DIR = dirname(HERE);

const backendSource = String.raw`
import { appendFileSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const version = readFileSync(join(HERE, "backend-version.txt"), "utf8").trim();
const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });

function write(message) {
  process.stdout.write(JSON.stringify(message) + "\n");
}

lines.on("line", async (line) => {
  const message = JSON.parse(line);
  if (message.id === undefined) {
    if (message.method === "notifications/cancelled") {
      appendFileSync(
        join(HERE, "cancelled.txt"),
        JSON.stringify(message.params) + "\n",
      );
    }
    return;
  }
  if (message.method === "initialize") {
    write({
      jsonrpc: "2.0",
      id: message.id,
      result: {
        protocolVersion: "2025-11-25",
        capabilities: { tools: { listChanged: true } },
        serverInfo: { name: "p17_fixture", version },
      },
    });
    return;
  }
  if (message.method === "tools/list") {
    write({
      jsonrpc: "2.0",
      id: message.id,
      result: {
        tools: [{
          name: "fixture_state",
          description: "Generated backend " + version,
          inputSchema: { type: "object", properties: {} },
        }],
      },
    });
    return;
  }
  if (message.method === "tools/call") {
    if (message.params?.name === "fixture_stuck") {
      return;
    }
    if (message.params?.name === "fixture_slow") {
      await new Promise((resolve) => setTimeout(resolve, 900));
    }
    const value = { version, pid: process.pid };
    write({
      jsonrpc: "2.0",
      id: message.id,
      result: {
        content: [{ type: "text", text: JSON.stringify(value) }],
        structuredContent: value,
        isError: false,
      },
    });
  }
});
`;

const failingInitializeBackendSource = String.raw`
import { appendFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
appendFileSync(join(HERE, "backend-pids.txt"), process.pid + "\n");
const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });

function write(message) {
  process.stdout.write(JSON.stringify(message) + "\n");
}

lines.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.method !== "initialize") {
    return;
  }
  if (existsSync(join(HERE, "hang-initialize"))) {
    return;
  }
  write({
    jsonrpc: "2.0",
    id: message.id,
    result: {
      protocolVersion: "2025-11-25",
      capabilities: { tools: { listChanged: true } },
      serverInfo: { name: "p17_failure_fixture", version: "one" },
    },
  });
});
`;

class ProxyClient {
  constructor(child) {
    this.child = child;
    this.nextID = 1;
    this.pending = new Map();
    this.notifications = [];
    this.notificationWaiters = [];
    this.stderr = "";
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => {
      this.stderr += chunk;
    });
    const lines = createInterface({
      input: child.stdout,
      crlfDelay: Infinity,
    });
    lines.on("line", (line) => {
      const message = JSON.parse(line);
      if (message.id === undefined) {
        this.notifications.push(message);
        const remaining = [];
        for (const waiter of this.notificationWaiters) {
          if (waiter.method === message.method) {
            waiter.resolve(message);
          } else {
            remaining.push(waiter);
          }
        }
        this.notificationWaiters = remaining;
        return;
      }
      const pending = this.pending.get(String(message.id));
      if (!pending) {
        return;
      }
      this.pending.delete(String(message.id));
      if (message.error) {
        pending.reject(new Error(message.error.message));
      } else {
        pending.resolve(message.result);
      }
    });
  }

  startRequest(method, params) {
    const id = this.nextID++;
    const promise = new Promise((resolve, reject) => {
      this.pending.set(String(id), { resolve, reject });
      this.child.stdin.write(
        `${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`,
      );
    });
    return { id, promise };
  }

  request(method, params) {
    return this.startRequest(method, params).promise;
  }

  notify(method, params) {
    this.child.stdin.write(
      `${JSON.stringify({ jsonrpc: "2.0", method, params })}\n`,
    );
  }

  tool(name) {
    return this.request("tools/call", {
      name,
      arguments: {},
    });
  }

  waitForNotification(method) {
    const existing = this.notifications.find((value) => value.method === method);
    if (existing) {
      return Promise.resolve(existing);
    }
    return new Promise((resolve) => {
      this.notificationWaiters.push({ method, resolve });
    });
  }

  async close() {
    if (this.child.exitCode !== null) {
      return;
    }
    this.child.stdin.end();
    await new Promise((resolve) => {
      const timeout = setTimeout(() => {
        this.child.kill("SIGTERM");
      }, 2000);
      this.child.once("exit", () => {
        clearTimeout(timeout);
        resolve();
      });
    });
  }
}

async function createProxyFixture(prefix, source = backendSource) {
  const directory = await makeScratch(prefix);
  const paths = {
    directory,
    proxyPath: join(directory, "mcp-proxy.mjs"),
    backendPath: join(directory, "cli.mjs"),
    databasePath: join(directory, "tables.sqlite"),
    generationPath: join(directory, "runtime-generation.json"),
  };
  await copyFile(join(PLUGIN_DIR, "mcp-proxy.mjs"), paths.proxyPath);
  await writeFile(paths.backendPath, source, "utf8");
  await writeFile(join(directory, "backend-version.txt"), "one\n", "utf8");
  return paths;
}

function launchProxy(paths, env = {}) {
  const child = spawn(
    process.execPath,
    [
      "--permission",
      `--allow-fs-read=${paths.directory}`,
      `--allow-fs-write=${paths.directory}`,
      "--allow-child-process",
      paths.proxyPath,
      "--db",
      paths.databasePath,
      "--generation",
      paths.generationPath,
    ],
    {
      cwd: paths.directory,
      env: { ...process.env, ...env },
      stdio: ["pipe", "pipe", "pipe"],
    },
  );
  return { child, client: new ProxyClient(child) };
}

async function waitFor(predicate, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (!(await predicate())) {
    if (Date.now() >= deadline) {
      throw new Error("timed out waiting for proxy state");
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}

function waitForExit(child, timeoutMs = 5000) {
  if (child.exitCode !== null) {
    return Promise.resolve({ code: child.exitCode, signal: child.signalCode });
  }
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      reject(new Error("timed out waiting for proxy exit"));
    }, timeoutMs);
    child.once("exit", (code, signal) => {
      clearTimeout(timeout);
      resolve({ code, signal });
    });
  });
}

function processIsRunning(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error.code !== "ESRCH";
  }
}

test("proxy keeps its process alive while backend generations reload", { timeout: 15000 }, async () => {
  const paths = await createProxyFixture("p17-proxy");
  const { child, client } = launchProxy(paths);
  try {
    const initialized = await client.request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "p17-proxy-test", version: "1" },
    });
    assert.equal(initialized.capabilities.tools.listChanged, true);

    const firstTools = await client.request("tools/list", {});
    assert.match(firstTools.tools[0].description, /one/);
    const firstState = await client.tool("fixture_state");
    const firstPID = firstState.structuredContent.pid;
    const proxyPID = child.pid;
    const listChanged = client.waitForNotification(
      "notifications/tools/list_changed",
    );

    const slow = client.tool("fixture_slow");
    await new Promise((resolve) => setTimeout(resolve, 100));
    await writeFile(join(paths.directory, "backend-version.txt"), "two\n", "utf8");
    await writeFile(
      paths.generationPath,
      `${JSON.stringify({ version: 1, generation: 1 })}\n`,
      "utf8",
    );
    await new Promise((resolve) => setTimeout(resolve, 350));
    const queuedList = client.request("tools/list", {});

    const slowResult = await slow;
    assert.equal(slowResult.structuredContent.version, "one");
    await listChanged;
    const secondTools = await queuedList;
    assert.match(secondTools.tools[0].description, /two/);
    const secondState = await client.tool("fixture_state");
    assert.equal(secondState.structuredContent.version, "two");
    assert.notEqual(secondState.structuredContent.pid, firstPID);
    assert.equal(child.pid, proxyPID);
    assert.equal(child.exitCode, null);
  } finally {
    await client.close();
  }
});

test("proxy forwards cancellation and removes the cancelled request from the drain", { timeout: 15000 }, async () => {
  const paths = await createProxyFixture("p17-cancel");
  const { child, client } = launchProxy(paths);
  try {
    await client.request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "p17-cancel-test", version: "1" },
    });
    const listChanged = client.waitForNotification(
      "notifications/tools/list_changed",
    );
    const stuck = client.startRequest("tools/call", {
      name: "fixture_stuck",
      arguments: {},
    });
    await new Promise((resolve) => setTimeout(resolve, 100));
    await writeFile(join(paths.directory, "backend-version.txt"), "two\n", "utf8");
    await writeFile(
      paths.generationPath,
      `${JSON.stringify({ version: 1, generation: 2 })}\n`,
      "utf8",
    );
    await new Promise((resolve) => setTimeout(resolve, 350));

    client.notify("notifications/cancelled", { requestId: stuck.id });
    const queuedList = client.request("tools/list", {});

    await listChanged;
    const tools = await queuedList;
    assert.match(tools.tools[0].description, /two/);
    await waitFor(async () => {
      try {
        const cancelled = await readFile(
          join(paths.directory, "cancelled.txt"),
          "utf8",
        );
        return cancelled.includes(String(stuck.id));
      } catch {
        return false;
      }
    });
    assert.equal(child.exitCode, null);
  } finally {
    await client.close();
  }
});

test("proxy expires a stuck drain and reloads with a bounded deadline", { timeout: 15000 }, async () => {
  assert.ok(DEFAULT_DRAIN_TIMEOUT_MS > 15000);
  const paths = await createProxyFixture("p17-drain");
  const { child, client } = launchProxy(paths, {
    BYOP_MCP_PROXY_DRAIN_MS: "120",
  });
  try {
    await client.request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "p17-drain-test", version: "1" },
    });
    const listChanged = client.waitForNotification(
      "notifications/tools/list_changed",
    );
    const stuck = client.startRequest("tools/call", {
      name: "fixture_stuck",
      arguments: {},
    });
    const drained = assert.rejects(stuck.promise, /did not drain/);
    await new Promise((resolve) => setTimeout(resolve, 100));
    await writeFile(join(paths.directory, "backend-version.txt"), "two\n", "utf8");
    await writeFile(
      paths.generationPath,
      `${JSON.stringify({ version: 1, generation: 3 })}\n`,
      "utf8",
    );
    await new Promise((resolve) => setTimeout(resolve, 350));
    const queuedList = client.request("tools/list", {});

    await drained;
    await listChanged;
    const tools = await queuedList;
    assert.match(tools.tools[0].description, /two/);
    assert.equal(child.exitCode, null);
  } finally {
    await client.close();
  }
});

test("proxy stops a failed candidate backend and exits nonzero", { timeout: 10000 }, async () => {
  const paths = await createProxyFixture(
    "p17-init-failure",
    failingInitializeBackendSource,
  );
  const { child, client } = launchProxy(paths, {
    BYOP_MCP_PROXY_INITIALIZE_MS: "150",
  });
  try {
    await client.request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "p17-failure-test", version: "1" },
    });
    await writeFile(join(paths.directory, "hang-initialize"), "1\n", "utf8");
    await writeFile(
      paths.generationPath,
      `${JSON.stringify({ version: 1, generation: 4 })}\n`,
      "utf8",
    );

    const result = await waitForExit(child);
    assert.notEqual(result.code, 0);
    const pids = (await readFile(join(paths.directory, "backend-pids.txt"), "utf8"))
      .trim()
      .split("\n")
      .map(Number)
      .filter((pid) => pid > 0);
    assert.ok(pids.length >= 2);
    for (const pid of pids) {
      assert.equal(processIsRunning(pid), false, `backend ${pid} leaked`);
    }
  } finally {
    if (child.exitCode === null) {
      child.kill("SIGTERM");
    }
  }
});
