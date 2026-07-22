// 路由 search 参数形状,与 main.tsx 的 validateSearch 保持同步。
// TanStack Router 的函数式 search 更新器把 prev 推导成含父路由 `{}` 的
// 联合类型,各更新器先用此类型收窄再操作。
export type AppSearch = {
  session?: string;
  draft?: string;
  project?: string;
  split?: string;
  view?: "apps" | "projects";
};

const lastRouteStorageKey = "pudding.lastRoute.v1";
const appRouteKeys = ["session", "draft", "project", "split", "view"] as const;

export function restoreInitialAppRoute() {
  const current = normalizeAppRoute(Object.fromEntries(new URL(window.location.href).searchParams));
  if (current) {
    return;
  }
  const saved = readLastAppRoute();
  if (!saved) {
    return;
  }
  const url = new URL(window.location.href);
  appRouteKeys.forEach((key) => url.searchParams.delete(key));
  Object.entries(saved).forEach(([key, value]) => {
    if (value) {
      url.searchParams.set(key, value);
    }
  });
  window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
}

export function saveLastAppRoute(search: AppSearch) {
  const normalized = normalizeAppRoute(search);
  if (!normalized) {
    return;
  }
  try {
    const next = JSON.stringify(normalized);
    if (window.localStorage.getItem(lastRouteStorageKey) !== next) {
      window.localStorage.setItem(lastRouteStorageKey, next);
    }
  } catch {
    // localStorage 不可用时继续使用当前路由,不影响应用启动。
  }
}

function readLastAppRoute() {
  try {
    return normalizeAppRoute(JSON.parse(window.localStorage.getItem(lastRouteStorageKey) || "null"));
  } catch {
    return null;
  }
}

function normalizeAppRoute(value: unknown): AppSearch | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const route = value as Record<string, unknown>;
  if (route.view === "apps" || route.view === "projects") {
    return { view: route.view };
  }
  const session = routeValue(route.session);
  if (session) {
    const split = routeValue(route.split);
    return split && split !== session ? { session, split } : { session };
  }
  if (route.draft === "1") {
    const project = routeValue(route.project);
    return project ? { draft: "1", project } : { draft: "1" };
  }
  return null;
}

function routeValue(value: unknown) {
  if (typeof value !== "string") {
    return undefined;
  }
  const normalized = value.trim();
  return normalized && normalized.length <= 512 ? normalized : undefined;
}
