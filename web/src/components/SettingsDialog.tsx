import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Loader2, Plus, Settings, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import {
  APIError,
  createProvider,
  createProviderRequest,
  deleteProvider,
  getSettings,
  listProviderModels,
  listProviders,
  patchProvider,
  patchProviderRequest,
  putSettings,
  type ProviderProfile,
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
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { BrandIcon } from "@/components/BrandIcons";
import { getOrderedProviderPresets, providerPresetName } from "@/provider/presets";
import { cn } from "@/lib/utils";

// 未显式设置时后端回落到名为 "default" 的 profile,表单直接以它为初值
const DEFAULT_PROVIDER = "default";

const providerFormSchema = createProviderRequest.extend({
  name: z.string().trim().min(1),
  baseURL: z.string().trim().optional(),
  apiKey: z.string().optional(),
  modelsText: z.string().optional(),
});

function linesToModels(text: string | undefined) {
  return Array.from(
    new Set(
      (text || "")
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean),
    ),
  );
}

const defaultsFormSchema = z.object({
  providerDefault: z.string(),
  systemPrompt: z.string(),
});

type ProviderFormValue = z.infer<typeof providerFormSchema>;
type DefaultsFormValue = z.infer<typeof defaultsFormSchema>;

type SettingsDialogProps = {
  token: string;
};

