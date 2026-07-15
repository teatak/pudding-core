import type { ProjectBrowserRoot, ProjectGitStatus } from "@/api/client";

export type ProjectGitRepositoryState = {
  error?: unknown;
  loading: boolean;
  root: ProjectBrowserRoot;
  status?: ProjectGitStatus;
};
