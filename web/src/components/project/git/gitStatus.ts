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
  if (file.kind === "conflicted") return "text-destructive";
  if (file.kind === "untracked" || file.kind === "added") return "text-emerald-600 dark:text-emerald-400";
  if (file.kind === "deleted") return "text-destructive";
  return "text-yellow-600 dark:text-yellow-400";
}
