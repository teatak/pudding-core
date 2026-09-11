import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const server = await createServer({
  root: fileURLToPath(new URL("..", import.meta.url)),
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
});
after(() => server.close());
const { mergeQueuedInputs } = await server.ssrLoadModule("/src/hooks/useQueuedInputs.ts");

const sessionID = "s";
const queued = (id) => ({ clientMessageID: id, sessionID, status: "queued", text: id, createdAt: "2026-09-11T00:00:00Z" });
const steering = (id) => ({ ...queued(id), status: "steering", turnID: "turn" });
const order = (inputs) => inputs.map((input) => input.clientMessageID);

// 模拟连续几帧:REST 权威列表 + overlay 项 -> 显示顺序。
function frame(previousOrder, entries, pending) {
  return mergeQueuedInputs(entries, pending, previousOrder);
}

test("steering a queued input keeps its slot instead of jumping to the end", () => {
  const first = frame([], [queued("1"), queued("2"), queued("3"), queued("4")], []);
  assert.deepEqual(order(first), ["1", "2", "3", "4"]);

  // 引导 1 后它已被 promoted,从 REST 权威列表消失,只剩 overlay 的 steering 项。
  const second = frame(order(first), [queued("2"), queued("3"), queued("4")], [steering("1")]);
  assert.deepEqual(order(second), ["1", "2", "3", "4"]);

  const third = frame(order(second), [queued("3"), queued("4")], [steering("1"), steering("2")]);
  assert.deepEqual(order(third), ["1", "2", "3", "4"]);
});

test("a delivered steer leaves the queue without moving the rest", () => {
  const first = frame([], [queued("1"), queued("2"), queued("3")], []);
  const second = frame(order(first), [queued("2"), queued("3")], [steering("1")]);
  const third = frame(order(second), [queued("2"), queued("3")], [{ ...queued("1"), status: "steered" }]);
  assert.deepEqual(order(third), ["2", "3"]);
});

test("an input that was never in the authoritative queue is appended", () => {
  const first = frame([], [queued("1")], []);
  assert.deepEqual(order(frame(order(first), [queued("1")], [steering("new")])), ["1", "new"]);
});
