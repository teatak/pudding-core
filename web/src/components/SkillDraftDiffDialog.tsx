import { useQuery } from "@tanstack/react-query";
import { Check, Trash } from "lucide-react";
import ReactDiffViewer, { DiffMethod } from "react-diff-viewer-continued";

import { getSkillDraft, type SkillDraft, type SkillDraftDetail } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/i18n";
import { useTheme } from "@/theme/theme";

export function SkillDraftDiffDialog({
  applying,
  draft,
  onApply,
  onOpenChange,
  onReject,
  rejecting,
  token,
}: {
  applying?: boolean;
  draft: SkillDraft | null;
  onApply: (draft: SkillDraft) => void;
  onOpenChange: (open: boolean) => void;
  onReject: (draft: SkillDraft) => void;
  rejecting?: boolean;
  token: string;
}) {
  const { t } = useI18n();
  const detailQuery = useQuery({
    queryKey: queryKeys.skillDraft(draft?.id || ""),
    queryFn: () => getSkillDraft(token, draft?.id || ""),
    enabled: Boolean(token && draft),
    staleTime: 0,
  });
  const detail = detailQuery.data;
  const visibleDraft = detail?.draft || draft;
  const busy = applying || rejecting;
  return (
    <Dialog open={Boolean(draft)} onOpenChange={onOpenChange}>
      <DialogContent className="grid max-h-[min(800px,calc(100svh-2rem))] w-[min(1120px,calc(100vw-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="px-4 pt-4 pr-12">
          <DialogTitle>{visibleDraft?.id || t("settings.skills.viewDiff")}</DialogTitle>
          <DialogDescription>{visibleDraft?.path}</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 overflow-y-auto px-4 py-3">
          {detailQuery.isLoading ? <SkillDraftDiffSkeleton /> : null}
          {detailQuery.isError ? (
            <Alert variant="destructive">
              <AlertDescription>{t("settings.skills.draftDetailFailed")}</AlertDescription>
            </Alert>
          ) : null}
          {visibleDraft ? <SkillDraftValidation validation={visibleDraft.validation} /> : null}
          {detail ? <SkillDraftDiffFiles detail={detail} /> : null}
        </div>
        <DialogFooter className="mx-0 mb-0 flex-row items-center justify-end gap-2 rounded-none rounded-b-xl px-4 py-3">
          <Button
            className="h-8 min-w-20 gap-1.5 px-3 leading-none [&_svg]:size-4"
            disabled={!visibleDraft || busy}
            type="button"
            variant="destructive"
            onClick={() => visibleDraft && onReject(visibleDraft)}
          >
            {rejecting ? <Spinner /> : <Trash />}
            {t("settings.skills.rejectDraft")}
          </Button>
          <Button
            className="h-8 min-w-20 gap-1.5 px-3 leading-none [&_svg]:size-4"
            disabled={!visibleDraft?.validation.ok || busy}
            type="button"
            onClick={() => visibleDraft && onApply(visibleDraft)}
          >
            {applying ? <Spinner /> : <Check />}
            {t("settings.skills.applyDraft")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SkillDraftValidation({ validation }: { validation: SkillDraft["validation"] }) {
  const { t } = useI18n();
  if (validation.ok && !validation.warnings?.length) {
    return null;
  }
  return (
    <div className="mb-3 grid gap-2 rounded-lg border bg-muted/30 p-3 text-xs">
      {validation.errors?.length ? (
        <div className="grid gap-1 text-destructive">
          <div className="font-medium">{t("settings.skills.validationErrors")}</div>
          {validation.errors.map((error) => (
            <div key={error}>{error}</div>
          ))}
        </div>
      ) : null}
      {validation.warnings?.length ? (
        <div className="grid gap-1 text-muted-foreground">
          <div className="font-medium text-foreground">{t("settings.skills.validationWarnings")}</div>
          {validation.warnings.map((warning) => (
            <div key={warning}>{warning}</div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function SkillDraftDiffFiles({ detail }: { detail: SkillDraftDetail }) {
  const { t } = useI18n();
  const { resolved } = useTheme();
  if (detail.files.length === 0) {
    return <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">{t("settings.skills.noDiff")}</div>;
  }
  const defaultOpen = detail.files[0]?.path ? [detail.files[0].path] : [];
  return (
    <Accordion className="grid gap-3" defaultValue={defaultOpen} type="multiple">
      {detail.files.map((file) => {
        const splitView = file.change === "modified";
        return (
          <AccordionItem key={file.path} className="overflow-hidden rounded-lg border not-last:border-b" value={file.path}>
            <AccordionTrigger className="items-center rounded-none border-0 bg-muted/40 px-3 py-2 text-xs hover:no-underline focus-visible:ring-0 [&_[data-slot=accordion-trigger-icon]]:ml-2 [&_[data-slot=accordion-trigger-icon]]:size-3.5">
              <span className="flex min-w-0 flex-1 items-center gap-2">
                <span className="min-w-0 truncate font-medium">{file.path}</span>
                <span className="shrink-0 text-muted-foreground">{t(`settings.skills.fileChange.${file.change}`)}</span>
              </span>
            </AccordionTrigger>
            <AccordionContent className="h-auto p-0 pb-0">
              {typeof file.old === "string" || typeof file.new === "string" ? (
                <div className="max-h-[420px] overflow-auto bg-background text-[11px]">
                  <div className={splitView ? "min-w-[760px]" : "min-w-[420px]"}>
                    <ReactDiffViewer
                      oldValue={file.old || ""}
                      newValue={file.new || ""}
                      splitView={splitView}
                      compareMethod={DiffMethod.WORDS_WITH_SPACE}
                      showDiffOnly={false}
                      hideSummary
                      hideLineNumbers={!splitView}
                      useDarkTheme={resolved === "dark"}
                      disableWorker
                      styles={diffViewerStyles}
                    />
                  </div>
                </div>
              ) : (
                <div className="px-3 py-4 text-xs text-muted-foreground">{t("settings.skills.noTextDiff")}</div>
              )}
            </AccordionContent>
          </AccordionItem>
        );
      })}
    </Accordion>
  );
}

const diffViewerStyles = {
  variables: {
    light: {
      diffViewerBackground: "var(--background)",
      diffViewerColor: "var(--foreground)",
      diffViewerTitleBackground: "var(--muted)",
      diffViewerTitleColor: "var(--muted-foreground)",
      diffViewerTitleBorderColor: "var(--border)",
      gutterBackground: "var(--muted)",
      gutterColor: "var(--muted-foreground)",
      addedBackground: "rgba(16, 185, 129, 0.12)",
      addedColor: "var(--foreground)",
      removedBackground: "rgba(239, 68, 68, 0.12)",
      removedColor: "var(--foreground)",
      wordAddedBackground: "rgba(16, 185, 129, 0.24)",
      wordRemovedBackground: "rgba(239, 68, 68, 0.24)",
    },
    dark: {
      diffViewerBackground: "var(--background)",
      diffViewerColor: "var(--foreground)",
      diffViewerTitleBackground: "var(--muted)",
      diffViewerTitleColor: "var(--muted-foreground)",
      diffViewerTitleBorderColor: "var(--border)",
      gutterBackground: "var(--muted)",
      gutterColor: "var(--muted-foreground)",
      addedBackground: "rgba(16, 185, 129, 0.18)",
      addedColor: "var(--foreground)",
      removedBackground: "rgba(239, 68, 68, 0.18)",
      removedColor: "var(--foreground)",
      wordAddedBackground: "rgba(16, 185, 129, 0.32)",
      wordRemovedBackground: "rgba(239, 68, 68, 0.32)",
    },
  },
  diffContainer: {
    borderRadius: 0,
    fontSize: "11px",
  },
  contentText: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
    lineHeight: "20px",
  },
  lineNumber: {
    fontSize: "11px",
  },
  marker: {
    fontSize: "11px",
  },
};

function SkillDraftDiffSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}
