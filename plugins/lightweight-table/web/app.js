const PAGE_SIZE = 100;
const SUPPORTED_LOCALES = new Set(["en", "zh-Hans"]);
const MAIN_COLUMNS = Object.freeze([
  { column_id: "institution", type: "text" },
  { column_id: "category", type: "text" },
  { column_id: "name", type: "text" },
  { column_id: "principal", type: "number" },
  { column_id: "actual_received", type: "number" },
  { column_id: "remaining_due", type: "number" },
  { column_id: "annualized_rate", type: "text" },
  { column_id: "repayment_status", type: "text" },
  { column_id: "plan_status", type: "text" },
  { column_id: "next_due", type: "date" },
]);
const MAIN_COLUMN_BY_ID = new Map(
  MAIN_COLUMNS.map((column) => [column.column_id, column]),
);

const dom = {
  body: document.body,
  title: document.querySelector("#page-title"),
  subtitle: document.querySelector("#page-subtitle"),
  dashboard: document.querySelector("#dashboard"),
  dashboardTotals: document.querySelector("#dashboard-totals"),
  dashboardPresets: document.querySelector("#dashboard-presets"),
  searchInput: document.querySelector("#search-input"),
  filterColumn: document.querySelector("#filter-column-select"),
  filterOperatorField: document.querySelector("#filter-operator-field"),
  filterOperator: document.querySelector("#filter-operator-select"),
  filterValueField: document.querySelector("#filter-value-field"),
  filterValue: document.querySelector("#filter-value-input"),
  clearFilter: document.querySelector("#filter-clear-button"),
  sortColumn: document.querySelector("#sort-column-select"),
  sortDirection: document.querySelector("#sort-direction-select"),
  loading: document.querySelector("#loading-indicator"),
  table: document.querySelector("#records-table"),
  head: document.querySelector("#records-head"),
  bodyRows: document.querySelector("#records-body"),
  empty: document.querySelector("#empty-state"),
  emptyTitle: document.querySelector("#empty-title"),
  emptyBody: document.querySelector("#empty-body"),
  recordCount: document.querySelector("#record-count"),
  selectedCount: document.querySelector("#selected-count"),
  clearSelection: document.querySelector("#clear-selection-button"),
  filteredCount: document.querySelector("#filtered-count"),
  sumLabel: document.querySelector("#sum-label"),
  previous: document.querySelector("#page-previous"),
  next: document.querySelector("#page-next"),
  pageStatus: document.querySelector("#page-status"),
  backdrop: document.querySelector("#drawer-backdrop"),
  historyDrawer: document.querySelector("#history-drawer"),
  historyList: document.querySelector("#history-list"),
  rowDrawer: document.querySelector("#row-editor-drawer"),
  rowForm: document.querySelector("#row-editor-form"),
  rowFields: document.querySelector("#row-editor-fields"),
  rowSave: document.querySelector("#row-editor-save-button"),
  detailDrawer: document.querySelector("#row-detail-drawer"),
  detailBody: document.querySelector("#row-detail-body"),
  bulkDrawer: document.querySelector("#bulk-drawer"),
  bulkForm: document.querySelector("#bulk-form"),
  bulkTargetSelected: document.querySelector("#bulk-target-selected"),
  bulkTargetFiltered: document.querySelector("#bulk-target-filtered"),
  bulkTargetSelectedLabel: document.querySelector("#bulk-target-selected-label"),
  bulkTargetFilteredLabel: document.querySelector("#bulk-target-filtered-label"),
  bulkField: document.querySelector("#bulk-field-select"),
  bulkValue: document.querySelector("#bulk-value-input"),
  bulkClear: document.querySelector("#bulk-clear-value"),
  bulkApply: document.querySelector("#bulk-apply-button"),
  toast: document.querySelector("#toast"),
};

const state = {
  locale: "en",
  messages: {},
  localeCache: new Map(),
  description: null,
  summary: null,
  dashboard: null,
  result: null,
  dataRevision: null,
  selected: new Set(),
  editors: new Map(),
  page: 1,
  loadingCount: 0,
  toastTimer: null,
  lastToastKey: "",
  lastToastAt: 0,
  pollTimer: null,
  pollInFlight: false,
  pendingRefresh: false,
  viewInstanceId:
    globalThis.crypto?.randomUUID?.() ??
    `view-${Date.now()}-${Math.random().toString(16).slice(2)}`,
  rowCommandId: null,
  rowAttemptedValue: null,
  pendingCellClick: null,
  bulkCommandId: null,
  bulkContextId: null,
  bulkFilteredCount: 0,
  bulkSelectedTargets: [],
  bulkOpening: false,
  historyCommandIds: new Map(),
  openDrawer: null,
};

function newCommandId() {
  return (
    globalThis.crypto?.randomUUID?.() ??
    `command-${Date.now()}-${Math.random().toString(16).slice(2)}`
  );
}

function message(key, variables = {}) {
  const template = state.messages[key] ?? key;
  return template.replace(/\{(\w+)\}/g, (_match, name) =>
    Object.hasOwn(variables, name) ? String(variables[name]) : `{${name}}`,
  );
}

function parseBridgeError(error) {
  let payload = {
    code: error?.code ?? "generic",
    message: error?.message || String(error),
  };
  const raw = error?.message;
  if (typeof raw === "string") {
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object") {
        payload = {
          code: parsed.code ?? "generic",
          message: parsed.message || raw,
          details: parsed.details,
        };
      }
    } catch {
      // Keep the bridge error as-is.
    }
  }
  const numericCode = Number(payload.code);
  if (Number.isFinite(numericCode)) {
    payload.code = numericCode;
  }
  return payload;
}

function localizedError(error) {
  const payload = parseBridgeError(error);
  const key = `error.${payload.code}`;
  const specific = message(key);
  const actual = typeof payload.message === "string" ? payload.message.trim() : "";
  if (specific !== key) {
    return specific;
  }
  if (actual && !actual.startsWith("error.")) {
    return actual;
  }
  const generic = message("error.generic");
  return generic !== "error.generic" ? generic : `Bridge error ${payload.code}`;
}

async function callPlugin(action, payload = {}) {
  const messagePayload = { action, ...payload };
  const bridge = window.byop;
  if (bridge && typeof bridge.postMessage === "function") {
    return bridge.postMessage(messagePayload);
  }
  if (window.mygo && typeof window.mygo.call === "function") {
    return window.mygo.call("BYOPWebView.Call", "plugin.postMessage", {
      message: messagePayload,
    });
  }
  const preview = message("previewMode");
  throw Object.assign(
    new Error(
      preview === "previewMode"
        ? "BYOP WebView bridge is unavailable: window.byop and window.mygo.call are missing."
        : preview,
    ),
    {
      code: "preview",
    },
  );
}

function setLoading(active) {
  state.loadingCount = Math.max(0, state.loadingCount + (active ? 1 : -1));
  dom.loading.hidden = state.loadingCount === 0;
}

function showToast(text, tone = "info", dedupeKey = text) {
  const now = Date.now();
  if (
    state.lastToastKey === dedupeKey &&
    now - state.lastToastAt < 1800 &&
    tone !== "error"
  ) {
    return;
  }
  state.lastToastKey = dedupeKey;
  state.lastToastAt = now;
  clearTimeout(state.toastTimer);
  dom.toast.textContent = text;
  dom.toast.dataset.tone = tone;
  dom.toast.hidden = false;
  state.toastTimer = setTimeout(() => {
    dom.toast.hidden = true;
  }, tone === "error" ? 5200 : 2600);
}

function showError(error) {
  const text = localizedError(error);
  showToast(text, "error", text);
  return parseBridgeError(error);
}

function showPageError(error) {
  const text = localizedError(error);
  state.loadingCount = 0;
  dom.loading.hidden = true;
  dom.table.hidden = true;
  dom.empty.hidden = false;
  dom.emptyTitle.textContent = text;
  dom.emptyBody.textContent =
    message("error.generic") === "error.generic" ? "" : message("refresh");
  dom.body.dataset.ready = "true";
}

async function loadLocale(locale) {
  const selected = SUPPORTED_LOCALES.has(locale) ? locale : "en";
  if (state.localeCache.has(selected)) {
    return state.localeCache.get(selected);
  }
  const result = await callPlugin("table.locale", { locale: selected });
  if (
    !result ||
    result.locale !== selected ||
    !result.messages ||
    typeof result.messages !== "object"
  ) {
    throw new Error("Locale package is invalid.");
  }
  const messages = result.messages;
  state.localeCache.set(selected, messages);
  return messages;
}

