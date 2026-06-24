import { consumeLaunchParam } from "@/state/launchParams";

const TOKEN_KEY = "pudding.daemonToken";
const PAIRING_CODE_KEY = "pudding.pendingPairingCode";

export function initialToken() {
  // daemon 启动日志给出 /?token=… 一键入口;读到后立刻从地址栏清掉,
  // 避免 token 留在历史记录/书签里
  const fromURL = consumeLaunchParam("token");
  if (fromURL) {
    sessionStorage.setItem(TOKEN_KEY, fromURL);
    return fromURL;
  }
  const pairingCode = consumeLaunchParam("pair");
  if (pairingCode) {
    sessionStorage.setItem(PAIRING_CODE_KEY, pairingCode);
    sessionStorage.removeItem(TOKEN_KEY);
    return "";
  }
  return import.meta.env.VITE_PUDDING_TOKEN || sessionStorage.getItem(TOKEN_KEY) || "";
}

export function saveToken(token: string) {
  const clean = token.trim();
  if (clean) {
    sessionStorage.setItem(TOKEN_KEY, clean);
  } else {
    sessionStorage.removeItem(TOKEN_KEY);
  }
  window.dispatchEvent(new CustomEvent("pudding-token-change", { detail: clean }));
}

export function pendingPairingCode() {
  return sessionStorage.getItem(PAIRING_CODE_KEY) || "";
}

export function clearPendingPairingCode() {
  sessionStorage.removeItem(PAIRING_CODE_KEY);
}
