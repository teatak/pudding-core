import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Eye, EyeOff, Loader2, Plus, Trash } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
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
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { generateProviderProfileID } from "@/provider/presets";

const providerProtocolSchema = z.enum(["openai-compatible", "openai-responses", "google", "anthropic"]);

const modelFormSchema = z.object({
  id: z.string().trim().min(1),
  displayName: z.string().optional(),
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
  displayName: z.string().trim().min(1),
  brand: z.string().optional(),
  protocol: providerProtocolSchema,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
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
  const [apiKeyVisible, setAPIKeyVisible] = useState(false);
  const form = useForm<ProviderProfileEditorValue>({
    resolver: zodResolver(providerFormSchema),
    defaultValues: emptyProviderProfileForm(),
  });
  const fields = useFieldArray({ control: form.control, name: "models" });
  const providerProtocol = form.watch("protocol");
  const profileID = form.watch("id");
  const existingIDs = useMemo(() => profiles.map((item) => item.id), [profiles]);
  const showAPIKeyToggle = Boolean(editingID);

  useEffect(() => {
    if (!open) {
      return;
    }
    const nextValue = profile
      ? providerToForm(profile)
      : initialValue
        ? {
            ...initialValue,
            id: initialValue.id || generateProviderProfileID(profiles, initialValue.brand || "custom"),
          }
        : emptyProviderProfileForm(generateProviderProfileID(profiles, "custom"));
    form.reset(nextValue);
    setAPIKeyVisible(false);
  }, [form, initialValue, open, profile, profiles]);

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
          <input type="hidden" {...form.register("brand")} />
          <input type="hidden" {...form.register("id")} />
          <div className="min-h-0 overflow-y-auto border-y px-5 py-4 [mask-image:linear-gradient(to_bottom,transparent_0,black_16px,black_calc(100%-16px),transparent_100%)]">
            <div className="grid gap-4">
              {form.formState.errors.root?.message ? (
                <Alert variant="destructive">
                  <AlertDescription>{form.formState.errors.root.message}</AlertDescription>
                </Alert>
              ) : null}

              <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(15rem,22rem)]">
                <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
                  <FieldLabel className="w-auto cursor-default text-sm font-medium" htmlFor="provider-name">
                    {t("provider.name")}
                  </FieldLabel>
                  <Input id="provider-name" {...form.register("displayName")} />
                  {form.formState.errors.id?.message ? (
                    <div className="text-xs text-destructive">{form.formState.errors.id.message}</div>
                  ) : null}
                </Field>
                <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
                  <FieldLabel className="w-auto cursor-default text-sm font-medium">{t("provider.profileID")}</FieldLabel>
                  <div className="flex h-9 min-w-0 items-center rounded-md border bg-muted/30 px-3 text-sm text-muted-foreground">
                    <span className="truncate font-mono">{profileID}</span>
                  </div>
                </Field>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
                  <FieldLabel className="w-auto cursor-default text-sm font-medium">{t("provider.protocol")}</FieldLabel>
                  <Select
                    value={providerProtocol}
                    onValueChange={(value) =>
                      form.setValue("protocol", value as ProviderProfileEditorValue["protocol"], { shouldDirty: true })
                    }
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="openai-compatible">OpenAI</SelectItem>
                      <SelectItem value="openai-responses">OpenAI Responses</SelectItem>
                      <SelectItem value="google">Google</SelectItem>
                      <SelectItem value="anthropic">Anthropic</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
                  <FieldLabel className="w-auto cursor-default text-sm font-medium" htmlFor="provider-base-url">
                    {t("provider.baseURL")}
                  </FieldLabel>
                  <Input id="provider-base-url" placeholder="https://api.openai.com/v1" {...form.register("baseURL")} />
                </Field>
              </div>

              <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
                <FieldLabel className="w-auto cursor-default text-sm font-medium" htmlFor="provider-api-key">
                  {t("provider.apiKey")}
                </FieldLabel>
                <div className="relative">
                  <Input
                    id="provider-api-key"
                    className={showAPIKeyToggle ? "pr-9" : undefined}
                    autoComplete="off"
                    placeholder={showAPIKeyToggle && profile?.apiKeySet ? "••••••••••••" : "sk-..."}
                    type={showAPIKeyToggle && !apiKeyVisible ? "password" : "text"}
                    {...form.register("apiKey")}
                  />
                  {showAPIKeyToggle ? (
                    <button
                      aria-label={apiKeyVisible ? t("provider.hideAPIKey") : t("provider.showAPIKey")}
                      className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
                      type="button"
                      onClick={() => setAPIKeyVisible((visible) => !visible)}
                    >
                      {apiKeyVisible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                    </button>
                  ) : null}
                </div>
              </Field>

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
                      providerProtocol={providerProtocol}
                      onRemove={() => fields.remove(index)}
                    />
                  ))}
                </div>
              </div>
            </div>
          </div>
          <DialogFooter className="m-0 rounded-none">
            <Button disabled={saving} type="submit">
              {saving ? <Loader2 className="animate-spin" /> : <Check />}
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
  providerProtocol,
  canRemove,
  form,
  onRemove,
}: {
  index: number;
  providerProtocol: ProviderProfileEditorValue["protocol"];
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
        <PlainField label={t("provider.modelID")}>
          <Input {...form.register(`${prefix}.id`)} />
        </PlainField>
        <PlainField label={t("provider.modelName")}>
          <Input {...form.register(`${prefix}.displayName`)} />
        </PlainField>
        <PlainField label={t("provider.contextWindow")}>
          <Input inputMode="numeric" {...form.register(`${prefix}.contextWindow`)} />
        </PlainField>
      </div>

      <div className="flex flex-wrap gap-4">
        <CheckField checked={watch(`${prefix}.image`)} label={t("provider.capability.image")} onCheckedChange={(checked) => setBoolean("image", checked)} />
        <CheckField checked={watch(`${prefix}.audio`)} label={t("provider.capability.audio")} onCheckedChange={(checked) => setBoolean("audio", checked)} />
        <CheckField checked={watch(`${prefix}.tools`)} label={t("provider.capability.tools")} onCheckedChange={(checked) => setBoolean("tools", checked)} />
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <PlainField label={t("provider.temperature")}>
          <Input inputMode="decimal" placeholder="0.7" {...form.register(`${prefix}.temperature`)} />
        </PlainField>
        {providerProtocol === "anthropic" ? (
          <PlainField label={t("provider.maxOutput")}>
            <Input inputMode="numeric" placeholder="4096" {...form.register(`${prefix}.anthropicMaxTokens`)} />
          </PlainField>
        ) : providerProtocol === "google" ? (
          <PlainField label={t("provider.maxOutput")}>
            <Input inputMode="numeric" {...form.register(`${prefix}.maxCompletionTokens`)} />
          </PlainField>
        ) : (
          <>
            <PlainField label={t("provider.reasoningEffort")}>
              <Input placeholder="low / medium / high" {...form.register(`${prefix}.reasoningEffort`)} />
            </PlainField>
            <PlainField label={t("provider.maxOutput")}>
              <Input inputMode="numeric" {...form.register(`${prefix}.maxCompletionTokens`)} />
            </PlainField>
          </>
        )}
      </div>
      {providerProtocol !== "anthropic" && providerProtocol !== "google" ? (
        <PlainField className="sm:w-40" label={t("provider.maxToolLoops")}>
          <Input inputMode="numeric" {...form.register(`${prefix}.maxToolLoops`)} />
        </PlainField>
      ) : null}
    </div>
  );
}

