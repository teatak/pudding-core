import assert from "node:assert/strict";
import test from "node:test";
import {dismissInputFlow, inputFlowRequestKey, showInputFlow, useInputFlowStore} from "../src/state/inputFlowStore.ts";

test("copied question identities never dismiss another session's panel", () => {
  useInputFlowStore.setState({requests: [], drafts: {}});
  const question = {id: "copied-turn:question", title: "test", args: {}};
  const first = showInputFlow({...question, sessionID: "first"});
  const second = showInputFlow({...question, sessionID: "second"});
  useInputFlowStore.getState().setDraft(first, "textValue", "first draft");
  useInputFlowStore.getState().setDraft(second, "textValue", "second draft");
  assert.equal(useInputFlowStore.getState().drafts[inputFlowRequestKey(first)]?.textValue, "first draft");
  useInputFlowStore.getState().clearDraft(second);
  assert.equal(useInputFlowStore.getState().drafts[inputFlowRequestKey(first)]?.textValue, "first draft");
  dismissInputFlow(second);
  assert.deepEqual(useInputFlowStore.getState().requests, [first]);
});
