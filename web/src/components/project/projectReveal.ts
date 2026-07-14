import type { ProjectBrowserRoot } from "@/api/client";
import type { ProjectFileReveal } from "@/state/projectRevealStore";

import type { ProjectSelection } from "./types";

export type ProjectEditorReveal = {
  column?: number;
  key: string;
  line: number;
  serial: number;
};

export function resolveProjectFileReveal(
  roots: ProjectBrowserRoot[],
  reveal: ProjectFileReveal,
): ProjectSelection | undefined {
  const rootPath = normalizedPath(reveal.rootPath);
  const absolutePath = normalizedPath(reveal.absolutePath);
  const explicitRoot = rootPath
    ? roots.find((root) => comparablePath(root.path) === comparablePath(rootPath))
    : undefined;
  const root = explicitRoot || roots
    .filter((candidate) => absolutePath && pathIsWithin(candidate.path, absolutePath))
    .sort((left, right) => normalizedPath(right.path).length - normalizedPath(left.path).length)[0];
  if (!root) {
    return undefined;
  }
  const relativePath = normalizedRelativePath(
    reveal.relativePath || relativePathFromRoot(root.path, reveal.absolutePath),
  );
  if (!relativePath || relativePath === "." || relativePath.startsWith("../")) {
    return undefined;
  }
  return { rootID: root.id, path: relativePath };
}

function relativePathFromRoot(rootPath: string, absolutePath?: string) {
  const root = normalizedPath(rootPath).replace(/\/$/, "");
  const absolute = normalizedPath(absolutePath);
  if (!root || !absolute || !pathIsWithin(root, absolute)) {
    return "";
  }
  return absolute.slice(root.length).replace(/^\/+/, "");
}

function pathIsWithin(rootPath: string, absolutePath: string) {
  const root = comparablePath(rootPath).replace(/\/$/, "");
  const absolute = comparablePath(absolutePath);
  return absolute === root || absolute.startsWith(`${root}/`);
}

function comparablePath(value?: string) {
  const normalized = normalizedPath(value).replace(/\/$/, "");
  return /^[a-z]:\//i.test(normalized) ? normalized.toLowerCase() : normalized;
}

function normalizedPath(value?: string) {
  return (value || "").trim().replace(/\\/g, "/").replace(/\/{2,}/g, "/");
}

function normalizedRelativePath(value?: string) {
  return normalizedPath(value).replace(/^\.\//, "").replace(/^\/+/, "");
}
