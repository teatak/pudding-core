import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

import {
  listProviders,
  updateSession,
  type ProviderModel,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuRadioItem as DropdownMenuRadioItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
  AppDropdownMenuSubContent as DropdownMenuSubContent,
  AppDropdownMenuSubTrigger as DropdownMenuSubTrigger,
} from "@/components/AppMenu";
import { type ResolvedModelSelection } from "@/lib/modelSelection";
import {
  defaultReasoningEffortForSelection,
  reasoningEffortOptionsForSelection,
} from "@/components/ReasoningEffortChip";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuSub,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { cn } from "@/lib/utils";

type ModelReasoningPickerProps = {
  token: string;
  session?: Session;
  value?: { provider?: string; model?: string };
  reasoningValue: string;
  onChange?: (value: { provider: string; model: string }) => void;
  onAfterClose?: () => void;
  onReasoningChange: (value: string) => void;
  onResolvedChange?: (value: ResolvedModelSelection | null) => void;
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
  className,
}: ModelReasoningPickerProps) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [open, setOpen] = useState(false);

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
    onSuccess: async (updated) => {
      if (updated.provider && updated.model) {
        onResolvedChange?.(resolveSelection(updated.provider, updated.model));
      }
      setOpen(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
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
  const defaultReasoningEffort = defaultReasoningEffortForSelection(resolvedSelection);
  const selectedReasoning = reasoningOptions.includes(reasoningValue) ? reasoningValue : "auto";
  const knownDefaultReasoning = defaultReasoningEffort && reasoningOptions.includes(defaultReasoningEffort) ? defaultReasoningEffort : undefined;
  const displayReasoning = selectedReasoning === "auto" && knownDefaultReasoning ? knownDefaultReasoning : selectedReasoning;

  const [expanded, setExpanded] = useState(selectedProvider);
  useEffect(() => {
    if (open) {
      const expandedProfile = selectableProfiles.some((profile) => profile.id === selectedProvider)
        ? selectedProvider
        : selectableProfiles[0]?.id || "";
      setExpanded(expandedProfile);
    }
  }, [open, selectedProvider, selectableProfiles]);

  useEffect(() => {
    if (!providersQuery.isSuccess) {
      return;
    }
    onResolvedChange?.(resolvedSelection);
  }, [onResolvedChange, providersQuery.isSuccess, resolvedSelection]);

  const activeBrand = visibleModel ? providerBrandKey(activeProfile) || selectedProvider : "";
  const label = visibleModel ? formatModelLabel(visibleModel) : t("picker.selectModel");
  const reasoningLabel = reasoningOptions.length > 0 ? t(`provider.reasoningEffort.${displayReasoning}`) : "";

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("session.model")}
          className={cn(
            "pudding-composer-model-picker group/model-picker h-6 min-w-0 max-w-[9.5rem] shrink gap-0.5 rounded-full border-0 bg-muted py-0 pr-1.5 text-xs font-normal text-foreground transition-none hover:bg-accent aria-expanded:bg-accent data-[state=open]:bg-accent sm:max-w-[12rem] dark:hover:bg-accent dark:aria-expanded:bg-accent dark:data-[state=open]:bg-accent",
            visibleModel ? "pl-0.5" : "pl-2",
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
          <span className="flex h-5 min-w-0 flex-1 items-center gap-1 overflow-hidden text-foreground/75">
            <span className="pudding-composer-model-label min-w-0 flex-1 truncate">{label}</span>
            {reasoningLabel ? <span className="pudding-composer-reasoning-detail shrink-0 text-muted-foreground/70">·</span> : null}
            {reasoningLabel ? <span className="pudding-composer-reasoning-detail shrink-0">{reasoningLabel}</span> : null}
            <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="w-44"
        side="top"
        sideOffset={8}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          onAfterClose?.();
        }}
      >
        {reasoningOptions.length > 0 ? (
          <>
            <DropdownMenuLabel>{t("provider.reasoningEffort")}</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={displayReasoning} onValueChange={(next) => onReasoningChange(next === "auto" ? "" : next)}>
              {reasoningOptions.map((item) => (
                <DropdownMenuRadioItem key={item} className="h-7 text-xs" value={item}>
                  {t(`provider.reasoningEffort.${item}`)}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
            <DropdownMenuSeparator />
          </>
        ) : null}
        <DropdownMenuLabel>{t("session.model")}</DropdownMenuLabel>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger className="h-8 min-w-0" >
            <span className="min-w-0 flex-1 truncate">{label}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent alignOffset={-164} className="w-56 max-w-[calc(100vw-2rem)] p-2">
            {providersQuery.isLoading ? (
              <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("common.loading")}</div>
            ) : selectableProfiles.length === 0 ? (
              <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("picker.noModels")}</div>
            ) : (
              <Accordion collapsible type="single" value={expanded} onValueChange={setExpanded}>
                {selectableProfiles.map((profile) => {
                  const profileSelected = currentModelAvailable && selectedProvider === profile.id;
                  return (
                    <AccordionItem key={profile.id} className="not-last:border-b-0" value={profile.id}>
                      <AccordionTrigger
                        aria-current={profileSelected ? "true" : undefined}
                        className={cn(
                          "min-w-0 items-center rounded-md px-2.5 py-1.5 text-sm font-normal text-muted-foreground hover:bg-accent hover:text-foreground hover:no-underline [&_[data-slot=accordion-trigger-icon]]:ml-2 [&_[data-slot=accordion-trigger-icon]]:text-muted-foreground/70",
                          profileSelected && "text-foreground",
                        )}

                      >
                        <span className="flex min-w-0 flex-1 items-center gap-2">
                          <RoundBrandIcon name={providerBrandKey(profile)} />
                          <span className="min-w-0 flex-1 truncate">{profile.displayName}</span>
                          {profileSelected ? <span className="size-2 shrink-0 rounded-full bg-success/85" /> : null}
                        </span>
                      </AccordionTrigger>
                      <AccordionContent className="pb-0">
                        <ProfileModels
                          currentModel={currentModelAvailable && selectedProvider === profile.id ? selectedModel : ""}
                          isCurrentProfile={currentModelAvailable && selectedProvider === profile.id}
                          profile={profile}
                          onPick={(model) => {
                            if (session) {
                              patchMutation.mutate({ provider: profile.id, model });
                              return;
                            }
                            onChange?.({ provider: profile.id, model });
                            setOpen(false);
                          }}
                        />
                      </AccordionContent>
                    </AccordionItem>
                  );
                })}
              </Accordion>
            )}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      </DropdownMenuContent>
    </DropdownMenu>
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
    <div className="grid gap-0.5 py-0.5">
      {models.map((model) => {
        const selected = isCurrentProfile && currentModel === model;
        const label = formatModelLabel(model);
        return (
          <button
            key={model}
            className="flex w-full min-w-0 items-center gap-2 overflow-hidden rounded-md py-1.5 pr-2.5 pl-5 text-left text-[13px] hover:bg-accent"

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
