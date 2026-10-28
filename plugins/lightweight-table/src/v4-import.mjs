import { createHash, randomUUID } from "node:crypto";
import {
  existsSync,
  readFileSync,
  statSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";

import { canonicalJSON, sha256 } from "./canonical.mjs";
import {
  DEFAULT_TABLE_ID,
  FORMAT_VERSION,
  TABLE_SCHEMA_REVISION,
  columnDefinitions,
} from "./database.mjs";
import { TableError } from "./errors.mjs";

export const V4_SOURCE_FILES = Object.freeze([
  "debts-api.json",
  "debts.json",
  "repayment_plans.json",
  "planned_payments.json",
  "payments.json",
  "payment_allocations.json",
  "debt_events.json",
]);

export const V4_IMPORT_FORMAT_VERSION = "byop-fuyao-v4-snapshot/1";

const PLAN_COLUMNS = Object.freeze([
  "id",
  "debt_id",
  "basis_explanation",
  "confirmed_remaining_cents",
  "created_at",
  "effective_on",
  "excluded_old_fee_cents",
  "excluded_old_interest_cents",
  "excluded_principal_cents",
  "first_due_on",
  "included_old_fee_cents",
  "included_old_interest_cents",
  "installments",
  "interest_basis",
  "interest_principal_cents",
  "method",
  "new_fee_cents",
  "original_rate_value",
  "plan_principal_cents",
  "rate_period",
  "source_note",
  "status",
  "updated_at",
  "verified_balance_at",
  "verified_balance_cents",
]);

const PLANNED_PAYMENT_COLUMNS = Object.freeze([
  "id",
  "plan_id",
  "debt_id",
  "actual_paid_on",
  "adjusted_at",
  "due_on",
  "installment_no",
  "interest_cents",
  "new_fee_cents",
  "note",
  "old_fee_cents",
  "old_interest_cents",
  "principal_cents",
  "status",
  "total_cents",
]);

const PAYMENT_COLUMNS = Object.freeze([
  "id",
  "debt_id",
  "amount_cents",
  "auto_allocation_blocked",
  "created_at",
  "note",
  "payment_on",
  "principal_effect",
]);

const ALLOCATION_COLUMNS = Object.freeze([
  "id",
  "payment_id",
  "planned_payment_id",
  "allocated_at",
  "allocation_method",
  "amount_cents",
]);

const EVENT_COLUMNS = Object.freeze([
  "id",
  "debt_id",
  "amount_cents",
  "content",
  "created_at",
  "event_type",
  "occurred_at",
  "title",
  "updated_at",
]);

function fail(message, details = undefined) {
  throw new TableError("invalid_v4_snapshot", message, details);
}

function object(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    fail(`${label} must be an object.`);
  }
  return value;
}

function array(value, label) {
  if (!Array.isArray(value)) {
    fail(`${label} must be an array.`);
  }
  return value;
}

function text(value, label, { required = false, nullable = true } = {}) {
  if (value === null || value === undefined) {
    if (required) {
      fail(`${label} is required.`);
    }
    if (nullable) {
      return null;
    }
  }
  if (typeof value !== "string" || (required && value.trim() === "")) {
    fail(`${label} must be ${required ? "a non-empty " : "a "}string${nullable ? " or null" : ""}.`);
  }
  return value;
}

function date(value, label, { required = false } = {}) {
  if (value === null || value === undefined) {
    if (required) {
      fail(`${label} is required.`);
    }
    return null;
  }
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    fail(`${label} must use YYYY-MM-DD.`);
  }
  const parsed = new Date(`${value}T00:00:00Z`);
  if (
    Number.isNaN(parsed.getTime()) ||
    parsed.toISOString().slice(0, 10) !== value
  ) {
    fail(`${label} is not a valid calendar date.`);
  }
  return value;
}

function timestamp(value, label, { required = false } = {}) {
  if (value === null || value === undefined) {
    if (required) {
      fail(`${label} is required.`);
    }
    return null;
  }
  if (typeof value !== "string" || Number.isNaN(Date.parse(value))) {
    fail(`${label} must be an ISO-compatible timestamp.`);
  }
  return value;
}

function integer(value, label, { required = false } = {}) {
  if (value === null || value === undefined) {
    if (required) {
      fail(`${label} is required.`);
    }
    return null;
  }
  if (!Number.isSafeInteger(value)) {
    fail(`${label} must be a safe integer.`);
  }
  return value;
}

function cents(value, label, { required = false } = {}) {
  return integer(value, label, { required });
}

function booleanInteger(value, label) {
  if (typeof value !== "boolean") {
    fail(`${label} must be a boolean.`);
  }
  return value ? 1 : 0;
}

