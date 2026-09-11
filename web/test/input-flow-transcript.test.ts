import assert from "node:assert/strict";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";
import React from "react";
import { renderToString } from "react-dom/server";

const server = await createServer({
  root: fileURLToPath(new URL("..", import.meta.url)),
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
});
after(() => server.close());
const { useOverlayStore } = await server.ssrLoadModule("/src/state/overlayStore.ts");
const { useTranscriptViewModel } = await server.ssrLoadModule("/src/components/transcript/useTranscriptViewModel.ts");
const { sessionEvent } = await server.ssrLoadModule("/contracts/events.ts");
const { UserInput } = await server.ssrLoadModule("/src/components/transcript/UserInput.tsx");
const { TooltipProvider } = await server.ssrLoadModule("/src/components/ui/tooltip.tsx");
const sessionID = "flow", turnID = "turn", clientMessageID = "input-flow-turn:question";
const form = { type: "form_result", title: "测试提问", schema: { type: "form", steps: [{ id: "dinner", type: "text_input", title: "晚饭？" }] }, result: { dinner: "肉饼" } };
const message = (id, role, parts, clientID = undefined) => ({ id, role, parts, sessionID, turnID, clientMessageID: clientID, createdAt: "2026-09-10T00:00:00Z" });
const initial = message("initial", "user", [{ type: "text", text: "测试提问" }], "initial");
const tool = message("first-output", "assistant", [{ type: "tool_use", id: "question", name: "builtin_request_user_input", args: {} }]);
const toolResult = message("result", "tool", [{ type: "tool_result", id: "question", name: "builtin_request_user_input", ok: true, content: '{}' }]);
const answer = message("answer", "user", [form], clientMessageID);
function apply(event) { useOverlayStore.getState().applyEvent(sessionEvent.parse({ sessionID, turnID, ...event })); }
function begin() {
  useOverlayStore.getState().clearSession(sessionID);
  apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial", text: "测试提问" });
  apply({ kind: "turn.tool", callID: "question", name: "builtin_request_user_input", phase: "ok", ok: true, content: '{}' });
  apply({ kind: "input.steered", seq: 2, clientMessageID, userMessageID: "answer", text: "已填写：肉饼", parts: [form] });
}
function view(messages, status = "running") {
  let result;
  function Probe() {
    const state = useOverlayStore.getState();
    result = useTranscriptViewModel({ sessionID, sessionRunning: status === "running", turns: [{ id: turnID, sessionID, clientMessageID: "initial", status, messages }], assistantOverlays: Object.values(state.assistants), pendingUsers: state.pendingUsers[sessionID] || [], turnPhase: state.turnPhases[sessionID], turnDurationByID: new Map() });
    return null;
  }
  renderToString(React.createElement(Probe));
  return result.turnVMs[0];
}
function bubbleHTML(user) {
  return renderToString(React.createElement(TooltipProvider, null, React.createElement(UserInput, { disclosureKey: "k", token: "", user })));
}
test("an answer without structured parts never renders the summary text as a bubble", () => {
  begin();
  assert.ok(bubbleHTML(view([initial, tool]).sequence[1].user).includes("肉饼"), "structured pending answer renders the card");
  useOverlayStore.getState().clearSession(sessionID);
  apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial", text: "测试提问" });
  apply({ kind: "turn.tool", callID: "question", name: "builtin_request_user_input", phase: "ok", ok: true, content: "{}" });
  // 事件没有 canonical user parts 时(快照尚未到达),overlay 只剩摘要文本:
  // 摘要气泡与 canonical 卡片形状不同,换成卡片时会跳变,所以不渲染这个临时气泡。
  apply({ kind: "input.steered", seq: 2, clientMessageID, userMessageID: "answer", text: "已填写：肉饼" });
  const pendingHTML = bubbleHTML(view([initial, tool]).sequence[1].user);
  assert.ok(!pendingHTML.includes("已填写"), "summary text must not become a temporary answer bubble");
  assert.ok(!pendingHTML.includes("pudding-user-message"), "no answer bubble until the structured result exists");
  const canonicalHTML = bubbleHTML(view([initial, tool, answer]).sequence[1].user);
  assert.ok(canonicalHTML.includes("肉饼"), "canonical answer keeps rendering");
});
test("an accepted form answer keeps the tool and structured bubble before the REST snapshot arrives", () => {
  begin();
  const vm = view([initial]);
  assert.equal(vm.sequence[0].kind, "assistant", "the previous tool must not disappear");
  assert.equal(vm.sequence[0].assistant.overlay.parts[0].name, "builtin_request_user_input");
  assert.deepEqual(vm.sequence[1].user.parts, [form], "never show summary text as a temporary answer bubble");
  assert.equal(useOverlayStore.getState().turnPhases[sessionID].activity, undefined, "answer is not generic steering activity");
});
test("the first output ID in a terminal event cannot retire a later live answer", () => {
  begin();
  const cached = [initial, tool, toolResult, answer];
  useOverlayStore.getState().reconcileMessages(sessionID, cached);
  apply({ kind: "turn.delta", part: "text", delta: "收到你的回答" });
  apply({ kind: "turn.completed", seq: 3, assistantMessageID: "first-output" });
  const vm = view(cached);
  assert.equal(vm.sequence.at(-1).assistant.canonicalReady, false, "running snapshot is not final even if it has the first output ID");
  useOverlayStore.getState().reconcileMessages(sessionID, cached);
  assert.equal(useOverlayStore.getState().assistants[turnID]?.text, "收到你的回答");
  const completed = [...cached, message("last-output", "assistant", [{ type: "text", text: "收到你的回答" }])];
  assert.equal(view(completed, "completed").sequence.at(-1).assistant.canonicalReady, true);
});
