import { useMemo, useState } from "react";

import type { ProjectBrowserRoot } from "@/api/client";

import { projectPathContains, projectTabKey, replaceProjectPath } from "./projectPaths";
import type { ProjectGitDiffSelection, ProjectSelection, ProjectTab } from "./types";

type ProjectWorkspace = {
  activeKey?: string;
  expandedKeys: string[];
  tabs: ProjectTab[];
};

const EMPTY_WORKSPACE: ProjectWorkspace = { expandedKeys: [], tabs: [] };
const PROJECT_WORKSPACE_STORAGE_KEY = "pudding.project.workspace.v1";

export function useProjectWorkspace(sessionID: string) {
  const [bySession, setBySession] = useState<Record<string, ProjectWorkspace>>(readProjectWorkspaces);
  const workspace = bySession[sessionID] || EMPTY_WORKSPACE;
  const selected = useMemo(
    () => workspace.tabs.find((tab) => projectTabKey(tab) === workspace.activeKey),
    [workspace.activeKey, workspace.tabs],
  );

  const updateSession = (targetSessionID: string, change: (current: ProjectWorkspace) => ProjectWorkspace) => {
    setBySession((current) => {
      const next = { ...current, [targetSessionID]: change(current[targetSessionID] || EMPTY_WORKSPACE) };
      writeProjectWorkspaces(next);
      return next;
    });
  };

  const update = (change: (current: ProjectWorkspace) => ProjectWorkspace) => updateSession(sessionID, change);

  const revealInSession = (targetSessionID: string, selection: ProjectSelection) => {
    updateSession(targetSessionID, (current) => {
      const expanded = new Set(current.expandedKeys);
      expanded.add(`${selection.rootID}:.`);
      const parents = selection.path.split("/").slice(0, -1);
      parents.forEach((_part, index) => expanded.add(`${selection.rootID}:${parents.slice(0, index + 1).join("/")}`));
      return { ...current, expandedKeys: Array.from(expanded) };
    });
  };

  const reveal = (selection: ProjectSelection) => revealInSession(sessionID, selection);

  const openTabInSession = (targetSessionID: string, selection: ProjectSelection | ProjectGitDiffSelection, pinned: boolean) => {
    const candidate = { ...selection, pinned } as ProjectTab;
    const key = projectTabKey(candidate);
    updateSession(targetSessionID, (current) => {
      const tabs = [...current.tabs];
      const existingIndex = tabs.findIndex((tab) => projectTabKey(tab) === key);
      if (existingIndex >= 0) {
        if (pinned && !tabs[existingIndex].pinned) {
          tabs[existingIndex] = { ...tabs[existingIndex], pinned: true };
        }
      } else {
        const previewIndex = pinned ? -1 : tabs.findIndex((tab) => !tab.pinned);
        if (previewIndex >= 0) {
          tabs[previewIndex] = candidate;
        } else {
          tabs.push(candidate);
        }
      }
      return { ...current, activeKey: key, tabs };
    });
    revealInSession(targetSessionID, selection);
  };

  const openInSession = (targetSessionID: string, selection: ProjectSelection, pinned: boolean) => {
    openTabInSession(targetSessionID, selection, pinned);
  };

  const open = (selection: ProjectSelection, pinned: boolean) => openInSession(sessionID, selection, pinned);

  const activate = (selection: ProjectTab) => {
    update((current) => ({ ...current, activeKey: projectTabKey(selection) }));
    reveal(selection);
  };

  const closeKeysInSession = (targetSessionID: string, keys: string[]) => {
    const closing = new Set(keys);
    updateSession(targetSessionID, (current) => {
      const activeIndex = current.tabs.findIndex((tab) => projectTabKey(tab) === current.activeKey);
      const tabs = current.tabs.filter((tab) => !closing.has(projectTabKey(tab)));
      let activeKey = current.activeKey;
      if (activeKey && closing.has(activeKey)) {
        const fallback = tabs[Math.min(Math.max(activeIndex, 0), tabs.length - 1)];
        activeKey = fallback ? projectTabKey(fallback) : undefined;
      }
      return { ...current, activeKey, tabs };
    });
  };

  const closeKeys = (keys: string[]) => closeKeysInSession(sessionID, keys);

  const renameUnderInSession = (targetSessionID: string, target: ProjectSelection, nextPath: string) => {
    updateSession(targetSessionID, (current) => {
      const tabs = current.tabs.map((tab) => projectPathContains(target, tab)
        ? { ...tab, path: replaceProjectPath(tab.path, target.path, nextPath) }
        : tab);
      const activeTab = current.tabs.find((tab) => projectTabKey(tab) === current.activeKey);
      const activeKey = activeTab && projectPathContains(target, activeTab)
        ? projectTabKey({ ...activeTab, path: replaceProjectPath(activeTab.path, target.path, nextPath) })
        : current.activeKey;
      const expandedKeys = current.expandedKeys.map((key) => {
        const prefix = `${target.rootID}:`;
        if (!key.startsWith(prefix)) {
          return key;
        }
        return `${prefix}${replaceProjectPath(key.slice(prefix.length), target.path, nextPath)}`;
      });
      return { ...current, activeKey, expandedKeys, tabs };
    });
  };

  const moveUnderInSession = (targetSessionID: string, target: ProjectSelection, destination: ProjectSelection) => {
    updateSession(targetSessionID, (current) => {
      const relocate = <T extends ProjectTab>(tab: T): T => projectPathContains(target, tab)
        ? { ...tab, rootID: destination.rootID, path: replaceProjectPath(tab.path, target.path, destination.path) }
        : tab;
      const tabs = current.tabs.map(relocate);
      const activeTab = current.tabs.find((tab) => projectTabKey(tab) === current.activeKey);
      const activeKey = activeTab ? projectTabKey(relocate(activeTab)) : current.activeKey;
      const expandedKeys = current.expandedKeys.map((key) => {
        const separator = key.indexOf(":");
        if (separator < 0) return key;
        const rootID = key.slice(0, separator);
        const path = key.slice(separator + 1);
        if (!projectPathContains(target, { rootID, path })) return key;
        return `${destination.rootID}:${replaceProjectPath(path, target.path, destination.path)}`;
      });
      return { ...current, activeKey, expandedKeys: Array.from(new Set(expandedKeys)), tabs };
    });
  };

  return {
    ...workspace,
    selected,
    activate,
    closeKeys,
    closeKeysInSession,
    closeUnder: (target: ProjectSelection) => closeKeys(workspace.tabs.filter((tab) => projectPathContains(target, tab)).map(projectTabKey)),
    ensureRootExpanded: (roots: ProjectBrowserRoot[]) => {
      if (roots.length === 0 || workspace.expandedKeys.length > 0) {
        return;
      }
      update((current) => ({ ...current, expandedKeys: [`${roots[0].id}:.`] }));
    },
    openPinned: (selection: ProjectSelection) => open(selection, true),
    pinTab: (selection: ProjectTab) => openTabInSession(sessionID, selection, true),
    openPinnedInSession: (targetSessionID: string, selection: ProjectSelection) => openInSession(targetSessionID, selection, true),
    openPreview: (selection: ProjectSelection) => open(selection, false),
    openGitDiff: (selection: ProjectGitDiffSelection, pinned = false) => openTabInSession(sessionID, selection, pinned),
    removeUnavailableRoots: (roots: ProjectBrowserRoot[]) => {
      if (roots.length === 0) {
        return;
      }
      const allowed = new Set(roots.map((root) => root.id));
      const expandedAvailable = workspace.expandedKeys.every((key) => allowed.has(key.slice(0, key.indexOf(":"))));
      if (workspace.tabs.every((tab) => allowed.has(tab.rootID)) && expandedAvailable) {
        return;
      }
      update((current) => {
        const tabs = current.tabs.filter((tab) => allowed.has(tab.rootID));
        const expandedKeys = current.expandedKeys.filter((key) => allowed.has(key.slice(0, key.indexOf(":"))));
        const activeKey = tabs.some((tab) => projectTabKey(tab) === current.activeKey)
          ? current.activeKey
          : tabs.at(-1) ? projectTabKey(tabs.at(-1)!) : undefined;
        return { ...current, activeKey, expandedKeys, tabs };
      });
    },
    renameUnder: (target: ProjectSelection, nextPath: string) => renameUnderInSession(sessionID, target, nextPath),
    renameUnderInSession,
    moveUnderInSession,
    reveal,
    toggleDirectory: (rootID: string, path: string) => update((current) => {
      const key = `${rootID}:${path}`;
      const expandedKeys = current.expandedKeys.includes(key)
        ? current.expandedKeys.filter((item) => item !== key)
        : [...current.expandedKeys, key];
      return { ...current, expandedKeys };
    }),
  };
}