function plainNumber(value, label) {
  if (value === null || value === undefined) {
    return null;
  }
  if (
    (typeof value !== "number" && typeof value !== "string") ||
    String(value).trim() === "" ||
    !Number.isFinite(Number(value))
  ) {
    fail(`${label} must be numeric or null.`);
  }
  return Number(value);
}

function id(value, label) {
  return text(value, label, { required: true, nullable: false });
}

function uniqueIDs(records, label) {
  const ids = records.map((record, index) => id(record.id, `${label}[${index}].id`));
  if (new Set(ids).size !== ids.length) {
    fail(`${label} contains duplicate IDs.`);
  }
  return new Set(ids);
}

function rateLabel(value, period) {
  if (value === null || value === undefined || String(value).trim() === "") {
    return null;
  }
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) {
    fail("rate value must be numeric.");
  }
  const rate = `${numeric.toFixed(2)}%`;
  if (!period) {
    return rate;
  }
  const labels = { day: "日", month: "月", year: "年" };
  return `${rate}／${labels[period] ?? period}`;
}

function readSource(inputDirectory) {
  const directory = resolve(inputDirectory);
  const files = new Map();
  for (const filename of [...V4_SOURCE_FILES, "manifest.json"]) {
    const path = join(directory, filename);
    if (!existsSync(path) || !statSync(path).isFile()) {
      fail(`Required source file ${filename} is missing.`);
    }
    const bytes = readFileSync(path);
    let value;
    try {
      value = JSON.parse(bytes.toString("utf8"));
    } catch (error) {
      fail(`${filename} is not valid JSON.`, {
        cause: error instanceof Error ? error.message : String(error),
      });
    }
    files.set(filename, { bytes, value });
  }
  return { directory, files };
}

function validateManifest(files) {
  const manifest = object(files.get("manifest.json").value, "manifest.json");
  if (manifest.format_version !== 4) {
    fail("manifest.json format_version must be 4.");
  }
  if (manifest.read_only !== true) {
    fail("manifest.json must describe a read-only export.");
  }
  text(manifest.source_instance, "manifest.source_instance", {
    required: true,
    nullable: false,
  });
  date(manifest.source_date, "manifest.source_date", { required: true });
  timestamp(manifest.exported_at, "manifest.exported_at", { required: true });
  const counts = object(manifest.counts, "manifest.counts");
  const digests = object(manifest.sha256, "manifest.sha256");
  for (const filename of V4_SOURCE_FILES) {
    const records = array(files.get(filename).value, filename);
    if (!Number.isSafeInteger(counts[filename]) || counts[filename] !== records.length) {
      fail(`manifest count for ${filename} does not match the export.`, {
        expected: counts[filename],
        actual: records.length,
      });
    }
    const expectedDigest = digests[filename];
    if (
      typeof expectedDigest !== "string" ||
      !/^[a-f0-9]{64}$/.test(expectedDigest)
    ) {
      fail(`manifest digest for ${filename} is invalid.`);
    }
    const actualDigest = createHash("sha256")
      .update(files.get(filename).bytes)
      .digest("hex");
    if (actualDigest !== expectedDigest) {
      fail(`manifest digest for ${filename} does not match the export.`);
    }
  }
  return { manifest, counts, digests };
}