function setOption(select, value, label, selected = false) {
  const option = document.createElement("option");
  option.value = value;
  option.textContent = label;
  option.selected = selected;
  select.append(option);
}

function columnByID(columnID) {
  return state.description?.columns.find(
    (column) => column.column_id === columnID,
  );
}

function visibleColumns() {
  return (state.description?.columns ?? []).filter((column) => !column.hidden);
}

function editableColumns() {
  return visibleColumns().filter(
    (column) => column.config?.read_only !== true,
  );
}

function isEditableColumn(columnID) {
  return columnByID(columnID)?.config?.read_only !== true;
}

function columnLabel(columnID) {
  return message(`column.${columnID}`);
}

// LABEL_KEYS maps a v4 enum namespace to its locale prefix, so a value shows
// in the page language the same way the v4 app shows it. An unknown value
// keeps its source text rather than showing a broken key.
const LABEL_KEYS = Object.freeze({
  repayment_status: "status",
  plan_status: "planStatus",
  category: "category",
  funding_source: "funding",
  product_type: "productType",
  method: "method",
  interest_basis: "basis",
  rate_period: "period",
  installment: "installment",
  principal_effect: "paymentEffect",
  allocation_method: "allocation",
});

function label(namespace, value) {
  if (value === null || value === undefined || value === "") {
    return "—";
  }
  const key = `${LABEL_KEYS[namespace]}.${value}`;
  return message(key) === key ? String(value) : message(key);
}

function localizedValue(columnID, value) {
  if (columnID in LABEL_KEYS) {
    return label(columnID, value);
  }
  if (value === null || value === undefined || value === "") {
    return "—";
  }
  return String(value);
}

function formatDecimal(value) {
  if (value === null || value === undefined || value === "") {
    return "—";
  }
  const raw = String(value);
  const negative = raw.startsWith("-");
  const unsigned = negative ? raw.slice(1) : raw;
  const [whole, fraction] = unsigned.split(".");
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return `${negative ? "-" : ""}${grouped}${fraction ? `.${fraction}` : ""}`;
}

// The dashboard summarizes the table and offers its saved queries as buttons.
// Every preset carries one filter, so pressing a button only writes that
// filter into the filter bar: the list and the button count cannot diverge.
const DASHBOARD_TOTAL_COLUMNS = ["principal", "remaining_due", "actual_paid"];
const DASHBOARD_PRESET_LABEL_KEYS = Object.freeze({
  all: "filterAll",
  due_30: "dashboardPresetDueSoon",
});

function presetLabel(presetID) {
  const key = DASHBOARD_PRESET_LABEL_KEYS[presetID];
  if (key) {
    return message(key);
  }
  // The rest are one-value queries, so the button takes its text from the
  // filter value on its own column: the same label the table and detail drawer
  // show for that value.
  const filter = (state.dashboard?.presets ?? [])
    .find((preset) => preset.preset_id === presetID)
    ?.filters[0];
  return filter ? label(filter.column_id, filter.value) : presetID;
}

function renderDashboard() {
  const dashboard = state.dashboard;
  if (!dashboard) {
    dom.dashboard.hidden = true;
    return;
  }
  dom.dashboard.hidden = false;
  dom.dashboard.setAttribute("aria-label", message("dashboardAria"));
  dom.dashboardTotals.replaceChildren();
  for (const columnID of DASHBOARD_TOTAL_COLUMNS) {
    const amount = dashboard.totals?.[columnID];
    const card = document.createElement("div");
    card.className = "dashboard-card";
    const label = document.createElement("span");
    label.className = "dashboard-card-label";
    label.textContent = columnLabel(columnID);
    const value = document.createElement("span");
    value.className = "dashboard-card-value";
    value.textContent = formatDecimal(amount?.value ?? "0.00");
    card.append(label, value);
    dom.dashboardTotals.append(card);
  }

  const activeID = activePresetID();
  dom.dashboardPresets.replaceChildren();
  const heading = document.createElement("span");
  heading.className = "dashboard-presets-label";
  heading.textContent = message("dashboardPresetsTitle");
  dom.dashboardPresets.append(heading);
  for (const preset of dashboard.presets ?? []) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "button dashboard-preset";
    button.dataset.action = "apply-preset";
    button.dataset.presetId = preset.preset_id;
    if (preset.preset_id === activeID) {
      button.classList.add("primary");
      button.setAttribute("aria-pressed", "true");
    }
    button.title = `${message("dashboardPresetHint", {
      amount: `${columnLabel(preset.amount.column_id)} ${formatDecimal(
        preset.amount.value,
      )}`,
    })}`;
    const name = document.createElement("span");
    name.className = "dashboard-preset-name";
    name.textContent = presetLabel(preset.preset_id);
    const count = document.createElement("span");
    count.className = "dashboard-preset-count";
    count.textContent = message("dashboardPresetCount", {
      count: preset.row_count,
    });
    button.append(name, count);
    dom.dashboardPresets.append(button);
  }
}

// activePresetID reports which preset the last query used, so its button stays
// pressed while that filter is in effect. Key order is normalized because the
// two sides build the same filter object from different code paths.
function activePresetID() {
  const filters = state.result?.filters ?? [];
  const canonical = (list) =>
    JSON.stringify(
      list.map((filter) =>
        Object.fromEntries(Object.entries(filter).sort(([a], [b]) => a.localeCompare(b))),
      ),
    );
  const current = canonical(filters);
  for (const preset of state.dashboard?.presets ?? []) {
    if (canonical(preset.filters) === current) {
      return preset.preset_id;
    }
  }
  return "";
}

function applyPreset(presetID) {
  const preset = (state.dashboard?.presets ?? []).find(
    (entry) => entry.preset_id === presetID,
  );
  if (!preset) {
    return;
  }
  dom.searchInput.value = "";
  const filter = preset.filters[0];
  if (!filter) {
    dom.filterColumn.value = "";
    dom.filterValue.value = "";
  } else {
    dom.filterColumn.value = filter.column_id;
  }
  // The filter controls are rebuilt before the operator and value are set, so
  // the bar shows exactly the query the button counts by.
  renderControls();
  if (filter) {
    dom.filterOperator.value = filter.op;
    dom.filterValue.value = filter.value ?? "";
  }
  state.page = 1;
  void loadQuery();
}

async function loadDashboard() {
  try {
    state.dashboard = await callPlugin("table.dashboard");
    renderDashboard();
  } catch (error) {
    // The summary is an extra view over the table: a failure here must not
    // hide the records, so it shows as a toast and leaves the table alone.
    showError(error);
  }
}

function cellText(column, value, row = null) {
  if (column.type === "number") {
    return formatDecimal(value);
  }
  if (column.type === "date") {
    if (column.column_id === "next_due" && value && row) {
      const amount = row.values.next_due_amount;
      return amount === null || amount === undefined || amount === ""
        ? value
        : `${value} · ${formatDecimal(amount)}`;
    }
    return value || "—";
  }
  return localizedValue(column.column_id, value);
}

function operatorOptions(column) {
  if (!column) {
    return [];
  }
  if (column.type === "number") {
    return [
      ["gte", "opGreaterThanOrEqual"],
      ["lte", "opLessThanOrEqual"],
      ["eq", "opEquals"],
      ["is_null", "opIsEmpty"],
      ["not_null", "opNotEmpty"],
    ];
  }
  if (column.type === "date") {
    return [
      ["eq", "opEquals"],
      ["lt", "opBefore"],
      ["gt", "opAfter"],
      ["is_null", "opIsEmpty"],
      ["not_null", "opNotEmpty"],
    ];
  }
  return [
    ["contains", "opContains"],
    ["eq", "opEquals"],
    ["neq", "opNotEquals"],
    ["is_null", "opIsEmpty"],
    ["not_null", "opNotEmpty"],
  ];
}

function updatePageSubtitle() {
  const source = state.description?.data_source;
  dom.subtitle.textContent = source
    ? message("pageSubtitleSnapshot", { date: source.source_date })
    : message("pageSubtitle");
}

