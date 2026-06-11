export function replaceSessionSearch(sessionID: string | undefined) {
  if (typeof window === "undefined") {
    return;
  }
  const url = new URL(window.location.href);
  if (sessionID) {
    url.searchParams.set("session", sessionID);
  } else {
    url.searchParams.delete("session");
  }
  window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
