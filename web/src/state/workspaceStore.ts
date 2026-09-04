import { useSyncExternalStore } from "react";

const STORAGE_KEY = "pudding.workspace.ui.v3";
const LEGACY_OPEN_KEY = "pudding.workspaceOpenSessions";
const LEGACY_SURFACE_KEYS = [
  "pudding.workspace.sessionSurface.v2",
  "pudding.workspace.sessionSurface.v1",
  "pudding.canvas.sessionSurface.v1",
];
const LEGACY_SELECTED_BROWSER_KEY = "pudding.browser.selectedTab.v1";
const LEGACY_CLOSED_PROJECT_KEY = "pudding.workspace.closedProjectTabs.v1";
const LEGACY_TAB_ORDER_KEYS = ["pudding.workspaceTabOrder.v1", "pudding.canvasTabOrder.v1"];
const LEGACY_BOOLEAN_KEYS = ["pudding.workspaceOpen", "pudding.canvasOpen"];

export type WorkspaceTabKey =
  | "project"
  | `browser:${string}`
  | `canvas:${string}`
  | `file:${string}`;

export type WorkspaceSessionUIState = {
  activeTab: WorkspaceTabKey | null;
  open: boolean;
  projectTabOpen: boolean;
  tabOrder: WorkspaceTabKey[];
};

const DEFAULT_SESSION_STATE: WorkspaceSessionUIState = {
  activeTab: null,
  open: false,
  projectTabOpen: true,
  tabOrder: [],
};
const EMPTY_ORDER: WorkspaceTabKey[] = [];

let sessionStates = readSessionStates();
let activeSessionID = "";
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function notify() {
  listeners.forEach((listener) => listener());
}

export function getWorkspaceSessionUI(sessionID: string): WorkspaceSessionUIState {
  return sessionStates[sessionID] || DEFAULT_SESSION_STATE;
}

export function replaceWorkspaceSessionUI(sessionID: string, next: WorkspaceSessionUIState) {
  if (!sessionID || sameSessionState(getWorkspaceSessionUI(sessionID), next)) {
    return;
  }
  sessionStates = { ...sessionStates, [sessionID]: next };
  persistSessionStates();
  notify();
}

export function updateWorkspaceSessionUI(
  sessionID: string,
  update: (current: WorkspaceSessionUIState) => WorkspaceSessionUIState,
) {
  if (!sessionID) return;
  replaceWorkspaceSessionUI(sessionID, update(getWorkspaceSessionUI(sessionID)));
}

export function setWorkspaceOpen(sessionID: string, open: boolean) {
  if (!sessionID) return;
  const activeChanged = open && activeSessionID !== sessionID;
  if (open) {
    activeSessionID = sessionID;
  }
  const current = getWorkspaceSessionUI(sessionID);
  if (current.open !== open) {
    replaceWorkspaceSessionUI(sessionID, { ...current, open });
  } else if (activeChanged) {
    notify();
  }
}

export function setWorkspaceActiveTab(sessionID: string, activeTab: WorkspaceTabKey | null) {
  if (!sessionID) return;
  const activeChanged = activeSessionID !== sessionID;
  activeSessionID = sessionID;
  const current = getWorkspaceSessionUI(sessionID);
  if (current.activeTab !== activeTab) {
    replaceWorkspaceSessionUI(sessionID, { ...current, activeTab });
  } else if (activeChanged) {
    notify();
  }
}

export function openWorkspaceTab(sessionID: string, activeTab: WorkspaceTabKey) {
  if (!sessionID) return;
  const activeChanged = activeSessionID !== sessionID;
  activeSessionID = sessionID;
  const current = getWorkspaceSessionUI(sessionID);
  const next = {
    ...current,
    activeTab,
    open: true,
    projectTabOpen: activeTab === "project" ? true : current.projectTabOpen,
  };
  if (!sameSessionState(current, next)) {
    replaceWorkspaceSessionUI(sessionID, next);
  } else if (activeChanged) {
    notify();
  }
}

export function setWorkspaceTabOrder(sessionID: string, tabOrder: WorkspaceTabKey[]) {
  updateWorkspaceSessionUI(sessionID, (current) => {
    const next = uniqueTabKeys(tabOrder);
    return sameOrder(current.tabOrder, next) ? current : { ...current, tabOrder: next };
  });
}

export function useWorkspaceOpen(sessionID: string | undefined) {
  return useSyncExternalStore(
    subscribe,
    () => Boolean(sessionID && getWorkspaceSessionUI(sessionID).open),
    () => false,
  );
}

