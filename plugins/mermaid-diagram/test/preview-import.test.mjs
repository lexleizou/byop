import assert from "node:assert/strict";
import test from "node:test";

import { createMermaidPreviewConsumer } from "../web/preview-import.mjs";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

test("start subscribes before the first read, then applies the queued request", async () => {
  const order = [];
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => {
      order.push("read");
      return { requestId: "r1", source: "flowchart LR\n  A-->B" };
    },
    apply: (request) => applied.push(request),
    subscribe: () => order.push("subscribe"),
  });
  await consumer.start();
  assert.deepEqual(order, ["subscribe", "read"]);
  assert.deepEqual(applied, [{ requestId: "r1", source: "flowchart LR\n  A-->B" }]);
});

test("null (no pending request) is a silent no-op, valid requests still apply", async () => {
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => null,
    apply: (request) => applied.push(request),
  });
  await consumer.start();
  assert.deepEqual(applied, []);
});

test("malformed payloads are ignored and a later valid request still applies", async () => {
  const results = [
    { requestId: "", source: "x" }, // empty requestId
    { requestId: "r2" }, // source missing
    {}, // null-ish object
    { requestId: "r4", source: "sequenceDiagram" },
  ];
  let n = 0;
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => results[n++],
    apply: (request) => applied.push(request.requestId),
  });
  await consumer.pull();
  await consumer.pull();
  await consumer.pull();
  await consumer.pull();
  assert.deepEqual(applied, ["r4"]);
});

test("the same requestId never re-applies, protecting the user-edited draft", async () => {
  let n = 0;
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => {
      n += 1;
      return { requestId: "r1", source: `source #${n}` };
    },
    apply: (request) => applied.push(request.source),
  });
  await consumer.start();
  await consumer.pull(); // event-less re-read of the same request
  assert.deepEqual(applied, ["source #1"]);
});

test("an event with an already-applied requestId does not trigger a re-read", async () => {
  let calls = 0;
  const consumer = createMermaidPreviewConsumer({
    call: async () => {
      calls += 1;
      return { requestId: "r1", source: "A" };
    },
    apply: () => {},
  });
  await consumer.start();
  assert.equal(calls, 1);
  await consumer.onEvent({ requestId: "r1" });
  assert.equal(calls, 1);
});

test("an event with a new requestId re-reads and applies the latest value", async () => {
  const results = [
    { requestId: "r1", source: "first" },
    { requestId: "r2", source: "second" }, // host returns the latest request
  ];
  let n = 0;
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => results[n++],
    apply: (request) => applied.push(request.source),
  });
  await consumer.start();
  await consumer.onEvent({ requestId: "r2" });
  assert.deepEqual(applied, ["first", "second"]);
});

test("a late first read cannot roll back a newer request (latest wins)", async () => {
  const initial = deferred();
  const fromEvent = deferred();
  const calls = [initial.promise, fromEvent.promise];
  let n = 0;
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => calls[n++],
    apply: (request) => applied.push(request.requestId),
  });
  const initialRead = consumer.pull(); // page-init read still in flight
  const eventRead = consumer.onEvent({ requestId: "r2" }); // chat click queues r2
  fromEvent.resolve({ requestId: "r2", source: "B" });
  await eventRead;
  initial.resolve({ requestId: "r1", source: "A" }); // stale answer arrives late
  await initialRead;
  assert.deepEqual(applied, ["r2"]);
});

test("a rejected call (0.20 host without the method) is ignored silently", async () => {
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => {
      const error = new Error("unknown WebView method: plugin.getMermaidPreview");
      error.code = -32601;
      throw error;
    },
    apply: (request) => applied.push(request),
    subscribe: () => {},
  });
  await consumer.start(); // must not throw: manual edit/render path stays usable
  assert.deepEqual(applied, []);
});

test("a transient failure does not block a later successful import", async () => {
  let n = 0;
  const applied = [];
  const consumer = createMermaidPreviewConsumer({
    call: async () => {
      n += 1;
      if (n === 1) throw new Error("host busy");
      return { requestId: "r2", source: "flowchart TD" };
    },
    apply: (request) => applied.push(request.requestId),
  });
  await consumer.pull();
  await consumer.pull();
  assert.deepEqual(applied, ["r2"]);
});
