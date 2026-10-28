import { Type } from "@earendil-works/pi-ai";
import { defineTool } from "@earendil-works/pi-coding-agent";

function unwrapMcpOutcome(outcome) {
  // Pi wraps the complete MCP CallToolResult in structuredContent.
  const callResult = outcome.result.structuredContent;
  return {
    isError: outcome.isError === true || callResult?.isError === true,
    payload: callResult?.structuredContent ?? callResult,
    callResult,
  };
}

const probe = defineTool({
  name: "lightweight_table_probe",
  label: "Lightweight table nested probe",
  description:
    "Test-only tool. Queries, updates, restores, and verifies one lightweight-table row through Pi's nested tool pipeline.",
  parameters: Type.Object({
    row_id: Type.String({ description: "Stable row ID." }),
    original_status: Type.String({ description: "Expected current repayment status." }),
    test_status: Type.String({ description: "Temporary test status." }),
    expected_revision: Type.Integer({ minimum: 1 }),
    expected_schema_revision: Type.Integer({ minimum: 1 }),
  }),
  async execute(_toolCallID, params, _signal, _onUpdate, ctx) {
    const before = await ctx.executeTool(
      "mcp__lightweight_table__table_query",
      {
        row_ids: [params.row_id],
        projection: ["repayment_status"],
      },
    );
    const beforeCall = unwrapMcpOutcome(before);
    const beforeRow = beforeCall.payload?.rows?.[0];
    if (
      beforeCall.isError ||
      beforeCall.payload?.matched_count !== 1 ||
      beforeRow?.revision !== params.expected_revision ||
      beforeRow?.values?.repayment_status !== params.original_status
    ) {
      return {
        content: [
          {
            type: "text",
            text: "Probe query did not match the expected row revision and status.",
          },
        ],
        details: { phase: "query" },
        isError: true,
      };
    }

    const changed = await ctx.executeTool(
      "mcp__lightweight_table__table_change",
      {
        action: "update",
        expected_schema_revision: params.expected_schema_revision,
        dry_run: false,
        targets: [
          {
            row_id: params.row_id,
            expected_revision: params.expected_revision,
          },
        ],
        values: { repayment_status: params.test_status },
      },
    );
    const changedCall = unwrapMcpOutcome(changed);
    if (changedCall.isError || changedCall.payload?.affected_count !== 1) {
      return {
        content: changed.result.content,
        details: { phase: "change", callResult: changedCall.callResult },
        isError: true,
      };
    }

    const restored = await ctx.executeTool(
      "mcp__lightweight_table__table_change",
      {
        action: "update",
        expected_schema_revision: params.expected_schema_revision,
        dry_run: false,
        targets: [
          {
            row_id: params.row_id,
            expected_revision: params.expected_revision + 1,
          },
        ],
        values: { repayment_status: params.original_status },
      },
    );
    const restoredCall = unwrapMcpOutcome(restored);
    if (restoredCall.isError || restoredCall.payload?.affected_count !== 1) {
      return {
        content: restored.result.content,
        details: { phase: "restore", callResult: restoredCall.callResult },
        isError: true,
      };
    }

    const after = await ctx.executeTool(
      "mcp__lightweight_table__table_query",
      {
        row_ids: [params.row_id],
        projection: ["repayment_status"],
      },
    );
    const afterCall = unwrapMcpOutcome(after);
    const expectedRevision = params.expected_revision + 2;
    const afterRow = afterCall.payload?.rows?.[0];
    const verified =
      !afterCall.isError &&
      afterCall.payload?.matched_count === 1 &&
      afterRow?.revision === expectedRevision &&
      afterRow?.values?.repayment_status === params.original_status;
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            row_id: params.row_id,
            revision: afterRow?.revision ?? null,
            restored: verified,
          }),
        },
      ],
      details: {
        changed_operation_id: changedCall.payload?.operation_id,
        restored_operation_id: restoredCall.payload?.operation_id,
        revision: afterRow?.revision ?? null,
      },
      isError: !verified,
    };
  },
});

export default function lightweightTableProbeExtension(pi) {
  pi.registerTool(probe);
}
