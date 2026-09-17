import { useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, ChevronLeft, ChevronRight, RotateCcw } from "@/components/icons";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  listProviders,
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
  const [open, setOpen] = useState(false);
  const interactedOutsideRef = useRef(false);
  const triggerButtonRef = useRef<HTMLButtonElement>(null);
  const [displayAsSliderView, setDisplayAsSliderView] = useState(false);
  const [preservedWidth, setPreservedWidth] = useState<number | null>(null);
  const sliderViewTimerRef = useRef<number | null>(null);
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
        className={cn(
          "max-h-[min(28rem,var(--radix-popover-content-available-height))] max-w-[calc(100vw-1rem)] gap-0 overflow-hidden p-0",
          view === "slider"
            ? "w-[17.5rem]"
            : selectableProfiles.length > 1
              ? "w-[19rem]"
              : "w-[13rem]",
        )}
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
                className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]"
                style={{ height: profilePaneHeight, maxHeight: "100%" }}
              >
                <ProfileModels
                  currentModel={currentModelAvailable ? selectedModel : ""}
                  isCurrentProfile={currentModelAvailable}
                  profile={selectableProfiles[0]}
                  disabled={modelSettingsPending}
                  onPick={(model) => handleModelPick(selectableProfiles[0], model)}
                />
              </div>
            ) : (
              <div
                className="grid min-h-0 flex-1 shrink grid-cols-[8rem_minmax(0,1fr)] overflow-hidden"
                style={{ height: profilePaneHeight, maxHeight: "100%" }}
              >
                <div className="min-h-0 overflow-y-auto border-r border-border/70 p-1.5">
                  <div className="grid gap-0.5">
                    {selectableProfiles.map((profile) => {
                      const viewed = viewedProfileID === profile.id;
                      const current = currentModelAvailable && selectedProvider === profile.id;
                      return (
                        <button
                          key={profile.id}
                          aria-current={viewed ? "true" : undefined}
                          className={cn(
                            "flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs text-muted-foreground hover:text-foreground cursor-default",
                            appPopoverItemStateClassName,
                            viewed && cn(appPopoverSelectedItemStateClassName, "text-foreground"),
                          )}
                          type="button"
                          onClick={() => setViewedProfileID(profile.id)}
                        >
                          <RoundBrandIcon name={providerBrandKey(profile)} />
                          <span className="min-w-0 flex-1 truncate">{profile.displayName}</span>
                          {current ? <span className="size-1.5 shrink-0 rounded-full bg-success" /> : null}
                        </button>
                      );
                    })}
                  </div>
                </div>
                <div className="min-h-0 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]">
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
    <div className="grid gap-0.5">
      {models.map((model) => {
        const selected = isCurrentProfile && currentModel === model.id;
        const label = formatModelLabel(model.id, model.displayName);
        return (
          <button
            key={model.id}
            className={cn(
              "flex h-8 w-full min-w-0 items-center gap-2 overflow-hidden rounded-md px-2 text-left text-[13px] cursor-default",
              appPopoverItemStateClassName,
            )}
            type="button"
            disabled={disabled}
            onClick={() => onPick(model.id)}
          >
            <span className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
              <span className="block min-w-0 flex-1 truncate">
                {label}
              </span>
            </span>
            {selected ? <Check className="size-4 shrink-0" /> : null}
          </button>
        );
      })}
    </div>
  );
}
