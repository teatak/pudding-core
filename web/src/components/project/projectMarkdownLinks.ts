import type { ProjectBrowserRoot } from "@/api/client";
import type { ProjectSelection } from "./types";

export type ProjectMarkdownLink =
  | { kind: "file"; selection: ProjectSelection; anchor?: string }
  | { kind: "web" | "external"; url: string }
  | { kind: "invalid" };

// Relative links belong to the document's directory, never to the renderer URL.
export function resolveProjectMarkdownLink(raw: string, current: ProjectSelection, roots: ProjectBrowserRoot[]): ProjectMarkdownLink {
  const value = raw.trim();
  if (!value || /[\u0000-\u001f\u007f]/.test(value)) return { kind: "invalid" };
  try {
    if (/^(https?:|\/\/)/i.test(value)) {
      const url = new URL(value.startsWith("//") ? `https:${value}` : value);
      return { kind: "web", url: url.href };
    }
    if (/^mailto:/i.test(value)) return { kind: "external", url: new URL(value).href };

    const hashIndex = value.indexOf("#");
    const anchor = hashIndex < 0 ? undefined : decodeURIComponent(value.slice(hashIndex + 1));
    if (/^file:/i.test(value)) {
      const url = new URL(value);
      if (url.hostname && url.hostname !== "localhost") return { kind: "invalid" };
      const absolute = decodeURIComponent(url.pathname);
      const root = roots.filter((candidate) => absolute.startsWith(`${candidate.path.replace(/\/$/, "")}/`))
        .sort((a, b) => b.path.length - a.path.length)[0];
      if (!root) return { kind: "invalid" };
      const path = normalizeProjectPath([], absolute.slice(root.path.replace(/\/$/, "").length + 1));
      return path ? { kind: "file", selection: { rootID: root.id, path }, anchor } : { kind: "invalid" };
    }
    if (/^[a-z][a-z\d+.-]*:/i.test(value)) return { kind: "invalid" };
    const pathname = decodeURIComponent(value.split(/[?#]/, 1)[0]);
    if (!pathname) return { kind: "file", selection: current, anchor };
    const parts = pathname.startsWith("/") ? [] : current.path.split("/").slice(0, -1);
    const path = normalizeProjectPath(parts, pathname);
    return path ? { kind: "file", selection: { rootID: current.rootID, path }, anchor } : { kind: "invalid" };
  } catch {
    return { kind: "invalid" };
  }
}

function normalizeProjectPath(parts: string[], path: string) {
  if (/[\\\u0000-\u001f\u007f]/.test(path)) return undefined;
  for (const part of path.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      if (!parts.length) return undefined;
      parts.pop();
    } else parts.push(part);
  }
  return parts.join("/") || undefined;
}
