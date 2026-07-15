import { useQuery } from "@tanstack/react-query";
import { FileDiff } from "lucide-react";
import { useTheme } from "next-themes";
import ReactDiffViewer, { DiffMethod } from "react-diff-viewer-continued";

import { getProjectGitDiff } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";

import { projectBrowserError } from "../projectErrors";
import type { ProjectGitDiffTab } from "../types";

export function ProjectGitDiffViewer({ active, selection, sessionID, token }: {
  active: boolean;
  selection: ProjectGitDiffTab;
  sessionID: string;
  token: string;
}) {
  const { t } = useI18n();
  const { resolvedTheme } = useTheme();
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
        <code className="min-w-0 flex-1 cursor-text select-text truncate font-mono text-xs" title={selection.path}>{selection.path}</code>
        <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {selection.staged ? t("project.gitStaged") : t("project.gitWorkingTree")}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-background">
        {diffQuery.isLoading ? (
          <ProjectGitDiffStatus><Spinner className="size-6" />{t("common.loading")}</ProjectGitDiffStatus>
        ) : diffQuery.isError ? (
          <ProjectGitDiffStatus>{projectBrowserError(diffQuery.error, t)}</ProjectGitDiffStatus>
        ) : diff?.binary ? (
          <ProjectGitDiffStatus>{t("project.gitBinary")}</ProjectGitDiffStatus>
        ) : diff?.tooLarge ? (
          <ProjectGitDiffStatus>{t("project.gitDiffTooLarge")}</ProjectGitDiffStatus>
        ) : diff ? (
          <div className="min-w-[520px] text-[11px]">
            <ReactDiffViewer
              oldValue={diff.oldContent}
              newValue={diff.newContent}
              splitView={false}
              compareMethod={DiffMethod.WORDS_WITH_SPACE}
              showDiffOnly={false}
              hideSummary
              useDarkTheme={resolvedTheme === "dark"}
              disableWorker
              styles={diffViewerStyles}
            />
          </div>
        ) : null}
      </div>
    </div>
  );
}

function ProjectGitDiffStatus({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full min-h-64 flex-col items-center justify-center gap-3 p-6 text-center text-sm text-muted-foreground">{children}</div>;
}

const diffViewerStyles = {
  variables: {
    light: {
      diffViewerBackground: "var(--background)", diffViewerColor: "var(--foreground)", diffViewerTitleBackground: "var(--muted)",
      diffViewerTitleColor: "var(--muted-foreground)", diffViewerTitleBorderColor: "var(--border)", gutterBackground: "var(--muted)",
      gutterColor: "var(--muted-foreground)", addedBackground: "rgba(16, 185, 129, 0.12)", addedColor: "var(--foreground)",
      removedBackground: "rgba(239, 68, 68, 0.12)", removedColor: "var(--foreground)", wordAddedBackground: "rgba(16, 185, 129, 0.24)",
      wordRemovedBackground: "rgba(239, 68, 68, 0.24)",
    },
    dark: {
      diffViewerBackground: "var(--background)", diffViewerColor: "var(--foreground)", diffViewerTitleBackground: "var(--muted)",
      diffViewerTitleColor: "var(--muted-foreground)", diffViewerTitleBorderColor: "var(--border)", gutterBackground: "var(--muted)",
      gutterColor: "var(--muted-foreground)", addedBackground: "rgba(16, 185, 129, 0.18)", addedColor: "var(--foreground)",
      removedBackground: "rgba(239, 68, 68, 0.18)", removedColor: "var(--foreground)", wordAddedBackground: "rgba(16, 185, 129, 0.32)",
      wordRemovedBackground: "rgba(239, 68, 68, 0.32)",
    },
  },
  diffContainer: { borderRadius: 0, fontSize: "11px" },
  contentText: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace", lineHeight: "20px" },
  lineNumber: { fontSize: "11px" },
  marker: { fontSize: "11px" },
};