function applyStaticLocale() {
  document.documentElement.lang = state.locale;
  document.title = message("pageTitle");
  for (const element of document.querySelectorAll("[data-i18n]")) {
    element.textContent = message(element.dataset.i18n);
  }
  updatePageSubtitle();
  dom.searchInput.placeholder = message("searchPlaceholder");
  dom.filterValue.placeholder = message("filterValue");
  dom.searchInput.setAttribute("aria-label", message("searchLabel"));
  dom.filterColumn.setAttribute("aria-label", message("filterLabel"));
  dom.filterOperator.setAttribute("aria-label", message("filterLabel"));
  dom.filterValue.setAttribute("aria-label", message("filterValue"));
  dom.sortColumn.setAttribute("aria-label", message("sortLabel"));
  dom.sortDirection.setAttribute("aria-label", message("sortLabel"));
  dom.table.setAttribute("aria-label", message("pageTitle"));
  document.querySelector(".query-bar").setAttribute(
    "aria-label",
    message("filterLabel"),
  );
  document.querySelector("#refresh-button").setAttribute(
    "aria-label",
    message("refresh"),
  );
  document.querySelector("#refresh-button").title = message("refresh");
  renderControls();
  if (state.description && state.editors.size === 0) {
    renderTable();
  }
}

function renderControls() {
  const previousFilterColumn = dom.filterColumn.value;
  const previousFilterOperator = dom.filterOperator.value;
  const previousSortColumn = dom.sortColumn.value;
  const previousSortDirection = dom.sortDirection.value || "asc";

  dom.filterColumn.replaceChildren();
  setOption(dom.filterColumn, "", message("filterAll"), !previousFilterColumn);
  for (const column of visibleColumns()) {
    setOption(
      dom.filterColumn,
      column.column_id,
      columnLabel(column.column_id),
      previousFilterColumn === column.column_id,
    );
  }

  const selectedColumn = columnByID(dom.filterColumn.value);
  dom.filterOperator.replaceChildren();
  for (const [value, key] of operatorOptions(selectedColumn)) {
    setOption(
      dom.filterOperator,
      value,
      message(key),
      previousFilterOperator === value,
    );
  }
  const operator = dom.filterOperator.value;
  const noValue = operator === "is_null" || operator === "not_null";
  dom.filterOperatorField.hidden = !selectedColumn;
  dom.filterValueField.hidden = !selectedColumn || noValue;
  dom.filterValue.type =
    selectedColumn?.type === "date"
      ? "date"
      : selectedColumn?.type === "number"
        ? "number"
        : "text";
  if (selectedColumn?.type === "number") {
    dom.filterValue.step = "0.01";
  } else {
    dom.filterValue.removeAttribute("step");
  }

  dom.sortColumn.replaceChildren();
  setOption(dom.sortColumn, "", message("sortDefault"), !previousSortColumn);
  for (const column of visibleColumns()) {
    setOption(
      dom.sortColumn,
      column.column_id,
      columnLabel(column.column_id),
      previousSortColumn === column.column_id,
    );
  }
  dom.sortDirection.replaceChildren();
  setOption(dom.sortDirection, "asc", message("ascending"), previousSortDirection === "asc");
  setOption(dom.sortDirection, "desc", message("descending"), previousSortDirection === "desc");
  dom.sortDirection.disabled = !dom.sortColumn.value;

  const selectedField = dom.bulkField.value;
  dom.bulkField.replaceChildren();
  for (const column of editableColumns()) {
    setOption(
      dom.bulkField,
      column.column_id,
      columnLabel(column.column_id),
      selectedField === column.column_id,
    );
  }
}

function currentQuery() {
  const query = {
    search: dom.searchInput.value.trim() || undefined,
    limit: PAGE_SIZE,
    offset: (state.page - 1) * PAGE_SIZE,
    sum_column_id: "principal",
  };
  const filterColumn = dom.filterColumn.value;
  const filterOperator = dom.filterOperator.value;
  if (filterColumn && filterOperator) {
    const noValue =
      filterOperator === "is_null" || filterOperator === "not_null";
    if (noValue || dom.filterValue.value !== "") {
      query.filters = [
        {
          column_id: filterColumn,
          op: filterOperator,
          value: noValue ? null : dom.filterValue.value,
        },
      ];
    }
  }
  if (dom.sortColumn.value) {
    query.sort = [
      {
        column_id: dom.sortColumn.value,
        direction: dom.sortDirection.value,
      },
    ];
  }
  return query;
}

async function loadQuery({
  clearSelection = true,
  showRefreshed = false,
  preserveDrafts = false,
} = {}) {
  if (state.editors.size > 0 && !preserveDrafts) {
    state.pendingRefresh = true;
    return;
  }
  if (clearSelection) {
    state.selected.clear();
  }
  setLoading(true);
  try {
    state.result = await callPlugin("table.query", { query: currentQuery() });
    if (!clearSelection) {
      const visibleRowIDs = new Set(state.result.rows.map((row) => row.row_id));
      for (const rowID of state.selected) {
        if (!visibleRowIDs.has(rowID)) {
          state.selected.delete(rowID);
        }
      }
    }
    state.dataRevision = state.result.data_revision;
    state.pendingRefresh = false;
    renderTable({ rebaseDrafts: preserveDrafts });
    // A revision the summary has not caught up with means the records changed,
    // so the dashboard counts are refreshed alongside them.
    if (state.dashboard?.data_revision !== state.result.data_revision) {
      void loadDashboard();
    }
    if (showRefreshed) {
      showToast(message("refreshed"));
    }
  } catch (error) {
    showError(error);
  } finally {
    setLoading(false);
  }
}

function headerCell(label, className = "") {
  const th = document.createElement("th");
  th.scope = "col";
  th.textContent = label;
  if (className) {
    th.className = className;
  }
  return th;
}

function mainColumnClass(column) {
  return [
    `${column.column_id.replaceAll("_", "-")}-cell`,
    column.type === "number" ? "number-cell" : "",
  ]
    .filter(Boolean)
    .join(" ");
}

function renderDraftValue(cell, draft) {
  const value = document.createElement("span");
  value.className = "cell-draft";
  value.textContent =
    draft.column.type === "number"
      ? formatDecimal(draft.value)
      : String(draft.value ?? "");
  cell.replaceChildren(value);
}

