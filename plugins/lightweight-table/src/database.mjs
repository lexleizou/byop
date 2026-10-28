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

export const FORMAT_VERSION = 1;
export const DEFAULT_TABLE_ID = "tbl_default";

const COLUMN_DEFINITIONS = [
  {
    column_id: "name",
    label: "名称",
    type: "text",
    required: true,
    config: { source_field: "product_name" },
  },
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
    column_id: "funding_source",
    label: "资金来源",
    type: "text",
    required: false,
    config: { source_field: "funding_source" },
  },
  {
    column_id: "principal",
    label: "本金",
    type: "number",
    required: false,
    config: {
      precision: 2,
      scale: 2,
      currency: "CNY",
      source_field: "contract_principal_cents",
    },
  },
  {
    column_id: "amount_due",
    label: "待还金额",
    type: "number",
    required: false,
    config: { precision: 2, scale: 2, currency: "CNY", source_field: null },
  },
  {
    column_id: "due_date",
    label: "还款日",
    type: "date",
    required: false,
    config: { source_field: null },
  },
  {
    column_id: "repayment_status",
    label: "还款状态",
    type: "text",
    required: false,
    config: { source_field: "repayment_status" },
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
  created_at TEXT NOT NULL,
  UNIQUE (execution_scope, dedupe_key)
);

CREATE INDEX IF NOT EXISTS rows_table_active
  ON rows (table_id, deleted_at);
CREATE INDEX IF NOT EXISTS operations_table_created
  ON operations (table_id, created_at DESC);
