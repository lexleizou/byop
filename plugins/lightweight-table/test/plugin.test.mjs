import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { join } from "node:path";
import { createInterface } from "node:readline";
import test from "node:test";

import { PLUGIN_DIR, seededStore } from "./support.mjs";

const PLUGIN_PATH = join(PLUGIN_DIR, "plugin.mjs");

class PluginProcess {
  constructor(dataDirectory) {
    this.dataDirectory = dataDirectory;
    this.nextID = 1;
    this.pending = new Map();
    this.hostCalls = [];
    this.stderr = "";
  }

  async start() {
    this.child = spawn(process.execPath, [PLUGIN_PATH], {
      env: {
        ...process.env,
        BYOP_PLUGIN_DATA_DIR: this.dataDirectory,
      },
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
      if (message.method === "host.openChat") {
        this.hostCalls.push(message.params);
        this.child.stdin.write(
          `${JSON.stringify({
            jsonrpc: "2.0",
            id: message.id,
            result: { accepted: true },
          })}\n`,
        );
        return;
      }
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
    await this.request("initialize", {});
    await this.request("activate", {});
    return this;
  }

  request(method, params = {}) {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.child.stdin.write(
        `${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`,
      );
    });
  }

  message(message) {
    return this.request("webviewMessage", { view: "table.main", message });
  }

  async close() {
    if (!this.child || this.child.exitCode !== null) {
      return;
    }
    this.child.stdin.end();
    await new Promise((resolve) => {
      this.child.once("exit", resolve);
    });
  }
}

test("plugin worker serves the WebView API and calls host.openChat with a fixed context", async () => {
  const { store, directory, summary } = await seededStore("plugin-worker");
  store.close();
  const plugin = await new PluginProcess(directory).start();
  try {
    const english = await plugin.message({
      action: "table.locale",
      locale: "en",
    });
    assert.equal(english.locale, "en");
    assert.equal(english.messages.pageTitle, "Lightweight Table");
    const chinese = await plugin.message({
      action: "table.locale",
      locale: "zh-Hans",
    });
    assert.equal(chinese.locale, "zh-Hans");
    assert.equal(chinese.messages.pageTitle, "轻量表格");

    const boot = await plugin.message({ action: "table.bootstrap" });
    assert.equal(boot.summary.row_count, 3);
    assert.equal(boot.description.table.schema_revision, 1);

    const query = await plugin.message({
      action: "table.query",
      query: {
        search: "alpha",
        limit: 100,
        sum_column_id: "principal",
      },
    });
    assert.equal(query.matched_count, 1);
    assert.equal(query.sum.value, "1200.50");

    const change = {
      action: "table.change",
      viewInstanceId: "view-worker",
      commandId: "page-worker-command",
      change: {
        action: "update",
        expected_schema_revision: 1,
        targets: [
          {
            row_id: summary.row_ids[0],
            expected_revision: 1,
          },
        ],
        values: { repayment_status: "repaying" },
      },
    };
    const committed = await plugin.message(change);
    const replay = await plugin.message(change);
    assert.equal(replay.operation_id, committed.operation_id);
    assert.equal(replay.replayed, true);

    const opened = await plugin.message({
      action: "selection.open_chat",
      query: { limit: 100 },
      selectedRowIds: summary.row_ids.slice(0, 2),
      text: "Synthetic context prompt",
      label: "2 synthetic records",
    });
    assert.equal(opened.context.captured_count, 2);
    assert.equal(plugin.hostCalls.length, 1);
    assert.deepEqual(Object.keys(plugin.hostCalls[0]).sort(), [
      "contextId",
      "label",
      "text",
    ]);
    assert.equal(plugin.hostCalls[0].text, "Synthetic context prompt");
    assert.equal(plugin.hostCalls[0].label, "2 synthetic records");
    assert.ok(plugin.hostCalls[0].contextId);

    const contextQuery = await plugin.message({
      action: "table.query",
      query: {
        context_id: plugin.hostCalls[0].contextId,
        limit: 100,
      },
    });
    assert.equal(contextQuery.matched_count, 2);
  } finally {
    await plugin.close();
  }
});
