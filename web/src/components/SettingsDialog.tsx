import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, KeyRound, Loader2, Pencil, Plus, Settings, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
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
  patchProvider,
  patchProviderRequest,
  putSettings,
  type ProviderModel,
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
import { Badge } from "@/components/ui/badge";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { BrandIcon } from "@/components/BrandIcons";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { getOrderedProviderPresets, providerPresetName } from "@/provider/presets";

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

const defaultsFormSchema = z.object({
  providerDefault: z.string(),
  systemPrompt: z.string(),
});

type ProviderFormValue = z.infer<typeof providerFormSchema>;
type ModelFormValue = z.infer<typeof modelFormSchema>;
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
      <DialogContent className="max-h-svh overflow-y-auto sm:max-w-6xl">
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
  const [editingID, setEditingID] = useState<string | null>(null);
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
  const modelFields = useFieldArray({ control: providerForm.control, name: "models" });
  const providerType = providerForm.watch("type");

  const createMutation = useMutation({
    mutationFn: (value: ProviderFormValue) => createProvider(token, cleanCreateProvider(value)),
    onSuccess: async (profile) => {
      setEditingID(profile.id);
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
        providerForm.reset(emptyProviderForm());
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const saving = createMutation.isPending || patchMutation.isPending;
  const [candidatesLoading, setCandidatesLoading] = useState(false);

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
  }

  function editProfile(profile: ProviderProfile) {
    setEditingID(profile.id);
    providerForm.reset(providerToForm(profile));
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
  }

  function applyPreset(preset: (typeof providerPresets)[number]) {
    setEditingID(null);
    providerForm.reset({
      id: providerPresetName(preset),
      name: preset.name,
      type: preset.type,
      baseURL: preset.baseURL,
      apiKey: "",
      apiKeyEnv: preset.apiKeyOptional ? "" : `${preset.id.toUpperCase()}_API_KEY`,
      models: preset.models.map(modelToForm),
    });
  }

  function submitProvider(value: ProviderFormValue) {
    if (editingID) {
      patchMutation.mutate(value);
      return;
    }
    createMutation.mutate(value);
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[18rem_minmax(0,1fr)]">
      <div className="grid content-start gap-4">
        <div className="grid gap-2">
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
                className="h-auto min-h-14 items-start justify-start gap-2 px-3 py-2 text-left"
                type="button"
                variant="outline"
                onClick={() => applyPreset(preset)}
              >
                <BrandIcon className="mt-0.5 size-4 shrink-0" name={preset.id} />
                <span className="grid min-w-0 gap-0.5">
                  <span className="truncate">{preset.name}</span>
                  <span className="truncate text-xs font-normal text-muted-foreground">{preset.models[0]?.id}</span>
                </span>
              </Button>
            ))}
          </div>
        </div>

        <div className="grid gap-2">
          <div className="grid gap-0.5">
            <Label>{t("provider.list")}</Label>
            <div className="text-xs text-muted-foreground">{t("provider.listHint")}</div>
          </div>
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
          <div className="grid gap-2">
            {profiles.map((profile) => (
              <div
                key={profile.id}
                className={cn(
                  "grid gap-2 rounded-lg border bg-card p-3 text-sm transition-colors",
                  "hover:bg-muted/60",
                  editingID === profile.id && "border-primary",
                )}
              >
                <button className="grid min-w-0 gap-1 text-left" type="button" onClick={() => editProfile(profile)}>
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-2">
                      <BrandIcon className="size-4 shrink-0" name={profile.id} />
                      <span className="truncate font-medium">{profile.name}</span>
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {profile.apiKeySet ? t("provider.keySet") : t("provider.keyMissing")}
                    </span>
                  </div>
                  <div className="flex min-w-0 items-center gap-1">
                    <Badge className="text-[10px] font-normal" variant="outline">
                      {profile.type}
                    </Badge>
                    <span className="truncate text-xs text-muted-foreground">{profile.models[0]?.id || t("picker.noModels")}</span>
                  </div>
                </button>
                <div className="flex items-center gap-1 border-t pt-2">
                  <Button className="h-7 px-2" size="sm" type="button" variant="ghost" onClick={() => editProfile(profile)}>
                    <Pencil />
                    {t("provider.editShort")}
                  </Button>
                  <Button className="h-7 px-2" size="sm" type="button" variant="ghost" onClick={() => cloneProfile(profile)}>
                    <Copy />
                    {t("common.copy")}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <form className="overflow-hidden rounded-lg border bg-card" onSubmit={providerForm.handleSubmit(submitProvider)}>
        <div className="grid gap-1 border-b bg-muted/30 px-4 py-3">
          <div className="text-sm font-medium">{editingID ? t("provider.edit") : t("provider.create")}</div>
          <div className="text-xs text-muted-foreground">{t("provider.keyHint")}</div>
        </div>
        <div className="grid gap-4 p-4">
          {providerForm.formState.errors.root?.message ? (
            <Alert variant="destructive">
              <AlertDescription>{providerForm.formState.errors.root.message}</AlertDescription>
            </Alert>
          ) : null}

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="provider-id">{t("provider.id")}</Label>
              <Input id="provider-id" disabled={Boolean(editingID)} {...providerForm.register("id")} />
              {providerForm.formState.errors.id?.message ? (
                <div className="text-xs text-destructive">{providerForm.formState.errors.id.message}</div>
              ) : null}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="provider-name">{t("provider.name")}</Label>
              <Input id="provider-name" {...providerForm.register("name")} />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label>{t("provider.type")}</Label>
              <Select
                value={providerType}
                onValueChange={(value) => providerForm.setValue("type", value as ProviderFormValue["type"], { shouldDirty: true })}
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
              <Input id="provider-base-url" placeholder="https://api.openai.com/v1" {...providerForm.register("baseURL")} />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="provider-api-key">API Key</Label>
              <Input id="provider-api-key" type="password" placeholder={editingID ? t("provider.apiKeyKeep") : "sk-..."} {...providerForm.register("apiKey")} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="provider-api-key-env">{t("provider.apiKeyEnv")}</Label>
              <Input id="provider-api-key-env" placeholder="OPENAI_API_KEY" {...providerForm.register("apiKeyEnv")} />
            </div>
          </div>

          <div className="grid gap-2">
            <div className="flex items-center justify-between gap-2">
              <Label>{t("provider.models")}</Label>
              <div className="flex gap-2">
                <Button disabled={!editingID || candidatesLoading} size="sm" type="button" variant="ghost" onClick={() => void loadCandidates()}>
                  {candidatesLoading ? <Loader2 className="animate-spin" /> : null}
                  {t("provider.loadCandidates")}
                </Button>
                <Button size="sm" type="button" variant="outline" onClick={() => modelFields.append(emptyModel())}>
                  <Plus />
                  {t("provider.addModel")}
                </Button>
              </div>
            </div>
            <div className="grid gap-3">
              {modelFields.fields.map((field, index) => (
                <ModelEditor
                  key={field.id}
                  index={index}
                  providerType={providerType}
                  canRemove={modelFields.fields.length > 1}
                  form={providerForm}
                  onRemove={() => modelFields.remove(index)}
                />
              ))}
            </div>
          </div>
        </div>

        <div className="flex items-center justify-between gap-2 border-t bg-muted/30 p-3">
          <div>
            {editingID ? (
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
                  <AlertDialogAction variant="destructive" onClick={() => deleteMutation.mutate(editingID)}>
                    {t("common.delete")}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
            ) : null}
          </div>
          <Button disabled={saving} type="submit">
            {saving ? <Loader2 className="animate-spin" /> : <KeyRound />}
            {t("common.save")}
          </Button>
        </div>
      </form>
    </div>
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
          <span className="truncate text-sm font-medium">{watch(`${prefix}.id`) || t("session.model")}</span>
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
      {providerType !== "anthropic" ? (
        <div className="grid gap-2 sm:w-40">
          <Label>{t("provider.maxToolLoops")}</Label>
          <Input inputMode="numeric" {...form.register(`${prefix}.maxToolLoops`)} />
        </div>
      ) : null}
    </div>
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
              <SelectItem key={profile.id} value={profile.id}>
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
    maxCompletionTokens: stringifyOption(model.openai?.max_completion_tokens),
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
    out.google = compactOptions({ temperature });
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

function defaultsPayload(value: DefaultsFormValue) {
  const payload: Record<string, string> = {
    "provider.default": value.providerDefault,
    system_prompt: value.systemPrompt,
  };
  return payload;
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
