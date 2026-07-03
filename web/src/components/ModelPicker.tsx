import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, CircleCheck } from "lucide-react";
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
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { cn } from "@/lib/utils";

export type ResolvedModelSelection = {
  provider: string;
  model: string;
  providerBrand?: string;
  providerProtocol?: ProviderProfile["protocol"];
  modelConfig?: ProviderModel;
};

type ModelPickerProps = {
  token: string;
  session?: Session;
  value?: { provider?: string; model?: string };
  onChange?: (value: { provider: string; model: string }) => void;
  onAfterClose?: () => void;
  onResolvedChange?: (value: ResolvedModelSelection | null) => void;
  className?: string;
};

// 两层模型选择(docs/design.md 第 4 节):第一层 profile,第二层模型。
// 真实 session 下选中后 PATCH;draft 下只更新本地 value,首条发送时随 createSession 落库。
export function ModelPicker({ token, session, value, onAfterClose, onChange, onResolvedChange, className }: ModelPickerProps) {
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
  const hasAvailableModels = selectableProfiles.length > 0;
  const selectedProvider = session?.provider || value?.provider || "";
  const selectedModel = session?.model || value?.model || "";
  const currentProfileID = selectedProvider;
  const activeProfile = profiles.find((p) => p.id === currentProfileID);
  const currentModel = selectedModel;
  const currentModelAvailable = Boolean(
    currentProfileID &&
      currentModel &&
      activeProfile?.models.some((model) => model.id === currentModel),
  );
  const visibleModel = currentModelAvailable ? currentModel : "";

  const [expanded, setExpanded] = useState(currentProfileID);
  useEffect(() => {
    if (open) {
      const expandedProfile = selectableProfiles.some((profile) => profile.id === currentProfileID)
        ? currentProfileID
        : selectableProfiles[0]?.id || "";
      setExpanded(expandedProfile);
    }
  }, [open, currentProfileID, selectableProfiles]);

  useEffect(() => {
    if (!providersQuery.isSuccess) {
      return;
    }
    onResolvedChange?.(
      currentModelAvailable
        ? resolveSelection(currentProfileID, currentModel)
        : null,
    );
  }, [currentModel, currentModelAvailable, currentProfileID, onResolvedChange, providersQuery.isSuccess, resolveSelection]);

  // 品牌图标代替 provider 名;未命中图标的 profile 回落为文字名
  const activeBrand = visibleModel ? providerBrandKey(activeProfile) || currentProfileID : "";
  const brandIcon = activeBrand ? <RoundBrandIcon name={activeBrand} sizeClassName="size-5" /> : null;
  const label = visibleModel ? formatModelLabel(visibleModel) : t("picker.selectModel");

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
      }}
    >
      <PopoverTrigger asChild>
        <Button
          aria-label={t("session.model")}
          className={cn(
            "group/model-picker h-6 min-w-0 max-w-[12rem] gap-1 rounded-full border-0 bg-muted py-0 pr-2 text-xs font-normal text-foreground transition-none hover:bg-accent aria-expanded:bg-accent data-[state=open]:bg-accent dark:hover:bg-accent dark:aria-expanded:bg-accent dark:data-[state=open]:bg-accent",
            visibleModel ? "pl-0.5" : "pl-2",
            className,
          )}
          size="sm"
          variant="ghost"
        >
          {visibleModel ? (
            <span className="relative z-10 grid size-5 shrink-0 place-items-center overflow-hidden rounded-full">
              {activeBrand && BrandIcon({ name: activeBrand })
                ? brandIcon
                : <span className="grid size-5 place-items-center rounded-full bg-background/60 text-[10px] text-foreground">{(activeProfile?.displayName || currentProfileID).slice(0, 1).toUpperCase()}</span>}
            </span>
          ) : null}
          <span className="flex h-5 min-w-0 flex-1 items-center gap-1 overflow-hidden text-foreground/75">
            <span className="min-w-0 flex-1 truncate">{label}</span>
            <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        alignOffset={-16}
        className="w-64 max-w-[calc(100vw-2rem)] p-2"
        side="top"
        sideOffset={8}
        onOpenAutoFocus={(event) => event.preventDefault()}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          onAfterClose?.();
        }}
      >
        {providersQuery.isLoading ? (
          <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("common.loading")}</div>
        ) : !hasAvailableModels ? (
          <div className="px-2.5 py-1.5 text-xs text-muted-foreground">{t("picker.noModels")}</div>
        ) : (
          <Accordion collapsible type="single" value={expanded} onValueChange={setExpanded}>
            {selectableProfiles.map((profile) => (
              <AccordionItem
                key={profile.id}
                // ui 组件的分割线带 not-last: 变体,关掉要用同变体才能命中
                className="not-last:border-b-0"
                value={profile.id}
              >
                <AccordionTrigger className="items-center rounded-md px-2.5 py-1.5 text-sm font-normal text-muted-foreground hover:bg-accent hover:text-foreground hover:no-underline [&_[data-slot=accordion-trigger-icon]]:text-muted-foreground/70">
                  <span className="flex min-w-0 items-center gap-2">
                    <RoundBrandIcon name={providerBrandKey(profile)} />
                    <span className="min-w-0 flex-1 truncate">{profile.displayName}</span>
                  </span>
                </AccordionTrigger>
                <AccordionContent className="pb-0">
                  <ProfileModels
                    currentModel={currentModelAvailable && selectedProvider === profile.id ? selectedModel : ""}
                    isCurrentProfile={currentModelAvailable && currentProfileID === profile.id}
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
            ))}
          </Accordion>
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
  return <BrandIcon className={sizeClassName} iconClassName={iconClassName} name={name} shape="circle" />;
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
  // 只显示 profile 配置的模型清单;端点的 /models 代理仅在配置表单里
  // 作为候选来源(用户反馈:选择器不做自动加载)
  const models = profile.models.map((model) => model.id).filter(Boolean);
  if (models.length === 0) {
    return <div className="px-2.5 py-1 text-xs text-muted-foreground">{t("picker.noModels")}</div>;
  }

  return (
    <div className="grid max-h-56 gap-0.5 overflow-y-auto py-0.5">
      {models.map((model) => {
        const selected = isCurrentProfile && currentModel === model;
        return (
          <button
            key={model}
            className={cn(
              "flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-[13px] hover:bg-accent",
              selected && "bg-accent",
            )}
            type="button"
            onClick={() => onPick(model)}
          >
            {selected ? (
              <span className="grid size-5 shrink-0 place-items-center">
                <CircleCheck className="size-4 text-success/85" />
              </span>
            ) : (
              <span className="size-5 shrink-0" />
            )}
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate" title={model}>
                {formatModelLabel(model)}
              </span>
            </span>
          </button>
        );
      })}
    </div>
  );
}