export function SettingsDialog({ token }: SettingsDialogProps) {
  const { t } = useI18n();

  return (
    <Dialog>
      <Tooltip>
        <TooltipTrigger asChild>
          <DialogTrigger asChild>
            <Button aria-label={t("settings.title")} size="icon" variant="ghost">
              <Settings />
            </Button>
          </DialogTrigger>
        </TooltipTrigger>
        <TooltipContent>{t("settings.title")}</TooltipContent>
      </Tooltip>
      <DialogContent className="max-h-svh overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("settings.title")}</DialogTitle>
          <DialogDescription>{t("settings.description")}</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="providers">
          <TabsList>
            <TabsTrigger value="providers">{t("settings.providers")}</TabsTrigger>
            <TabsTrigger value="defaults">{t("settings.defaults")}</TabsTrigger>
          </TabsList>
          <TabsContent value="providers">
            <ProviderSettings token={token} />
          </TabsContent>
          <TabsContent value="defaults">
            <DefaultSettings token={token} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

function ProviderSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { locale, t } = useI18n();
  const [editingName, setEditingName] = useState<string | null>(null);
  const providerPresets = getOrderedProviderPresets(locale);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });

  const providerForm = useForm<ProviderFormValue>({
    resolver: zodResolver(providerFormSchema),
    defaultValues: emptyProviderForm(),
  });

  const createMutation = useMutation({
    mutationFn: (value: ProviderFormValue) => createProvider(token, cleanCreateProvider(value)),
    onSuccess: async (profile) => {
      setEditingName(profile.name);
      providerForm.reset(providerToForm(profile));
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === "profile_exists") {
        providerForm.setError("name", { message: t("provider.profileExists") });
        return;
      }
      providerForm.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  const patchMutation = useMutation({
    mutationFn: (value: ProviderFormValue) => {
      if (!editingName) {
        throw new Error("missing provider name");
      }
      return patchProvider(token, editingName, cleanPatchProvider(value));
    },
    onSuccess: async (profile) => {
      providerForm.reset(providerToForm(profile));
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
    onError: (error) => {
      providerForm.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (name: string) => deleteProvider(token, name),
    onSuccess: async (_, name) => {
      if (editingName === name) {
        setEditingName(null);
        providerForm.reset(emptyProviderForm());
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const saving = createMutation.isPending || patchMutation.isPending;
  const [candidatesLoading, setCandidatesLoading] = useState(false);

  // 端点 /models 代理只在这里使用:把候选并入配置的模型清单
  async function loadCandidates() {
    if (!editingName) {
      return;
    }
    setCandidatesLoading(true);
    try {
      const { models } = await listProviderModels(token, editingName);
      const merged = Array.from(new Set([...linesToModels(providerForm.getValues("modelsText")), ...models]));
      providerForm.setValue("modelsText", merged.join("\n"), { shouldDirty: true, shouldTouch: true });
    } catch {
      providerForm.setError("root", { message: t("provider.candidatesFailed") });
    } finally {
      setCandidatesLoading(false);
    }
  }

  function startCreate() {
    setEditingName(null);
    providerForm.reset(emptyProviderForm());
  }

  function editProfile(profile: ProviderProfile) {
    setEditingName(profile.name);
    providerForm.reset(providerToForm(profile));
  }

  function applyPreset(preset: (typeof providerPresets)[number]) {
    setEditingName(null);
    providerForm.reset({
      name: providerPresetName(preset),
      type: preset.type,
      baseURL: preset.baseURL,
      apiKey: "",
      extra: "",
      defaultModel: preset.defaultModel,
      modelsText: preset.models.join("\n"),
    });
  }

  function submitProvider(value: ProviderFormValue) {
    if (editingName) {
      patchMutation.mutate(value);
      return;
    }
    createMutation.mutate(value);
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,0.9fr)]">
      <div className="grid gap-3">
        <div className="flex items-center justify-between gap-2">
          <Label>{t("settings.providerPresets")}</Label>
          <Button size="sm" type="button" variant="outline" onClick={startCreate}>
            <Plus />
            {t("provider.new")}
          </Button>
        </div>
        <div className="grid grid-cols-2 gap-2">
          {providerPresets.map((preset) => (
            <Button
              key={preset.id}
              className="h-auto min-h-12 flex-col items-start gap-1 px-3 py-2 text-left"
              type="button"
              variant="outline"
              onClick={() => applyPreset(preset)}
            >
              <span>{preset.name}</span>
              <span className="max-w-full truncate text-xs font-normal text-muted-foreground">
                {preset.defaultModel}
              </span>
            </Button>
          ))}
        </div>
        <div className="grid gap-2">
          <Label>{t("provider.list")}</Label>
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
            <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
              {t("provider.empty")}
            </div>
          ) : null}
          <div className="grid gap-2">
            {profiles.map((profile) => (
              <button
                key={profile.name}
                className={cn(
                  "grid gap-1 rounded-lg border bg-card p-3 text-left text-sm transition-colors hover:bg-muted",
                  editingName === profile.name && "border-primary",
                )}
                type="button"
                onClick={() => editProfile(profile)}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{profile.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {profile.apiKeySet ? t("provider.keySet") : t("provider.keyMissing")}
                  </span>
                </div>
                <div className="truncate text-xs text-muted-foreground">{profile.type}</div>
                <div className="truncate text-xs text-muted-foreground">{profile.baseURL || t("provider.defaultEndpoint")}</div>
              </button>
            ))}
          </div>
        </div>
      </div>
      <form className="grid content-start gap-3" onSubmit={providerForm.handleSubmit(submitProvider)}>
        <div className="grid gap-1">
          <div className="text-sm font-medium">{editingName ? t("provider.edit") : t("provider.create")}</div>
          <div className="text-xs text-muted-foreground">{t("provider.keyHint")}</div>
        </div>
        {providerForm.formState.errors.root?.message ? (
          <Alert variant="destructive">
            <AlertDescription>{providerForm.formState.errors.root.message}</AlertDescription>
          </Alert>
        ) : null}
        <div className="grid gap-2">
          <Label htmlFor="provider-name">{t("provider.name")}</Label>
          <Input id="provider-name" disabled={Boolean(editingName)} {...providerForm.register("name")} />
          {providerForm.formState.errors.name?.message ? (
            <div className="text-xs text-destructive">{providerForm.formState.errors.name.message}</div>
          ) : null}
        </div>
        <div className="grid gap-2">
          <Label>{t("provider.type")}</Label>
          <Select
            value={providerForm.watch("type")}
            onValueChange={(value) => providerForm.setValue("type", value as ProviderFormValue["type"], { shouldDirty: true })}
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="openai-compatible">openai-compatible</SelectItem>
              <SelectItem value="google">google</SelectItem>
              <SelectItem value="anthropic">anthropic</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="provider-base-url">Base URL</Label>
          <Input id="provider-base-url" placeholder="https://api.openai.com/v1" {...providerForm.register("baseURL")} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="provider-api-key">API Key</Label>
          <Input id="provider-api-key" type="password" placeholder={editingName ? t("provider.apiKeyKeep") : "sk-..."} {...providerForm.register("apiKey")} />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="provider-default-model">{t("settings.defaultModel")}</Label>
          <Input id="provider-default-model" placeholder="gpt-5.5" {...providerForm.register("defaultModel")} />
        </div>
        <div className="grid gap-2">
          <div className="flex items-center justify-between gap-2">
            <Label htmlFor="provider-models">{t("provider.models")}</Label>
            <Button
              disabled={!editingName || candidatesLoading}
              size="sm"
              type="button"
              variant="ghost"
              onClick={() => void loadCandidates()}
            >
              {candidatesLoading ? <Loader2 className="animate-spin" /> : null}
              {t("provider.loadCandidates")}
            </Button>
          </div>
          <Textarea
            className="min-h-24 font-mono text-xs"
            id="provider-models"
            placeholder={"deepseek-v4-flash\ndeepseek-v4-pro"}
            {...providerForm.register("modelsText")}
          />
        </div>
        <DialogFooter>
          {editingName ? (
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button disabled={deleteMutation.isPending} type="button" variant="destructive">
                  <Trash2 />
                  {t("common.delete")}
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>{t("provider.deleteTitle")}</AlertDialogTitle>
                  <AlertDialogDescription>{t("provider.deleteDescription")}</AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                  <AlertDialogAction variant="destructive" onClick={() => deleteMutation.mutate(editingName)}>
                    {t("common.delete")}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          ) : null}
          <Button disabled={saving} type="submit">
            {saving ? <Loader2 className="animate-spin" /> : <KeyRound />}
            {t("common.save")}
          </Button>
        </DialogFooter>
      </form>
    </div>
  );
}

function DefaultSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
  });
  const defaultsForm = useForm<DefaultsFormValue>({
    resolver: zodResolver(defaultsFormSchema),
    defaultValues: {
      providerDefault: DEFAULT_PROVIDER,
      systemPrompt: "",
    },
  });

  useEffect(() => {
    if (!settingsQuery.data) {
      return;
    }
    const settings = settingsQuery.data.settings;
    defaultsForm.reset({
      providerDefault: settings["provider.default"] || DEFAULT_PROVIDER,
      systemPrompt: settings.system_prompt || "",
    });
  }, [defaultsForm, settingsQuery.data]);

  const saveMutation = useMutation({
    mutationFn: (value: DefaultsFormValue) => putSettings(token, defaultsPayload(value)),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.settings() }),
  });

  return (
    <form className="grid gap-4" onSubmit={defaultsForm.handleSubmit((value) => saveMutation.mutate(value))}>
      {settingsQuery.isLoading ? (
        <div className="grid gap-2">
          <Skeleton className="h-8" />
          <Skeleton className="h-8" />
          <Skeleton className="h-24" />
        </div>
      ) : null}
      {settingsQuery.isError ? (
        <Alert variant="destructive">
          <AlertDescription className="grid gap-2">
            <span>{t("settings.loadFailed")}</span>
            <Button size="sm" type="button" variant="outline" onClick={() => void settingsQuery.refetch()}>
              {t("common.refresh")}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      <div className="grid gap-2">
        <Label>{t("settings.defaultProvider")}</Label>
        <Select
          value={defaultsForm.watch("providerDefault")}
          onValueChange={(value) => defaultsForm.setValue("providerDefault", value, { shouldDirty: true })}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(providersQuery.data?.providers || []).map((profile) => (
              <SelectItem key={profile.name} value={profile.name}>
                {profile.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="system-prompt">system_prompt</Label>
        <Textarea id="system-prompt" className="min-h-32" {...defaultsForm.register("systemPrompt")} />
      </div>
      <DialogFooter>
        <Button disabled={saveMutation.isPending} type="submit">
          {saveMutation.isPending ? <Loader2 className="animate-spin" /> : null}
          {t("common.save")}
        </Button>
      </DialogFooter>
    </form>
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
    name: "",
    type: "openai-compatible",
    baseURL: "",
    apiKey: "",
    extra: "",
    defaultModel: "",
    modelsText: "",
  };
}

function providerToForm(profile: ProviderProfile): ProviderFormValue {
  return {
    name: profile.name,
    type: profile.type,
    baseURL: profile.baseURL,
    apiKey: "",
    extra: profile.extra || "",
    defaultModel: profile.defaultModel || "",
    modelsText: profile.models.join("\n"),
  };
}

function cleanCreateProvider(value: ProviderFormValue) {
  const parsed = createProviderRequest.parse({
    name: value.name.trim(),
    type: value.type,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim(),
    defaultModel: value.defaultModel?.trim(),
    models: linesToModels(value.modelsText),
    extra: value.extra?.trim(),
  });
  return parsed;
}

function cleanPatchProvider(value: ProviderFormValue) {
  const parsed = patchProviderRequest.parse({
    type: value.type,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim() || undefined,
    defaultModel: value.defaultModel?.trim(),
    models: linesToModels(value.modelsText),
    extra: value.extra?.trim(),
  });
  return parsed;
}

function defaultsPayload(value: DefaultsFormValue) {
  const payload: Record<string, string> = {
    // 旧全局键随本次重构清空:默认模型已是 profile 属性
    "model.default": "",
    "provider.default": value.providerDefault,
  };
  if (value.systemPrompt.trim()) {
    payload.system_prompt = value.systemPrompt;
  }
  return payload;
}
