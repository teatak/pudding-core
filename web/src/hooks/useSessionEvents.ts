import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import {
  getBrowserState,
  getTurn,
  listBrowserTabs,
  listPendingApprovals,
  type AudioBindings,
  type BrowserState,
  type BrowserTab,
  type BackgroundProcess,
  type PendingApproval,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import {
  allowElectronBrowserTab,
  hasElectronWebviewBrowser,
  markElectronBrowserSessionClosed,
} from "@/browser/electronBridge";
import { browserTabFaviconURL, browserTabTitle, upsertBrowserTab } from "@/browser/helpers";
import type { BrowserTabsData } from "@/browser/types";
import { upsertTurnIntoPages, type TurnsInfiniteData } from "@/components/transcript/useTranscriptTurns";
import { sessionEvent, type SessionEvent } from "@/contracts/events";
import { apiURL } from "@/state/apiBase";
import { requestBrowserReveal, retainBrowserActivities } from "@/state/browserRevealStore";
import { useOverlayStore } from "@/state/overlayStore";

export function useSessionEvents(sessionID: string | undefined, token: string) {
  useVisibleSessionEvents(sessionID ? [sessionID] : [], token);
}

export function useVisibleSessionEvents(sessionIDs: string[], token: string) {
  useSessionEventSources(sessionIDs, token, true);
}

export function useBackgroundSessionEvents(sessionIDs: string[], token: string) {
  useSessionEventSources(sessionIDs, token, false);
}

function useSessionEventSources(sessionIDs: string[], token: string, syncMessages: boolean) {
  const queryClient = useQueryClient();
  const applyEvent = useOverlayStore((state) => state.applyEvent);
  const sessionIDsKey = normalizeSessionIDs(sessionIDs).join("\n");

  useEffect(() => {
    if (!token || !sessionIDsKey) {
      return;
    }
    const sources = sessionIDsKey.split("\n").map((sessionID) =>
      openSessionEventSource({
        applyEvent,
        queryClient,
        sessionID,
        syncMessages,
        token,
      }),
    );
    return () => {
      for (const source of sources) {
        source.close();
      }
    };
  }, [applyEvent, queryClient, sessionIDsKey, syncMessages, token]);
}

function normalizeSessionIDs(sessionIDs: string[]) {
  return Array.from(new Set(sessionIDs.filter(Boolean))).sort();
}

function openSessionEventSource({
  applyEvent,
  queryClient,
  sessionID,
  syncMessages,
  token,
}: {
  applyEvent: (event: SessionEvent) => void;
  queryClient: QueryClient;
  sessionID: string;
  syncMessages: boolean;
  token: string;
}) {
  const params = new URLSearchParams({ token });
  const after = useOverlayStore.getState().lastEventSeqs[sessionID];
  if (after) {
    params.set("after", String(after));
  }
  const source = new EventSource(apiURL(`/sessions/${encodeURIComponent(sessionID)}/events?${params.toString()}`));
  source.onopen = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.backgroundProcesses(sessionID) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
  };
  const handleMessage = (event: MessageEvent<string>) => {
    let payload: unknown;
    try {
      payload = JSON.parse(event.data);
    } catch {
      console.warn("malformed session event payload", event.data);
      return;
    }
    const parsed = sessionEvent.safeParse(payload);
    if (!parsed.success) {
      console.warn("invalid session event", parsed.error);
      return;
    }
    if (parsed.data.kind === "turn.started") {
      useOverlayStore.getState().clearSessionCompletion(parsed.data.sessionID);
    } else if (!syncMessages && parsed.data.kind === "turn.completed") {
      useOverlayStore.getState().markSessionCompleted(parsed.data.sessionID);
    }
    applyEvent(parsed.data);
    syncAudioBindingsFromEvent(queryClient, parsed.data);
    syncBrowserStateFromEvent(queryClient, parsed.data, syncMessages, token);
    syncProjectGitFromEvent(queryClient, parsed.data);
    syncBackgroundProcessFromEvent(queryClient, parsed.data);
    syncSessionListFromEvent(queryClient, parsed.data);
    if (
      parsed.data.kind === "turn.tool" &&
      (parsed.data.name === "builtin_app_load" || parsed.data.name === "builtin_app_unload") &&
      (parsed.data.phase === "ok" || parsed.data.phase === "error")
    ) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    }
    if (parsed.data.kind === "turn.started" || isTurnTerminalEvent(parsed.data)) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessionUsage(sessionID) });
    }
    if ((parsed.data.kind === "turn.started" || isTurnTerminalEvent(parsed.data)) && syncMessages) {
      syncTurn(queryClient, token, sessionID, parsed.data.turnID);
    }
    if (parsed.data.kind === "input.steered" && syncMessages) {
      syncTurn(queryClient, token, sessionID, parsed.data.turnID);
    }
    if (syncMessages && (isInputEvent(parsed.data) || parsed.data.kind === "turn.started")) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.queuedInputs(sessionID) });
    }
  };

  source.addEventListener("turn.started", handleMessage);
  source.addEventListener("turn.delta", handleMessage);
  source.addEventListener("turn.tool", handleMessage);
  source.addEventListener("turn.completed", handleMessage);
  source.addEventListener("turn.failed", handleMessage);
  source.addEventListener("turn.cancelled", handleMessage);
  source.addEventListener("input.queued", handleMessage);
  source.addEventListener("input.updated", handleMessage);
  source.addEventListener("input.steered", handleMessage);
  source.addEventListener("audio.bindings", handleMessage);
  source.addEventListener("audio.input_level", handleMessage);
  source.addEventListener("approval.requested", handleMessage);
  source.addEventListener("approval.resolved", handleMessage);
  source.addEventListener("process.started", handleMessage);
  source.addEventListener("process.finished", handleMessage);
  source.addEventListener("process.stopped", handleMessage);
  source.addEventListener("process.removed", handleMessage);
  source.addEventListener("session.titled", handleMessage);
  source.addEventListener("ping", handleMessage);
  hydratePendingApprovals(applyEvent, token, sessionID);
  return {
    close: () => {
      source.close();
    },
  };
}

