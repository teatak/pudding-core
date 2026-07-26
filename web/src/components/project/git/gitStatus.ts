import type { ProjectGitStatusFile } from "@/api/client";

export function projectGitFileKey(rootID: string, path: string) {
  return `${rootID}:${path}`;
}

export function isProjectGitStaged(file: ProjectGitStatusFile) {
  return file.kind !== "conflicted" && file.indexStatus !== "." && file.indexStatus !== "?";
}

export function isProjectGitWorking(file: ProjectGitStatusFile) {
  return file.worktreeStatus !== "." || file.kind === "untracked" || file.kind === "conflicted";
}

export function projectGitStatusLabel(file: ProjectGitStatusFile, staged: boolean) {
  if (file.kind === "conflicted") return "!";
  if (file.kind === "untracked") return "U";
  const status = staged ? file.indexStatus : file.worktreeStatus;
  return status === "?" ? "U" : status === "." ? "M" : status;
}

export function projectGitStatusTone(file: ProjectGitStatusFile) {
  if (file.kind === "conflicted") return "text-git-conflicted";
  if (file.kind === "untracked" || file.kind === "added") return "text-git-added";
  if (file.kind === "deleted") return "text-git-deleted";
  if (file.kind === "renamed") return "text-git-renamed";
  return "text-git-modified";
}
