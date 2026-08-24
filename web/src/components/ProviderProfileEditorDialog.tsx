import {
  closestCenter,
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CircleHelp, Copy, Ellipsis, Eye, EyeOff, GripVertical, Plus, Trash, X } from "@/components/icons";
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
import { DialogSelectContent } from "@/components/DialogSelectContent";
import { Spinner } from "@/components/Spinner";
import { UnsavedChangesAlert } from "@/components/UnsavedChangesAlert";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
} from "@/components/AppMenu";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldContent, FieldLabel, FieldTitle } from "@/components/ui/field";
import {
  DropdownMenu,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverAnchor,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
} from "@/components/ui/popover";
import { NeutralRadioCard, NeutralRadioGroup } from "@/components/NeutralRadioGroup";
import { Select, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { useUnsavedChangesGuard } from "@/hooks/useUnsavedChangesGuard";
import { shouldKeepDialogOpenForSelectDismiss } from "@/lib/layerGuards";
import { cn } from "@/lib/utils";
import {
  generateProviderProfileID,
  mergeProviderModelCandidate,
  providerModelDisplayName,
  providerModelDiscoveryForVariant,
  providerProtocolDisplayName,
  providerPresetForBrand,
  providerPresetGroups,
  providerPresetProtocolsForGroup,
  providerSupportsModelDiscovery,
  providerPresetVariantForSelection,
  providerPresetVariantGroup,
  type ProviderPreset,
  type ProviderPresetProtocol,
  type ProviderPresetVariant,
} from "@/provider/presets";

const providerProtocolSchema = z.enum(["openai-compatible", "openai-responses", "google", "anthropic"]);
const PROVIDER_PROTOCOL_OPTIONS: ProviderPresetProtocol[] = ["openai-compatible", "openai-responses", "google", "anthropic"];
const BASE_REASONING_EFFORT_OPTIONS = ["auto", "low", "medium", "high"];
const STANDARD_REASONING_EFFORT_OPTIONS = [...BASE_REASONING_EFFORT_OPTIONS, "xhigh", "max"];
const MODEL_ID_REQUIRED = "provider.modelIDRequired";
const PROVIDER_NAME_REQUIRED = "provider.nameRequired";
const optionSelectedClass = "border-foreground/20 bg-accent text-foreground";
const optionIdleClass = "bg-transparent text-muted-foreground hover:bg-transparent";

const modelFormSchema = z.object({
  id: z.string(),
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
}).superRefine((value, context) => {
  if (!value.id.trim() && !isBlankModel(value)) {
    context.addIssue({ code: "custom", path: ["id"], message: MODEL_ID_REQUIRED });
  }
});

const providerFormSchema = z.object({
  id: z.string().trim().min(1),
  displayName: z.string().trim().min(1, PROVIDER_NAME_REQUIRED),
  brand: z.string().optional(),
  group: z.string().optional(),
  protocol: providerProtocolSchema,
  baseURL: z.string().optional(),
  apiKey: z.string().optional(),
  models: z.array(modelFormSchema).transform((models) => models.filter((model) => model.id.trim() !== "")),
});

export type ProviderProfileEditorValue = z.infer<typeof providerFormSchema>;
type ModelFormValue = z.infer<typeof modelFormSchema>;
type ModelDialogState =
  | { mode: "create" }
  | { mode: "edit" | "duplicate"; index: number };
type ModelCommit = {
  models: ModelFormValue[];
  protocol: ProviderProfileEditorValue["protocol"];
  onCommitted?: () => void;
};

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
  const [candidatesFailed, setCandidatesFailed] = useState(false);
  const [candidatePopoverOpen, setCandidatePopoverOpen] = useState(false);
  const [candidateIDs, setCandidateIDs] = useState<string[]>([]);
  const [selectedCandidateIDs, setSelectedCandidateIDs] = useState<string[]>([]);
  const [candidateFilter, setCandidateFilter] = useState("");
  const [apiKeyVisible, setAPIKeyVisible] = useState(false);
  const [modelDialogState, setModelDialogState] = useState<ModelDialogState | null>(null);
  const [pendingRevealModelIndex, setPendingRevealModelIndex] = useState<number | null>(null);
  const [highlightedModelID, setHighlightedModelID] = useState<string | null>(null);
  const [modelSaveError, setModelSaveError] = useState<string | null>(null);
  const [basicInfoSaving, setBasicInfoSaving] = useState(false);
  const [modelDialogSaving, setModelDialogSaving] = useState(false);
  const form = useForm<ProviderProfileEditorValue>({
    resolver: zodResolver(providerFormSchema),
    defaultValues: emptyProviderProfileForm(),
  });
  const fields = useFieldArray({ control: form.control, name: "models" });
  const providerBrand = form.watch("brand");
  const providerGroup = form.watch("group") || "default";
  const providerProtocol = form.watch("protocol");
  const profileID = form.watch("id");
  const providerName = form.watch("displayName");
  const configuredModels = form.watch("models");
  const formReady = Boolean(providerName.trim());
  const basicInfoDirty = Object.entries(form.formState.dirtyFields).some(
    ([field, dirty]) => field !== "models" && Boolean(dirty),
  );
  const existingIDs = useMemo(() => profiles.map((item) => item.id), [profiles]);
  const preset = useMemo(() => providerPresetForBrand(providerBrand), [providerBrand]);
  const activeVariant = useMemo(
    () => providerPresetVariantForSelection(preset, providerGroup, providerProtocol),
    [preset, providerGroup, providerProtocol],
  );
  const supportsModelDiscovery = providerSupportsModelDiscovery(activeVariant);
  const presetGroups = useMemo(() => (preset ? providerPresetGroups(preset) : []), [preset]);
  const filteredCandidateIDs = useMemo(() => {
    const query = candidateFilter.trim().toLowerCase();
    if (!query) {
      return candidateIDs;
    }
    return candidateIDs.filter((id) =>
      id.toLowerCase().includes(query) || providerModelDisplayName(id).toLowerCase().includes(query),
    );
  }, [candidateFilter, candidateIDs]);
  const providerProtocolOptions = useMemo(() => {
    const presetProtocols = preset ? providerPresetProtocolsForGroup(preset, providerGroup) : [];
    return presetProtocols.length > 0 ? presetProtocols : PROVIDER_PROTOCOL_OPTIONS;
  }, [preset, providerGroup]);
  const showAPIKeyToggle = Boolean(editingID);
  const modelSortSensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

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
    setModelDialogState(null);
    setPendingRevealModelIndex(null);
    setHighlightedModelID(null);
    setModelSaveError(null);
    setBasicInfoSaving(false);
    setModelDialogSaving(false);
    setCandidatesFailed(false);
    setCandidatePopoverOpen(false);
    setCandidateIDs([]);
    setSelectedCandidateIDs([]);
    setCandidateFilter("");
  }, [form, initialValue, open, profile]);

  useEffect(() => {
    if (pendingRevealModelIndex === null) {
      return;
    }
    const field = fields.fields[pendingRevealModelIndex];
    if (!field) {
      return;
    }
    setHighlightedModelID(field.id);
    setPendingRevealModelIndex(null);
    window.requestAnimationFrame(() => {
      document.getElementById(`provider-model-${field.id}`)?.scrollIntoView({
        behavior: "smooth",
        block: "nearest",
      });
    });
  }, [fields.fields, pendingRevealModelIndex]);

  useEffect(() => {
    if (!highlightedModelID) {
      return;
    }
    const timeout = window.setTimeout(() => setHighlightedModelID(null), 1600);
    return () => window.clearTimeout(timeout);
  }, [highlightedModelID]);

  const createMutation = useMutation({
    mutationFn: (value: ProviderProfileEditorValue) => createProvider(token, cleanCreateProvider(value)),
    onMutate: () => setBasicInfoSaving(true),
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      onSaved?.(saved);
      onOpenChange(false);
    },
    onError: (error) => {
      setBasicInfoSaving(false);
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
    onMutate: () => setBasicInfoSaving(true),
    onSuccess: async (saved) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      onSaved?.(saved);
      onOpenChange(false);
    },
    onError: (error) => {
      setBasicInfoSaving(false);
      form.setError("root", { message: error instanceof Error ? error.message : t("provider.saveFailed") });
    },
  });

  const modelMutation = useMutation({
    mutationFn: async (commit: ModelCommit) => {
      if (!editingID) {
        throw new Error("missing provider id");
      }
      return patchProvider(token, editingID, patchProviderRequest.parse({
        models: commit.models.map((model) => cleanModel(model, commit.protocol)),
      }));
    },
    onSuccess: async (saved, commit) => {
      fields.replace(saved.models.map((model) => modelToForm(model, saved.protocol)));
      form.clearErrors("models");
      setModelSaveError(null);
      commit.onCommitted?.();
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
    onError: (error) => {
      setModelDialogSaving(false);
      setModelSaveError(error instanceof Error ? error.message : t("provider.modelSaveFailed"));
    },
  });

  function commitModels(models: ModelFormValue[], onCommitted?: () => void) {
    setModelSaveError(null);
    if (!editingID) {
      fields.replace(models);
      form.clearErrors("models");
      onCommitted?.();
      return;
    }
    const values = form.getValues();
    modelMutation.mutate({
      models,
      protocol: values.protocol,
      onCommitted,
    });
  }

  async function loadCandidates() {
    setCandidatePopoverOpen(true);
    setModelSaveError(null);
    setCandidatesLoading(true);
    setCandidatesFailed(false);
    setCandidateIDs([]);
    setSelectedCandidateIDs([]);
    setCandidateFilter("");
    form.clearErrors("root");
    try {
      const values = form.getValues();
      const discovery = activeVariant && preset
        ? providerModelDiscoveryForVariant(preset, activeVariant, values.baseURL)
        : null;
      const response = !editingID || values.apiKey?.trim()
        ? await probeProviderModels(token, {
            protocol: discovery?.protocol || values.protocol,
            baseURL: discovery?.baseURL || values.baseURL,
            apiKey: values.apiKey?.trim() || "",
            brand: values.brand,
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
      setCandidatesFailed(true);
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

  function selectAllFilteredCandidates() {
    setSelectedCandidateIDs((current) => Array.from(new Set([...current, ...filteredCandidateIDs])));
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
    const nextModels = [
      ...values.models,
      ...selected.map((id) => modelToForm(mergeProviderModelCandidate(id, variant, values.protocol), values.protocol)),
    ];
    commitModels(nextModels, () => {
      setPendingRevealModelIndex(values.models.length);
      setCandidateIDs((current) => current.filter((id) => !selected.includes(id)));
      setSelectedCandidateIDs([]);
      setCandidatePopoverOpen(false);
    });
  }

  function handleModelDragEnd({ active, over }: DragEndEvent) {
    if (!over || active.id === over.id) {
      return;
    }
    const from = fields.fields.findIndex((field) => field.id === active.id);
    const to = fields.fields.findIndex((field) => field.id === over.id);
    if (from >= 0 && to >= 0) {
      const nextModels = [...form.getValues("models")];
      const [moved] = nextModels.splice(from, 1);
      if (moved) {
        nextModels.splice(to, 0, moved);
        commitModels(nextModels);
      }
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

  function applyPresetVariant(variant: ProviderPresetVariant) {
    form.setValue("group", variant.group || "", { shouldDirty: true });
    form.setValue("protocol", variant.protocol, { shouldDirty: true });
    form.setValue("baseURL", baseURLForVariantSwitch(form.getValues("baseURL"), variant), { shouldDirty: true });
    // 动态模型只表示"预设不内置模型清单";切换接入方式时不能清空
    // 用户已经配置的模型,也不在这里从端点隐式恢复。
    if (!editingID && !variant.dynamicModels && variant.models.length > 0) {
      fields.replace(variant.models.map((model) => modelToForm(model, variant.protocol)));
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

  const modelSaving = modelMutation.isPending;
  const saving = basicInfoSaving || modelSaving;
  const unsavedChanges = useUnsavedChangesGuard(editingID ? basicInfoDirty : form.formState.isDirty);

  function handleDialogOpenChange(nextOpen: boolean) {
    if (saving) {
      return;
    }
    if (nextOpen) {
      onOpenChange(true);
      return;
    }
    unsavedChanges.request(() => onOpenChange(false));
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleDialogOpenChange}>
        <DialogContent
          className="pudding-settings-control-surface top-[calc(var(--toolbar-h)+(100svh-var(--toolbar-h))/2)] grid h-[min(900px,calc(100svh-var(--toolbar-h)-1.5rem))] w-[min(680px,calc(100vw-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-none"
          showCloseButton={false}
          onPointerDownOutside={(event) => {
            if (shouldKeepDialogOpenForSelectDismiss(event.target)) {
              event.preventDefault();
            }
          }}
          onInteractOutside={(event) => {
            if (shouldKeepDialogOpenForSelectDismiss(event.target)) {
              event.preventDefault();
            }
          }}
        >
        <DialogClose asChild>
          <Button
            aria-label={t("common.close")}
            className="absolute top-2 right-2"
            size="icon-sm"
            type="button"
            variant="ghost"
          >
            <X />
          </Button>
        </DialogClose>
        <DialogHeader className="px-5 py-4 pr-14">
          <DialogTitle>{editingID ? t("provider.edit") : t("provider.create")}</DialogTitle>
          <DialogDescription>{t("provider.editorHint")}</DialogDescription>
        </DialogHeader>
        <form id="provider-profile-form" className="contents" onSubmit={form.handleSubmit(submitProvider)}>
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
                  {form.formState.errors.displayName?.message ? (
                    <div className="text-xs text-destructive">{t(form.formState.errors.displayName.message)}</div>
                  ) : null}
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
                    <DialogSelectContent>
                      {providerProtocolOptions.map((protocol) => (
                        <SelectItem key={protocol} value={protocol}>
                          {providerProtocolDisplayName(protocol)}
                        </SelectItem>
                      ))}
                    </DialogSelectContent>
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
                      className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
                      type="button"
                      onClick={() => setAPIKeyVisible((visible) => !visible)}
                    >
                      {apiKeyVisible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                    </button>
                  ) : null}
                </div>
              </Field>

              <div className="flex justify-end">
                <Button
                  disabled={saving || !formReady || Boolean(editingID && !basicInfoDirty)}
                  size="sm"
                  type="submit"
                >
                  {basicInfoSaving ? <Spinner /> : null}
                  {t(editingID ? "provider.saveBasicInfo" : "provider.createAction")}
                </Button>
              </div>

              <div className="grid gap-2 border-t pt-4">
                <div className="flex items-center justify-between gap-2">
                  <div className="grid gap-0.5">
                    <Label>{t("provider.models")}</Label>
                    <span className="text-xs text-muted-foreground">
                      {t(editingID ? "provider.modelsImmediateHint" : "provider.modelsHint")}
                    </span>
                  </div>
                  <div className="flex gap-2">
                    {supportsModelDiscovery ? <Popover modal open={candidatePopoverOpen} onOpenChange={setCandidatePopoverOpen}>
                      <PopoverAnchor asChild>
                        <Button disabled={candidatesLoading || modelSaving} size="sm" type="button" variant="ghost" onClick={() => void loadCandidates()}>
                          {candidatesLoading ? <Spinner className="size-4" /> : null}
                          {editingID ? t("provider.loadCandidates") : t("provider.testAndLoadModels")}
                        </Button>
                      </PopoverAnchor>
                      <PopoverContent align="end" className="pudding-settings-control-surface w-80 gap-0 overflow-hidden p-0">
                        <PopoverHeader className="border-b px-3 py-2.5">
                          <PopoverTitle>{t("provider.candidateModels")}</PopoverTitle>
                          <PopoverDescription>{t("provider.candidateModelsHint")}</PopoverDescription>
                        </PopoverHeader>
                        {candidateIDs.length > 8 ? (
                          <div className="border-b p-2">
                            <Input
                              autoComplete="off"
                              placeholder={t("provider.filterCandidates")}
                              value={candidateFilter}
                              onChange={(event) => setCandidateFilter(event.target.value)}
                            />
                          </div>
                        ) : null}
                        <div
                          className="h-64 overflow-y-scroll overscroll-contain"
                        >
                          <div className="grid p-1">
                            {candidatesLoading ? (
                              <div className="flex h-24 items-center justify-center gap-2 text-sm text-muted-foreground">
                                <Spinner className="size-4" />
                                {t("common.loading")}
                              </div>
                            ) : candidatesFailed ? (
                              <div className="flex h-40 flex-col items-center justify-center gap-3 px-4 text-center text-sm text-muted-foreground">
                                <span>{t("provider.candidatesFailed")}</span>
                                <Button size="sm" type="button" variant="outline" onClick={() => void loadCandidates()}>
                                  {t("provider.retryCandidates")}
                                </Button>
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
                                  <span className="grid min-w-0 gap-0.5">
                                    <span className="truncate">{providerModelDisplayName(id)}</span>
                                    <span className="truncate font-mono text-xs text-muted-foreground">{id}</span>
                                  </span>
                                </label>
                              ))
                            ) : (
                              <div className="flex h-24 items-center justify-center px-3 text-center text-sm text-muted-foreground">
                                {candidateIDs.length > 0 ? t("provider.candidatesNoMatch") : t("provider.candidatesEmpty")}
                              </div>
                            )}
                          </div>
                        </div>
                        {candidateIDs.length > 0 && !candidatesLoading && !candidatesFailed ? (
                          <div className="grid gap-2 border-t p-2">
                            <div className="flex items-center justify-between gap-2">
                              <span className="text-xs text-muted-foreground">
                                {t("provider.selectedAndTotalCount")
                                  .replace("{selected}", String(selectedCandidateIDs.length))
                                  .replace("{total}", String(candidateIDs.length))}
                              </span>
                              <div className="flex items-center gap-1">
                                <Button
                                  disabled={filteredCandidateIDs.length === 0}
                                  size="xs"
                                  type="button"
                                  variant="ghost"
                                  onClick={selectAllFilteredCandidates}
                                >
                                  {t("common.selectAll")}
                                </Button>
                                <Button
                                  disabled={selectedCandidateIDs.length === 0}
                                  size="xs"
                                  type="button"
                                  variant="ghost"
                                  onClick={() => setSelectedCandidateIDs([])}
                                >
                                  {t("common.clear")}
                                </Button>
                              </div>
                            </div>
                            <Button className="w-full" disabled={selectedCandidateIDs.length === 0 || modelSaving} size="sm" type="button" onClick={addSelectedCandidates}>
                              {modelSaving ? <Spinner /> : null}
                              {t("provider.addSelectedModelsCount").replace("{count}", String(selectedCandidateIDs.length))}
                            </Button>
                          </div>
                        ) : null}
                      </PopoverContent>
                    </Popover> : null}
                    <Button
                      disabled={modelSaving}
                      size="sm"
                      type="button"
                      variant="outline"
                      onClick={() => {
                        setModelSaveError(null);
                        setModelDialogSaving(false);
                        setModelDialogState({ mode: "create" });
                      }}
                    >
                      <Plus />
                      {t("provider.addModel")}
                    </Button>
                  </div>
                </div>
                {fields.fields.length > 0 ? (
                  <DndContext
                    collisionDetection={closestCenter}
                    sensors={modelSortSensors}
                    onDragEnd={handleModelDragEnd}
                  >
                    <div className="overflow-hidden rounded-lg border bg-card">
                      <SortableContext
                        items={fields.fields.map((field) => field.id)}
                        strategy={verticalListSortingStrategy}
                      >
                        {fields.fields.map((field, index) => (
                          <SortableModelRow
                            key={field.id}
                            disabled={modelSaving}
                            highlighted={highlightedModelID === field.id}
                            model={configuredModels[index] || emptyModel()}
                            sortableDisabled={fields.fields.length < 2 || modelSaving}
                            sortableID={field.id}
                            onDuplicate={() => {
                              setModelSaveError(null);
                              setModelDialogSaving(false);
                              setModelDialogState({ mode: "duplicate", index });
                            }}
                            onEdit={() => {
                              setModelSaveError(null);
                              setModelDialogSaving(false);
                              setModelDialogState({ mode: "edit", index });
                            }}
                            onRemove={() => {
                              const nextModels = form.getValues("models").filter((_, modelIndex) => modelIndex !== index);
                              commitModels(nextModels);
                            }}
                          />
                        ))}
                      </SortableContext>
                    </div>
                  </DndContext>
                ) : (
                  <div className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
                    {t("picker.noModels")}
                  </div>
                )}
                {form.formState.errors.models?.message ? (
                  <div className="text-xs text-destructive">{t(form.formState.errors.models.message)}</div>
                ) : null}
                {modelSaveError && !modelDialogState ? (
                  <Alert variant="destructive">
                    <AlertDescription>{modelSaveError}</AlertDescription>
                  </Alert>
                ) : null}
              </div>
            </div>
          </div>
        </form>
        </DialogContent>
      </Dialog>
      {modelDialogState ? (
        <ModelDetailsDialog
          existingModelIDs={configuredModels.flatMap((model, index) =>
            modelDialogState.mode === "edit" && modelDialogState.index === index ? [] : [model.id],
          )}
          initialValue={modelDialogState.mode === "create" ? null : configuredModels[modelDialogState.index]}
          mode={modelDialogState.mode}
          errorMessage={modelSaveError}
          providerBrand={providerBrand}
          providerProtocol={providerProtocol}
          saving={modelSaving || modelDialogSaving}
          onClose={() => {
            if (!modelSaving && !modelDialogSaving) {
              setModelDialogState(null);
              setModelSaveError(null);
            }
          }}
          onSave={(model) => {
            setModelDialogSaving(true);
            const nextModels = [...form.getValues("models")];
            let nextIndex: number;
            if (modelDialogState.mode === "edit") {
              nextIndex = modelDialogState.index;
              nextModels[nextIndex] = model;
            } else if (modelDialogState.mode === "duplicate") {
              nextIndex = modelDialogState.index + 1;
              nextModels.splice(nextIndex, 0, model);
            } else {
              nextIndex = nextModels.length;
              nextModels.push(model);
            }
            commitModels(nextModels, () => {
              setPendingRevealModelIndex(nextIndex);
              setModelDialogState(null);
            });
          }}
        />
      ) : null}
      <UnsavedChangesAlert
        open={unsavedChanges.confirmationOpen}
        onDiscard={unsavedChanges.discard}
        onOpenChange={unsavedChanges.setConfirmationOpen}
      />
    </>
  );
}

function SortableModelRow({
  disabled,
  highlighted,
  model,
  sortableDisabled,
  sortableID,
  onDuplicate,
  onEdit,
  onRemove,
}: {
  disabled: boolean;
  highlighted: boolean;
  model: ModelFormValue;
  sortableDisabled: boolean;
  sortableID: string;
  onDuplicate: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const displayName = model.displayName?.trim() || providerModelDisplayName(model.id) || t("provider.unnamedModel");
  const { attributes, isDragging, listeners, setNodeRef, transform, transition } = useSortable({
    id: sortableID,
    disabled: sortableDisabled,
  });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "relative grid h-12 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-1 px-2 not-last:border-b",
        highlighted && "bg-primary/5 ring-1 ring-inset ring-primary/40",
        isDragging && "z-10 bg-card opacity-80 shadow-md",
      )}
      id={`provider-model-${sortableID}`}
      style={{ transform: CSS.Transform.toString(transform), transition }}
    >
      <Button
        {...attributes}
        {...listeners}
        aria-label={t("provider.reorderModel")}
        className="touch-none cursor-grab text-muted-foreground active:cursor-grabbing"
        disabled={sortableDisabled}
        size="icon-sm"
        type="button"
        variant="ghost"
      >
        <GripVertical />
      </Button>
      <button
        aria-label={t("provider.editModel")}
        className="h-12 min-w-0 truncate text-left text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/50"
        disabled={disabled}
        type="button"
        onClick={onEdit}
      >
        {displayName}
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button aria-label={t("provider.modelActions")} disabled={disabled} size="icon-sm" type="button" variant="ghost">
            <Ellipsis />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuItem onSelect={onDuplicate}>
            <Copy />
            {t("provider.duplicateModel")}
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onSelect={onRemove}>
            <Trash />
            {t("common.delete")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function ModelDetailsDialog({
  existingModelIDs,
  errorMessage,
  initialValue,
  mode,
  providerBrand,
  providerProtocol,
  saving,
  onClose,
  onSave,
}: {
  existingModelIDs: string[];
  errorMessage?: string | null;
  initialValue?: ModelFormValue | null;
  mode: "create" | "edit" | "duplicate";
  providerBrand?: string;
  providerProtocol: ProviderProfileEditorValue["protocol"];
  saving: boolean;
  onClose: () => void;
  onSave: (model: ModelFormValue) => void;
}) {
  const { t } = useI18n();
  const modelForm = useForm<ModelFormValue>({ defaultValues: initialValue || emptyModel() });
  const modelID = modelForm.watch("id");
  const temperature = modelForm.watch("temperature");
  const reasoningEffort = modelForm.watch("reasoningEffort");
  const titleKey = mode === "edit"
    ? "provider.editModel"
    : mode === "duplicate"
      ? "provider.duplicateModel"
      : "provider.addModel";
  const descriptionKey = mode === "edit"
    ? "provider.editModelHint"
    : mode === "duplicate"
      ? "provider.duplicateModelHint"
      : "provider.addModelHint";

  function submitModel(value: ModelFormValue) {
    if (mode === "edit" && !modelForm.formState.isDirty) {
      return;
    }
    const id = value.id.trim();
    if (!id) {
      modelForm.setError("id", { message: MODEL_ID_REQUIRED });
      return;
    }
    if (existingModelIDs.some((item) => item.trim() === id)) {
      modelForm.setError("id", { message: "provider.modelExists" });
      return;
    }
    onSave({
      ...value,
      id,
      displayName: value.displayName?.trim() || providerModelDisplayName(id),
    });
  }

  const setCapability = (name: "image" | "audio" | "tools", checked: boolean | "indeterminate") => {
    modelForm.setValue(name, checked === true, { shouldDirty: true });
  };

  return (
    <Dialog open onOpenChange={(open) => {
      if (!open && !saving) {
        onClose();
      }
    }}>
      <DialogContent
        className="pudding-settings-control-surface grid max-h-[min(760px,calc(100svh-var(--toolbar-h)-2rem))] w-[min(560px,calc(100vw-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-none"
        showCloseButton={false}
      >
        <DialogClose asChild>
          <Button
            aria-label={t("common.close")}
            className="absolute top-2 right-2"
            disabled={saving}
            size="icon-sm"
            type="button"
            variant="ghost"
          >
            <X />
          </Button>
        </DialogClose>
        <DialogHeader className="px-5 py-4 pr-14">
          <DialogTitle>{t(titleKey)}</DialogTitle>
          <DialogDescription>{t(descriptionKey)}</DialogDescription>
        </DialogHeader>
        <form className="contents" onSubmit={modelForm.handleSubmit(submitModel)}>
          <div className="grid min-h-0 gap-4 overflow-y-auto border-y px-5 py-4">
            {errorMessage ? (
              <Alert variant="destructive">
                <AlertDescription>{errorMessage}</AlertDescription>
              </Alert>
            ) : null}
            <div className="grid gap-3 sm:grid-cols-2">
              <PlainField label={t("provider.modelID")}>
                <Input
                  autoFocus
                  {...modelForm.register("id")}
                  onFocus={(event) => {
                    if (mode === "duplicate") {
                      event.currentTarget.select();
                    }
                  }}
                />
                {modelForm.formState.errors.id?.message ? (
                  <div className="text-xs text-destructive">{t(modelForm.formState.errors.id.message)}</div>
                ) : null}
              </PlainField>
              <PlainField hint={t("provider.modelNameAutoHint")} label={t("provider.modelName")}>
                <Input
                  placeholder={modelID.trim() ? providerModelDisplayName(modelID) : undefined}
                  {...modelForm.register("displayName")}
                />
              </PlainField>
            </div>

            <div className="grid gap-2">
              <div className="text-sm font-medium">{t("provider.capabilities")}</div>
              <div className="grid grid-cols-3 gap-2">
                <CheckField checked={modelForm.watch("image")} label={t("provider.capability.image")} onCheckedChange={(checked) => setCapability("image", checked)} />
                <CheckField checked={modelForm.watch("audio")} label={t("provider.capability.audio")} onCheckedChange={(checked) => setCapability("audio", checked)} />
                <CheckField checked={modelForm.watch("tools")} label={t("provider.capability.tools")} onCheckedChange={(checked) => setCapability("tools", checked)} />
              </div>
            </div>

            <div className="grid gap-3 border-t pt-4 sm:grid-cols-2">
              <ReadableNumberField label={t("provider.contextWindow")} value={modelForm.watch("contextWindow")}>
                <Input inputMode="numeric" {...modelForm.register("contextWindow")} />
              </ReadableNumberField>
              <ReadableNumberField
                label={t("provider.maxOutput")}
                value={modelForm.watch(providerProtocol === "anthropic" ? "anthropicMaxTokens" : "maxCompletionTokens")}
              >
                <Input
                  inputMode="numeric"
                  {...modelForm.register(providerProtocol === "anthropic" ? "anthropicMaxTokens" : "maxCompletionTokens")}
                />
              </ReadableNumberField>
              <TemperatureField
                label={t("provider.temperature")}
                value={temperature}
                onValueChange={(value) => modelForm.setValue("temperature", value.toFixed(1), { shouldDirty: true })}
              />
              <ReasoningEffortField
                label={t("provider.reasoningEffort")}
                options={reasoningEffortOptions(providerProtocol)}
                value={reasoningEffort}
                onValueChange={(value) =>
                  modelForm.setValue("reasoningEffort", value === "auto" ? "" : value, { shouldDirty: true })
                }
              />
            </div>
            {providerProtocol !== "anthropic" && providerProtocol !== "google" ? (
              <PlainField className="sm:w-[calc(50%-0.375rem)]" label={t("provider.maxToolLoops")}>
                <Input inputMode="numeric" {...modelForm.register("maxToolLoops")} />
              </PlainField>
            ) : null}
          </div>
          <DialogFooter className="m-0 rounded-none">
            <Button disabled={saving} type="button" variant="outline" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button disabled={saving || (mode === "edit" && !modelForm.formState.isDirty)} type="submit">
              {saving ? <Spinner /> : mode !== "edit" ? <Plus /> : null}
              {t(mode === "edit" ? "common.save" : mode === "duplicate" ? "provider.createModelCopy" : "provider.addModel")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function PlainField({
  children,
  className,
  hint,
  label,
}: {
  children: ReactNode;
  className?: string;
  hint?: string;
  label: string;
}) {
  return (
    <Field className={cn("gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent", className)}>
      <FieldLabel className="w-auto cursor-default gap-1.5 text-sm font-medium">
        <span>{label}</span>
        {hint ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span
                aria-label={hint}
                className="inline-flex size-4 cursor-help items-center justify-center text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
                role="button"
                tabIndex={0}
              >
                <CircleHelp className="size-3.5" />
              </span>
            </TooltipTrigger>
            <TooltipContent>{hint}</TooltipContent>
          </Tooltip>
        ) : null}
      </FieldLabel>
      {children}
    </Field>
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
        <DialogSelectContent>
          {options.map((item) => (
            <SelectItem key={item} value={item}>
              {t(`provider.reasoningEffort.${item}`)}
            </SelectItem>
          ))}
        </DialogSelectContent>
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
        "flex h-9 min-w-0 items-center gap-2 rounded-md border px-3 text-sm",
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
      <NeutralRadioGroup className="grid w-full grid-cols-2 gap-2" value={group} onValueChange={onGroupChange}>
        {groups.map((item) => {
          const active = item === group;
          const id = `provider-profile-${preset.id}-group-${item}`;
          return (
            <NeutralRadioCard
              key={item}
              id={id}
              selected={active}
              title={presetGroupLabel(preset, item, t)}
              value={item}
            />
          );
        })}
      </NeutralRadioGroup>
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
    models: [],
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

function isBlankModel(value: ModelFormValue) {
  return (
    !value.id.trim() &&
    !value.displayName?.trim() &&
    !value.contextWindow?.trim() &&
    !value.temperature?.trim() &&
    !value.reasoningEffort?.trim() &&
    !value.maxCompletionTokens?.trim() &&
    !value.maxToolLoops?.trim() &&
    !value.anthropicMaxTokens?.trim() &&
    !value.image &&
    !value.audio &&
    value.tools
  );
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
    models: profile.models.map((model) => modelToForm(model, profile.protocol)),
  };
}

function modelToForm(model: ProviderModel, providerProtocol: ProviderProfileEditorValue["protocol"]): ModelFormValue {
  const options = model.providerOptions?.openai || model.providerOptions?.google || model.providerOptions?.anthropic || {};
  return {
    id: model.id,
    displayName: model.displayName?.trim() || providerModelDisplayName(model.id),
    contextWindow: model.contextWindow ? String(model.contextWindow) : "",
    image: model.capabilities?.image === true,
    audio: model.capabilities?.audio === true,
    tools: model.capabilities?.tools !== false,
    temperature: stringifyOption(options.temperature),
    reasoningEffort: stringifyOption(reasoningEffortFromModel(providerProtocol, model)),
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
    models: cleanModels(value),
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
  });
}

function cleanModels(value: ProviderProfileEditorValue) {
  return value.models
    .filter((model) => model.id.trim() !== "")
    .map((model) => cleanModel(model, value.protocol));
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
  const reasoningEffort = normalizedReasoningEffort(value.reasoningEffort, providerProtocol);
  const maxOutputTokens = providerProtocol === "anthropic" ? positiveInt(value.anthropicMaxTokens) : positiveInt(value.maxCompletionTokens);
  const maxToolLoops = providerProtocol === "anthropic" || providerProtocol === "google" ? undefined : positiveInt(value.maxToolLoops);
  if (maxOutputTokens || maxToolLoops) {
    out.limits = {
      maxOutputTokens,
      maxToolLoops,
    };
  }
  if (providerProtocol === "anthropic") {
    const outputConfig = compactOptions({ effort: reasoningEffort });
    const anthropic = compactOptions({
      temperature,
      output_config: Object.keys(outputConfig).length > 0 ? outputConfig : undefined,
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
  model: ProviderModel,
) {
  if (providerProtocol === "anthropic") {
    return anthropicEffort(model.providerOptions?.anthropic);
  }
  return model.providerOptions?.openai?.reasoning_effort ?? googleThinkingLevel(model.providerOptions?.google);
}

function anthropicEffort(options: Record<string, unknown> | undefined) {
  const outputConfig = options?.output_config;
  if (!outputConfig || typeof outputConfig !== "object" || Array.isArray(outputConfig)) {
    return undefined;
  }
  const effort = (outputConfig as Record<string, unknown>).effort;
  return typeof effort === "string" ? effort : undefined;
}

function reasoningEffortOptions(providerProtocol: ProviderProfileEditorValue["protocol"]) {
  if (providerProtocol === "openai-compatible" || providerProtocol === "openai-responses" || providerProtocol === "anthropic") {
    return STANDARD_REASONING_EFFORT_OPTIONS;
  }
  return BASE_REASONING_EFFORT_OPTIONS;
}

function normalizedReasoningEffort(value: string | undefined, providerProtocol: ProviderProfileEditorValue["protocol"]) {
  const effort = value?.trim();
  if (!effort || effort === "auto") {
    return undefined;
  }
  return reasoningEffortOptions(providerProtocol).includes(effort) ? effort : undefined;
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
