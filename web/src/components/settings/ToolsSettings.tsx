import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Eye, EyeOff } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import {
  getWebTools,
  listBrowserMCPSessions,
  listBuiltinTools,
  patchWebTools,
  type BrowserMCPSession,
  type BuiltinTool,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

import { SETTINGS_NARROW_CONTENT_CLASS, SettingsPanel } from "./shared";

export function ToolsSettings({
  token,
  onDirtyChange,
}: {
  token: string;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [apiKey, setAPIKey] = useState("");
  const [apiKeyEdited, setAPIKeyEdited] = useState(false);
  const [visible, setVisible] = useState(false);
  const builtinToolsQuery = useQuery({
    queryKey: queryKeys.builtinTools(),
    queryFn: () => listBuiltinTools(token),
    enabled: Boolean(token),
    staleTime: Infinity,
  });
  const browserMCPQuery = useQuery({
    queryKey: queryKeys.browserMCPSessions(),
    queryFn: () => listBrowserMCPSessions(token),
    enabled: Boolean(token),
    refetchInterval: 2000,
  });
  const toolsQuery = useQuery({
    queryKey: queryKeys.webTools(),
    queryFn: () => getWebTools(token),
    enabled: Boolean(token),
  });
  const tavily = toolsQuery.data?.providers.find((provider) => provider.name === "tavily");

  useEffect(() => {
    if (toolsQuery.isSuccess) {
      setAPIKey(tavily?.apiKey || "");
      setAPIKeyEdited(false);
    }
  }, [tavily?.apiKey, toolsQuery.isSuccess]);

  const mutation = useMutation({
    mutationFn: (nextAPIKey: string) =>
      patchWebTools(token, {
        fetchProvider: nextAPIKey.trim() ? "tavily" : "",
        providers: { tavily: { apiKey: nextAPIKey } },
        searchProvider: nextAPIKey.trim() ? "tavily" : "",
      }),
    onSuccess: async (_data, nextAPIKey) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.webTools() });
      toast.success(nextAPIKey.trim() ? t("settings.tools.web.saved") : t("settings.tools.web.cleared"));
    },
    onError: () => toast.error(t("settings.tools.web.saveFailed")),
  });

  const savedAPIKey = tavily?.apiKey || "";
  const dirty = apiKeyEdited && apiKey.trim() !== savedAPIKey.trim();
  const configured = Boolean(tavily?.apiKeySet);
  const loadingTools = toolsQuery.isLoading;
  const saving = mutation.isPending;
  const runtimeBuiltinTools = useMemo(
    () =>
      uniqueBrowserTools(
        browserMCPQuery.data?.sessions || [],
        (name, appID) => !appID && name === "builtin_request_user_input",
      ),
    [browserMCPQuery.data?.sessions],
  );
  const builtinTools = useMemo(
    () => mergeTools(builtinToolsQuery.data?.tools || [], runtimeBuiltinTools),
    [builtinToolsQuery.data?.tools, runtimeBuiltinTools],
  );

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  return (
    <div className={SETTINGS_NARROW_CONTENT_CLASS}>
      <BuiltinToolsPanel
        loading={builtinToolsQuery.isFetching || browserMCPQuery.isLoading}
        error={builtinToolsQuery.isError}
        tools={builtinTools}
        onRetry={() => void builtinToolsQuery.refetch()}
      />
      <SettingsPanel
        action={
          <span
            className={cn(
              "rounded-md px-2 py-0.5 text-xs",
              configured ? "bg-success/15 text-success" : "bg-warning/15 text-warning",
            )}
          >
            {configured ? t("provider.keySet") : t("provider.keyMissing")}
          </span>
        }
        title={t("settings.tools.web.title")}
      >
        <div className="grid gap-4">
          <div className="grid gap-1">
            <p className="text-sm leading-6 text-muted-foreground">{t("settings.tools.web.desc")}</p>
            <a
              className="inline-flex w-fit items-center gap-1 text-sm text-foreground underline-offset-4 hover:underline"
              href={t("settings.tools.web.signupLink")}
              rel="noreferrer"
              target="_blank"
            >
              {t("settings.tools.web.signup")}
              <ExternalLink className="size-3.5" />
            </a>
          </div>

          {toolsQuery.isError ? (
            <Alert variant="destructive">
              <AlertDescription className="grid gap-2">
                <span>{t("settings.tools.web.loadFailed")}</span>
                <Button size="sm" type="button" variant="outline" onClick={() => void toolsQuery.refetch()}>
                  {t("common.refresh")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}

          <div className="flex flex-col gap-2">
            <label className="text-sm" htmlFor="pudding-tavily-api-key">
              {t("settings.tools.web.apiKey")}
            </label>
            <div className="flex flex-col gap-2 sm:flex-row">
              <div className="relative min-w-0 flex-1">
                <Input
                  autoComplete="off"
                  className="pr-9"
                  disabled={loadingTools}
                  id="pudding-tavily-api-key"
                  name="pudding-tavily-api-key"
                  type={visible ? "text" : "password"}
                  value={apiKey}
                  onChange={(event) => {
                    setAPIKey(event.target.value);
                    setAPIKeyEdited(true);
                  }}
                />
                <button
                  aria-label={visible ? t("provider.hideAPIKey") : t("provider.showAPIKey")}
                  className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
                  type="button"
                  onClick={() => setVisible((value) => !value)}
                >
                  {visible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                </button>
              </div>
              <div className="flex gap-2">
                <Button disabled={saving || loadingTools || !dirty} type="button" onClick={() => mutation.mutate(apiKey.trim())}>
                  {mutation.isPending ? <Spinner /> : null}
                  {t("common.save")}
                </Button>
                <Button disabled={saving || loadingTools || !configured} type="button" variant="outline" onClick={() => mutation.mutate("")}>
                  {t("common.clear")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      </SettingsPanel>
    </div>
  );
}


function BuiltinToolsPanel({
  error,
  loading,
  onRetry,
  tools,
}: {
  error: boolean;
  loading: boolean;
  onRetry: () => void;
  tools: ToolInfo[];
}) {
  const { t } = useI18n();

  return (
    <Accordion className="overflow-hidden rounded-xl border bg-card" collapsible type="single">
      <AccordionItem className="border-b-0" value="builtin-tools">
        <AccordionTrigger className="h-11 items-center rounded-none border-0 px-3 py-0 text-sm font-normal hover:no-underline focus-visible:ring-0">
          <span>{`${t("settings.tools.builtin.title")} (${tools.length})`}</span>
          {loading ? <Spinner className="mr-2 size-4 text-muted-foreground" /> : null}
        </AccordionTrigger>
        <AccordionContent className="p-0">
          {error ? (
            <div className="border-t p-3">
              <Alert variant="destructive">
                <AlertDescription className="grid gap-2">
                  <span>{t("settings.tools.builtin.loadFailed")}</span>
                  <Button size="sm" type="button" variant="outline" onClick={onRetry}>
                    {t("common.refresh")}
                  </Button>
                </AlertDescription>
              </Alert>
            </div>
          ) : (
            <ToolList tools={tools} />
          )}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

function ToolList({ tools }: { tools: ToolInfo[] }) {
  const { t } = useI18n();
  if (tools.length === 0) {
    return <div className="border-t px-3 py-3 text-sm text-muted-foreground">{t("settings.tools.builtin.empty")}</div>;
  }
  return (
    <div className="divide-y divide-border/70 border-t">
      {tools.map((tool) => (
        <ToolInfoRow key={tool.id} tool={tool} />
      ))}
    </div>
  );
}

type ToolInfo = {
  id: string;
  description?: string;
  capability?: "chat" | "work" | "code";
};

function uniqueBrowserTools(
  sessions: BrowserMCPSession[],
  matches: (name: string, appID: string | undefined) => boolean,
): ToolInfo[] {
  const seen = new Set<string>();
  const tools: ToolInfo[] = [];
  for (const session of sessions) {
    for (const tool of session.tools) {
      if (!matches(tool.name, tool.appID) || seen.has(tool.name)) {
        continue;
      }
      seen.add(tool.name);
      tools.push({
        id: tool.name,
        description: tool.description,
        capability: tool.capability,
      });
    }
  }
  return tools;
}

function mergeTools(staticTools: BuiltinTool[], runtimeTools: ToolInfo[]): ToolInfo[] {
  const merged = new Map<string, ToolInfo>();
  for (const tool of [...staticTools, ...runtimeTools]) {
    merged.set(tool.id, tool);
  }
  return Array.from(merged.values());
}

function ToolInfoRow({ tool }: { tool: ToolInfo }) {
  const { t } = useI18n();
  const capabilityLabel = tool.capability ? t(`mode.${tool.capability}`) : "";
  return (
    <div className="grid gap-1 px-3 py-3">
      <div className="flex min-w-0 items-center gap-2">
        <div className="min-w-0 break-all text-xs text-foreground">{tool.id}</div>
        {capabilityLabel ? (
          <span className="shrink-0 rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {capabilityLabel}
          </span>
        ) : null}
      </div>
      <div className="line-clamp-2 text-xs leading-5 text-muted-foreground">
        {tool.description || t("settings.tools.builtin.noDescription")}
      </div>
    </div>
  );
}
