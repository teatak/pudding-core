import assert from "node:assert/strict";
import { test } from "node:test";

const values = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", { value: {
  getItem: (key: string) => values.get(key) ?? null,
  setItem: (key: string, value: string) => values.set(key, value),
  removeItem: (key: string) => values.delete(key),
} });
values.set("pudding.workspace.ui.v5", JSON.stringify({
  migrated: { presentation: "focused", activeApp: "artifacts", apps: {
    project: { activeTab: "file:diff", tabOrder: ["file:diff"] },
    browser: { activeTab: "browser:one", tabOrder: ["browser:two", "browser:one"] },
    artifacts: { activeTab: "canvas:table", tabOrder: ["canvas:table"], closedTabs: { "canvas:closed": "2026-09-05T12:00:00Z" } },
  } },
}));
values.set("pudding.agentConsoleMode", "floating");
values.set("pudding.workspace.focusLayout.v1", JSON.stringify({ chatWidth: 800 }));
const workspace = await import("../src/state/workspaceStore.ts");
const reconcileCanvas = (sessionID: string, ids: string[]) => workspace.updateWorkspaceSessionUI(sessionID, (current) => workspace.resolveCanvasTabs(current, ids.map(id => ({ id, visible: true, updatedAt: "2026-09-05T12:00:00Z" }))));

test("focus starts compact instead of restoring a previously wide conversation", () => {
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-reset"), 340);
});

test("re-entering focus resets a manually widened conversation", () => {
  workspace.setWorkspacePresentation("focus-reset", "focused");
  workspace.setWorkspaceFocusChatWidth("focus-reset", 800);
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-reset"), 800);
  workspace.setWorkspacePresentation("focus-reset", "standard");
  workspace.setWorkspacePresentation("focus-reset", "focused");
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-reset"), 340);
});

test("focus resizing survives app switches, stays session-owned and ends when hidden", () => {
  workspace.setWorkspacePresentation("focus-a", "focused");
  workspace.setWorkspaceFocusChatWidth("focus-a", 460);
  workspace.openWorkspaceView("focus-a", "project");
  workspace.openWorkspaceTab("focus-a", "canvas:table");
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-a"), 460);
  workspace.setWorkspacePresentation("focus-b", "focused");
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-b"), 340);
  workspace.setWorkspaceFocusChatWidth("focus-b", 500);
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-a"), 460);
  workspace.setWorkspaceOpen("focus-a", false);
  workspace.setWorkspacePresentation("focus-a", "focused");
  assert.equal(workspace.getWorkspaceFocusChatWidth("focus-a"), 340);
  const saved = JSON.parse(values.get("pudding.workspace.ui.v7")!);
  assert.equal("focusChatWidths" in saved, false);
  assert.equal("chatWidth" in saved["focus-b"], false);
});

test("v5 migrates independent orders into one persistent order and preserves project and closed canvas", () => {
  assert.deepEqual(workspace.getWorkspaceSessionUI("migrated"), {
    presentation: "focused", activeTab: "canvas:table", tabOrder: ["project", "browser:two", "browser:one", "canvas:table"],
    project: { activeTab: "file:diff", tabOrder: ["file:diff"] }, closedCanvasTabs: { "canvas:closed": "2026-09-05T12:00:00Z" },
  });
  assert.equal(values.has("pudding.workspace.ui.v5"), false);
  assert.equal(values.has("pudding.agentConsoleMode"), false);
  assert.equal("apps" in JSON.parse(values.get("pudding.workspace.ui.v7")!).migrated, false);
  const legacy = workspace.migrateWorkspaceSession({ open: true, activeTab: "file:one", tabOrder: ["browser:a", "file:one", "canvas:b"] });
  assert.equal(legacy.activeTab, "project");
  assert.equal(legacy.project.activeTab, "file:one");
  assert.deepEqual(legacy.tabOrder, ["project", "browser:a", "canvas:b"]);
});

test("project and library preserve the common content order and project preview selection", () => {
  workspace.openWorkspaceTab("switch", "browser:one");
  workspace.openWorkspaceTab("switch", "canvas:table");
  workspace.openWorkspaceTab("switch", "browser:two");
  workspace.openWorkspaceTab("switch", "file:diff");
  workspace.openWorkspaceView("switch", "library");
  workspace.openWorkspaceView("switch", "project");
  const state = workspace.getWorkspaceSessionUI("switch");
  assert.equal(state.activeTab, "project");
  assert.equal(state.project.activeTab, "file:diff");
  assert.deepEqual(state.tabOrder, ["project", "browser:one", "canvas:table", "browser:two"]);
  assert.deepEqual(workspace.getWorkspaceSessionUI("unrelated").tabOrder, ["project"]);
});

