import { useQuery } from "@tanstack/react-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { PanelLeft, RefreshCw } from "lucide-react";
import { useEffect, useMemo } from "react";

import { getSettings, listProviders, listSessions, updateSession, type Session } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Composer } from "@/components/Composer";
import { LanguageToggle } from "@/components/LanguageToggle";
import { SettingsDialog } from "@/components/SettingsDialog";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Transcript } from "@/components/Transcript";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useSessionEvents } from "@/hooks/useSessionEvents";
import { useI18n } from "@/i18n";
import { PROVIDER_PRESETS } from "@/provider/presets";

type ChatPaneProps = {
  token: string;
  selectedSessionID: string | undefined;
};

const DEFAULT_PROVIDER = "__default__";
const DEFAULT_MODEL = "__default_model__";

export function ChatPane({ token, selectedSessionID }: ChatPaneProps) {
  const navigate = useNavigate({ from: "/" });
  const { t } = useI18n();
  const sessionsQuery = useQuery({
    queryKey: queryKeys.sessions(),
    queryFn: () => listSessions(token),
    enabled: Boolean(token),
  });
  const sessions = sessionsQuery.data?.sessions || [];
  const selectedSession = sessions.find((session) => session.id === selectedSessionID);
  const activeSessionID = selectedSession?.id;

  useEffect(() => {
    if (!sessionsQuery.isSuccess) {
      return;
    }
    if (!selectedSessionID && sessions[0]) {
      void navigate({ to: "/", search: { session: sessions[0].id } });
      return;
    }
    if (selectedSessionID && !selectedSession) {
      const nextSessionID = sessions[0]?.id;
      if (nextSessionID) {
        void navigate({ to: "/", search: { session: nextSessionID } });
      } else {
        void navigate({ to: "/", search: {}, replace: true });
      }
    }
  }, [navigate, selectedSession, selectedSessionID, sessions, sessionsQuery.isSuccess]);

  useSessionEvents(activeSessionID, token);

  return (
    <section className="flex h-full min-w-0 flex-1 flex-col overflow-hidden">
      <header className="flex h-14 items-center justify-between border-b bg-background px-4">
        <div className="flex min-w-0 items-center gap-2">
          <PanelLeft className="h-4 w-4 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{selectedSession?.title || t("session.noSelected")}</div>
            {selectedSession ? (
              <div className="truncate text-xs text-muted-foreground">
                {selectedSession.provider || t("session.providerDefault")} · {selectedSession.model || t("session.modelDefault")}
              </div>
            ) : null}
          </div>
        </div>
        <div className="flex items-center gap-1">
          {selectedSession ? <SessionProviderControls token={token} session={selectedSession} /> : null}
          <ThemeToggle />
          <LanguageToggle />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label={t("common.refresh")}
                size="icon"
                variant="ghost"
                onClick={() => {
                  void sessionsQuery.refetch();
                }}
              >
                <RefreshCw />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("common.refresh")}</TooltipContent>
          </Tooltip>
          <SettingsDialog token={token} />
        </div>
      </header>
      <Separator />
      {activeSessionID ? (
        <>
          <Transcript token={token} sessionID={activeSessionID} />
          <Composer token={token} sessionID={activeSessionID} />
        </>
      ) : (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          {t("session.selectOrCreate")}
        </div>
      )}
    </section>
  );
}

function SessionProviderControls({ token, session }: { token: string; session: Session }) {
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
  const defaultProvider = settingsQuery.data?.settings["provider.default"] || "";
  const providerName = session.provider || defaultProvider;
  const modelOptions = useMemo(() => {
    const preset = PROVIDER_PRESETS.find((item) => item.id === providerName || item.name === providerName);
    const options = new Set<string>();
    if (session.model) {
      options.add(session.model);
    }
    if (settingsQuery.data?.settings["model.default"]) {
      options.add(settingsQuery.data.settings["model.default"]);
    }
    preset?.models.forEach((model) => options.add(model));
    return Array.from(options);
  }, [providerName, session.model, settingsQuery.data?.settings]);

  return (
    <div className="hidden items-center gap-2 md:flex">
      <Select
        value={session.provider || DEFAULT_PROVIDER}
        onValueChange={(value) => patchMutation.mutate({ provider: value === DEFAULT_PROVIDER ? "" : value })}
      >
        <SelectTrigger aria-label={t("session.provider")} className="w-40" size="sm">
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
        <SelectTrigger aria-label={t("session.model")} className="w-44" size="sm">
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
