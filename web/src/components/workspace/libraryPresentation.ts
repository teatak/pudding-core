import type { LibraryEntry } from "@/contracts/api";

export function libraryMatches(
  entry: LibraryEntry,
  query: string,
  t: (key: string) => string,
) {
  const source =
    entry.kind === "canvas"
      ? [entry.sourceSessionTitle, entry.sourceProjectName]
      : [];
  return [
    entry.title,
    entry.url,
    t(`workspace.kind.${entry.canvasKind || entry.kind}`),
    ...source,
  ]
    .join(" ")
    .toLocaleLowerCase()
    .includes(query.toLocaleLowerCase());
}

export function libraryDescription(
  entry: LibraryEntry,
  t: (key: string) => string,
) {
  const web = entry.kind === "web";
  const source = web
    ? t("workspace.library.global")
    : [entry.sourceProjectName, entry.sourceSessionTitle]
        .filter(Boolean)
        .join(" · ");
  const version = entry.savedItemID
    ? t("workspace.savedVersion").replace("{revision}", String(entry.revision))
    : "";
  return {
    title: entry.title || entry.url || t("canvas.untitled"),
    kind: t(`workspace.kind.${entry.canvasKind || entry.kind}`),
    source,
    version,
    location: entry.url || "",
    description:
      web && entry.url
        ? new URL(entry.url).host
        : [t(`workspace.kind.${entry.canvasKind}`), version, source]
            .filter(Boolean)
            .join(" · "),
  };
}
