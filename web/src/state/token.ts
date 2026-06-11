const TOKEN_KEY = "pudding.daemonToken";

export function initialToken() {
  // daemon 启动日志给出 /?token=… 一键入口;读到后立刻从地址栏清掉,
  // 避免 token 留在历史记录/书签里
  const fromURL = consumeTokenFromURL();
  if (fromURL) {
    sessionStorage.setItem(TOKEN_KEY, fromURL);
    return fromURL;
  }
  return import.meta.env.VITE_PUDDING_TOKEN || sessionStorage.getItem(TOKEN_KEY) || "";
}

function consumeTokenFromURL() {
  const url = new URL(window.location.href);
  const token = (url.searchParams.get("token") || "").trim();
  if (!token) {
    return "";
  }
  url.searchParams.delete("token");
  window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  return token;
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