CREATE INDEX IF NOT EXISTS operations_source_session
  ON operations (source_session_id, created_at DESC);
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
  const rows = input.rows.map((row, index) => {
    assertPlainObject(row, `rows[${index}]`, [
      "name",
      "institution",
      "category",
      "funding_source",
      "principal",
      "amount_due",
      "due_date",
      "repayment_status",
    ]);
    return {
      name: requiredText(row.name, `rows[${index}].name`),
      institution: optionalText(row.institution, `rows[${index}].institution`),
      category: optionalText(row.category, `rows[${index}].category`),
      funding_source: optionalText(
        row.funding_source,
        `rows[${index}].funding_source`,
      ),
      principal: centsFromDecimal(row.principal, `rows[${index}].principal`, 2),
      amount_due: centsFromDecimal(row.amount_due, `rows[${index}].amount_due`, 2),
      due_date: validDate(row.due_date, `rows[${index}].due_date`),
      repayment_status: optionalText(
        row.repayment_status,
        `rows[${index}].repayment_status`,
      ),
    };
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

function parseJSON(value) {
  return value === null || value === undefined ? null : JSON.parse(value);
}

function rowValuesForOutput(values) {
  return {
    ...values,
    principal: decimalFromCents(values.principal, 2),
    amount_due: decimalFromCents(values.amount_due, 2),
  };
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
    switch (column.type) {
      case "number":
        normalized[columnID] = centsFromDecimal(
          rawValue,
          `${label}.${columnID}`,
          column.config.scale,
        );
        break;
      case "date":
        normalized[columnID] = validDate(rawValue, `${label}.${columnID}`);
        break;
      case "text":
        normalized[columnID] = optionalText(rawValue, `${label}.${columnID}`);
        if (column.required && normalized[columnID] === null) {
          throw new TableError(
            "invalid_value",
            `${label}.${columnID} is required.`,
          );
        }
        break;
      default:
        throw new TableError("internal_error", `Unknown column type ${column.type}.`);
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
    if (!["eq", "contains", "is_null", "not_null"].includes(filter.op)) {
      throw new TableError(
        "invalid_arguments",
        `filters[${index}].op is not supported.`,
      );
    }
    return {
      column_id: columnID,
      op: filter.op,
      value: filter.value,
    };
  });
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
    return typeof value === "string" && value.includes(String(filter.value ?? ""));
  }
  const column = COLUMN_BY_ID.get(filter.column_id);
  const expected =
    column.type === "number"
      ? centsFromDecimal(filter.value, `filters.${filter.column_id}`, column.config.scale)
      : column.type === "date"
        ? validDate(filter.value, `filters.${filter.column_id}`)
        : filter.value ?? null;
  return value === expected;
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

function receiptForReplay(receipt) {
  return { ...receipt, replayed: true };
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
    };
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
           VALUES (?, ?, 1, ?)`,
        )
        .run(DEFAULT_TABLE_ID, "银行负债", normalized.rows.length > 0 ? 1 : 0);
      const insertColumn = this.db.prepare(
        `INSERT INTO columns
           (column_id, table_id, label, type, required, config, position, hidden)
         VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
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
        );
      });
      const insertRow = this.db.prepare(
        `INSERT INTO rows
           (row_id, table_id, revision, values_json, created_at, updated_at, deleted_at)
         VALUES (?, ?, 1, ?, ?, ?, NULL)`,
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
        schema_revision: 1,
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
        source_local_date: localDateForSource(source, workbook.timezone),
      };
    });
  }

  query(args, rawSource) {
    assertPlainObject(args, "table_query", [
      "table_id",
      "row_ids",
      "filters",
      "projection",
      "limit",
      "offset",
      "sum_column_id",
    ]);
    if (args.table_id !== undefined && args.table_id !== DEFAULT_TABLE_ID) {
      throw new TableError("table_not_found", "Only the default table is available.");
    }
    const source = normalizeByop(rawSource);
    const limit = args.limit ?? 50;
    const offset = args.offset ?? 0;
    if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
      throw new TableError("invalid_arguments", "limit must be an integer from 1 to 200.");
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
    const filters = normalizeFilters(args.filters);
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
    return this.read(() => {
      const workbook = workbookRecord(this.db);
      const table = tableRecord(this.db);
      if (!workbook || !table) {
        throw new TableError("database_not_initialized", "The table database is empty.");
      }
      const allRows = this.db
        .prepare(
          `SELECT row_id, revision, values_json, created_at, updated_at
             FROM rows
            WHERE table_id = ? AND deleted_at IS NULL
            ORDER BY created_at, row_id`,
        )
        .all(DEFAULT_TABLE_ID)
        .map((row) => ({
          row_id: row.row_id,
          revision: row.revision,
          values: parseJSON(row.values_json),
          created_at: row.created_at,
          updated_at: row.updated_at,
        }));
      const matched = allRows.filter(
        (row) =>
          (!rowIDs || rowIDs.has(row.row_id)) &&
          filters.every((filter) => matchesFilter(row, filter)),
      );
      const page = matched.slice(offset, offset + limit);
      const result = {
        table_id: DEFAULT_TABLE_ID,
        schema_revision: table.schema_revision,
        data_revision: table.data_revision,
        matched_count: matched.length,
        offset,
        limit,
        rows: page.map((row) => ({
          row_id: row.row_id,
          revision: row.revision,
          values: Object.fromEntries(
            projection.map((columnID) => [
              columnID,
              rowValuesForOutput(row.values)[columnID],
            ]),
          ),
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
      return result;
    });
  }

  change(args, rawSource) {
    assertPlainObject(args, "table_change", [
      "action",
      "table_id",
      "rows",
      "targets",
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
      if (args.targets !== undefined || args.values !== undefined) {
        throw new TableError(
          "invalid_arguments",
          "Insert accepts rows and does not accept targets or values.",
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
      targets = normalizeTargets(args.targets);
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
    const dedupeKind = args.action === "insert" ? "intent" : "command";
    const dedupeKey = args.action === "insert" ? intentHash : commandHash;

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
            `SELECT receipt
               FROM operations
              WHERE execution_scope = ? AND dedupe_key = ?`,
          )
          .get(executionScope(source), dedupeKey);
        if (lookup) {
          return receiptForReplay(parseJSON(lookup.receipt));
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
        };
        this.db
          .prepare(
            `INSERT INTO operations
               (operation_id, table_id, source_kind, source_session_id,
                source_message_ids, source_time, client_command_id,
                execution_scope, dedupe_kind, dedupe_key, intent_hash,
                command_hash, canonical_command, before_after, receipt, created_at)
             VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
          )
          .run(
            operationID,
            DEFAULT_TABLE_ID,
            source.source_kind,
            source.session_id,
            canonicalJSON(source.message_ids),
            source.message_time,
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
    if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
      throw new TableError("invalid_arguments", "limit must be an integer from 1 to 200.");
    }
    if (!Number.isInteger(offset) || offset < 0) {
      throw new TableError("invalid_arguments", "offset must be a non-negative integer.");
    }
    let sql = `SELECT operation_id, source_kind, source_session_id,
                      source_message_ids, source_time, execution_scope,
                      dedupe_kind, dedupe_key, receipt, created_at
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
      .map((operation) => ({
        operation_id: operation.operation_id,
        source_kind: operation.source_kind,
        source_session_id: operation.source_session_id,
        source_message_ids: parseJSON(operation.source_message_ids),
        source_time: operation.source_time,
        execution_scope: operation.execution_scope,
        dedupe_kind: operation.dedupe_kind,
        dedupe_key: operation.dedupe_key,
        receipt: parseJSON(operation.receipt),
        created_at: operation.created_at,
      }));
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
