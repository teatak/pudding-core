const TOKEN_KEY = "pudding.daemonToken";

export function initialToken() {
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
