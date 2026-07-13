import type { ProjectBrowserRoot, ProjectGitStatus } from "@/api/client";

export type ProjectGitRepositoryState = {
  error?: unknown;
  fetching: boolean;
  loading: boolean;
  root: ProjectBrowserRoot;
  status?: ProjectGitStatus;
};
