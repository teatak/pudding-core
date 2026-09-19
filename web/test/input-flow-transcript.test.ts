import assert from "node:assert/strict";
import { test } from "node:test";
import { createTestViteServer } from "./vite-test-server.ts";
import React from "react";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const server = await createTestViteServer();
const { useOverlayStore } = await server.ssrLoadModule("/src/state/overlayStore.ts");
const { useTranscriptViewModel } = await server.ssrLoadModule("/src/components/transcript/useTranscriptViewModel.ts");
const { sessionEvent } = await server.ssrLoadModule("/contracts/events.ts");
const { UserInput } = await server.ssrLoadModule("/src/components/transcript/UserInput.tsx");
const { TooltipProvider } = await server.ssrLoadModule("/src/components/ui/tooltip.tsx");
const { TranscriptTurn } = await server.ssrLoadModule("/src/components/transcript/TranscriptTurn.tsx");
const { isSessionTurnRunning } = await server.ssrLoadModule("/src/components/session-rail/activity.ts");
const { setLocale } = await server.ssrLoadModule("/src/i18n/index.ts");
setLocale("zh-CN");
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
function view(messages, status = "running", fields = {}) {
  return viewTurns([{ id: turnID, sessionID, clientMessageID: "initial", status, messages,
    createdAt: "2026-09-10T00:00:00Z", updatedAt: "2026-09-10T00:01:23Z", ...fields }], status === "running")[0];
}
function viewTurns(turns, sessionRunning = true) {
  let result;
  function Probe() {
    const state = useOverlayStore.getState();
    result = useTranscriptViewModel({ sessionID, sessionRunning, turns, assistantOverlays: Object.values(state.assistants), pendingUsers: state.pendingUsers[sessionID] || [], turnPhase: state.turnPhases[sessionID] });
    return null;
  }
  renderToString(React.createElement(Probe));
  return result.turnVMs;
}
function bubbleHTML(user) {
  return renderToString(React.createElement(TooltipProvider, null, React.createElement(UserInput, { disclosureKey: "k", token: "", user })));
}

for (const status of ["completed", "failed", "cancelled"]) {
  test(`late submit acknowledgement cannot revive a ${status} turn or its sidebar spinner`, () => {
    const store = useOverlayStore.getState();
    store.clearSession(sessionID);
    store.startSubmittingTurn(sessionID, "initial");
    apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial", text: "test" });
    apply({ kind: `turn.${status}`, seq: 2, assistantMessageID: "output", ...(status === "failed" ? { error: "Test failure" } : {}) });
    for (const reconciled of [false, true]) {
      if (reconciled) store.markAssistantRevealed(turnID);
      const before = useOverlayStore.getState();
      store.acceptSubmittingTurn(sessionID, "initial", turnID);
      const after = useOverlayStore.getState();
      assert.equal(after.assistants, before.assistants, "HTTP acknowledgement must not create/revive an assistant");
      assert.equal(after.turnPhases, before.turnPhases, "terminal phase must not regress to waiting");
      assert.equal(after.runningTurns, before.runningTurns, "terminal turn must not become running again");
      assert.equal(isSessionTurnRunning({ id: sessionID, running: false }, after.runningTurns, after.turnPhases), false);
    }
  });
}

test("HTTP acknowledgement only reconciles pending input, before and after streaming starts", () => {
  const store = useOverlayStore.getState();
  store.clearSession(sessionID);
  store.addPendingUser({ sessionID, clientMessageID: "initial", status: "submitting", text: "test", createdAt: initial.createdAt });
  store.startSubmittingTurn(sessionID, "initial");
  store.acceptSubmittingTurn(sessionID, "initial", turnID);
  assert.equal(useOverlayStore.getState().pendingUsers[sessionID][0].turnID, turnID);
  assert.equal(useOverlayStore.getState().assistants[turnID], undefined);
  assert.equal(useOverlayStore.getState().turnPhases[sessionID].phase, "submitting");
  apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial" });
  apply({ kind: "turn.delta", part: "thought", delta: "Thinking" });
  const before = useOverlayStore.getState();
  store.acceptSubmittingTurn(sessionID, "initial", turnID);
  assert.equal(useOverlayStore.getState().turnPhases, before.turnPhases, "acknowledgement cannot reset thinking to waiting");
  assert.equal(useOverlayStore.getState().assistants, before.assistants);
});

test("terminal rendering wins over stale phases and stale session running summaries", () => {
  const store = useOverlayStore.getState();
  store.clearSession(sessionID);
  apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial" });
  apply({ kind: "turn.failed", seq: 2, error: "Test failure" });
  // Reproduce the inconsistent phase previously written by a late HTTP ack.
  useOverlayStore.setState({ turnPhases: { [sessionID]: { sessionID, turnID, phase: "awaiting_model", updatedAt: initial.createdAt } } });
  const vm = viewTurns([], false)[0];
  assert.equal(vm.assistant.phase, undefined);
  const html = renderToString(React.createElement(TooltipProvider, null,
    React.createElement(TranscriptTurn, { sessionID, token: "", turn: vm })));
  assert.ok(html.includes("请求失败"));
  assert.ok(!html.includes("等待模型"));
  store.clearSession(sessionID);
  const completed = { id: turnID, sessionID, clientMessageID: "initial", status: "completed", createdAt: initial.createdAt, updatedAt: initial.createdAt, messages: [initial] };
  const settled = viewTurns([completed], true);
  assert.equal(settled.length, 1, "stale session.running cannot invent another waiting row");
  assert.equal(settled[0].assistant, undefined);
  const restored = viewTurns([{ ...completed, status: "running" }], true);
  assert.equal(restored.length, 1);
  assert.equal(restored[0].assistant.phase.turnID, turnID, "reconnect restores the actual running turn, not an unscoped row");
});