function renderTable({ rebaseDrafts = false } = {}) {
  if (!state.description || !state.result) {
    return;
  }
  const activeDraft = [...state.editors.values()].find(
    (draft) => draft.editing,
  );
  for (const draft of state.editors.values()) {
    draft.value = draft.input?.value ?? draft.value ?? draft.original;
    draft.wasFocused = draft.input === document.activeElement;
    draft.selectionStart = draft.input?.selectionStart ?? null;
    draft.selectionEnd = draft.input?.selectionEnd ?? null;
    draft.input = null;
    if (draft !== activeDraft) {
      draft.editing = false;
    }
  }
  const renderedDraftKeys = new Set();
  dom.head.replaceChildren();
  const headerRow = document.createElement("tr");
  const selectHeader = document.createElement("th");
  selectHeader.className = "checkbox-cell";
  const selectPage = document.createElement("input");
  selectPage.id = "select-page-checkbox";
  selectPage.type = "checkbox";
  selectPage.dataset.action = "select-page";
  selectPage.setAttribute("aria-label", message("selectPage"));
  const pageRowIDs = state.result.rows.map((row) => row.row_id);
  selectPage.checked =
    pageRowIDs.length > 0 && pageRowIDs.every((rowID) => state.selected.has(rowID));
  selectPage.indeterminate =
    !selectPage.checked && pageRowIDs.some((rowID) => state.selected.has(rowID));
  selectHeader.append(selectPage);
  headerRow.append(selectHeader);

  for (const column of MAIN_COLUMNS) {
    headerRow.append(
      headerCell(columnLabel(column.column_id), mainColumnClass(column)),
    );
  }
  headerRow.append(headerCell(message("column.actions"), "actions-cell"));
  dom.head.append(headerRow);

  dom.bodyRows.replaceChildren();
  for (const row of state.result.rows) {
    const tr = document.createElement("tr");
    tr.dataset.rowId = row.row_id;
    tr.dataset.selected = state.selected.has(row.row_id) ? "true" : "false";

    const selectCell = document.createElement("td");
    selectCell.className = "checkbox-cell";
    const checkbox = document.createElement("input");
    checkbox.id = `row-select-${row.row_id}`;
    checkbox.type = "checkbox";
    checkbox.checked = state.selected.has(row.row_id);
    checkbox.dataset.action = "select-row";
    checkbox.dataset.rowId = row.row_id;
    checkbox.setAttribute("aria-label", message("selectRow"));
    selectCell.append(checkbox);
    tr.append(selectCell);

    for (const column of MAIN_COLUMNS) {
      const td = document.createElement("td");
      td.id = `cell-${row.row_id}-${column.column_id}`;
      td.dataset.action = "edit-cell";
      td.dataset.rowId = row.row_id;
      td.dataset.columnId = column.column_id;
      td.dataset.editable = isEditableColumn(column.column_id) ? "true" : "false";
      td.dataset.contextChanged = row.context_changed ? "true" : "false";
      td.classList.add(...mainColumnClass(column).split(" "));
      td.tabIndex = 0;
      td.setAttribute(
        "aria-label",
        message(isEditableColumn(column.column_id) ? "editCellAria" : "readOnlyCellAria", {
          column: columnLabel(column.column_id),
        }),
      );
      const draftKey = `${row.row_id}:${column.column_id}`;
      const draft = state.editors.get(draftKey);
      if (draft) {
        renderedDraftKeys.add(draftKey);
        draft.row = row;
        draft.original = row.values[column.column_id] ?? "";
        if (rebaseDrafts) {
          draft.commandId = newCommandId();
          draft.submitted = false;
          draft.lastSubmittedValue = null;
        }
        draft.column = column;
        if (draft.editing) {
          mountCellEditor(td, draft);
        } else {
          renderDraftValue(td, draft);
        }
        tr.append(td);
        continue;
      }
      const value = row.values[column.column_id];
      if (
        (column.column_id === "repayment_status" ||
          column.column_id === "plan_status") &&
        value
      ) {
        const badge = document.createElement("span");
        badge.className = "status-badge";
        badge.textContent = localizedValue(column.column_id, value);
        td.append(badge);
      } else {
        td.textContent = cellText(column, value, row);
      }
      td.title = value === null || value === undefined ? "" : String(value);
      tr.append(td);
    }

    const actions = document.createElement("td");
    actions.className = "actions-cell";
    const detailButton = document.createElement("button");
    detailButton.id = `detail-${row.row_id}`;
    detailButton.className = "row-action";
    detailButton.type = "button";
    detailButton.dataset.action = "open-detail";
    detailButton.dataset.rowId = row.row_id;
    detailButton.textContent = message("detailShort");
    detailButton.title = message("detailActionAria");
    detailButton.setAttribute("aria-label", message("detailActionAria"));
    const deleteButton = document.createElement("button");
    deleteButton.id = `delete-${row.row_id}`;
    deleteButton.className = "row-delete";
    deleteButton.type = "button";
    deleteButton.dataset.action = "delete-row";
    deleteButton.dataset.rowId = row.row_id;
    deleteButton.textContent = "×";
    deleteButton.title = message("deleteRowAria");
    deleteButton.setAttribute("aria-label", message("deleteRowAria"));
    actions.append(detailButton, deleteButton);
    tr.append(actions);
    dom.bodyRows.append(tr);
  }
  let staleDraft = false;
  for (const key of state.editors.keys()) {
    if (!renderedDraftKeys.has(key)) {
      state.editors.delete(key);
      staleDraft = true;
    }
  }
  if (staleDraft) {
    state.pendingRefresh = true;
    showToast(message("draftConflict"), "error", "stale-draft");
  }

  const hasRows = state.result.rows.length > 0;
  dom.table.hidden = !hasRows;
  dom.empty.hidden = hasRows;
  dom.emptyTitle.textContent =
    state.result.matched_count === 0 && (dom.searchInput.value || dom.filterColumn.value)
      ? message("noMatches")
      : message("emptyTitle");
  dom.emptyBody.textContent =
    state.result.matched_count === 0 && (dom.searchInput.value || dom.filterColumn.value)
      ? ""
      : message("emptyBody");

  const pageCount = Math.max(1, Math.ceil(state.result.matched_count / PAGE_SIZE));
  dom.recordCount.textContent = message("recordCount", {
    count: state.summary?.row_count ?? state.result.matched_count,
  });
  dom.selectedCount.textContent = message("selectedCount", {
    count: state.selected.size,
  });
  dom.clearSelection.disabled = state.selected.size === 0;
  dom.filteredCount.textContent = message("filteredCount", {
    count: state.result.matched_count,
  });
  dom.sumLabel.textContent = `${message("sumLabel")}: ${formatDecimal(
    state.result.sum?.value ?? "0.00",
  )}`;
  dom.pageStatus.textContent = message("pageStatus", {
    page: Math.min(state.page, pageCount),
    pages: pageCount,
  });
  dom.previous.disabled = state.page <= 1;
  dom.next.disabled = state.page >= pageCount;
  renderDashboard();
  const bulkOpen = state.openDrawer === dom.bulkDrawer;
  dom.bulkTargetSelectedLabel.textContent = message("selectedCount", {
    count: bulkOpen ? state.bulkSelectedTargets.length : state.selected.size,
  });
  dom.bulkTargetFilteredLabel.textContent = message("filteredCount", {
    count: bulkOpen ? state.bulkFilteredCount : state.result.matched_count,
  });
}

function resetFilterInputs() {
  dom.searchInput.value = "";
  dom.filterColumn.value = "";
  dom.filterOperator.replaceChildren();
  dom.filterValue.value = "";
  dom.sortColumn.value = "";
  dom.sortDirection.value = "asc";
  state.page = 1;
  renderControls();
  void loadQuery();
}

function rowByID(rowID) {
  return state.result?.rows.find((row) => row.row_id === rowID);
}

function mountCellEditor(cell, draft, { focus = false } = {}) {
  const input = document.createElement("input");
  input.id = `cell-editor-${draft.row.row_id}-${draft.column.column_id}`;
  input.className = "cell-editor";
  if (draft.column.type === "number") {
    input.classList.add("number-cell");
  }
  input.type = draft.column.type === "date" ? "date" : "text";
  if (draft.column.type === "number") {
    input.inputMode = "decimal";
  }
  input.value = draft.value ?? draft.original;
  input.dataset.action = "edit-cell-input";
  input.disabled = draft.saving;
  draft.input = input;
  draft.editing = true;
  cell.replaceChildren(input);

  input.addEventListener("compositionstart", () => {
    draft.composing = true;
  });
  input.addEventListener("compositionend", () => {
    draft.composing = false;
  });
  input.addEventListener("input", () => {
    draft.value = input.value;
    if (
      draft.submitted &&
      input.value !== draft.lastSubmittedValue &&
      !draft.saving
    ) {
      draft.commandId = newCommandId();
      draft.submitted = false;
    }
    input.removeAttribute("aria-invalid");
  });
  input.addEventListener("keydown", (event) => {
    if (event.isComposing || draft.composing || event.keyCode === 229) {
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      cancelCellEdit(draft);
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      void commitCellEdit(draft, false);
      return;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      void commitCellEdit(draft, true, event.shiftKey);
    }
  });

  if (focus) {
    input.focus();
    input.select();
  } else if (draft.wasFocused) {
    queueMicrotask(() => {
      if (!input.isConnected) {
        return;
      }
      input.focus();
      if (draft.selectionStart !== null && draft.selectionEnd !== null) {
        input.setSelectionRange(draft.selectionStart, draft.selectionEnd);
      }
    });
  }
  draft.wasFocused = false;
}

function beginCellEdit(cell) {
  const displayColumn = MAIN_COLUMN_BY_ID.get(cell.dataset.columnId);
  if (!displayColumn || !isEditableColumn(displayColumn.column_id)) {
    return;
  }
  const activeDraft = state.editors.values().next().value;
  if (activeDraft) {
    const value =
      activeDraft.input?.value ??
      activeDraft.value ??
      activeDraft.original;
    activeDraft.value = value;
    if (value !== activeDraft.original) {
      if (activeDraft.editing && activeDraft.input?.isConnected) {
        activeDraft.input.focus();
        activeDraft.input.select();
      } else {
        const draftCell = document.querySelector(
          `#cell-${CSS.escape(activeDraft.row.row_id)}-${CSS.escape(activeDraft.column.column_id)}`,
        );
        mountCellEditor(draftCell, activeDraft, { focus: true });
      }
      return;
    }
    closeCellEdit(activeDraft);
  }
  const row = rowByID(cell.dataset.rowId);
  const schemaColumn = columnByID(cell.dataset.columnId);
  const column = { ...displayColumn, schemaColumn };
  if (!row || !schemaColumn) {
    return;
  }
  const original = row.values[column.column_id] ?? "";
  const draft = {
    row,
    column,
    input: null,
    value: original,
    original,
    commandId: newCommandId(),
    submitted: false,
    lastSubmittedValue: null,
    composing: false,
    saving: false,
    editing: true,
    wasFocused: false,
    selectionStart: null,
    selectionEnd: null,
  };
  state.editors.set(`${row.row_id}:${column.column_id}`, draft);
  mountCellEditor(cell, draft, { focus: true });
}

