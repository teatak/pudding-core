import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, ChevronDown, Copy, Eye, EyeOff, Loader2, Plus, Trash } from "lucide-react";
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
  probeProviderModels,
  type ProviderModel,
  type ProviderProfile,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
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
import { Field, FieldContent, FieldLabel, FieldTitle } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverAnchor,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
} from "@/components/ui/popover";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import {
  generateProviderProfileID,
  mergeProviderModelCandidate,
  providerPresetForBrand,
  providerPresetGroups,
  providerPresetProtocolsForGroup,
  providerPresetVariantForSelection,
  providerPresetVariantGroup,
  type ProviderPreset,
  type ProviderPresetProtocol,
  type ProviderPresetVariant,
} from "@/provider/presets";

const providerProtocolSchema = z.enum(["openai-compatible", "openai-responses", "google", "anthropic"]);
const PROVIDER_PROTOCOL_OPTIONS: ProviderPresetProtocol[] = ["openai-compatible", "openai-responses", "google", "anthropic"];
const BASE_REASONING_EFFORT_OPTIONS = ["auto", "low", "medium", "high"];
const OPENAI_REASONING_EFFORT_OPTIONS = [...BASE_REASONING_EFFORT_OPTIONS, "xhigh"];
const optionSelectedClass = "border-primary/45 bg-primary/5 text-foreground";
const optionIdleClass = "bg-background text-muted-foreground";

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
  group: z.string().optional(),
  protocol: providerProtocolSchema,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  models: z.array(modelFormSchema),
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
  const [candidatePopoverOpen, setCandidatePopoverOpen] = useState(false);
  const [candidateIDs, setCandidateIDs] = useState<string[]>([]);
  const [selectedCandidateIDs, setSelectedCandidateIDs] = useState<string[]>([]);
  const [candidateFilter, setCandidateFilter] = useState("");
  const [apiKeyVisible, setAPIKeyVisible] = useState(false);
  const form = useForm<ProviderProfileEditorValue>({
    resolver: zodResolver(providerFormSchema),
    defaultValues: emptyProviderProfileForm(),
  });
  const fields = useFieldArray({ control: form.control, name: "models" });
  const providerBrand = form.watch("brand");
  const providerGroup = form.watch("group") || "default";
  const providerProtocol = form.watch("protocol");
  const profileID = form.watch("id");
  const existingIDs = useMemo(() => profiles.map((item) => item.id), [profiles]);
  const preset = useMemo(() => providerPresetForBrand(providerBrand), [providerBrand]);
  const presetGroups = useMemo(() => (preset ? providerPresetGroups(preset) : []), [preset]);
  const filteredCandidateIDs = useMemo(() => {
    const query = candidateFilter.trim().toLowerCase();
    if (!query) {
      return candidateIDs;
    }
    return candidateIDs.filter((id) => id.toLowerCase().includes(query));
  }, [candidateFilter, candidateIDs]);
  const providerProtocolOptions = useMemo(() => {
    const presetProtocols = preset ? providerPresetProtocolsForGroup(preset, providerGroup) : [];
    return presetProtocols.length > 0 ? presetProtocols : PROVIDER_PROTOCOL_OPTIONS;
  }, [preset, providerGroup]);
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
    setCandidatePopoverOpen(false);
    setCandidateIDs([]);
    setSelectedCandidateIDs([]);
    setCandidateFilter("");
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
    setCandidatePopoverOpen(true);
    setCandidatesLoading(true);
    try {
      const values = form.getValues();
      const response = values.apiKey?.trim()
        ? await probeProviderModels(token, {
            protocol: values.protocol,
            baseURL: values.baseURL,
            apiKey: values.apiKey.trim(),
        })
        : await listProviderModels(token, editingID);
      const { models } = response;
      const existing = new Set(form.getValues("models").map((model) => model.id.trim()).filter(Boolean));
      const nextCandidates: string[] = [];
      const seen = new Set<string>();
      for (const rawID of models) {
        const id = rawID.trim();
        if (id && !existing.has(id) && !seen.has(id)) {
          nextCandidates.push(id);
          seen.add(id);
        }
      }
      setCandidateIDs(nextCandidates);
      setSelectedCandidateIDs([]);
      setCandidateFilter("");
    } catch {
      form.setError("root", { message: t("provider.candidatesFailed") });
    } finally {
      setCandidatesLoading(false);
    }
  }

  function toggleCandidate(id: string, checked: boolean | "indeterminate") {
    setSelectedCandidateIDs((current) => {
      if (checked === true) {
        return current.includes(id) ? current : [...current, id];
      }
      return current.filter((item) => item !== id);
    });
  }

  function addSelectedCandidates() {
    if (selectedCandidateIDs.length === 0) {
      return;
    }
    const values = form.getValues();
    const variant = providerPresetVariantForSelection(preset, values.group, values.protocol);
    const existing = new Set(values.models.map((model) => model.id.trim()).filter(Boolean));
    const selected = selectedCandidateIDs.filter((id) => !existing.has(id));
    if (selected.length === 0) {
      setSelectedCandidateIDs([]);
      return;
    }
    fields.append(selected.map((id) => modelToForm(mergeProviderModelCandidate(id, variant, values.protocol), values.protocol, values.brand)));
    setCandidateIDs((current) => current.filter((id) => !selected.includes(id)));
    setSelectedCandidateIDs([]);
    setCandidatePopoverOpen(false);
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

  function applyPresetVariant(variant: ProviderPresetVariant) {
    form.setValue("group", variant.group || "", { shouldDirty: true });
    form.setValue("protocol", variant.protocol, { shouldDirty: true });
    form.setValue("baseURL", baseURLForVariantSwitch(form.getValues("baseURL"), variant), { shouldDirty: true });
    // 动态模型只表示"预设不内置模型清单";切换接入方式时不能清空
    // 用户已经配置的模型,也不在这里从端点隐式恢复。
    if (!variant.dynamicModels && variant.models.length > 0) {
      fields.replace(variant.models.map((model) => modelToForm(model, variant.protocol, form.getValues("brand"))));
    }
    form.clearErrors("root");
  }

  function handlePresetGroupChange(group: string) {
    if (!preset) {
      form.setValue("group", group, { shouldDirty: true });
      return;
    }
    const nextVariant =
      providerPresetVariantForSelection(preset, group, providerProtocol) ||
      preset.variants.find((variant) => providerPresetVariantGroup(variant) === group);
    if (nextVariant) {
      applyPresetVariant(nextVariant);
      return;
    }
    form.setValue("group", group, { shouldDirty: true });
  }

  function handleProtocolChange(protocol: ProviderPresetProtocol) {
    const nextVariant = providerPresetVariantForSelection(preset, providerGroup, protocol);
    if (nextVariant) {
      applyPresetVariant(nextVariant);
      return;
    }
    form.setValue("protocol", protocol, { shouldDirty: true });
  }

  const saving = createMutation.isPending || patchMutation.isPending;

  return (
    <Dialog open={open} onOpenChange={saving ? undefined : onOpenChange}>
      <DialogContent className="top-[calc(var(--toolbar-h)+(100svh-var(--toolbar-h))/2)] grid h-[min(900px,calc(100svh-var(--toolbar-h)-1.5rem))] w-[min(680px,calc(100vw-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-none">
        <DialogHeader className="px-5 py-4 pr-14">
          <DialogTitle>{editingID ? t("provider.edit") : t("provider.create")}</DialogTitle>
          <DialogDescription>{t("provider.keyHint")}</DialogDescription>
        </DialogHeader>
        <form className="contents" onSubmit={form.handleSubmit(submitProvider)}>
          <input type="hidden" {...form.register("brand")} />
          <input type="hidden" {...form.register("group")} />
          <input type="hidden" {...form.register("id")} />
          <div className="min-h-0 overflow-y-auto overscroll-none border-y px-5 py-4 [mask-image:linear-gradient(to_bottom,transparent_0,black_16px,black_calc(100%-16px),transparent_100%)]">
            <div className="grid gap-4">
              {form.formState.errors.root?.message ? (
                <Alert variant="destructive">
                  <AlertDescription>{form.formState.errors.root.message}</AlertDescription>
                </Alert>
              ) : null}

              <div className="grid gap-3 sm:grid-cols-2">
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

              {preset && presetGroups.length > 1 ? (
                <PresetGroupSwitch
                  group={providerGroup}
                  groups={presetGroups}
                  preset={preset}
                  t={t}
                  onGroupChange={handlePresetGroupChange}
                />
              ) : null}

              <div className="grid gap-3 sm:grid-cols-2">
                <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
                  <FieldLabel className="w-auto cursor-default text-sm font-medium">{t("provider.protocol")}</FieldLabel>
                  <Select
                    value={providerProtocol}
                    onValueChange={(value) => handleProtocolChange(value as ProviderPresetProtocol)}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {providerProtocolOptions.map((protocol) => (
                        <SelectItem key={protocol} value={protocol}>
                          {providerProtocolLabel(protocol)}
                        </SelectItem>
                      ))}
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
                    <Popover open={candidatePopoverOpen} onOpenChange={setCandidatePopoverOpen}>
                      <PopoverAnchor asChild>
                        <Button disabled={!editingID || candidatesLoading} size="sm" type="button" variant="ghost" onClick={() => void loadCandidates()}>
                          {t("provider.loadCandidates")}
                        </Button>
                      </PopoverAnchor>
                      <PopoverContent align="end" className="w-80 gap-0 overflow-hidden p-0">
                        <PopoverHeader className="border-b px-3 py-2.5">
                          <PopoverTitle>{t("provider.candidateModels")}</PopoverTitle>
                          <PopoverDescription>{t("provider.candidateModelsHint")}</PopoverDescription>
                        </PopoverHeader>
                        <div className="border-b p-2">
                          <Input
                            autoComplete="off"
                            placeholder={t("provider.filterCandidates")}
                            value={candidateFilter}
                            onChange={(event) => setCandidateFilter(event.target.value)}
                          />
                        </div>
                        <div
                          className="h-64 overflow-y-scroll overscroll-contain"
                          onWheelCapture={(event) => {
                            event.currentTarget.scrollTop += event.deltaY;
                            event.preventDefault();
                            event.stopPropagation();
                          }}
                        >
                          <div className="grid p-1">
                            {candidatesLoading ? (
                              <div className="flex h-24 items-center justify-center gap-2 text-sm text-muted-foreground">
                                <Loader2 className="size-4 animate-spin" />
                                {t("common.loading")}
                              </div>
                            ) : filteredCandidateIDs.length > 0 ? (
                              filteredCandidateIDs.map((id) => (
                                <label
                                  key={id}
                                  className="flex min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
                                >
                                  <Checkbox
                                    checked={selectedCandidateIDs.includes(id)}
                                    onCheckedChange={(checked) => toggleCandidate(id, checked)}
                                  />
                                  <span className="truncate font-mono">{id}</span>
                                </label>
                              ))
                            ) : (
                              <div className="flex h-24 items-center justify-center px-3 text-center text-sm text-muted-foreground">
                                {candidateIDs.length > 0 ? t("provider.candidatesNoMatch") : t("provider.candidatesEmpty")}
                              </div>
                            )}
                          </div>
                        </div>
                        <div className="flex items-center justify-between gap-2 border-t p-2">
                          <span className="text-xs text-muted-foreground">
                            {selectedCandidateIDs.length} / {filteredCandidateIDs.length} / {candidateIDs.length}
                          </span>
                          <Button disabled={selectedCandidateIDs.length === 0} size="sm" type="button" onClick={addSelectedCandidates}>
                            {t("provider.addSelectedModels")}
                          </Button>
                        </div>
                      </PopoverContent>
                    </Popover>
                    <Button size="sm" type="button" variant="outline" onClick={() => fields.append(emptyModel())}>
                      <Plus />
                      {t("provider.addModel")}
                    </Button>
                  </div>
                </div>
                <Accordion className="overflow-hidden rounded-lg border bg-card" type="multiple">
                  {fields.fields.map((field, index) => (
                    <ModelEditor
                      key={field.id}
                      canMoveDown={index < fields.fields.length - 1}
                      canMoveUp={index > 0}
                      canRemove={fields.fields.length > 1}
                      form={form}
                      index={index}
                      providerBrand={providerBrand}
                      providerProtocol={providerProtocol}
                      value={field.id}
                      onDuplicate={() => fields.insert(index + 1, { ...form.getValues(`models.${index}`) })}
                      onMoveDown={() => fields.move(index, index + 1)}
                      onMoveUp={() => fields.move(index, index - 1)}
                      onRemove={() => fields.remove(index)}
                    />
                  ))}
                </Accordion>
              </div>
            </div>
          </div>
          <DialogFooter className="m-0 rounded-none">
            <Button disabled={saving} type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button disabled={saving} type="submit">
              {saving ? <Loader2 className="animate-spin" /> : null}
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
  providerBrand,
  providerProtocol,
  canMoveDown,
  canMoveUp,
  canRemove,
  form,
  value,
  onDuplicate,
  onMoveDown,
  onMoveUp,
  onRemove,
}: {
  index: number;
  providerBrand?: string;
  providerProtocol: ProviderProfileEditorValue["protocol"];
  canMoveDown: boolean;
  canMoveUp: boolean;
  canRemove: boolean;
  form: ReturnType<typeof useForm<ProviderProfileEditorValue>>;
  value: string;
  onDuplicate: () => void;
  onMoveDown: () => void;
  onMoveUp: () => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const prefix = `models.${index}` as const;
  const watch = form.watch;
  const modelID = watch(`${prefix}.id`);
  const displayName = watch(`${prefix}.displayName`);
  const reasoningEffort = watch(`${prefix}.reasoningEffort`);
  const temperature = watch(`${prefix}.temperature`);
  const setBoolean = (name: "image" | "audio" | "tools", checked: boolean | "indeterminate") => {
    form.setValue(`${prefix}.${name}`, checked === true, { shouldDirty: true });
  };
  const setTemperature = (value: number) => {
    form.setValue(`${prefix}.temperature`, value.toFixed(1), { shouldDirty: true });
  };

  return (
    <AccordionItem className="not-last:border-b" value={value}>
      <div className="grid h-12 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-3">
        <AccordionTrigger className="min-w-0 items-center rounded-none border-0 p-0 hover:no-underline focus-visible:ring-0 [&_[data-slot=accordion-trigger-icon]]:hidden">
          <div className="flex min-w-0 items-center gap-3">
            <span className="w-7 shrink-0 text-sm tabular-nums text-muted-foreground">#{index}</span>
            <span className="truncate text-sm font-medium">{displayName?.trim() || modelID?.trim() || t("session.model")}</span>
            <ChevronDown className="size-4 shrink-0 text-muted-foreground transition-transform group-aria-expanded/accordion-trigger:rotate-180" />
          </div>
        </AccordionTrigger>
        <div className="flex items-center gap-1">
          <IconAction disabled={!canMoveUp} label={t("provider.moveUp")} onClick={onMoveUp}>
            <ArrowUp />
          </IconAction>
          <IconAction disabled={!canMoveDown} label={t("provider.moveDown")} onClick={onMoveDown}>
            <ArrowDown />
          </IconAction>
          <IconAction label={t("provider.duplicateModel")} onClick={onDuplicate}>
            <Copy />
          </IconAction>
          <IconAction className="text-destructive hover:text-destructive" disabled={!canRemove} label={t("common.delete")} onClick={onRemove}>
            <Trash />
          </IconAction>
        </div>
      </div>

      <AccordionContent className="pb-3">
        <div className="border-t" />
        <div className="grid gap-3 px-3 pt-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <PlainField label={t("provider.modelID")}>
              <Input {...form.register(`${prefix}.id`)} />
            </PlainField>
            <PlainField label={t("provider.modelName")}>
              <Input {...form.register(`${prefix}.displayName`)} />
            </PlainField>
          </div>

          <div className="grid gap-2">
            <div className="text-sm font-medium">{t("provider.capabilities")}</div>
            <div className="grid grid-cols-3 gap-2">
              <CheckField checked={watch(`${prefix}.image`)} label={t("provider.capability.image")} onCheckedChange={(checked) => setBoolean("image", checked)} />
              <CheckField checked={watch(`${prefix}.audio`)} label={t("provider.capability.audio")} onCheckedChange={(checked) => setBoolean("audio", checked)} />
              <CheckField checked={watch(`${prefix}.tools`)} label={t("provider.capability.tools")} onCheckedChange={(checked) => setBoolean("tools", checked)} />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <ReadableNumberField label={t("provider.contextWindow")} value={watch(`${prefix}.contextWindow`)}>
              <Input inputMode="numeric" {...form.register(`${prefix}.contextWindow`)} />
            </ReadableNumberField>
            <MaxOutputField form={form} prefix={prefix} providerProtocol={providerProtocol} />
            <TemperatureField label={t("provider.temperature")} value={temperature} onValueChange={setTemperature} />
            {providerProtocol !== "anthropic" ? (
              <ReasoningEffortField
                label={t("provider.reasoningEffort")}
                options={reasoningEffortOptions(providerProtocol, providerBrand, modelID)}
                value={reasoningEffort}
                onValueChange={(value) =>
                  form.setValue(`${prefix}.reasoningEffort`, value === "auto" ? "" : value, { shouldDirty: true })
                }
              />
            ) : null}
          </div>
          {providerProtocol !== "anthropic" && providerProtocol !== "google" ? (
            <PlainField className="sm:w-[calc(50%-0.375rem)]" label={t("provider.maxToolLoops")}>
              <Input inputMode="numeric" {...form.register(`${prefix}.maxToolLoops`)} />
            </PlainField>
          ) : null}
        </div>
      </AccordionContent>
    </AccordionItem>
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

function IconAction({
  children,
  className,
  disabled,
  label,
  onClick,
}: {
  children: ReactNode;
  className?: string;
  disabled?: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button
      aria-label={label}
      className={className}
      disabled={disabled}
      size="icon-sm"
      title={label}
      type="button"
      variant="ghost"
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onClick();
      }}
    >
      {children}
    </Button>
  );
}

function ReadableNumberField({
  children,
  className,
  label,
  value,
}: {
  children: ReactNode;
  className?: string;
  label: string;
  value?: string;
}) {
  return (
    <PlainField className={className} label={label}>
      <div className="grid grid-cols-[minmax(0,1fr)_3.75rem] items-center gap-2">
        {children}
        <span className="text-right text-sm tabular-nums text-muted-foreground">
          {formatReadableTokenCount(value)}
        </span>
      </div>
    </PlainField>
  );
}

function MaxOutputField({
  form,
  prefix,
  providerProtocol,
}: {
  form: ReturnType<typeof useForm<ProviderProfileEditorValue>>;
  prefix: `models.${number}`;
  providerProtocol: ProviderProfileEditorValue["protocol"];
}) {
  const { t } = useI18n();
  if (providerProtocol === "anthropic") {
    return (
      <ReadableNumberField label={t("provider.maxOutput")} value={form.watch(`${prefix}.anthropicMaxTokens`)}>
        <Input inputMode="numeric" placeholder="4096" {...form.register(`${prefix}.anthropicMaxTokens`)} />
      </ReadableNumberField>
    );
  }
  return (
    <ReadableNumberField label={t("provider.maxOutput")} value={form.watch(`${prefix}.maxCompletionTokens`)}>
      <Input inputMode="numeric" {...form.register(`${prefix}.maxCompletionTokens`)} />
    </ReadableNumberField>
  );
}

function formatReadableTokenCount(value?: string) {
  const count = Number(value?.replace(/,/g, "").trim());
  if (!Number.isFinite(count) || count <= 0) {
    return "";
  }
  if (count >= 1_000_000) {
    return `${(count / 1_000_000).toFixed(2)}M`;
  }
  if (count >= 100_000) {
    return `${Math.round(count / 1_000)}K`;
  }
  if (count >= 1_000) {
    return `${(count / 1_000).toFixed(1)}K`;
  }
  return `${Math.round(count)}`;
}

function TemperatureField({
  label,
  value,
  onValueChange,
}: {
  label: string;
  value?: string;
  onValueChange: (value: number) => void;
}) {
  const temperature = parseTemperature(value);
  return (
    <PlainField label={label}>
      <div className="grid h-8 grid-cols-[minmax(0,1fr)_2.25rem] items-center gap-3">
        <Slider
          aria-label={label}
          max={2}
          min={0}
          step={0.1}
          value={[temperature]}
          onValueChange={(values) => onValueChange(values[0] ?? temperature)}
        />
        <span className="text-right text-sm tabular-nums text-muted-foreground">{temperature.toFixed(1)}</span>
      </div>
    </PlainField>
  );
}

function parseTemperature(value?: string) {
  const temperature = Number(value?.trim());
  if (!Number.isFinite(temperature)) {
    return 0.7;
  }
  return Math.min(2, Math.max(0, temperature));
}

function ReasoningEffortField({
  label,
  options,
  value,
  onValueChange,
}: {
  label: string;
  options: string[];
  value?: string;
  onValueChange: (value: string) => void;
}) {
  const { t } = useI18n();
  const selectedValue = options.includes(value || "auto") ? value || "auto" : "auto";
  return (
    <PlainField label={label}>
      <Select value={selectedValue} onValueChange={onValueChange}>
        <SelectTrigger className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((item) => (
            <SelectItem key={item} value={item}>
              {t(`provider.reasoningEffort.${item}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </PlainField>
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
    <label
      className={cn(
        "flex h-9 min-w-0 items-center gap-2 rounded-md border px-3 text-sm transition-colors",
        checked ? optionSelectedClass : optionIdleClass,
      )}
    >
      <Checkbox checked={checked} onCheckedChange={onCheckedChange} />
      <span className="truncate">{label}</span>
    </label>
  );
}

function PresetGroupSwitch({
  group,
  groups,
  preset,
  t,
  onGroupChange,
}: {
  group: string;
  groups: string[];
  preset: ProviderPreset;
  t: (key: string) => string;
  onGroupChange: (group: string) => void;
}) {
  return (
    <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
      <FieldLabel className="w-auto cursor-default text-sm font-medium">{t("provider.endpointMode")}</FieldLabel>
      <RadioGroup className="grid w-full grid-cols-2 gap-2" value={group} onValueChange={onGroupChange}>
        {groups.map((item) => {
          const active = item === group;
          const id = `provider-profile-${preset.id}-group-${item}`;
          return (
            <FieldLabel key={item} htmlFor={id}>
              <Field
                className={cn("h-9 px-3 py-2", active ? optionSelectedClass : optionIdleClass)}
                orientation="horizontal"
              >
                <FieldContent>
                  <FieldTitle className="font-medium">{presetGroupLabel(preset, item, t)}</FieldTitle>
                </FieldContent>
                <RadioGroupItem id={id} value={item} />
              </Field>
            </FieldLabel>
          );
        })}
      </RadioGroup>
    </Field>
  );
}

export function emptyProviderProfileForm(id = ""): ProviderProfileEditorValue {
  return {
    id,
    displayName: "",
    brand: "",
    group: "",
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
    group: profile.group || "",
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
  const variant = providerPresetVariantForSelection(providerPresetForBrand(profile.brand), profile.group, profile.protocol);
  return {
    id: profile.id,
    displayName: profile.displayName,
    brand: profile.brand || "",
    group: profile.group || "",
    protocol: profile.protocol,
    baseURL: profile.baseURL || variant?.baseURL || "",
    apiKey: profile.apiKey || "",
    models: profile.models.map((model) => modelToForm(model, profile.protocol, profile.brand)),
  };
}

function modelToForm(model: ProviderModel, providerProtocol: ProviderProfileEditorValue["protocol"], providerBrand?: string): ModelFormValue {
  const options = model.providerOptions?.openai || model.providerOptions?.google || model.providerOptions?.anthropic || {};
  return {
    id: model.id,
    displayName: model.displayName || "",
    contextWindow: model.contextWindow ? String(model.contextWindow) : "",
    image: model.capabilities?.image === true,
    audio: model.capabilities?.audio === true,
    tools: model.capabilities?.tools !== false,
    temperature: stringifyOption(options.temperature),
    reasoningEffort: stringifyOption(reasoningEffortFromModel(providerProtocol, providerBrand, model)),
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
    group: value.group?.trim() || undefined,
    protocol: value.protocol,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim(),
    models: value.models.map((model) => cleanModel(model, value.protocol, value.brand)),
  });
}

function cleanPatchProvider(value: ProviderProfileEditorValue) {
  return patchProviderRequest.parse({
    displayName: value.displayName.trim(),
    brand: value.brand?.trim() || undefined,
    group: value.group?.trim() || undefined,
    protocol: value.protocol,
    baseURL: value.baseURL?.trim(),
    apiKey: value.apiKey?.trim() || undefined,
    models: value.models.map((model) => cleanModel(model, value.protocol, value.brand)),
  });
}

function cleanModel(value: ModelFormValue, providerProtocol: ProviderProfileEditorValue["protocol"], providerBrand?: string): ProviderModel {
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
  const reasoningEffort = normalizedReasoningEffort(value.reasoningEffort, providerProtocol, providerBrand, value.id);
  const maxOutputTokens = providerProtocol === "anthropic" ? positiveInt(value.anthropicMaxTokens) : positiveInt(value.maxCompletionTokens);
  const maxToolLoops = providerProtocol === "anthropic" || providerProtocol === "google" ? undefined : positiveInt(value.maxToolLoops);
  if (maxOutputTokens || maxToolLoops) {
    out.limits = {
      maxOutputTokens,
      maxToolLoops,
    };
  }
  if (providerProtocol === "anthropic") {
    const anthropic = compactOptions({
      temperature,
      output_config: supportsDeepSeekReasoning(providerProtocol, providerBrand, value.id)
        ? compactOptions({ effort: deepSeekReasoningAPIValue(reasoningEffort) })
        : undefined,
    });
    if (Object.keys(anthropic).length > 0) {
      out.providerOptions = { anthropic };
    }
  } else if (providerProtocol === "google") {
    const google = compactOptions({
      temperature,
      thinking: compactOptions({
        include_thoughts: true,
        level: reasoningEffort,
      }),
    });
    if (Object.keys(google).length > 0) {
      out.providerOptions = { google };
    }
  } else {
    const openai = compactOptions({
      temperature,
      reasoning_effort: reasoningEffort,
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

function googleThinkingLevel(options: Record<string, unknown> | undefined) {
  const thinking = options?.thinking;
  if (!thinking || typeof thinking !== "object" || Array.isArray(thinking)) {
    return undefined;
  }
  const level = (thinking as Record<string, unknown>).level;
  return typeof level === "string" ? level : undefined;
}

function reasoningEffortFromModel(
  providerProtocol: ProviderProfileEditorValue["protocol"],
  providerBrand: string | undefined,
  model: ProviderModel,
) {
  if (supportsDeepSeekReasoning(providerProtocol, providerBrand, model.id)) {
    if (providerProtocol === "anthropic") {
      return normalizeDeepSeekReasoningValue(deepSeekAnthropicEffort(model.providerOptions?.anthropic));
    }
    return normalizeDeepSeekReasoningValue(model.providerOptions?.openai?.reasoning_effort);
  }
  return model.providerOptions?.openai?.reasoning_effort ?? googleThinkingLevel(model.providerOptions?.google);
}

function deepSeekAnthropicEffort(options: Record<string, unknown> | undefined) {
  const outputConfig = options?.output_config;
  if (!outputConfig || typeof outputConfig !== "object" || Array.isArray(outputConfig)) {
    return undefined;
  }
  return (outputConfig as Record<string, unknown>).effort;
}

function reasoningEffortOptions(providerProtocol: ProviderProfileEditorValue["protocol"], providerBrand?: string, modelID?: string) {
  if (supportsDeepSeekReasoning(providerProtocol, providerBrand, modelID)) {
    return providerProtocol === "anthropic" ? ["auto", "high", "xhigh"] : OPENAI_REASONING_EFFORT_OPTIONS;
  }
  if (providerProtocol === "openai-compatible" || providerProtocol === "openai-responses") {
    return OPENAI_REASONING_EFFORT_OPTIONS;
  }
  return BASE_REASONING_EFFORT_OPTIONS;
}

function normalizedReasoningEffort(value: string | undefined, providerProtocol: ProviderProfileEditorValue["protocol"], providerBrand?: string, modelID?: string) {
  const effort = value?.trim();
  if (!effort || effort === "auto") {
    return undefined;
  }
  return reasoningEffortOptions(providerProtocol, providerBrand, modelID).includes(effort) ? effort : undefined;
}

function supportsDeepSeekReasoning(providerProtocol: ProviderProfileEditorValue["protocol"], providerBrand?: string, modelID?: string) {
  if (providerBrand !== "deepseek") {
    return false;
  }
  return (providerProtocol === "openai-compatible" || providerProtocol === "anthropic") && /^deepseek-v4-/i.test(modelID?.trim() || "");
}

function normalizeDeepSeekReasoningValue(value: unknown) {
  if (value === "max") {
    return "xhigh";
  }
  return typeof value === "string" ? value : undefined;
}

function deepSeekReasoningAPIValue(value: string | undefined) {
  if (value === "xhigh") {
    return "max";
  }
  if (value === "low" || value === "medium" || value === "high") {
    return "high";
  }
  return undefined;
}

function baseURLForVariantSwitch(currentBaseURL: string | undefined, variant: ProviderPresetVariant) {
  const target = variant.baseURL.trim();
  const current = (currentBaseURL || "").trim();
  if (!current || !target) {
    return target || current;
  }
  if (!variant.baseURLEditable) {
    return target;
  }
  try {
    const currentURL = new URL(current);
    const variantURL = new URL(target);
    const path = variantURL.pathname === "/" ? "" : variantURL.pathname.replace(/\/+$/, "");
    return `${currentURL.origin}${path}${variantURL.search}`;
  } catch {
    return target;
  }
}

function presetGroupLabel(preset: ProviderPreset, group: string, t: (key: string) => string) {
  const key = `providerPreset.${preset.id}.plan.${group}.label`;
  const translated = t(key);
  if (translated !== key) {
    return translated;
  }
  if (group === "default") {
    return t("provider.customHint");
  }
  return group;
}

function providerProtocolLabel(protocol: ProviderPresetProtocol) {
  switch (protocol) {
    case "openai-compatible":
      return "OpenAI";
    case "openai-responses":
      return "OpenAI Responses";
    case "google":
      return "Google";
    case "anthropic":
      return "Anthropic";
  }
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
