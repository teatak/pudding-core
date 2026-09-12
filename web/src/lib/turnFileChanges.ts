import type { TurnFileChange } from "@/api/client";

export function turnFileDiffChanges(changes: TurnFileChange[]) {
  return changes.filter((change) => !change.binary);
}

export function turnFileResourceChanges(changes: TurnFileChange[]) {
  return changes.filter((change) => change.binary);
}

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

// 窄卡片下只显示文件名:尾部截断会把最有信息量的文件名丢掉,先拆出 dir 段。
export function turnFilePathParts(label: string) {
  const index = label.lastIndexOf("/");
  if (index < 0) {
    return { base: label, dir: "" };
  }
  return { base: label.slice(index + 1), dir: label.slice(0, index + 1) };
}

function pathParts(path: string) {
  return path.replace(/[\\/]+$/, "").split(/[\\/]+/).filter(Boolean);
}

function suffix(parts: string[], depth: number) {
  return parts.slice(-depth).join("/");
}
