import type { Project, Session } from "@/api/client";

const collapsedSessionGroupsStorageKey = "pudding.sessionRail.collapsedGroups";
const projectSortModeStorageKey = "pudding.sessionRail.projectSortMode";
const customProjectOrderStorageKey = "pudding.sessionRail.customProjectOrder";

export type ProjectSortMode = "activity" | "name" | "name-desc" | "custom";

export type ProjectSessionGroup = {
  key: string;
  label: string;
  project?: Project;
  projectID?: string;
  sessions: Session[];
  lastActivity: number;
};

export function sortPinnedSessions(sessions: Session[]) {
  return [...sessions].sort((left, right) => {
    const leftOrder = left.pinnedOrder || Number.MAX_SAFE_INTEGER;
    const rightOrder = right.pinnedOrder || Number.MAX_SAFE_INTEGER;
    if (leftOrder !== rightOrder) {
      return leftOrder - rightOrder;
    }
    return sessionActivityTime(right) - sessionActivityTime(left);
  });
}

export function sortSessionsByActivity(sessions: Session[]) {
  return [...sessions].sort((left, right) => sessionActivityTime(right) - sessionActivityTime(left));
}

export function groupProjectSessions(projects: Project[], sessions: Session[]): ProjectSessionGroup[] {
  const groups = new Map<string, ProjectSessionGroup>();
  for (const project of projects) {
    groups.set(project.id, {
      key: project.id,
      label: project.name || basename(project.rootDirs[0] || project.id),
      project,
      projectID: project.id,
      sessions: [],
      lastActivity: new Date(project.updatedAt || project.createdAt).getTime(),
    });
  }
  for (const session of sessions) {
    const key = session.projectID || "__missing_project__";
    const existing = groups.get(key);
    if (existing) {
      existing.sessions.push(session);
      existing.lastActivity = Math.max(existing.lastActivity, sessionActivityTime(session));
      continue;
    }
    groups.set(key, {
      key,
      label: tProjectFallbackLabel(session.projectID),
      projectID: session.projectID,
      sessions: [session],
      lastActivity: sessionActivityTime(session),
    });
  }
  return Array.from(groups.values()).map((group) => ({
    ...group,
    sessions: sortSessionsByActivity(group.sessions),
  }));
}

export function sortProjectGroups(
  groups: ProjectSessionGroup[],
  mode: ProjectSortMode,
  customOrder: string[],
) {
  if (mode === "name" || mode === "name-desc") {
    return [...groups].sort((left, right) =>
      (mode === "name" ? left.label : right.label).localeCompare(
        mode === "name" ? right.label : left.label,
        undefined,
        { numeric: true, sensitivity: "base" },
      ),
    );
  }
  if (mode === "custom") {
    const order = normalizeProjectOrder(groups, customOrder);
    const orderByKey = new Map(order.map((key, index) => [key, index]));
    return [...groups].sort(
      (left, right) =>
        (orderByKey.get(left.key) ?? Number.MAX_SAFE_INTEGER) -
        (orderByKey.get(right.key) ?? Number.MAX_SAFE_INTEGER),
    );
  }
  return [...groups].sort((left, right) => right.lastActivity - left.lastActivity);
}

export function normalizeProjectOrder(groups: ProjectSessionGroup[], order: string[]) {
  const groupKeys = new Set(groups.map((group) => group.key));
  const next = order.filter((key, index) => groupKeys.has(key) && order.indexOf(key) === index);
  for (const group of groups) {
    if (!next.includes(group.key)) {
      next.push(group.key);
    }
  }
  return next;
}

export function readCollapsedSessionGroups() {
  if (typeof window === "undefined") {
    return new Set<string>();
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(collapsedSessionGroupsStorageKey) || "[]");
    if (!Array.isArray(parsed)) {
      return new Set<string>();
    }
    return new Set(parsed.filter((value): value is string => typeof value === "string"));
  } catch {
    return new Set<string>();
  }
}

export function writeCollapsedSessionGroups(groups: Set<string>) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(collapsedSessionGroupsStorageKey, JSON.stringify([...groups]));
  } catch {
    // localStorage 可能被禁用;折叠状态继续保留在内存态。
  }
}

export function readProjectSortMode(): ProjectSortMode {
  if (typeof window === "undefined") {
    return "activity";
  }
  try {
    const stored = window.localStorage.getItem(projectSortModeStorageKey);
    return stored === "name" || stored === "name-desc" || stored === "custom"
      ? stored
      : "activity";
  } catch {
    return "activity";
  }
}

export function writeProjectSortMode(mode: ProjectSortMode) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(projectSortModeStorageKey, mode);
  } catch {
    // localStorage 可能被禁用;排序偏好继续保留在内存态。
  }
}

export function readCustomProjectOrder() {
  if (typeof window === "undefined") {
    return [];
  }
  try {
    const parsed = JSON.parse(window.localStorage.getItem(customProjectOrderStorageKey) || "[]");
    return Array.isArray(parsed)
      ? parsed.filter((value): value is string => typeof value === "string")
      : [];
  } catch {
    return [];
  }
}

export function writeCustomProjectOrder(order: string[]) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(customProjectOrderStorageKey, JSON.stringify(order));
  } catch {
    // localStorage 可能被禁用;自定义顺序继续保留在内存态。
  }
}

function tProjectFallbackLabel(projectID: string | undefined) {
  return projectID || "Project";
}

function sessionActivityTime(session: Session) {
  return new Date(session.lastActivityAt || session.createdAt).getTime();
}

function basename(path: string) {
  const normalized = path.replace(/\/+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}
