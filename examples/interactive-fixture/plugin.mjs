import readline from "node:readline";

// Contract fixture for the P25 interactive contributions. It is a plain Node
// worker on the existing JSONL protocol: one command per declaration, no
// timers, no file or network access. Each command answers exactly what the
// host sends, so a walkthrough can tell which contribution was consumed.

const interface_ = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

const STATUS_ITEM_ID = "fixture.status";
let nextHostRequestID = 1;
const pendingHostCalls = new Map();

function send(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function requestHost(method, params = {}) {
  const id = `fixture-host-${nextHostRequestID++}`;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      pendingHostCalls.delete(id);
      reject(new Error(`${method} timed out`));
    }, 8000);
    pendingHostCalls.set(id, { resolve, reject, timeout });
    send({ jsonrpc: "2.0", id, method, params });
  });
}

async function publishStatus(hidden) {
  // host.setStatusItem takes the declared item id; an empty text needs hidden
  // so a plugin cannot keep a HUD slot it asked to release.
  await requestHost("host.setStatusItem", hidden
    ? { id: STATUS_ITEM_ID, text: "", hidden: true }
    : { id: STATUS_ITEM_ID, text: "QA fixture active", tone: "success" });
  return { message: hidden ? "Fixture status cleared" : "Fixture status published" };
}

function renderQaCard(args) {
  const language = typeof args?.language === "string" ? args.language : "";
  const code = typeof args?.code === "string" ? args.code : "";
  return {
    title: "QA card renderer",
    blocks: [
      { type: "heading", text: "QA CARD from interactive-fixture" },
      {
        type: "notice",
        tone: "success",
        text: `Language fence "${language}" rendered by the plugin, ${code.length} characters.`,
      },
      { type: "code", text: code },
      {
        type: "text",
        text: "This block replaces the plain fence, so the language renderer was consumed.",
      },
      {
        type: "button",
        label: "Publish status from QA card",
        command: "fixture.status.publish",
        args: {},
      },
    ],
  };
}

function renderImage(args) {
  const name = typeof args?.name === "string" ? args.name : "";
  const mimeType = typeof args?.mimeType === "string" ? args.mimeType : "";
  const size = Number.isFinite(args?.size) ? args.size : -1;
  const path = typeof args?.path === "string" ? args.path : "";
  return {
    title: "QA image renderer",
    blocks: [
      { type: "heading", text: "QA IMAGE from interactive-fixture" },
      {
        type: "keyValue",
        items: [
          { label: "Name", value: name },
          { label: "MIME type", value: mimeType },
          { label: "Size", value: String(size) },
          { label: "Path", value: path || "(not carried)" },
        ],
      },
      {
        type: "text",
        text: "This card replaces the plain chip, so the MIME renderer was consumed.",
      },
    ],
  };
}

function normalizeToken(query) {
  return String(query ?? "").trim().toLowerCase();
}

function mentionItems(query) {
  // Selection inserts text, so every item carries insertable text; the query is
  // whatever the user typed after the @ they chose to complete.
  const token = normalizeToken(query);
  const items = [
    { id: "qa-overview", label: "QA overview", text: "QA overview" },
    { id: "qa-checklist", label: "QA checklist", text: "QA checklist" },
  ];
  if (!token) {
    return items;
  }
  return items.filter((item) => item.label.toLowerCase().includes(token));
}

function paletteItems(query) {
  // Only commands this plugin declares are executable from the palette, and
  // selection is explicit, so nothing here runs while the user only types.
  const items = [
    {
      id: "palette-status-publish",
      label: "QA: publish fixture status",
      command: "fixture.status.publish",
      args: {},
    },
    {
      id: "palette-status-clear",
      label: "QA: clear fixture status",
      command: "fixture.status.clear",
      args: {},
    },
  ];
  const token = normalizeToken(query);
  if (!token) {
    return items;
  }
  return items.filter((item) => item.label.toLowerCase().includes(token));
}

async function invokeCommand(command, args) {
  switch (command) {
    case "fixture.status.publish":
      return publishStatus(false);
    case "fixture.status.clear":
      return publishStatus(true);
    case "fixture.render.qa-card":
      return renderQaCard(args);
    case "fixture.render.image":
      return renderImage(args);
    case "fixture.mention.query": {
      const items = mentionItems(args?.query);
      return { items };
    }
    case "fixture.palette.query": {
      const items = paletteItems(args?.query);
      return { items };
    }
    default:
      throw new Error(`Unknown command: ${command}`);
  }
}

async function handleMessage(message) {
  if (!message || typeof message !== "object") {
    return;
  }
  if (!message.method && Object.hasOwn(message, "id")) {
    const pending = pendingHostCalls.get(message.id);
    if (!pending) {
      return;
    }
    pendingHostCalls.delete(message.id);
    clearTimeout(pending.timeout);
    if (message.error) {
      pending.reject(new Error(message.error.message || "Host call failed"));
    } else {
      pending.resolve(message.result);
    }
    return;
  }
  if (typeof message.method !== "string") {
    return;
  }

  const response = { jsonrpc: "2.0", id: message.id };
  try {
    let result = {};
    switch (message.method) {
      case "initialize":
      case "activate":
      case "deactivate":
      case "settingsChanged":
        break;
      case "invokeCommand":
        result = await invokeCommand(message.params?.command, message.params?.args);
        break;
      default:
        response.error = {
          code: -32601,
          message: `Unknown method: ${message.method}`,
        };
    }
    if (response.id !== undefined) {
      response.result = result;
      send(response);
    }
  } catch (error) {
    if (response.id !== undefined) {
      response.error = { code: -32000, message: error.message };
      send(response);
    }
  }
}

interface_.on("line", (line) => {
  try {
    void handleMessage(JSON.parse(line));
  } catch {
    // Invalid lines cannot be correlated with a host request.
  }
});
