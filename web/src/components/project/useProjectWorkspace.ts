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

export function useProjectWorkspace(sessionID: string) {
  const [bySession, setBySession] = useState<Record<string, ProjectWorkspace>>({});
  const workspace = bySession[sessionID] || EMPTY_WORKSPACE;
  const selected = useMemo(
    () => workspace.tabs.find((tab) => projectTabKey(tab) === workspace.activeKey),
    [workspace.activeKey, workspace.tabs],
  );

  const updateSession = (targetSessionID: string, change: (current: ProjectWorkspace) => ProjectWorkspace) => {
    setBySession((current) => ({ ...current, [targetSessionID]: change(current[targetSessionID] || EMPTY_WORKSPACE) }));
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
      if (workspace.tabs.every((tab) => allowed.has(tab.rootID))) {
        return;
      }
      update((current) => {
        const tabs = current.tabs.filter((tab) => allowed.has(tab.rootID));
        const activeKey = tabs.some((tab) => projectTabKey(tab) === current.activeKey)
          ? current.activeKey
          : tabs.at(-1) ? projectTabKey(tabs.at(-1)!) : undefined;
        return { ...current, activeKey, tabs };
      });
    },
    renameUnder: (target: ProjectSelection, nextPath: string) => renameUnderInSession(sessionID, target, nextPath),
    renameUnderInSession,
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
