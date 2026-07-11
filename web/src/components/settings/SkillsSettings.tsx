import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Eye, Trash } from "lucide-react";
import { useState } from "react";

import {
  applySkillDraft,
  deleteSkill,
  deleteSkillDraft,
  listSkillDrafts,
  listSkills,
  skillIconURL,
  type Skill,
  type SkillDraft,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { IdentityIcon } from "@/components/IdentityIcon";
import { SkillDraftDiffDialog } from "@/components/SkillDraftDiffDialog";
import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/i18n";
import { toast } from "sonner";

import { SETTINGS_CONTENT_CLASS, SettingsSection } from "./shared";

export function SkillsSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [deletingSkill, setDeletingSkill] = useState<Skill | null>(null);
  const [viewingDraft, setViewingDraft] = useState<SkillDraft | null>(null);
  const draftsQuery = useQuery({
    queryKey: queryKeys.skillDrafts(),
    queryFn: () => listSkillDrafts(token),
    enabled: Boolean(token),
    staleTime: Infinity,
  });
  const skillsQuery = useQuery({
    queryKey: queryKeys.skills(),
    queryFn: () => listSkills(token),
    enabled: Boolean(token),
    staleTime: Infinity,
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteSkill(token, id),
    onSuccess: async () => {
      setDeletingSkill(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.skills() });
      toast.success(t("settings.skills.deleted"));
    },
    onError: () => toast.error(t("settings.skills.deleteFailed")),
  });
  const applyDraftMutation = useMutation({
    mutationFn: (id: string) => applySkillDraft(token, id),
    onSuccess: async () => {
      setViewingDraft(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.skillDrafts() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.skills() }),
      ]);
      toast.success(t("settings.skills.draftApplied"));
    },
    onError: () => toast.error(t("settings.skills.draftApplyFailed")),
  });
  const rejectDraftMutation = useMutation({
    mutationFn: (id: string) => deleteSkillDraft(token, id),
    onSuccess: async (_data, id) => {
      if (viewingDraft?.id === id) {
        setViewingDraft(null);
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.skillDrafts() });
      toast.success(t("settings.skills.draftRejected"));
    },
    onError: () => toast.error(t("settings.skills.draftRejectFailed")),
  });
  const drafts = draftsQuery.data?.drafts || [];
  const skills = skillsQuery.data?.skills || [];

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      {draftsQuery.isLoading || draftsQuery.isError || drafts.length > 0 ? (
        <SettingsSection title={`${t("settings.skills.pendingTitle")} (${drafts.length})`}>
          {draftsQuery.isError ? (
            <Alert variant="destructive">
              <AlertDescription className="grid gap-2">
                <span>{t("settings.skills.draftsLoadFailed")}</span>
                <Button size="sm" type="button" variant="outline" onClick={() => void draftsQuery.refetch()}>
                  {t("common.refresh")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          {draftsQuery.isLoading ? (
            <SkillsSkeleton />
          ) : (
            <SkillDraftList
              applyingID={applyDraftMutation.isPending ? applyDraftMutation.variables : undefined}
              drafts={drafts}
              rejectingID={rejectDraftMutation.isPending ? rejectDraftMutation.variables : undefined}
              token={token}
              onApply={(draft) => applyDraftMutation.mutate(draft.id)}
              onReject={(draft) => rejectDraftMutation.mutate(draft.id)}
              onView={setViewingDraft}
            />
          )}
        </SettingsSection>
      ) : null}
      <SettingsSection title={`${t("settings.skills.title")} (${skills.length})`}>
        {skillsQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription className="grid gap-2">
              <span>{t("settings.skills.loadFailed")}</span>
              <Button size="sm" type="button" variant="outline" onClick={() => void skillsQuery.refetch()}>
                {t("common.refresh")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}
        {skillsQuery.isLoading ? (
          <SkillsSkeleton />
        ) : (
          <SkillList
            deletingID={deleteMutation.isPending ? deletingSkill?.id : undefined}
            skills={skills}
            token={token}
            onDelete={(skill) => setDeletingSkill(skill)}
          />
        )}
      </SettingsSection>
      <SkillDraftDiffDialog
        applying={applyDraftMutation.isPending && applyDraftMutation.variables === viewingDraft?.id}
        draft={viewingDraft}
        rejecting={rejectDraftMutation.isPending && rejectDraftMutation.variables === viewingDraft?.id}
        token={token}
        onApply={(draft) => applyDraftMutation.mutate(draft.id)}
        onOpenChange={(open) => !open && setViewingDraft(null)}
        onReject={(draft) => rejectDraftMutation.mutate(draft.id)}
      />
      <AlertDialog open={Boolean(deletingSkill)} onOpenChange={(open) => !open && setDeletingSkill(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("settings.skills.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("settings.skills.deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={!deletingSkill || deleteMutation.isPending}
              variant="destructive"
              onClick={(event) => {
                event.preventDefault();
                if (deletingSkill) {
                  deleteMutation.mutate(deletingSkill.id);
                }
              }}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SkillList({
  deletingID,
  onDelete,
  skills,
  token,
}: {
  deletingID?: string;
  onDelete: (skill: Skill) => void;
  skills: Skill[];
  token: string;
}) {
  const { t } = useI18n();
  if (skills.length === 0) {
    return <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">{t("settings.skills.empty")}</div>;
  }
  return (
    <ItemGroup className="gap-2">
      {skills.map((skill) => (
        <SkillRow
          key={`${skill.source}:${skill.id}`}
          deleting={deletingID === skill.id}
          skill={skill}
          token={token}
          onDelete={() => onDelete(skill)}
        />
      ))}
    </ItemGroup>
  );
}

function SkillDraftList({
  applyingID,
  drafts,
  onApply,
  onReject,
  onView,
  rejectingID,
  token,
}: {
  applyingID?: string;
  drafts: SkillDraft[];
  onApply: (draft: SkillDraft) => void;
  onReject: (draft: SkillDraft) => void;
  onView: (draft: SkillDraft) => void;
  rejectingID?: string;
  token: string;
}) {
  if (drafts.length === 0) {
    return null;
  }
  return (
    <ItemGroup className="gap-2">
      {drafts.map((draft) => (
        <SkillDraftRow
          key={draft.id}
          applying={applyingID === draft.id}
          draft={draft}
          rejecting={rejectingID === draft.id}
          token={token}
          onApply={() => onApply(draft)}
          onReject={() => onReject(draft)}
          onView={() => onView(draft)}
        />
      ))}
    </ItemGroup>
  );
}

function SkillDraftRow({
  applying,
  draft,
  onApply,
  onReject,
  onView,
  rejecting,
  token,
}: {
  applying?: boolean;
  draft: SkillDraft;
  onApply: () => void;
  onReject: () => void;
  onView: () => void;
  rejecting?: boolean;
  token: string;
}) {
  const { t } = useI18n();
  const busy = applying || rejecting;
  const iconURL = skillIconURL(token, draft);
  return (
    <Item className="items-start gap-3 rounded-lg px-3 py-3" variant="outline">
      <ItemMedia>
        <IdentityIcon fallback="skill" fit="contain" size="lg" src={iconURL || undefined} />
      </ItemMedia>
      <ItemContent className="min-w-0 gap-1">
        <ItemTitle className="flex max-w-full flex-wrap items-center gap-2">
          <span className="min-w-0 truncate text-sm font-medium">{draft.id}</span>
          <span className="min-w-0 truncate text-xs font-normal text-muted-foreground">{draft.path}</span>
          <span className="rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {t(`settings.skills.draftChange.${draft.change}`)}
          </span>
          {!draft.validation.ok ? (
            <span className="rounded border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 text-[10px] leading-none text-destructive">
              {t("settings.skills.draftInvalid")}
            </span>
          ) : null}
        </ItemTitle>
        <ItemDescription className="line-clamp-2 text-xs leading-5">
          {draft.description || draft.validation.errors?.[0] || t("settings.skills.noDescription")}
        </ItemDescription>
      </ItemContent>
      <ItemActions className="self-center">
        <Button aria-label={t("settings.skills.viewDiff")} disabled={busy} size="icon-sm" type="button" variant="ghost" onClick={onView}>
          <Eye />
        </Button>
        <Button
          aria-label={t("settings.skills.applyDraft")}
          disabled={busy || !draft.validation.ok}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onApply}
        >
          {applying ? <Spinner /> : <Check />}
        </Button>
        <Button
          aria-label={t("settings.skills.rejectDraft")}
          className="text-destructive hover:bg-destructive/10 hover:text-destructive"
          disabled={busy}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onReject}
        >
          {rejecting ? <Spinner /> : <Trash />}
        </Button>
      </ItemActions>
    </Item>
  );
}

function SkillRow({ deleting, onDelete, skill, token }: { deleting?: boolean; onDelete: () => void; skill: Skill; token: string }) {
  const { t } = useI18n();
  const iconURL = skillIconURL(token, skill);
  return (
    <Item className="items-start gap-3 rounded-lg px-3 py-3" variant="outline">
      <ItemMedia>
        <IdentityIcon fallback="skill" fit="contain" size="lg" src={iconURL || undefined} />
      </ItemMedia>
      <ItemContent className="min-w-0 gap-1">
        <ItemTitle className="flex max-w-full flex-wrap items-center gap-2">
          <span className="min-w-0 truncate text-sm font-medium">{skill.id}</span>
          <span className="min-w-0 truncate text-[11px] font-normal text-muted-foreground">
            {skillDisplayPath(skill)}
          </span>
          <span className="rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {t(`settings.skills.source.${skill.source}`)}
          </span>
          <span className="rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {t("settings.skills.scope.global")}
          </span>
        </ItemTitle>
        <ItemDescription className="line-clamp-2 text-xs leading-5">
          {skill.description || t("settings.skills.noDescription")}
        </ItemDescription>
      </ItemContent>
      {skill.source === "user" ? (
        <ItemActions className="self-center opacity-0 transition-opacity group-hover/item:opacity-100 group-focus-within/item:opacity-100">
          <Button
            aria-label={t("common.delete")}
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
            disabled={deleting}
            size="icon"
            type="button"
            variant="ghost"
            onClick={onDelete}
          >
            {deleting ? <Spinner /> : <Trash />}
          </Button>
        </ItemActions>
      ) : null}
    </Item>
  );
}

function skillDisplayPath(skill: Skill) {
  if (skill.source === "builtin") {
    return `builtin/${skill.id}`;
  }
  return `skills/${skill.id}`;
}

function SkillsSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}

