export function consumeLaunchParam(name: string) {
  const url = new URL(window.location.href);
  if (!url.searchParams.has(name)) {
    return "";
  }
  const value = (url.searchParams.get(name) || "").trim();
  url.searchParams.delete(name);
  window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  return value;
}