function syncProjectGitFromEvent(queryClient: QueryClient, event: SessionEvent) {
  if (event.kind === "turn.tool" && (event.phase === "ok" || event.phase === "error")) {
    const name = event.name || "";
    if (["builtin_file_", "builtin_patch_", "builtin_git_", "builtin_command_"].some((prefix) => name.startsWith(prefix))) {
      void queryClient.invalidateQueries({ queryKey: ["session", event.sessionID, "project", "git"] });
    }
    return;
  }
  if (event.kind === "process.finished" || event.kind === "process.stopped") {
    void queryClient.invalidateQueries({ queryKey: ["session", event.sessionID, "project", "git"] });
  }
}

function syncBrowserStateFromEvent(
  queryClient: QueryClient,
  event: SessionEvent,
  revealVisibleSession: boolean,
  token: string,
) {
  if (event.kind !== "turn.tool" || (event.phase !== "ok" && event.phase !== "error")) {
    return;
  }
  if (!event.name?.startsWith("builtin_browser_")) {
    return;
  }
  const syncedFromToolResult = syncBrowserToolResult(queryClient, event);
  void queryClient.invalidateQueries({ queryKey: queryKeys.browserState(event.sessionID) });
  void queryClient.invalidateQueries({ queryKey: queryKeys.browserTabs(event.sessionID) });
  if (event.name === "builtin_browser_close" && event.phase === "ok") {
    retainBrowserActivities(
      event.sessionID,
      browserTabsFromToolContent(event.content, event.sessionID).map((tab) => tab.id),
    );
  }
  const shouldReveal = revealVisibleSession && shouldRevealBrowserTool(event);
  if (shouldReveal && syncedFromToolResult) {
    publishBrowserActivity(queryClient, event);
  }
  void hydrateBrowserState(queryClient, token, event.sessionID).then((hasBrowser) => {
    if (shouldReveal && hasBrowser && !syncedFromToolResult) {
      publishBrowserActivity(queryClient, event);
    }
  });
}