export function useWorkspaceActiveTab(sessionID: string | undefined) {
  return useSyncExternalStore(
    subscribe,
    () => sessionID ? getWorkspaceSessionUI(sessionID).activeTab : null,
    () => null,
  );
}

export function useProjectTabOpen(sessionID: string | undefined) {
  return useSyncExternalStore(
    subscribe,
    () => sessionID ? getWorkspaceSessionUI(sessionID).projectTabOpen : true,
    () => true,
  );
}

export function useWorkspaceTabOrder(sessionID: string) {
  return useSyncExternalStore(
    subscribe,
    () => sessionID ? getWorkspaceSessionUI(sessionID).tabOrder : EMPTY_ORDER,
    () => EMPTY_ORDER,
  );
}

export function useActiveWorkspaceSessionID(
  primarySessionID: string | undefined,
  secondarySessionID?: string,
) {
  return useSyncExternalStore(
    subscribe,
    () => visibleWorkspaceSessionID(primarySessionID, secondarySessionID),
    () => primarySessionID || secondarySessionID || "",
  );
}

export function mergeWorkspaceTabOrder(
  saved: readonly WorkspaceTabKey[] | undefined,
  availableIDs: readonly WorkspaceTabKey[],
) {
  const available = new Set(availableIDs);
  const seen = new Set<WorkspaceTabKey>();
  const next: WorkspaceTabKey[] = [];
  (saved || []).forEach((id) => {
    if (available.has(id) && !seen.has(id)) {
      seen.add(id);
      next.push(id);
    }
  });
  availableIDs.forEach((id) => {
    if (!seen.has(id)) {
      seen.add(id);
      next.push(id);
    }
  });
  return next;
}

export function nextWorkspaceTabAfterClose(
  closingTab: WorkspaceTabKey,
  availableTabs: readonly WorkspaceTabKey[],
  savedOrder: readonly WorkspaceTabKey[],
) {
  const fullOrder = mergeWorkspaceTabOrder(savedOrder, availableTabs);
  const closingIndex = fullOrder.indexOf(closingTab);
  const remaining = fullOrder.filter((tab) => tab !== closingTab);
  if (closingIndex < 0) {
    return remaining[0] || null;
  }
  return remaining[closingIndex] || remaining[closingIndex - 1] || null;
}

export function browserWorkspaceTabKey(tabID: string): WorkspaceTabKey {
  return `browser:${tabID}`;
}

export function canvasWorkspaceTabKey(itemID: string): WorkspaceTabKey {
  return `canvas:${itemID}`;
}

export function fileWorkspaceTabKey(previewID: string): WorkspaceTabKey {
  return `file:${previewID}`;
}

export function workspaceTabResourceID(
  tab: WorkspaceTabKey | null,
  kind: "browser" | "canvas" | "file",
) {
  const prefix = `${kind}:`;
  return tab?.startsWith(prefix) ? tab.slice(prefix.length) : undefined;
}

function visibleWorkspaceSessionID(primarySessionID: string | undefined, secondarySessionID?: string) {
  if (activeSessionID && (activeSessionID === primarySessionID || activeSessionID === secondarySessionID)) {
    return activeSessionID;
  }
  if (primarySessionID && getWorkspaceSessionUI(primarySessionID).open) {
    return primarySessionID;
  }
  if (secondarySessionID && getWorkspaceSessionUI(secondarySessionID).open) {
    return secondarySessionID;
  }
  return primarySessionID || secondarySessionID || "";
}

function sameSessionState(left: WorkspaceSessionUIState, right: WorkspaceSessionUIState) {
  return left.open === right.open
    && left.activeTab === right.activeTab
    && left.projectTabOpen === right.projectTabOpen
    && sameOrder(left.tabOrder, right.tabOrder);
}

function sameOrder(left: readonly string[], right: readonly string[]) {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function uniqueTabKeys(values: readonly WorkspaceTabKey[]) {
  return [...new Set(values)];
}

function isWorkspaceTabKey(value: unknown): value is WorkspaceTabKey {
  return value === "project"
    || (typeof value === "string" && (
      value.startsWith("browser:")
      || value.startsWith("canvas:")
      || value.startsWith("file:")
    ));
}

function normalizeLegacyTabKey(value: unknown): WorkspaceTabKey | undefined {
  if (value === "project" || value === "project:project") return "project";
  if (typeof value !== "string") return undefined;
  if (value.startsWith("widget:")) return canvasWorkspaceTabKey(value.slice("widget:".length));
  return isWorkspaceTabKey(value) ? value : undefined;
}

function parseRecord(key: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(localStorage.getItem(key) || "{}") as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? parsed as Record<string, unknown>
      : {};
  } catch {
    return {};
  }
}

