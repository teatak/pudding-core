import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, ChevronRight } from "@/components/icons";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  listProviders,
  updateSession,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuRadioItem as DropdownMenuRadioItem,
} from "@/components/AppMenu";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { composerControlStateClassName } from "@/components/composerControlStyles";
import { type ResolvedModelSelection } from "@/lib/modelSelection";
import { reasoningEffortOptionsForSelection } from "@/components/ReasoningEffortChip";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuRadioGroup,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { cn } from "@/lib/utils";
import { providerPresetForModel } from "@/provider/presets";

type ModelReasoningPickerProps = {
  token: string;
  session?: Session;
  value?: { provider?: string; model?: string };
  reasoningValue: string;
  onChange?: (value: { provider: string; model: string }) => void;
  onAfterClose?: () => void;
  onReasoningChange: (value: string) => void;
  onResolvedChange?: (value: ResolvedModelSelection | null) => void;
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
  onResolvedChange,
  iconOnly = false,
  className,
}: ModelReasoningPickerProps) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const restoreComposerFocusOnCloseRef = useRef(false);
  const reasoningMenu = useHoverSubmenu();

  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const profiles = useMemo(() => providersQuery.data?.providers ?? [], [providersQuery.data?.providers]);
  const resolveSelection = useCallback(
    (providerID: string, modelID: string): ResolvedModelSelection | null => {
      if (!providerID || !modelID) {
        return null;
      }
      const profile = profiles.find((item) => item.id === providerID);
      const modelConfig = profile?.models.find((item) => item.id === modelID);
      if (!profile || !modelConfig) {
        return { provider: providerID, model: modelID };
      }
      return {
        provider: providerID,
        model: modelID,
        providerBrand: profile.brand,
        providerProtocol: profile.protocol,
        modelConfig,
      };
    },
    [profiles],
  );
  const patchMutation = useMutation({
    mutationFn: (body: { provider?: string; model?: string }) => {
      if (!session) {
        throw new Error("missing session");
      }
      return updateSession(token, session.id, body);
    },
    onMutate: async (body) => {
      if (!session) {
        return {};
      }
      await Promise.all([
        queryClient.cancelQueries({ queryKey: queryKeys.sessions() }),
        queryClient.cancelQueries({ queryKey: queryKeys.session(session.id) }),
      ]);
      const previousSessions = queryClient.getQueryData<{ sessions: Session[] }>(queryKeys.sessions());
      const previousSession = queryClient.getQueryData<Session>(queryKeys.session(session.id));
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous
          ? {
              sessions: previous.sessions.map((item) =>
                item.id === session.id ? { ...item, ...body } : item,
              ),
            }
          : previous,
      );
      queryClient.setQueryData<Session>(queryKeys.session(session.id), (previous) =>
        previous ? { ...previous, ...body } : previous,
      );
      return { previousSession, previousSessions, sessionID: session.id };
    },
    onError: (_error, _body, context) => {
      if (context?.previousSessions) {
        queryClient.setQueryData(queryKeys.sessions(), context.previousSessions);
      }
      if (context?.previousSession && context.sessionID) {
        queryClient.setQueryData(queryKeys.session(context.sessionID), context.previousSession);
      }
    },
    onSuccess: (updated) => {
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous
          ? {
              sessions: previous.sessions.map((item) =>
                item.id === updated.id ? updated : item,
              ),
            }
          : previous,
      );
      queryClient.setQueryData(queryKeys.session(updated.id), updated);
      if (updated.provider && updated.model) {
        onResolvedChange?.(resolveSelection(updated.provider, updated.model));
      }
    },
    onSettled: (_data, _error, _body, context) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
      if (context?.sessionID) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.session(context.sessionID) });
      }
    },
  });

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
  const reasoningOptions = reasoningEffortOptionsForSelection(resolvedSelection);
  const selectedReasoning = reasoningOptions.includes(reasoningValue) ? reasoningValue : "auto";

  const [viewedProfileID, setViewedProfileID] = useState(selectedProvider);
  useEffect(() => {
    if (open) {
      const initialProfile = selectableProfiles.some((profile) => profile.id === selectedProvider)
        ? selectedProvider
        : selectableProfiles[0]?.id || "";
      setViewedProfileID(initialProfile);
    }
  }, [open, selectedProvider, selectableProfiles]);
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

  useEffect(() => {
    if (!providersQuery.isSuccess) {
      return;
    }
    onResolvedChange?.(resolvedSelection);
  }, [onResolvedChange, providersQuery.isSuccess, resolvedSelection]);

  const activeBrand = visibleModel
    ? providerPresetForModel(visibleModel)?.id || providerBrandKey(activeProfile) || selectedProvider
    : "";
  const label = visibleModel ? formatModelLabel(visibleModel) : t("picker.selectModel");
  const reasoningLabel = reasoningOptions.length > 0 ? t(`provider.reasoningEffort.${selectedReasoning}`) : "";
  const triggerLabel = reasoningLabel ? `${label} · ${reasoningLabel}` : label;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) {
          restoreComposerFocusOnCloseRef.current = false;
        }
        setOpen(next);
        if (!next) {
          reasoningMenu.close();
        }
      }}
    >
      <PopoverTrigger asChild>
        <Button
          aria-label={`${t("session.model")}: ${triggerLabel}`}
          className={cn(
            "pudding-composer-model-picker group/model-picker h-8 shrink rounded-full border-0 bg-transparent py-0 text-xs font-normal text-foreground transition-none",
            iconOnly
              ? "w-8 max-w-8 flex-none justify-center p-0"
              : "min-w-0 max-w-[9.5rem] gap-1 pr-1.5 sm:max-w-[10.5rem]",
            composerControlStateClassName,
            !iconOnly && (visibleModel ? "pl-1" : "pl-2"),
            className,
          )}
          size="sm"
          title={triggerLabel}
          variant="ghost"
        >
          {visibleModel ? (
            activeBrand && BrandIcon({ name: activeBrand })
              ? <RoundBrandIcon name={activeBrand} sizeClassName="size-5" />
              : <span className="grid size-5 shrink-0 place-items-center rounded-full bg-background/60 text-[10px] text-foreground">{(activeProfile?.displayName || selectedProvider).slice(0, 1).toUpperCase()}</span>
          ) : null}
          {iconOnly ? null : (
            <span className="flex h-5 min-w-0 flex-1 items-center gap-1 overflow-hidden text-foreground/75">
              <span className="pudding-composer-model-label min-w-0 flex-1 truncate">{label}</span>
              {reasoningLabel ? <span className="pudding-composer-reasoning-detail shrink-0 text-muted-foreground/70">·</span> : null}
              {reasoningLabel ? <span className="pudding-composer-reasoning-detail shrink-0 text-muted-foreground/70">{reasoningLabel}</span> : null}
              <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className={cn(
          "max-h-[min(28rem,var(--radix-popover-content-available-height))] max-w-[calc(100vw-1rem)] gap-0 overflow-hidden p-0",
          selectableProfiles.length > 1 ? "w-[19rem]" : "w-[13rem]",
        )}
        collisionPadding={8}
        side="top"
        sideOffset={8}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          if (restoreComposerFocusOnCloseRef.current) {
            restoreComposerFocusOnCloseRef.current = false;
            onAfterClose?.();
          }
        }}
      >
        {reasoningOptions.length > 0 ? (
          <div className="shrink-0 border-b border-border/70 px-1 py-1.5">
            <DropdownMenu
              modal={false}
              open={reasoningMenu.open}
              onOpenChange={reasoningMenu.setOpen}
            >
              <DropdownMenuTrigger asChild>
                <button
                  className="flex h-8 w-full items-center gap-1.5 rounded-md px-1.5 text-left text-xs hover:bg-control-hover active:bg-control-active data-[state=open]:bg-control-hover"
                  type="button"
                  onPointerEnter={reasoningMenu.openFromHover}
                  onPointerLeave={reasoningMenu.closeFromHover}
                >
                  <span className="whitespace-nowrap text-muted-foreground">{t("provider.reasoningEffort")}</span>
                  <span className="ml-auto whitespace-nowrap">{reasoningLabel}</span>
                  <ChevronRight className="size-3 shrink-0 text-muted-foreground" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                align="start"
                className="w-28 min-w-28"
                collisionPadding={8}
                side="right"
                sideOffset={6}
                onPointerEnter={reasoningMenu.cancelClose}
                onPointerLeave={reasoningMenu.closeFromHover}
              >
                <DropdownMenuRadioGroup
                  value={selectedReasoning}
                  onValueChange={(next) => {
                    onReasoningChange(next === "auto" ? "" : next);
                    reasoningMenu.close();
                  }}
                >
                  {reasoningOptions.map((item) => (
                    <DropdownMenuRadioItem key={item} className="h-7 text-xs" value={item}>
                      {t(`provider.reasoningEffort.${item}`)}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
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
            className="min-h-0 overflow-y-auto overscroll-contain p-1.5 [scrollbar-gutter:stable]"
            style={{ height: profilePaneHeight }}
          >
            <ProfileModels
              currentModel={currentModelAvailable ? selectedModel : ""}
              isCurrentProfile={currentModelAvailable}
              profile={selectableProfiles[0]}
              onPick={(model) => {
                const profile = selectableProfiles[0];
                if (session) {
                  reasoningMenu.close();
                  restoreComposerFocusOnCloseRef.current = true;
                  setOpen(false);
                  patchMutation.mutate({ provider: profile.id, model });
                  return;
                }
                onChange?.({ provider: profile.id, model });
                restoreComposerFocusOnCloseRef.current = true;
                setOpen(false);
              }}
            />
          </div>
        ) : (
          <div
            className="grid min-h-0 shrink grid-cols-[8rem_minmax(0,1fr)] overflow-hidden"
            style={{ height: profilePaneHeight }}
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
                        "flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs text-muted-foreground hover:bg-control-hover hover:text-foreground active:bg-control-active",
                        viewed && "bg-control-hover text-foreground",
                      )}
                      type="button"
                      onClick={() => {
                        reasoningMenu.close();
                        setViewedProfileID(profile.id);
                      }}
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
                  onPick={(model) => {
                    if (session) {
                      reasoningMenu.close();
                      restoreComposerFocusOnCloseRef.current = true;
                      setOpen(false);
                      patchMutation.mutate({ provider: viewedProfile.id, model });
                      return;
                    }
                    onChange?.({ provider: viewedProfile.id, model });
                    restoreComposerFocusOnCloseRef.current = true;
                    setOpen(false);
                  }}
                />
              ) : (
                <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("picker.noModels")}</div>
              )}
            </div>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

