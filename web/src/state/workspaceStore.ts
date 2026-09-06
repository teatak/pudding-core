import { useStore } from "zustand";
import { createStore } from "zustand/vanilla";

const STORAGE_KEY = "pudding.workspace.ui.v7";
const PREVIOUS_STORAGE_KEYS = ["pudding.workspace.ui.v6", "pudding.workspace.ui.v5", "pudding.workspace.ui.v4", "pudding.workspace.ui.v3"];
export const minimumFocusChatWidth = 340;
export type WorkspacePresentation = "hidden" | "standard" | "focused";
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


export type WorkspaceContentTabKey = `browser:${string}` | `canvas:${string}`;
export type WorkspaceTopTabKey = "project" | WorkspaceContentTabKey;
export type WorkspaceTabKey = WorkspaceTopTabKey | `file:${string}`;
export type WorkspaceSelection = "project" | "library" | WorkspaceContentTabKey;
export type WorkspaceProjectUIState = { activeTab: "project" | `file:${string}`; tabOrder: WorkspaceTabKey[] };
export type WorkspaceSessionUIState = {
  activeTab: WorkspaceSelection;
  presentation: WorkspacePresentation;
  tabOrder: WorkspaceTopTabKey[];
  closedCanvasTabs: Record<string, string>;
  project: WorkspaceProjectUIState;
};
const DEFAULT_SESSION_STATE: WorkspaceSessionUIState = {
  activeTab: "project", presentation: "hidden", tabOrder: ["project"], closedCanvasTabs: {},
  project: { activeTab: "project", tabOrder: [] },
};
const workspaceStore = createStore(() => ({ sessions: readSessionStates(), activeSessionID: "", focusChatWidths: {} as Record<string, number> }));