function readSessionStates(): Record<string, WorkspaceSessionUIState> {
  if (typeof localStorage === "undefined") return {};
  if (localStorage.getItem(STORAGE_KEY) !== null) {
    const current = parseRecord(STORAGE_KEY);
    const parsed = parseSessionStates(current);
    removeLegacyState();
    return parsed;
  }

  const migrated = migrateLegacyState();
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(migrated));
  } catch {
    // Best-effort UI preference.
  }
  removeLegacyState();
  return migrated;
}

function parseSessionStates(raw: Record<string, unknown>) {
  const parsed: Record<string, WorkspaceSessionUIState> = {};
  Object.entries(raw).forEach(([sessionID, value]) => {
    if (!sessionID || !value || typeof value !== "object" || Array.isArray(value)) return;
    const record = value as Record<string, unknown>;
    parsed[sessionID] = {
      activeTab: isWorkspaceTabKey(record.activeTab) ? record.activeTab : null,
      open: record.open === true,
      projectTabOpen: record.projectTabOpen !== false,
      tabOrder: Array.isArray(record.tabOrder)
        ? uniqueTabKeys(record.tabOrder.filter(isWorkspaceTabKey))
        : [],
    };
  });
  return parsed;
}

function migrateLegacyState() {
  const openSessionIDs = new Set<string>();
  try {
    const open = JSON.parse(localStorage.getItem(LEGACY_OPEN_KEY) || "[]") as unknown;
    if (Array.isArray(open)) {
      open.filter((value): value is string => typeof value === "string").forEach((id) => openSessionIDs.add(id));
    }
  } catch {
    // Invalid legacy state is ignored.
  }
  const surfaces = LEGACY_SURFACE_KEYS.map(parseRecord).find((value) => Object.keys(value).length > 0) || {};
  const selectedBrowsers = parseRecord(LEGACY_SELECTED_BROWSER_KEY);
  const closedProjects = parseRecord(LEGACY_CLOSED_PROJECT_KEY);
  const orders = LEGACY_TAB_ORDER_KEYS.map(parseRecord).find((value) => Object.keys(value).length > 0) || {};
  const sessionIDs = new Set([
    ...openSessionIDs,
    ...Object.keys(surfaces),
    ...Object.keys(selectedBrowsers),
    ...Object.keys(closedProjects),
    ...Object.keys(orders),
  ]);
  const migrated: Record<string, WorkspaceSessionUIState> = {};
  sessionIDs.forEach((sessionID) => {
    const tabOrder = uniqueTabKeys(
      (Array.isArray(orders[sessionID]) ? orders[sessionID] : [])
        .map(normalizeLegacyTabKey)
        .filter((value): value is WorkspaceTabKey => Boolean(value)),
    );
    const surface = surfaces[sessionID];
    const projectTabOpen = closedProjects[sessionID] !== true;
    let activeTab: WorkspaceTabKey | null = null;
    if (surface === "project" && projectTabOpen) {
      activeTab = "project";
    } else if (surface === "browser") {
      const selected = selectedBrowsers[sessionID];
      activeTab = typeof selected === "string" && selected
        ? browserWorkspaceTabKey(selected)
        : tabOrder.find((tab) => tab.startsWith("browser:")) || null;
    } else if (surface === "canvas") {
      activeTab = tabOrder.find((tab) => tab.startsWith("canvas:")) || null;
    }
    migrated[sessionID] = {
      activeTab,
      open: openSessionIDs.has(sessionID),
      projectTabOpen,
      tabOrder,
    };
  });
  return migrated;
}

function persistSessionStates() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(sessionStates));
  } catch {
    // Best-effort UI preference.
  }
}

function removeLegacyState() {
  try {
    [
      LEGACY_OPEN_KEY,
      ...LEGACY_SURFACE_KEYS,
      LEGACY_SELECTED_BROWSER_KEY,
      LEGACY_CLOSED_PROJECT_KEY,
      ...LEGACY_TAB_ORDER_KEYS,
      ...LEGACY_BOOLEAN_KEYS,
    ].forEach((key) => localStorage.removeItem(key));
  } catch {
    // Best-effort one-time migration cleanup.
  }
}