test("closing the selected tab chooses its mixed neighbour, then the project after the last tab", () => {
  for (const id of ["browser:left", "canvas:middle", "browser:right"] as const) workspace.openWorkspaceTab("close", id);
  workspace.setWorkspaceActiveTab("close", "canvas:middle");
  workspace.closeWorkspaceTab("close", "canvas:middle");
  assert.equal(workspace.getWorkspaceSessionUI("close").activeTab, "browser:right");
  workspace.closeWorkspaceTab("close", "browser:right");
  assert.equal(workspace.getWorkspaceSessionUI("close").activeTab, "browser:left");
  workspace.closeWorkspaceTab("close", "browser:left");
  assert.equal(workspace.getWorkspaceSessionUI("close").activeTab, "project");
  assert.equal(workspace.getWorkspaceSessionUI("close").presentation, "standard");
});

test("reconcile one resource type keeps the other type and mixed ordering", () => {
  const ids = ["browser:a", "canvas:a", "browser:b", "canvas:b"] as const;
  for (const id of ids) workspace.openWorkspaceTab("reconcile", id);
  workspace.setWorkspaceTabOrder("reconcile", ["canvas:b", "browser:a", "canvas:a", "browser:b"]);
  workspace.reconcileWorkspaceTabs("reconcile", "browser", ["browser:a", "browser:new"]);
  assert.deepEqual(workspace.getWorkspaceSessionUI("reconcile").tabOrder, ["canvas:b", "browser:a", "canvas:a", "browser:new"]);
  reconcileCanvas("reconcile", ["a", "b", "new"]);
  assert.deepEqual(workspace.getWorkspaceSessionUI("reconcile").tabOrder, ["canvas:b", "browser:a", "canvas:a", "browser:new", "canvas:new"]);
});

test("mixed bulk close preserves project files, scopes to its session and keeps canvas content closed on refetch", () => {
  const ids = ["browser:left", "canvas:a", "browser:middle", "canvas:b", "browser:right"] as const;
  for (const sessionID of ["batch-owner", "batch-other"]) {
    for (const id of ids) workspace.openWorkspaceTab(sessionID, id);
    workspace.openWorkspaceTab(sessionID, "file:diff");
  }
  workspace.setWorkspaceActiveTab("batch-owner", "canvas:a");
  workspace.closeWorkspaceTabs("batch-owner", ["canvas:a", "browser:middle", "canvas:b"]);
  reconcileCanvas("batch-owner", ["a", "b"]);
  const state = workspace.getWorkspaceSessionUI("batch-owner");
  assert.equal(state.activeTab, "browser:right");
  assert.deepEqual(state.tabOrder, ["project", "browser:left", "browser:right"]);
  assert.equal(state.project.activeTab, "file:diff");
  assert.equal(Object.keys(state.closedCanvasTabs).length, 2);
  assert.equal(new Set(Object.values(state.closedCanvasTabs)).size, 1);
  assert.deepEqual(workspace.getWorkspaceSessionUI("batch-other").tabOrder, ["project", ...ids]);
});

test("closing an inactive resource while the library is shown does not change the view", () => {
  workspace.openWorkspaceTab("library", "browser:one");
  workspace.openWorkspaceView("library", "library");
  workspace.closeWorkspaceTab("library", "browser:one");
  assert.equal(workspace.getWorkspaceSessionUI("library").activeTab, "library");
});

test("the resource page never becomes a tab, including after restoring saved UI", () => {
  const id = "resource-page";
  const restored = workspace.migrateWorkspaceSession({
    activeTab: "library", tabOrder: ["project", "library", "browser:one"],
    project: { activeTab: "file:draft", tabOrder: ["file:draft"] },
  }, false);
  workspace.replaceWorkspaceSessionUI(id, restored);
  workspace.openWorkspaceView(id, "library");
  workspace.openWorkspaceView(id, "library");
  assert.equal(workspace.getWorkspaceSessionUI(id).activeTab, "library");
  assert.deepEqual(workspace.getWorkspaceSessionUI(id).tabOrder, ["project", "browser:one"]);
  workspace.openWorkspaceView(id, "project");
  assert.equal(workspace.getWorkspaceSessionUI(id).project.activeTab, "file:draft");
  workspace.closeWorkspaceTabs(id, ["project", "browser:one"]);
  workspace.openWorkspaceView(id, "library");
  assert.equal(workspace.getWorkspaceSessionUI(id).activeTab, "library");
  assert.deepEqual(workspace.getWorkspaceSessionUI(id).tabOrder, []);
});

test("legacy invisible canvas seeds closed state once; explicit reveal reopens the same item", () => {
  const items = [{ id: "legacy", visible: false, updatedAt: "2026-09-05T12:00:00Z" }];
  const reconcile = () => workspace.updateWorkspaceSessionUI("legacy-canvas", current => workspace.resolveCanvasTabs(current, items));
  reconcile();
  assert.deepEqual(workspace.getWorkspaceSessionUI("legacy-canvas").tabOrder, ["project"]);
  workspace.openWorkspaceTab("legacy-canvas", "canvas:legacy");
  reconcile();
  assert.deepEqual(workspace.getWorkspaceSessionUI("legacy-canvas").tabOrder, ["project", "canvas:legacy"]);
  workspace.closeWorkspaceTab("legacy-canvas", "canvas:legacy");
  reconcile();
  assert.deepEqual(workspace.getWorkspaceSessionUI("legacy-canvas").tabOrder, ["project"]);
});

