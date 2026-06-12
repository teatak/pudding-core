import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown } from "lucide-react";
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
import { cn } from "@/lib/utils";

// 两层模型选择(docs/design.md 第 4 节):第一层 profile,第二层模型,
// 默认展开当前 session 所用 profile;选中一次 PATCH 同写 provider + model。
export function ModelPicker({ token, session }: { token: string; session: Session }) {
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
    mutationFn: (body: { provider?: string; model?: string }) => updateSession(token, session.id, body),
    onSuccess: async () => {
      setOpen(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const defaultProvider = settingsQuery.data?.settings["provider.default"] || "default";
  const currentProfileName = session.provider || defaultProvider;
  const activeProfile = profiles.find((p) => p.name === currentProfileName);
  const followingDefault = !session.provider && !session.model;

  const [expanded, setExpanded] = useState(currentProfileName);
  useEffect(() => {
    if (open) {
      setExpanded(currentProfileName);
    }
  }, [open, currentProfileName]);

  const triggerLabel = followingDefault
    ? t("session.providerDefault")
    : `${currentProfileName} · ${session.model || activeProfile?.defaultModel || t("common.default")}`;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          aria-label={t("session.model")}
          className="h-7 max-w-60 gap-1 px-2 text-xs font-normal text-muted-foreground hover:text-foreground"
          size="sm"
          variant="ghost"
        >
          <span className="truncate">{triggerLabel}</span>
          <ChevronDown className="size-3 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 p-2" side="top" sideOffset={8}>
        <button
          className={cn(
            "flex w-full items-center justify-between rounded-md px-2.5 py-2 text-sm hover:bg-accent",
            followingDefault && "text-foreground",
          )}
          type="button"
          onClick={() => patchMutation.mutate({ provider: "", model: "" })}
        >
          {t("session.providerDefault")}
          {followingDefault ? <Check className="size-3.5 text-primary" /> : null}
        </button>
        <Accordion collapsible className="mt-1" type="single" value={expanded} onValueChange={setExpanded}>
          {profiles.map((profile) => (
            <AccordionItem key={profile.name} className="border-b-0" value={profile.name}>
              <AccordionTrigger className="rounded-md px-2.5 py-2 text-sm hover:bg-accent hover:no-underline">
                <span className="flex min-w-0 items-center gap-2">
                  <BrandIcon className="size-4 shrink-0" name={profile.name} />
                  <span className="truncate">{profile.name}</span>
                  <Badge className="text-[10px] font-normal" variant="outline">
                    {profile.type}
                  </Badge>
                </span>
              </AccordionTrigger>
              <AccordionContent className="pb-1">
                <ProfileModels
                  currentModel={session.provider === profile.name ? session.model : ""}
                  isCurrentProfile={currentProfileName === profile.name && Boolean(session.provider)}
                  profile={profile}
                  onPick={(model) => patchMutation.mutate({ provider: profile.name, model })}
                />
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </PopoverContent>
    </Popover>
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
  const models = Array.from(new Set([profile.defaultModel, ...profile.models, currentModel].filter(Boolean)));

  if (models.length === 0) {
    return <div className="px-2.5 py-1 text-xs text-muted-foreground">{t("picker.noModels")}</div>;
  }

  return (
    <div className="grid max-h-56 gap-0.5 overflow-y-auto px-1">
      {models.map((model) => {
        const selected = isCurrentProfile && (currentModel ? currentModel === model : profile.defaultModel === model);
        return (
          <button
            key={model}
            className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-[13px] hover:bg-accent"
            type="button"
            onClick={() => onPick(model)}
          >
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate font-mono text-xs">{model}</span>
              {model === profile.defaultModel ? (
                <Badge className="text-[10px] font-normal" variant="secondary">
                  {t("common.default")}
                </Badge>
              ) : null}
            </span>
            {selected ? <Check className="size-3.5 shrink-0 text-primary" /> : null}
          </button>
        );
      })}
    </div>
  );
}