function publishBrowserActivity(
  queryClient: QueryClient,
  event: Extract<SessionEvent, { kind: "turn.tool" }>,
) {
  const state = queryClient.getQueryData<BrowserState>(queryKeys.browserState(event.sessionID));
  requestBrowserReveal(event.sessionID, {
    faviconURL: state?.faviconURL,
    resourceID: state?.tabID,
    title: state?.title,
    toolName: event.name,
    url: state?.url,
  });
}

function syncBrowserToolResult(queryClient: QueryClient, event: Extract<SessionEvent, { kind: "turn.tool" }>) {
  const processModeFallback = hasElectronWebviewBrowser() ? "webview" : "headless";
  if (event.name === "builtin_browser_close" && event.phase === "ok") {
    const remaining = browserTabsFromToolContent(event.content, event.sessionID);
    if (remaining.length > 0) {
      remaining.forEach((tab) => allowElectronBrowserTab(event.sessionID, tab.id));
      queryClient.setQueryData(queryKeys.browserTabs(event.sessionID), { tabs: remaining, processMode: remaining[0]?.mode || processModeFallback });
      return true;
    }
    markElectronBrowserSessionClosed(event.sessionID);
    queryClient.setQueryData(queryKeys.browserTabs(event.sessionID), { tabs: [], processMode: processModeFallback });
    queryClient.setQueryData(queryKeys.browserState(event.sessionID), { hasState: false, sessionID: event.sessionID, processMode: processModeFallback });
    return false;
  }
  const tab = browserTabFromToolContent(event.content, event.sessionID);
  if (!tab) {
    return false;
  }
  allowElectronBrowserTab(event.sessionID, tab.id);
  queryClient.setQueryData(queryKeys.browserTabs(event.sessionID), (current: BrowserTabsData | undefined) => ({
    tabs: upsertBrowserTab(current?.tabs || [], tab),
    processMode: tab.mode || current?.processMode || processModeFallback,
  }));
  const title = browserTabTitle(tab, tab.title || tab.url || "about:blank", "about:blank");
  queryClient.setQueryData(queryKeys.browserState(event.sessionID), {
    hasState: true,
    sessionID: event.sessionID,
    tabID: tab.id,
    url: tab.url,
    title,
    faviconURL: browserTabFaviconURL(tab),
    mode: tab.mode || processModeFallback,
    processMode: tab.mode || processModeFallback,
    createdAt: tab.createdAt,
    updatedAt: tab.updatedAt,
  });
  return true;
}

function browserTabsFromToolContent(content: string | undefined, expectedSessionID: string): BrowserTab[] {
  if (!content) {
    return [];
  }
  try {
    const payload = JSON.parse(content) as { tabs?: unknown };
    if (!Array.isArray(payload.tabs)) {
      return [];
    }
    return payload.tabs
      .map((item) => normalizeBrowserTab(item, expectedSessionID))
      .filter((tab): tab is BrowserTab => Boolean(tab));
  } catch {
    return [];
  }
}

function browserTabFromToolContent(content: string | undefined, expectedSessionID: string): BrowserTab | null {
  if (!content) {
    return null;
  }
  try {
    const payload = JSON.parse(content);
    return firstBrowserTab(payload, expectedSessionID);
  } catch {
    return null;
  }
}

function firstBrowserTab(value: unknown, expectedSessionID: string): BrowserTab | null {
  const direct = normalizeBrowserTab(value, expectedSessionID);
  if (direct) {
    return direct;
  }
  if (!value || typeof value !== "object") {
    return null;
  }
  const record = value as Record<string, unknown>;
  for (const key of ["tab", "action", "observation"]) {
    const tab = firstBrowserTab(record[key], expectedSessionID);
    if (tab) {
      return tab;
    }
  }
  if (Array.isArray(record.tabs)) {
    for (const item of record.tabs) {
      const tab = firstBrowserTab(item, expectedSessionID);
      if (tab) {
        return tab;
      }
    }
  }
  return null;
}