function normalizeDebtRows(apiDebts, rawDebts) {
  const apiByID = new Map();
  for (const [index, raw] of apiDebts.entries()) {
    const record = object(raw, `debts-api[${index}]`);
    const recordID = id(record.id, `debts-api[${index}].id`);
    if (apiByID.has(recordID)) {
      fail("debts-api.json contains duplicate IDs.");
    }
    apiByID.set(recordID, record);
  }
  const rawByID = new Map();
  for (const [index, raw] of rawDebts.entries()) {
    const record = object(raw, `debts[${index}]`);
    const recordID = id(record.id, `debts[${index}].id`);
    if (rawByID.has(recordID)) {
      fail("debts.json contains duplicate IDs.");
    }
    rawByID.set(recordID, record);
  }
  if (
    apiByID.size !== rawByID.size ||
    [...apiByID.keys()].some((recordID) => !rawByID.has(recordID))
  ) {
    fail("API and raw debt IDs/counts do not match.");
  }

  const rows = [];
  for (const [recordID, api] of apiByID) {
    const raw = rawByID.get(recordID);
    const nextDue = api.next_due === null ? null : object(api.next_due, `debts API ${recordID}.next_due`);
    const originalRate =
      api.original_rate_value === null || api.original_rate_value === undefined
        ? raw.original_rate_value
        : api.original_rate_value;
    const manualRate = plainNumber(
      api.manual_annualized_rate_percent,
      `debts API ${recordID}.manual_annualized_rate_percent`,
    );
    const calculatedRate = plainNumber(
      api.calculated_annualized_rate_percent,
      `debts API ${recordID}.calculated_annualized_rate_percent`,
    );
    const ratePeriod = text(api.rate_period, `debts API ${recordID}.rate_period`);
    const annualizedRate =
      rateLabel(manualRate, null) ??
      rateLabel(calculatedRate, null) ??
      rateLabel(originalRate, ratePeriod);
    const values = {
      institution: text(api.creditor, `debts API ${recordID}.creditor`, {
        required: true,
        nullable: false,
      }),
      category: text(api.category, `debts API ${recordID}.category`),
      name: text(api.product_name, `debts API ${recordID}.product_name`, {
        required: true,
        nullable: false,
      }),
      principal: cents(
        api.contract_principal_cents,
        `debts API ${recordID}.contract_principal_cents`,
      ),
      actual_received: cents(
        api.actual_disbursed_cents,
        `debts API ${recordID}.actual_disbursed_cents`,
      ),
      remaining_due: cents(
        api.principal_balance_cents,
        `debts API ${recordID}.principal_balance_cents`,
        { required: true },
      ),
      annualized_rate: annualizedRate,
      repayment_status: text(
        api.repayment_status,
        `debts API ${recordID}.repayment_status`,
        { required: true, nullable: false },
      ),
      plan_status: text(api.plan_status, `debts API ${recordID}.plan_status`, {
        required: true,
        nullable: false,
      }),
      next_due: nextDue
        ? date(nextDue.due_on, `debts API ${recordID}.next_due.due_on`, {
            required: true,
          })
        : null,
      next_due_amount: nextDue
        ? cents(nextDue.amount_cents, `debts API ${recordID}.next_due.amount_cents`, {
            required: true,
          })
        : null,
      funding_source: text(raw.funding_source, `debts ${recordID}.funding_source`),
      product_type: text(api.product_type, `debts API ${recordID}.product_type`),
      source_record_id: recordID,
      product_group_id: text(
        api.product_group_id,
        `debts API ${recordID}.product_group_id`,
      ),
      loan_sequence: integer(
        api.loan_sequence,
        `debts API ${recordID}.loan_sequence`,
      ),
      loan_code: text(api.loan_code, `debts API ${recordID}.loan_code`),
      contract_reference: text(
        api.contract_reference,
        `debts API ${recordID}.contract_reference`,
      ),
      opening_principal: cents(
        api.opening_principal_cents,
        `debts API ${recordID}.opening_principal_cents`,
        { required: true },
      ),
      planned_interest: cents(
        api.planned_interest_cents,
        `debts API ${recordID}.planned_interest_cents`,
        { required: true },
      ),
      remaining_interest: cents(
        api.remaining_interest_cents,
        `debts API ${recordID}.remaining_interest_cents`,
        { required: true },
      ),
      actual_paid: cents(
        api.actual_paid_cents,
        `debts API ${recordID}.actual_paid_cents`,
        { required: true },
      ),
      borrowed_on: date(api.borrowed_on, `debts API ${recordID}.borrowed_on`),
      disbursed_on: date(api.disbursed_on, `debts API ${recordID}.disbursed_on`),
      original_rate_value: plainNumber(
        originalRate,
        `debts API ${recordID}.original_rate_value`,
      ),
      rate_period: ratePeriod,
      manual_annualized_rate_percent: manualRate,
      calculated_annualized_rate_percent: calculatedRate,
      interest_basis: text(raw.interest_basis, `debts ${recordID}.interest_basis`),
      source_created_at: timestamp(
        api.created_at,
        `debts API ${recordID}.created_at`,
        { required: true },
      ),
      source_updated_at: timestamp(
        api.updated_at,
        `debts API ${recordID}.updated_at`,
        { required: true },
      ),
    };
    rows.push({ row_id: recordID, source_record_id: recordID, values });
  }
  return rows;
}

