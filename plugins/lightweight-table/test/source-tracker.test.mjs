import assert from "node:assert/strict";
import test from "node:test";

import { SourceTracker } from "../src/source-tracker.mjs";

function context(entries, sessionID = "session-a") {
  return {
    sessionManager: {
      getBranch: () => entries,
      getSessionId: () => sessionID,
    },
  };
}

function userEntry(id, timestamp = "2026-10-28T08:00:00.000Z") {
  return {
    id,
    type: "message",
    timestamp,
    message: { role: "user", content: "synthetic" },
  };
}

function assistantEntry(id, stopReason = "stop", content = []) {
  return {
    id,
    type: "message",
    timestamp: "2026-10-28T08:01:00.000Z",
    message: {
      role: "assistant",
      content,
      stopReason,
    },
  };
}

function contextEdit(id, targetId, replacement = null) {
  return {
    id,
    type: "context_edit",
    targetId,
    replacement,
  };
}

function directCall(id) {
  return {
    toolCallId: id,
    toolName: "mcp__lightweight_table__table_query",
    input: {},
  };
}

test("direct and nested calls inherit the delivered user entry", () => {
  const tracker = new SourceTracker();
  const base = { id: "old-user", type: "message", message: { role: "user" } };
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([base]));
  tracker.turnStarted();

  const direct = {
    toolCallId: "root-1",
    toolName: "mcp__lightweight_table__table_query",
    input: {},
  };
  tracker.toolCall(
    direct,
    context([base, userEntry("new-user")]),
  );
  assert.equal(direct.input._byop.source_kind, "agent");
  assert.deepEqual(direct.input._byop.message_ids, ["new-user"]);

  const customRoot = { toolCallId: "probe-1", toolName: "probe", input: {} };
  tracker.toolCall(customRoot, context([base, userEntry("new-user")]));
  const nested = {
    toolCallId: "probe-1/1",
    parentToolCallId: "probe-1",
    toolName: "mcp__lightweight_table__table_change",
    input: {},
  };
  tracker.toolCall(nested, context([base, userEntry("new-user")]));
  assert.deepEqual(nested.input._byop, direct.input._byop);
});

test("turn changes cannot reuse an old root tool call ID", () => {
  const tracker = new SourceTracker();
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();
  const entries = [userEntry("new-user")];
  tracker.toolCall(
    { toolCallId: "same-id", toolName: "probe", input: {} },
    context(entries),
  );
  tracker.turnStarted();
  const nested = {
    toolCallId: "same-id/1",
    parentToolCallId: "same-id",
    toolName: "mcp__lightweight_table__table_change",
    input: {},
  };
  tracker.toolCall(nested, context(entries));
  assert.equal(nested.input._byop.source_kind, "unavailable");
});

test("custom starts are unavailable, agent_end keeps source, settlement clears it", () => {
  const tracker = new SourceTracker();
  const entries = [userEntry("new-user")];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "custom" }, context(entries));
  tracker.turnStarted();
  const customCall = {
    toolCallId: "custom",
    toolName: "mcp__lightweight_table__table_change",
    input: {},
  };
  tracker.toolCall(customCall, context(entries));
  assert.equal(customCall.input._byop.source_kind, "unavailable");

  const entriesAfterLateUser = [...entries, userEntry("late-user")];
  const lateUserCall = {
    toolCallId: "late-user",
    toolName: "mcp__lightweight_table__table_query",
    input: {},
  };
  tracker.toolCall(lateUserCall, context(entriesAfterLateUser));
  assert.equal(lateUserCall.input._byop.source_kind, "agent");
  assert.deepEqual(lateUserCall.input._byop.message_ids, ["late-user"]);

  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();
  const first = {
    toolCallId: "user-1",
    toolName: "mcp__lightweight_table__table_query",
    input: {},
  };
  tracker.toolCall(first, context(entries));
  assert.equal(first.input._byop.source_kind, "agent");
  tracker.agentEnd();
  tracker.turnStarted();
  const retry = {
    toolCallId: "user-2",
    toolName: "mcp__lightweight_table__table_query",
    input: {},
  };
  tracker.toolCall(retry, context(entries));
  assert.equal(retry.input._byop.source_kind, "agent");
  tracker.agentSettled();
  tracker.turnStarted();
  const settled = {
    toolCallId: "user-3",
    toolName: "mcp__lightweight_table__table_query",
    input: {},
  };
  tracker.toolCall(settled, context(entries));
  assert.equal(settled.input._byop.source_kind, "unavailable");
});

