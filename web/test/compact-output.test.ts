import assert from "node:assert/strict";
import { test } from "node:test";
import { compactLiveOutput, splitCompactMessages } from "../src/components/transcript/compactOutput.ts";

const result = (id: string) => ({ id, role: "tool", parts: [{ type: "tool_result", id, name: "time", ok: true, content: "result" }] });
const summary = (id: string) => ({ id, role: "summary", parts: [{ type: "text", text: "summary" }], metadata: { compact: { source_message_ids: ["old"] } } });
const text = (id: string) => ({ id, role: "assistant", parts: [{ type: "text", text: id }] });
const overlay = (parts: any[]) => ({ turnID: "turn", sessionID: "session", status: "streaming", text: parts.filter(p => p.type === "text").map(p => p.text).join(""), parts });
const tool = (callID: string) => ({ type: "tool", callID, argsText: "{}", phase: "ok" });

test("summary cards remain between ordinary outputs in one canonical turn", () => {
  const messages = [text("before"), result("call-a"), summary("compact-a"), text("middle"), result("call-b"), summary("compact-b"), text("after")];
  assert.deepEqual(splitCompactMessages(messages).map(group => group.map(m => m.id)), [
    ["before", "call-a"], ["compact-a"], ["middle", "call-b"], ["compact-b"], ["after"],
  ]);
});

test("canonical compact prefix replaces streamed tools once while preserving new output", () => {
  const messages = [text("before"), result("call-a"), summary("compact-a"), result("call-b"), summary("compact-b")];
  const live = overlay([{ type: "text", text: "before" }, tool("call-a"), tool("call-b"), { type: "text", text: "NEXT_STEP" }, tool("call-c")]);
  const projection = compactLiveOutput(messages, live);
  assert.deepEqual(projection.messages, messages);
  assert.deepEqual(projection.overlay.parts, live.parts.slice(3));
  assert.equal(projection.overlay.text, "NEXT_STEP");
  assert.equal(live.parts.length, 5, "projection leaves the overlay unchanged");
});

test("reconnect retains canonical compact cards and only fresh streamed output", () => {
  const messages = [result("old-call"), summary("compact")];
  const live = overlay([{ type: "text", text: "resumed" }, tool("new-call")]);
  const projection = compactLiveOutput(messages, live);
  assert.deepEqual(projection.messages, messages);
  assert.equal(projection.overlay, live);
  assert.deepEqual(compactLiveOutput([], live), { messages: [], overlay: live }, "ordinary turns stay unchanged");
});
