import assert from "node:assert/strict";
import { test } from "node:test";

import { createSessionEventBatcher } from "../src/lib/sessionEventBatcher.ts";

const delta = (text: string, part: "text" | "thought" = "text", sessionID = "session") => ({
  kind: "turn.delta" as const, sessionID, turnID: "turn", part, delta: text,
});

test("continuous deltas flush on a fixed deadline without dropping content", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const received = [];
  const batch = createSessionEventBatcher((event) => received.push(event));
  batch.push(delta("一"));
  t.mock.timers.tick(30);
  batch.push(delta("二"));
  t.mock.timers.tick(19);
  assert.equal(received.length, 0);
  t.mock.timers.tick(1);
  assert.deepEqual(received, [delta("一二")]);
  batch.push(delta("三"));
  batch.flush(); // Disconnect/unmount must retain the last partial response.
  t.mock.timers.tick(100);
  assert.deepEqual(received, [delta("一二"), delta("三")]);
});

test("session, part, tool and steering boundaries retain event order", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const received = [];
  const batch = createSessionEventBatcher((event) => received.push(event));
  const events = [
    delta("思考", "thought"), delta("答案"), delta("另一个会话", "text", "other"),
    { ...delta("另一轮"), turnID: "other-turn" },
    { kind: "turn.tool" as const, sessionID: "session", turnID: "turn", callID: "call", phase: "running" as const },
    delta("调用后"),
    { kind: "input.steered" as const, sessionID: "session", turnID: "turn", seq: 2, clientMessageID: "guide", userMessageID: "guide", text: "引导" },
  ];
  events.forEach(batch.push);
  assert.deepEqual(received, events);
  t.mock.timers.tick(100);
  assert.deepEqual(received, events);
});

for (const kind of ["turn.completed", "turn.cancelled", "turn.failed"] as const) {
  test(`${kind} flushes the last text before canonical reconciliation`, (t) => {
    t.mock.timers.enable({ apis: ["setTimeout"] });
    const received = [];
    const batch = createSessionEventBatcher((event) => received.push(event));
    const terminal = { kind, sessionID: "session", turnID: "turn", seq: 3, assistantMessageID: "message", error: "failed" };
    batch.push(delta("最后"));
    batch.push(terminal);
    assert.deepEqual(received, [delta("最后"), terminal]);
    t.mock.timers.tick(100);
    assert.equal(received.length, 2);
  });
}
