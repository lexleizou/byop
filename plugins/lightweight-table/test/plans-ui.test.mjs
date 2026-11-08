import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import test from "node:test";

import { PLUGIN_DIR } from "./support.mjs";

const appSource = await readFile(join(PLUGIN_DIR, "web", "app.js"), "utf8");
const dbSource = await readFile(join(PLUGIN_DIR, "src", "database.mjs"), "utf8");

function functionSource(source, name) {
  const start = source.indexOf(`function ${name}(`);
  if (start < 0) {
    return "";
  }
  const next = source.indexOf("\nfunction ", start + 1);
  return source.slice(start, next < 0 ? undefined : next);
}

test("P32 table.plans returns the contract structure", () => {
  const start = dbSource.indexOf("  plans(args = {}) {");
  const end = dbSource.indexOf("  seed(input", start);
  assert.ok(start >= 0 && end > start, "plans() method must exist before seed()");
  const plans = dbSource.slice(start, end);
  assert.match(plans, /assertPlainObject\(args, "table_plans", \["search", "filters"\]\)/);
  assert.match(plans, /status <> 'replaced'/);
  assert.match(plans, /ORDER BY effective_on DESC, created_at DESC, id DESC/);
  assert.match(plans, /row_id: row\.row_id/);
  assert.match(plans, /progress_pct:/);
  assert.match(plans, /totals: \{\s*count: plans\.length,\s*overdue_count:/);
  assert.match(plans, /overdue_amount:/);
});

test("P32 plugin forwards table.plans with the query scope", async () => {
  const plugin = await readFile(
    join(PLUGIN_DIR, "plugin.mjs"),
    "utf8",
  );
  assert.match(plugin, /case "table\.plans":/);
  assert.match(plugin, /tableStore\(\)\.plans\(\{\s*search: message\.search,\s*filters: message\.filters,\s*\}\)/);
});

test("renderTable preserves the scroll position across redraws", () => {
  const render = functionSource(appSource, "renderTable");
  assert.match(render, /const scroll = captureScroll\(dom\.tableScroll\)/);
  assert.match(render, /restoreScroll\(dom\.tableScroll, scroll\)/);
  assert.match(render, /canReuseTableRows\(rebaseDrafts, signature\)/);
  assert.match(render, /updateRowsIncrementally\(\)/);
  assert.match(render, /state\.renderedQuerySignature = signature/);
  assert.match(
    appSource,
    /dom\.bodyRows\.children\.length === \(state\.result\?\.rows\.length \?\? 0\)/,
  );
});

test("single click no longer steals keyboard focus from the scroll container", () => {
  const clickHandler = appSource.slice(
    appSource.indexOf('document.addEventListener("click"'),
  );
  assert.doesNotMatch(clickHandler, /cell\.focus\(/);
  assert.match(appSource, /dom\.tableScroll/);
});

test("captureScroll and restoreScroll round-trip the container offsets", () => {
  const source = [
    functionSource(appSource, "captureScroll"),
    functionSource(appSource, "restoreScroll"),
  ].join("\n");
  const api = new Function(
    "container",
    `${source}
    const saved = captureScroll(container);
    container.scrollTop = 0;
    container.scrollLeft = 0;
    restoreScroll(container, saved);
    return { top: container.scrollTop, left: container.scrollLeft };
  `,
  )({ scrollTop: 120, scrollLeft: 40 });
  assert.deepEqual(api, { top: 120, left: 40 });
});

test("loading state switches the body flag without blocking clicks", () => {
  const setLoading = functionSource(appSource, "setLoading");
  assert.match(setLoading, /document\.body\.dataset\.loading = "true"/);
  assert.match(setLoading, /delete document\.body\.dataset\.loading/);
  assert.match(setLoading, /dom\.loading\.hidden = !busy/);
});

test("plans view, detail sub-tabs and chat panel follow the P32 contract", () => {
  const switchView = functionSource(appSource, "switchView");
  const renderPlans = functionSource(appSource, "renderPlans");
  const buildPlanRow = functionSource(appSource, "buildPlanRow");
  const setDetailTab = functionSource(appSource, "setDetailTab");
  const renderPlanSubpage = functionSource(appSource, "renderPlanSubpage");
  const toggle = functionSource(appSource, "toggleChatPanel");
  const openConfirm = functionSource(appSource, "openDeleteConfirm");

  assert.match(switchView, /dom\.tabLedger\.setAttribute\("aria-selected"/);
  assert.match(switchView, /dom\.viewPlans\.hidden = next !== "plans"/);
  assert.match(switchView, /void loadPlans\(\)/);
  assert.match(renderPlans, /planColumn\.\$\{column\}/);
  assert.match(renderPlans, /dom\.plansEmpty\.hidden = hasRows/);
  assert.match(buildPlanRow, /bui-progress/);
  assert.match(buildPlanRow, /bui-progress-track/);
  assert.match(buildPlanRow, /bui-progress-fill/);
  assert.match(buildPlanRow, /bui-progress-label/);
  assert.match(buildPlanRow, /tr\.classList\.add\("overdue"\)/);
  assert.match(buildPlanRow, /detailButton\.dataset\.detailTab = "plan"/);
  assert.match(setDetailTab, /dom\.rowDetailPlanBody\.hidden = next !== "plan"/);
  assert.match(renderPlanSubpage, /tr\.classList\.add\("period-paid"\)/);
  assert.match(renderPlanSubpage, /tr\.dataset\.overdue = "true"/);
  assert.match(renderPlanSubpage, /badge\.dataset\.status = entry\.status/);
  assert.match(toggle, /host\.setChatPanel", \{ open: !pressed \}/);
  assert.match(toggle, /aria-pressed/);
  assert.match(toggle, /showToast\(message\("chatPanelError"\), "error", "chat-panel"\)/);
  assert.match(openConfirm, /state\.confirmRowID = rowID/);
  assert.match(appSource, /case "close-confirm-dialog"|action === "close-confirm-dialog"/);
  assert.match(appSource, /action === "confirm-delete-row"/);
  assert.match(
    appSource,
    /const mode = typeof theme === "string" \? theme : theme && theme\.mode;/,
    /document\.documentElement\.dataset\.mode = mode === "dark" \? "dark" : "light";/,
  );
  assert.match(appSource, /initialBridge\.onTheme/);
});

test("table.plans aggregates installments from the latest plan", async () => {
  const { seededStore } = await import("./support.mjs");
  const { store, directory } = await seededStore("plans-aggregate");
  try {
    const row = store.db
      .prepare(
        "SELECT row_id, values_json FROM rows WHERE deleted_at IS NULL LIMIT 1",
      )
      .get();
    const values = JSON.parse(row.values_json);
    assert.ok(values.name, "seeded row must have a name");
    const planID = "plan-test-1";
    store.db
      .prepare(
        `INSERT INTO fuyao_repayment_plans
           (id, debt_id, status, method, effective_on, first_due_on,
            installments, plan_principal_cents, confirmed_remaining_cents,
            original_rate_value, rate_period, created_at, updated_at,
            excluded_old_fee_cents, excluded_old_interest_cents,
            excluded_principal_cents, included_old_fee_cents,
            included_old_interest_cents, interest_basis,
            interest_principal_cents, new_fee_cents)
         VALUES (?, ?, 'active', 'equal_payment', '2026-01-01', '2026-02-01',
                 3, 120000, 60000, 0, 'monthly', '2026-01-01', '2026-01-01',
                 0, 0, 0, 0, 0, 'monthly', 0, 0)`,
      )
      .run(planID, row.row_id);
    const insert = store.db.prepare(
      `INSERT INTO fuyao_planned_payments
         (id, plan_id, debt_id, due_on, installment_no, principal_cents,
          interest_cents, old_interest_cents, old_fee_cents, new_fee_cents,
          total_cents, status, actual_paid_on)
       VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, 0, ?, ?, ?)`,
    );
    insert.run("p1", planID, row.row_id, "2020-01-01", 1, 10000, 10000, "paid", "2020-01-02");
    insert.run("p2", planID, row.row_id, "2020-02-01", 2, 10000, 20000, "overdue", null);
    insert.run("p3", planID, row.row_id, "2999-01-01", 3, 10000, 30000, "pending", null);
    const result = store.plans({});
    assert.equal(result.plans.length, 3);
    const plan = result.plans.find((entry) => entry.row_id === row.row_id);
    assert.ok(plan, "seeded debt must appear in plans");
    assert.equal(plan.plan_id, planID);
    assert.equal(plan.method, "equal_payment");
    assert.equal(plan.periods, 3);
    assert.equal(plan.paid, 1);
    assert.equal(plan.pending, 1);
    assert.equal(plan.overdue, 1);
    assert.equal(plan.next_due, "2999-01-01");
    assert.equal(plan.next_amount, "300.00");
    assert.equal(plan.progress_pct, 67);
    assert.deepEqual(result.totals, {
      count: 3,
      overdue_count: 1,
      overdue_amount: "200.00",
    });
  } finally {
    const { rmSync } = await import("node:fs");
    rmSync(directory, { recursive: true, force: true });
  }
});
