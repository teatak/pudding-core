import assert from "node:assert/strict";
import { setImmediate } from "node:timers/promises";
import { test } from "node:test";

import type { Session } from "../src/api/client.ts";
import type { SessionModelSettingsChange } from "../src/lib/sessionModelSettings.ts";
import { mutationKeys, queryKeys } from "../src/api/queryKeys.ts";
import { createTestViteServer } from "./vite-test-server.ts";

const server = await createTestViteServer({ configFile: false });
const { MutationObserver, QueryClient } = await server.ssrLoadModule("@tanstack/react-query") as typeof import("@tanstack/react-query");
const { sessionModelSettingsMutationOptions } = await server.ssrLoadModule("/src/lib/sessionModelSettings.ts") as typeof import("../src/lib/sessionModelSettings.ts");

function createSession(id: string, provider: string, model: string, reasoningEffort = "high") {
  return { id, provider, model, reasoningEffort, reasoningModelKey: reasoningEffort ? `${provider}:${model}` : "" } as Session;
}

function setup() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const session = createSession("session-a", "provider-a", "model-a");
  const other = createSession("session-b", "provider-b", "model-b");
  queryClient.setQueryData(queryKeys.sessions(), { sessions: [session, other] });
  queryClient.setQueryData(queryKeys.session(session.id), session);
  queryClient.setQueryData(queryKeys.session(other.id), other);
  const preferences = new Map([["provider-a:model-a", "high"]]);
  const setPreference = (key: string, value: string) => {
    if (value) preferences.set(key, value);
    else preferences.delete(key);
  };
  const change: SessionModelSettingsChange = {
    token: "token-a",
    sessionID: session.id,
    body: { provider: session.provider!, model: session.model!, reasoningEffort: "low" },
  };
  return { queryClient, session, other, preferences, setPreference, change };
}

test("settings remain confirmed until PATCH success and stale fetches cannot replace the result", async () => {
  const f = setup();
  const started = Promise.withResolvers<void>();
  const response = Promise.withResolvers<Session>();
  const observer = new MutationObserver(f.queryClient, sessionModelSettingsMutationOptions(
    f.queryClient, f.session.id,
    async (token, sessionID, body) => {
      assert.equal(token, f.change.token);
      assert.equal(sessionID, f.change.sessionID);
      assert.deepEqual(body, f.change.body);
      started.resolve();
      return response.promise;
    },
    f.setPreference,
  ));
  const saving = observer.mutate(f.change);
  await started.promise;
  assert.equal(f.queryClient.isMutating({ mutationKey: mutationKeys.sessionModelSettings(f.session.id) }), 1);
  assert.equal(f.queryClient.getQueryData(queryKeys.session(f.session.id)), f.session);
  assert.equal(f.preferences.get("provider-a:model-a"), "high");

  const stale = Promise.withResolvers<Session>();
  const staleSession = f.queryClient.fetchQuery({ queryKey: queryKeys.session(f.session.id), queryFn: () => stale.promise }).catch(() => {});
  const staleSessions = f.queryClient.fetchQuery({ queryKey: queryKeys.sessions(), queryFn: async () => ({ sessions: [await stale.promise, f.other] }) }).catch(() => {});
  const updated = { ...f.session, reasoningEffort: "low" };
  response.resolve(updated);
  await saving;
  stale.resolve(f.session);
  await Promise.all([staleSession, staleSessions]);
  assert.deepEqual(f.queryClient.getQueryData(queryKeys.session(f.session.id)), updated);
  assert.deepEqual(f.queryClient.getQueryData(queryKeys.sessions()), { sessions: [updated, f.other] });
  assert.equal(f.preferences.get("provider-a:model-a"), "low");
  f.queryClient.clear();
});

