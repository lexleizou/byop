import process from "node:process";
import readline from "node:readline";

// Model Release Labels is a declarative plugin: the host renders the badges from
// plugin.json. This entry exists because the manifest contract requires one.
// The plugin declares no commands and no activation events, so the host never
// starts this process; the handlers below only keep the contract honest.

const send = (message) => {
  process.stdout.write(`${JSON.stringify(message)}\n`);
};

readline.createInterface({ input: process.stdin }).on("line", (line) => {
  let message;
  try {
    message = JSON.parse(line);
  } catch {
    return;
  }
  if (!message || typeof message.method !== "string" || message.id === undefined) {
    return;
  }
  send({ jsonrpc: "2.0", id: message.id, result: {} });
});