function normalizePlans(records) {
  uniqueIDs(records, "repayment_plans.json");
  return records.map((raw, index) => {
    const record = object(raw, `repayment_plans[${index}]`);
    const label = `repayment_plans[${index}]`;
    return {
      id: id(record.id, `${label}.id`),
      debt_id: id(record.debt_id, `${label}.debt_id`),
      basis_explanation: text(record.basis_explanation, `${label}.basis_explanation`),
      confirmed_remaining_cents: cents(
        record.confirmed_remaining_cents,
        `${label}.confirmed_remaining_cents`,
      ),
      created_at: timestamp(record.created_at, `${label}.created_at`, {
        required: true,
      }),
      effective_on: date(record.effective_on, `${label}.effective_on`, {
        required: true,
      }),
      excluded_old_fee_cents: cents(
        record.excluded_old_fee_cents,
        `${label}.excluded_old_fee_cents`,
        { required: true },
      ),
      excluded_old_interest_cents: cents(
        record.excluded_old_interest_cents,
        `${label}.excluded_old_interest_cents`,
        { required: true },
      ),
      excluded_principal_cents: cents(
        record.excluded_principal_cents,
        `${label}.excluded_principal_cents`,
        { required: true },
      ),
      first_due_on: date(record.first_due_on, `${label}.first_due_on`, {
        required: true,
      }),
      included_old_fee_cents: cents(
        record.included_old_fee_cents,
        `${label}.included_old_fee_cents`,
        { required: true },
      ),
      included_old_interest_cents: cents(
        record.included_old_interest_cents,
        `${label}.included_old_interest_cents`,
        { required: true },
      ),
      installments: integer(record.installments, `${label}.installments`, {
        required: true,
      }),
      interest_basis: text(record.interest_basis, `${label}.interest_basis`, {
        required: true,
        nullable: false,
      }),
      interest_principal_cents: cents(
        record.interest_principal_cents,
        `${label}.interest_principal_cents`,
        { required: true },
      ),
      method: text(record.method, `${label}.method`, {
        required: true,
        nullable: false,
      }),
      new_fee_cents: cents(record.new_fee_cents, `${label}.new_fee_cents`, {
        required: true,
      }),
      original_rate_value: plainNumber(
        record.original_rate_value,
        `${label}.original_rate_value`,
      ),
      plan_principal_cents: cents(
        record.plan_principal_cents,
        `${label}.plan_principal_cents`,
      ),
      rate_period: text(record.rate_period, `${label}.rate_period`, {
        required: true,
        nullable: false,
      }),
      source_note: text(record.source_note, `${label}.source_note`),
      status: text(record.status, `${label}.status`, {
        required: true,
        nullable: false,
      }),
      updated_at: timestamp(record.updated_at, `${label}.updated_at`, {
        required: true,
      }),
      verified_balance_at: timestamp(
        record.verified_balance_at,
        `${label}.verified_balance_at`,
      ),
      verified_balance_cents: cents(
        record.verified_balance_cents,
        `${label}.verified_balance_cents`,
      ),
    };
  });
}

function normalizePlannedPayments(records, planByID) {
  uniqueIDs(records, "planned_payments.json");
  return records.map((raw, index) => {
    const record = object(raw, `planned_payments[${index}]`);
    const label = `planned_payments[${index}]`;
    const planID = id(record.plan_id, `${label}.plan_id`);
    const plan = planByID.get(planID);
    if (!plan) {
      fail(`${label}.plan_id does not reference repayment_plans.json.`);
    }
    return {
      id: id(record.id, `${label}.id`),
      plan_id: planID,
      debt_id: plan.debt_id,
      actual_paid_on: date(record.actual_paid_on, `${label}.actual_paid_on`),
      adjusted_at: timestamp(record.adjusted_at, `${label}.adjusted_at`),
      due_on: date(record.due_on, `${label}.due_on`, { required: true }),
      installment_no: integer(record.installment_no, `${label}.installment_no`, {
        required: true,
      }),
      interest_cents: cents(record.interest_cents, `${label}.interest_cents`, {
        required: true,
      }),
      new_fee_cents: cents(record.new_fee_cents, `${label}.new_fee_cents`, {
        required: true,
      }),
      note: text(record.note, `${label}.note`),
      old_fee_cents: cents(record.old_fee_cents, `${label}.old_fee_cents`, {
        required: true,
      }),
      old_interest_cents: cents(
        record.old_interest_cents,
        `${label}.old_interest_cents`,
        { required: true },
      ),
      principal_cents: cents(record.principal_cents, `${label}.principal_cents`, {
        required: true,
      }),
      status: text(record.status, `${label}.status`, {
        required: true,
        nullable: false,
      }),
      total_cents: cents(record.total_cents, `${label}.total_cents`, {
        required: true,
      }),
    };
  });
}

function normalizePayments(records) {
  uniqueIDs(records, "payments.json");
  return records.map((raw, index) => {
    const record = object(raw, `payments[${index}]`);
    const label = `payments[${index}]`;
    return {
      id: id(record.id, `${label}.id`),
      debt_id: id(record.debt_id, `${label}.debt_id`),
      amount_cents: cents(record.amount_cents, `${label}.amount_cents`, {
        required: true,
      }),
      auto_allocation_blocked: booleanInteger(
        record.auto_allocation_blocked,
        `${label}.auto_allocation_blocked`,
      ),
      created_at: timestamp(record.created_at, `${label}.created_at`, {
        required: true,
      }),
      note: text(record.note, `${label}.note`),
      payment_on: date(record.payment_on, `${label}.payment_on`, {
        required: true,
      }),
      principal_effect: text(record.principal_effect, `${label}.principal_effect`, {
        required: true,
        nullable: false,
      }),
    };
  });
}

