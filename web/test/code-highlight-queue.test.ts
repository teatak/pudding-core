import assert from "node:assert/strict";
import { test } from "node:test";

import { requestCodeHighlight } from "../src/lib/shiki.ts";

test("superseded code never occupies the worker queue or overwrites current content", (t) => {
  let worker;
  class FakeWorker {
    sent = [];
    onmessage;
    onerror;
    terminated = false;
    constructor() { worker = this; }
    postMessage(message) { this.sent.push(message); }
    terminate() { this.terminated = true; }
    reply(id, html) { this.onmessage({ data: { id, html } }); }
  }
  const original = globalThis.Worker;
  globalThis.Worker = FakeWorker;
  t.after(() => { globalThis.Worker = original; });
  const received = [];
  const cancelFirst = requestCodeHighlight("old", "ts", (html) => received.push(html));
  const first = worker.sent[0];
  const cancelQueued = requestCodeHighlight("intermediate", "ts", (html) => received.push(html));
  cancelFirst();
  cancelQueued();
  requestCodeHighlight("current", "ts", (html) => received.push(html));
  assert.equal(worker.sent.length, 1, "only one expensive job runs at a time");
  worker.reply(first.id, "old HTML");
  assert.deepEqual(received, [], "cancelled in-flight result is ignored");
  assert.equal(worker.sent.length, 2);
  assert.equal(worker.sent[1].code, "current", "intermediate version is never tokenized");
  worker.reply(worker.sent[1].id, "current HTML");
  assert.deepEqual(received, ["current HTML"]);

  t.mock.method(console, "error", () => {});
  requestCodeHighlight("failing", "ts", (html) => received.push(html));
  const failedWorker = worker;
  failedWorker.onerror({ message: "module load failed" });
  assert.ok(failedWorker.terminated);
  assert.equal(received.at(-1), null);
  requestCodeHighlight("next", "ts", (html) => received.push(html));
  assert.notEqual(worker, failedWorker, "later requests cannot wait on a dead worker");
  worker.reply(worker.sent[0].id, "next HTML");
  assert.equal(received.at(-1), "next HTML");
});
