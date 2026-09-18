import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronLeft, ChevronRight, RefreshCw, RotateCcw } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  listProviders,
  syncProviderModels,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import {
  AppPopoverContent as PopoverContent,
  appPopoverItemStateClassName,
  appPopoverSelectedItemStateClassName,
} from "@/components/AppPopover";
import { modelSelectionKey, resolveModelSelection, type ResolvedModelSelection } from "@/lib/modelSelection";
import {
  reasoningEffortOptionsForSelection,
  recommendedReasoningEffortForSelection,
  resolveReasoningEffortForSelection,
} from "@/lib/reasoningEffort";
import { getEffortColor, SteppedSlider } from "@/components/SteppedSlider";
import { Button } from "@/components/ui/button";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { useSessionModelSettings } from "@/hooks/useSessionModelSettings";
import { formatModelLabel } from "@/lib/model";
import { cn } from "@/lib/utils";
import { providerBrandForModel } from "@/provider/presets";
import { useReasoningEffortPreferenceStore } from "@/state/reasoningEffortPreferenceStore";

type ModelReasoningPickerProps = {
  token: string;
  session?: Session;
  value?: { provider?: string; model?: string };
  reasoningValue: string;
  onChange?: (value: { provider: string; model: string }) => void;
  onAfterClose?: () => void;
  onReasoningChange: (selection: ResolvedModelSelection, value: string) => void;
  iconOnly?: boolean;
  className?: string;
};

