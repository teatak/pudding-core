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

type WorkspaceActivityState = {
  activities: Record<string, WorkspaceActivity[] | undefined>;
  clear: (sessionID: string, kind?: WorkspaceActivity["kind"]) => void;
  record: (activity: WorkspaceActivityInput) => void;
  retain: (sessionID: string, kind: WorkspaceActivity["kind"], resourceIDs: string[]) => void;
  updateBrowserMetadata: (
    sessionID: string,
    resourceID: string,
    metadata: BrowserWorkspaceActivityMetadata,
  ) => void;
};

let activitySerial = 0;
const maxActivitiesPerSession = 8;
const emptyActivities: WorkspaceActivity[] = [];

const useWorkspaceActivityStore = create<WorkspaceActivityState>((set) => ({
  activities: {},
  clear: (sessionID, kind) =>
    set((state) => {
      const current = state.activities[sessionID] || [];
      if (current.length === 0) {
        return state;
      }
      const remaining = kind ? current.filter((activity) => activity.kind !== kind) : [];
      if (remaining.length === current.length) {
        return state;
      }
      return {
        activities: {
          ...state.activities,
          [sessionID]: remaining,
        },
      };
    }),
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
          [activity.sessionID]: [nextActivity, ...remaining].slice(0, maxActivitiesPerSession),
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

export function clearWorkspaceActivity(sessionID: string, kind?: WorkspaceActivity["kind"]) {
  useWorkspaceActivityStore.getState().clear(sessionID, kind);
}

export function retainWorkspaceActivities(
  sessionID: string,
  kind: WorkspaceActivity["kind"],
  resourceIDs: string[],
) {
  useWorkspaceActivityStore.getState().retain(sessionID, kind, resourceIDs);
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