function normalizeAllocations(records, paymentByID, plannedByID) {
  uniqueIDs(records, "payment_allocations.json");
  return records.map((raw, index) => {
    const record = object(raw, `payment_allocations[${index}]`);
    const label = `payment_allocations[${index}]`;
    const paymentID = id(record.payment_id, `${label}.payment_id`);
    const plannedPaymentID = id(
      record.planned_payment_id,
      `${label}.planned_payment_id`,
    );
    const payment = paymentByID.get(paymentID);
    const plannedPayment = plannedByID.get(plannedPaymentID);
    if (!payment) {
      fail(`${label}.payment_id does not reference payments.json.`);
    }
    if (!plannedPayment) {
      fail(`${label}.planned_payment_id does not reference planned_payments.json.`);
    }
    if (payment.debt_id !== plannedPayment.debt_id) {
      fail(`${label} links records from different debts.`);
    }
    return {
      id: id(record.id, `${label}.id`),
      payment_id: paymentID,
      planned_payment_id: plannedPaymentID,
      allocated_at: timestamp(record.allocated_at, `${label}.allocated_at`, {
        required: true,
      }),
      allocation_method: text(
        record.allocation_method,
        `${label}.allocation_method`,
        { required: true, nullable: false },
      ),
      amount_cents: cents(record.amount_cents, `${label}.amount_cents`, {
        required: true,
      }),
    };
  });
}

function normalizeEvents(records) {
  uniqueIDs(records, "debt_events.json");
  return records.map((raw, index) => {
    const record = object(raw, `debt_events[${index}]`);
    const label = `debt_events[${index}]`;
    return {
      id: id(record.id, `${label}.id`),
      debt_id: id(record.debt_id, `${label}.debt_id`),
      amount_cents: cents(record.amount_cents, `${label}.amount_cents`, {
        required: true,
      }),
      content: text(record.content, `${label}.content`, {
        required: true,
        nullable: false,
      }),
      created_at: timestamp(record.created_at, `${label}.created_at`, {
        required: true,
      }),
      event_type: text(record.event_type, `${label}.event_type`, {
        required: true,
        nullable: false,
      }),
      occurred_at: timestamp(record.occurred_at, `${label}.occurred_at`, {
        required: true,
      }),
      title: text(record.title, `${label}.title`, {
        required: true,
        nullable: false,
      }),
      updated_at: timestamp(record.updated_at, `${label}.updated_at`, {
        required: true,
      }),
    };
  });
}

function verifyDebtLinks({ debtIDs, plans, plannedPayments, payments, events }) {
  for (const [index, plan] of plans.entries()) {
    if (!debtIDs.has(plan.debt_id)) {
      fail(`repayment_plans[${index}].debt_id does not reference a debt.`);
    }
  }
  for (const [index, payment] of payments.entries()) {
    if (!debtIDs.has(payment.debt_id)) {
      fail(`payments[${index}].debt_id does not reference a debt.`);
    }
  }
  for (const [index, event] of events.entries()) {
    if (!debtIDs.has(event.debt_id)) {
      fail(`debt_events[${index}].debt_id does not reference a debt.`);
    }
  }
  for (const payment of plannedPayments) {
    if (!debtIDs.has(payment.debt_id)) {
      fail(`planned payment ${payment.id} resolves to a missing debt.`);
    }
  }
}

