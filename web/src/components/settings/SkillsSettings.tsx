import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash } from "@/components/icons";
import { useState } from "react";

import { deleteSkill, listSkills, skillIconURL, type Skill } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { IdentityIcon } from "@/components/IdentityIcon";
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
} from "@/components/ConfirmationDialog";
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
  const skillsQuery = useQuery({
    queryKey: queryKeys.skills(),
    queryFn: () => listSkills(token),
    enabled: Boolean(token),
    staleTime: 0,
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
  const skills = skillsQuery.data?.skills || [];

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
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
    <ItemGroup className="min-w-0 gap-2">
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

function SkillRow({ deleting, onDelete, skill, token }: { deleting?: boolean; onDelete: () => void; skill: Skill; token: string }) {
  const { t } = useI18n();
  const iconURL = skillIconURL(token, skill);
  return (
    <Item
      className="group min-h-16 min-w-0 max-w-full flex-nowrap overflow-hidden rounded-xl bg-card px-4 py-3"
      role="listitem"
      variant="outline"
    >
      <div className="flex min-w-0 flex-1 items-center gap-3 overflow-hidden text-left">
        <ItemMedia>
          <IdentityIcon fallback="skill" fit="contain" size="lg" src={iconURL || undefined} />
        </ItemMedia>
        <ItemContent className="min-w-0 gap-0.5">
          <ItemTitle className="w-full min-w-0 flex-nowrap overflow-hidden font-normal">
            <span className="min-w-0 truncate text-base font-normal">{skill.id}</span>
            <span className="min-w-0 truncate text-xs font-normal text-muted-foreground">
              {skillDisplayPath(skill)}
            </span>
            <span className="shrink-0 rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
              {t(`settings.skills.source.${skill.source}`)}
            </span>
            <span className="shrink-0 rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
              {t("settings.skills.scope.global")}
            </span>
          </ItemTitle>
          <ItemDescription className="truncate text-xs">
            {skill.description || t("settings.skills.noDescription")}
          </ItemDescription>
        </ItemContent>
      </div>
      {skill.source === "user" ? (
        <ItemActions className="ml-auto shrink-0 gap-1">
          <Button
            aria-label={t("common.delete")}
            disabled={deleting}
            size="icon-sm"
            type="button"
            variant="ghost"
            onClick={onDelete}
          >
            {deleting ? <Spinner /> : <Trash className="text-destructive" />}
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