test("a rejected PATCH preserves confirmed caches and preferences", async () => {
  const f = setup();
  const observer = new MutationObserver(f.queryClient, sessionModelSettingsMutationOptions(
    f.queryClient, f.session.id, async () => { throw new Error("save rejected"); }, f.setPreference,
  ));
  await assert.rejects(observer.mutate(f.change), /save rejected/);
  assert.equal(observer.getCurrentResult().isError, true);
  assert.equal(f.queryClient.getQueryData(queryKeys.session(f.session.id)), f.session);
  assert.deepEqual(f.queryClient.getQueryData(queryKeys.sessions()), { sessions: [f.session, f.other] });
  assert.deepEqual([...f.preferences], [["provider-a:model-a", "high"]]);
  f.queryClient.clear();
});

test("pending writes retain their original session and token after observer options change", async () => {
  const f = setup();
  const started = Promise.withResolvers<void>();
  const proceed = Promise.withResolvers<void>();
  const calls: SessionModelSettingsChange[] = [];
  const request = async (token: string, sessionID: string, body: SessionModelSettingsChange["body"]) => {
    calls.push({ token, sessionID, body });
    return { ...f.session, reasoningEffort: "low" };
  };
  const observer = new MutationObserver(f.queryClient, {
    ...sessionModelSettingsMutationOptions(f.queryClient, f.session.id, request, f.setPreference),
    onMutate: async () => { started.resolve(); await proceed.promise; },
  });
  const saving = observer.mutate(f.change);
  await started.promise;
  observer.setOptions(sessionModelSettingsMutationOptions(f.queryClient, f.other.id, request, f.setPreference));
  proceed.resolve();
  await saving;
  assert.deepEqual(calls, [f.change]);
  assert.equal(f.queryClient.getQueryData(queryKeys.session(f.other.id)), f.other);
  assert.equal(f.preferences.has("provider-b:model-b"), false);
  assert.equal(f.preferences.get("provider-a:model-a"), "low");
  f.queryClient.clear();
});

test("same-session observers serialize writes and model changes do not write reasoning preferences", async () => {
  const f = setup();
  const started = Promise.withResolvers<void>();
  const response = Promise.withResolvers<Session>();
  const calls: SessionModelSettingsChange["body"][] = [];
  const request = async (_token: string, _sessionID: string, body: SessionModelSettingsChange["body"]) => {
    calls.push(body);
    if (calls.length === 1) {
      started.resolve();
      return response.promise;
    }
    return createSession(f.session.id, body.provider, body.model, "");
  };
  const first = new MutationObserver(f.queryClient, sessionModelSettingsMutationOptions(f.queryClient, f.session.id, request, f.setPreference));
  const second = new MutationObserver(f.queryClient, sessionModelSettingsMutationOptions(f.queryClient, f.session.id, request, f.setPreference));
  const savingFirst = first.mutate(f.change);
  await started.promise;
  const modelChange = { ...f.change, body: { provider: "provider-b", model: "model-b" } };
  const savingSecond = second.mutate(modelChange);
  await setImmediate();
  assert.equal(second.getCurrentResult().isPaused, true);
  assert.deepEqual(calls, [f.change.body]);
  response.resolve({ ...f.session, reasoningEffort: "low" });
  await Promise.all([savingFirst, savingSecond]);
  assert.deepEqual(calls, [f.change.body, modelChange.body]);
  assert.equal(f.preferences.get("provider-a:model-a"), "low");
  assert.equal(f.preferences.has("provider-b:model-b"), false);
  f.queryClient.clear();
});

test("restoring inherited effort removes only the confirmed model preference", async () => {
  const f = setup();
  f.preferences.set("provider-b:model-b", "medium");
  const observer = new MutationObserver(f.queryClient, sessionModelSettingsMutationOptions(
    f.queryClient, f.session.id,
    async () => ({ ...f.session, reasoningEffort: "", reasoningModelKey: "" }),
    f.setPreference,
  ));
  await observer.mutate({ ...f.change, body: { ...f.change.body, reasoningEffort: "" } });
  assert.deepEqual([...f.preferences], [["provider-b:model-b", "medium"]]);
  f.queryClient.clear();
});