export function ModelReasoningPicker({
  token,
  session,
  value,
  reasoningValue,
  onAfterClose,
  onChange,
  onReasoningChange,
  iconOnly = false,
  className,
}: ModelReasoningPickerProps) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const interactedOutsideRef = useRef(false);
  const triggerButtonRef = useRef<HTMLButtonElement>(null);
  const [displayAsSliderView, setDisplayAsSliderView] = useState(false);
  const [preservedWidth, setPreservedWidth] = useState<number | null>(null);
  const sliderViewTimerRef = useRef<number | null>(null);
  const [syncingProfileID, setSyncingProfileID] = useState<string | null>(null);
  const lastSyncedRef = useRef<Record<string, number>>({});
  const { update: updateModelSettings, pending: modelSettingsPending } = useSessionModelSettings(token, session?.id);

  const clearSliderViewTimer = useCallback(() => {
    if (sliderViewTimerRef.current !== null) {
      window.clearTimeout(sliderViewTimerRef.current);
      sliderViewTimerRef.current = null;
    }
  }, []);

  useEffect(() => clearSliderViewTimer, [clearSliderViewTimer]);

  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const profiles = useMemo(() => providersQuery.data?.providers ?? [], [providersQuery.data?.providers]);
  const resolveSelection = useCallback(
    (provider: string, model: string) => resolveModelSelection(profiles, { provider, model }),
    [profiles],
  );

  const selectableProfiles = useMemo(
    () => profiles.filter((profile) => profile.models.some((model) => model.id)),
    [profiles],
  );
  const selectedProvider = session?.provider || value?.provider || "";
  const selectedModel = session?.model || value?.model || "";
  const activeProfile = profiles.find((p) => p.id === selectedProvider);
  const currentModelAvailable = Boolean(
    selectedProvider &&
      selectedModel &&
      activeProfile?.models.some((model) => model.id === selectedModel),
  );
  const visibleModel = currentModelAvailable ? selectedModel : "";
  const resolvedSelection = useMemo(
    () => (currentModelAvailable ? resolveSelection(selectedProvider, selectedModel) : null),
    [currentModelAvailable, resolveSelection, selectedModel, selectedProvider],
  );
  const reasoningOptions = useMemo(() => reasoningEffortOptionsForSelection(resolvedSelection), [resolvedSelection]);
  const recommendedReasoning = useMemo(
    () => recommendedReasoningEffortForSelection(resolvedSelection),
    [resolvedSelection],
  );
  const effectiveReasoning = resolveReasoningEffortForSelection(resolvedSelection, reasoningValue);
  const [selectedReasoning, setSelectedReasoning] = useState<string | null>(null);

  const [view, setView] = useState<"slider" | "catalog">("slider");
  const [viewedProfileID, setViewedProfileID] = useState(selectedProvider);

  useEffect(() => {
    if (!modelSettingsPending) {
      setSelectedReasoning(null);
    }
  }, [reasoningValue, visibleModel, open, modelSettingsPending]);

  useEffect(() => {
    if (open) {
      if (reasoningOptions.length > 0) {
        setView("slider");
      } else {
        setView("catalog");
      }
      const initialProfile = selectableProfiles.some((profile) => profile.id === selectedProvider)
        ? selectedProvider
        : selectableProfiles[0]?.id || "";
      setViewedProfileID(initialProfile);
    }
  }, [open, reasoningOptions.length, selectedProvider, selectableProfiles]);

  const viewedProfile = selectableProfiles.find((profile) => profile.id === viewedProfileID);
  const longestModelList = selectableProfiles.reduce(
    (longest, profile) =>
      Math.max(longest, profile.models.filter((model) => model.id).length),
    0,
  );
  const profilePaneHeight = Math.min(
    Math.max(Math.max(selectableProfiles.length, longestModelList) * 34 + 12, 160),
    360,
  );

  const isSliderViewActive = open && view === "slider" && reasoningOptions.length > 0;

  useEffect(() => {
    if (isSliderViewActive) {
      clearSliderViewTimer();
      if (triggerButtonRef.current && preservedWidth === null) {
        setPreservedWidth(triggerButtonRef.current.offsetWidth);
      }
      setDisplayAsSliderView(true);
    } else if (displayAsSliderView) {
      if (!open) {
        sliderViewTimerRef.current = window.setTimeout(() => {
          setDisplayAsSliderView(false);
          setPreservedWidth(null);
          sliderViewTimerRef.current = null;
        }, 150);
      } else {
        clearSliderViewTimer();
        setDisplayAsSliderView(false);
        setPreservedWidth(null);
      }
    }
  }, [isSliderViewActive, open, displayAsSliderView, preservedWidth, clearSliderViewTimer]);

  const activeReasoning = selectedReasoning || effectiveReasoning;
  const activeBrand = visibleModel
    ? providerBrandForModel(visibleModel) || providerBrandKey(activeProfile) || selectedProvider
    : "";
  const label = visibleModel
    ? formatModelLabel(visibleModel, resolvedSelection?.modelConfig?.displayName)
    : t("picker.selectModel");
  const reasoningLabel = reasoningOptions.length > 0 ? t(`provider.reasoningEffort.${activeReasoning}`) : "";
  const triggerLabel = reasoningLabel ? `${label} · ${reasoningLabel}` : label;
  const isSliderMode = displayAsSliderView;
  const triggerText = isSliderMode ? t("provider.reasoningEffort.selectEffort") : triggerLabel;

  const handleReasoningSelect = useCallback(
    (next: string) => {
      if (!resolvedSelection) {
        return;
      }
      setSelectedReasoning(next);
      onReasoningChange(resolvedSelection, next);
    },
    [onReasoningChange, resolvedSelection],
  );

  const handleModelPick = useCallback(
    (profile: ProviderProfile, model: string) => {
      setSelectedReasoning(null);
      const nextSelection = resolveSelection(profile.id, model);
      if (!nextSelection) {
        return;
      }
      const nextModelKey = modelSelectionKey(nextSelection);
      const preferredReasoning = resolvedSelection && modelSelectionKey(resolvedSelection) === nextModelKey
        ? reasoningValue
        : useReasoningEffortPreferenceStore.getState().byModel[nextModelKey] || "";
      const nextReasoning = resolveReasoningEffortForSelection(nextSelection, preferredReasoning);
      if (session) {
        void updateModelSettings({ provider: profile.id, model, reasoningEffort: nextReasoning }).catch((error) => {
          console.warn("failed to update model settings", error);
        });
      } else {
        onChange?.({ provider: profile.id, model });
        onReasoningChange(nextSelection, nextReasoning);
      }
      setView("slider");
    },
    [onChange, onReasoningChange, reasoningValue, resolvedSelection, resolveSelection, session, updateModelSettings],
  );

  const handleSyncProfile = useCallback(
    async (profileID: string) => {
      if (syncingProfileID) {
        return;
      }
      setSyncingProfileID(profileID);
      lastSyncedRef.current[profileID] = Date.now();
      try {
        await syncProviderModels(token, profileID);
        await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      } catch (error) {
        console.warn("failed to sync provider models", error);
      } finally {
        setSyncingProfileID(null);
      }
    },
    [queryClient, syncingProfileID, token],
  );

  useEffect(() => {
    if (!open || !token) {
      return;
    }
    const syncableProfiles = profiles.filter(isSyncableProfile);
    const now = Date.now();
    for (const p of syncableProfiles) {
      const last = lastSyncedRef.current[p.id] || 0;
      if (now - last > 60_000) {
        lastSyncedRef.current[p.id] = now;
        syncProviderModels(token, p.id)
          .then(() => {
            void queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
          })
          .catch((error) => {
            console.warn("silent sync provider models failed", error);
          });
      }
    }
  }, [open, token, profiles, queryClient]);

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        clearSliderViewTimer();
        if (next) {
          interactedOutsideRef.current = false;
          if (triggerButtonRef.current) {
            setPreservedWidth(triggerButtonRef.current.offsetWidth);
          }
        }
        setOpen(next);
      }}
    >
      <PopoverTrigger asChild>
        <Button
          ref={triggerButtonRef}
          disabled={modelSettingsPending}
          aria-busy={modelSettingsPending}
          style={!iconOnly && isSliderMode && preservedWidth ? { width: `${preservedWidth}px` } : undefined}
          aria-label={`${t("session.model")}: ${triggerText}`}
          className={cn(
            "pudding-composer-model-picker group/model-picker h-8 shrink rounded-full border-0 bg-transparent py-0 text-xs font-normal text-[var(--composer-control-foreground)] transition-none",
            iconOnly
              ? "w-8 max-w-8 flex-none justify-center p-0"
              : cn(
                  "min-w-0 gap-1 pr-1.5",
                  isSliderMode || reasoningLabel
                    ? "max-w-[14rem] sm:max-w-[16rem]"
                    : "max-w-[9.5rem] sm:max-w-[10.5rem]",
                ),
            !iconOnly && (visibleModel ? "pl-1" : "pl-2"),
            className,
          )}
          size="sm"
          variant="ghost"
        >
          {visibleModel ? (
            activeBrand && BrandIcon({ name: activeBrand })
              ? <RoundBrandIcon name={activeBrand} sizeClassName="size-5" />
              : <span className="grid size-5 shrink-0 place-items-center rounded-full bg-background/60 text-[10px] text-foreground">{(activeProfile?.displayName || selectedProvider).slice(0, 1).toUpperCase()}</span>
          ) : null}
          {iconOnly ? null : isSliderMode ? (
            <span className="flex h-5 min-w-0 flex-1 items-center gap-1 overflow-hidden">
              <span className="pudding-composer-model-label min-w-0 flex-1 truncate">
                {t("provider.reasoningEffort.selectEffort")}
              </span>
              <ChevronDown className={cn("size-3 shrink-0", visibleModel ? "text-muted-foreground" : "text-current")} />
            </span>
          ) : (
            <span className={cn("flex h-5 min-w-0 flex-1 items-center gap-1 overflow-hidden", !visibleModel && "text-warning")}>
              <span className="pudding-composer-model-label min-w-0 flex-1 truncate">{label}</span>
              {reasoningLabel ? (
                <span className="pudding-composer-reasoning-detail shrink-0 text-muted-foreground/70">
                  ·
                </span>
              ) : null}
              {reasoningLabel ? (
                <span
                  className={cn(
                    "pudding-composer-reasoning-detail shrink-0 font-medium",
                    reasoningOptions.length > 0 && reasoningOptions.indexOf(activeReasoning) >= 0
                      ? getEffortColor(reasoningOptions.indexOf(activeReasoning), reasoningOptions.length).text
                      : "text-muted-foreground/80",
                  )}
                >
                  {reasoningLabel}
                </span>
              ) : null}
              <ChevronDown className={cn("size-3 shrink-0", visibleModel ? "text-muted-foreground" : "text-current")} />
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="center"
        className="max-h-[min(28rem,var(--radix-popover-content-available-height))] w-[17.5rem] max-w-[calc(100vw-1rem)] gap-0 overflow-hidden p-0"
        collisionPadding={8}
        side="top"
        sideOffset={8}
        onPointerDownOutside={(event) => {
          if (triggerButtonRef.current?.contains(event.target as Node)) {
            event.preventDefault();
          }
        }}
        onInteractOutside={(event) => {
          if (!triggerButtonRef.current?.contains(event.target as Node)) {
            interactedOutsideRef.current = true;
          }
        }}
        onCloseAutoFocus={(event) => {
          if (onAfterClose) {
            event.preventDefault();
            if (!interactedOutsideRef.current) {
              onAfterClose();
            }
          }
        }}
      >
        {view === "slider" && reasoningOptions.length > 0 ? (
          <div className="w-[17.5rem] p-2.5">
            {/* 单行 Header：展示 Logo、模型名 · 档位高亮、紧随其后的下钻箭头（hover不位移）、重置按钮 */}
            <div className="flex h-8 items-center justify-between gap-1.5 border-b border-border/60 pb-1.5">
              <button
                type="button"
                className={cn(
                  "flex min-w-0 max-w-[calc(100%-2rem)] items-center gap-1.5 rounded-md px-1.5 py-1 text-left transition-colors cursor-default",
                  appPopoverItemStateClassName,
                )}
                onClick={() => setView("catalog")}
              >
                {activeBrand ? <RoundBrandIcon name={activeBrand} sizeClassName="size-4" /> : null}
                <span className="min-w-0 truncate text-xs font-medium text-foreground">
                  {label}
                </span>
                {resolvedSelection?.modelConfig?.costMultiplier !== undefined ? (
                  <span
                    className={cn(
                      "shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium leading-none tracking-wide select-none",
                      resolvedSelection.modelConfig.costMultiplier === 0
                        ? "bg-emerald-500/15 text-emerald-600 dark:bg-emerald-500/20 dark:text-emerald-400"
                        : "bg-muted text-muted-foreground",
                    )}
                  >
                    {formatModelCostMultiplier(resolvedSelection.modelConfig.costMultiplier, t("models.cost_free"))}
                  </span>
                ) : null}
                <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
              </button>
              <button
                type="button"
                aria-label={t("provider.reasoningEffort.reset")}
                title={t("provider.reasoningEffort.reset")}
                disabled={modelSettingsPending || activeReasoning === recommendedReasoning}
                className={cn(
                  "flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors cursor-default",
                  activeReasoning !== recommendedReasoning
                    ? "hover:bg-interactive-hover hover:text-foreground"
                    : "opacity-35",
                )}
                onClick={() => handleReasoningSelect(recommendedReasoning)}
              >
                <RotateCcw className="size-3.5" />
              </button>
            </div>

            {/* 分段滑块 */}
            <div className="pt-2.5 px-0.5" inert={modelSettingsPending}>
              <SteppedSlider
                options={reasoningOptions}
                value={activeReasoning}
                onChange={handleReasoningSelect}
                onPreviewChange={(next) => {
                  setSelectedReasoning(next);
                }}
              />
            </div>
          </div>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
            {/* 如果支持推理模型，从 slider 下钻到 catalog 时显示返回按钮 */}
            {reasoningOptions.length > 0 ? (
              <div className="flex h-8 shrink-0 items-center border-b border-border/60 px-2.5">
                <button
                  type="button"
                  className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground cursor-default"
                  onClick={() => setView("slider")}
                >
                  <ChevronLeft className="size-3.5" />
                  <span>{t("common.back")}</span>
                </button>
              </div>
            ) : null}

            {providersQuery.isLoading ? (
              <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
                <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("common.loading")}</div>
              </div>
            ) : selectableProfiles.length === 0 ? (
              <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
                <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("picker.noModels")}</div>
              </div>
            ) : selectableProfiles.length === 1 ? (
              <div
                className="flex min-h-0 flex-1 flex-col overflow-hidden"
                style={{ height: profilePaneHeight, maxHeight: "100%" }}
              >
                {isSyncableProfile(selectableProfiles[0]) ? (
                  <div className="flex h-7 shrink-0 items-center justify-between border-b border-border/40 px-2.5 text-[11px] text-muted-foreground">
                    <span className="truncate font-medium">{selectableProfiles[0].displayName}</span>
                    <button
                      type="button"
                      aria-label={t("picker.syncModels")}
                      title={t("picker.syncModels")}
                      disabled={modelSettingsPending || syncingProfileID === selectableProfiles[0].id}
                      className="flex size-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-interactive-hover hover:text-foreground cursor-default"
                      onClick={() => handleSyncProfile(selectableProfiles[0].id)}
                    >
                      {syncingProfileID === selectableProfiles[0].id ? (
                        <Spinner className="size-3" />
                      ) : (
                        <RefreshCw className="size-3" />
                      )}
                    </button>
                  </div>
                ) : null}
                <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]">
                  <ProfileModels
                    currentModel={currentModelAvailable ? selectedModel : ""}
                    isCurrentProfile={currentModelAvailable}
                    profile={selectableProfiles[0]}
                    disabled={modelSettingsPending}
                    onPick={(model) => handleModelPick(selectableProfiles[0], model)}
                  />
                </div>
              </div>
            ) : (
              <div
                className="grid min-h-0 flex-1 shrink grid-cols-[2.75rem_minmax(0,1fr)] overflow-hidden"
                style={{ height: profilePaneHeight, maxHeight: "100%" }}
              >
                <div className="min-h-0 overflow-y-auto border-r border-border/70 p-1">
                  <div className="flex flex-col items-center gap-0.5">
                    {selectableProfiles.map((profile) => {
                      const viewed = viewedProfileID === profile.id;
                      const current = currentModelAvailable && selectedProvider === profile.id;
                      return (
                        <button
                          key={profile.id}
                          aria-current={viewed ? "true" : undefined}
                          title={profile.displayName}
                          aria-label={profile.displayName}
                          className={cn(
                            "relative flex size-8 items-center justify-center rounded-md text-muted-foreground hover:text-foreground cursor-default transition-colors",
                            appPopoverItemStateClassName,
                            viewed && cn(appPopoverSelectedItemStateClassName, "text-foreground"),
                          )}
                          type="button"
                          onClick={() => setViewedProfileID(profile.id)}
                        >
                          <RoundBrandIcon name={providerBrandKey(profile)} />
                          <span className="sr-only">{profile.displayName}</span>
                          {current ? (
                            <span className="absolute bottom-1 right-1 size-1.5 rounded-full bg-success ring-1 ring-background" />
                          ) : null}
                        </button>
                      );
                    })}
                  </div>
                </div>
                <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
                  {viewedProfile && isSyncableProfile(viewedProfile) ? (
                    <div className="flex h-7 shrink-0 items-center justify-between border-b border-border/40 px-2.5 text-[11px] text-muted-foreground">
                      <span className="truncate font-medium">{viewedProfile.displayName}</span>
                      <button
                        type="button"
                        aria-label={t("picker.syncModels")}
                        title={t("picker.syncModels")}
                        disabled={modelSettingsPending || syncingProfileID === viewedProfile.id}
                        className="flex size-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-interactive-hover hover:text-foreground cursor-default"
                        onClick={() => handleSyncProfile(viewedProfile.id)}
                      >
                        {syncingProfileID === viewedProfile.id ? (
                          <Spinner className="size-3" />
                        ) : (
                          <RefreshCw className="size-3" />
                        )}
                      </button>
                    </div>
                  ) : null}
                  <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]">
                    {viewedProfile ? (
                      <ProfileModels
                        currentModel={currentModelAvailable && selectedProvider === viewedProfile.id ? selectedModel : ""}
                        isCurrentProfile={currentModelAvailable && selectedProvider === viewedProfile.id}
                        profile={viewedProfile}
                        disabled={modelSettingsPending}
                        onPick={(model) => handleModelPick(viewedProfile, model)}
                      />
                    ) : (
                      <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("picker.noModels")}</div>
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

function RoundBrandIcon({
  name,
  sizeClassName = "size-5",
  iconClassName,
}: {
  name: string;
  sizeClassName?: string;
  iconClassName?: string;
}) {
  return <BrandIcon className={cn("shrink-0", sizeClassName)} iconClassName={iconClassName} name={name} shape="circle" />;
}

function providerBrandKey(profile?: ProviderProfile) {
  return profile?.brand || profile?.displayName || profile?.id || "";
}

function formatContextWindow(tokens: number): string {
  if (tokens >= 1_000_000) {
    const m = tokens / 1_000_000;
    return `${Number.isInteger(m) ? m : m.toFixed(1)}M`;
  }
  if (tokens >= 1_000) {
    const k = tokens / 1_000;
    return `${Number.isInteger(k) ? k : k.toFixed(0)}K`;
  }
  return String(tokens);
}

function isSyncableProfile(profile: ProviderProfile | undefined | null): boolean {
  if (!profile?.brand) return false;
  const brand = profile.brand.toLowerCase();
  return brand === "buzzhive" || brand === "openrouter";
}

function formatModelCostMultiplier(costMultiplier: number | undefined | null, freeLabel: string): string | null {
  if (costMultiplier === undefined || costMultiplier === null) {
    return null;
  }
  if (costMultiplier === 0) {
    return freeLabel;
  }
  if (costMultiplier < 0.01) {
    return "<0.01x";
  }
  if (costMultiplier < 1) {
    const formatted = parseFloat(costMultiplier.toFixed(2));
    return `${formatted}x`;
  }
  const formatted = parseFloat(costMultiplier.toFixed(1));
  return `${formatted}x`;
}

function ProfileModels({
  profile,
  disabled,
  currentModel,
  isCurrentProfile,
  onPick,
}: {
  profile: ProviderProfile;
  disabled: boolean;
  currentModel: string;
  isCurrentProfile: boolean;
  onPick: (model: string) => void;
}) {
  const { t } = useI18n();
  const models = profile.models.filter((model) => Boolean(model.id));
  if (models.length === 0) {
    return <div className="px-2.5 py-1 text-xs text-muted-foreground">{t("picker.noModels")}</div>;
  }

  return (
    <TooltipProvider delayDuration={350}>
      <div className="grid gap-0.5">
        {models.map((model) => {
          const selected = isCurrentProfile && currentModel === model.id;
          const unavailable = Boolean(model.unavailable);
          const label = formatModelLabel(model.id, model.displayName);
          const costBadge = formatModelCostMultiplier(model.costMultiplier, t("models.cost_free"));

          const caps: string[] = [];
          if (model.capabilities?.image) {
            caps.push(t("models.capability.vision"));
          }
          if (model.capabilities?.tools !== false) {
            caps.push(t("models.capability.tools"));
          }
          if (model.capabilities?.audio) {
            caps.push(t("models.capability.audio"));
          }
          const capabilitiesText = caps.length > 0 ? caps.join(" · ") : null;

          return (
            <Tooltip key={model.id}>
              <TooltipTrigger asChild>
                <button
                  aria-current={selected ? "true" : undefined}
                  className={cn(
                    "flex h-8 w-full min-w-0 items-center gap-2 overflow-hidden rounded-md px-2 text-left text-[13px] cursor-default",
                    appPopoverItemStateClassName,
                    selected && cn(appPopoverSelectedItemStateClassName, "text-foreground font-medium"),
                    unavailable && "opacity-50 hover:bg-transparent cursor-not-allowed",
                  )}
                  type="button"
                  disabled={disabled || unavailable}
                  onClick={() => !unavailable && onPick(model.id)}
                >
                  <span className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
                    <span className={cn("block min-w-0 flex-1 truncate", unavailable && "line-through text-muted-foreground")}>
                      {label}
                    </span>
                  </span>
                  {unavailable ? (
                    <span className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium leading-none tracking-wide select-none bg-destructive/10 text-destructive dark:bg-destructive/20 dark:text-destructive">
                      {t("models.unavailable")}
                    </span>
                  ) : costBadge ? (
                    <span
                      className={cn(
                        "shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium leading-none tracking-wide select-none",
                        model.costMultiplier === 0
                          ? "bg-emerald-500/15 text-emerald-600 dark:bg-emerald-500/20 dark:text-emerald-400"
                          : "bg-muted text-muted-foreground",
                      )}
                    >
                      {costBadge}
                    </span>
                  ) : null}
                </button>
              </TooltipTrigger>
              <TooltipContent
                side="right"
                align="center"
                sideOffset={8}
                className="grid gap-1.5 p-2 text-xs font-normal"
              >
                <div className="flex flex-col">
                  <span className="font-medium text-background">{label}</span>
                  {model.id !== label ? (
                    <span className="font-mono text-[10px] text-background/70">{model.id}</span>
                  ) : null}
                </div>
                <div className="grid gap-0.5 border-t border-background/20 pt-1 text-[11px] text-background/80">
                  {model.contextWindow ? (
                    <div className="flex items-center justify-between gap-3">
                      <span>{t("provider.contextWindow")}</span>
                      <span className="font-mono text-background">{formatContextWindow(model.contextWindow)}</span>
                    </div>
                  ) : null}
                  {capabilitiesText ? (
                    <div className="flex items-center justify-between gap-3">
                      <span>{t("provider.capabilities")}</span>
                      <span className="text-background">{capabilitiesText}</span>
                    </div>
                  ) : null}
                  {costBadge ? (
                    <div className="flex items-center justify-between gap-3">
                      <span>{t("models.costMultiplier")}</span>
                      <span
                        className={cn(
                          "font-medium",
                          model.costMultiplier === 0 ? "text-emerald-400" : "text-background",
                        )}
                      >
                        {costBadge}
                      </span>
                    </div>
                  ) : null}
                  {unavailable ? (
                    <div className="border-t border-background/20 pt-1 text-[11px] text-amber-400">
                      {t("models.unavailableTip")}
                    </div>
                  ) : null}
                </div>
              </TooltipContent>
            </Tooltip>
          );
        })}
      </div>
    </TooltipProvider>
  );
}
