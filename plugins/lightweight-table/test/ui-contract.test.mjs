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
    "clearSelection",
    "readOnlyCellAria",
    "detailTitle",
    "detailDescription",
    "column.name",
    "column.repayment_status",
    "column.actual_received",
    "column.remaining_due",
    "column.annualized_rate",
    "column.plan_status",
    "column.next_due",
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
    "clear-selection-button",
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
    "row-detail-drawer",
    "row-detail-body",
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
    "open-detail",
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
  assert.match(app, /callPlugin\("table\.detail"/);
  assert.match(app, /dom\.detailDrawer/);
  assert.doesNotMatch(app, /notImported|missingFieldHint|column\.missing/);
  assert.match(app, /column\.config\?\.read_only !== true/);
  assert.match(app, /td\.dataset\.editable = isEditableColumn/);
  const detailStart = html.indexOf('id="row-detail-drawer"');
  const detailEnd = html.indexOf("</aside>", detailStart);
  assert.ok(detailStart > 0 && detailEnd > detailStart);
  assert.doesNotMatch(html.slice(detailStart, detailEnd), /delete-row/);
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

test("table redraw remounts the active draft and refreshes after it closes", async () => {
  const app = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
  const render = functionSource(app, "renderTable");
  const settle = functionSource(app, "settleCellDraft");
  const close = functionSource(app, "closeCellEdit");
  const load = functionSource(app, "loadQuery");
  const selectPage = functionSource(app, "selectPage");
  const selectRow = functionSource(app, "selectRow");

  assert.match(render, /draft\.value = draft\.input\?\.value/);
  assert.match(render, /const activeDraft = \[\.\.\.state\.editors\.values\(\)\]\.find/);
  assert.match(render, /draft !== activeDraft[\s\S]*draft\.editing = false/);
  assert.match(render, /draft\.row = row/);
  assert.match(render, /draft\.commandId = newCommandId\(\)/);
  assert.match(render, /mountCellEditor\(td, draft\)/);
  assert.match(settle, /draft\.editing = false/);
  assert.match(settle, /renderDraftValue\(cell, draft\)/);
  assert.match(
    app,
    /draft\.column\.type === "number"[\s\S]*formatDecimal\(draft\.value\)[\s\S]*String\(draft\.value \?\? ""\)/,
  );
  assert.match(app, /input\.classList\.add\("number-cell"\)/);
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

test("P15b.1 row selection and cell editing have separate lifecycles", async () => {
  const app = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
  const html = await readFile(join(PLUGIN_DIR, "web", "index.html"), "utf8");
  const css = await readFile(join(PLUGIN_DIR, "web", "style.css"), "utf8");
  const begin = functionSource(app, "beginCellEdit");
  const settle = functionSource(app, "settleCellDraft");
  const clear = functionSource(app, "clearSelection");
  const sumMatch = app.match(
    /dom\.sumLabel\.textContent = `\$\{message\("sumLabel"\)\}: \$\{formatDecimal\(([\s\S]*?)\)\}`;/,
  );

  assert.match(app, /globalThis\.crypto\?\.randomUUID/);
  assert.doesNotMatch(begin, /if \(cell\.querySelector\("input"\)\)/);
  assert.match(begin, /const activeDraft = state\.editors\.values\(\)\.next\(\)\.value/);
  assert.match(begin, /mountCellEditor\(draftCell, activeDraft, \{ focus: true \}\)/);
  assert.match(settle, /draft\.editing = false/);
  assert.match(settle, /renderDraftValue\(cell, draft\)/);
  assert.match(clear, /state\.selected\.clear\(\)/);
  assert.match(app, /dom\.table\.addEventListener\("dblclick"/);
  assert.match(app, /event\.target\.closest\("input"\)[\s\S]*\? null/);
  assert.match(app, /event\.target\.closest\("td\[data-action='edit-cell'\]"\)/);
  assert.match(app, /event\.key === "Enter"[\s\S]*beginCellEdit\(event\.target\)/);
  assert.match(app, /event\.key === "Escape"[\s\S]*cancelCellEdit\(draft\)/);
  assert.match(app, /event\.key === "Escape"[\s\S]*clearSelection\(\)/);
  assert.match(html, /data-action="clear-selection"/);
  assert.match(app, /"institution"[\s\S]*"category"[\s\S]*"name"[\s\S]*"principal"/);
  assert.match(app, /"actual_received"[\s\S]*"remaining_due"[\s\S]*"annualized_rate"/);
  assert.match(app, /"plan_status"[\s\S]*"next_due"/);
  assert.doesNotMatch(app, /sumLabel[\s\S]{0,200}CNY/);
  assert.ok(sumMatch, "principal total must render without a currency suffix");
  assert.match(css, /\.number-cell,[\s\S]*text-align: right/);
  assert.match(css, /table \{[\s\S]*min-width: 1640px/);
  assert.match(css, /\.number-cell,[\s\S]*min-width: 150px[\s\S]*max-width: 150px/);
  assert.match(css, /\.plan-status-cell \{[\s\S]*width: 150px/);
  assert.match(css, /\.next-due-cell \{[\s\S]*width: 140px/);
  assert.match(css, /text-align: right/);
});

test("P15b.1 pending single-click cannot override clear or explicit selection", async () => {
  const app = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
  const names = [
    "scheduleRowSelectionToggle",
    "settlePendingCellClick",
    "cancelPendingCellClick",
    "clearSelection",
    "selectRow",
    "selectPage",
  ];
  const source = names.map((name) => functionSource(app, name)).join("\n");
  let nextTimer = 0;
  const timers = new Map();
  const setTimeoutFake = (callback, delay) => {
    const id = ++nextTimer;
    timers.set(id, { callback, delay });
    return id;
  };
  const clearTimeoutFake = (id) => {
    timers.delete(id);
  };
  const runDue = (delay) => {
    for (const [id, timer] of [...timers]) {
      if (timer.delay <= delay) {
        timers.delete(id);
        timer.callback();
      }
    }
  };
  const lifecycle = new Function(
    "state",
    "toggleRowSelection",
    "updateSelectionUI",
    "dom",
    "CSS",
    "setTimeout",
    "clearTimeout",
    `${source}
    return {
      scheduleRowSelectionToggle,
      cancelPendingCellClick,
      clearSelection,
      selectRow,
      selectPage,
    };`,
  );
  const makeCell = (rowID, columnID = "name") => ({
    dataset: { rowId: rowID, columnId: columnID },
  });
  const createState = () => ({
    selected: new Set(),
    pendingCellClick: null,
    result: { rows: [{ row_id: "row-a" }, { row_id: "row-b" }] },
  });

  {
    const state = createState();
    const clearSelection = () =>
      state.selected.clear();
    const api = lifecycle(
      state,
      (rowID) => {
        if (state.selected.has(rowID)) {
          state.selected.delete(rowID);
        } else {
          state.selected.add(rowID);
        }
      },
      () => {},
      {
        bodyRows: { querySelector: () => null },
        selectedCount: { textContent: "" },
        clearSelection: { disabled: false },
      },
      { escape: (value) => value },
      setTimeoutFake,
      clearTimeoutFake,
    );
    api.scheduleRowSelectionToggle(makeCell("row-a"));
    clearSelection();
    api.clearSelection();
    runDue(250);
    assert.equal(state.selected.size, 0);
  }

  {
    const state = createState();
    const api = lifecycle(
      state,
      (rowID) => {
        if (state.selected.has(rowID)) {
          state.selected.delete(rowID);
        } else {
          state.selected.add(rowID);
        }
      },
      () => {},
      {
        bodyRows: { querySelector: () => null },
        selectedCount: { textContent: "" },
        clearSelection: { disabled: false },
      },
      { escape: (value) => value },
      setTimeoutFake,
      clearTimeoutFake,
    );
    api.scheduleRowSelectionToggle(makeCell("row-a"));
    api.selectRow("row-b", true);
    runDue(250);
    assert.deepEqual([...state.selected], ["row-b"]);
  }

  {
    const state = createState();
    const api = lifecycle(
      state,
      (rowID) => {
        if (state.selected.has(rowID)) {
          state.selected.delete(rowID);
        } else {
          state.selected.add(rowID);
        }
      },
      () => {},
      {
        bodyRows: { querySelector: () => null },
        selectedCount: { textContent: "" },
        clearSelection: { disabled: false },
      },
      { escape: (value) => value },
      setTimeoutFake,
      clearTimeoutFake,
    );
    api.scheduleRowSelectionToggle(makeCell("row-a"));
    api.scheduleRowSelectionToggle(makeCell("row-b"));
    runDue(250);
    assert.deepEqual([...state.selected], ["row-a", "row-b"]);
  }
});