function normalizeBrowserTab(value: unknown, expectedSessionID: string): BrowserTab | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const record = value as Record<string, unknown>;
  const id = stringField(record.id);
  const sessionID = stringField(record.sessionID) || expectedSessionID;
  const url = stringField(record.url);
  if (!id || sessionID !== expectedSessionID || !url) {
    return null;
  }
  const now = new Date().toISOString();
  return {
    id,
    sessionID,
    targetID: stringField(record.targetID) || undefined,
    url,
    title: stringField(record.title),
    faviconURL: stringField(record.faviconURL) || undefined,
    mode: record.mode === "external" ? "external" : record.mode === "webview" ? "webview" : hasElectronWebviewBrowser() ? "webview" : "headless",
    canGoBack: booleanField(record.canGoBack),
    canGoForward: booleanField(record.canGoForward),
    createdAt: stringField(record.createdAt) || now,
    updatedAt: stringField(record.updatedAt) || now,
  };
}

function stringField(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function booleanField(value: unknown) {
  return typeof value === "boolean" ? value : undefined;
}

async function hydrateBrowserState(queryClient: QueryClient, token: string, sessionID: string) {
  try {
    const [state, tabs] = await Promise.all([
      queryClient.fetchQuery({
        queryKey: queryKeys.browserState(sessionID),
        queryFn: () => getBrowserState(token, sessionID),
        staleTime: 0,
      }),
      queryClient.fetchQuery({
        queryKey: queryKeys.browserTabs(sessionID),
        queryFn: () => listBrowserTabs(token, sessionID),
        staleTime: 0,
      }),
    ]);
    const liveTabs = tabs.tabs.filter((tab) => tab.sessionID === sessionID);
    if (liveTabs.length > 0) {
      liveTabs.forEach((tab) => allowElectronBrowserTab(sessionID, tab.id));
    } else if (state.hasState && state.tabID) {
      allowElectronBrowserTab(sessionID, state.tabID);
    }
    return Boolean(state.hasState || liveTabs.length > 0);
  } catch (error) {
    console.warn("failed to hydrate browser state from tool event", error);
    return false;
  }
}

function shouldRevealBrowserTool(event: Extract<SessionEvent, { kind: "turn.tool" }>) {
  if (event.name === "builtin_browser_close") {
    return false;
  }
  if (event.name === "builtin_browser_status") {
    return browserStatusHasTab(event.content);
  }
  return true;
}

function browserStatusHasTab(content: string | undefined) {
  if (!content) {
    return false;
  }
  try {
    const payload = JSON.parse(content) as { has_tab?: unknown; tab?: unknown };
    return payload.has_tab === true || Boolean(payload.tab);
  } catch {
    return false;
  }
}

function hydratePendingApprovals(
  applyEvent: (event: SessionEvent) => void,
  token: string,
  sessionID: string,
) {
  void listPendingApprovals(token, sessionID)
    .then(({ approvals }) => {
      for (const approval of approvals) {
        applyEvent(pendingApprovalToEvent(approval));
      }
    })
    .catch((error) => {
      console.warn("failed to hydrate pending approvals", error);
    });
}

function pendingApprovalToEvent(approval: PendingApproval): Extract<SessionEvent, { kind: "approval.requested" }> {
  return {
    kind: "approval.requested",
    sessionID: approval.sessionID,
    turnID: approval.turnID,
    callID: approval.callID,
    approvalID: approval.id,
    approvalKind: approval.approvalKind,
    title: approval.title,
    reason: approval.reason,
    risk: approval.risk,
    payload: approval.payload,
  };
}

function syncTurn(queryClient: QueryClient, token: string, sessionID: string, turnID: string) {
  void getTurn(token, sessionID, turnID)
    .then((turn) => {
      queryClient.setQueryData<TurnsInfiniteData>(queryKeys.turns(sessionID), (previous) => upsertTurnIntoPages(previous, turn));
    })
    .catch((error) => {
      console.warn("failed to sync turn", error);
    });
}

function syncAudioBindingsFromEvent(queryClient: QueryClient, event: SessionEvent) {
  if (event.kind === "audio.bindings") {
    queryClient.setQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings(), {
      bindings: {
        inputOwner: event.inputOwner,
        inputMode: event.inputMode,
        outputOwner: event.outputOwner,
        inputLevel: event.inputLevel,
      },
    });
    return;
  }
  if (event.kind === "audio.input_level") {
    queryClient.setQueryData<{ bindings: AudioBindings }>(queryKeys.audioBindings(), (previous) => {
      const current = previous?.bindings;
      if (!current || current.inputOwner !== event.sessionID) {
        return previous;
      }
      return {
        bindings: {
          ...current,
          inputLevel: event.inputLevel,
        },
      };
    });
  }
}

