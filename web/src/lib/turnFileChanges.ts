import type { TurnFileChange } from "@/api/client";

export function turnFileChangeLabel(change: TurnFileChange, changes: TurnFileChange[]) {
  const matchingPaths = changes.filter((candidate) => candidate.path === change.path);
  if (matchingPaths.length <= 1) {
    return change.path;
  }
  const rootParts = pathParts(change.rootPath);
  for (let depth = 1; depth <= rootParts.length; depth += 1) {
    const rootSuffix = suffix(rootParts, depth);
    const unique = matchingPaths.every(
      (candidate) => candidate.id === change.id || suffix(pathParts(candidate.rootPath), depth) !== rootSuffix,
    );
    if (unique) {
      return `${rootSuffix}/${change.path}`;
    }
  }
  return turnFileChangeFullPath(change);
}

export function turnFileChangeFullPath(change: TurnFileChange) {
  return `${change.rootPath.replace(/[\\/]+$/, "")}/${change.path}`;
}

function pathParts(path: string) {
  return path.replace(/[\\/]+$/, "").split(/[\\/]+/).filter(Boolean);
}

function suffix(parts: string[], depth: number) {
  return parts.slice(-depth).join("/");
}
