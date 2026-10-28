export const LIGHTWEIGHT_TABLE_MCP_PREFIX = "mcp__lightweight_table__";

function unavailableSource() {
  return {
    version: 1,
    source_kind: "unavailable",
    reason: "source_unavailable",
  };
}

function isTableTool(toolName) {
  return typeof toolName === "string" && toolName.startsWith(LIGHTWEIGHT_TABLE_MCP_PREFIX);
}

function rootToolCallID(toolCallID) {
  return String(toolCallID).split("/", 1)[0];
}

function latestContextEdits(branch) {
  const edits = new Map();
  for (const entry of branch) {
    if (entry.type === "context_edit") {
      edits.set(entry.targetId, entry);
    }
  }
  return edits;
}

function isOmitted(entry, edits) {
  return edits.get(entry.id)?.replacement === null;
}

function isSuccessfulFinalAssistant(entry, edits) {
  if (
    entry.type !== "message" ||
    entry.message?.role !== "assistant" ||
    isOmitted(entry, edits)
  ) {
    return false;
  }
  const message = entry.message;
  const hasToolCall =
    Array.isArray(message.content) &&
    message.content.some(
      (block) => block?.type === "toolCall" || block?.type === "tool_call",
    );
  return !hasToolCall && ["stop", "length"].includes(message.stopReason);
}

export class SourceTracker {
  constructor() {
    this.sessionSerial = 0;
    this.runSerial = 0;
    this.turnSerial = 0;
    this.activeRun = null;
    this.rootSources = new Map();
  }

  sessionStarted() {
    this.sessionSerial += 1;
    this.runSerial = 0;
    this.turnSerial = 0;
    this.activeRun = null;
    this.rootSources.clear();
  }

  beforeAgentStart(event, ctx) {
    this.runSerial += 1;
    this.turnSerial = 0;
    this.rootSources.clear();
    const branch = ctx.sessionManager.getBranch();
    this.activeRun = {
      messageType: event.messageType,
      branchIDs: new Set(branch.map((entry) => entry.id)),
      runSerial: this.runSerial,
    };
  }

  turnStarted() {
    this.turnSerial += 1;
    this.rootSources.clear();
  }

  agentEnd() {
    // Pi may automatically retry after agent_end. Only agent_settled ends the source.
  }

  agentSettled() {
    this.activeRun = null;
    this.rootSources.clear();
  }

  sessionShutdown() {
    this.activeRun = null;
    this.rootSources.clear();
  }

  toolCall(event, ctx) {
    if (!event?.toolCallId || !event.input || typeof event.input !== "object") {
      return;
    }
    const rootID = rootToolCallID(event.parentToolCallId ?? event.toolCallId);
    const key = this.rootKey(rootID);
    let source;
    if (event.parentToolCallId) {
      source = this.rootSources.get(key) ?? unavailableSource();
    } else {
      source = this.resolveCurrentSource(ctx);
      this.rootSources.set(key, source);
    }
    if (isTableTool(event.toolName)) {
      event.input._byop = { ...source };
    }
  }

  rootKey(rootID) {
    return [
      this.sessionSerial,
      this.activeRun?.runSerial ?? this.runSerial,
      this.turnSerial,
      rootID,
    ].join(":");
  }

  resolveCurrentSource(ctx) {
    if (!this.activeRun) {
      return unavailableSource();
    }
    const branch = ctx.sessionManager.getBranch();
    const edits = latestContextEdits(branch);
    let sourceStart = 0;
    for (let index = 0; index < branch.length; index += 1) {
      const entry = branch[index];
      if (
        !this.activeRun.branchIDs.has(entry.id) &&
        isSuccessfulFinalAssistant(entry, edits)
      ) {
        sourceStart = index + 1;
      }
    }
    const userEntries = branch.slice(sourceStart).filter(
      (entry) =>
        entry.type === "message" &&
        entry.message?.role === "user" &&
        !this.activeRun.branchIDs.has(entry.id),
    );
    if (userEntries.length === 0) {
      return unavailableSource();
    }
    const last = userEntries[userEntries.length - 1];
    if (isOmitted(last, edits)) {
      return unavailableSource();
    }
    const activeUsers = userEntries.filter((entry) => !isOmitted(entry, edits));
    return {
      version: 1,
      source_kind: "agent",
      session_id: ctx.sessionManager.getSessionId(),
      message_ids: activeUsers.map((entry) => entry.id),
      message_time: last.timestamp,
    };
  }
}
