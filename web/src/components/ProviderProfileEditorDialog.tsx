import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Loader2, Plus, Trash } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { z } from "zod";

import {
  APIError,
  createProvider,
  createProviderRequest,
  listProviderModels,
  patchProvider,
  patchProviderRequest,
  type ProviderModel,
  type ProviderProfile,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
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
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/i18n";

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

export type ProviderProfileEditorValue = z.infer<typeof providerFormSchema>;
type ModelFormValue = z.infer<typeof modelFormSchema>;

export function ProviderProfileEditorDialog({
  initialValue,
  open,
  profile,
  profiles = [],
  token,
  onOpenChange,
  onSaved,
}: {
  initialValue?: ProviderProfileEditorValue | null;
  open: boolean;
  profile?: ProviderProfile | null;
  profiles?: ProviderProfile[];
  token: string;
  onOpenChange: (open: boolean) => void;
  onSaved?: (profile: ProviderProfile) => void;
}) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const editingID = profile?.id || null;
  const [candidatesLoading, setCandidatesLoading] = useState(false);
  const form = useForm<ProviderProfileEditorValue>({
    resolver: zodResolver(providerFormSchema),
    defaultValues: emptyProviderProfileForm(),
  });
  const fields = useFieldArray({ control: form.control, name: "models" });
  const providerType = form.watch("type");
  const existingIDs = useMemo(() => profiles.map((item) => item.id), [profiles]);

  useEffect(() => {
    if (!open) {
      return;
    }
    form.reset(profile ? providerToForm(profile) : initialValue || emptyProviderProfileForm());
  }, [form, initialValue, open, profile]);

  const createMutation = useMutation({
    mutationFn: (value: ProviderProfileEditorValue) => createProvider(token, cleanCreateProvider(value)),
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      onSaved?.(saved);
      onOpenChange(false);
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === "profile_exists") {
        form.setError("id", { message: t("provider.profileExists") });
        return;
      }
      form.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  const patchMutation = useMutation({
    mutationFn: (value: ProviderProfileEditorValue) => {
      if (!editingID) {
        throw new Error("missing provider id");
      }
      return patchProvider(token, editingID, cleanPatchProvider(value));
    },
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      onSaved?.(saved);
      onOpenChange(false);
    },
    onError: (error) => {
      form.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  async function loadCandidates() {
    if (!editingID) {
      return;
    }
    setCandidatesLoading(true);
    try {
      const { models } = await listProviderModels(token, editingID);
      const existing = new Set(form.getValues("models").map((model) => model.id.trim()).filter(Boolean));
      for (const id of models) {
        if (!existing.has(id)) {
          fields.append(emptyModel(id));
          existing.add(id);
        }
      }
    } catch {
      form.setError("root", { message: t("provider.candidatesFailed") });
    } finally {
      setCandidatesLoading(false);
    }
  }

  function submitProvider(value: ProviderProfileEditorValue) {
    if (!editingID && existingIDs.includes(value.id.trim())) {
      form.setError("id", { message: t("provider.profileExists") });
      return;
    }
    if (editingID) {
      patchMutation.mutate(value);
      return;
    }
    createMutation.mutate(value);
  }

  const saving = createMutation.isPending || patchMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={saving ? undefined : onOpenChange}>
      <DialogContent className="top-[calc(var(--toolbar-h)+(100svh-var(--toolbar-h))/2)] grid h-[min(900px,calc(100svh-var(--toolbar-h)-1.5rem))] w-[min(920px,calc(100vw-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="px-5 py-4 pr-14">
          <DialogTitle>{editingID ? t("provider.edit") : t("provider.create")}</DialogTitle>
          <DialogDescription>{t("provider.keyHint")}</DialogDescription>
        </DialogHeader>
        <form className="contents" onSubmit={form.handleSubmit(submitProvider)}>
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
                      form.setValue("type", value as ProviderProfileEditorValue["type"], { shouldDirty: true })
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
                    <Button disabled={!editingID || candidatesLoading} size="sm" type="button" variant="ghost" onClick={() => void loadCandidates()}>
                      {candidatesLoading ? <Loader2 className="animate-spin" /> : null}
                      {t("provider.loadCandidates")}
                    </Button>
                    <Button size="sm" type="button" variant="outline" onClick={() => fields.append(emptyModel())}>
                      <Plus />
                      {t("provider.addModel")}
                    </Button>
                  </div>
                </div>
                <div className="grid gap-3">
                  {fields.fields.map((field, index) => (
                    <ModelEditor
                      key={field.id}
                      canRemove={fields.fields.length > 1}
                      form={form}
                      index={index}
                      providerType={providerType}
                      onRemove={() => fields.remove(index)}
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
  providerType: ProviderProfileEditorValue["type"];
  canRemove: boolean;
  form: ReturnType<typeof useForm<ProviderProfileEditorValue>>;
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
          <Badge variant="outline">{index + 1}</Badge>
          <span className="truncate text-sm font-normal">{watch(`${prefix}.id`) || t("session.model")}</span>
        </div>
        <Button disabled={!canRemove} size="icon" type="button" variant="ghost" onClick={onRemove}>
          <Trash />
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

export function emptyProviderProfileForm(): ProviderProfileEditorValue {
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

export function cloneProviderProfileForm(
  profile: ProviderProfile,
  profiles: ProviderProfile[],
  copySuffix: string,
): ProviderProfileEditorValue {
  return {
    ...providerToForm(profile),
    id: uniqueProfileID(profile.id, profiles.map((item) => item.id)),
    name: `${profile.name} ${copySuffix}`,
    apiKey: "",
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

function providerToForm(profile: ProviderProfile): ProviderProfileEditorValue {
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

function cleanCreateProvider(value: ProviderProfileEditorValue) {
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

function cleanPatchProvider(value: ProviderProfileEditorValue) {
  return patchProviderRequest.parse({
    name: value.name.trim(),
    type: value.type,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim() || undefined,
    apiKeyEnv: value.apiKeyEnv?.trim(),
    models: value.models.map((model) => cleanModel(model, value.type)),
  });
}

function cleanModel(value: ModelFormValue, providerType: ProviderProfileEditorValue["type"]): ProviderModel {
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
