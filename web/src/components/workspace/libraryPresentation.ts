import type { LibraryEntry, LibraryRecentEntry } from "@/contracts/api";

export type LibraryReference = LibraryEntry | LibraryRecentEntry;

export function libraryReferenceKey(entry: LibraryReference) {
  if (entry.kind === "file") return JSON.stringify([entry.kind, entry.rootPath, entry.path]);
  if (entry.kind === "web") return JSON.stringify([entry.kind, entry.url]);
  // A saved version and a session's working canvas are different resources.
  return JSON.stringify([entry.kind, "savedItemID" in entry ? "saved" : "working", entry.id]);
}

export function libraryMatches(entry: LibraryReference, query: string, t: (key: string) => string) {
  return [entry.title, ...(entry.kind === "file" ? [entry.path, entry.rootPath] : []), entry.url, entry.sourceSessionTitle, entry.sourceProjectName, t(`workspace.kind.${entry.canvasKind || entry.kind}`)]
    .join(" ").toLocaleLowerCase().includes(query.toLocaleLowerCase());
}

export function libraryDescription(entry: LibraryReference, t: (key: string) => string) {
  const title = entry.title || entry.url || t("canvas.untitled");
  const kind = t(`workspace.kind.${entry.canvasKind || entry.kind}`);
  const session = entry.sourceSessionID
    ? entry.sourceSessionAvailable ? entry.sourceSessionTitle || t("session.untitled") : t("workspace.sourceSessionUnavailable")
    : "";
  const source = [entry.sourceProjectName, session].filter(Boolean).join(" · ");
  const version = "savedItemID" in entry && entry.savedItemID ? t("workspace.savedVersion").replace("{revision}", String(entry.revision)) : "";
  let location = "";
  let description = [kind, source, version].filter(Boolean).join(" · ");
  if (entry.kind === "file") {
    location = `${entry.rootPath}/${entry.path}`;
    const root = entry.sourceProjectName || entry.rootPath?.split(/[\\/]/).pop();
    const folder = entry.path?.split("/").slice(0, -1).join("/");
    description = [[root, folder].filter(Boolean).join(" / "), session].filter(Boolean).join(" · ");
  } else if (entry.kind === "web" && entry.url) {
    location = entry.url;
    description = new URL(entry.url).host;
  }
  return { title, kind, source, location, description, version };
}
