import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Pencil, Plus, Sparkles, Trash } from "@/components/icons";
import { useEffect, useState } from "react";

import { deleteProvider, listProviders, type ProviderProfile } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import {
  cloneProviderProfileForm,
  ProviderProfileEditorDialog,
  type ProviderProfileEditorValue,
} from "@/components/ProviderProfileEditorDialog";
import { ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
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
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
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
import { cn } from "@/lib/utils";
import {
  getOrderedProviderPresets,
  type ProviderPreset,
} from "@/provider/presets";

import { SETTINGS_CONTENT_CLASS, SettingsSection } from "./shared";

export function ProviderSettings({
  createNonce = 0,
  token,
  onCreateHandled,
}: {
  createNonce?: number;
  token: string;
  onCreateHandled?: () => void;
}) {
  const queryClient = useQueryClient();
  const { locale, t } = useI18n();
  const [editingProfile, setEditingProfile] = useState<ProviderProfile | null>(null);
  const [editorInitialValue, setEditorInitialValue] = useState<ProviderProfileEditorValue | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [quickPreset, setQuickPreset] = useState<ProviderPreset | null>(null);
  const [presetPickerOpen, setPresetPickerOpen] = useState(false);
  const [deletingProfile, setDeletingProfile] = useState<ProviderProfile | null>(null);
  const providerPresets = getOrderedProviderPresets(locale);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteProvider(token, id),
    onSuccess: async (_, id) => {
      if (editingProfile?.id === id) {
        setEditingProfile(null);
        setEditorInitialValue(null);
        setEditorOpen(false);
      }
      setDeletingProfile(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const showInlinePresets = !providersQuery.isLoading && !providersQuery.isError && profiles.length === 0;

  function startCreate() {
    setEditingProfile(null);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }

  useEffect(() => {
    if (!createNonce) {
      return;
    }
    setEditingProfile(null);
    setEditorInitialValue(null);
    setEditorOpen(true);
    onCreateHandled?.();
  }, [createNonce, onCreateHandled]);

  function editProfile(profile: ProviderProfile) {
    setEditingProfile(profile);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }

  function cloneProfile(profile: ProviderProfile) {
    setEditingProfile(null);
    setEditorInitialValue(cloneProviderProfileForm(profile, profiles, t("provider.copySuffix")));
    setEditorOpen(true);
  }

  function selectPreset(preset: ProviderPreset) {
    setPresetPickerOpen(false);
    setQuickPreset(preset);
  }

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      {showInlinePresets ? (
        <SettingsSection title={t("provider.addFromPreset")}>
          <ProviderPresetGrid presets={providerPresets} onSelect={selectPreset} />
        </SettingsSection>
      ) : null}

      <SettingsSection
        action={
          <div className="flex items-center gap-2">
            {showInlinePresets ? null : (
              <Button size="sm" type="button" variant="outline" onClick={() => setPresetPickerOpen(true)}>
                <Sparkles />
                {t("provider.addFromPreset")}
              </Button>
            )}
            <Button size="sm" type="button" variant="outline" onClick={startCreate}>
              <Plus />
              {t("provider.new")}
            </Button>
          </div>
        }
        title={t("provider.list")}
      >
        {providersQuery.isLoading ? <ProviderSkeleton /> : null}
        {providersQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription className="grid gap-2">
              <span>{t("provider.loadFailed")}</span>
              <Button size="sm" type="button" variant="outline" onClick={() => void providersQuery.refetch()}>
                {t("common.refresh")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}
        {showInlinePresets ? (
          <div className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">
            {t("provider.empty")}
          </div>
        ) : null}
        {profiles.length > 0 ? (
          <ItemGroup className="gap-2">
            {profiles.map((profile) => {
              return (
                <Item
                  key={profile.id}
                  className="group min-h-16 flex-nowrap rounded-xl bg-card px-4 py-3 hover:bg-accent/50"
                  role="listitem"
                  variant="outline"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3 text-left">
                    <ItemMedia>
                      <BrandIcon className="shrink-0" name={profile.brand || profile.displayName || profile.id} size="lg" />
                    </ItemMedia>
                    <ItemContent className="min-w-0 gap-0.5">
                      <ItemTitle className="w-full min-w-0 font-normal">
                        <span className="truncate text-base font-normal">{profile.displayName}</span>
                        <span
                          className={cn(
                            "size-2 shrink-0 rounded-full",
                            profile.apiKeySet ? "bg-success" : "bg-warning",
                          )}

                        />
                      </ItemTitle>
                      <ItemDescription className="truncate text-xs">
                        {profile.protocol} · {profileModelCountLabel(profile, t)}
                      </ItemDescription>
                    </ItemContent>
                  </div>
                  <ItemActions className="ml-auto shrink-0 gap-1">
                    <Button aria-label={t("provider.editShort")} size="icon-sm" type="button" variant="ghost" onClick={() => editProfile(profile)}>
                      <Pencil />
                    </Button>
                    <Button aria-label={t("common.copy")} size="icon-sm" type="button" variant="ghost" onClick={() => cloneProfile(profile)}>
                      <Copy />
                    </Button>
                    <Button
                      aria-label={t("common.delete")}
                      disabled={deleteMutation.isPending}
                      size="icon-sm"
                      type="button"
                      variant="ghost"
                      onClick={() => setDeletingProfile(profile)}
                    >
                      <Trash className="text-destructive" />
                    </Button>
                  </ItemActions>
                </Item>
              );
            })}
          </ItemGroup>
        ) : null}
      </SettingsSection>

      <AlertDialog open={Boolean(deletingProfile)} onOpenChange={(open) => {
        if (!open) {
          setDeletingProfile(null);
        }
      }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("provider.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("provider.deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={!deletingProfile || deleteMutation.isPending}
              variant="destructive"
              onClick={() => {
                if (deletingProfile) {
                  deleteMutation.mutate(deletingProfile.id);
                }
              }}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={presetPickerOpen} onOpenChange={setPresetPickerOpen}>
        <DialogContent className="@container w-[min(1120px,calc(100vw-2rem))] max-w-none sm:max-w-none">
          <DialogHeader>
            <DialogTitle>{t("provider.addFromPreset")}</DialogTitle>
            <DialogDescription>{t("provider.addFromPresetHint")}</DialogDescription>
          </DialogHeader>
          <ProviderPresetGrid
            className="pudding-provider-preset-surface-dark"
            presets={providerPresets}
            onSelect={selectPreset}
          />
        </DialogContent>
      </Dialog>

      <ProviderPresetCreateDialog
        open={Boolean(quickPreset)}
        preset={quickPreset}
        profiles={profiles}
        token={token}
        onOpenChange={(open) => {
          if (!open) {
            setQuickPreset(null);
          }
        }}
      />

      <ProviderProfileEditorDialog
        initialValue={editorInitialValue}
        open={editorOpen}
        profile={editingProfile}
        profiles={profiles}
        token={token}
        onOpenChange={(next) => {
          setEditorOpen(next);
          if (!next) {
            setEditingProfile(null);
            setEditorInitialValue(null);
          }
        }}
      />
    </div>
  );
}

function modelCountLabel(count: number, t: (key: string) => string) {
  return `${Math.max(0, count)}${t("provider.modelCountSuffix")}`;
}

function profileModelCountLabel(profile: ProviderProfile, t: (key: string) => string) {
  return modelCountLabel(profile.models.length, t);
}

function ProviderSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}