function useHoverSubmenu() {
  const [open, setOpen] = useState(false);
  const timerRef = useRef<number | null>(null);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);
  const close = useCallback(() => {
    clearTimer();
    setOpen(false);
  }, [clearTimer]);
  const openFromHover = useCallback(() => {
    clearTimer();
    timerRef.current = window.setTimeout(() => {
      setOpen(true);
      timerRef.current = null;
    }, 80);
  }, [clearTimer]);
  const closeFromHover = useCallback(() => {
    clearTimer();
    timerRef.current = window.setTimeout(() => {
      setOpen(false);
      timerRef.current = null;
    }, 180);
  }, [clearTimer]);

  useEffect(() => clearTimer, [clearTimer]);

  return {
    cancelClose: clearTimer,
    close,
    closeFromHover,
    open,
    openFromHover,
    setOpen,
  };
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
  currentModel,
  isCurrentProfile,
  onPick,
}: {
  profile: ProviderProfile;
  currentModel: string;
  isCurrentProfile: boolean;
  onPick: (model: string) => void;
}) {
  const { t } = useI18n();
  const models = profile.models.map((model) => model.id).filter(Boolean);
  if (models.length === 0) {
    return <div className="px-2.5 py-1 text-xs text-muted-foreground">{t("picker.noModels")}</div>;
  }

  return (
    <div className="grid gap-0.5">
      {models.map((model) => {
        const selected = isCurrentProfile && currentModel === model;
        const label = formatModelLabel(model);
        return (
          <button
            key={model}
            className="flex h-8 w-full min-w-0 items-center gap-2 overflow-hidden rounded-md px-2 text-left text-[13px] hover:bg-control-hover active:bg-control-active"

            type="button"
            onClick={() => onPick(model)}
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