test("one fixed-height turn clock survives phase changes, steering and canonical reconciliation", () => {
  useOverlayStore.getState().clearSession(sessionID);
  apply({ kind: "turn.started", seq: 1, userMessageID: "initial", clientMessageID: "initial", text: "测试提问" });
  const waiting = viewTurns([])[0];
  assert.deepEqual(waiting.header, { status: "running" }, "reserve the row before the snapshot; do not fabricate a start time");
  const running = view([initial]);
  assert.equal(running.key, waiting.key);
  apply({ kind: "turn.tool", callID: "question", name: "builtin_request_user_input", phase: "running" });
  assert.deepEqual(view([initial]).header, running.header, "tool phases do not reset total duration");
  apply({ kind: "input.steered", seq: 2, clientMessageID, userMessageID: "answer", text: "已填写：肉饼", parts: [form] });
  assert.deepEqual(view([initial, tool, answer]).header, running.header, "answers belong to the original turn clock");
  apply({ kind: "turn.completed", seq: 3, assistantMessageID: "first-output" });
  const beforeRefetch = view([initial, tool, answer]);
  assert.equal(beforeRefetch.header.status, "completed", "terminal SSE stops ticking before final refetch");
  assert.equal(beforeRefetch.header.endedAt, undefined, "the old running snapshot is not a finish time");
  useOverlayStore.getState().markAssistantRevealed(turnID);
  const completed = view([initial, tool, answer], "completed", { model: "clock-test-model" });
  assert.deepEqual(completed.header, { ...running.header, status: "completed", endedAt: "2026-09-10T00:01:23Z" });
  assert.equal(completed.key, running.key);
  const html = renderToString(React.createElement(QueryClientProvider, { client: new QueryClient() },
    React.createElement(TooltipProvider, null,
      React.createElement(TranscriptTurn, { sessionID, token: "", turn: completed }))));
  assert.equal((html.match(/data-turn-header=/g) || []).length, 1, "one header for the whole guided turn");
  assert.match(html, /h-6[^>]*data-turn-header/);
  assert.equal((html.match(/用时 1分23秒/g) || []).length, 1, "no duplicate duration in bottom actions");
  assert.ok(html.indexOf("Clock Test Model") > html.indexOf("data-turn-duration"), "model stays in the bottom metadata");
});

test("cancelled/failed turns without assistant output retain the clock; compact-only markers do not gain a header", () => {
  for (const status of ["cancelled", "failed"]) {
    useOverlayStore.getState().clearSession(sessionID);
    const vm = view([initial], status);
    assert.equal(vm.assistant, undefined);
    assert.equal(vm.header.status, status);
    assert.equal(vm.header.endedAt, "2026-09-10T00:01:23Z");
    const html = renderToString(React.createElement(TooltipProvider, null,
      React.createElement(TranscriptTurn, { sessionID, token: "", turn: vm })));
    assert.match(html, /data-turn-header/);
    assert.match(html, status === "failed" ? /本轮在 1分23秒 后失败/ : /本轮在 1分23秒 后中止/);
    const interrupted = { ...message("partial", "assistant", [{ type: "text", text: "Partial output" }]), interrupted: true };
    const partialHTML = renderToString(React.createElement(QueryClientProvider, { client: new QueryClient() },
      React.createElement(TooltipProvider, null,
        React.createElement(TranscriptTurn, { sessionID, token: "", turn: view([initial, interrupted], status) }))));
    assert.ok(!partialHTML.includes("已中断"), "the old interrupted badge is replaced by the turn header");
  }
  const compact = { ...message("compact-only", "summary", [{ type: "text", text: "Summary" }]), metadata: { compact: {} } };
  assert.equal(view([compact], "completed").header, undefined);
});
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
      result = useTranscriptViewModel({ sessionID, sessionRunning: false, turns: [later], compactRun: useOverlayStore.getState().compactRuns[sessionID], assistantOverlays: [], pendingUsers: [] });
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

test("initial message when SSE skips turn.started still pairs user bubble with live assistant", () => {
  const store = useOverlayStore.getState();
  store.clearSession(sessionID);
  store.addPendingUser({ sessionID, clientMessageID: "initial", status: "submitting", text: "寻找下推广pudding的机会", createdAt: initial.createdAt });
  store.startSubmittingTurn(sessionID, "initial");
  store.acceptSubmittingTurn(sessionID, "initial", turnID);
  // turn.started is missing due to SSE tail catchup, only turn.delta arrives
  apply({ kind: "turn.delta", part: "thought", delta: "我先看看" });
  const vms = viewTurns([]);
  assert.equal(vms.length, 1, "must be a single turn rather than assistant above and user below");
  assert.equal(vms[0].user?.text, "寻找下推广pudding的机会");
  assert.equal(vms[0].assistant?.kind, "live");
});