test("a successful no-tool final closes the source for a following custom follow-up", () => {
  const tracker = new SourceTracker();
  const entries = [
    userEntry("user-a"),
    assistantEntry("final-a", "stop"),
  ];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();

  const followUp = directCall("follow-up");
  tracker.toolCall(followUp, context(entries));
  assert.equal(followUp.input._byop.source_kind, "unavailable");
});

test("error, aborted, and tool-use assistant messages do not close the source", () => {
  for (const stopReason of ["error", "aborted", "toolUse", "pending", "deferred"]) {
    const tracker = new SourceTracker();
    const entries = [
      userEntry("user-a"),
      assistantEntry(
        `assistant-${stopReason}`,
        stopReason,
        stopReason === "toolUse"
          ? [{ type: "toolCall", id: "call-1", name: "read", arguments: {} }]
          : [],
      ),
    ];
    tracker.sessionStarted();
    tracker.beforeAgentStart({ messageType: "user" }, context([]));
    tracker.turnStarted();

    const call = directCall(`call-${stopReason}`);
    tracker.toolCall(call, context(entries));
    assert.equal(
      call.input._byop.source_kind,
      "agent",
      `stopReason=${stopReason}`,
    );
  }
});

test("a real user delivered after the final starts a new source group", () => {
  const tracker = new SourceTracker();
  const entries = [
    userEntry("user-a"),
    assistantEntry("final-a", "stop"),
    userEntry("user-b"),
  ];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();

  const call = directCall("after-new-user");
  tracker.toolCall(call, context(entries));
  assert.equal(call.input._byop.source_kind, "agent");
  assert.deepEqual(call.input._byop.message_ids, ["user-b"]);
});

test("custom context entries do not split a delivered user group", () => {
  const tracker = new SourceTracker();
  const entries = [
    userEntry("user-a"),
    {
      id: "custom-context",
      type: "message",
      message: { role: "custom", content: "synthetic context" },
    },
    userEntry("user-b"),
  ];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();

  const call = directCall("same-user-group");
  tracker.toolCall(call, context(entries));
  assert.equal(call.input._byop.source_kind, "agent");
  assert.deepEqual(call.input._byop.message_ids, ["user-a", "user-b"]);
});

test("a context-edited user is unavailable and never falls back to an earlier user", () => {
  const tracker = new SourceTracker();
  const entries = [
    userEntry("user-a"),
    assistantEntry("final-a", "stop"),
    userEntry("user-b"),
    contextEdit("omit-user-b", "user-b", null),
  ];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();

  const call = directCall("omitted-user");
  tracker.toolCall(call, context(entries));
  assert.equal(call.input._byop.source_kind, "unavailable");
});

test("an omitted latest user does not fall back when no final boundary exists", () => {
  const tracker = new SourceTracker();
  const entries = [
    userEntry("user-a"),
    userEntry("user-b"),
    contextEdit("omit-user-b", "user-b", null),
  ];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();

  const call = directCall("omitted-latest-user");
  tracker.toolCall(call, context(entries));
  assert.equal(call.input._byop.source_kind, "unavailable");
});

test("an omitted final assistant does not create a source cutoff", () => {
  const tracker = new SourceTracker();
  const entries = [
    userEntry("user-a"),
    assistantEntry("failed-final", "stop"),
    contextEdit("omit-final", "failed-final", null),
  ];
  tracker.sessionStarted();
  tracker.beforeAgentStart({ messageType: "user" }, context([]));
  tracker.turnStarted();

  const call = directCall("after-omitted-final");
  tracker.toolCall(call, context(entries));
  assert.equal(call.input._byop.source_kind, "agent");
  assert.deepEqual(call.input._byop.message_ids, ["user-a"]);
});
