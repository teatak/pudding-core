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

test("a compacted event preserves the running turn and its phase", () => {
  begin();
  const before = useOverlayStore.getState();
  const phase = before.turnPhases[sessionID];
  apply({ kind: "turn.compacted", seq: 3, assistantMessageID: "summary" });
  const afterCompact = useOverlayStore.getState();
  assert.equal(afterCompact.runningTurns[sessionID], turnID);
  assert.deepEqual(afterCompact.turnPhases[sessionID], phase);
  assert.equal(afterCompact.assistants[turnID].status, "streaming");
  assert.equal(afterCompact.lastEventSeqs[sessionID], 3);
  apply({ kind: "turn.completed", seq: 4, assistantMessageID: "final-answer" });
  assert.equal(useOverlayStore.getState().runningTurns[sessionID], undefined);
  assert.equal(useOverlayStore.getState().turnPhases[sessionID], undefined);
});

test("manual compaction completion waits for canonical without inventing a live assistant row", () => {
  const state = useOverlayStore.getState();
  state.clearSession(sessionID);
  state.startCompactRun(sessionID, "manual-compact");
  apply({ kind: "turn.completed", seq: 1, assistantMessageID: "summary" });
  const current = useOverlayStore.getState();
  assert.equal(current.assistants[turnID], undefined, "completion without streamed output must not insert an empty assistant above the compact card");
  assert.equal(current.compactRuns[sessionID]?.clientMessageID, "manual-compact");
  assert.equal(current.lastEventSeqs[sessionID], 1, "completion still advances the event sequence");
});

test("steering after compaction never attaches the old compact card to the new live segment", () => {
  useOverlayStore.getState().clearSession(sessionID);
  apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial", text: "测试提问" });
  apply({ kind: "turn.delta", part: "text", delta: "Earlier output" });
  apply({ kind: "turn.tool", callID: "question", name: "builtin_request_user_input", phase: "ok", ok: true, content: "{}" });
  apply({ kind: "turn.compacted", seq: 2, assistantMessageID: "compact" });
  const compact = { ...message("compact", "summary", [{ type: "text", text: "Task state" }]), metadata: { compact: { source_message_ids: ["older"] } } };
  apply({ kind: "input.steered", seq: 3, clientMessageID, userMessageID: "answer", text: "new direction", parts: [form] });
  apply({ kind: "turn.delta", part: "text", delta: "Following new direction" });
  for (const messages of [[initial, tool, toolResult, compact], [initial, tool, toolResult, compact, answer]]) {
    const vm = view(messages);
    const outputs = vm.sequence.filter(item => item.kind === "assistant").map(item => item.assistant);
    assert.equal(outputs.flatMap(output => output.messages || []).filter(m => m.id === "compact").length, 1);
    assert.equal(outputs.at(-1).kind, "live");
    assert.ok(!outputs.at(-1).messages?.some(m => m.id === "compact"));
    assert.equal(outputs.at(-1).overlay.text, "Following new direction");
  }
});

test("failed compaction keeps its row identity and stays before later conversation turns", () => {
  useOverlayStore.getState().clearSession(sessionID);
  const state = useOverlayStore.getState();
  state.startCompactRun(sessionID, "compact-attempt");
  const run = useOverlayStore.getState().compactRuns[sessionID];
  const later = { id: "later", sessionID, clientMessageID: "later", status: "completed", createdAt: new Date(Date.parse(run.startedAt) + 1000).toISOString(), messages: [{ ...initial, turnID: "later" }] };
  function read() {
    let result;
    function Probe() {
      result = useTranscriptViewModel({ sessionID, sessionRunning: false, turns: [later], compactRun: useOverlayStore.getState().compactRuns[sessionID], assistantOverlays: [], pendingUsers: [], turnDurationByID: new Map() });
      return null;
    }
    renderToString(React.createElement(Probe));
    return result.turnVMs;
  }
  const pending = read();
  state.failCompactRun(sessionID, "compact-attempt", { code: "compact_not_reduced", message: "Summary did not reduce context" });
  const failed = read();
  assert.equal(failed[0].key, pending[0].key);
  assert.deepEqual(failed[0].compact.error, { code: "compact_not_reduced", message: "Summary did not reduce context" });
  assert.equal(failed[1].turnID, "later");
  state.startCompactRun(sessionID, "retry");
  state.finishCompactRun(sessionID, "compact-attempt");
  state.failCompactRun(sessionID, "compact-attempt", { message: "late error" });
  assert.equal(useOverlayStore.getState().compactRuns[sessionID].clientMessageID, "retry");
  assert.equal(useOverlayStore.getState().compactRuns[sessionID].error, undefined);
  state.finishCompactRun(sessionID, "retry");
  assert.equal(useOverlayStore.getState().compactRuns[sessionID], undefined);
});
