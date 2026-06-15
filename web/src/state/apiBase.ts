import { consumeLaunchParam } from "@/state/launchParams";

const API_BASE_KEY = "pudding.apiBase";

let apiBase = "";

export function initAPIBase() {
  apiBase = readInitialAPIBase();
  return apiBase;
}

export function apiURL(path: string) {
  if (!apiBase) {
    return path;
  }
  return `${apiBase}${path.startsWith("/") ? path : `/${path}`}`;
}

function readInitialAPIBase() {
  const fromURL = normalizeLoopbackHTTPBase(consumeLaunchParam("api"));
  if (fromURL) {
    sessionStorage.setItem(API_BASE_KEY, fromURL);
    return fromURL;
  }
  return normalizeLoopbackHTTPBase(import.meta.env.VITE_PUDDING_API_BASE) ||
    normalizeLoopbackHTTPBase(sessionStorage.getItem(API_BASE_KEY) || "");
}

function normalizeLoopbackHTTPBase(value: string | undefined) {
  const raw = (value || "").trim();
  if (!raw) {
    return "";
  }
  try {
    const url = new URL(raw);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return "";
    }
    if (!isLoopbackHost(url.hostname)) {
      return "";
    }
    url.pathname = url.pathname.replace(/\/+$/, "");
    url.search = "";
    url.hash = "";
    return url.toString().replace(/\/$/, "");
  } catch {
    return "";
  }
}

function isLoopbackHost(hostname: string) {
  const host = hostname.toLowerCase();
  return host === "localhost" || host === "127.0.0.1" || host === "::1" || host === "[::1]";
}