test("project preview reconciliation and close stay inside the project", () => {
  workspace.openWorkspaceTab("preview", "file:diff");
  workspace.reconcileWorkspaceTabs("preview", "project", ["file:diff", "canvas:one", "browser:one"]);
  assert.deepEqual(workspace.getWorkspaceSessionUI("preview").project.tabOrder, ["file:diff"]);
  workspace.openWorkspaceTab("preview", "browser:one");
  workspace.closeWorkspaceTab("preview", "file:diff");
  assert.equal(workspace.getWorkspaceSessionUI("preview").activeTab, "browser:one");
  assert.equal(workspace.getWorkspaceSessionUI("preview").project.activeTab, "project");
});

test("renderer restart preserves mixed drag order, selected resource and closed canvas", async () => {
  for (const id of ["browser:a", "canvas:a", "browser:b", "canvas:b"] as const) workspace.openWorkspaceTab("restart", id);
  workspace.setWorkspaceTabOrder("restart", ["canvas:b", "browser:b", "canvas:a", "browser:a"]);
  workspace.setWorkspaceActiveTab("restart", "browser:b");
  workspace.closeWorkspaceTab("restart", "canvas:a");
  const fresh = await import(new URL("../src/state/workspaceStore.ts?unified-restart", import.meta.url).href);
  const state = fresh.getWorkspaceSessionUI("restart");
  assert.equal(state.activeTab, "browser:b");
  assert.deepEqual(state.tabOrder, ["canvas:b", "browser:b", "browser:a"]);
  fresh.updateWorkspaceSessionUI("restart", current => fresh.resolveCanvasTabs(current, ["a", "b"].map(id => ({ id, visible: true, updatedAt: "2026-09-05T12:00:00Z" }))));
  assert.deepEqual(fresh.getWorkspaceSessionUI("restart").tabOrder, state.tabOrder);
  fresh.openWorkspaceTab("restart", "canvas:a");
  assert.deepEqual(fresh.getWorkspaceSessionUI("restart").tabOrder, [...state.tabOrder, "canvas:a"]);
});

const { resolveCenteredLayoutPresentation } = await import("../src/lib/centeredLayout.ts");
const constraints = { dockedMinimumWidth: 800, railChatMinimumWidth: 688, thirdColumnMinimumWidth: 380 };

test("focus keeps the left conversation beside the workspace at every desktop width", () => {
  for (const layoutWidth of [560, 720, 1024, 1440]) {
    assert.deepEqual(resolveCenteredLayoutPresentation({ constraints, layoutWidth, leftGroupRatio: 0.55, workspaceDockRequested: true, focused: true }), {
      railResponsiveCollapsed: true, workspaceOverlay: false,
    });
  }
});

test("leaving focus restores normal responsive layout without a forced collapsed rail", () => {
  assert.deepEqual(resolveCenteredLayoutPresentation({ constraints, layoutWidth: 1440, leftGroupRatio: 0.55, workspaceDockRequested: true, focused: false }), {
    railResponsiveCollapsed: false, workspaceOverlay: false,
  });
  assert.deepEqual(resolveCenteredLayoutPresentation({ constraints, layoutWidth: 560, leftGroupRatio: 0.55, workspaceDockRequested: true, focused: false }), {
    railResponsiveCollapsed: true, workspaceOverlay: true,
  });
});


test("project closes like a resource, preserves file state, stays closed on reload and explicitly reopens", async () => {
  workspace.openWorkspaceTab("project-close", "file:diff");
  workspace.reconcileWorkspaceTabs("project-close", "project", ["file:diff"]);
  workspace.openWorkspaceTab("project-close", "browser:one");
  workspace.openWorkspaceView("project-close", "project");
  workspace.closeWorkspaceTab("project-close", "project");
  let state = workspace.getWorkspaceSessionUI("project-close");
  assert.equal(state.activeTab, "browser:one");
  assert.deepEqual(state.project, { activeTab: "file:diff", tabOrder: ["file:diff"] });
  const fresh = await import(new URL("../src/state/workspaceStore.ts?project-closed", import.meta.url).href);
  assert.deepEqual(fresh.getWorkspaceSessionUI("project-close").tabOrder, ["browser:one"]);
  fresh.closeWorkspaceTab("project-close", "browser:one");
  assert.equal(fresh.getWorkspaceSessionUI("project-close").activeTab, "library");
  fresh.openWorkspaceView("project-close", "project");
  state = fresh.getWorkspaceSessionUI("project-close");
  assert.equal(state.project.activeTab, "file:diff");
  assert.deepEqual(state.tabOrder, ["project"]);
});