function readProjectWorkspaces(): Record<string, ProjectWorkspace> {
  if (typeof window === "undefined") return {};
  try {
    const value = JSON.parse(window.localStorage.getItem(PROJECT_WORKSPACE_STORAGE_KEY) || "{}") as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    return Object.fromEntries(Object.entries(value).flatMap(([sessionID, raw]) => {
      if (!raw || typeof raw !== "object" || Array.isArray(raw)) return [];
      const candidate = raw as Partial<ProjectWorkspace>;
      const tabs = Array.isArray(candidate.tabs) ? candidate.tabs.filter(isProjectTab).slice(-50) : [];
      const expandedKeys = Array.isArray(candidate.expandedKeys)
        ? candidate.expandedKeys.filter((key): key is string => typeof key === "string").slice(-500)
        : [];
      const activeKey = typeof candidate.activeKey === "string" && tabs.some((tab) => projectTabKey(tab) === candidate.activeKey)
        ? candidate.activeKey
        : tabs.at(-1) ? projectTabKey(tabs.at(-1)!) : undefined;
      return [[sessionID, { activeKey, expandedKeys, tabs }]];
    }));
  } catch {
    return {};
  }
}

function writeProjectWorkspaces(value: Record<string, ProjectWorkspace>) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(PROJECT_WORKSPACE_STORAGE_KEY, JSON.stringify(value));
  } catch {
    // Best-effort UI preference only.
  }
}

function isProjectTab(value: unknown): value is ProjectTab {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const tab = value as Partial<ProjectTab> & { view?: unknown; staged?: unknown };
  if (typeof tab.rootID !== "string" || typeof tab.path !== "string" || typeof tab.pinned !== "boolean") return false;
  if (tab.view === undefined) return true;
  return tab.view === "git-diff" && typeof tab.staged === "boolean";
}
