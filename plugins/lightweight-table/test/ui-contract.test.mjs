import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import test from "node:test";

import { PLUGIN_DIR } from "./support.mjs";

async function readJSON(path) {
  return JSON.parse(await readFile(path, "utf8"));
}

function functionSource(source, name) {
  const start = source.indexOf(`function ${name}(`);
  if (start < 0) {
    return "";
  }
  const next = source.indexOf("\nfunction ", start + 1);
  return source.slice(start, next < 0 ? undefined : next);
}

test("en and zh-Hans expose the same complete UI locale keys", async () => {
  const en = await readJSON(join(PLUGIN_DIR, "l10n", "en.json"));
  const zh = await readJSON(join(PLUGIN_DIR, "l10n", "zh-Hans.json"));
  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort());
  for (const key of [
    "manifestName",
    "manifestDescription",
    "navigationTitle",
    "webviewTitle",
    "pageTitle",
    "addRow",
    "sendToChat",
    "setField",
    "history",
    "column.name",
    "column.repayment_status",
  ]) {
    assert.equal(typeof en[key], "string");
    assert.equal(typeof zh[key], "string");
  }
});

test("WebView uses the worker locale action and sets its ready flag", async () => {
  const html = await readFile(join(PLUGIN_DIR, "web", "index.html"), "utf8");
  const app = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
  const manifest = await readJSON(join(PLUGIN_DIR, "plugin.json"));
  const en = await readJSON(join(PLUGIN_DIR, "l10n", "en.json"));

  assert.match(html, /<body data-ready="false">/);
  for (const id of [
    "add-row-button",
    "send-chat-button",
    "bulk-set-button",
    "history-button",
    "refresh-button",
    "search-input",
    "filter-column-select",
    "filter-operator-select",
    "filter-value-input",
    "sort-column-select",
    "sort-direction-select",
    "records-table",
    "history-drawer",
    "row-editor-drawer",
    "bulk-drawer",
  ]) {
    assert.match(html, new RegExp(`id="${id}"`));
  }
  for (const action of [
    "add-row",
    "send-chat",
    "bulk-set",
    "open-history",
    "refresh",
    "edit-cell",
    "delete-row",
    "undo-operation",
  ]) {
    assert.ok(
      html.includes(`data-action="${action}"`) ||
        app.includes(`data-action="${action}"`) ||
        app.includes(`"${action}"`),
    );
  }

  const i18nKeys = [...html.matchAll(/data-i18n="([^"]+)"/g)].map(
    (match) => match[1],
  );
  for (const key of i18nKeys) {
    assert.equal(typeof en[key], "string", `missing locale key ${key}`);
  }

  for (const match of html.matchAll(/(?:src|href)="([^"]+)"/g)) {
    assert.equal(match[1].includes("?"), false);
    assert.equal(match[1].includes("#"), false);
  }
  assert.equal(app.includes("fetch("), false);
  assert.match(app, /callPlugin\("table\.locale"/);
  assert.match(app, /dom\.body\.dataset\.ready = "true"/);
  assert.equal(app.includes("state.body"), false);
  assert.equal(manifest.contributes.agentExtension, "pi-extension.mjs");
});

test("bulk editor captures one fixed context and selected revisions before apply", async () => {
  const app = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
  const open = functionSource(app, "openBulkEditor");
  const apply = functionSource(app, "applyBulk");

  assert.match(open, /table\.capture_context/);
  assert.match(open, /state\.bulkContextId = context\.context_id/);
  assert.match(open, /state\.bulkSelectedTargets = selectedTargets/);
  assert.doesNotMatch(apply, /table\.capture_context/);
  assert.match(apply, /state\.bulkContextId/);
  assert.match(apply, /state\.bulkSelectedTargets/);
  assert.match(app, /bulkForm\.addEventListener\("input", bulkInputChanged\)/);
  assert.match(app, /bulkForm\.addEventListener\("change", bulkInputChanged\)/);
});

test("table redraw remounts drafts and refreshes after the last draft closes", async () => {
  const app = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
  const render = functionSource(app, "renderTable");
  const close = functionSource(app, "closeCellEdit");
  const load = functionSource(app, "loadQuery");
  const selectPage = functionSource(app, "selectPage");
  const selectRow = functionSource(app, "selectRow");

  assert.match(render, /draft\.value = draft\.input\?\.value/);
  assert.match(render, /draft\.row = row/);
  assert.match(render, /draft\.commandId = newCommandId\(\)/);
  assert.match(render, /mountCellEditor\(td, draft\)/);
  assert.match(load, /state\.editors\.size > 0/);
  assert.match(load, /!preserveDrafts/);
  assert.match(load, /state\.pendingRefresh = true/);
  assert.match(load, /renderTable\(\{ rebaseDrafts: preserveDrafts \}\)/);
  assert.match(close, /state\.editors\.size === 0/);
  assert.match(close, /state\.pendingRefresh/);
  assert.match(close, /loadQuery\(\{ clearSelection: false \}\)/);
  assert.doesNotMatch(selectPage, /renderTable\(/);
  assert.doesNotMatch(selectRow, /renderTable\(/);
  assert.match(app, /preserveDrafts: true/);
});