function closeCellEdit(draft, { triggerRefresh = true } = {}) {
  state.editors.delete(`${draft.row.row_id}:${draft.column.column_id}`);
  const column = draft.column;
  const value = draft.row.values[column.column_id];
  const td = document.querySelector(
    `#cell-${CSS.escape(draft.row.row_id)}-${CSS.escape(column.column_id)}`,
  );
  if (td) {
    if (
      (column.column_id === "repayment_status" ||
        column.column_id === "plan_status") &&
      value
    ) {
      const badge = document.createElement("span");
      badge.className = "status-badge";
      badge.textContent = localizedValue(column.column_id, value);
      td.replaceChildren(badge);
    } else {
      td.textContent = cellText(column, value, draft.row);
    }
    td.title = value === null || value === undefined ? "" : String(value);
  }
  if (
    triggerRefresh &&
    state.editors.size === 0 &&
    state.pendingRefresh
  ) {
    void loadQuery({ clearSelection: false });
  }
}

function cancelCellEdit(draft) {
  closeCellEdit(draft);
}

function settleCellDraft() {
  const draft = state.editors.values().next().value;
  if (!draft) {
    return;
  }
  const value = draft.input?.value ?? draft.value ?? draft.original;
  draft.value = value;
  if (value !== draft.original) {
    const cell = document.querySelector(
      `#cell-${CSS.escape(draft.row.row_id)}-${CSS.escape(draft.column.column_id)}`,
    );
    if (cell) {
      draft.editing = false;
      draft.input = null;
      renderDraftValue(cell, draft);
    }
    return;
  }
  closeCellEdit(draft);
}

function adjacentCellTarget(draft, backwards) {
  const rows = state.result?.rows ?? [];
  const columns = MAIN_COLUMNS.filter(
    (column) =>
      isEditableColumn(column.column_id) && columnByID(column.column_id),
  );
  const rowIndex = rows.findIndex((row) => row.row_id === draft.row.row_id);
  const columnIndex = columns.findIndex(
    (column) => column.column_id === draft.column.column_id,
  );
  if (rowIndex < 0 || columnIndex < 0) {
    return null;
  }
  let flatIndex = rowIndex * columns.length + columnIndex + (backwards ? -1 : 1);
  const total = rows.length * columns.length;
  if (flatIndex < 0 || flatIndex >= total) {
    return null;
  }
  const targetRow = rows[Math.floor(flatIndex / columns.length)];
  const targetColumn = columns[flatIndex % columns.length];
  return {
    rowID: targetRow.row_id,
    columnID: targetColumn.column_id,
  };
}

function focusCellTarget(target) {
  if (!target) {
    return;
  }
  const cell = document.querySelector(
    `#cell-${CSS.escape(target.rowID)}-${CSS.escape(target.columnID)}`,
  );
  if (cell) {
    beginCellEdit(cell);
  }
}

async function commitCellEdit(draft, moveFocus, backwards = false) {
  if (draft.saving) {
    return;
  }
  const rawValue = draft.input.value;
  const value = rawValue === "" ? null : rawValue;
  const focusTarget = moveFocus ? adjacentCellTarget(draft, backwards) : null;
  if (value === (draft.row.values[draft.column.column_id] ?? null)) {
    const refreshPending = state.pendingRefresh;
    closeCellEdit(draft, { triggerRefresh: false });
    if (refreshPending) {
      await loadQuery({ clearSelection: false });
    } else if (moveFocus) {
      focusCellTarget(focusTarget);
    }
    return;
  }
  draft.saving = true;
  draft.submitted = true;
  draft.lastSubmittedValue = rawValue;
  draft.input.disabled = true;
  try {
    await callPlugin("table.change", {
      viewInstanceId: state.viewInstanceId,
      commandId: draft.commandId,
      change: {
        action: "update",
        expected_schema_revision: state.description.table.schema_revision,
        targets: [
          {
            row_id: draft.row.row_id,
            expected_revision: draft.row.revision,
          },
        ],
        values: { [draft.column.column_id]: value },
      },
    });
    closeCellEdit(draft, { triggerRefresh: false });
    showToast(message("saved"));
    await loadQuery({ clearSelection: false });
    if (moveFocus) {
      focusCellTarget(focusTarget);
    }
  } catch (error) {
    const payload = showError(error);
    draft.input.disabled = false;
    draft.input.focus();
    if (
      payload.code === "version_conflict" ||
      payload.code === "command_id_conflict"
    ) {
      draft.input.setAttribute("aria-invalid", "true");
      showToast(message("draftConflict"), "error", "draft-conflict");
    }
  } finally {
    draft.saving = false;
  }
}

function selectPage(checked) {
  cancelPendingCellClick();
  for (const row of state.result?.rows ?? []) {
    if (checked) {
      state.selected.add(row.row_id);
    } else {
      state.selected.delete(row.row_id);
    }
  }
  updateSelectionUI();
}

function selectRow(rowID, checked) {
  cancelPendingCellClick();
  if (checked) {
    state.selected.add(rowID);
  } else {
    state.selected.delete(rowID);
  }
  const row = dom.bodyRows.querySelector(
    `tr[data-row-id="${CSS.escape(rowID)}"]`,
  );
  if (row) {
    row.dataset.selected = checked ? "true" : "false";
  }
  updateSelectionUI();
}

function updateSelectionUI() {
  const rows = state.result?.rows ?? [];
  const pageRowIDs = rows.map((row) => row.row_id);
  const selectedOnPage = pageRowIDs.filter((rowID) => state.selected.has(rowID));
  const header = document.querySelector("#select-page-checkbox");
  if (header) {
    header.checked =
      pageRowIDs.length > 0 && selectedOnPage.length === pageRowIDs.length;
    header.indeterminate =
      !header.checked && selectedOnPage.length > 0;
  }
  for (const row of rows) {
    const selected = state.selected.has(row.row_id);
    const checkbox = document.querySelector(
      `#row-select-${CSS.escape(row.row_id)}`,
    );
    const tableRow = dom.bodyRows.querySelector(
      `tr[data-row-id="${CSS.escape(row.row_id)}"]`,
    );
    if (checkbox) {
      checkbox.checked = selected;
    }
    if (tableRow) {
      tableRow.dataset.selected = selected ? "true" : "false";
    }
  }
  dom.selectedCount.textContent = message("selectedCount", {
    count: state.selected.size,
  });
  dom.clearSelection.disabled = state.selected.size === 0;
  if (state.openDrawer === dom.bulkDrawer) {
    dom.bulkTargetSelectedLabel.textContent = message("selectedCount", {
      count: state.bulkSelectedTargets.length,
    });
  }
}

function toggleRowSelection(rowID) {
  selectRow(rowID, !state.selected.has(rowID));
}

function clearSelection() {
  cancelPendingCellClick();
  state.selected.clear();
  updateSelectionUI();
}

function scheduleRowSelectionToggle(cell) {
  const rowID = cell.dataset.rowId;
  const columnID = cell.dataset.columnId;
  settlePendingCellClick();
  const timer = setTimeout(() => {
    if (state.pendingCellClick?.timer !== timer) {
      return;
    }
    state.pendingCellClick = null;
    toggleRowSelection(rowID);
  }, 180);
  state.pendingCellClick = { rowID, columnID, timer };
}

function settlePendingCellClick() {
  const pending = state.pendingCellClick;
  if (!pending) {
    return;
  }
  state.pendingCellClick = null;
  clearTimeout(pending.timer);
  toggleRowSelection(pending.rowID);
}

