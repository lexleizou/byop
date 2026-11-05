import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SUPPORTED = new Map([
  ["en", { l10n: join(HERE, "l10n", "en.json"), sample: join(HERE, "samples", "en.txt") }],
  ["zh-Hans", { l10n: join(HERE, "l10n", "zh-Hans.json"), sample: join(HERE, "samples", "zh-Hans.txt") }],
]);
const localeCache = new Map();

const input = createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});

function send(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function localeMessages(locale) {
  const selected = SUPPORTED.has(locale) ? locale : "en";
  if (!localeCache.has(selected)) {
    const parsed = JSON.parse(readFileSync(SUPPORTED.get(selected).l10n, "utf8"));
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("locale package is invalid");
    }
    // The starter diagram lives in samples/<locale>.txt, not in the l10n
    // package: the host manifest loader rejects control characters in any
    // value, and multi-line sample text would invalidate the whole file.
    let sample = "";
    try {
      sample = readFileSync(SUPPORTED.get(selected).sample, "utf8").trimEnd();
    } catch {
      // A missing sample only leaves the editor empty.
    }
    localeCache.set(selected, { messages: parsed, sample });
  }
  const entry = localeCache.get(selected);
  return { locale: selected, messages: entry.messages, sample: entry.sample };
}

function handleRequest(request) {
  const response = { jsonrpc: "2.0", id: request.id };
  switch (request.method) {
    case "initialize":
    case "activate":
    case "deactivate":
      response.result = {};
      break;
    case "webviewMessage": {
      const message = request.params?.message;
      if (message?.action === "locale") {
        try {
          response.result = localeMessages(message.locale);
        } catch (error) {
          response.error = { code: -32000, message: String(error?.message || error) };
        }
      } else {
        response.error = {
          code: -32602,
          message: `Unsupported action: ${String(message?.action ?? "")}`,
        };
      }
      break;
    }
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
