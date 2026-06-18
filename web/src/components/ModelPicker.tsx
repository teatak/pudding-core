import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, CircleCheck } from "lucide-react";
import { useEffect, useState } from "react";

import {
  getSettings,
  listProviders,
  updateSession,
  type ProviderProfile,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { cn } from "@/lib/utils";

type ModelPickerProps = {
  token: string;
  session?: Session;
  value?: { provider?: string; model?: string };
  onChange?: (value: { provider: string; model: string }) => void;
};

// 两层模型选择(docs/design.md 第 4 节):第一层 profile,第二层模型。
// 真实 session 下选中后 PATCH;draft 下只更新本地 value,首条发送时随 createSession 落库。
export function ModelPicker({ token, session, value, onChange }: ModelPickerProps) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [open, setOpen] = useState(false);

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
  const patchMutation = useMutation({
    mutationFn: (body: { provider?: string; model?: string }) => {
      if (!session) {
        throw new Error("missing session");
      }
      return updateSession(token, session.id, body);
    },
    onSuccess: async () => {
      setOpen(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const defaultProvider = settingsQuery.data?.settings["provider.default"] || "default";
  const selectedProvider = session?.provider || value?.provider || "";
  const selectedModel = session?.model || value?.model || "";
  const currentProfileID = selectedProvider || defaultProvider;
  const activeProfile = profiles.find((p) => p.id === currentProfileID);
  const currentModel = selectedModel || activeProfile?.models[0]?.id || "";

  const [expanded, setExpanded] = useState(currentProfileID);
  useEffect(() => {
    if (open) {
      setExpanded(currentProfileID);
    }
  }, [open, currentProfileID]);

  // 品牌图标代替 provider 名;未命中图标的 profile 回落为文字名
  const brandIcon = <RoundBrandIcon iconClassName="size-6" name={activeProfile?.id || currentProfileID} sizeClassName="size-6" />;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          aria-label={t("session.model")}
          className="group/model-picker ml-1 h-6 max-w-64 gap-0 bg-transparent p-0 text-xs font-normal text-foreground hover:bg-transparent aria-expanded:bg-transparent"
          size="sm"
          variant="ghost"
        >
          <span className="relative z-10 grid size-6 shrink-0 place-items-center overflow-hidden rounded-full">
            {BrandIcon({ name: activeProfile?.id || currentProfileID })
              ? brandIcon
              : <span className="grid size-6 place-items-center rounded-full bg-muted text-[10px] text-foreground transition-colors group-hover/model-picker:bg-accent group-aria-expanded/model-picker:bg-accent group-data-[state=open]/model-picker:bg-accent">{(activeProfile?.name || currentProfileID).slice(0, 1).toUpperCase()}</span>}
          </span>
          <span className="-ml-3 flex h-5.5 min-w-0 items-center gap-1 rounded-r-lg bg-muted pr-2 pl-4.5 text-foreground/75 transition-colors group-hover/model-picker:bg-accent group-aria-expanded/model-picker:bg-accent group-data-[state=open]/model-picker:bg-accent">
            <span className="truncate">{currentModel ? formatModelLabel(currentModel) : t("common.default")}</span>
            <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" alignOffset={-16} className="w-80 p-2" side="top" sideOffset={8}>
        <Accordion collapsible type="single" value={expanded} onValueChange={setExpanded}>
          {profiles.map((profile) => (
            <AccordionItem
              key={profile.id}
              // ui 组件的分割线带 not-last: 变体,关掉要用同变体才能命中
              className="not-last:border-b-0"
              value={profile.id}
            >
              <AccordionTrigger className="items-center rounded-md px-2.5 py-1.5 text-sm font-normal text-muted-foreground hover:bg-accent hover:text-foreground hover:no-underline [&_[data-slot=accordion-trigger-icon]]:text-muted-foreground/70">
                <span className="flex min-w-0 items-center gap-2">
                  <RoundBrandIcon name={profile.id} />
                  <span className="truncate">{profile.name}</span>
                  <Badge className="h-4 border-muted-foreground/20 bg-transparent px-1.5 text-[9px] font-normal text-muted-foreground/70" variant="outline">
                    {profile.type}
                  </Badge>
                </span>
              </AccordionTrigger>
              <AccordionContent className="pb-0">
                <ProfileModels
                  currentModel={selectedProvider === profile.id ? selectedModel : ""}
                  isCurrentProfile={currentProfileID === profile.id}
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
      </PopoverContent>
    </Popover>
  );
}

function RoundBrandIcon({
  name,
  sizeClassName = "size-5",
  iconClassName = sizeClassName,
}: {
  name: string;
  sizeClassName?: string;
  iconClassName?: string;
}) {
  const icon = BrandIcon({ name, className: iconClassName });
  if (!icon) {
    return null;
  }
  return (
    <span className={cn("grid shrink-0 place-items-center overflow-hidden rounded-full", sizeClassName)}>
      {icon}
    </span>
  );
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
  if (currentModel && !models.includes(currentModel)) {
    models.unshift(currentModel);
  }
  const defaultModel = profile.models[0]?.id || "";

  if (models.length === 0) {
    return <div className="px-2.5 py-1 text-xs text-muted-foreground">{t("picker.noModels")}</div>;
  }

  return (
    <div className="grid max-h-56 gap-0.5 overflow-y-auto py-0.5">
      {models.map((model) => {
        const selected = isCurrentProfile && (currentModel ? currentModel === model : defaultModel === model);
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
              {model === defaultModel ? (
                <Badge className="text-[10px] font-normal" variant="secondary">
                  {t("common.default")}
                </Badge>
              ) : null}
            </span>
          </button>
        );
      })}
    </div>
  );
}
