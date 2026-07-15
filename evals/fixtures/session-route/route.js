export function selectSessionURL(currentURL, sessionID) {
  const url = new URL(currentURL, "http://pudding.local");
  return `/?session=${sessionID}`;
}
