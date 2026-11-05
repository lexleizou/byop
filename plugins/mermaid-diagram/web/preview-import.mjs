// Host-initiated preview import (P28). The chat "Preview diagram" button queues
// the latest { requestId, source } in host memory. This consumer pulls it with
// `plugin.getMermaidPreview` (idempotent; the host does not clear it on read),
// re-pulls when a `byop.mermaid-preview` event announces a new requestId, and
// deduplicates by requestId so a repeated request never overwrites a draft the
// user has edited. A read that resolves after a newer read was issued is
// discarded, so events racing the initial read cannot roll the page back to an
// older request. On a 0.20 host the method does not exist: the call rejects,
// the failure is swallowed, and the manual edit/render path is unchanged.
export function createMermaidPreviewConsumer({ call, apply, subscribe }) {
  let readSeq = 0;
  let appliedRequestId = null;

  async function pull() {
    const seq = ++readSeq;
    let result;
    try {
      result = await call("plugin.getMermaidPreview");
    } catch {
      return null; // method unavailable (old host) or call failed: manual path stays
    }
    if (seq !== readSeq) {
      return null; // a newer read was issued while this one was in flight
    }
    if (
      !result ||
      typeof result.requestId !== "string" ||
      result.requestId === "" ||
      typeof result.source !== "string"
    ) {
      return null; // no pending request (null) or malformed payload
    }
    if (result.requestId === appliedRequestId) {
      return null; // duplicate id: keep the draft the user may have edited
    }
    appliedRequestId = result.requestId;
    apply(result);
    return result;
  }

  function onEvent(payload) {
    if (payload && payload.requestId === appliedRequestId) {
      return Promise.resolve(null); // already applied: skip a pointless re-read
    }
    return pull();
  }

  function start() {
    if (typeof subscribe === "function") {
      subscribe(onEvent); // subscribe before the first read so no event is missed
    }
    return pull(); // the page must read actively; events before load can be lost
  }

  return { start, pull, onEvent };
}
