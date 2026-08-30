import { create } from "zustand";

export type BrowserWorkspaceActivity = {
  faviconURL?: string;
  kind: "browser";
  resourceID?: string;
  sessionID: string;
  title?: string;
  toolName?: string;
  url?: string;
};

export type CanvasWorkspaceActivity = {
  kind: "canvas";
  resourceID: string;
  resourceKind: string;
  sessionID: string;
  title?: string;
};

export type WorkspaceActivity = (BrowserWorkspaceActivity | CanvasWorkspaceActivity) & {
  serial: number;
};

type WorkspaceActivityInput = BrowserWorkspaceActivity | CanvasWorkspaceActivity;
type BrowserWorkspaceActivityMetadata = Pick<BrowserWorkspaceActivity, "faviconURL" | "title" | "url">;
type BrowserWorkspaceResource = BrowserWorkspaceActivityMetadata & { resourceID: string };

type WorkspaceActivityState = {
  activities: Record<string, WorkspaceActivity[] | undefined>;
  record: (activity: WorkspaceActivityInput) => void;
  retain: (sessionID: string, kind: WorkspaceActivity["kind"], resourceIDs: string[]) => void;
  syncBrowsers: (sessionID: string, resources: BrowserWorkspaceResource[]) => void;
  updateBrowserMetadata: (
    sessionID: string,
    resourceID: string,
    metadata: BrowserWorkspaceActivityMetadata,
  ) => void;
};

let activitySerial = 0;
const emptyActivities: WorkspaceActivity[] = [];

const useWorkspaceActivityStore = create<WorkspaceActivityState>((set) => ({
  activities: {},
  record: (activity) =>
    set((state) => {
      activitySerial += 1;
      const current = state.activities[activity.sessionID] || [];
      const nextActivity = {
        ...normalizeWorkspaceActivity(activity),
        serial: activitySerial,
      } as WorkspaceActivity;
      const remaining = current.filter((entry) => !sameWorkspaceResource(entry, nextActivity));
      return {
        activities: {
          ...state.activities,
          [activity.sessionID]: [nextActivity, ...remaining],
        },
      };
    }),
  retain: (sessionID, kind, resourceIDs) =>
    set((state) => {
      const current = state.activities[sessionID] || [];
      const allowed = new Set(resourceIDs);
      const remaining = current.filter(
        (activity) => activity.kind !== kind || Boolean(activity.resourceID && allowed.has(activity.resourceID)),
      );
      return remaining.length === current.length
        ? state
        : { activities: { ...state.activities, [sessionID]: remaining } };
    }),
  syncBrowsers: (sessionID, resources) =>
    set((state) => {
      const current = state.activities[sessionID] || [];
      const existingByID = new Map(
        current.flatMap((activity) => (
          activity.kind === "browser" && activity.resourceID
            ? [[activity.resourceID, activity] as const]
            : []
        )),
      );
      const browsers = resources.map((resource) => {
        const existing = existingByID.get(resource.resourceID);
        const normalized = normalizeWorkspaceActivity({
          ...resource,
          kind: "browser",
          sessionID,
        }) as BrowserWorkspaceActivity;
        if (existing) {
          return {
            ...existing,
            ...normalized,
          };
        }
        activitySerial += 1;
        return {
          ...normalized,
          serial: activitySerial,
        };
      });
      const next = [
        ...browsers,
        ...current.filter((activity) => activity.kind !== "browser"),
      ];
      return sameWorkspaceActivities(current, next)
        ? state
        : { activities: { ...state.activities, [sessionID]: next } };
    }),
  updateBrowserMetadata: (sessionID, resourceID, metadata) =>
    set((state) => {
      const current = state.activities[sessionID] || [];
      let changed = false;
      const next = current.map((activity) => {
        if (activity.kind !== "browser" || activity.resourceID !== resourceID) {
          return activity;
        }
        const url = metadata.url?.trim() || activity.url;
        const urlChanged = url !== activity.url;
        const title = normalizeBrowserTitle(metadata.title, url)
          || (urlChanged ? undefined : activity.title);
        const faviconURL = metadata.faviconURL?.trim() || undefined;
        if (
          title === activity.title
          && url === activity.url
          && faviconURL === activity.faviconURL
        ) {
          return activity;
        }
        changed = true;
        return {
          ...activity,
          faviconURL,
          title,
          url,
        };
      });
      return changed
        ? { activities: { ...state.activities, [sessionID]: next } }
        : state;
    }),
}));

export function recordWorkspaceActivity(activity: WorkspaceActivityInput) {
  useWorkspaceActivityStore.getState().record(activity);
}

export function retainWorkspaceActivities(
  sessionID: string,
  kind: WorkspaceActivity["kind"],
  resourceIDs: string[],
) {
  useWorkspaceActivityStore.getState().retain(sessionID, kind, resourceIDs);
}

export function syncBrowserWorkspaceActivities(
  sessionID: string,
  resources: BrowserWorkspaceResource[],
) {
  useWorkspaceActivityStore.getState().syncBrowsers(sessionID, resources);
}

export function updateBrowserWorkspaceActivity(
  sessionID: string,
  resourceID: string,
  metadata: BrowserWorkspaceActivityMetadata,
) {
  useWorkspaceActivityStore.getState().updateBrowserMetadata(sessionID, resourceID, metadata);
}

export function useWorkspaceActivities(sessionID: string | undefined) {
  return useWorkspaceActivityStore((state) => (sessionID ? state.activities[sessionID] || emptyActivities : emptyActivities));
}

function normalizeWorkspaceActivity(activity: WorkspaceActivityInput): WorkspaceActivityInput {
  if (activity.kind !== "browser") {
    return activity;
  }
  const url = activity.url?.trim() || undefined;
  return {
    ...activity,
    faviconURL: activity.faviconURL?.trim() || undefined,
    title: normalizeBrowserTitle(activity.title, url),
    url,
  };
}

function normalizeBrowserTitle(title: string | undefined, url: string | undefined) {
  const value = title?.trim();
  if (!value || value === "about:blank" || value === url?.trim() || browserTitleIsURL(value)) {
    return undefined;
  }
  return value;
}

function browserTitleIsURL(value: string) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

function sameWorkspaceResource(left: WorkspaceActivity, right: WorkspaceActivity) {
  if (left.kind !== right.kind) {
    return false;
  }
  if (left.resourceID || right.resourceID) {
    return left.resourceID === right.resourceID;
  }
  return left.kind === "browser" && right.kind === "browser" && left.url === right.url;
}

function sameWorkspaceActivities(current: WorkspaceActivity[], next: WorkspaceActivity[]) {
  return current.length === next.length && current.every((activity, index) => {
    const candidate = next[index];
    if (activity === candidate) {
      return true;
    }
    return activity.kind === "browser"
      && candidate.kind === "browser"
      && activity.resourceID === candidate.resourceID
      && activity.serial === candidate.serial
      && activity.title === candidate.title
      && activity.url === candidate.url
      && activity.faviconURL === candidate.faviconURL;
  });
}
