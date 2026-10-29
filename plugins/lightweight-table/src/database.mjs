import { randomUUID } from "node:crypto";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";

import { canonicalJSON, sha256 } from "./canonical.mjs";
import { TableError, asTableError } from "./errors.mjs";
import {
  centsFromDecimal,
  centsFromInteger,
  decimalFromCents,
  decimalFromCentsString,
  sumCents,
} from "./money.mjs";

export const FORMAT_VERSION = 2;
export const TABLE_SCHEMA_REVISION = 2;
export const DEFAULT_TABLE_ID = "tbl_default";

const COLUMN_DEFINITIONS = [
  {
    column_id: "institution",
    label: "机构",
    type: "text",
    required: false,
    config: { source_field: "creditor_name" },
  },
  {
    column_id: "category",
    label: "分类",
    type: "text",
    required: false,
    config: { source_field: "category" },
  },
  {
    column_id: "name",
    label: "名称",
    type: "text",
    required: true,
    config: { source_field: "product_name" },
  },
  {
    column_id: "principal",
    label: "本金",
    type: "number",
    required: false,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "contract_principal_cents",
    },
  },
  {
    column_id: "actual_received",
    label: "实际到账",
    type: "number",
    required: false,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "actual_disbursed_cents",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "remaining_due",
    label: "剩余应还",
    type: "number",
    required: false,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "principal_balance_cents",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "annualized_rate",
    label: "年化利率",
    type: "text",
    required: false,
    config: {
      source_field: "annualized_rate",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "repayment_status",
    label: "还款状态",
    type: "text",
    required: false,
    config: { source_field: "repayment_status" },
  },
  {
    column_id: "plan_status",
    label: "计划状态",
    type: "text",
    required: false,
    config: {
      source_field: "plan_status",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "next_due",
    label: "下次应还日期",
    type: "date",
    required: false,
    config: {
      source_field: "next_due.due_on",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "next_due_amount",
    label: "下次应还金额",
    type: "number",
    required: false,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "next_due.amount_cents",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "funding_source",
    label: "资金来源",
    type: "text",
    required: false,
    config: { source_field: "funding_source" },
  },
  {
    column_id: "product_type",
    label: "产品类型",
    type: "text",
    required: false,
    hidden: true,
    config: { source_field: "product_type", read_only: true, imported: true },
  },
  {
    column_id: "source_record_id",
    label: "源记录 ID",
    type: "text",
    required: false,
    hidden: true,
    config: { source_field: "id", read_only: true, imported: true },
  },
  {
    column_id: "product_group_id",
    label: "产品组 ID",
    type: "text",
    required: false,
    hidden: true,
    config: {
      source_field: "product_group_id",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "loan_sequence",
    label: "贷款序号",
    type: "number",
    required: false,
    hidden: true,
    config: {
      storage: "integer",
      source_field: "loan_sequence",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "loan_code",
    label: "贷款编号",
    type: "text",
    required: false,
    hidden: true,
    config: { source_field: "loan_code", read_only: true, imported: true },
  },
  {
    column_id: "contract_reference",
    label: "合同参考号",
    type: "text",
    required: false,
    hidden: true,
    config: {
      source_field: "contract_reference",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "opening_principal",
    label: "期初本金",
    type: "number",
    required: false,
    hidden: true,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "opening_principal_cents",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "planned_interest",
    label: "计划利息",
    type: "number",
    required: false,
    hidden: true,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "planned_interest_cents",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "remaining_interest",
    label: "剩余利息",
    type: "number",
    required: false,
    hidden: true,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "remaining_interest_cents",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "actual_paid",
    label: "实际已付",
    type: "number",
    required: false,
    hidden: true,
    config: {
      precision: 2,
      scale: 2,
      storage: "cents",
      currency: "CNY",
      source_field: "actual_paid_cents",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "borrowed_on",
    label: "借款日期",
    type: "date",
    required: false,
    hidden: true,
    config: {
      source_field: "borrowed_on",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "disbursed_on",
    label: "放款日期",
    type: "date",
    required: false,
    hidden: true,
    config: {
      source_field: "disbursed_on",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "original_rate_value",
    label: "原始利率",
    type: "number",
    required: false,
    hidden: true,
    config: {
      storage: "decimal",
      source_field: "original_rate_value",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "rate_period",
    label: "利率周期",
    type: "text",
    required: false,
    hidden: true,
    config: { source_field: "rate_period", read_only: true, imported: true },
  },
  {
    column_id: "manual_annualized_rate_percent",
    label: "手工年化利率",
    type: "number",
    required: false,
    hidden: true,
    config: {
      storage: "decimal",
      source_field: "manual_annualized_rate_percent",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "calculated_annualized_rate_percent",
    label: "计算年化利率",
    type: "number",
    required: false,
    hidden: true,
    config: {
      storage: "decimal",
      source_field: "calculated_annualized_rate_percent",
      read_only: true,
      imported: true,
      derived: true,
    },
  },
  {
    column_id: "interest_basis",
    label: "计息基础",
    type: "text",
    required: false,
    hidden: true,
    config: { source_field: "interest_basis", read_only: true, imported: true },
  },
  {
    column_id: "source_created_at",
    label: "源创建时间",
    type: "text",
    required: false,
    hidden: true,
    config: {
      source_field: "created_at",
      read_only: true,
      imported: true,
    },
  },
  {
    column_id: "source_updated_at",
    label: "源更新时间",
    type: "text",
    required: false,
    hidden: true,
    config: {
      source_field: "updated_at",
      read_only: true,
      imported: true,
    },
  },
];

const COLUMN_BY_ID = new Map(
  COLUMN_DEFINITIONS.map((column) => [column.column_id, column]),
);

const SCHEMA_SQL = `
CREATE TABLE IF NOT EXISTS workbook_meta (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  workbook_id TEXT NOT NULL UNIQUE,
  format_version INTEGER NOT NULL,
  name TEXT NOT NULL,
  timezone TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tables (
  table_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  schema_revision INTEGER NOT NULL,
  data_revision INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS columns (
  column_id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES tables(table_id),
  label TEXT NOT NULL,
  type TEXT NOT NULL,
  required INTEGER NOT NULL,
  config TEXT NOT NULL,
  position INTEGER NOT NULL,
  hidden INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS rows (
  row_id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES tables(table_id),
  source_record_id TEXT,
  revision INTEGER NOT NULL,
  values_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS operations (
  operation_id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES tables(table_id),
  source_kind TEXT NOT NULL,
  source_session_id TEXT,
  source_message_ids TEXT NOT NULL,
  source_time TEXT,
  client_command_id TEXT,
  execution_scope TEXT NOT NULL,
  dedupe_kind TEXT NOT NULL,
  dedupe_key TEXT NOT NULL,
  intent_hash TEXT NOT NULL,
  command_hash TEXT NOT NULL,
  canonical_command TEXT NOT NULL,
  before_after TEXT NOT NULL,
  receipt TEXT NOT NULL,
  undo_of TEXT,
  created_at TEXT NOT NULL,
  UNIQUE (execution_scope, dedupe_key)
);

CREATE TABLE IF NOT EXISTS contexts (
  context_id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES tables(table_id),
  row_ids TEXT NOT NULL,
  captured_versions TEXT NOT NULL,
  schema_revision INTEGER NOT NULL,
  data_revision INTEGER NOT NULL,
  query_description TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS rows_table_active
  ON rows (table_id, deleted_at);
CREATE INDEX IF NOT EXISTS operations_table_created
  ON operations (table_id, created_at DESC);
CREATE INDEX IF NOT EXISTS operations_source_session
  ON operations (source_session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS contexts_table_created
  ON contexts (table_id, created_at DESC);

CREATE TABLE IF NOT EXISTS fuyao_repayment_plans (
  id TEXT PRIMARY KEY,
  debt_id TEXT NOT NULL REFERENCES rows(row_id) ON DELETE CASCADE,
  basis_explanation TEXT,
  confirmed_remaining_cents INTEGER,
  created_at TEXT NOT NULL,
  effective_on TEXT NOT NULL,
  excluded_old_fee_cents INTEGER NOT NULL,
  excluded_old_interest_cents INTEGER NOT NULL,
  excluded_principal_cents INTEGER NOT NULL,
  first_due_on TEXT NOT NULL,
  included_old_fee_cents INTEGER NOT NULL,
  included_old_interest_cents INTEGER NOT NULL,
  installments INTEGER NOT NULL,
  interest_basis TEXT NOT NULL,
  interest_principal_cents INTEGER NOT NULL,
  method TEXT NOT NULL,
  new_fee_cents INTEGER NOT NULL,
  original_rate_value REAL NOT NULL,
  plan_principal_cents INTEGER,
  rate_period TEXT NOT NULL,
  source_note TEXT,
  status TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  verified_balance_at TEXT,
  verified_balance_cents INTEGER
);

CREATE TABLE IF NOT EXISTS fuyao_planned_payments (
  id TEXT PRIMARY KEY,
  plan_id TEXT NOT NULL REFERENCES fuyao_repayment_plans(id) ON DELETE CASCADE,
  debt_id TEXT NOT NULL REFERENCES rows(row_id) ON DELETE CASCADE,
  actual_paid_on TEXT,
  adjusted_at TEXT,
  due_on TEXT NOT NULL,
  installment_no INTEGER NOT NULL,
  interest_cents INTEGER NOT NULL,
  new_fee_cents INTEGER NOT NULL,
  note TEXT,
  old_fee_cents INTEGER NOT NULL,
  old_interest_cents INTEGER NOT NULL,
  principal_cents INTEGER NOT NULL,
  status TEXT NOT NULL,
  total_cents INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS fuyao_payments (
  id TEXT PRIMARY KEY,
  debt_id TEXT NOT NULL REFERENCES rows(row_id) ON DELETE CASCADE,
  amount_cents INTEGER NOT NULL,
  auto_allocation_blocked INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  note TEXT,
  payment_on TEXT NOT NULL,
  principal_effect TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS fuyao_payment_allocations (
  id TEXT PRIMARY KEY,
  payment_id TEXT NOT NULL REFERENCES fuyao_payments(id) ON DELETE CASCADE,
  planned_payment_id TEXT NOT NULL
    REFERENCES fuyao_planned_payments(id) ON DELETE CASCADE,
  allocated_at TEXT NOT NULL,
  allocation_method TEXT NOT NULL,
  amount_cents INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS fuyao_debt_events (
  id TEXT PRIMARY KEY,
  debt_id TEXT NOT NULL REFERENCES rows(row_id) ON DELETE CASCADE,
  amount_cents INTEGER NOT NULL,
  content TEXT NOT NULL,
  created_at TEXT NOT NULL,
  event_type TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  title TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS fuyao_import_meta (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  source_instance TEXT NOT NULL,
  source_date TEXT NOT NULL,
  exported_at TEXT NOT NULL,
  manifest_format_version INTEGER NOT NULL,
  import_format_version TEXT NOT NULL,
  debt_count INTEGER NOT NULL,
  repayment_plan_count INTEGER NOT NULL,
  planned_payment_count INTEGER NOT NULL,
  payment_count INTEGER NOT NULL,
  payment_allocation_count INTEGER NOT NULL,
  debt_event_count INTEGER NOT NULL,
  counts_json TEXT NOT NULL,
  digests_json TEXT NOT NULL,
  snapshot_digest TEXT NOT NULL,
  imported_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS fuyao_repayment_plans_debt
  ON fuyao_repayment_plans (debt_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS fuyao_planned_payments_plan
  ON fuyao_planned_payments (plan_id, installment_no);
CREATE INDEX IF NOT EXISTS fuyao_planned_payments_debt
  ON fuyao_planned_payments (debt_id, due_on);
CREATE INDEX IF NOT EXISTS fuyao_payments_debt
  ON fuyao_payments (debt_id, payment_on DESC);
CREATE INDEX IF NOT EXISTS fuyao_payment_allocations_payment
  ON fuyao_payment_allocations (payment_id, allocated_at DESC);
CREATE INDEX IF NOT EXISTS fuyao_payment_allocations_planned
  ON fuyao_payment_allocations (planned_payment_id, allocated_at DESC);
CREATE INDEX IF NOT EXISTS fuyao_debt_events_debt
  ON fuyao_debt_events (debt_id, occurred_at DESC);
`;

function nowISO() {
  return new Date().toISOString();
}

function assertPlainObject(value, label, allowedKeys) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new TableError("invalid_arguments", `${label} must be an object.`);
  }
  const allowed = new Set(allowedKeys);
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) {
      throw new TableError(
        "invalid_arguments",
        `${label} contains unsupported field ${JSON.stringify(key)}.`,
      );
    }
  }
}

function optionalText(value, label) {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value !== "string") {
    throw new TableError("invalid_value", `${label} must be a string or null.`);
  }
  return value;
}

function requiredText(value, label) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new TableError("invalid_value", `${label} must be a non-empty string.`);
  }
  return value;
}

function validDate(value, label) {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    throw new TableError("invalid_value", `${label} must use YYYY-MM-DD.`);
  }
  const parsed = new Date(`${value}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== value) {
    throw new TableError("invalid_value", `${label} is not a valid calendar date.`);
  }
  return value;
}

function validateTimezone(value) {
  const timezone = value ?? "Asia/Singapore";
  if (typeof timezone !== "string" || timezone.trim() === "") {
    throw new TableError("invalid_seed", "timezone must be a non-empty string.");
  }
  try {
    new Intl.DateTimeFormat("en", { timeZone: timezone }).format(new Date());
  } catch {
    throw new TableError("invalid_seed", `Unknown timezone ${JSON.stringify(timezone)}.`);
  }
  return timezone;
}

function normalizeSeed(input) {
  assertPlainObject(input, "seed", ["workbook_name", "timezone", "rows"]);
  const workbookName = requiredText(input.workbook_name, "workbook_name");
  const timezone = validateTimezone(input.timezone);
  if (!Array.isArray(input.rows)) {
    throw new TableError("invalid_seed", "rows must be an array.");
  }
  const editableColumns = COLUMN_DEFINITIONS.filter(
    (column) => column.config.read_only !== true,
  );
  const rows = input.rows.map((row, index) => {
    assertPlainObject(
      row,
      `rows[${index}]`,
      editableColumns.map((column) => column.column_id),
    );
    return Object.fromEntries(
      editableColumns.map((column) => [
        column.column_id,
        normalizeEditableValue(column, row[column.column_id], `rows[${index}]`),
      ]),
    );
  });
  return { workbookName, timezone, rows };
}

function normalizeByop(value) {
  if (value === null || value === undefined) {
    return null;
  }
  assertPlainObject(value, "_byop", [
    "version",
    "source_kind",
    "session_id",
    "message_ids",
    "message_time",
    "reason",
    "view_instance_id",
    "client_command_id",
  ]);
  if (value.version !== 1) {
    throw new TableError("source_unavailable", "Unsupported _byop source version.");
  }
  if (value.source_kind === "unavailable") {
    return {
      available: false,
      reason:
        typeof value.reason === "string" ? value.reason : "source_unavailable",
    };
  }
  if (value.source_kind === "page") {
    return {
      available: true,
      source_kind: "page",
      view_instance_id: requiredText(
        value.view_instance_id,
        "_byop.view_instance_id",
      ),
      client_command_id: requiredText(
        value.client_command_id,
        "_byop.client_command_id",
      ),
      session_id: null,
      message_ids: [],
      message_time: null,
    };
  }
  if (value.source_kind !== "agent") {
    throw new TableError("source_unavailable", "Unknown _byop source kind.");
  }
  const sessionID = requiredText(value.session_id, "_byop.session_id");
  if (
    !Array.isArray(value.message_ids) ||
    value.message_ids.length === 0 ||
    value.message_ids.some((id) => typeof id !== "string" || id === "")
  ) {
    throw new TableError(
      "source_unavailable",
      "_byop.message_ids must contain at least one Pi entry ID.",
    );
  }
  return {
    available: true,
    source_kind: "agent",
    session_id: sessionID,
    message_ids: [...value.message_ids],
    message_time:
      typeof value.message_time === "string" ? value.message_time : null,
  };
}

function sourceForWrite(source) {
  if (!source?.available) {
    throw new TableError(
      "source_unavailable",
      "A current delivered Pi user message is required for table writes.",
    );
  }
  return source;
}

function executionScope(source) {
  if (source.source_kind === "page") {
    return `page|${source.view_instance_id}|${source.client_command_id}`;
  }
  return `agent|${source.session_id}|${source.message_ids.join(",")}`;
}

function localDateForSource(source, timezone) {
  if (!source?.available || !source.message_time) {
    return null;
  }
  const date = new Date(source.message_time);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${values.year}-${values.month}-${values.day}`;
}

// localToday is today in the workbook timezone, the same calendar the table
// uses for its date columns, so a preset such as "already due" compares like
// the rows it selects.
function localToday(timezone) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(new Date());
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${values.year}-${values.month}-${values.day}`;
}

function shiftISODate(dateText, days) {
  const date = new Date(`${dateText}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

// DUE_WINDOW_DAYS is how far ahead the "due soon" preset looks.
const DUE_WINDOW_DAYS = 30;

// DASHBOARD_TOTAL_COLUMNS are the amount columns the summary totals. They are
// the amounts a reader checks first, and each stays on its own column so the
// totals never add up to a made-up number.
const DASHBOARD_TOTAL_COLUMNS = Object.freeze([
  "principal",
  "remaining_due",
  "actual_paid",
]);

const DASHBOARD_STATUS_ORDER = Object.freeze([
  "overdue",
  "repaying",
  "not_started",
  "settled",
]);

function dashboardTotals(rows) {
  return Object.fromEntries(
    DASHBOARD_TOTAL_COLUMNS.map((columnID) => {
      const column = COLUMN_BY_ID.get(columnID);
      return [
        columnID,
        {
          column_id: columnID,
          value: decimalFromCentsString(
            sumCents(rows.map((row) => row.values[columnID])),
            column.config.scale ?? 2,
          ),
          currency: column.config.currency ?? null,
        },
      ];
    }),
  );
}

function dashboardFocusFilter(filters, columnID) {
  const matches = filters.filter(
    (filter) => filter.column_id === columnID && filter.op === "eq",
  );
  return matches.length === 1 ? matches[0] : null;
}

function dashboardGroupEntries(rows, columnID, { focus, order = [] } = {}) {
  const groups = new Map();
  for (const row of rows) {
    const value = row.values[columnID] ?? null;
    const key = value === null ? "\u0000null" : String(value);
    let group = groups.get(key);
    if (!group) {
      group = { value, row_count: 0, amounts: [] };
      groups.set(key, group);
    }
    group.row_count += 1;
    group.amounts.push(row.values.remaining_due);
  }

  const orderIndex = new Map(order.map((value, index) => [value, index]));
  return [...groups.values()]
    .map((group) => {
      const remainingCents = sumCents(group.amounts);
      return {
        value: group.value,
        row_count: group.row_count,
        remaining_due: decimalFromCentsString(
          remainingCents,
          COLUMN_BY_ID.get("remaining_due").config.scale ?? 2,
        ),
        remainingCents,
      };
    })
    .sort((left, right) => {
      const leftOrder = orderIndex.get(left.value);
      const rightOrder = orderIndex.get(right.value);
      if (leftOrder !== undefined || rightOrder !== undefined) {
        return (
          (leftOrder ?? Number.MAX_SAFE_INTEGER) -
          (rightOrder ?? Number.MAX_SAFE_INTEGER)
        );
      }
      const amountDifference = BigInt(right.remainingCents) - BigInt(left.remainingCents);
      if (amountDifference !== 0n) {
        return amountDifference > 0n ? 1 : -1;
      }
      return String(left.value ?? "").localeCompare(String(right.value ?? ""));
    })
    .map(({ remainingCents: _remainingCents, ...entry }) => ({
      ...entry,
      ...(Object.hasOwn(entry, "value") && entry.value === focus
        ? { focus: true }
        : {}),
    }));
}

function daysBetweenISODates(start, end) {
  return Math.round(
    (Date.parse(`${end}T00:00:00Z`) - Date.parse(`${start}T00:00:00Z`)) /
      86400000,
  );
}

// dashboardPresets are the saved queries the page shows as buttons. Every
// preset is one filter on a column the query bar can display, so a pressed
// button always matches the filter controls, and each carries the amount
// column that makes its count worth reading as money.
function dashboardPresets(today) {
  const dueWindowEnd = shiftISODate(today, DUE_WINDOW_DAYS);
  const presets = [
    { preset_id: "all", filters: [], amount_column_id: "principal" },
    {
      preset_id: "overdue",
      filters: [{ column_id: "repayment_status", op: "eq", value: "overdue" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "repaying",
      filters: [{ column_id: "repayment_status", op: "eq", value: "repaying" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "settled",
      filters: [{ column_id: "repayment_status", op: "eq", value: "settled" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "no_plan",
      filters: [{ column_id: "plan_status", op: "eq", value: "not_created" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "bank",
      filters: [{ column_id: "funding_source", op: "eq", value: "bank" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "online_financial_platform",
      filters: [
        { column_id: "funding_source", op: "eq", value: "online_financial_platform" },
      ],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "financial",
      filters: [{ column_id: "category", op: "eq", value: "financial" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "high_interest",
      filters: [{ column_id: "category", op: "eq", value: "high_interest" }],
      amount_column_id: "remaining_due",
    },
    {
      preset_id: "friend",
      filters: [{ column_id: "category", op: "eq", value: "friend" }],
      amount_column_id: "remaining_due",
    },
  ];
  if (today !== null) {
    // The window is one bound so the preset stays a single filter: the filter
    // bar shows the same date the button counts by.
    const windowEnd = shiftISODate(today, DUE_WINDOW_DAYS + 1);
    if (windowEnd !== null) {
      presets.splice(4, 0, {
        preset_id: "due_30",
        filters: [{ column_id: "next_due", op: "lt", value: windowEnd }],
        amount_column_id: "next_due_amount",
      });
    }
  }
  return presets;
}

function parseJSON(value) {
  return value === null || value === undefined ? null : JSON.parse(value);
}

function rowValuesForOutput(values) {
  return Object.fromEntries(
    COLUMN_DEFINITIONS.map((column) => {
      const value = values[column.column_id] ?? null;
      if (value === null || column.type !== "number") {
        return [column.column_id, value];
      }
      if ((column.config.storage ?? "cents") === "cents") {
        return [
          column.column_id,
          decimalFromCents(value, column.config.scale ?? 2),
        ];
      }
      if (column.config.storage === "integer") {
        return [column.column_id, String(value)];
      }
      return [column.column_id, String(value)];
    }),
  );
}

function normalizeEditableValue(column, rawValue, label) {
  if (column.config.read_only === true) {
    throw new TableError(
      "read_only_column",
      `${label}.${column.column_id} is imported or derived and cannot be changed.`,
      { column_id: column.column_id },
    );
  }
  const value = rawValue === undefined ? null : rawValue;
  switch (column.type) {
    case "number":
      if ((column.config.storage ?? "cents") === "cents") {
        return centsFromDecimal(
          value,
          `${label}.${column.column_id}`,
          column.config.scale ?? 2,
        );
      }
      if (value === null) {
        return null;
      }
      if (
        typeof value !== "string" ||
        !/^-?(?:0|[1-9]\d*)(?:\.\d+)?$/.test(value)
      ) {
        throw new TableError(
          "invalid_value",
          `${label}.${column.column_id} must be a decimal string or null.`,
        );
      }
      return value;
    case "date":
      return validDate(value, `${label}.${column.column_id}`);
    case "text":
      return optionalText(value, `${label}.${column.column_id}`);
    default:
      throw new TableError("internal_error", `Unknown column type ${column.type}.`);
  }
}

function normalizeChangeValues(value, label) {
  assertPlainObject(
    value,
    label,
    COLUMN_DEFINITIONS.map((column) => column.column_id),
  );
  const normalized = {};
  for (const [columnID, rawValue] of Object.entries(value)) {
    const column = COLUMN_BY_ID.get(columnID);
    if (!column) {
      throw new TableError(
        "invalid_arguments",
        `${label} contains unknown column ${JSON.stringify(columnID)}.`,
      );
    }
    normalized[columnID] = normalizeEditableValue(
      column,
      rawValue,
      label,
    );
    if (column.required && normalized[columnID] === null) {
      throw new TableError("invalid_value", `${label}.${columnID} is required.`);
    }
  }
  return normalized;
}

function normalizeTargets(value) {
  if (!Array.isArray(value) || value.length === 0) {
    throw new TableError("invalid_arguments", "targets must be a non-empty array.");
  }
  const seen = new Set();
  const targets = value.map((target, index) => {
    assertPlainObject(target, `targets[${index}]`, ["row_id", "expected_revision"]);
    const rowID = requiredText(target.row_id, `targets[${index}].row_id`);
    if (seen.has(rowID)) {
      throw new TableError("invalid_arguments", `Duplicate target row_id ${rowID}.`);
    }
    seen.add(rowID);
    if (!Number.isSafeInteger(target.expected_revision) || target.expected_revision < 1) {
      throw new TableError(
        "invalid_arguments",
        `targets[${index}].expected_revision must be a positive integer.`,
      );
    }
    return { row_id: rowID, expected_revision: target.expected_revision };
  });
  targets.sort((left, right) => left.row_id.localeCompare(right.row_id));
  return targets;
}

function normalizeInsertRows(value) {
  if (!Array.isArray(value) || value.length === 0) {
    throw new TableError("invalid_arguments", "rows must be a non-empty array.");
  }
  return value.map((row, index) => {
    const normalized = normalizeChangeValues(row, `rows[${index}]`);
    if (!normalized.name) {
      throw new TableError("invalid_value", `rows[${index}].name is required.`);
    }
    return Object.fromEntries(
      COLUMN_DEFINITIONS.map((column) => [
        column.column_id,
        normalized[column.column_id] ?? null,
      ]),
    );
  });
}

function normalizeFilters(value) {
  if (value === undefined) {
    return [];
  }
  if (!Array.isArray(value)) {
    throw new TableError("invalid_arguments", "filters must be an array.");
  }
  return value.map((filter, index) => {
    assertPlainObject(filter, `filters[${index}]`, ["column_id", "op", "value"]);
    const columnID = requiredText(filter.column_id, `filters[${index}].column_id`);
    if (!COLUMN_BY_ID.has(columnID)) {
      throw new TableError("invalid_arguments", `Unknown filter column ${columnID}.`);
    }
    if (
      ![
        "eq",
        "neq",
        "contains",
        "not_contains",
        "is_null",
        "not_null",
        "gt",
        "gte",
        "lt",
        "lte",
        "between",
      ].includes(filter.op)
    ) {
      throw new TableError(
        "invalid_arguments",
        `filters[${index}].op is not supported.`,
      );
    }
    const column = COLUMN_BY_ID.get(columnID);
    if (
      ["gt", "gte", "lt", "lte", "between"].includes(filter.op) &&
      column.type === "text"
    ) {
      throw new TableError(
        "invalid_arguments",
        `filters[${index}].op requires a number or date column.`,
      );
    }
    if (
      ["contains", "not_contains"].includes(filter.op) &&
      column.type !== "text"
    ) {
      throw new TableError(
        "invalid_arguments",
        `filters[${index}].op requires a text column.`,
      );
    }
    if (
      filter.op === "between" &&
      (!Array.isArray(filter.value) || filter.value.length !== 2)
    ) {
      throw new TableError(
        "invalid_arguments",
        `filters[${index}].value must contain two bounds.`,
      );
    }
    return {
      column_id: columnID,
      op: filter.op,
      value: filter.value,
    };
  });
}

function normalizeSort(value) {
  if (value === undefined) {
    return [];
  }
  if (!Array.isArray(value) || value.length > 8) {
    throw new TableError("invalid_arguments", "sort must be an array of at most 8 columns.");
  }
  const seen = new Set();
  return value.map((entry, index) => {
    assertPlainObject(entry, `sort[${index}]`, ["column_id", "direction"]);
    const columnID = requiredText(entry.column_id, `sort[${index}].column_id`);
    if (!COLUMN_BY_ID.has(columnID)) {
      throw new TableError("invalid_arguments", `Unknown sort column ${columnID}.`);
    }
    if (seen.has(columnID)) {
      throw new TableError("invalid_arguments", `Duplicate sort column ${columnID}.`);
    }
    seen.add(columnID);
    const direction = entry.direction ?? "asc";
    if (!["asc", "desc"].includes(direction)) {
      throw new TableError(
        "invalid_arguments",
        `sort[${index}].direction must be asc or desc.`,
      );
    }
    return { column_id: columnID, direction };
  });
}

function normalizeSearch(value) {
  if (value === undefined || value === null) {
    return null;
  }
  if (typeof value !== "string") {
    throw new TableError("invalid_arguments", "search must be a string.");
  }
  const search = value.trim();
  return search === "" ? null : search;
}

function typedFilterValue(column, value, label) {
  if (column.type === "number") {
    if ((column.config.storage ?? "cents") === "cents") {
      return centsFromDecimal(value, label, column.config.scale ?? 2);
    }
    if (
      (typeof value !== "string" && typeof value !== "number") ||
      String(value).trim() === "" ||
      !Number.isFinite(Number(value))
    ) {
      throw new TableError("invalid_value", `${label} must be numeric.`);
    }
    return Number(value);
  }
  if (column.type === "date") {
    return validDate(value, label);
  }
  return value ?? null;
}

function matchesFilter(row, filter) {
  const value = row.values[filter.column_id];
  if (filter.op === "is_null") {
    return value === null || value === undefined;
  }
  if (filter.op === "not_null") {
    return value !== null && value !== undefined;
  }
  if (filter.op === "contains") {
    return (
      typeof value === "string" &&
      value.toLocaleLowerCase("en-US").includes(
        String(filter.value ?? "").toLocaleLowerCase("en-US"),
      )
    );
  }
  if (filter.op === "not_contains") {
    return (
      value !== null &&
      value !== undefined &&
      !String(value)
        .toLocaleLowerCase("en-US")
        .includes(String(filter.value ?? "").toLocaleLowerCase("en-US"))
    );
  }
  const column = COLUMN_BY_ID.get(filter.column_id);
  if (filter.op === "between") {
    if (value === null || value === undefined) {
      return false;
    }
    const lower = typedFilterValue(
      column,
      filter.value[0],
      `filters.${filter.column_id}.lower`,
    );
    const upper = typedFilterValue(
      column,
      filter.value[1],
      `filters.${filter.column_id}.upper`,
    );
    return value >= lower && value <= upper;
  }
  const expected = typedFilterValue(
    column,
    filter.value,
    `filters.${filter.column_id}`,
  );
  if (filter.op === "neq") {
    return value !== null && value !== undefined && value !== expected;
  }
  if (filter.op === "gt") {
    return value !== null && value !== undefined && value > expected;
  }
  if (filter.op === "gte") {
    return value !== null && value !== undefined && value >= expected;
  }
  if (filter.op === "lt") {
    return value !== null && value !== undefined && value < expected;
  }
  if (filter.op === "lte") {
    return value !== null && value !== undefined && value <= expected;
  }
  return value === expected;
}

function matchesSearch(row, search) {
  if (!search) {
    return true;
  }
  const needle = search.toLocaleLowerCase("en-US");
  return COLUMN_DEFINITIONS.some((column) => {
    if (column.type !== "text") {
      return false;
    }
    const value = row.values[column.column_id];
    return (
      typeof value === "string" &&
      value.toLocaleLowerCase("en-US").includes(needle)
    );
  });
}

function compareRows(left, right, sort) {
  for (const item of sort) {
    const leftValue = left.values[item.column_id];
    const rightValue = right.values[item.column_id];
    const leftNull = leftValue === null || leftValue === undefined;
    const rightNull = rightValue === null || rightValue === undefined;
    if (leftNull || rightNull) {
      if (leftNull && rightNull) {
        continue;
      }
      return leftNull ? 1 : -1;
    }
    let comparison = 0;
    if (leftValue < rightValue) {
      comparison = -1;
    } else if (leftValue > rightValue) {
      comparison = 1;
    }
    if (comparison !== 0) {
      return item.direction === "desc" ? -comparison : comparison;
    }
  }
  const created = left.created_at.localeCompare(right.created_at);
  return created !== 0 ? created : left.row_id.localeCompare(right.row_id);
}

function tableRecord(db) {
  return db
    .prepare(
      `SELECT table_id, name, schema_revision, data_revision
         FROM tables
        WHERE table_id = ?`,
    )
    .get(DEFAULT_TABLE_ID);
}

function workbookRecord(db) {
  return db
    .prepare(
      `SELECT workbook_id, format_version, name, timezone, created_at
         FROM workbook_meta
        WHERE singleton = 1`,
    )
    .get();
}

function importMetaRecord(db) {
  const meta = db
    .prepare(
      `SELECT source_instance, source_date, exported_at,
              manifest_format_version, import_format_version,
              debt_count, repayment_plan_count, planned_payment_count,
              payment_count, payment_allocation_count, debt_event_count,
              snapshot_digest, imported_at
         FROM fuyao_import_meta
        WHERE singleton = 1`,
    )
    .get();
  if (!meta) {
    return null;
  }
  return {
    source_instance: meta.source_instance,
    source_date: meta.source_date,
    exported_at: meta.exported_at,
    manifest_format_version: meta.manifest_format_version,
    import_format_version: meta.import_format_version,
    counts: {
      debts: meta.debt_count,
      repayment_plans: meta.repayment_plan_count,
      planned_payments: meta.planned_payment_count,
      payments: meta.payment_count,
      payment_allocations: meta.payment_allocation_count,
      debt_events: meta.debt_event_count,
    },
    snapshot_digest: meta.snapshot_digest,
    imported_at: meta.imported_at,
    live_connected: false,
  };
}

function rowsForTargets(db, targets) {
  const statement = db.prepare(
    `SELECT row_id, revision, values_json, created_at, updated_at
       FROM rows
      WHERE table_id = ? AND row_id = ? AND deleted_at IS NULL`,
  );
  return targets.map((target) => {
    const row = statement.get(DEFAULT_TABLE_ID, target.row_id);
    if (!row) {
      throw new TableError(
        "row_not_found",
        `Row ${target.row_id} does not exist in the active table.`,
      );
    }
    if (row.revision !== target.expected_revision) {
      throw new TableError(
        "version_conflict",
        `Row ${target.row_id} changed before this command.`,
        {
          row_id: target.row_id,
          expected_revision: target.expected_revision,
          current_revision: row.revision,
        },
      );
    }
    return {
      ...row,
      values: parseJSON(row.values_json),
    };
  });
}

function contextRecord(db, contextID) {
  const record = db
    .prepare(
      `SELECT context_id, table_id, row_ids, captured_versions,
              schema_revision, data_revision, query_description, created_at
         FROM contexts
        WHERE context_id = ?`,
    )
    .get(contextID);
  if (!record) {
    throw new TableError(
      "context_not_found",
      `Context ${contextID} does not exist.`,
    );
  }
  if (record.table_id !== DEFAULT_TABLE_ID) {
    throw new TableError(
      "context_not_found",
      `Context ${contextID} belongs to another table.`,
    );
  }
  const rowIDs = parseJSON(record.row_ids);
  const capturedVersions = parseJSON(record.captured_versions);
  return {
    ...record,
    row_ids: rowIDs,
    captured_versions: capturedVersions,
  };
}

function targetsFromContext(context) {
  return normalizeTargets(
    context.row_ids.map((rowID) => ({
      row_id: rowID,
      expected_revision: context.captured_versions[rowID],
    })),
  );
}

function undoRecordFor(db, operationID) {
  const record = db
    .prepare(
      `SELECT operation_id, receipt, created_at
         FROM operations
        WHERE undo_of = ?`,
    )
    .get(operationID);
  if (!record) {
    return null;
  }
  return {
    operation_id: record.operation_id,
    receipt: parseJSON(record.receipt),
    created_at: record.created_at,
  };
}

function receiptForReplay(db, receipt) {
  return {
    ...receipt,
    replayed: true,
    undo: undoRecordFor(db, receipt.operation_id),
  };
}

export class TableStore {
  constructor(databasePath) {
    if (typeof databasePath !== "string" || databasePath.trim() === "") {
      throw new TableError("invalid_database_path", "A database path is required.");
    }
    this.databasePath = resolve(databasePath);
    mkdirSync(dirname(this.databasePath), { recursive: true });
    this.db = new DatabaseSync(this.databasePath);
    this.configureConnection();
    this.ensureSchema();
  }

  configureConnection() {
    this.db.exec("PRAGMA busy_timeout = 3000");
    this.db.exec("PRAGMA synchronous = FULL");
    this.db.exec("PRAGMA foreign_keys = ON");
    this.db.exec("PRAGMA journal_mode = WAL");
    const journal = this.db.prepare("PRAGMA journal_mode").get().journal_mode;
    const timeout = this.db.prepare("PRAGMA busy_timeout").get().timeout;
    const synchronous = this.db.prepare("PRAGMA synchronous").get().synchronous;
    const foreignKeys = this.db.prepare("PRAGMA foreign_keys").get().foreign_keys;
    if (
      String(journal).toLowerCase() !== "wal" ||
      Number(timeout) !== 3000 ||
      Number(synchronous) !== 2 ||
      Number(foreignKeys) !== 1
    ) {
      throw new TableError(
        "database_configuration",
        "SQLite did not accept WAL, FULL, foreign_keys, and busy_timeout settings.",
      );
    }
  }

  ensureSchema() {
    this.db.exec(SCHEMA_SQL);
    const rowColumns = this.db
      .prepare("PRAGMA table_info(rows)")
      .all()
      .map((column) => column.name);
    if (!rowColumns.includes("source_record_id")) {
      this.db.exec("ALTER TABLE rows ADD COLUMN source_record_id TEXT");
    }
    const operationColumns = this.db
      .prepare("PRAGMA table_info(operations)")
      .all()
      .map((column) => column.name);
    if (!operationColumns.includes("undo_of")) {
      this.db.exec("ALTER TABLE operations ADD COLUMN undo_of TEXT");
    }
    this.db.exec(
      `CREATE UNIQUE INDEX IF NOT EXISTS operations_undo_of
         ON operations (undo_of)
       WHERE undo_of IS NOT NULL`,
    );
    this.db.exec(
      `CREATE UNIQUE INDEX IF NOT EXISTS rows_source_record
         ON rows (table_id, source_record_id)
       WHERE source_record_id IS NOT NULL`,
    );
    if (!this.initialized()) {
      return;
    }
    const table = tableRecord(this.db);
    if (!table) {
      return;
    }
    if (table.schema_revision > TABLE_SCHEMA_REVISION) {
      throw new TableError(
        "unsupported_schema",
        `The table schema ${table.schema_revision} is newer than this plugin supports.`,
      );
    }
    this.immediate(() => {
      this.db
        .prepare(
          `UPDATE workbook_meta
              SET format_version = ?
            WHERE singleton = 1`,
        )
        .run(FORMAT_VERSION);
      this.db
        .prepare(
          `UPDATE tables
              SET schema_revision = ?
            WHERE table_id = ?`,
        )
        .run(TABLE_SCHEMA_REVISION, DEFAULT_TABLE_ID);
      this.db
        .prepare("DELETE FROM columns WHERE table_id = ?")
        .run(DEFAULT_TABLE_ID);
      const insertColumn = this.db.prepare(
        `INSERT INTO columns
           (column_id, table_id, label, type, required, config, position, hidden)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      );
      COLUMN_DEFINITIONS.forEach((column, position) => {
        insertColumn.run(
          column.column_id,
          DEFAULT_TABLE_ID,
          column.label,
          column.type,
          column.required ? 1 : 0,
          canonicalJSON(column.config),
          position,
          column.hidden ? 1 : 0,
        );
      });
      this.db.exec(
        `UPDATE rows
            SET source_record_id = json_extract(values_json, '$.source_record_id')
          WHERE source_record_id IS NULL
            AND json_type(values_json, '$.source_record_id') = 'text'`,
      );
    });
  }

  close() {
    this.db.close();
  }

  immediate(callback) {
    this.db.exec("BEGIN IMMEDIATE");
    try {
      const result = callback();
      this.db.exec("COMMIT");
      return result;
    } catch (error) {
      try {
        this.db.exec("ROLLBACK");
      } catch {
        // Preserve the original error.
      }
      throw error;
    }
  }

  read(callback) {
    this.db.exec("BEGIN");
    try {
      const result = callback();
      this.db.exec("COMMIT");
      return result;
    } catch (error) {
      try {
        this.db.exec("ROLLBACK");
      } catch {
        // Preserve the original error.
      }
      throw error;
    }
  }

  initialized() {
    const table = this.db
      .prepare(
        "SELECT 1 AS present FROM sqlite_master WHERE type = 'table' AND name = 'workbook_meta'",
      )
      .get();
    if (!table) {
      return false;
    }
    return Boolean(workbookRecord(this.db));
  }

  summary() {
    return this.read(() => this.summaryFromCurrentSnapshot());
  }

  summaryFromCurrentSnapshot() {
    const workbook = workbookRecord(this.db);
    if (!workbook) {
      throw new TableError("database_not_initialized", "The table database is empty.");
    }
    const table = tableRecord(this.db);
    const count = this.db
      .prepare(
        "SELECT COUNT(*) AS count FROM rows WHERE table_id = ? AND deleted_at IS NULL",
      )
      .get(DEFAULT_TABLE_ID);
    return {
      workbook_id: workbook.workbook_id,
      table_id: table.table_id,
      schema_revision: table.schema_revision,
      data_revision: table.data_revision,
      row_count: Number(count.count),
      data_source: importMetaRecord(this.db),
    };
  }

  // dashboard summarizes the table for the saved-query buttons: every preset
  // gets its row count and the total of the amount column that matters for it.
  // Rows are read once and matched with the same filter code the query uses,
  // so a button's number and the list it opens cannot disagree.
  dashboard(args = {}) {
    assertPlainObject(args, "dashboard", ["search", "filters"]);
    const search = normalizeSearch(args.search);
    const filters = normalizeFilters(args.filters);
    return this.read(() => {
      const workbook = workbookRecord(this.db);
      if (!workbook) {
        throw new TableError("database_not_initialized", "The table database is empty.");
      }
      const table = tableRecord(this.db);
      const rows = this.db
        .prepare(
          `SELECT row_id, values_json
             FROM rows
            WHERE table_id = ? AND deleted_at IS NULL`,
        )
        .all(DEFAULT_TABLE_ID)
        .map((row) => ({
          row_id: row.row_id,
          values: parseJSON(row.values_json),
        }));
      const today = localToday(workbook.timezone);
      const searchRows = rows.filter((row) => matchesSearch(row, search));
      const matchedRows = searchRows.filter((row) =>
        filters.every((filter) => matchesFilter(row, filter)),
      );
      const statusFocus = dashboardFocusFilter(filters, "repayment_status");
      const statusRows = statusFocus ? searchRows : matchedRows;
      const status = dashboardGroupEntries(statusRows, "repayment_status", {
        focus: statusFocus?.value,
        order: DASHBOARD_STATUS_ORDER,
      });
      const breakdowns = Object.fromEntries(
        ["category", "funding_source"].map((columnID) => {
          const focus = dashboardFocusFilter(filters, columnID);
          return [
            columnID,
            dashboardGroupEntries(
              focus ? searchRows : matchedRows,
              columnID,
              { focus: focus?.value },
            ),
          ];
        }),
      );
      const noPlanFocus =
        filters.length === 1 &&
        filters[0].column_id === "plan_status" &&
        filters[0].op === "eq" &&
        filters[0].value === "not_created";
      const noPlanRows = noPlanFocus ? searchRows : matchedRows;
      const attentionEnd = shiftISODate(today, DUE_WINDOW_DAYS + 1);
      const attentionRows = matchedRows
        .filter(
          (row) =>
            row.values.next_due !== null &&
            row.values.next_due !== undefined &&
            row.values.next_due < attentionEnd,
        );
      const attention = attentionRows
        .map((row) => ({
          row_id: row.row_id,
          institution: row.values.institution ?? null,
          name: row.values.name ?? null,
          next_due: row.values.next_due,
          next_due_amount:
            row.values.next_due_amount === null ||
            row.values.next_due_amount === undefined
              ? null
              : decimalFromCentsString(
                  sumCents([row.values.next_due_amount]),
                  COLUMN_BY_ID.get("next_due_amount").config.scale ?? 2,
                ),
          days: daysBetweenISODates(today, row.values.next_due),
        }))
        .sort(
          (left, right) =>
            left.next_due.localeCompare(right.next_due) ||
            left.row_id.localeCompare(right.row_id),
        );
      return {
        table_id: DEFAULT_TABLE_ID,
        schema_revision: table.schema_revision,
        data_revision: table.data_revision,
        row_count: rows.length,
        source_local_date: today,
        due_window_days: DUE_WINDOW_DAYS,
        scope: {
          search,
          filters,
          matched_count: matchedRows.length,
          total_count: rows.length,
          // The hero percentage needs the whole-table remaining amount even
          // when the active query is scoped to a different dimension.
          total_remaining_due: decimalFromCentsString(
            sumCents(rows.map((row) => row.values.remaining_due)),
            COLUMN_BY_ID.get("remaining_due").config.scale ?? 2,
          ),
        },
        totals: dashboardTotals(matchedRows),
        status,
        no_plan_count: noPlanRows.filter(
          (row) => row.values.plan_status === "not_created",
        ).length,
        breakdowns,
        attention,
        attention_total: decimalFromCentsString(
          sumCents(attentionRows.map((row) => row.values.next_due_amount)),
          COLUMN_BY_ID.get("next_due_amount").config.scale ?? 2,
        ),
        presets: dashboardPresets(today).map((preset) => {
          const presetFilters = normalizeFilters(preset.filters);
          const matched = rows.filter(
            (row) =>
              presetFilters.every((filter) => matchesFilter(row, filter)),
          );
          return {
            preset_id: preset.preset_id,
            filters: presetFilters,
            row_count: matched.length,
            amount: {
              column_id: preset.amount_column_id,
              value: decimalFromCentsString(
                sumCents(
                  matched.map((row) => row.values[preset.amount_column_id]),
                ),
                COLUMN_BY_ID.get(preset.amount_column_id).config.scale ?? 2,
              ),
              currency:
                COLUMN_BY_ID.get(preset.amount_column_id).config.currency ?? null,
            },
          };
        }),
      };
    });
  }

  seed(input, { ifEmpty = false } = {}) {
    const normalized = normalizeSeed(input);
    return this.immediate(() => {
      this.db.exec(SCHEMA_SQL);
      if (this.initialized()) {
        if (ifEmpty) {
          return { ...this.summaryFromCurrentSnapshot(), seeded: false };
        }
        throw new TableError(
          "database_not_empty",
          "The database already contains a workbook; use --if-empty or a new path.",
        );
      }
      const workbookID = randomUUID();
      const createdAt = nowISO();
      this.db
        .prepare(
          `INSERT INTO workbook_meta
             (singleton, workbook_id, format_version, name, timezone, created_at)
           VALUES (1, ?, ?, ?, ?, ?)`,
        )
        .run(
          workbookID,
          FORMAT_VERSION,
          normalized.workbookName,
          normalized.timezone,
          createdAt,
        );
      this.db
        .prepare(
          `INSERT INTO tables
             (table_id, name, schema_revision, data_revision)
           VALUES (?, ?, ?, ?)`,
        )
        .run(
          DEFAULT_TABLE_ID,
          "银行负债",
          TABLE_SCHEMA_REVISION,
          normalized.rows.length > 0 ? 1 : 0,
        );
      const insertColumn = this.db.prepare(
        `INSERT INTO columns
           (column_id, table_id, label, type, required, config, position, hidden)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      );
      COLUMN_DEFINITIONS.forEach((column, position) => {
        insertColumn.run(
          column.column_id,
          DEFAULT_TABLE_ID,
          column.label,
          column.type,
          column.required ? 1 : 0,
          canonicalJSON(column.config),
          position,
          column.hidden ? 1 : 0,
        );
      });
      const insertRow = this.db.prepare(
        `INSERT INTO rows
           (row_id, table_id, source_record_id, revision, values_json,
            created_at, updated_at, deleted_at)
         VALUES (?, ?, NULL, 1, ?, ?, ?, NULL)`,
      );
      const rowIDs = [];
      for (const values of normalized.rows) {
        const rowID = randomUUID();
        rowIDs.push(rowID);
        insertRow.run(
          rowID,
          DEFAULT_TABLE_ID,
          canonicalJSON(values),
          createdAt,
          createdAt,
        );
      }
      return {
        workbook_id: workbookID,
        table_id: DEFAULT_TABLE_ID,
        schema_revision: TABLE_SCHEMA_REVISION,
        data_revision: normalized.rows.length > 0 ? 1 : 0,
        row_count: normalized.rows.length,
        row_ids: rowIDs,
        seeded: true,
      };
    });
  }

  describe(rawSource) {
    const source = normalizeByop(rawSource);
    return this.read(() => {
      const workbook = workbookRecord(this.db);
      const table = tableRecord(this.db);
      if (!workbook || !table) {
        throw new TableError("database_not_initialized", "The table database is empty.");
      }
      const storedValues = this.db
        .prepare(
          `SELECT values_json
             FROM rows
            WHERE table_id = ? AND deleted_at IS NULL`,
        )
        .all(DEFAULT_TABLE_ID)
        .map((row) => parseJSON(row.values_json));
      const optionSets = Object.fromEntries(
        COLUMN_DEFINITIONS.map((column) => [
          column.column_id,
          new Set(
            storedValues
              .map((values) => values[column.column_id])
              .filter((value) => typeof value === "string" && value !== ""),
          ),
        ]),
      );
      const columns = this.db
        .prepare(
          `SELECT column_id, label, type, required, config, position, hidden
             FROM columns
            WHERE table_id = ?
            ORDER BY position`,
        )
        .all(DEFAULT_TABLE_ID)
        .map((column) => ({
          column_id: column.column_id,
          label: column.label,
          type: column.type,
          required: Boolean(column.required),
          config: parseJSON(column.config),
          hidden: Boolean(column.hidden),
          options: [...(optionSets[column.column_id] ?? [])].sort(),
        }));
      return {
        workbook: {
          workbook_id: workbook.workbook_id,
          format_version: workbook.format_version,
          name: workbook.name,
          timezone: workbook.timezone,
        },
        table: {
          table_id: table.table_id,
          name: table.name,
          schema_revision: table.schema_revision,
          data_revision: table.data_revision,
        },
        columns,
        data_source: importMetaRecord(this.db),
        source_local_date: localDateForSource(source, workbook.timezone),
      };
    });
  }

  query(args, rawSource) {
    assertPlainObject(args, "table_query", [
      "table_id",
      "row_ids",
      "context_id",
      "filters",
      "search",
      "sort",
      "projection",
      "limit",
      "offset",
      "sum_column_id",
      "capture_targets",
    ]);
    if (args.table_id !== undefined && args.table_id !== DEFAULT_TABLE_ID) {
      throw new TableError("table_not_found", "Only the default table is available.");
    }
    const source = normalizeByop(rawSource);
    const limit = args.limit ?? 50;
    const offset = args.offset ?? 0;
    if (!Number.isInteger(limit) || limit < 1 || limit > 100) {
      throw new TableError("invalid_arguments", "limit must be an integer from 1 to 100.");
    }
    if (!Number.isInteger(offset) || offset < 0) {
      throw new TableError("invalid_arguments", "offset must be a non-negative integer.");
    }
    let projection = COLUMN_DEFINITIONS.map((column) => column.column_id);
    if (args.projection !== undefined) {
      if (!Array.isArray(args.projection) || args.projection.length === 0) {
        throw new TableError("invalid_arguments", "projection must be a non-empty array.");
      }
      projection = args.projection.map((columnID) => {
        if (!COLUMN_BY_ID.has(columnID)) {
          throw new TableError("invalid_arguments", `Unknown projection column ${columnID}.`);
        }
        return columnID;
      });
    }
    let rowIDs = null;
    if (args.row_ids !== undefined) {
      if (!Array.isArray(args.row_ids) || args.row_ids.length === 0) {
        throw new TableError("invalid_arguments", "row_ids must be a non-empty array.");
      }
      rowIDs = new Set(
        args.row_ids.map((rowID) => requiredText(rowID, "row_ids[]")),
      );
    }
    if (args.context_id !== undefined && rowIDs) {
      throw new TableError(
        "invalid_arguments",
        "context_id and row_ids cannot be used together.",
      );
    }
    const contextID =
      args.context_id === undefined
        ? null
        : requiredText(args.context_id, "context_id");
    const filters = normalizeFilters(args.filters);
    const search = normalizeSearch(args.search);
    const sort = normalizeSort(args.sort);
    if (
      args.capture_targets !== undefined &&
      typeof args.capture_targets !== "boolean"
    ) {
      throw new TableError("invalid_arguments", "capture_targets must be a boolean.");
    }
    const captureTargets = args.capture_targets === true;
    if (
      args.sum_column_id !== undefined &&
      (!COLUMN_BY_ID.has(args.sum_column_id) ||
        COLUMN_BY_ID.get(args.sum_column_id).type !== "number")
    ) {
      throw new TableError(
        "invalid_arguments",
        "sum_column_id must identify a numeric column.",
      );
    }

    const execute = () => {
      const workbook = workbookRecord(this.db);
      const table = tableRecord(this.db);
      if (!workbook || !table) {
        throw new TableError("database_not_initialized", "The table database is empty.");
      }
      const storedRows = this.db
        .prepare(
          `SELECT row_id, revision, values_json, created_at, updated_at, deleted_at
             FROM rows
            WHERE table_id = ?`,
        )
        .all(DEFAULT_TABLE_ID)
        .map((row) => ({
          row_id: row.row_id,
          revision: row.revision,
          values: parseJSON(row.values_json),
          created_at: row.created_at,
          updated_at: row.updated_at,
          deleted_at: row.deleted_at,
        }));
      const allRows = storedRows.filter((row) => row.deleted_at === null);
      const rowByID = new Map(storedRows.map((row) => [row.row_id, row]));
      let context = null;
      if (contextID) {
        context = contextRecord(this.db, contextID);
        rowIDs = new Set(context.row_ids);
      }
      const matched = allRows.filter(
        (row) =>
          (!rowIDs || rowIDs.has(row.row_id)) &&
          matchesSearch(row, search) &&
          filters.every((filter) => matchesFilter(row, filter)),
      );
      if (sort.length > 0) {
        matched.sort((left, right) => compareRows(left, right, sort));
      } else {
        matched.sort((left, right) => compareRows(left, right, sort));
      }
      const page = matched.slice(offset, offset + limit);
      const result = {
        table_id: DEFAULT_TABLE_ID,
        schema_revision: table.schema_revision,
        data_revision: table.data_revision,
        matched_count: matched.length,
        offset,
        limit,
        search,
        filters,
        sort,
        rows: page.map((row) => ({
          row_id: row.row_id,
          revision: row.revision,
          values: Object.fromEntries(
            projection.map((columnID) => [
              columnID,
              rowValuesForOutput(row.values)[columnID],
            ]),
          ),
          context_changed: context
            ? context.captured_versions[row.row_id] !== row.revision
            : false,
        })),
        source_local_date: localDateForSource(source, workbook.timezone),
      };
      if (args.sum_column_id !== undefined) {
        result.sum = {
          column_id: args.sum_column_id,
          value: decimalFromCentsString(
            sumCents(matched.map((row) => row.values[args.sum_column_id])),
            2,
          ),
          currency:
            COLUMN_BY_ID.get(args.sum_column_id).config.currency ?? null,
        };
      }
      if (captureTargets) {
        const capturedAt = nowISO();
        const createdContextID = randomUUID();
        const capturedVersions = Object.fromEntries(
          matched.map((row) => [row.row_id, row.revision]),
        );
        const queryDescription = {
          search,
          filters,
          sort,
          row_ids: rowIDs ? [...rowIDs].sort() : null,
        };
        this.db
          .prepare(
            `INSERT INTO contexts
               (context_id, table_id, row_ids, captured_versions,
                schema_revision, data_revision, query_description, created_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            createdContextID,
            DEFAULT_TABLE_ID,
            canonicalJSON(matched.map((row) => row.row_id)),
            canonicalJSON(capturedVersions),
            table.schema_revision,
            table.data_revision,
            canonicalJSON(queryDescription),
            capturedAt,
          );
        result.context_id = createdContextID;
        result.context = {
          context_id: createdContextID,
          captured_count: matched.length,
          current_count: matched.length,
          changed_count: 0,
          schema_revision: table.schema_revision,
          data_revision: table.data_revision,
          created_at: capturedAt,
        };
      } else if (context) {
        let changedCount = 0;
        for (const rowID of context.row_ids) {
          const current = rowByID.get(rowID);
          if (
            !current ||
            current.deleted_at !== null ||
            current.revision !== context.captured_versions[rowID]
          ) {
            changedCount += 1;
          }
        }
        result.context_id = context.context_id;
        result.context = {
          context_id: context.context_id,
          captured_count: context.row_ids.length,
          current_count: matched.length,
          changed_count: changedCount,
          schema_revision: context.schema_revision,
          data_revision: context.data_revision,
          created_at: context.created_at,
        };
      }
      return result;
    };

    return captureTargets ? this.immediate(execute) : this.read(execute);
  }

  change(args, rawSource) {
    assertPlainObject(args, "table_change", [
      "action",
      "table_id",
      "rows",
      "targets",
      "context_id",
      "values",
      "expected_schema_revision",
      "dry_run",
    ]);
    if (args.table_id !== undefined && args.table_id !== DEFAULT_TABLE_ID) {
      throw new TableError("table_not_found", "Only the default table is available.");
    }
    if (!["insert", "update", "delete"].includes(args.action)) {
      throw new TableError("invalid_arguments", "action must be insert, update, or delete.");
    }
    if (
      !Number.isSafeInteger(args.expected_schema_revision) ||
      args.expected_schema_revision < 1
    ) {
      throw new TableError(
        "invalid_arguments",
        "expected_schema_revision must be a positive integer.",
      );
    }
    const dryRun = args.dry_run === true;
    const source = dryRun ? normalizeByop(rawSource) : sourceForWrite(normalizeByop(rawSource));
    let rows = null;
    let targets = null;
    let values = null;
    if (args.action === "insert") {
      if (
        args.targets !== undefined ||
        args.context_id !== undefined ||
        args.values !== undefined
      ) {
        throw new TableError(
          "invalid_arguments",
          "Insert accepts rows and does not accept targets, context_id, or values.",
        );
      }
      rows = normalizeInsertRows(args.rows);
    } else {
      if (args.rows !== undefined) {
        throw new TableError(
          "invalid_arguments",
          "Update and delete accept targets and do not accept rows.",
        );
      }
      const hasTargets = args.targets !== undefined;
      const hasContext = args.context_id !== undefined;
      if (hasTargets === hasContext) {
        throw new TableError(
          "invalid_arguments",
          "Update and delete require exactly one of targets or context_id.",
        );
      }
      if (hasContext) {
        const context = this.read(() =>
          contextRecord(
            this.db,
            requiredText(args.context_id, "context_id"),
          ),
        );
        targets = targetsFromContext(context);
      } else {
        targets = normalizeTargets(args.targets);
      }
      if (args.action === "update") {
        values = normalizeChangeValues(args.values, "values");
        if (Object.keys(values).length === 0) {
          throw new TableError("invalid_arguments", "Update values must not be empty.");
        }
      } else if (args.values !== undefined) {
        throw new TableError("invalid_arguments", "Delete does not accept values.");
      }
    }

    const intent = {
      action: args.action,
      table_id: DEFAULT_TABLE_ID,
      rows,
      targets: targets?.map((target) => target.row_id) ?? null,
      values,
    };
    const command = {
      action: args.action,
      table_id: DEFAULT_TABLE_ID,
      expected_schema_revision: args.expected_schema_revision,
      rows,
      targets,
      values,
    };
    const intentHash = sha256(intent);
    const commandHash = sha256(command);
    const dedupeKind =
      source?.source_kind === "page"
        ? "command"
        : args.action === "insert"
          ? "intent"
          : "command";
    const dedupeKey =
      source?.source_kind === "page"
        ? "page-command"
        : args.action === "insert"
          ? intentHash
          : commandHash;

    if (dryRun) {
      const preview = this.previewChange(args.action, args.expected_schema_revision, rows, targets, values);
      return {
        dry_run: true,
        operation_id: null,
        ...preview,
      };
    }

    try {
      return this.immediate(() => {
        const table = tableRecord(this.db);
        const lookup = this.db
          .prepare(
            `SELECT receipt, command_hash
               FROM operations
              WHERE execution_scope = ? AND dedupe_key = ?`,
          )
          .get(executionScope(source), dedupeKey);
        if (lookup) {
          if (
            source.source_kind === "page" &&
            lookup.command_hash !== commandHash
          ) {
            throw new TableError(
              "command_id_conflict",
              "The page command ID was already used for different values.",
            );
          }
          return receiptForReplay(this.db, parseJSON(lookup.receipt));
        }
        if (!table) {
          throw new TableError("database_not_initialized", "The table database is empty.");
        }
        if (table.schema_revision !== args.expected_schema_revision) {
          throw new TableError(
            "version_conflict",
            "The table structure changed before this command.",
            {
              expected_revision: args.expected_schema_revision,
              current_revision: table.schema_revision,
            },
          );
        }
        const timestamp = nowISO();
        const operationID = randomUUID();
        let affectedCount = 0;
        let rowIDs = [];
        let beforeAfter = [];
        if (args.action === "insert") {
          const insert = this.db.prepare(
            `INSERT INTO rows
               (row_id, table_id, revision, values_json, created_at, updated_at, deleted_at)
             VALUES (?, ?, 1, ?, ?, ?, NULL)`,
          );
          for (const row of rows) {
            const rowID = randomUUID();
            rowIDs.push(rowID);
            insert.run(
              rowID,
              DEFAULT_TABLE_ID,
              canonicalJSON(row),
              timestamp,
              timestamp,
            );
          }
          affectedCount = rows.length;
          beforeAfter = rowIDs.map((rowID) => ({
            row_id: rowID,
            before: null,
            after_revision: 1,
          }));
        } else {
          const currentRows = rowsForTargets(this.db, targets);
          rowIDs = currentRows.map((row) => row.row_id);
          if (args.action === "update") {
            const update = this.db.prepare(
              `UPDATE rows
                  SET revision = ?, values_json = ?, updated_at = ?
                WHERE table_id = ? AND row_id = ?`,
            );
            for (const row of currentRows) {
              const nextValues = { ...row.values, ...values };
              if (canonicalJSON(nextValues) === canonicalJSON(row.values)) {
                continue;
              }
              const nextRevision = row.revision + 1;
              update.run(
                nextRevision,
                canonicalJSON(nextValues),
                timestamp,
                DEFAULT_TABLE_ID,
                row.row_id,
              );
              affectedCount += 1;
              beforeAfter.push({
                row_id: row.row_id,
                before: row.values,
                before_revision: row.revision,
                after: nextValues,
                after_revision: nextRevision,
              });
            }
          } else {
            const remove = this.db.prepare(
              `UPDATE rows
                  SET revision = ?, updated_at = ?, deleted_at = ?
                WHERE table_id = ? AND row_id = ?`,
            );
            for (const row of currentRows) {
              const nextRevision = row.revision + 1;
              remove.run(
                nextRevision,
                timestamp,
                timestamp,
                DEFAULT_TABLE_ID,
                row.row_id,
              );
              affectedCount += 1;
              beforeAfter.push({
                row_id: row.row_id,
                before: row.values,
                before_revision: row.revision,
                after: null,
                after_revision: nextRevision,
              });
            }
          }
        }
        const nextDataRevision =
          table.data_revision + (affectedCount > 0 ? 1 : 0);
        if (affectedCount > 0) {
          this.db
            .prepare("UPDATE tables SET data_revision = ? WHERE table_id = ?")
            .run(nextDataRevision, DEFAULT_TABLE_ID);
        }
        const receipt = {
          operation_id: operationID,
          table_id: DEFAULT_TABLE_ID,
          action: args.action,
          affected_count: affectedCount,
          row_ids: rowIDs,
          schema_revision: table.schema_revision,
          data_revision: nextDataRevision,
          created_at: timestamp,
          replayed: false,
          source_kind: source.source_kind,
          client_command_id: source.client_command_id ?? null,
          undo: null,
        };
        this.db
          .prepare(
            `INSERT INTO operations
               (operation_id, table_id, source_kind, source_session_id,
                source_message_ids, source_time, client_command_id,
                execution_scope, dedupe_kind, dedupe_key, intent_hash,
                command_hash, canonical_command, before_after, receipt,
                undo_of, created_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
          )
          .run(
            operationID,
            DEFAULT_TABLE_ID,
            source.source_kind,
            source.session_id ?? null,
            canonicalJSON(source.message_ids),
            source.message_time ?? null,
            source.client_command_id ?? null,
            executionScope(source),
            dedupeKind,
            dedupeKey,
            intentHash,
            commandHash,
            canonicalJSON(command),
            canonicalJSON(beforeAfter),
            canonicalJSON(receipt),
            timestamp,
          );
        return receipt;
      });
    } catch (error) {
      throw asTableError(error);
    }
  }

  previewChange(action, expectedSchemaRevision, rows, targets, values) {
    return this.read(() => {
      const table = tableRecord(this.db);
      if (!table) {
        throw new TableError("database_not_initialized", "The table database is empty.");
      }
      if (table.schema_revision !== expectedSchemaRevision) {
        throw new TableError(
          "version_conflict",
          "The table structure does not match expected_schema_revision.",
          {
            expected_revision: expectedSchemaRevision,
            current_revision: table.schema_revision,
          },
        );
      }
      if (action === "insert") {
        return {
          action,
          affected_count: rows.length,
          row_ids: [],
          schema_revision: table.schema_revision,
          data_revision: table.data_revision,
        };
      }
      const currentRows = rowsForTargets(this.db, targets);
      let affectedCount = currentRows.length;
      if (action === "update") {
        affectedCount = currentRows.filter(
          (row) =>
            canonicalJSON({ ...row.values, ...values }) !== canonicalJSON(row.values),
        ).length;
      }
      return {
        action,
        affected_count: affectedCount,
        row_ids: currentRows.map((row) => row.row_id),
        schema_revision: table.schema_revision,
        data_revision: table.data_revision,
      };
    });
  }

  detail(args) {
    assertPlainObject(args, "table_debt_details", ["row_id"]);
    const rowID = requiredText(args.row_id, "row_id");
    return this.read(() => {
      const row = this.db
        .prepare(
          `SELECT row_id, revision, values_json, created_at, updated_at
             FROM rows
            WHERE table_id = ? AND row_id = ? AND deleted_at IS NULL`,
        )
        .get(DEFAULT_TABLE_ID, rowID);
      if (!row) {
        throw new TableError(
          "row_not_found",
          `Row ${rowID} does not exist in the active table.`,
        );
      }
      const planCount = Number(
        this.db
          .prepare(
            "SELECT COUNT(*) AS count FROM fuyao_repayment_plans WHERE debt_id = ?",
          )
          .get(rowID).count,
      );
      const currentPlan = this.db
        .prepare(
          `SELECT *
             FROM fuyao_repayment_plans
            WHERE debt_id = ? AND status <> 'replaced'
            ORDER BY effective_on DESC, created_at DESC, id DESC
            LIMIT 1`,
        )
        .get(rowID) ?? null;
      let installmentCounts = {};
      let installments = [];
      if (currentPlan) {
        installments = this.db
          .prepare(
            `SELECT id, installment_no, due_on, principal_cents, interest_cents,
                    old_interest_cents, old_fee_cents, new_fee_cents, total_cents,
                    status, actual_paid_on, note, adjusted_at
               FROM fuyao_planned_payments
              WHERE plan_id = ?
              ORDER BY installment_no, id`,
          )
          .all(currentPlan.id)
          .map((installment) => ({
            ...installment,
            principal: decimalFromCents(installment.principal_cents, 2),
            interest: decimalFromCents(installment.interest_cents, 2),
            old_interest: decimalFromCents(installment.old_interest_cents, 2),
            old_fee: decimalFromCents(installment.old_fee_cents, 2),
            new_fee: decimalFromCents(installment.new_fee_cents, 2),
            total: decimalFromCents(installment.total_cents, 2),
          }));
        installmentCounts = Object.fromEntries(
          this.db
            .prepare(
              `SELECT status, COUNT(*) AS count
                 FROM fuyao_planned_payments
                WHERE plan_id = ?
                GROUP BY status
                ORDER BY status`,
            )
            .all(currentPlan.id)
            .map((entry) => [entry.status, Number(entry.count)]),
        );
      }
      const paymentCount = Number(
        this.db
          .prepare(
            "SELECT COUNT(*) AS count FROM fuyao_payments WHERE debt_id = ?",
          )
          .get(rowID).count,
      );
      const paymentTotal = this.db
        .prepare(
          `SELECT COALESCE(SUM(amount_cents), 0) AS total
             FROM fuyao_payments
            WHERE debt_id = ?`,
        )
        .get(rowID).total;
      const payments = this.db
        .prepare(
          `SELECT id, amount_cents, payment_on, principal_effect, note, created_at
             FROM fuyao_payments
            WHERE debt_id = ?
            ORDER BY payment_on DESC, created_at DESC, id DESC`,
        )
        .all(rowID)
        .map((payment) => ({
          ...payment,
          amount: decimalFromCents(payment.amount_cents, 2),
          amount_cents: payment.amount_cents,
        }));
      const eventCount = Number(
        this.db
          .prepare(
            "SELECT COUNT(*) AS count FROM fuyao_debt_events WHERE debt_id = ?",
          )
          .get(rowID).count,
      );
      const events = this.db
        .prepare(
          `SELECT id, event_type, title, content, amount_cents, occurred_at,
                  created_at, updated_at
             FROM fuyao_debt_events
            WHERE debt_id = ?
            ORDER BY occurred_at DESC, created_at DESC, id DESC`,
        )
        .all(rowID)
        .map((event) => ({
          ...event,
          amount: decimalFromCents(event.amount_cents, 2),
          amount_cents: event.amount_cents,
        }));
      const currentPlanSummary = currentPlan
        ? {
            id: currentPlan.id,
            status: currentPlan.status,
            method: currentPlan.method,
            effective_on: currentPlan.effective_on,
            first_due_on: currentPlan.first_due_on,
            installments: currentPlan.installments,
            installment_rows: installments,
            plan_principal:
              currentPlan.plan_principal_cents === null
                ? null
                : decimalFromCents(currentPlan.plan_principal_cents, 2),
            confirmed_remaining:
              currentPlan.confirmed_remaining_cents === null
                ? null
                : decimalFromCents(currentPlan.confirmed_remaining_cents, 2),
            original_rate_value: currentPlan.original_rate_value,
            rate_period: currentPlan.rate_period,
            updated_at: currentPlan.updated_at,
          }
        : null;
      return {
        table_id: DEFAULT_TABLE_ID,
        row_id: row.row_id,
        revision: row.revision,
        debt: rowValuesForOutput(parseJSON(row.values_json)),
        plan_count: planCount,
        current_plan: currentPlanSummary,
        installment_counts: installmentCounts,
        payment_summary: {
          count: paymentCount,
          total: decimalFromCentsString(BigInt(paymentTotal), 2),
          currency: "CNY",
        },
        payments,
        recent_payments: payments.slice(0, 5),
        event_summary: { count: eventCount },
        events,
        recent_events: events.slice(0, 5),
        data_source: importMetaRecord(this.db),
        created_at: row.created_at,
        updated_at: row.updated_at,
      };
    });
  }

  undo(args, rawSource) {
    assertPlainObject(args, "table_undo", ["operation_id", "dry_run"]);
    const operationID = requiredText(args.operation_id, "operation_id");
    const dryRun = args.dry_run === true;
    const source = dryRun
      ? normalizeByop(rawSource)
      : sourceForWrite(normalizeByop(rawSource));

    const inspectUndo = (allowReplay) => {
      const existing = undoRecordFor(this.db, operationID);
      if (existing) {
        return {
          dry_run: dryRun,
          operation_id: existing.operation_id,
          undo_of: operationID,
          affected_count: existing.receipt.affected_count,
          row_ids: existing.receipt.row_ids,
          schema_revision: existing.receipt.schema_revision,
          data_revision: existing.receipt.data_revision,
          created_at: existing.created_at,
          replayed: allowReplay,
        };
      }
      const original = this.db
        .prepare(
          `SELECT operation_id, source_kind, source_session_id,
                  source_message_ids, source_time, client_command_id,
                  canonical_command, before_after, receipt, undo_of, created_at
             FROM operations
            WHERE operation_id = ? AND table_id = ?`,
        )
        .get(operationID, DEFAULT_TABLE_ID);
      if (!original) {
        throw new TableError(
          "operation_not_found",
          `Operation ${operationID} does not exist.`,
        );
      }
      if (original.undo_of) {
        throw new TableError(
          "operation_not_undoable",
          "Undo operations cannot be undone in this version.",
        );
      }
      const receipt = parseJSON(original.receipt);
      if (!Number.isSafeInteger(receipt.affected_count) || receipt.affected_count < 1) {
        throw new TableError(
          "operation_not_undoable",
          "Operations with no data changes cannot be undone.",
        );
      }
      const table = tableRecord(this.db);
      if (!table) {
        throw new TableError("database_not_initialized", "The table database is empty.");
      }
      if (table.schema_revision !== receipt.schema_revision) {
        throw new TableError(
          "version_conflict",
          "The table structure changed after the original operation.",
          {
            expected_revision: receipt.schema_revision,
            current_revision: table.schema_revision,
          },
        );
      }
      const beforeAfter = parseJSON(original.before_after);
      const statement = this.db.prepare(
        `SELECT row_id, revision, values_json, deleted_at
           FROM rows
          WHERE table_id = ? AND row_id = ?`,
      );
      const inverse = [];
      for (const entry of beforeAfter) {
        const current = statement.get(DEFAULT_TABLE_ID, entry.row_id);
        if (!current) {
          throw new TableError(
            "version_conflict",
            `Row ${entry.row_id} no longer exists for undo.`,
            {
              row_id: entry.row_id,
              expected_revision: entry.after_revision,
              current_revision: null,
            },
          );
        }
        if (current.revision !== entry.after_revision) {
          throw new TableError(
            "version_conflict",
            `Row ${entry.row_id} changed after the original operation.`,
            {
              row_id: entry.row_id,
              expected_revision: entry.after_revision,
              current_revision: current.revision,
            },
          );
        }
        const isInsert = entry.before === null;
        const isDelete = entry.after === null && entry.before !== null;
        if (isInsert && current.deleted_at !== null) {
          throw new TableError(
            "version_conflict",
            `Row ${entry.row_id} was already removed.`,
            { row_id: entry.row_id },
          );
        }
        if (isDelete && current.deleted_at === null) {
          throw new TableError(
            "version_conflict",
            `Row ${entry.row_id} was restored outside this undo chain.`,
            { row_id: entry.row_id },
          );
        }
        inverse.push({
          row_id: entry.row_id,
          before: parseJSON(current.values_json),
          before_revision: current.revision,
          after: isInsert ? null : entry.before,
          after_revision: current.revision + 1,
          undo_mode: isInsert
            ? "delete"
            : isDelete
              ? "restore"
              : "update",
        });
      }
      return {
        original,
        receipt,
        table,
        inverse,
        beforeAfter,
      };
    };

    if (dryRun) {
      return this.read(() => {
        const preview = inspectUndo(false);
        if (preview.replayed) {
          return preview;
        }
        return {
          dry_run: true,
          operation_id: null,
          undo_of: operationID,
          affected_count: preview.inverse.length,
          row_ids: preview.inverse.map((entry) => entry.row_id),
          schema_revision: preview.table.schema_revision,
          data_revision: preview.table.data_revision,
          replayed: false,
        };
      });
    }

    try {
      return this.immediate(() => {
        const inspected = inspectUndo(true);
        if (inspected.replayed) {
          return inspected;
        }
        const timestamp = nowISO();
        const undoOperationID = randomUUID();
        const update = this.db.prepare(
          `UPDATE rows
              SET revision = ?, values_json = ?, updated_at = ?, deleted_at = ?
            WHERE table_id = ? AND row_id = ?`,
        );
        for (const entry of inspected.inverse) {
          update.run(
            entry.after_revision,
            canonicalJSON(entry.after ?? entry.before),
            timestamp,
            entry.after === null ? timestamp : null,
            DEFAULT_TABLE_ID,
            entry.row_id,
          );
        }
        const nextDataRevision = inspected.table.data_revision + 1;
        this.db
          .prepare("UPDATE tables SET data_revision = ? WHERE table_id = ?")
          .run(nextDataRevision, DEFAULT_TABLE_ID);
        const canonicalCommand = {
          action: "undo",
          operation_id: operationID,
        };
        const commandHash = sha256({
          ...canonicalCommand,
          expected_schema_revision: inspected.receipt.schema_revision,
          targets: inspected.inverse.map((entry) => ({
            row_id: entry.row_id,
            expected_revision: entry.before_revision,
          })),
        });
        const intentHash = sha256(canonicalCommand);
        const receipt = {
          operation_id: undoOperationID,
          undo_of: operationID,
          table_id: DEFAULT_TABLE_ID,
          action: "undo",
          affected_count: inspected.inverse.length,
          row_ids: inspected.inverse.map((entry) => entry.row_id),
          schema_revision: inspected.table.schema_revision,
          data_revision: nextDataRevision,
          created_at: timestamp,
          replayed: false,
          source_kind: source.source_kind,
          client_command_id: source.client_command_id ?? null,
          undo: null,
        };
        const dedupeKey =
          source.source_kind === "page" ? "page-command" : commandHash;
        this.db
          .prepare(
            `INSERT INTO operations
               (operation_id, table_id, source_kind, source_session_id,
                source_message_ids, source_time, client_command_id,
                execution_scope, dedupe_kind, dedupe_key, intent_hash,
                command_hash, canonical_command, before_after, receipt,
                undo_of, created_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'command', ?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            undoOperationID,
            DEFAULT_TABLE_ID,
            source.source_kind,
            source.session_id ?? null,
            canonicalJSON(source.message_ids),
            source.message_time ?? null,
            source.client_command_id ?? null,
            executionScope(source),
            dedupeKey,
            intentHash,
            commandHash,
            canonicalJSON(canonicalCommand),
            canonicalJSON(inspected.inverse),
            canonicalJSON(receipt),
            operationID,
            timestamp,
          );
        return receipt;
      });
    } catch (error) {
      throw asTableError(error);
    }
  }

  history(args) {
    assertPlainObject(args, "table_history", [
      "operation_id",
      "source_session_id",
      "message_id",
      "limit",
      "offset",
    ]);
    const limit = args.limit ?? 50;
    const offset = args.offset ?? 0;
    if (!Number.isInteger(limit) || limit < 1 || limit > 100) {
      throw new TableError("invalid_arguments", "limit must be an integer from 1 to 100.");
    }
    if (!Number.isInteger(offset) || offset < 0) {
      throw new TableError("invalid_arguments", "offset must be a non-negative integer.");
    }
    let sql = `SELECT operation_id, source_kind, source_session_id,
                      source_message_ids, source_time, client_command_id,
                      execution_scope, dedupe_kind, dedupe_key, receipt,
                      undo_of, created_at
                 FROM operations
                WHERE table_id = ?`;
    const parameters = [DEFAULT_TABLE_ID];
    if (args.operation_id !== undefined) {
      sql += " AND operation_id = ?";
      parameters.push(requiredText(args.operation_id, "operation_id"));
    }
    if (args.source_session_id !== undefined) {
      sql += " AND source_session_id = ?";
      parameters.push(requiredText(args.source_session_id, "source_session_id"));
    }
    if (args.message_id !== undefined) {
      sql +=
        " AND EXISTS (SELECT 1 FROM json_each(operations.source_message_ids) WHERE value = ?)";
      parameters.push(requiredText(args.message_id, "message_id"));
    }
    sql += " ORDER BY created_at DESC LIMIT ? OFFSET ?";
    parameters.push(limit, offset);
    const operations = this.db
      .prepare(sql)
      .all(...parameters)
      .map((operation) => {
        const receipt = parseJSON(operation.receipt);
        const undo = operation.undo_of
          ? null
          : undoRecordFor(this.db, operation.operation_id);
        return {
          operation_id: operation.operation_id,
          source_kind: operation.source_kind,
          source_session_id: operation.source_session_id,
          source_message_ids: parseJSON(operation.source_message_ids),
          source_time: operation.source_time,
          client_command_id: operation.client_command_id,
          execution_scope: operation.execution_scope,
          dedupe_kind: operation.dedupe_kind,
          dedupe_key: operation.dedupe_key,
          receipt,
          undo_of: operation.undo_of,
          undo,
          undoable:
            operation.undo_of === null &&
            receipt.affected_count > 0 &&
            undo === null,
          created_at: operation.created_at,
        };
      });
    return {
      table_id: DEFAULT_TABLE_ID,
      operations,
      count: operations.length,
      offset,
      limit,
    };
  }
}

export const columnDefinitions = COLUMN_DEFINITIONS;
