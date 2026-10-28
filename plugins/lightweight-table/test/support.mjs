import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile, mkdir, mkdtemp, writeFile } from "node:fs/promises";
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

const SYNTHETIC_V4_FILES = [
  "debts-api.json",
  "debts.json",
  "repayment_plans.json",
  "planned_payments.json",
  "payments.json",
  "payment_allocations.json",
  "debt_events.json",
];

export function syntheticV4Records() {
  const firstDebtID = "11111111-1111-4111-8111-111111111111";
  const secondDebtID = "22222222-2222-4222-8222-222222222222";
  const planID = "33333333-3333-4333-8333-333333333333";
  const firstInstallmentID = "44444444-4444-4444-8444-444444444444";
  const secondInstallmentID = "55555555-5555-4555-8555-555555555555";
  const paymentID = "66666666-6666-4666-8666-666666666666";
  const allocationID = "77777777-7777-4777-8777-777777777777";
  const firstEventID = "88888888-8888-4888-8888-888888888888";
  const secondEventID = "99999999-9999-4999-8999-999999999999";
  const debtsAPI = [
    {
      actual_disbursed_cents: 99000,
      actual_paid_cents: 12000,
      borrowed_on: "2026-01-02",
      calculated_annualized_rate_percent: null,
      category: "financial",
      contract_principal_cents: 100000,
      contract_reference: "SYNTHETIC-REFERENCE-ONE",
      created_at: "2026-01-02T00:00:00.000Z",
      creditor: "Synthetic creditor one",
      disbursed_on: "2026-01-03",
      financial_source: "synthetic_source",
      id: firstDebtID,
      loan_code: "SYNTHETIC-LOAN-ONE",
      loan_sequence: 1,
      manual_annualized_rate_percent: "12.5",
      next_due: { amount_cents: 34567, due_on: "2026-11-15" },
      opening_principal_cents: 100000,
      original_rate_value: "1.5",
      plan_status: "active",
      planned_interest_cents: 5000,
      principal_balance_cents: 70000,
      product_group_id: "synthetic-group-one",
      product_name: "Synthetic product one",
      product_type: "installment",
      rate_period: "month",
      remaining_interest_cents: 2000,
      repayment_status: "repaying",
      updated_at: "2026-10-01T00:00:00.000Z",
    },
    {
      actual_disbursed_cents: null,
      actual_paid_cents: 0,
      borrowed_on: null,
      calculated_annualized_rate_percent: null,
      category: "friend",
      contract_principal_cents: 250000,
      contract_reference: null,
      created_at: "2026-02-04T00:00:00.000Z",
      creditor: "Synthetic creditor two",
      disbursed_on: null,
      financial_source: null,
      id: secondDebtID,
      loan_code: null,
      loan_sequence: null,
      manual_annualized_rate_percent: null,
      next_due: null,
      opening_principal_cents: 250000,
      original_rate_value: "0.8",
      plan_status: "not_started",
      planned_interest_cents: 10000,
      principal_balance_cents: 250000,
      product_group_id: null,
      product_name: "Synthetic product two",
      product_type: "lump_sum",
      rate_period: "month",
      remaining_interest_cents: 10000,
      repayment_status: "not_started",
      updated_at: "2026-10-02T00:00:00.000Z",
    },
  ];
  const debts = [
    {
      borrowed_on: "2026-01-02",
      calculated_annualized_rate_percent: null,
      category: "financial",
      contract_principal_cents: 100000,
      contract_reference: "SYNTHETIC-REFERENCE-ONE",
      created_at: "2026-01-02T00:00:00.000Z",
      creditor_name: "Synthetic creditor one",
      disbursed_amount_cents: 99000,
      disbursed_on: "2026-01-03",
      funding_source: "bank",
      id: firstDebtID,
      interest_basis: "synthetic_basis",
      loan_code: "SYNTHETIC-LOAN-ONE",
      loan_sequence: 1,
      manual_annualized_rate_percent: null,
      opening_principal_cents: 100000,
      original_rate_value: 1.5,
      product_group_id: "synthetic-group-one",
      product_name: "Synthetic product one",
      product_type: "installment",
      rate_period: "month",
      repayment_status: "repaying",
      updated_at: "2026-10-01T00:00:00.000Z",
    },
    {
      borrowed_on: null,
      calculated_annualized_rate_percent: null,
      category: "friend",
      contract_principal_cents: 250000,
      contract_reference: null,
      created_at: "2026-02-04T00:00:00.000Z",
      creditor_name: "Synthetic creditor two",
      disbursed_amount_cents: null,
      disbursed_on: null,
      funding_source: "family",
      id: secondDebtID,
      interest_basis: null,
      loan_code: null,
      loan_sequence: null,
      manual_annualized_rate_percent: null,
      opening_principal_cents: 250000,
      original_rate_value: 0.8,
      product_group_id: null,
      product_name: "Synthetic product two",
      product_type: "lump_sum",
      rate_period: "month",
      repayment_status: "not_started",
      updated_at: "2026-10-02T00:00:00.000Z",
    },
  ];
  return {
    "debts-api.json": debtsAPI,
    "debts.json": debts,
    "repayment_plans.json": [
      {
        basis_explanation: "Synthetic plan",
        confirmed_remaining_cents: null,
        created_at: "2026-03-01T00:00:00.000Z",
        debt_id: firstDebtID,
        effective_on: "2026-03-01",
        excluded_old_fee_cents: 0,
        excluded_old_interest_cents: 0,
        excluded_principal_cents: 0,
        first_due_on: "2026-04-01",
        id: planID,
        included_old_fee_cents: 0,
        included_old_interest_cents: 0,
        installments: 2,
        interest_basis: "synthetic_basis",
        interest_principal_cents: 100000,
        method: "equal_principal",
        new_fee_cents: 0,
        original_rate_value: 1.5,
        plan_principal_cents: 100000,
        rate_period: "month",
        source_note: null,
        status: "active",
        updated_at: "2026-03-01T00:00:00.000Z",
        verified_balance_at: null,
        verified_balance_cents: null,
      },
    ],
    "planned_payments.json": [
      {
        actual_paid_on: "2026-04-01",
        adjusted_at: null,
        due_on: "2026-04-01",
        id: firstInstallmentID,
        installment_no: 1,
        interest_cents: 1000,
        new_fee_cents: 0,
        note: "Synthetic paid installment",
        old_fee_cents: 0,
        old_interest_cents: 0,
        plan_id: planID,
        principal_cents: 50000,
        status: "paid",
        total_cents: 51000,
      },
      {
        actual_paid_on: null,
        adjusted_at: null,
        due_on: "2026-05-01",
        id: secondInstallmentID,
        installment_no: 2,
        interest_cents: 1000,
        new_fee_cents: 0,
        note: null,
        old_fee_cents: 0,
        old_interest_cents: 0,
        plan_id: planID,
        principal_cents: 50000,
        status: "pending",
        total_cents: 51000,
      },
    ],
    "payments.json": [
      {
        amount_cents: 51000,
        auto_allocation_blocked: false,
        created_at: "2026-04-01T00:00:00.000Z",
        debt_id: firstDebtID,
        id: paymentID,
        note: "Synthetic payment",
        payment_on: "2026-04-01",
        principal_effect: "principal_and_interest",
      },
    ],
    "payment_allocations.json": [
      {
        allocated_at: "2026-04-01T00:00:00.000Z",
        allocation_method: "manual",
        amount_cents: 51000,
        id: allocationID,
        payment_id: paymentID,
        planned_payment_id: firstInstallmentID,
      },
    ],
    "debt_events.json": [
      {
        amount_cents: 51000,
        content: "Synthetic payment event",
        created_at: "2026-04-01T00:00:00.000Z",
        debt_id: firstDebtID,
        event_type: "payment",
        id: firstEventID,
        occurred_at: "2026-04-01T00:00:00.000Z",
        title: "Synthetic event one",
        updated_at: "2026-04-01T00:00:00.000Z",
      },
      {
        amount_cents: 0,
        content: "Synthetic note event",
        created_at: "2026-04-02T00:00:00.000Z",
        debt_id: secondDebtID,
        event_type: "note",
        id: secondEventID,
        occurred_at: "2026-04-02T00:00:00.000Z",
        title: "Synthetic event two",
        updated_at: "2026-04-02T00:00:00.000Z",
      },
    ],
  };
}

export async function writeSyntheticV4Snapshot(directory, records = syntheticV4Records()) {
  await mkdir(directory, { recursive: true });
  const counts = {};
  const sha256 = {};
  for (const filename of SYNTHETIC_V4_FILES) {
    const contents = `${JSON.stringify(records[filename], null, 2)}\n`;
    await writeFile(join(directory, filename), contents, "utf8");
    counts[filename] = records[filename].length;
    sha256[filename] = createHash("sha256").update(contents).digest("hex");
  }
  const manifest = {
    counts,
    exported_at: "2026-10-29T00:00:00.000Z",
    format_version: 4,
    read_only: true,
    sha256,
    source_date: "2026-10-29",
    source_instance: "synthetic-v4-test-instance",
  };
  await writeFile(
    join(directory, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
    "utf8",
  );
  return { directory, manifest, records };
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

export function pageSource(
  commandID = "page-command-1",
  viewInstanceID = "view-instance-1",
) {
  return {
    version: 1,
    source_kind: "page",
    view_instance_id: viewInstanceID,
    client_command_id: commandID,
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
