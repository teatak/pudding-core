import { useQuery } from "@tanstack/react-query";
import { FileDiff } from "@/components/icons";

import { getProjectGitDiff } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { TextDiffViewer } from "@/components/diff/TextDiffViewer";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";

import { projectGitReadError } from "../projectErrors";
import type { ProjectGitDiffTab } from "../types";

export function ProjectGitDiffViewer({ active, selection, sessionID, token }: {
  active: boolean;
  selection: ProjectGitDiffTab;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const diffQuery = useQuery({
    enabled: active,
    queryKey: queryKeys.projectGitDiff(sessionID, selection.rootID, selection.path, selection.staged),
    queryFn: () => getProjectGitDiff(token, sessionID, selection.rootID, selection.path, selection.staged),
    retry: false,
    staleTime: 0,
  });
  const diff = diffQuery.data;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <FileDiff className="size-4 shrink-0 text-muted-foreground" />
        <code className="min-w-0 flex-1 cursor-text select-text truncate font-mono text-xs" >{selection.path}</code>
        <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {selection.staged ? t("project.gitStaged") : t("project.gitWorkingTree")}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-background">
        {diffQuery.isLoading ? (
          <ProjectGitDiffStatus><Spinner className="size-6" />{t("common.loading")}</ProjectGitDiffStatus>
        ) : diffQuery.isError ? (
          <ProjectGitDiffStatus>{projectGitReadError(diffQuery.error, t)}</ProjectGitDiffStatus>
        ) : diff?.binary ? (
          <ProjectGitDiffStatus>{t("project.gitBinary")}</ProjectGitDiffStatus>
        ) : diff?.tooLarge ? (
          <ProjectGitDiffStatus>{t("project.gitDiffTooLarge")}</ProjectGitDiffStatus>
        ) : diff ? (
          <TextDiffViewer oldValue={diff.oldContent} newValue={diff.newContent} />
        ) : null}
      </div>
    </div>
  );
}

function ProjectGitDiffStatus({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full min-h-64 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-muted-foreground">{children}</div>;
}