export function loadV4Snapshot(inputDirectory) {
  const { directory, files } = readSource(inputDirectory);
  const { manifest, counts, digests } = validateManifest(files);
  const apiDebts = array(files.get("debts-api.json").value, "debts-api.json");
  const rawDebts = array(files.get("debts.json").value, "debts.json");
  const rows = normalizeDebtRows(apiDebts, rawDebts);
  const plans = normalizePlans(
    array(files.get("repayment_plans.json").value, "repayment_plans.json"),
  );
  const planByID = new Map(plans.map((plan) => [plan.id, plan]));
  const plannedPayments = normalizePlannedPayments(
    array(files.get("planned_payments.json").value, "planned_payments.json"),
    planByID,
  );
  const payments = normalizePayments(
    array(files.get("payments.json").value, "payments.json"),
  );
  const paymentByID = new Map(payments.map((payment) => [payment.id, payment]));
  const plannedByID = new Map(
    plannedPayments.map((payment) => [payment.id, payment]),
  );
  const allocations = normalizeAllocations(
    array(
      files.get("payment_allocations.json").value,
      "payment_allocations.json",
    ),
    paymentByID,
    plannedByID,
  );
  const events = normalizeEvents(
    array(files.get("debt_events.json").value, "debt_events.json"),
  );
  const debtIDs = new Set(rows.map((row) => row.row_id));
  verifyDebtLinks({
    debtIDs,
    plans,
    plannedPayments,
    payments,
    events,
  });
  const normalizedCounts = {
    debts: rows.length,
    repayment_plans: plans.length,
    planned_payments: plannedPayments.length,
    payments: payments.length,
    payment_allocations: allocations.length,
    debt_events: events.length,
  };
  if (
    normalizedCounts.debts !== counts["debts-api.json"] ||
    normalizedCounts.debts !== counts["debts.json"] ||
    normalizedCounts.repayment_plans !== counts["repayment_plans.json"] ||
    normalizedCounts.planned_payments !== counts["planned_payments.json"] ||
    normalizedCounts.payments !== counts["payments.json"] ||
    normalizedCounts.payment_allocations !== counts["payment_allocations.json"] ||
    normalizedCounts.debt_events !== counts["debt_events.json"]
  ) {
    fail("Normalized snapshot counts do not match the manifest.");
  }
  const digestPayload = {
    manifest_format_version: manifest.format_version,
    source_instance: manifest.source_instance,
    source_date: manifest.source_date,
    exported_at: manifest.exported_at,
    counts,
    sha256: Object.fromEntries(
      Object.entries(digests).sort(([left], [right]) => left.localeCompare(right)),
    ),
  };
  return {
    directory,
    manifest,
    counts: normalizedCounts,
    manifestCounts: counts,
    digests,
    snapshotDigest: sha256(digestPayload),
    rows,
    plans,
    plannedPayments,
    payments,
    allocations,
    events,
  };
}

function guardReplace(store, replaceUnmodified) {
  if (!store.initialized()) {
    return { existing: false, operations: 0, contexts: 0 };
  }
  const operations = Number(
    store.db.prepare("SELECT COUNT(*) AS count FROM operations").get().count,
  );
  const contexts = Number(
    store.db.prepare("SELECT COUNT(*) AS count FROM contexts").get().count,
  );
  if (!replaceUnmodified) {
    throw new TableError(
      "database_not_empty",
      "The database already contains a workbook; use --replace-unmodified.",
    );
  }
  if (operations !== 0 || contexts !== 0) {
    throw new TableError(
      "import_history_present",
      "Refusing to replace a table with saved operations or contexts.",
      { operations, contexts },
    );
  }
  return { existing: true, operations, contexts };
}

