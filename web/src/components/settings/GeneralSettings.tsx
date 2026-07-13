import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { getSettings, getUserPrompt, putSettings, putUserPrompt, resetSettings } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/i18n";
import { SETTINGS_KEYS, settingsWithDefaults } from "@/lib/appSettings";
import {
  type DesktopUpdateState,
  getDesktopUpdateState,
  onDesktopUpdateState,
  setDesktopPreviewUpdatesEnabled,
} from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

import {
  SETTINGS_NARROW_CONTENT_CLASS,
  SettingsActionRow,
  SettingsNumberField,
  SettingsToggleRow,
} from "./shared";

export function GeneralSettings({
  token,
  onDirtyChange,
}: {
  token: string;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [promptContent, setPromptContent] = useState("");
  const [promptEdited, setPromptEdited] = useState(false);
  const [tailTurns, setTailTurns] = useState("2");
  const [autoThreshold, setAutoThreshold] = useState("80");
  const [showCompactSummary, setShowCompactSummary] = useState(true);
  const [showReasoning, setShowReasoning] = useState(true);
  const [showRawToolInfo, setShowRawToolInfo] = useState(true);
  const [showPreviewAppVersions, setShowPreviewAppVersions] = useState(false);
  const [desktopUpdateState, setDesktopUpdateState] = useState<DesktopUpdateState | null>(null);
  const [resetOpen, setResetOpen] = useState(false);

  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
  });
  const userPromptQuery = useQuery({
    queryKey: queryKeys.userPrompt(),
    queryFn: () => getUserPrompt(token),
    enabled: Boolean(token),
  });

  const savedSettings = useMemo(() => settingsWithDefaults(settingsQuery.data?.settings), [settingsQuery.data?.settings]);
  const savedPrompt = userPromptQuery.data?.content || "";
  const previewUpdateBusy =
    desktopUpdateState?.status === "checking" ||
    desktopUpdateState?.status === "downloading" ||
    desktopUpdateState?.status === "downloaded" ||
    desktopUpdateState?.status === "installing";

  useEffect(() => {
    let active = true;
    let receivedEvent = false;
    const unsubscribe = onDesktopUpdateState((state) => {
      if (active) {
        receivedEvent = true;
        setDesktopUpdateState(state);
      }
    });
    void getDesktopUpdateState().then((state) => {
      if (active && !receivedEvent) {
        setDesktopUpdateState(state);
      }
    });
    return () => {
      active = false;
      unsubscribe();
    };
  }, []);

  useEffect(() => {
    if (userPromptQuery.isSuccess) {
      setPromptContent(savedPrompt);
      setPromptEdited(false);
    }
  }, [savedPrompt, userPromptQuery.isSuccess]);

  useEffect(() => {
    if (!settingsQuery.isSuccess) {
      return;
    }
    setTailTurns(savedSettings[SETTINGS_KEYS.compactTailInputTurns]);
    setAutoThreshold(savedSettings[SETTINGS_KEYS.compactAutoThresholdPercent]);
    setShowCompactSummary(savedSettings[SETTINGS_KEYS.showCompactSummary] !== "false");
    setShowReasoning(savedSettings[SETTINGS_KEYS.showReasoning] !== "false");
    setShowRawToolInfo(savedSettings[SETTINGS_KEYS.showRawToolInfo] !== "false");
    setShowPreviewAppVersions(savedSettings[SETTINGS_KEYS.showAppPreviewVersions] === "true");
  }, [savedSettings, settingsQuery.isSuccess]);

  const promptMutation = useMutation({
    mutationFn: () => putUserPrompt(token, promptContent),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.userPrompt() });
      toast.success(t("settings.general.personalizationSaved"));
    },
    onError: () => toast.error(t("settings.general.saveFailed")),
  });

  const settingsMutation = useMutation({
    mutationFn: (settings: Record<string, string>) => putSettings(token, settings),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.settings() }),
        queryClient.invalidateQueries({ queryKey: ["session"] }),
      ]);
    },
    onError: () => {
      toast.error(t("settings.general.saveFailed"));
      void queryClient.invalidateQueries({ queryKey: queryKeys.settings() });
    },
  });

  const previewUpdatesMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      const state = await setDesktopPreviewUpdatesEnabled(enabled);
      if (!state) {
        throw new Error("desktop update bridge unavailable");
      }
      return state;
    },
    onSuccess: setDesktopUpdateState,
    onError: () => toast.error(t("settings.general.previewUpdatesSaveFailed")),
  });

  const resetMutation = useMutation({
    mutationFn: async () => {
      const previousPreviewSetting = desktopUpdateState?.receivePreviewUpdates === true;
      let updateState: DesktopUpdateState | null = null;
      if (previousPreviewSetting) {
        updateState = await setDesktopPreviewUpdatesEnabled(false);
        if (!updateState) {
          throw new Error("desktop update bridge unavailable");
        }
      }
      try {
        const response = await resetSettings(token);
        return { response, updateState };
      } catch (error) {
        if (previousPreviewSetting) {
          try {
            const restored = await setDesktopPreviewUpdatesEnabled(true);
            if (restored) {
              setDesktopUpdateState(restored);
            }
          } catch {
            // The original reset error remains the actionable failure.
          }
        }
        throw error;
      }
    },
    onSuccess: async ({ response, updateState }) => {
      setResetOpen(false);
      if (updateState) {
        setDesktopUpdateState(updateState);
      }
      queryClient.setQueryData(queryKeys.settings(), response);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.settings() }),
        queryClient.invalidateQueries({ queryKey: ["session"] }),
      ]);
      toast.success(t("settings.general.resetDefaultsSuccess"));
    },
    onError: () => toast.error(t("settings.general.resetDefaultsFailed")),
  });

  const saveBooleanSetting = (key: string, next: boolean, setValue: (value: boolean) => void) => {
    setValue(next);
    settingsMutation.mutate({ [key]: String(next) });
  };

  const saveNumberSetting = (
    key: string,
    value: string,
    setValue: (value: string) => void,
    min: number,
    max: number,
  ) => {
    const fallback = savedSettings[key];
    const raw = value.trim();
    if (!raw) {
      setValue(fallback);
      return;
    }
    const parsed = Number(raw);
    if (!Number.isInteger(parsed) || parsed < min || parsed > max) {
      setValue(fallback);
      toast.error(t("settings.general.saveFailed"));
      return;
    }
    const normalized = String(parsed);
    setValue(normalized);
    if (normalized !== savedSettings[key]) {
      settingsMutation.mutate({ [key]: normalized });
    }
  };

  const promptDirty = promptEdited && promptContent !== savedPrompt;
  const settingsDisabled = settingsQuery.isLoading || settingsMutation.isPending || resetMutation.isPending;

  useEffect(() => {
    onDirtyChange(promptDirty);
  }, [onDirtyChange, promptDirty]);

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-8")}>
      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.general.personalization")}</h3>
          <p className="text-sm leading-6 text-muted-foreground">{t("settings.general.personalizationDesc")}</p>
        </div>
        {userPromptQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{t("settings.general.loadFailed")}</AlertDescription>
          </Alert>
        ) : null}
        <div className="grid gap-2">
          <div className="text-xs text-muted-foreground">{userPromptQuery.data?.path || "<home>/pudding.md"}</div>
          <Textarea
            className="min-h-48 resize-y font-mono text-sm leading-6"
            disabled={userPromptQuery.isLoading}
            placeholder={t("settings.general.personalizationPlaceholder")}
            value={promptContent}
            onChange={(event) => {
              setPromptContent(event.target.value);
              setPromptEdited(true);
            }}
          />
          <div className="flex justify-end">
            <Button
              disabled={userPromptQuery.isLoading || promptMutation.isPending || !promptDirty}
              size="sm"
              type="button"
              onClick={() => promptMutation.mutate()}
            >
              {promptMutation.isPending ? <Spinner /> : null}
              {t("common.save")}
            </Button>
          </div>
        </div>
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.general.context")}</h3>
        </div>
        {settingsQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{t("settings.general.loadFailed")}</AlertDescription>
          </Alert>
        ) : null}
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <SettingsNumberField
            description={t("settings.general.tailTurnsDesc")}
            disabled={settingsDisabled}
            id="pudding-compact-tail-turns"
            label={t("settings.general.tailTurns")}
            max={50}
            min={1}
            value={tailTurns}
            onBlur={() => saveNumberSetting(SETTINGS_KEYS.compactTailInputTurns, tailTurns, setTailTurns, 1, 50)}
            onChange={setTailTurns}
          />
          <SettingsNumberField
            description={t("settings.general.autoThresholdDesc")}
            disabled={settingsDisabled}
            id="pudding-auto-compact-threshold"
            label={t("settings.general.autoThreshold")}
            max={100}
            min={0}
            suffix="%"
            value={autoThreshold}
            onBlur={() =>
              saveNumberSetting(SETTINGS_KEYS.compactAutoThresholdPercent, autoThreshold, setAutoThreshold, 0, 100)
            }
            onChange={setAutoThreshold}
          />
          <SettingsToggleRow
            checked={showCompactSummary}
            description={t("settings.general.showCompactSummaryDesc")}
            disabled={settingsDisabled}
            id="pudding-show-compact-summary"
            label={t("settings.general.showCompactSummary")}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showCompactSummary, next, setShowCompactSummary)}
          />
          <SettingsToggleRow
            checked={showReasoning}
            description={t("settings.general.showReasoningDesc")}
            disabled={settingsDisabled}
            id="pudding-show-reasoning"
            label={t("settings.general.showReasoning")}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showReasoning, next, setShowReasoning)}
          />
          <SettingsToggleRow
            checked={showRawToolInfo}
            description={t("settings.general.showRawToolInfoDesc")}
            disabled={settingsDisabled}
            id="pudding-show-raw-tool-info"
            label={t("settings.general.showRawToolInfo")}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showRawToolInfo, next, setShowRawToolInfo)}
          />
        </div>
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.general.developer")}</h3>
        </div>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <SettingsToggleRow
            checked={desktopUpdateState?.receivePreviewUpdates === true}
            description={t("settings.general.receivePreviewUpdatesDesc")}
            disabled={
              !desktopUpdateState ||
              desktopUpdateState.status === "unavailable" ||
              previewUpdateBusy ||
              previewUpdatesMutation.isPending
            }
            id="pudding-receive-preview-updates"
            label={t("settings.general.receivePreviewUpdates")}
            onChange={(next) => previewUpdatesMutation.mutate(next)}
          />
          <SettingsToggleRow
            checked={showPreviewAppVersions}
            description={t("settings.general.showPreviewAppVersionsDesc")}
            disabled={settingsDisabled}
            id="pudding-show-preview-app-versions"
            label={t("settings.general.showPreviewAppVersions")}
            onChange={(next) =>
              saveBooleanSetting(SETTINGS_KEYS.showAppPreviewVersions, next, setShowPreviewAppVersions)
            }
          />
        </div>
      </section>

      <div className="overflow-hidden rounded-xl border bg-card">
        <SettingsActionRow
          description={t("settings.general.resetDefaultsDesc")}
          label={t("settings.general.resetDefaults")}
        >
          <Button
            disabled={settingsDisabled || previewUpdateBusy || previewUpdatesMutation.isPending}
            size="sm"
            type="button"
            variant="outline"
            onClick={() => setResetOpen(true)}
          >
            <RotateCcw />
            {t("settings.resetDefaults")}
          </Button>
        </SettingsActionRow>
      </div>

      <AlertDialog
        open={resetOpen}
        onOpenChange={(open) => {
          if (!resetMutation.isPending) {
            setResetOpen(open);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("settings.general.resetDefaultsTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("settings.general.resetDefaultsConfirm")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={settingsMutation.isPending || resetMutation.isPending || previewUpdateBusy}
              variant="destructive"
              onClick={(event) => {
                event.preventDefault();
                resetMutation.mutate();
              }}
            >
              {resetMutation.isPending ? <Spinner /> : null}
              {t("settings.resetDefaults")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
