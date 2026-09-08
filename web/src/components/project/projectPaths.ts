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
