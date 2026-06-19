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
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { BrandIcon } from "@/components/BrandIcons";
import {
  cloneProviderProfileForm,
  ProviderProfileEditorDialog,
  type ProviderProfileEditorValue,
} from "@/components/ProviderProfileEditorDialog";
import { ProviderCustomCard, ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
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
};

export function SettingsDialog({ token }: SettingsDialogProps) {
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
      <DialogTrigger asChild>
        <Button aria-label={t("settings.title")} size="icon" tabIndex={-1} variant="ghost">
          <Settings />
        </Button>
      </DialogTrigger>
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
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const sessions = sessionsQuery.data?.sessions || [];

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

  return (
    <div className="@container mx-auto grid w-full max-w-6xl gap-5">
      <SettingsPanel title={t("settings.providerPresets")}>
        <ProviderPresetGrid presets={providerPresets} onSelect={setQuickPreset}>
          <ProviderCustomCard onSelect={startCreate} />
        </ProviderPresetGrid>
      </SettingsPanel>

      <SettingsPanel
        action={
          <Button size="sm" type="button" variant="outline" onClick={startCreate}>
            <Plus />
            {t("provider.new")}
          </Button>
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
        {!providersQuery.isLoading && profiles.length === 0 ? (
          <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">{t("provider.empty")}</div>
        ) : null}
        <div className="overflow-hidden rounded-lg border">
          {profiles.map((profile) => {
            const usedBy = usage.get(profile.id) || [];
            const deleteBlocked = usedBy.length > 0;
            return (
              <div key={profile.id} className="flex items-center gap-4 border-b px-4 py-3 last:border-b-0">
                <button className="flex min-w-0 flex-1 items-center gap-3 text-left" type="button" onClick={() => editProfile(profile)}>
                  <BrandIcon className="size-6 shrink-0" name={profile.id} />
                  <span className="grid min-w-0 gap-1">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="truncate text-sm font-normal">{profile.name}</span>
                      <span
                        className={cn(
                          "size-2 shrink-0 rounded-full",
                          profile.apiKeySet || profile.apiKeyEnv ? "bg-success" : "bg-warning",
                        )}
                        title={profile.apiKeySet || profile.apiKeyEnv ? t("provider.keySet") : t("provider.keyMissing")}
                      />
                    </span>
                    <span className="truncate text-xs text-muted-foreground">
                      {profile.type} · {modelCountLabel(profile.models.length, t)}
                    </span>
                  </span>
                </button>
                <div className="flex shrink-0 items-center gap-1">
                  <IconButton label={t("provider.editShort")} onClick={() => editProfile(profile)}>
                    <Pencil />
                  </IconButton>
                  <IconButton label={t("common.copy")} onClick={() => cloneProfile(profile)}>
                    <Copy />
                  </IconButton>
                  <AlertDialog>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <AlertDialogTrigger asChild>
                          <Button
                            aria-label={t("common.delete")}
                            disabled={deleteBlocked || deleteMutation.isPending}
                            size="icon-sm"
                            type="button"
                            variant="ghost"
                          >
                            <Trash className="text-destructive" />
                          </Button>
                        </AlertDialogTrigger>
                      </TooltipTrigger>
                      <TooltipContent>
                        {deleteBlocked ? t("provider.deleteBlockedSessions") : t("common.delete")}
                      </TooltipContent>
                    </Tooltip>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>{t("provider.deleteTitle")}</AlertDialogTitle>
                        <AlertDialogDescription>{t("provider.deleteDescription")}</AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                        <AlertDialogAction variant="destructive" onClick={() => deleteMutation.mutate(profile.id)}>
                          {t("common.delete")}
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </div>
              </div>
            );
          })}
        </div>
      </SettingsPanel>

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

function IconButton({
  children,
  label,
  onClick,
}: {
  children: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button aria-label={label} size="icon-sm" type="button" variant="ghost" onClick={onClick}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
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
