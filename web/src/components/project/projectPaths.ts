import { projectResourceURL, type ProjectFile } from "@/api/client";

import { isProjectGitDiffTab, type ProjectSelection, type ProjectTab } from "./types";

export function projectSelectionKey(selection: ProjectSelection) {
  return `${selection.rootID}:${selection.path}`;
}

export function projectTabKey(tab: ProjectTab) {
  return isProjectGitDiffTab(tab)
    ? `git-diff:${tab.staged ? "staged" : "working"}:${projectSelectionKey(tab)}`
    : projectSelectionKey(tab);
}

export function projectFileName(path: string) {
  return path.split("/").at(-1) || path;
}

export function projectParentPath(path: string) {
  const parts = path.split("/").filter(Boolean);
  parts.pop();
  return parts.join("/") || ".";
}

export function projectAbsolutePath(rootPath: string, relativePath: string) {
  if (relativePath === ".") {
    return rootPath;
  }
  const separator = rootPath.includes("\\") && !rootPath.includes("/") ? "\\" : "/";
  const base = rootPath.replace(/[\\/]+$/, "") || separator;
  const relative = relativePath.replace(/[\\/]/g, separator);
  return base.endsWith(separator) ? `${base}${relative}` : `${base}${separator}${relative}`;
}

export function projectPathContains(parent: ProjectSelection, child: ProjectSelection) {
  return parent.rootID === child.rootID && (parent.path === child.path || child.path.startsWith(`${parent.path}/`));
}

export function replaceProjectPath(path: string, previous: string, next: string) {
  if (path === previous) {
    return next;
  }
  return path.startsWith(`${previous}/`) ? `${next}${path.slice(previous.length)}` : path;
}

export function projectMarkdownResolvers(file: ProjectFile, token: string, sessionID: string) {
  return {
    resolveImageURL: (raw: string) => {
      const path = resolveRelativeProjectPath(file.path, raw);
      return path ? projectResourceURL(token, sessionID, file.rootID, path) : "";
    },
    resolveLinkURL: (raw: string) => {
      if (isExternalMarkdownURL(raw) || raw.startsWith("#")) {
        return raw;
      }
      const path = resolveRelativeProjectPath(file.path, raw);
      return path ? projectMarkdownLink(file.rootID, path) : "";
    },
  };
}

export function parseProjectMarkdownLink(href: string): ProjectSelection | null {
  try {
    const url = new URL(href);
    if (url.protocol !== "pudding-project:") {
      return null;
    }
    const path = url.pathname.split("/").filter(Boolean).map((part) => decodeURIComponent(part)).join("/");
    return path ? { rootID: decodeURIComponent(url.hostname), path } : null;
  } catch {
    return null;
  }
}

function resolveRelativeProjectPath(currentFile: string, raw: string) {
  const value = raw.trim();
  if (!value || isExternalMarkdownURL(value) || value.startsWith("#")) {
    return "";
  }
  const withoutHash = value.split("#", 1)[0].split("?", 1)[0];
  let decoded = withoutHash;
  try {
    decoded = decodeURIComponent(withoutHash);
  } catch {
    return "";
  }
  const parts = decoded.startsWith("/") ? [] : currentFile.split("/").slice(0, -1);
  for (const part of decoded.replace(/^\/+/, "").split("/")) {
    if (!part || part === ".") {
      continue;
    }
    if (part === "..") {
      if (parts.length === 0) {
        return "";
      }
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  return parts.join("/");
}

function isExternalMarkdownURL(raw: string) {
  return /^(?:https?:|mailto:)/i.test(raw.trim());
}

function projectMarkdownLink(rootID: string, path: string) {
  const encodedPath = path.split("/").map((part) => encodeURIComponent(part)).join("/");
  return `pudding-project://${encodeURIComponent(rootID)}/${encodedPath}`;
}
