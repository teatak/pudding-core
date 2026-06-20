import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Copy,
  Info,
  MessageSquareText,
  Palette,
  Pencil,
  Plus,
  Settings,
  Sparkles,
  Trash,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";

import {
  deleteProvider,
  listProviders,
  listSessions,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInput,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { BrandIcon } from "@/components/BrandIcons";
import {
  cloneProviderProfileForm,
  ProviderProfileEditorDialog,
  type ProviderProfileEditorValue,
} from "@/components/ProviderProfileEditorDialog";
import { ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { SETTINGS_DIALOG_OPEN_EVENT, type SettingsDialogOpenDetail, type SettingsSectionID } from "@/lib/settingsDialog";
import {
  getOrderedProviderPresets,
  type ProviderPreset,
} from "@/provider/presets";

const SETTINGS_SECTIONS: Array<{
  id: SettingsSectionID;
  icon: typeof MessageSquareText;
  labelKey: string;
}> = [
  { id: "dialogue", icon: MessageSquareText, labelKey: "settings.section.dialogue" },
  { id: "model", icon: Sparkles, labelKey: "settings.section.model" },
  { id: "appearance", icon: Palette, labelKey: "settings.section.appearance" },
  { id: "about", icon: Info, labelKey: "settings.section.about" },
];

type SettingsDialogProps = {
  token: string;
  showTrigger?: boolean;
};

export function SettingsDialog({ token, showTrigger = true }: SettingsDialogProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<SettingsSectionID>("model");
  const [createProviderNonce, setCreateProviderNonce] = useState(0);
  const activeSection = SETTINGS_SECTIONS.find((section) => section.id === active) || SETTINGS_SECTIONS[0];

  useEffect(() => {
    const handleOpen = (event: Event) => {
      const detail = (event as CustomEvent<SettingsDialogOpenDetail>).detail || {};
      if (detail.section) {
        setActive(detail.section);
      }
      if (detail.createProvider) {
        setCreateProviderNonce((nonce) => nonce + 1);
      }
      setOpen(true);
    };
    window.addEventListener(SETTINGS_DIALOG_OPEN_EVENT, handleOpen);
    return () => window.removeEventListener(SETTINGS_DIALOG_OPEN_EVENT, handleOpen);
  }, []);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {showTrigger ? (
        <DialogTrigger asChild>
          <Button aria-label={t("settings.title")} size="icon" tabIndex={-1} variant="ghost">
            <Settings />
          </Button>
        </DialogTrigger>
      ) : null}
      <DialogContent className="top-[calc(var(--toolbar-h)+(100svh-var(--toolbar-h))/2)] h-[min(900px,calc(100svh-var(--toolbar-h)-1.5rem))] w-[calc(100vw-2rem)] max-w-[1180px] overflow-hidden bg-background p-0 sm:max-w-[1180px] xl:max-w-[1240px]">
        <DialogTitle className="sr-only">{t("settings.title")}</DialogTitle>
        <DialogDescription className="sr-only">{t("settings.description")}</DialogDescription>
        <SidebarProvider className="h-full min-h-0 items-start" style={{ "--sidebar-width": "14rem" } as CSSProperties}>
          <SettingsSidebar active={active} onActiveChange={setActive} />
          <main className="flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-background">
            <header className="flex h-16 shrink-0 items-center gap-2 transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-12">
              <div className="flex items-center gap-2 px-4">
                <h2 className="text-sm font-normal text-foreground">{t(activeSection.labelKey)}</h2>
              </div>
            </header>
            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 pt-0">
              {active === "model" ? <ProviderSettings createNonce={createProviderNonce} token={token} /> : null}
            </div>
          </main>
        </SidebarProvider>
      </DialogContent>
    </Dialog>
  );
}

function SettingsSidebar({
  active,
  onActiveChange,
}: {
  active: SettingsSectionID;
  onActiveChange: (section: SettingsSectionID) => void;
}) {
  const { t } = useI18n();

  return (
    <Sidebar collapsible="none" className="flex shrink-0 border-r">
      <SidebarHeader className="p-4 pb-0">
        <SidebarInput aria-label={t("settings.search")} placeholder={t("settings.search")} />
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup className="p-3">
          <SidebarGroupLabel>{t("settings.title")}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-1">
              {SETTINGS_SECTIONS.map((section) => {
                const Icon = section.icon;
                const isActive = section.id === active;
                return (
                  <SidebarMenuItem key={section.id}>
                    <SidebarMenuButton asChild isActive={isActive}>
                      <a
                        aria-current={isActive ? "page" : undefined}
                        href={`#settings-${section.id}`}
                        onClick={(event) => {
                          event.preventDefault();
                          onActiveChange(section.id);
                        }}
                      >
                        <Icon />
                        <span>{t(section.labelKey)}</span>
                      </a>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}

function ProviderSettings({ createNonce = 0, token }: { createNonce?: number; token: string }) {
  const queryClient = useQueryClient();
  const { locale, t } = useI18n();
  const [editingProfile, setEditingProfile] = useState<ProviderProfile | null>(null);
  const [editorInitialValue, setEditorInitialValue] = useState<ProviderProfileEditorValue | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [quickPreset, setQuickPreset] = useState<ProviderPreset | null>(null);
  const [presetPickerOpen, setPresetPickerOpen] = useState(false);
  const [deletingProfile, setDeletingProfile] = useState<ProviderProfile | null>(null);
  const handledCreateNonceRef = useRef(0);
  const providerPresets = getOrderedProviderPresets(locale);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
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
  const sessions = sessionsQuery.data?.sessions || [];
  const showInlinePresets = !providersQuery.isLoading && !providersQuery.isError && profiles.length === 0;

  const usage = useMemo(() => {
    const byProfile = new Map<string, Session[]>();
    for (const session of sessions) {
      const profileID = session.provider;
      if (!profileID) {
        continue;
      }
      byProfile.set(profileID, [...(byProfile.get(profileID) || []), session]);
    }
    return byProfile;
  }, [sessions]);

  function startCreate() {
    setEditingProfile(null);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }

  useEffect(() => {
    if (!createNonce || handledCreateNonceRef.current === createNonce) {
      return;
    }
    handledCreateNonceRef.current = createNonce;
    setEditingProfile(null);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }, [createNonce]);

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
    <div className="@container mx-auto grid w-full max-w-6xl gap-5">
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
              const usedBy = usage.get(profile.id) || [];
              const deleteBlocked = usedBy.length > 0;
              return (
                <Item
                  key={profile.id}
                  className="group min-h-16 flex-nowrap rounded-xl bg-card px-4 py-3 hover:bg-accent/50"
                  role="listitem"
                  variant="outline"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3 text-left">
                    <ItemMedia>
                      <BrandIcon className="size-9 shrink-0" name={profile.brand || profile.name || profile.id} />
                    </ItemMedia>
                    <ItemContent className="min-w-0 gap-0.5">
                      <ItemTitle className="w-full min-w-0 font-normal">
                        <span className="truncate text-base font-normal">{profile.name}</span>
                        <span
                          className={cn(
                            "size-2 shrink-0 rounded-full",
                            profile.apiKeySet ? "bg-success" : "bg-warning",
                          )}
                          title={profile.apiKeySet ? t("provider.keySet") : t("provider.keyMissing")}
                        />
                      </ItemTitle>
                      <ItemDescription className="truncate text-xs">
                        {profile.type} · {modelCountLabel(profile.models.length, t)}
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
                      disabled={deleteBlocked || deleteMutation.isPending}
                      size="icon-sm"
                      title={deleteBlocked ? t("provider.deleteBlockedSessions") : undefined}
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

function SettingsPanel({
  action,
  children,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  title: string;
}) {
  return (
    <section className="overflow-hidden rounded-xl border bg-card shadow-sm">
      <div className="pudding-settings-panel-header flex min-h-11 items-center justify-between gap-3 border-b px-4 py-2">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      <div className="grid gap-3 p-4">{children}</div>
    </section>
  );
}

function SettingsSection({
  action,
  children,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  title: string;
}) {
  return (
    <section className="grid gap-3">
      <div className="flex min-h-8 items-center justify-between gap-3">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

function modelCountLabel(count: number, t: (key: string) => string) {
  if (count <= 0) {
    return t("picker.noModels");
  }
  return `${count}${t("provider.modelCountSuffix")}`;
}

function ProviderSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}