export function getWorkspaceSessionUI(sessionID: string): WorkspaceSessionUIState {
  return workspaceStore.getState().sessions[sessionID] || DEFAULT_SESSION_STATE;
}
export function replaceWorkspaceSessionUI(sessionID: string, next: WorkspaceSessionUIState) {
  const current = getWorkspaceSessionUI(sessionID);
  if (!sessionID || sameSessionState(current, next)) return;
  workspaceStore.setState((state) => {
    const focusChatWidths = { ...state.focusChatWidths };
    if (current.presentation !== next.presentation) delete focusChatWidths[sessionID];
    return { sessions: { ...state.sessions, [sessionID]: next }, focusChatWidths };
  });
  persistSessionStates();
}
export function updateWorkspaceSessionUI(sessionID: string, update: (current: WorkspaceSessionUIState) => WorkspaceSessionUIState) {
  if (sessionID) replaceWorkspaceSessionUI(sessionID, update(getWorkspaceSessionUI(sessionID)));
}
export function setWorkspacePresentation(sessionID: string, presentation: WorkspacePresentation) {
  if (!sessionID) return;
  if (presentation !== "hidden") workspaceStore.setState({ activeSessionID: sessionID });
  updateWorkspaceSessionUI(sessionID, (current) => ({ ...current, presentation }));
}
export function setWorkspaceOpen(sessionID: string, open: boolean) {
  const current = getWorkspaceSessionUI(sessionID).presentation;
  setWorkspacePresentation(sessionID, open ? (current === "hidden" ? "standard" : current) : "hidden");
}
export function openWorkspaceView(sessionID: string, activeTab: "project" | "library") {
  if (!sessionID) return;
  workspaceStore.setState({ activeSessionID: sessionID });
  updateWorkspaceSessionUI(sessionID, (current) => ({ ...current, activeTab, tabOrder: activeTab === "project" && !current.tabOrder.includes("project") ? [...current.tabOrder, "project"] : current.tabOrder, presentation: current.presentation === "hidden" ? "standard" : current.presentation }));
}
export function setWorkspaceActiveTab(sessionID: string, tab: WorkspaceTabKey) {
  if (!sessionID) return;
  workspaceStore.setState({ activeSessionID: sessionID });
  updateWorkspaceSessionUI(sessionID, (current) => {
    if (!isContentTab(tab)) return { ...current, activeTab: "project", tabOrder: current.tabOrder.includes("project") ? current.tabOrder : [...current.tabOrder, "project"], project: { ...current.project, activeTab: tab } };
    const closedCanvasTabs = { ...current.closedCanvasTabs };
    delete closedCanvasTabs[tab];
    return { ...current, activeTab: tab, closedCanvasTabs, tabOrder: current.tabOrder.includes(tab) ? current.tabOrder : [...current.tabOrder, tab] };
  });
}
export function openWorkspaceTab(sessionID: string, activeTab: WorkspaceTabKey) {
  setWorkspaceActiveTab(sessionID, activeTab);
  setWorkspaceOpen(sessionID, true);
}
export function setWorkspaceTabOrder(sessionID: string, tabOrder: WorkspaceTabKey[]) {
  updateWorkspaceSessionUI(sessionID, (current) => ({ ...current, tabOrder: uniqueTabKeys(tabOrder.filter(isTopTab)) }));
}
function nextSelection(current: WorkspaceSessionUIState, available: readonly WorkspaceTopTabKey[]): WorkspaceSelection {
  if (current.activeTab === "library" || available.includes(current.activeTab)) return current.activeTab;
  const index = current.tabOrder.indexOf(current.activeTab);
  return current.tabOrder.slice(index + 1).find((key) => available.includes(key))
    || current.tabOrder.slice(0, index).reverse().find((key) => available.includes(key)) || available[0] || "library";
}
export function reconcileWorkspaceTabs(sessionID: string, kind: "project" | "browser", availableTabs: WorkspaceTabKey[]) {
  updateWorkspaceSessionUI(sessionID, (current) => {
    if (kind === "project") {
      const tabOrder = mergeWorkspaceTabOrder(current.project.tabOrder, availableTabs.filter((key) => key.startsWith("file:")));
      return { ...current, project: { tabOrder, activeTab: tabOrder.includes(current.project.activeTab) ? current.project.activeTab : "project" } };
    }
    const available = [...current.tabOrder.filter((key) => !key.startsWith("browser:")), ...availableTabs.filter((key): key is WorkspaceContentTabKey => key.startsWith("browser:"))];
    const tabOrder = mergeWorkspaceTabOrder(current.tabOrder, available);
    return { ...current, tabOrder, activeTab: nextSelection(current, tabOrder) };
  });
}
// Canonical visible=false items seed closed state once; reopening/closing belongs to local UI.
export function resolveCanvasTabs(current: WorkspaceSessionUIState, items: readonly { id: string; visible: boolean; updatedAt: string }[]): WorkspaceSessionUIState {
  const keys = items.map((item) => canvasWorkspaceTabKey(item.id));
  const closedCanvasTabs = Object.fromEntries(Object.entries(current.closedCanvasTabs).filter(([key]) => keys.includes(key as WorkspaceContentTabKey)));
  for (const item of items) {
    const key = canvasWorkspaceTabKey(item.id);
    if (!item.visible && !current.tabOrder.includes(key) && current.activeTab !== key && !closedCanvasTabs[key]) closedCanvasTabs[key] = item.updatedAt;
  }
  const available = [...current.tabOrder.filter((key) => !key.startsWith("canvas:")), ...keys.filter((key) => !closedCanvasTabs[key])];
  const tabOrder = mergeWorkspaceTabOrder(current.tabOrder, available);
  return { ...current, closedCanvasTabs, tabOrder, activeTab: nextSelection(current, tabOrder) };
}
export function closeWorkspaceTab(sessionID: string, closingTab: WorkspaceTabKey) {
  closeWorkspaceTabs(sessionID, [closingTab]);
}
export function closeWorkspaceTabs(sessionID: string, closingTabs: WorkspaceTabKey[]) {
  updateWorkspaceSessionUI(sessionID, (current) => {
    const closing = new Set(closingTabs);
    const tabOrder = current.tabOrder.filter((key) => !closing.has(key));
    const closedAt = new Date().toISOString();
    return { ...current, tabOrder, activeTab: nextSelection(current, tabOrder),
      project: { activeTab: closing.has(current.project.activeTab) ? "project" : current.project.activeTab, tabOrder: current.project.tabOrder.filter((key) => !closing.has(key)) },
      closedCanvasTabs: { ...current.closedCanvasTabs, ...Object.fromEntries(current.tabOrder.filter((key) => closing.has(key) && key.startsWith("canvas:")).map((key) => [key, closedAt])) },
    };
  });
}
export function useWorkspaceSessionUI(sessionID: string) {
  return useStore(workspaceStore, (state) => state.sessions[sessionID] || DEFAULT_SESSION_STATE);
}
export function useWorkspacePresentation(sessionID: string | undefined) {
  return useStore(workspaceStore, (state) => (sessionID && state.sessions[sessionID]?.presentation) || "hidden");
}
export function getWorkspaceFocusChatWidth(sessionID: string) {
  return workspaceStore.getState().focusChatWidths[sessionID] ?? minimumFocusChatWidth;
}
export function useWorkspaceFocusChatWidth(sessionID: string) {
  return useStore(workspaceStore, (state) => state.focusChatWidths[sessionID] ?? minimumFocusChatWidth);
}
export function setWorkspaceFocusChatWidth(sessionID: string, chatWidth: number) {
  if (!sessionID || getWorkspaceSessionUI(sessionID).presentation !== "focused") return;
  workspaceStore.setState((state) => ({ focusChatWidths: { ...state.focusChatWidths, [sessionID]: Math.max(minimumFocusChatWidth, chatWidth) } }));
}
export function useWorkspaceOpen(sessionID: string | undefined) { return useWorkspacePresentation(sessionID) !== "hidden"; }
export function useWorkspaceActiveTab(sessionID: string) {
  return useStore(workspaceStore, (state) => (state.sessions[sessionID] || DEFAULT_SESSION_STATE).activeTab);
}
export function useWorkspaceTabOrder(sessionID: string) {
  return useStore(workspaceStore, (state) => (state.sessions[sessionID] || DEFAULT_SESSION_STATE).tabOrder);
}
export function useActiveWorkspaceSessionID(primarySessionID: string | undefined, secondarySessionID?: string) {
  return useStore(workspaceStore, (state) => {
    const { activeSessionID, sessions } = state;
    if (activeSessionID && (activeSessionID === primarySessionID || activeSessionID === secondarySessionID)) return activeSessionID;
    if (primarySessionID && sessions[primarySessionID] && sessions[primarySessionID].presentation !== "hidden") return primarySessionID;
    if (secondarySessionID && sessions[secondarySessionID] && sessions[secondarySessionID].presentation !== "hidden") return secondarySessionID;
    return primarySessionID || secondarySessionID || "";
  });
}
export function mergeWorkspaceTabOrder<T extends WorkspaceTabKey>(saved: readonly WorkspaceTabKey[] | undefined, availableIDs: readonly T[]): T[] {
  const available = new Set<WorkspaceTabKey>(availableIDs);
  return uniqueTabKeys([...(saved || []).filter((id): id is T => available.has(id)), ...availableIDs]);
}
export function browserWorkspaceTabKey(tabID: string): WorkspaceContentTabKey { return `browser:${tabID}`; }
export function canvasWorkspaceTabKey(itemID: string): WorkspaceContentTabKey { return `canvas:${itemID}`; }
export function fileWorkspaceTabKey(previewID: string): `file:${string}` { return `file:${previewID}`; }
export function workspaceTabResourceID(tab: string | null, kind: "browser" | "canvas" | "file") {
  const prefix = `${kind}:`;
  return tab?.startsWith(prefix) ? tab.slice(prefix.length) : undefined;
}
function sameSessionState(left: WorkspaceSessionUIState, right: WorkspaceSessionUIState) {
  return left.presentation === right.presentation && left.activeTab === right.activeTab && sameOrder(left.tabOrder, right.tabOrder)
    && left.project.activeTab === right.project.activeTab && sameOrder(left.project.tabOrder, right.project.tabOrder)
    && Object.keys(left.closedCanvasTabs).length === Object.keys(right.closedCanvasTabs).length
    && Object.entries(left.closedCanvasTabs).every(([key, date]) => right.closedCanvasTabs[key] === date);
}
function sameOrder(left: readonly string[], right: readonly string[]) { return left.length === right.length && left.every((value, index) => value === right[index]); }
function uniqueTabKeys<T extends WorkspaceTabKey>(values: readonly T[]): T[] { return [...new Set(values)]; }
function isTopTab(value: unknown): value is WorkspaceTopTabKey { return value === "project" || isContentTab(value); }
function isContentTab(value: unknown): value is WorkspaceContentTabKey { return typeof value === "string" && /^(browser|canvas):.+/.test(value); }
function isProjectTab(value: unknown): value is "project" | `file:${string}` { return value === "project" || (typeof value === "string" && /^file:.+/.test(value)); }
function isWorkspaceTabKey(value: unknown): value is WorkspaceTabKey { return isContentTab(value) || isProjectTab(value); }
function normalizeLegacyTabKey(value: unknown): WorkspaceTabKey | undefined {
  if (value === "project" || value === "project:project") return "project";
  if (typeof value !== "string") return undefined;
  if (value.startsWith("widget:")) return canvasWorkspaceTabKey(value.slice("widget:".length));
  return isWorkspaceTabKey(value) ? value : undefined;
}
function parseRecord(key: string): Record<string, unknown> {
  try { const parsed: unknown = JSON.parse(localStorage.getItem(key) || "{}"); return asRecord(parsed); } catch { return {}; }
}
function asRecord(value: unknown): Record<string, unknown> { return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function tabKeys(value: unknown): WorkspaceTabKey[] { return Array.isArray(value) ? uniqueTabKeys(value.filter(isWorkspaceTabKey)) : []; }
export function migrateWorkspaceSession(record: Record<string, unknown>, projectWasFixed = true): WorkspaceSessionUIState {
  // v5 had independent browser/canvas orders. Import them once, then persist only the merged order.
  const apps = asRecord(record.apps), project = asRecord(apps.project || record.project);
  const browser = asRecord(apps.browser), canvas = asRecord(apps.artifacts);
  const selectedApp = asRecord(apps[String(record.activeApp)]);
  const selected = record.activeApp === "library" ? "library" : selectedApp.activeTab || record.activeTab;
  const order = Object.keys(apps).length ? [...tabKeys(browser.tabOrder), ...tabKeys(canvas.tabOrder)] : tabKeys(record.tabOrder);
  const closedCanvasTabs = Object.fromEntries(Object.entries(asRecord(canvas.closedTabs || record.closedCanvasTabs)).filter(([key, date]) => key.startsWith("canvas:") && typeof date === "string" && Number.isFinite(Date.parse(date)))) as Record<string, string>;
  const activeTab = isContentTab(selected) || selected === "library" ? selected : "project";
  const tabOrder = uniqueTabKeys(order.filter(isTopTab).filter((key) => !closedCanvasTabs[key]));
  if (projectWasFixed && !tabOrder.includes("project")) tabOrder.unshift("project");
  if (isTopTab(activeTab) && !tabOrder.includes(activeTab)) { delete closedCanvasTabs[activeTab]; tabOrder.push(activeTab); }
  return { activeTab, tabOrder, closedCanvasTabs,
    presentation: record.presentation === "focused" ? "focused" : record.presentation === "standard" || record.open === true ? "standard" : "hidden",
    project: { activeTab: isProjectTab(project.activeTab) ? project.activeTab : isProjectTab(selected) ? selected : "project", tabOrder: tabKeys(project.tabOrder || record.tabOrder).filter((key) => key.startsWith("file:")) },
  };
}
function readSessionStates(): Record<string, WorkspaceSessionUIState> {
  if (typeof localStorage === "undefined") return {};
  const key = [STORAGE_KEY, ...PREVIOUS_STORAGE_KEYS].find((entry) => localStorage.getItem(entry) !== null);
  const source = key ? parseRecord(key) : migrateLegacyState();
  const sessions = Object.fromEntries(Object.entries(source).flatMap(([id, value]) => value && typeof value === "object" ? [[id, migrateWorkspaceSession(asRecord(value), key !== STORAGE_KEY)]] : []));
  localStorage.setItem(STORAGE_KEY, JSON.stringify(sessions));
  [...PREVIOUS_STORAGE_KEYS, LEGACY_OPEN_KEY, ...LEGACY_SURFACE_KEYS, LEGACY_SELECTED_BROWSER_KEY, LEGACY_CLOSED_PROJECT_KEY, ...LEGACY_TAB_ORDER_KEYS, ...LEGACY_BOOLEAN_KEYS, "pudding.agentConsoleMode", "pudding.workspace.focusLayout.v1"].forEach((entry) => localStorage.removeItem(entry));
  return sessions;
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
  const migrated: Record<string, Record<string, unknown>> = {};
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
      presentation: openSessionIDs.has(sessionID) ? "standard" : "hidden",
      projectTabOpen,
      tabOrder,
    };
  });
  return migrated;
}

function persistSessionStates() { localStorage.setItem(STORAGE_KEY, JSON.stringify(workspaceStore.getState().sessions)); }