function backupPath(databasePath) {
  const date = new Date();
  const pad = (value) => String(value).padStart(2, "0");
  const stamp =
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}-` +
    `${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`;
  const base = `${databasePath}.pre-v4-import-${stamp}`;
  if (!existsSync(base)) {
    return base;
  }
  let suffix = 1;
  while (existsSync(`${base}-${suffix}`)) {
    suffix += 1;
  }
  return `${base}-${suffix}`;
}

function backupDatabase(store) {
  const destination = backupPath(store.databasePath);
  try {
    store.db.prepare("VACUUM INTO ?").run(destination);
  } catch (error) {
    throw new TableError(
      "import_backup_failed",
      "The pre-import database backup could not be created.",
      { cause: error instanceof Error ? error.message : String(error) },
    );
  }
  if (!existsSync(destination) || statSync(destination).size < 1) {
    throw new TableError(
      "import_backup_failed",
      "The pre-import database backup is empty.",
    );
  }
  return destination;
}

export function inspectAndBackupV4Database(
  databasePath,
  { replaceUnmodified = false } = {},
) {
  const path = resolve(databasePath);
  if (!existsSync(path)) {
    return { existing: false, operations: 0, contexts: 0, backup: null };
  }
  let db;
  try {
    db = new DatabaseSync(path);
    db.exec("PRAGMA query_only = ON");
    const tableCount = Number(
      db
        .prepare(
          `SELECT COUNT(*) AS count
             FROM sqlite_master
            WHERE type = 'table' AND name IN ('tables', 'workbook_meta')`,
        )
        .get().count,
    );
    const hasWorkbook = tableCount === 2;
    if (!hasWorkbook) {
      return { existing: false, operations: 0, contexts: 0, backup: null };
    }
    const workbook = db
      .prepare("SELECT singleton FROM workbook_meta WHERE singleton = 1")
      .get();
    if (!workbook) {
      return { existing: false, operations: 0, contexts: 0, backup: null };
    }
    const operations = Number(
      db.prepare("SELECT COUNT(*) AS count FROM operations").get().count,
    );
    const contexts = Number(
      db.prepare("SELECT COUNT(*) AS count FROM contexts").get().count,
    );
    if (!replaceUnmodified) {
      throw new TableError(
        "database_not_empty",
        "The database already contains a workbook; use --replace-unmodified.",
      );
    }
    if (operations !== 0 || contexts !== 0) {
      throw new TableError(
        "import_history_present",
        "Refusing to replace a table with saved operations or contexts.",
        { operations, contexts },
      );
    }
    const destination = backupPath(path);
    db.exec("PRAGMA query_only = OFF");
    db.prepare("VACUUM INTO ?").run(destination);
    db.exec("PRAGMA query_only = ON");
    if (!existsSync(destination) || statSync(destination).size < 1) {
      throw new TableError(
        "import_backup_failed",
        "The pre-import database backup is empty.",
      );
    }
    return { existing: true, operations, contexts, backup: destination };
  } catch (error) {
    if (error instanceof TableError) {
      throw error;
    }
    throw new TableError(
      "import_backup_failed",
      "The pre-import database backup could not be created.",
      { cause: error instanceof Error ? error.message : String(error) },
    );
  } finally {
    db?.close();
  }
}

function bindable(value) {
  return value === undefined ? null : value;
}

function insertRecords(db, table, columns, records) {
  const statement = db.prepare(
    `INSERT INTO ${table} (${columns.join(", ")})
     VALUES (${columns.map(() => "?").join(", ")})`,
  );
  for (const record of records) {
    statement.run(...columns.map((column) => bindable(record[column])));
  }
}

function ensureImportedTable(store, importedAt) {
  const workbook = store.db
    .prepare(
      `SELECT workbook_id, name, timezone, created_at
         FROM workbook_meta
        WHERE singleton = 1`,
    )
    .get();
  if (!workbook) {
    const workbookID = randomUUID();
    store.db
      .prepare(
        `INSERT INTO workbook_meta
           (singleton, workbook_id, format_version, name, timezone, created_at)
         VALUES (1, ?, ?, ?, ?, ?)`,
      )
      .run(
        workbookID,
        FORMAT_VERSION,
        "扶摇 v4 快照",
        "Asia/Shanghai",
        importedAt,
      );
  } else {
    store.db
      .prepare(
        `UPDATE workbook_meta
            SET format_version = ?, name = ?, timezone = ?
          WHERE singleton = 1`,
      )
      .run(FORMAT_VERSION, "扶摇 v4 快照", "Asia/Shanghai");
  }
  const table = store.db
    .prepare("SELECT table_id FROM tables WHERE table_id = ?")
    .get(DEFAULT_TABLE_ID);
  const previousTable = store.db
    .prepare("SELECT data_revision FROM tables WHERE table_id = ?")
    .get(DEFAULT_TABLE_ID);
  const dataRevision = previousTable ? previousTable.data_revision + 1 : 1;
  if (table) {
    store.db
      .prepare(
        `UPDATE tables
            SET name = ?, schema_revision = ?, data_revision = ?
          WHERE table_id = ?`,
      )
      .run("扶摇负债", TABLE_SCHEMA_REVISION, dataRevision, DEFAULT_TABLE_ID);
  } else {
    store.db
      .prepare(
        `INSERT INTO tables
           (table_id, name, schema_revision, data_revision)
         VALUES (?, ?, ?, ?)`,
      )
      .run(DEFAULT_TABLE_ID, "扶摇负债", TABLE_SCHEMA_REVISION, dataRevision);
  }
  store.db.prepare("DELETE FROM columns WHERE table_id = ?").run(DEFAULT_TABLE_ID);
  const insertColumn = store.db.prepare(
    `INSERT INTO columns
       (column_id, table_id, label, type, required, config, position, hidden)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
  );
  columnDefinitions.forEach((column, position) => {
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
  return dataRevision;
}

function replaceSnapshot(store, snapshot) {
  const importedAt = new Date().toISOString();
  const dataRevision = ensureImportedTable(store, importedAt);
  store.db.prepare("DELETE FROM rows").run();
  store.db.prepare("DELETE FROM fuyao_import_meta").run();

  const insertRow = store.db.prepare(
    `INSERT INTO rows
       (row_id, table_id, source_record_id, revision, values_json,
        created_at, updated_at, deleted_at)
     VALUES (?, ?, ?, 1, ?, ?, ?, NULL)`,
  );
  for (const row of snapshot.rows) {
    insertRow.run(
      row.row_id,
      DEFAULT_TABLE_ID,
      row.source_record_id,
      canonicalJSON(row.values),
      importedAt,
      importedAt,
    );
  }
  insertRecords(
    store.db,
    "fuyao_repayment_plans",
    PLAN_COLUMNS,
    snapshot.plans,
  );
  insertRecords(
    store.db,
    "fuyao_planned_payments",
    PLANNED_PAYMENT_COLUMNS,
    snapshot.plannedPayments,
  );
  insertRecords(store.db, "fuyao_payments", PAYMENT_COLUMNS, snapshot.payments);
  insertRecords(
    store.db,
    "fuyao_payment_allocations",
    ALLOCATION_COLUMNS,
    snapshot.allocations,
  );
  insertRecords(store.db, "fuyao_debt_events", EVENT_COLUMNS, snapshot.events);
  store.db
    .prepare(
      `INSERT INTO fuyao_import_meta
         (singleton, source_instance, source_date, exported_at,
          manifest_format_version, import_format_version, debt_count,
          repayment_plan_count, planned_payment_count, payment_count,
          payment_allocation_count, debt_event_count, counts_json,
          digests_json, snapshot_digest, imported_at)
       VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    )
    .run(
      snapshot.manifest.source_instance,
      snapshot.manifest.source_date,
      snapshot.manifest.exported_at,
      snapshot.manifest.format_version,
      V4_IMPORT_FORMAT_VERSION,
      snapshot.counts.debts,
      snapshot.counts.repayment_plans,
      snapshot.counts.planned_payments,
      snapshot.counts.payments,
      snapshot.counts.payment_allocations,
      snapshot.counts.debt_events,
      canonicalJSON(snapshot.counts),
      canonicalJSON(snapshot.digests),
      snapshot.snapshotDigest,
      importedAt,
    );

  const actualCounts = {
    debts: Number(
      store.db
        .prepare(
          `SELECT COUNT(*) AS count
             FROM rows
            WHERE table_id = ? AND deleted_at IS NULL`,
        )
        .get(DEFAULT_TABLE_ID).count,
    ),
    repayment_plans: Number(
      store.db
        .prepare("SELECT COUNT(*) AS count FROM fuyao_repayment_plans")
        .get().count,
    ),
    planned_payments: Number(
      store.db
        .prepare("SELECT COUNT(*) AS count FROM fuyao_planned_payments")
        .get().count,
    ),
    payments: Number(
      store.db.prepare("SELECT COUNT(*) AS count FROM fuyao_payments").get()
        .count,
    ),
    payment_allocations: Number(
      store.db
        .prepare("SELECT COUNT(*) AS count FROM fuyao_payment_allocations")
        .get().count,
    ),
    debt_events: Number(
      store.db.prepare("SELECT COUNT(*) AS count FROM fuyao_debt_events").get()
        .count,
    ),
  };
  if (canonicalJSON(actualCounts) !== canonicalJSON(snapshot.counts)) {
    fail("Database counts do not match the validated snapshot.", {
      expected: snapshot.counts,
      actual: actualCounts,
    });
  }
  const foreignKeyErrors = store.db.prepare("PRAGMA foreign_key_check").all();
  if (foreignKeyErrors.length > 0) {
    fail("Imported snapshot has foreign-key violations.", {
      violations: foreignKeyErrors.length,
    });
  }
  const sourceKeys = store.db
    .prepare(
      `SELECT COUNT(DISTINCT source_record_id) AS count
         FROM rows
        WHERE table_id = ? AND source_record_id IS NOT NULL`,
    )
    .get(DEFAULT_TABLE_ID);
  if (Number(sourceKeys.count) !== snapshot.counts.debts) {
    fail("Imported debt source keys are not unique.");
  }
  return { actualCounts, importedAt, dataRevision };
}

export function importV4Snapshot(
  store,
  {
    inputDirectory,
    replaceUnmodified = false,
    snapshot: providedSnapshot = null,
    backupBeforeReplace = true,
    preparedBackup = null,
  } = {},
) {
  const snapshot = providedSnapshot ?? loadV4Snapshot(inputDirectory);
  const initialGuard = guardReplace(store, replaceUnmodified);
  const backup =
    preparedBackup ??
    (backupBeforeReplace && initialGuard.existing && replaceUnmodified
      ? backupDatabase(store)
      : null);
  return store.immediate(() => {
    guardReplace(store, replaceUnmodified);
    const result = replaceSnapshot(store, snapshot);
    return {
      ok: true,
      replaced: initialGuard.existing,
      backup,
      source_directory: snapshot.directory,
      source_instance: snapshot.manifest.source_instance,
      source_date: snapshot.manifest.source_date,
      snapshot_digest: snapshot.snapshotDigest,
      counts: result.actualCounts,
      data_revision: result.dataRevision,
      imported_at: result.importedAt,
    };
  });
}