function syncSessionListFromEvent(queryClient: QueryClient, event: SessionEvent) {
  if (event.kind === "turn.started") {
    patchSessionInList(queryClient, event.sessionID, { lastActivityAt: new Date().toISOString(), running: true });
    return;
  }
  if (isTurnTerminalEvent(event)) {
    patchSessionInList(queryClient, event.sessionID, { lastActivityAt: new Date().toISOString(), running: false });
    return;
  }
  if (event.kind === "input.queued" || event.kind === "input.steered") {
    patchSessionInList(queryClient, event.sessionID, { lastActivityAt: new Date().toISOString() });
    return;
  }
  if (event.kind === "session.titled") {
    patchSessionInList(queryClient, event.sessionID, { title: event.title });
  }
}

function syncBackgroundProcessFromEvent(queryClient: QueryClient, event: SessionEvent) {
  if (event.kind !== "process.started" && event.kind !== "process.finished" && event.kind !== "process.stopped" && event.kind !== "process.removed") {
    return;
  }
  if (event.kind === "process.removed") {
    queryClient.setQueryData<{ processes: BackgroundProcess[] }>(
      queryKeys.backgroundProcesses(event.sessionID),
      (previous) => ({
        processes: (previous?.processes ?? []).filter((process) => process.processID !== event.payload.processID),
      }),
    );
    queryClient.removeQueries({ queryKey: queryKeys.backgroundProcess(event.sessionID, event.payload.processID) });
    return;
  }
  queryClient.setQueryData<{ processes: BackgroundProcess[] }>(
    queryKeys.backgroundProcesses(event.sessionID),
    (previous) => {
      const current = previous?.processes ?? [];
      const index = current.findIndex((process) => process.processID === event.payload.processID);
      if (index < 0) {
        return { processes: [event.payload, ...current] };
      }
      const processes = [...current];
      processes[index] = event.payload;
      return { processes };
    },
  );
  void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
  void queryClient.invalidateQueries({ queryKey: queryKeys.backgroundProcess(event.sessionID, event.payload.processID) });
}

function isTurnTerminalEvent(
  event: SessionEvent,
): event is Extract<SessionEvent, { kind: "turn.completed" | "turn.failed" | "turn.cancelled" }> {
  return event.kind === "turn.completed" || event.kind === "turn.failed" || event.kind === "turn.cancelled";
}

function isInputEvent(event: SessionEvent) {
  return event.kind === "input.queued" || event.kind === "input.updated";
}

function patchSessionInList(queryClient: QueryClient, sessionID: string, patch: Partial<Session>) {
  queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) => {
    if (!previous) {
      return previous;
    }
    let matched = false;
    const sessions = previous.sessions.map((session) => {
      if (session.id !== sessionID) {
        return session;
      }
      matched = true;
      return { ...session, ...patch };
    });
    return matched ? { sessions: sortSessionsByActivity(sessions) } : previous;
  });
}

function sortSessionsByActivity(sessions: Session[]) {
  return [...sessions].sort((a, b) => {
    const activityDiff = Date.parse(b.lastActivityAt) - Date.parse(a.lastActivityAt);
    if (activityDiff !== 0) {
      return activityDiff;
    }
    return Date.parse(b.createdAt) - Date.parse(a.createdAt);
  });
}
