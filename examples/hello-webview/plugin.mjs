import readline from "node:readline";

const input = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

function send(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function handleRequest(request) {
  const response = { jsonrpc: "2.0", id: request.id };
  switch (request.method) {
    case "initialize":
    case "activate":
    case "deactivate":
      response.result = {};
      break;
    case "webviewMessage":
      response.result = {
        echo: request.params?.message,
        at: new Date().toISOString(),
      };
      break;
    default:
      response.error = {
        code: -32601,
        message: `Unknown method: ${request.method}`,
      };
  }
  send(response);
}

input.on("line", (line) => {
  try {
    const request = JSON.parse(line);
    if (typeof request.method === "string" && Object.hasOwn(request, "id")) {
      handleRequest(request);
    }
  } catch {
    // Invalid lines cannot be correlated with a host request.
  }
});