function cancelPendingCellClick(cell = null) {
  const pending = state.pendingCellClick;
  if (
    !pending ||
    (cell &&
      (pending.rowID !== cell.dataset.rowId ||
        pending.columnID !== cell.dataset.columnId))
  ) {
    return;
  }
  state.pendingCellClick = null;
  clearTimeout(pending.timer);
}

async function deleteRow(rowID) {
  const row = rowByID(rowID);
  if (!row || !state.description) {
    return;
  }
  setLoading(true);
  try {
    await callPlugin("table.change", {
      viewInstanceId: state.viewInstanceId,
      commandId: newCommandId(),
      change: {
        action: "delete",
        expected_schema_revision: state.description.table.schema_revision,
        targets: [{ row_id: row.row_id, expected_revision: row.revision }],
      },
    });
    state.selected.delete(rowID);
    showToast(message("deleted"));
    await loadQuery({ clearSelection: false });
  } catch (error) {
    showError(error);
  } finally {
    setLoading(false);
  }
}

function detailSection(titleKey) {
  const section = document.createElement("section");
  section.className = "detail-section";
  const heading = document.createElement("h3");
  heading.textContent = message(titleKey);
  section.append(heading);
  return section;
}

function detailLabel(labelKey) {
  return message(`detailField.${labelKey}`);
}

function appendDetailFields(section, fields) {
  const list = document.createElement("dl");
  list.className = "detail-fields";
  for (const [labelKey, value] of fields) {
    const row = document.createElement("div");
    const term = document.createElement("dt");
    const description = document.createElement("dd");
    term.textContent = detailLabel(labelKey);
    description.textContent =
      value === null || value === undefined || value === "" ? "—" : String(value);
    row.append(term, description);
    list.append(row);
  }
  section.append(list);
}

function detailNote(textValue) {
  const note = document.createElement("p");
  note.className = "detail-note";
  note.textContent = textValue;
  return note;
}

function renderDetail(data) {
  dom.detailBody.replaceChildren();
  const debt = data.debt;
  dom.detailBody.append(
    detailNote(
      message("detailSourceNote", {
        date: data.data_source?.source_date ?? "—",
      }),
    ),
  );

  const debtSection = detailSection("detailDebtSection");
  appendDetailFields(debtSection, [
    ["institution", debt.institution],
    ["name", debt.name],
    ["category", localizedValue("category", debt.category)],
    ["funding_source", localizedValue("funding_source", debt.funding_source)],
    ["product_type", label("product_type", debt.product_type)],
    ["loan_code", debt.loan_code],
    ["contract_reference", debt.contract_reference],
    ["product_group_id", debt.product_group_id],
    ["loan_sequence", debt.loan_sequence],
    ["borrowed_on", debt.borrowed_on],
    ["disbursed_on", debt.disbursed_on],
    ["principal", debt.principal],
    ["actual_received", debt.actual_received],
    ["remaining_due", debt.remaining_due],
    ["annualized_rate", debt.annualized_rate],
    ["original_rate_value", debt.original_rate_value],
    ["rate_period", debt.rate_period],
    ["manual_annualized_rate_percent", debt.manual_annualized_rate_percent],
    ["calculated_annualized_rate_percent", debt.calculated_annualized_rate_percent],
    ["interest_basis", label("interest_basis", debt.interest_basis)],
    ["rate_period", label("rate_period", debt.rate_period)],
    ["opening_principal", debt.opening_principal],
    ["planned_interest", debt.planned_interest],
    ["remaining_interest", debt.remaining_interest],
    ["actual_paid", debt.actual_paid],
    ["source_record_id", debt.source_record_id],
    ["source_created_at", debt.source_created_at],
    ["source_updated_at", debt.source_updated_at],
  ]);
  dom.detailBody.append(debtSection);

  const planSection = detailSection("detailPlanSection");
  if (data.current_plan) {
    const counts = Object.entries(data.installment_counts ?? {})
      .map(([status, count]) => `${label("installment", status)} ${count}`)
      .join(" · ");
    appendDetailFields(planSection, [
      ["plan_status", label("plan_status", data.current_plan.status)],
      ["plan_method", label("method", data.current_plan.method)],
      ["effective_on", data.current_plan.effective_on],
      ["first_due_on", data.current_plan.first_due_on],
      ["installments", data.current_plan.installments],
      ["installment_status", counts || "—"],
      ["plan_principal", data.current_plan.plan_principal],
      ["confirmed_remaining", data.current_plan.confirmed_remaining],
      ["original_rate_value", data.current_plan.original_rate_value],
      ["rate_period", label("rate_period", data.current_plan.rate_period)],
      ["source_updated_at", data.current_plan.updated_at],
    ]);
    const rows = data.current_plan.installment_rows ?? [];
    if (rows.length > 0) {
      const schedule = document.createElement("div");
      schedule.className = "detail-records";
      for (const installment of rows) {
        const record = document.createElement("article");
        const title = document.createElement("strong");
        title.textContent = message("detailInstallmentTitle", {
          no: installment.installment_no,
          due: installment.due_on,
        });
        const meta = document.createElement("span");
        meta.textContent = message("detailInstallmentMeta", {
          principal: formatDecimal(installment.principal),
          interest: formatDecimal(installment.interest),
          status: label("installment", installment.status),
        });
        record.append(title, meta);
        schedule.append(record);
      }
      planSection.append(schedule);
    }
  } else {
    planSection.append(detailNote(message("detailNoPlan")));
  }
  dom.detailBody.append(planSection);

  const paymentSection = detailSection("detailPaymentsSection");
  appendDetailFields(paymentSection, [
    [
      "payment_count",
      data.payment_summary?.count ?? 0,
    ],
    ["payment_total", data.payment_summary?.total],
  ]);
  if (data.recent_payments.length === 0) {
    paymentSection.append(detailNote(message("detailNoPayments")));
  } else {
    const recent = document.createElement("div");
    recent.className = "detail-records";
    for (const payment of data.recent_payments) {
      const record = document.createElement("article");
      const title = document.createElement("strong");
      title.textContent = `${payment.payment_on} · ${formatDecimal(payment.amount)}`;
      const meta = document.createElement("span");
      meta.textContent = [
        payment.principal_effect ? label("principal_effect", payment.principal_effect) : "",
        payment.note,
      ]
        .filter(Boolean)
        .join(" · ");
      record.append(title, meta);
      recent.append(record);
    }
    paymentSection.append(recent);
  }
  dom.detailBody.append(paymentSection);

  const eventSection = detailSection("detailEventsSection");
  appendDetailFields(eventSection, [
    ["event_count", data.event_summary?.count ?? 0],
  ]);
  if (data.recent_events.length === 0) {
    eventSection.append(detailNote(message("detailNoEvents")));
  } else {
    const recent = document.createElement("div");
    recent.className = "detail-records";
    for (const eventItem of data.recent_events) {
      const record = document.createElement("article");
      const title = document.createElement("strong");
      title.textContent = `${eventItem.occurred_at.slice(0, 10)} · ${eventItem.title}`;
      const meta = document.createElement("span");
      meta.textContent = [eventItem.event_type, eventItem.content]
        .filter(Boolean)
        .join(" · ");
      const amount = document.createElement("span");
      amount.textContent = message("detailEventAmount", {
        amount: formatDecimal(eventItem.amount),
      });
      record.append(title, meta, amount);
      recent.append(record);
    }
    eventSection.append(recent);
  }
  dom.detailBody.append(eventSection);
}

async function openDetail(rowID) {
  const row = rowByID(rowID);
  if (!row) {
    return;
  }
  openDrawer(dom.detailDrawer);
  dom.detailBody.replaceChildren(detailNote(message("loading")));
  setLoading(true);
  try {
    const detail = await callPlugin("table.detail", { rowId: rowID });
    renderDetail(detail);
  } catch (error) {
    showError(error);
    dom.detailBody.replaceChildren(detailNote(localizedError(error)));
  } finally {
    setLoading(false);
  }
}

function openDrawer(name) {
  settleCellDraft();
  for (const drawer of [
    dom.historyDrawer,
    dom.detailDrawer,
    dom.rowDrawer,
    dom.bulkDrawer,
  ]) {
    drawer.hidden = drawer !== name;
    drawer.setAttribute("aria-hidden", drawer === name ? "false" : "true");
  }
  dom.backdrop.hidden = false;
  state.openDrawer = name;
}

