import readline from "node:readline";

const interface_ = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

let settings = {
  greeting: "Hello",
  showDetails: true,
  tone: "calm",
};
let nextHostRequestID = 1;
const pendingHostCalls = new Map();

function send(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function requestHost(method, params = {}) {
  const id = `hello-host-${nextHostRequestID++}`;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      pendingHostCalls.delete(id);
      reject(new Error(`${method} timed out`));
    }, 8000);
    pendingHostCalls.set(id, { resolve, reject, timeout });
    send({ jsonrpc: "2.0", id, method, params });
  });
}

function toneForHost() {
  return settings.tone === "bright" ? "success" : "info";
}

async function renderView() {
  const [workspace, theme] = await Promise.all([
    requestHost("host.getWorkspace"),
    requestHost("host.getTheme"),
  ]);
  const items = [
    { label: "Workspace", value: workspace.name || workspace.root },
    { label: "Theme", value: theme.mode },
  ];
  if (settings.showDetails) {
    items.push({ label: "Tone", value: settings.tone });
  }
  return {
    title: `${settings.greeting} · Node`,
    blocks: [
      { type: "heading", text: `${settings.greeting} from a Node plugin` },
      {
        type: "text",
        text: "This view is a typed native model rendered by BYOP. The plugin only uses stdin, stdout, and declared host calls.",
      },
      { type: "keyValue", items },
      {
        type: "list",
        items: [
          {
            title: "Runtime",
            detail: "Node.js 25+ runs with the permission model enabled.",
          },
          {
            title: "Capabilities",
            detail: "Workspace and file reads are granted. Network and child processes stay disabled.",
          },
        ],
      },
      { type: "button", label: "Say hello", command: "hello.greet", args: {} },
      { type: "button", label: "Read README", command: "hello.readme", args: {} },
    ],
  };
}

async function invokeCommand(command) {
  if (command === "hello.greet") {
    const message = `${settings.greeting}, BYOP!`;
    await requestHost("host.showNotice", { text: message, tone: toneForHost() });
    await requestHost("host.updateTask", {
      id: "hello.tasks",
      detail: `Greeting completed with ${settings.tone} tone.`,
      status: "Completed",
      tone: toneForHost(),
    });
    return { message };
  }
  if (command === "hello.readme") {
    try {
      const file = await requestHost("host.readFile", { path: "README.md" });
      const firstLine = file.content.split(/\r?\n/, 1)[0] || "(empty README)";
      return { message: firstLine };
    } catch (error) {
      return { message: `Could not read README.md: ${error.message}` };
    }
  }
  if (command === "hello.fail") {
    throw new Error("Fixture failure for automation contract");
  }
  throw new Error(`Unknown command: ${command}`);
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
        settings = { ...settings, ...(message.params?.settings || {}) };
        break;
      case "activate":
      case "deactivate":
        break;
      case "invokeCommand":
        result = await invokeCommand(message.params?.command);
        break;
      case "renderView":
        result = await renderView();
        break;
      case "settingsChanged":
        settings = { ...settings, ...(message.params?.settings || {}) };
        await requestHost("host.refreshView", { view: "hello.panel" });
        return;
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
