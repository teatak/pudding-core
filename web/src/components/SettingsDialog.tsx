import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Copy,
  Info,
  KeyRound,
  Loader2,
  MessageSquareText,
  Palette,
  Pencil,
  Plus,
  Settings,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { z } from "zod";

import {
  APIError,
  createProvider,
  createProviderRequest,
  deleteProvider,
  getSettings,
  listProviderModels,
  listProviders,
  listSessions,
  patchProvider,
  patchProviderRequest,
  putSettings,
  type ProviderModel,
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
import { Badge } from "@/components/ui/badge";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
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
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import {
  getOrderedProviderPresets,
  providerPresetName,
  type ProviderPreset,
} from "@/provider/presets";

const DEFAULT_PROVIDER = "default";

const providerTypeSchema = z.enum(["openai-compatible", "openai-responses", "google", "anthropic"]);

const modelFormSchema = z.object({
  id: z.string().trim().min(1),
  name: z.string().optional(),
  contextWindow: z.string().optional(),
  image: z.boolean(),
  audio: z.boolean(),
  tools: z.boolean(),
  temperature: z.string().optional(),
  reasoningEffort: z.string().optional(),
  maxCompletionTokens: z.string().optional(),
  maxToolLoops: z.string().optional(),
  anthropicMaxTokens: z.string().optional(),
});

const providerFormSchema = z.object({
  id: z.string().trim().min(1),
  name: z.string().trim().min(1),
  type: providerTypeSchema,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  apiKeyEnv: z.string().optional(),
  models: z.array(modelFormSchema).min(1),
});

type ProviderFormValue = z.infer<typeof providerFormSchema>;
type ModelFormValue = z.infer<typeof modelFormSchema>;
type SettingsSectionID = "dialogue" | "model" | "appearance" | "about";

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
  const [active, setActive] = useState<SettingsSectionID>("model");
  const activeSection = SETTINGS_SECTIONS.find((section) => section.id === active) || SETTINGS_SECTIONS[0];

  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button aria-label={t("settings.title")} size="icon" variant="ghost">
          <Settings />
        </Button>
      </DialogTrigger>
      <DialogContent className="h-[min(720px,calc(100svh-2rem))] overflow-hidden bg-background p-0 md:max-w-[860px] lg:max-w-[980px]">
        <DialogTitle className="sr-only">{t("settings.title")}</DialogTitle>
        <DialogDescription className="sr-only">{t("settings.description")}</DialogDescription>
        <SidebarProvider className="h-full min-h-0 items-start" style={{ "--sidebar-width": "14rem" } as CSSProperties}>
          <SettingsSidebar active={active} onActiveChange={setActive} />
          <main className="flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-background">
            <header className="flex h-16 shrink-0 items-center gap-2 transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-12">
              <div className="flex items-center gap-2 px-4">
                <Breadcrumb>
                  <BreadcrumbList>
                    <BreadcrumbItem>
                      <BreadcrumbLink asChild>
                        <span>{t("settings.title")}</span>
                      </BreadcrumbLink>
                    </BreadcrumbItem>
                    <BreadcrumbSeparator />
                    <BreadcrumbItem>
                      <BreadcrumbPage>{t(activeSection.labelKey)}</BreadcrumbPage>
                    </BreadcrumbItem>
                  </BreadcrumbList>
                </Breadcrumb>
              </div>
            </header>
            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 pt-0">
              {active === "model" ? <ProviderSettings token={token} /> : null}
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
    <Sidebar collapsible="none" className="hidden md:flex">
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

function ProviderSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { locale, t } = useI18n();
  const [editingID, setEditingID] = useState<string | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [quickPreset, setQuickPreset] = useState<ProviderPreset | null>(null);
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
  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
  });

  const providerForm = useForm<ProviderFormValue>({
    resolver: zodResolver(providerFormSchema),
    defaultValues: emptyProviderForm(),
  });
  const modelFields = useFieldArray({ control: providerForm.control, name: "models" });
  const providerType = providerForm.watch("type");

  const createMutation = useMutation({
    mutationFn: (value: ProviderFormValue) => createProvider(token, cleanCreateProvider(value)),
    onSuccess: async (profile) => {
      setEditingID(profile.id);
      setEditorOpen(false);
      providerForm.reset(providerToForm(profile));
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === "profile_exists") {
        providerForm.setError("id", { message: t("provider.profileExists") });
        return;
      }
      providerForm.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  const patchMutation = useMutation({
    mutationFn: (value: ProviderFormValue) => {
      if (!editingID) {
        throw new Error("missing provider id");
      }
      return patchProvider(token, editingID, cleanPatchProvider(value));
    },
    onSuccess: async (profile) => {
      setEditorOpen(false);
      providerForm.reset(providerToForm(profile));
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
    onError: (error) => {
      providerForm.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteProvider(token, id),
    onSuccess: async (_, id) => {
      if (editingID === id) {
        setEditingID(null);
        setEditorOpen(false);
        providerForm.reset(emptyProviderForm());
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
  });

  const defaultMutation = useMutation({
    mutationFn: (id: string) => putSettings(token, { "provider.default": id }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings() });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const sessions = sessionsQuery.data?.sessions || [];
  const defaultProvider = settingsQuery.data?.settings["provider.default"] || DEFAULT_PROVIDER;
  const saving = createMutation.isPending || patchMutation.isPending;
  const [candidatesLoading, setCandidatesLoading] = useState(false);

  const usage = useMemo(() => {
    const byProfile = new Map<string, Session[]>();
    for (const session of sessions) {
      const profileID = session.provider || defaultProvider;
      byProfile.set(profileID, [...(byProfile.get(profileID) || []), session]);
    }
    return byProfile;
  }, [defaultProvider, sessions]);

  async function loadCandidates() {
    if (!editingID) {
      return;
    }
    setCandidatesLoading(true);
    try {
      const { models } = await listProviderModels(token, editingID);
      const existing = new Set(providerForm.getValues("models").map((model) => model.id.trim()).filter(Boolean));
      for (const id of models) {
        if (!existing.has(id)) {
          modelFields.append(emptyModel(id));
          existing.add(id);
        }
      }
    } catch {
      providerForm.setError("root", { message: t("provider.candidatesFailed") });
    } finally {
      setCandidatesLoading(false);
    }
  }

  function startCreate() {
    setEditingID(null);
    providerForm.reset(emptyProviderForm());
    setEditorOpen(true);
  }

  function editProfile(profile: ProviderProfile) {
    setEditingID(profile.id);
    providerForm.reset(providerToForm(profile));
    setEditorOpen(true);
  }

  function cloneProfile(profile: ProviderProfile) {
    const nextID = uniqueProfileID(profile.id, profiles.map((item) => item.id));
    setEditingID(null);
    providerForm.reset({
      ...providerToForm(profile),
      id: nextID,
      name: `${profile.name} ${t("provider.copySuffix")}`,
      apiKey: "",
    });
    setEditorOpen(true);
  }

  function openPresetAdvanced(preset: ProviderPreset) {
    setQuickPreset(null);
    setEditingID(null);
    providerForm.reset(presetToForm(preset));
    setEditorOpen(true);
  }

  function submitProvider(value: ProviderFormValue) {
    if (editingID) {
      patchMutation.mutate(value);
      return;
    }
    createMutation.mutate(value);
  }

  return (
    <div className="mx-auto grid w-full max-w-5xl gap-5">
      <SettingsPanel title={t("settings.providerPresets")}>
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {providerPresets.map((preset) => (
            <button
              key={preset.id}
              className="flex min-h-14 items-center gap-3 rounded-lg border bg-background px-3 py-2 text-left transition-colors hover:bg-accent"
              type="button"
              onClick={() => setQuickPreset(preset)}
            >
              <BrandIcon className="size-6 shrink-0" name={preset.id} />
              <span className="grid min-w-0 gap-0.5">
                <span className="truncate text-sm font-normal">{preset.name}</span>
                <span className="truncate text-xs text-muted-foreground">{preset.models[0]?.id || preset.type}</span>
              </span>
            </button>
          ))}
          <button
            className="flex min-h-14 items-center gap-3 rounded-lg border border-dashed bg-background px-3 py-2 text-left transition-colors hover:bg-accent"
            type="button"
            onClick={startCreate}
          >
            <span className="flex size-6 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
              <Plus className="size-4" />
            </span>
            <span className="grid min-w-0 gap-0.5">
              <span className="truncate text-sm font-normal">{t("provider.custom")}</span>
              <span className="truncate text-xs text-muted-foreground">{t("provider.customHint")}</span>
            </span>
          </button>
        </div>
      </SettingsPanel>

      <SettingsPanel title={t("settings.defaultProvider")}>
        <DefaultProfileControl
          defaultProvider={defaultProvider}
          disabled={defaultMutation.isPending || profiles.length === 0}
          profiles={profiles}
          onChange={(value) => defaultMutation.mutate(value)}
        />
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
            const isDefault = profile.id === defaultProvider;
            const usedBy = usage.get(profile.id) || [];
            const deleteBlocked = isDefault || usedBy.length > 0;
            return (
              <div key={profile.id} className="flex items-center gap-4 border-b px-4 py-3 last:border-b-0">
                <button className="flex min-w-0 flex-1 items-center gap-3 text-left" type="button" onClick={() => editProfile(profile)}>
                  <BrandIcon className="size-6 shrink-0" name={profile.id} />
                  <span className="grid min-w-0 gap-1">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="truncate text-sm font-normal">{profile.name}</span>
                      {isDefault ? (
                        <Badge className="shrink-0 text-[10px] font-normal" variant="secondary">
                          {t("common.default")}
                        </Badge>
                      ) : null}
                      <span
                        className={cn(
                          "size-2 shrink-0 rounded-full",
                          profile.apiKeySet || profile.apiKeyEnv ? "bg-emerald-500" : "bg-amber-500",
                        )}
                        title={profile.apiKeySet || profile.apiKeyEnv ? t("provider.keySet") : t("provider.keyMissing")}
                      />
                    </span>
                    <span className="truncate text-xs text-muted-foreground">
                      {profile.type} · {profile.models[0]?.id || t("picker.noModels")}
                      {profile.models.length > 1 ? ` (+${profile.models.length - 1})` : ""}
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
                            <Trash2 className="text-destructive" />
                          </Button>
                        </AlertDialogTrigger>
                      </TooltipTrigger>
                      <TooltipContent>
                        {deleteBlocked
                          ? isDefault
                            ? t("provider.deleteBlockedDefault")
                            : t("provider.deleteBlockedSessions")
                          : t("common.delete")}
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

      <QuickPresetDialog
        open={Boolean(quickPreset)}
        preset={quickPreset}
        profiles={profiles}
        token={token}
        onAdvanced={openPresetAdvanced}
        onOpenChange={(open) => {
          if (!open) {
            setQuickPreset(null);
          }
        }}
      />

      <ProfileEditorDialog
        candidatesLoading={candidatesLoading}
        editingID={editingID}
        fields={modelFields.fields}
        form={providerForm}
        open={editorOpen}
        providerType={providerType}
        saving={saving}
        onAppendModel={() => modelFields.append(emptyModel())}
        onLoadCandidates={() => void loadCandidates()}
        onOpenChange={setEditorOpen}
        onRemoveModel={(index) => modelFields.remove(index)}
        onSubmit={submitProvider}
      />
    </div>
  );
}

function QuickPresetDialog({
  open,
  preset,
  profiles,
  token,
  onAdvanced,
  onOpenChange,
}: {
  open: boolean;
  preset: ProviderPreset | null;
  profiles: ProviderProfile[];
  token: string;
  onAdvanced: (preset: ProviderPreset) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [name, setName] = useState("");
  const [apiKey, setAPIKey] = useState("");
  const [apiKeyEnv, setAPIKeyEnv] = useState("");
  const [makeDefault, setMakeDefault] = useState(false);
  const [localError, setLocalError] = useState("");

  useEffect(() => {
    if (!preset || !open) {
      return;
    }
    setName(preset.name);
    setAPIKey("");
    setAPIKeyEnv(preset.apiKeyOptional ? "" : `${preset.id.toUpperCase()}_API_KEY`);
    setMakeDefault(profiles.length === 0);
    setLocalError("");
  }, [open, preset, profiles.length]);

  const mutation = useMutation({
    mutationFn: async () => {
      if (!preset) {
        throw new Error("missing preset");
      }
      if (!preset.apiKeyOptional && !apiKey.trim() && !apiKeyEnv.trim()) {
        throw new Error(t("provider.credentialRequired"));
      }
      const id = uniqueProfileID(providerPresetName(preset), profiles.map((profile) => profile.id));
      const profile = await createProvider(
        token,
        createProviderRequest.parse({
          id,
          name: name.trim() || preset.name,
          type: preset.type,
          baseURL: preset.baseURL,
          apiKey: apiKey.trim(),
          apiKeyEnv: apiKeyEnv.trim(),
          models: preset.models,
        }),
      );
      if (makeDefault) {
        await putSettings(token, { "provider.default": profile.id });
      }
      return profile;
    },
    onSuccess: async () => {
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings() });
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
    onError: (error) => {
      setLocalError(error instanceof Error ? error.message : t("provider.saveFailed"));
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{preset ? preset.name : t("provider.create")}</DialogTitle>
          <DialogDescription>{t("provider.quickCreateHint")}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          {localError ? (
            <Alert variant="destructive">
              <AlertDescription>{localError}</AlertDescription>
            </Alert>
          ) : null}
          <div className="grid gap-2">
            <Label>{t("provider.name")}</Label>
            <Input value={name} onChange={(event) => setName(event.target.value)} />
          </div>
          <div className="grid gap-2">
            <Label>API Key</Label>
            <Input
              autoComplete="off"
              placeholder={preset?.apiKeyOptional ? t("provider.apiKeyOptional") : "sk-..."}
              type="password"
              value={apiKey}
              onChange={(event) => setAPIKey(event.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label>{t("provider.apiKeyEnv")}</Label>
            <Input value={apiKeyEnv} onChange={(event) => setAPIKeyEnv(event.target.value)} />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={makeDefault} onCheckedChange={(checked) => setMakeDefault(checked === true)} />
            {t("provider.makeDefault")}
          </label>
        </div>
        <DialogFooter>
          {preset ? (
            <Button disabled={mutation.isPending} type="button" variant="outline" onClick={() => onAdvanced(preset)}>
              {t("provider.advanced")}
            </Button>
          ) : null}
          <Button disabled={mutation.isPending} type="button" onClick={() => mutation.mutate()}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
            {t("provider.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ProfileEditorDialog({
  candidatesLoading,
  editingID,
  fields,
  form,
  open,
  providerType,
  saving,
  onAppendModel,
  onLoadCandidates,
  onOpenChange,
  onRemoveModel,
  onSubmit,
}: {
  candidatesLoading: boolean;
  editingID: string | null;
  fields: Array<{ id: string }>;
  form: ReturnType<typeof useForm<ProviderFormValue>>;
  open: boolean;
  providerType: ProviderFormValue["type"];
  saving: boolean;
  onAppendModel: () => void;
  onLoadCandidates: () => void;
  onOpenChange: (open: boolean) => void;
  onRemoveModel: (index: number) => void;
  onSubmit: (value: ProviderFormValue) => void;
}) {
  const { t } = useI18n();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid h-[min(820px,calc(100vh-2rem))] w-[min(920px,calc(100vw-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="px-5 py-4 pr-14">
          <DialogTitle>{editingID ? t("provider.edit") : t("provider.create")}</DialogTitle>
          <DialogDescription>{t("provider.keyHint")}</DialogDescription>
        </DialogHeader>
        <form className="contents" onSubmit={form.handleSubmit(onSubmit)}>
          <div className="min-h-0 overflow-y-auto border-y px-5 py-4 [mask-image:linear-gradient(to_bottom,transparent_0,black_16px,black_calc(100%-16px),transparent_100%)]">
            <div className="grid gap-4">
              {form.formState.errors.root?.message ? (
                <Alert variant="destructive">
                  <AlertDescription>{form.formState.errors.root.message}</AlertDescription>
                </Alert>
              ) : null}

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="grid gap-2">
                  <Label htmlFor="provider-id">{t("provider.id")}</Label>
                  <Input id="provider-id" disabled={Boolean(editingID)} {...form.register("id")} />
                  {form.formState.errors.id?.message ? (
                    <div className="text-xs text-destructive">{form.formState.errors.id.message}</div>
                  ) : null}
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="provider-name">{t("provider.name")}</Label>
                  <Input id="provider-name" {...form.register("name")} />
                </div>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="grid gap-2">
                  <Label>{t("provider.type")}</Label>
                  <Select
                    value={providerType}
                    onValueChange={(value) =>
                      form.setValue("type", value as ProviderFormValue["type"], { shouldDirty: true })
                    }
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="openai-responses">openai-responses</SelectItem>
                      <SelectItem value="openai-compatible">openai-compatible</SelectItem>
                      <SelectItem value="google">google</SelectItem>
                      <SelectItem value="anthropic">anthropic</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="provider-base-url">Base URL</Label>
                  <Input id="provider-base-url" placeholder="https://api.openai.com/v1" {...form.register("baseURL")} />
                </div>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="grid gap-2">
                  <Label htmlFor="provider-api-key">API Key</Label>
                  <Input
                    id="provider-api-key"
                    autoComplete="off"
                    placeholder={editingID ? t("provider.apiKeyKeep") : "sk-..."}
                    type="password"
                    {...form.register("apiKey")}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="provider-api-key-env">{t("provider.apiKeyEnv")}</Label>
                  <Input id="provider-api-key-env" placeholder="OPENAI_API_KEY" {...form.register("apiKeyEnv")} />
                </div>
              </div>

              <div className="grid gap-2">
                <div className="flex items-center justify-between gap-2">
                  <Label>{t("provider.models")}</Label>
                  <div className="flex gap-2">
                    <Button disabled={!editingID || candidatesLoading} size="sm" type="button" variant="ghost" onClick={onLoadCandidates}>
                      {candidatesLoading ? <Loader2 className="animate-spin" /> : null}
                      {t("provider.loadCandidates")}
                    </Button>
                    <Button size="sm" type="button" variant="outline" onClick={onAppendModel}>
                      <Plus />
                      {t("provider.addModel")}
                    </Button>
                  </div>
                </div>
                <div className="grid gap-3">
                  {fields.map((field, index) => (
                    <ModelEditor
                      key={field.id}
                      canRemove={fields.length > 1}
                      form={form}
                      index={index}
                      providerType={providerType}
                      onRemove={() => onRemoveModel(index)}
                    />
                  ))}
                </div>
              </div>
            </div>
          </div>
          <DialogFooter className="m-0 rounded-none">
            <Button disabled={saving} type="submit">
              {saving ? <Loader2 className="animate-spin" /> : <KeyRound />}
              {t("common.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ModelEditor({
  index,
  providerType,
  canRemove,
  form,
  onRemove,
}: {
  index: number;
  providerType: ProviderFormValue["type"];
  canRemove: boolean;
  form: ReturnType<typeof useForm<ProviderFormValue>>;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const prefix = `models.${index}` as const;
  const watch = form.watch;
  const setBoolean = (name: "image" | "audio" | "tools", checked: boolean | "indeterminate") => {
    form.setValue(`${prefix}.${name}`, checked === true, { shouldDirty: true });
  };

  return (
    <div className="grid gap-3 rounded-lg border p-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <Badge variant={index === 0 ? "default" : "outline"}>{index === 0 ? t("common.default") : index + 1}</Badge>
          <span className="truncate text-sm font-normal">{watch(`${prefix}.id`) || t("session.model")}</span>
        </div>
        <Button disabled={!canRemove} size="icon" type="button" variant="ghost" onClick={onRemove}>
          <Trash2 />
        </Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_8rem]">
        <div className="grid gap-2">
          <Label>{t("provider.modelID")}</Label>
          <Input {...form.register(`${prefix}.id`)} />
        </div>
        <div className="grid gap-2">
          <Label>{t("provider.modelName")}</Label>
          <Input {...form.register(`${prefix}.name`)} />
        </div>
        <div className="grid gap-2">
          <Label>{t("provider.contextWindow")}</Label>
          <Input inputMode="numeric" {...form.register(`${prefix}.contextWindow`)} />
        </div>
      </div>

      <div className="flex flex-wrap gap-4">
        <CheckField checked={watch(`${prefix}.image`)} label="Image" onCheckedChange={(checked) => setBoolean("image", checked)} />
        <CheckField checked={watch(`${prefix}.audio`)} label="Audio" onCheckedChange={(checked) => setBoolean("audio", checked)} />
        <CheckField checked={watch(`${prefix}.tools`)} label="Tools" onCheckedChange={(checked) => setBoolean("tools", checked)} />
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="grid gap-2">
          <Label>{t("provider.temperature")}</Label>
          <Input inputMode="decimal" placeholder="0.7" {...form.register(`${prefix}.temperature`)} />
        </div>
        {providerType === "anthropic" ? (
          <div className="grid gap-2">
            <Label>{t("provider.maxOutput")}</Label>
            <Input inputMode="numeric" placeholder="4096" {...form.register(`${prefix}.anthropicMaxTokens`)} />
          </div>
        ) : providerType === "google" ? (
          <div className="grid gap-2">
            <Label>{t("provider.maxOutput")}</Label>
            <Input inputMode="numeric" {...form.register(`${prefix}.maxCompletionTokens`)} />
          </div>
        ) : (
          <>
            <div className="grid gap-2">
              <Label>{t("provider.reasoningEffort")}</Label>
              <Input placeholder="low / medium / high" {...form.register(`${prefix}.reasoningEffort`)} />
            </div>
            <div className="grid gap-2">
              <Label>{t("provider.maxOutput")}</Label>
              <Input inputMode="numeric" {...form.register(`${prefix}.maxCompletionTokens`)} />
            </div>
          </>
        )}
      </div>
      {providerType !== "anthropic" && providerType !== "google" ? (
        <div className="grid gap-2 sm:w-40">
          <Label>{t("provider.maxToolLoops")}</Label>
          <Input inputMode="numeric" {...form.register(`${prefix}.maxToolLoops`)} />
        </div>
      ) : null}
    </div>
  );
}

function DefaultProfileControl({
  defaultProvider,
  disabled,
  profiles,
  onChange,
}: {
  defaultProvider: string;
  disabled: boolean;
  profiles: ProviderProfile[];
  onChange: (value: string) => void;
}) {
  const knownDefault = profiles.some((profile) => profile.id === defaultProvider);
  return (
    <Select disabled={disabled} value={defaultProvider} onValueChange={onChange}>
      <SelectTrigger className="w-full sm:w-80">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {!knownDefault && defaultProvider ? <SelectItem value={defaultProvider}>{defaultProvider}</SelectItem> : null}
        {profiles.map((profile) => (
          <SelectItem key={profile.id} value={profile.id}>
            {profile.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
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
      <div className="flex min-h-11 items-center justify-between gap-3 border-b bg-muted/20 px-4 py-2">
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

function CheckField({
  label,
  checked,
  onCheckedChange,
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (checked: boolean | "indeterminate") => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <Checkbox checked={checked} onCheckedChange={onCheckedChange} />
      {label}
    </label>
  );
}

function ProviderSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}

function emptyProviderForm(): ProviderFormValue {
  return {
    id: "",
    name: "",
    type: "openai-compatible",
    baseURL: "",
    apiKey: "",
    apiKeyEnv: "",
    models: [emptyModel()],
  };
}

function emptyModel(id = ""): ModelFormValue {
  return {
    id,
    name: "",
    contextWindow: "",
    image: false,
    audio: false,
    tools: true,
    temperature: "",
    reasoningEffort: "",
    maxCompletionTokens: "",
    maxToolLoops: "",
    anthropicMaxTokens: "",
  };
}

function presetToForm(preset: ProviderPreset): ProviderFormValue {
  return {
    id: providerPresetName(preset),
    name: preset.name,
    type: preset.type,
    baseURL: preset.baseURL,
    apiKey: "",
    apiKeyEnv: preset.apiKeyOptional ? "" : `${preset.id.toUpperCase()}_API_KEY`,
    models: preset.models.map(modelToForm),
  };
}

function providerToForm(profile: ProviderProfile): ProviderFormValue {
  return {
    id: profile.id,
    name: profile.name,
    type: profile.type,
    baseURL: profile.baseURL,
    apiKey: "",
    apiKeyEnv: profile.apiKeyEnv || "",
    models: profile.models.length > 0 ? profile.models.map(modelToForm) : [emptyModel()],
  };
}

function modelToForm(model: ProviderModel): ModelFormValue {
  const options = model.openai || model.google || model.anthropic || {};
  return {
    id: model.id,
    name: model.name || "",
    contextWindow: model.contextWindow ? String(model.contextWindow) : "",
    image: model.capabilities?.image === true,
    audio: model.capabilities?.audio === true,
    tools: model.capabilities?.tools !== false,
    temperature: stringifyOption(options.temperature),
    reasoningEffort: stringifyOption(model.openai?.reasoning_effort),
    maxCompletionTokens: stringifyOption(model.openai?.max_completion_tokens ?? model.google?.maxOutputTokens),
    maxToolLoops: stringifyOption(model.openai?.max_tool_loops),
    anthropicMaxTokens: stringifyOption(model.anthropic?.max_tokens),
  };
}

function cleanCreateProvider(value: ProviderFormValue) {
  return createProviderRequest.parse({
    id: value.id.trim(),
    name: value.name.trim(),
    type: value.type,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim(),
    apiKeyEnv: value.apiKeyEnv?.trim(),
    models: value.models.map((model) => cleanModel(model, value.type)),
  });
}

function cleanPatchProvider(value: ProviderFormValue) {
  return patchProviderRequest.parse({
    name: value.name.trim(),
    type: value.type,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim() || undefined,
    apiKeyEnv: value.apiKeyEnv?.trim(),
    models: value.models.map((model) => cleanModel(model, value.type)),
  });
}

function cleanModel(value: ModelFormValue, providerType: ProviderFormValue["type"]): ProviderModel {
  const out: ProviderModel = {
    id: value.id.trim(),
    capabilities: {
      image: value.image,
      audio: value.audio,
      tools: value.tools,
    },
  };
  const name = value.name?.trim();
  if (name) {
    out.name = name;
  }
  const contextWindow = positiveInt(value.contextWindow);
  if (contextWindow) {
    out.contextWindow = contextWindow;
  }
  const temperature = numberValue(value.temperature);
  if (providerType === "anthropic") {
    out.anthropic = compactOptions({
      temperature,
      max_tokens: positiveInt(value.anthropicMaxTokens),
    });
  } else if (providerType === "google") {
    out.google = compactOptions({
      temperature,
      maxOutputTokens: positiveInt(value.maxCompletionTokens),
    });
  } else {
    out.openai = compactOptions({
      temperature,
      reasoning_effort: value.reasoningEffort?.trim() || undefined,
      max_completion_tokens: positiveInt(value.maxCompletionTokens),
      max_tool_loops: positiveInt(value.maxToolLoops),
    });
  }
  return out;
}

function stringifyOption(value: unknown) {
  if (value === undefined || value === null) {
    return "";
  }
  return String(value);
}

function positiveInt(value: string | undefined) {
  const parsed = Number.parseInt((value || "").trim(), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function numberValue(value: string | undefined) {
  const parsed = Number.parseFloat((value || "").trim());
  return Number.isFinite(parsed) ? parsed : undefined;
}

function compactOptions(options: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(options).filter(([, value]) => value !== undefined && value !== ""));
}

function uniqueProfileID(baseID: string, existingIDs: string[]) {
  const existing = new Set(existingIDs);
  if (baseID && !existing.has(baseID)) {
    return baseID;
  }
  const base = `${baseID || "profile"}-copy`;
  if (!existing.has(base)) {
    return base;
  }
  for (let index = 2; index < 1000; index += 1) {
    const candidate = `${base}-${index}`;
    if (!existing.has(candidate)) {
      return candidate;
    }
  }
  return `${base}-${Date.now()}`;
}