function closeDrawer() {
  for (const drawer of [
    dom.historyDrawer,
    dom.detailDrawer,
    dom.rowDrawer,
    dom.bulkDrawer,
  ]) {
    drawer.hidden = true;
    drawer.setAttribute("aria-hidden", "true");
  }
  dom.backdrop.hidden = true;
  state.openDrawer = null;
  state.rowCommandId = null;
  state.rowAttemptedValue = null;
  state.bulkCommandId = null;
  state.bulkContextId = null;
  state.bulkFilteredCount = 0;
  state.bulkSelectedTargets = [];
  if (state.pendingRefresh && state.editors.size === 0) {
    void loadQuery({ clearSelection: true });
  }
}

function inputForColumn(column, prefix) {
  const input = document.createElement("input");
  input.id = `${prefix}-${column.column_id}`;
  input.dataset.action = `${prefix}-field`;
  input.dataset.columnId = column.column_id;
  input.type = column.type === "date" ? "date" : "text";
  if (column.type === "number") {
    input.inputMode = "decimal";
  }
  if (column.required) {
    input.required = true;
  }
  return input;
}

function openRowEditor() {
  dom.rowFields.replaceChildren();
  state.rowCommandId = newCommandId();
  state.rowAttemptedValue = null;
  for (const column of editableColumns()) {
    const label = document.createElement("label");
    const caption = document.createElement("span");
    caption.textContent = columnLabel(column.column_id);
    label.append(caption, inputForColumn(column, "new-row"));
    dom.rowFields.append(label);
  }
  dom.rowForm.reset();
  openDrawer(dom.rowDrawer);
  dom.rowFields.querySelector("input")?.focus();
}

function rowFormValues() {
  const values = {};
  for (const column of editableColumns()) {
    const input = dom.rowFields.querySelector(
      `[data-column-id="${CSS.escape(column.column_id)}"]`,
    );
    values[column.column_id] = input?.value === "" ? null : input?.value ?? null;
  }
  return values;
}

async function saveNewRow(event) {
  event.preventDefault();
  if (!state.description || !state.rowCommandId) {
    return;
  }
  const values = rowFormValues();
  const serialized = JSON.stringify(values);
  if (state.rowAttemptedValue !== null && state.rowAttemptedValue !== serialized) {
    state.rowCommandId = newCommandId();
  }
  state.rowAttemptedValue = serialized;
  dom.rowSave.disabled = true;
  try {
    await callPlugin("table.change", {
      viewInstanceId: state.viewInstanceId,
      commandId: state.rowCommandId,
      change: {
        action: "insert",
        expected_schema_revision: state.description.table.schema_revision,
        rows: [values],
      },
    });
    closeDrawer();
    showToast(message("added"));
    await loadQuery();
  } catch (error) {
    showError(error);
  } finally {
    dom.rowSave.disabled = false;
  }
}

async function openBulkEditor() {
  if (!state.result || state.bulkOpening) {
    return;
  }
  const selectedTargets = state.result.rows
    .filter((row) => state.selected.has(row.row_id))
    .map((row) => ({
      row_id: row.row_id,
      expected_revision: row.revision,
    }));
  state.bulkOpening = true;
  setLoading(true);
  try {
    const context = await callPlugin("table.capture_context", {
      query: currentQuery(),
    });
    if (!context.context_id) {
      throw new Error("Context capture did not return an ID.");
    }
    if (selectedTargets.length === 0 && context.context.captured_count === 0) {
      throw Object.assign(new Error(message("error.empty_context")), {
        message: JSON.stringify({ code: "empty_context" }),
      });
    }
    state.bulkCommandId = newCommandId();
    state.bulkContextId = context.context_id;
    state.bulkFilteredCount = context.context.captured_count;
    state.bulkSelectedTargets = selectedTargets;
    dom.bulkForm.reset();
    dom.bulkTargetSelected.disabled = selectedTargets.length === 0;
    dom.bulkTargetSelected.checked = selectedTargets.length > 0;
    dom.bulkTargetFiltered.checked = selectedTargets.length === 0;
    dom.bulkValue.value = "";
    dom.bulkClear.checked = false;
    renderControls();
    openDrawer(dom.bulkDrawer);
    renderTable();
    dom.bulkField.focus();
  } catch (error) {
    showError(error);
  } finally {
    state.bulkOpening = false;
    setLoading(false);
  }
}

async function applyBulk(event) {
  event.preventDefault();
  if (
    !state.description ||
    !state.result ||
    !state.bulkCommandId ||
    !state.bulkContextId
  ) {
    return;
  }
  const field = dom.bulkField.value;
  if (!field) {
    return;
  }
  const value = dom.bulkClear.checked
    ? null
    : dom.bulkValue.value === ""
      ? null
      : dom.bulkValue.value;
  const selectedMode = dom.bulkTargetSelected.checked;
  const change = {
    action: "update",
    expected_schema_revision: state.description.table.schema_revision,
    values: { [field]: value },
  };
  if (selectedMode) {
    const targets = state.bulkSelectedTargets;
    if (targets.length === 0) {
      showToast(message("error.empty_context"), "error");
      return;
    }
    change.targets = targets;
  } else {
    if (state.bulkFilteredCount === 0) {
      showToast(message("error.empty_context"), "error");
      return;
    }
    change.context_id = state.bulkContextId;
  }
  dom.bulkApply.disabled = true;
  setLoading(true);
  try {
    await callPlugin("table.change", {
      viewInstanceId: state.viewInstanceId,
      commandId: state.bulkCommandId,
      change,
    });
    closeDrawer();
    showToast(message("bulkSaved"));
    await loadQuery();
  } catch (error) {
    showError(error);
  } finally {
    dom.bulkApply.disabled = false;
    setLoading(false);
  }
}

async function sendToChat() {
  if (!state.result) {
    return;
  }
  const selected = state.selected.size > 0;
  const count = selected ? state.selected.size : state.result.matched_count;
  if (count === 0) {
    showToast(message("error.empty_context"), "error");
    return;
  }
  setLoading(true);
  try {
    await callPlugin("selection.open_chat", {
      query: currentQuery(),
      selectedRowIds: selected ? [...state.selected] : undefined,
      text: message("chatPrompt"),
      label: message("chatLabel", { count }),
    });
    showToast(message("contextOpened"));
  } catch (error) {
    showError(error);
  } finally {
    setLoading(false);
  }
}

