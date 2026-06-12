import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import { getSettings, listProviders, updateSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/i18n";
import { PROVIDER_PRESETS } from "@/provider/presets";

const DEFAULT_PROVIDER = "__default__";
const DEFAULT_MODEL = "__default_model__";

// 选择器刻意做"安静"样式:无边框、低对比,hover 才浮现交互感
const quietTrigger =
  "h-7 max-w-44 gap-1 rounded-md border-0 bg-transparent px-2 text-xs text-muted-foreground shadow-none " +
  "hover:bg-accent hover:text-foreground focus-visible:ring-0 dark:bg-transparent dark:hover:bg-accent";

// ModelPicker 住在 composer 底排:模型跟随输入,不进 header。
// provider/model 是 session 属性,改动走 PATCH,只影响后续 turn(后端快照语义)。
export function ModelPicker({ token, session }: { token: string; session: Session }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
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
      await queryClient.invalidateQueries({ queryKey: queryKeys.sessions() });
    },
  });
  const defaultProvider = settingsQuery.data?.settings["provider.default"] || "default";
  const providerName = session.provider || defaultProvider;
  const activeProfile = providersQuery.data?.providers.find((profile) => profile.name === providerName);
  const modelOptions = useMemo(() => {
    const preset = PROVIDER_PRESETS.find((item) => item.id === providerName || item.name === providerName);
    const options = new Set<string>();
    if (session.model) {
      options.add(session.model);
    }
    // 默认模型是 profile 属性,不存在全局默认模型
    if (activeProfile?.defaultModel) {
      options.add(activeProfile.defaultModel);
    }
    preset?.models.forEach((model) => options.add(model));
    return Array.from(options);
  }, [activeProfile?.defaultModel, providerName, session.model]);

  return (
    <div className="flex min-w-0 items-center">
      <Select
        value={session.provider || DEFAULT_PROVIDER}
        onValueChange={(value) => patchMutation.mutate({ provider: value === DEFAULT_PROVIDER ? "" : value })}
      >
        <SelectTrigger aria-label={t("session.provider")} className={quietTrigger} size="sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={DEFAULT_PROVIDER}>{t("session.providerDefault")}</SelectItem>
          {(providersQuery.data?.providers || []).map((profile) => (
            <SelectItem key={profile.name} value={profile.name}>
              {profile.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        value={session.model || DEFAULT_MODEL}
        onValueChange={(value) => patchMutation.mutate({ model: value === DEFAULT_MODEL ? "" : value })}
      >
        <SelectTrigger aria-label={t("session.model")} className={quietTrigger} size="sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={DEFAULT_MODEL}>{t("session.modelDefault")}</SelectItem>
          {modelOptions.map((model) => (
            <SelectItem key={model} value={model}>
              {model}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