function PlainField({
  children,
  className,
  label,
}: {
  children: ReactNode;
  className?: string;
  label: string;
}) {
  return (
    <Field className={cn("gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent", className)}>
      <FieldLabel className="w-auto cursor-default text-sm font-medium">{label}</FieldLabel>
      {children}
    </Field>
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

export function emptyProviderProfileForm(id = ""): ProviderProfileEditorValue {
  return {
    id,
    displayName: "",
    brand: "",
    protocol: "openai-compatible",
    baseURL: "",
    apiKey: "",
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
    id: generateProviderProfileID(profiles, profile.brand || profile.id || "custom"),
    displayName: `${profile.displayName} ${copySuffix}`,
    brand: profile.brand || "",
    apiKey: "",
  };
}

function emptyModel(id = ""): ModelFormValue {
  return {
    id,
    displayName: "",
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
    displayName: profile.displayName,
    brand: profile.brand || "",
    protocol: profile.protocol,
    baseURL: profile.baseURL,
    apiKey: profile.apiKey || "",
    models: profile.models.length > 0 ? profile.models.map(modelToForm) : [emptyModel()],
  };
}

function modelToForm(model: ProviderModel): ModelFormValue {
  const options = model.providerOptions?.openai || model.providerOptions?.google || model.providerOptions?.anthropic || {};
  return {
    id: model.id,
    displayName: model.displayName || "",
    contextWindow: model.contextWindow ? String(model.contextWindow) : "",
    image: model.capabilities?.image === true,
    audio: model.capabilities?.audio === true,
    tools: model.capabilities?.tools !== false,
    temperature: stringifyOption(options.temperature),
    reasoningEffort: stringifyOption(model.providerOptions?.openai?.reasoning_effort),
    maxCompletionTokens: stringifyOption(model.limits?.maxOutputTokens),
    maxToolLoops: stringifyOption(model.limits?.maxToolLoops),
    anthropicMaxTokens: stringifyOption(model.limits?.maxOutputTokens),
  };
}

function cleanCreateProvider(value: ProviderProfileEditorValue) {
  return createProviderRequest.parse({
    id: value.id.trim(),
    displayName: value.displayName.trim(),
    brand: value.brand?.trim() || undefined,
    protocol: value.protocol,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim(),
    models: value.models.map((model) => cleanModel(model, value.protocol)),
  });
}

function cleanPatchProvider(value: ProviderProfileEditorValue) {
  return patchProviderRequest.parse({
    displayName: value.displayName.trim(),
    brand: value.brand?.trim() || undefined,
    protocol: value.protocol,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim() || undefined,
    models: value.models.map((model) => cleanModel(model, value.protocol)),
  });
}

function cleanModel(value: ModelFormValue, providerProtocol: ProviderProfileEditorValue["protocol"]): ProviderModel {
  const out: ProviderModel = {
    id: value.id.trim(),
    capabilities: {
      image: value.image,
      audio: value.audio,
      tools: value.tools,
    },
  };
  const displayName = value.displayName?.trim();
  if (displayName) {
    out.displayName = displayName;
  }
  const contextWindow = positiveInt(value.contextWindow);
  if (contextWindow) {
    out.contextWindow = contextWindow;
  }
  const temperature = numberValue(value.temperature);
  const maxOutputTokens = providerProtocol === "anthropic" ? positiveInt(value.anthropicMaxTokens) : positiveInt(value.maxCompletionTokens);
  const maxToolLoops = providerProtocol === "anthropic" || providerProtocol === "google" ? undefined : positiveInt(value.maxToolLoops);
  if (maxOutputTokens || maxToolLoops) {
    out.limits = {
      maxOutputTokens,
      maxToolLoops,
    };
  }
  if (providerProtocol === "anthropic") {
    const anthropic = compactOptions({ temperature });
    if (Object.keys(anthropic).length > 0) {
      out.providerOptions = { anthropic };
    }
  } else if (providerProtocol === "google") {
    const google = compactOptions({ temperature });
    if (Object.keys(google).length > 0) {
      out.providerOptions = { google };
    }
  } else {
    const openai = compactOptions({
      temperature,
      reasoning_effort: value.reasoningEffort?.trim() || undefined,
    });
    if (Object.keys(openai).length > 0) {
      out.providerOptions = { openai };
    }
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