function historyTime(value) {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(state.locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function historyAction(action) {
  const keys = {
    insert: "historyActionInsert",
    update: "historyActionUpdate",
    delete: "historyActionDelete",
    undo: "historyActionUndo",
  };
  return message(keys[action] ?? "historyActionUpdate");
}

function historySource(sourceKind) {
  return message(sourceKind === "page" ? "historySourcePage" : "historySourceAgent");
}

function renderHistory(operations) {
  dom.historyList.replaceChildren();
  if (operations.length === 0) {
    const empty = document.createElement("p");
    empty.className = "history-meta";
    empty.textContent = message("historyEmpty");
    dom.historyList.append(empty);
    return;
  }
  for (const operation of operations) {
    const item = document.createElement("article");
    item.className = "history-item";
    const title = document.createElement("strong");
    title.textContent = historyAction(operation.receipt?.action);
    const meta = document.createElement("div");
    meta.className = "history-meta";
    meta.textContent = [
      historySource(operation.source_kind),
      message("historyAffected", {
        count: operation.receipt?.affected_count ?? 0,
      }),
      historyTime(operation.created_at),
    ].join(" · ");
    item.append(title, meta);
    if (operation.undoable) {
      const actions = document.createElement("div");
      actions.className = "history-actions";
      const undo = document.createElement("button");
      undo.id = `undo-${operation.operation_id}`;
      undo.className = "button quiet";
      undo.type = "button";
      undo.dataset.action = "undo-operation";
      undo.dataset.operationId = operation.operation_id;
      undo.textContent = message("undo");
      actions.append(undo);
      item.append(actions);
    }
    dom.historyList.append(item);
  }
}

async function openHistory() {
  openDrawer(dom.historyDrawer);
  dom.historyList.replaceChildren();
  setLoading(true);
  try {
    const history = await callPlugin("table.history", {
      query: { limit: 50, offset: 0 },
    });
    renderHistory(history.operations ?? []);
  } catch (error) {
    showError(error);
  } finally {
    setLoading(false);
  }
}

async function undoOperation(operationID) {
  const commandId =
    state.historyCommandIds.get(operationID) ?? newCommandId();
  state.historyCommandIds.set(operationID, commandId);
  setLoading(true);
  try {
    await callPlugin("table.undo", {
      operationId: operationID,
      commandId,
      viewInstanceId: state.viewInstanceId,
    });
    state.historyCommandIds.delete(operationID);
    showToast(message("undoDone"));
    await loadQuery({ clearSelection: true });
    const history = await callPlugin("table.history", {
      query: { limit: 50, offset: 0 },
    });
    renderHistory(history.operations ?? []);
  } catch (error) {
    showError(error);
  } finally {
    setLoading(false);
  }
}

function dirtyDraftExists() {
  return (
    state.editors.size > 0 ||
    (state.openDrawer === dom.rowDrawer &&
      [...dom.rowFields.querySelectorAll("input")].some((input) => input.value)) ||
    (state.openDrawer === dom.bulkDrawer &&
      (dom.bulkValue.value !== "" || dom.bulkClear.checked))
  );
}

async function pollVersion() {
  if (
    document.visibilityState !== "visible" ||
    state.pollInFlight ||
    !state.description
  ) {
    return;
  }
  state.pollInFlight = true;
  try {
    const summary = await callPlugin("table.version");
    if (
      state.dataRevision !== null &&
      summary.data_revision !== state.dataRevision
    ) {
      state.dataRevision = summary.data_revision;
      state.summary = summary;
      if (dirtyDraftExists()) {
        state.pendingRefresh = true;
        showToast(message("dataChanged"), "error", "remote-data-change");
      } else {
        await loadQuery({ clearSelection: true });
      }
    }
  } catch (error) {
    if (document.visibilityState === "visible") {
      showError(error);
    }
  } finally {
    state.pollInFlight = false;
  }
}

function startPolling() {
  if (state.pollTimer) {
    clearInterval(state.pollTimer);
  }
  if (document.visibilityState === "visible") {
    state.pollTimer = setInterval(() => {
      void pollVersion();
    }, 1000);
  }
}

function stopPolling() {
  if (state.pollTimer) {
    clearInterval(state.pollTimer);
    state.pollTimer = null;
  }
}

async function bootstrap() {
  setLoading(true);
  try {
    const boot = await callPlugin("table.bootstrap");
    state.description = boot.description;
    state.summary = boot.summary;
    state.dataRevision = boot.summary.data_revision;
    updatePageSubtitle();
    renderControls();
    await Promise.all([loadQuery(), loadDashboard()]);
  } catch (error) {
    showPageError(error);
  } finally {
    setLoading(false);
    dom.body.dataset.ready = "true";
    startPolling();
  }
}

async function applyLocale(locale) {
  const selected = SUPPORTED_LOCALES.has(locale) ? locale : "en";
  try {
    state.locale = selected;
    state.messages = await loadLocale(selected);
  } catch (error) {
    if (selected !== "en") {
      state.locale = "en";
      state.messages = await loadLocale("en");
    } else {
      throw error;
    }
  }
  applyStaticLocale();
}

document.addEventListener("click", (event) => {
  const cell = event.target.closest("input")
    ? null
    : event.target.closest("td[data-action='edit-cell']");
  if (cell) {
    cell.focus({ preventScroll: true });
    const draft = state.editors.values().next().value;
    const sameDraft =
      draft &&
      draft.row.row_id === cell.dataset.rowId &&
      draft.column.column_id === cell.dataset.columnId;
    if (!sameDraft) {
      settleCellDraft();
    }
    if (event.detail > 1) {
      cancelPendingCellClick(cell);
    } else {
      scheduleRowSelectionToggle(cell);
    }
  } else if (
    !event.target.closest("button, input, select, label, aside, [role='toolbar']")
  ) {
    settleCellDraft();
  }
  const actionElement = event.target.closest("[data-action]");
  if (!actionElement) {
    return;
  }
  const action = actionElement.dataset.action;
  if (action === "add-row") {
    openRowEditor();
  } else if (action === "send-chat") {
    void sendToChat();
  } else if (action === "bulk-set") {
    void openBulkEditor();
  } else if (action === "open-history") {
    void openHistory();
  } else if (action === "refresh") {
    void loadQuery({
      clearSelection: false,
      showRefreshed: true,
      preserveDrafts: true,
    });
    void loadDashboard();
  } else if (action === "clear-filter") {
    resetFilterInputs();
  } else if (action === "apply-preset") {
    applyPreset(actionElement.dataset.presetId);
  } else if (action === "clear-selection") {
    clearSelection();
  } else if (action === "page-previous") {
    if (state.page > 1) {
      state.page -= 1;
      void loadQuery();
    }
  } else if (action === "page-next") {
    state.page += 1;
    void loadQuery();
  } else if (action === "delete-row") {
    void deleteRow(actionElement.dataset.rowId);
  } else if (action === "open-detail") {
    void openDetail(actionElement.dataset.rowId);
  } else if (action === "close-drawer") {
    closeDrawer();
  } else if (action === "undo-operation") {
    void undoOperation(actionElement.dataset.operationId);
  }
});

dom.table.addEventListener("dblclick", (event) => {
  const cell = event.target.closest("td[data-action='edit-cell']");
  if (!cell) {
    return;
  }
  cancelPendingCellClick(cell);
  beginCellEdit(cell);
});

document.addEventListener("change", (event) => {
  const target = event.target;
  if (target.dataset.action === "select-page") {
    selectPage(target.checked);
  } else if (target.dataset.action === "select-row") {
    selectRow(target.dataset.rowId, target.checked);
  } else if (target === dom.filterColumn) {
    renderControls();
    state.page = 1;
    void loadQuery();
  } else if (target === dom.filterOperator) {
    renderControls();
    state.page = 1;
    void loadQuery();
  } else if (target === dom.filterValue) {
    state.page = 1;
    void loadQuery();
  } else if (target === dom.sortColumn || target === dom.sortDirection) {
    renderControls();
    state.page = 1;
    void loadQuery();
  } else if (target === dom.bulkClear) {
    dom.bulkValue.disabled = target.checked;
  }
});

let searchTimer = null;
dom.searchInput.addEventListener("input", () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    state.page = 1;
    void loadQuery();
  }, 220);
});

dom.rowForm.addEventListener("submit", (event) => {
  void saveNewRow(event);
});

function bulkInputChanged() {
  if (state.openDrawer === dom.bulkDrawer && state.bulkCommandId) {
    state.bulkCommandId = newCommandId();
  }
}

dom.bulkForm.addEventListener("input", bulkInputChanged);
dom.bulkForm.addEventListener("change", bulkInputChanged);

dom.bulkForm.addEventListener("submit", (event) => {
  void applyBulk(event);
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    const draft = state.editors.values().next().value;
    if (!event.target.matches("input") && draft) {
      event.preventDefault();
      cancelCellEdit(draft);
    } else if (!event.target.matches("input") && state.openDrawer) {
      closeDrawer();
    } else if (!event.target.matches("input")) {
      clearSelection();
    }
    return;
  }
  if (event.key === "Enter" && event.target.matches("td[data-action='edit-cell']")) {
    event.preventDefault();
    beginCellEdit(event.target);
  }
});

document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") {
    startPolling();
    void pollVersion();
  } else {
    stopPolling();
  }
});

const initialBridge = window.byop;
if (initialBridge && typeof initialBridge.onLocale === "function") {
  initialBridge.onLocale((locale) => {
    void applyLocale(locale).catch((error) => {
      showPageError(error);
    });
  });
}

async function resolveInitialLocale() {
  if (typeof initialBridge?.locale === "string") {
    return initialBridge.locale;
  }
  if (window.mygo && typeof window.mygo.call === "function") {
    try {
      const result = await window.mygo.call("BYOPWebView.Call", "host.getLocale", {});
      if (typeof result?.locale === "string") {
        return result.locale;
      }
    } catch {
      // Fall back to the renderer language when the host call is unavailable.
    }
  }
  return navigator.language.toLowerCase().startsWith("zh") ? "zh-Hans" : "en";
}

async function initialize() {
  const initialLocale = await resolveInitialLocale();
  try {
    await applyLocale(initialLocale);
  } catch (error) {
    console.error(error);
    showPageError(error);
    return;
  }
  await bootstrap();
}

void initialize();
