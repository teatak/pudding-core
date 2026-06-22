import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Globe2,
  Info,
  Loader2,
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
  getWebTools,
  listBuiltinTools,
  listProviders,
  listSessions,
  patchWebTools,
  type BuiltinTool,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
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
import { Input } from "@/components/ui/input";
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
import { toast } from "sonner";

const SETTINGS_SECTIONS: Array<{
  id: SettingsSectionID;
  icon: typeof MessageSquareText;
  labelKey: string;
}> = [
  { id: "dialogue", icon: MessageSquareText, labelKey: "settings.section.dialogue" },
  { id: "model", icon: Sparkles, labelKey: "settings.section.model" },
  { id: "tools", icon: Globe2, labelKey: "settings.section.tools" },
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
              {active === "tools" ? <ToolsSettings token={token} /> : null}
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

function ToolsSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [apiKey, setAPIKey] = useState("");
  const [visible, setVisible] = useState(false);
  const builtinToolsQuery = useQuery({
    queryKey: queryKeys.builtinTools(),
    queryFn: () => listBuiltinTools(token),
    enabled: Boolean(token),
    staleTime: Infinity,
  });
  const toolsQuery = useQuery({
    queryKey: queryKeys.webTools(),
    queryFn: () => getWebTools(token),
    enabled: Boolean(token),
  });
  const tavily = toolsQuery.data?.providers.find((provider) => provider.name === "tavily");

  useEffect(() => {
    if (toolsQuery.isSuccess) {
      setAPIKey(tavily?.apiKey || "");
    }
  }, [tavily?.apiKey, toolsQuery.isSuccess]);

  const mutation = useMutation({
    mutationFn: (nextAPIKey: string) =>
      patchWebTools(token, {
        fetchProvider: nextAPIKey.trim() ? "tavily" : "",
        providers: { tavily: { apiKey: nextAPIKey } },
        searchProvider: nextAPIKey.trim() ? "tavily" : "",
      }),
    onSuccess: async (_data, nextAPIKey) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.webTools() });
      toast.success(nextAPIKey.trim() ? t("settings.tools.web.saved") : t("settings.tools.web.cleared"));
    },
    onError: () => toast.error(t("settings.tools.web.saveFailed")),
  });

  const savedAPIKey = tavily?.apiKey || "";
  const dirty = apiKey.trim() !== savedAPIKey.trim();
  const configured = Boolean(tavily?.apiKeySet);
  const loadingTools = toolsQuery.isLoading;
  const saving = mutation.isPending;

  return (
    <div className="@container mx-auto grid w-full max-w-6xl gap-5">
      <BuiltinToolsPanel
        loading={builtinToolsQuery.isFetching}
        error={builtinToolsQuery.isError}
        tools={builtinToolsQuery.data?.tools || []}
        onRetry={() => void builtinToolsQuery.refetch()}
      />
      <SettingsPanel
        action={
          <span
            className={cn(
              "rounded-md px-2 py-0.5 text-xs",
              configured ? "bg-success/15 text-success" : "bg-warning/15 text-warning",
            )}
          >
            {configured ? t("provider.keySet") : t("provider.keyMissing")}
          </span>
        }
        title={t("settings.tools.web.title")}
      >
        <div className="grid gap-4">
          <div className="grid gap-1">
            <p className="text-sm leading-6 text-muted-foreground">{t("settings.tools.web.desc")}</p>
            <a
              className="inline-flex w-fit items-center gap-1 text-sm text-foreground underline-offset-4 hover:underline"
              href={t("settings.tools.web.signupLink")}
              rel="noreferrer"
              target="_blank"
            >
              {t("settings.tools.web.signup")}
              <ExternalLink className="size-3.5" />
            </a>
          </div>

          {toolsQuery.isError ? (
            <Alert variant="destructive">
              <AlertDescription className="grid gap-2">
                <span>{t("settings.tools.web.loadFailed")}</span>
                <Button size="sm" type="button" variant="outline" onClick={() => void toolsQuery.refetch()}>
                  {t("common.refresh")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}

          <div className="flex flex-col gap-2">
            <label className="text-sm" htmlFor="pudding-tavily-api-key">
              {t("settings.tools.web.apiKey")}
            </label>
            <div className="flex flex-col gap-2 sm:flex-row">
              <div className="relative min-w-0 flex-1">
                <Input
                  autoComplete="off"
                  className="pr-9"
                  disabled={loadingTools}
                  id="pudding-tavily-api-key"
                  name="pudding-tavily-api-key"
                  type={visible ? "text" : "password"}
                  value={apiKey}
                  onChange={(event) => setAPIKey(event.target.value)}
                />
                <button
                  aria-label={visible ? t("provider.hideAPIKey") : t("provider.showAPIKey")}
                  className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
                  type="button"
                  onClick={() => setVisible((value) => !value)}
                >
                  {visible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                </button>
              </div>
              <div className="flex gap-2">
                <Button disabled={saving || loadingTools || !dirty} type="button" onClick={() => mutation.mutate(apiKey.trim())}>
                  {mutation.isPending ? <Loader2 className="animate-spin" /> : null}
                  {t("common.save")}
                </Button>
                <Button disabled={saving || loadingTools || !configured} type="button" variant="outline" onClick={() => mutation.mutate("")}>
                  {t("common.clear")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      </SettingsPanel>
    </div>
  );
}

function BuiltinToolsPanel({
  error,
  loading,
  onRetry,
  tools,
}: {
  error: boolean;
  loading: boolean;
  onRetry: () => void;
  tools: BuiltinTool[];
}) {
  const { t } = useI18n();

  return (
    <Accordion className="overflow-hidden rounded-xl border bg-card shadow-sm" collapsible type="single">
      <AccordionItem className="border-b-0" value="builtin-tools">
        <AccordionTrigger className="h-14 items-center rounded-none border-0 px-4 py-0 text-sm font-normal hover:no-underline focus-visible:ring-0">
          <span>{`${t("settings.tools.builtin.title")} (${tools.length})`}</span>
          {loading ? <Loader2 className="mr-2 size-4 animate-spin text-muted-foreground" /> : null}
        </AccordionTrigger>
        <AccordionContent className="p-0">
          {error ? (
            <div className="border-t p-4">
              <Alert variant="destructive">
                <AlertDescription className="grid gap-2">
                  <span>{t("settings.tools.builtin.loadFailed")}</span>
                  <Button size="sm" type="button" variant="outline" onClick={onRetry}>
                    {t("common.refresh")}
                  </Button>
                </AlertDescription>
              </Alert>
            </div>
          ) : (
            <ToolList tools={tools} />
          )}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

function ToolList({ tools }: { tools: BuiltinTool[] }) {
  const { t } = useI18n();
  if (tools.length === 0) {
    return <div className="border-t px-4 py-3 text-sm text-muted-foreground">{t("settings.tools.builtin.empty")}</div>;
  }
  return (
    <div className="divide-y divide-border/70 border-t">
      {tools.map((tool) => (
        <ToolRow key={tool.id} tool={tool} />
      ))}
    </div>
  );
}

function ToolRow({ tool }: { tool: BuiltinTool }) {
  const { t } = useI18n();
  return (
    <div className="grid gap-1 px-4 py-3">
      <div className="break-all font-mono text-xs text-foreground">{tool.id}</div>
      <div className="line-clamp-2 text-xs leading-5 text-muted-foreground">
        {tool.description || t("settings.tools.builtin.noDescription")}
      </div>
    </div>
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
                      <BrandIcon className="size-9 shrink-0" name={profile.brand || profile.displayName || profile.id} />
                    </ItemMedia>
                    <ItemContent className="min-w-0 gap-0.5">
                      <ItemTitle className="w-full min-w-0 font-normal">
                        <span className="truncate text-base font-normal">{profile.displayName}</span>
                        <span
                          className={cn(
                            "size-2 shrink-0 rounded-full",
                            profile.apiKeySet ? "bg-success" : "bg-warning",
                          )}
                          title={profile.apiKeySet ? t("provider.keySet") : t("provider.keyMissing")}
                        />
                      </ItemTitle>
                      <ItemDescription className="truncate text-xs">
                        {profile.protocol} · {modelCountLabel(profile.models.length, t)}
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
  title: ReactNode;
}) {
  return (
    <section className="overflow-hidden rounded-xl border bg-card shadow-sm">
      <div className="pudding-settings-panel-header flex h-14 items-center justify-between gap-3 border-b px-4">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      {children ? <div className="grid gap-3 p-4">{children}</div> : null}
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
