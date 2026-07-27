import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw } from "@/components/icons";
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
} from "@/components/ConfirmationDialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
  SETTINGS_CARD_CLASS,
  SETTINGS_GROUP_CLASS,
  SETTINGS_SECTION_HEADING_CLASS,
  SettingsActionRow,
  SettingsControlRow,
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
  const [editorFontFamily, setEditorFontFamily] = useState("");
  const [editorFontSize, setEditorFontSize] = useState("12");
  const [editorLineHeight, setEditorLineHeight] = useState("20");
  const [pendingSettingSaveCount, setPendingSettingSaveCount] = useState(0);
  const [pendingSettingCounts, setPendingSettingCounts] = useState<Record<string, number>>({});
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
    if (!settingsQuery.isSuccess || pendingSettingSaveCount > 0) {
      return;
    }
    setTailTurns(savedSettings[SETTINGS_KEYS.compactTailInputTurns]);
    setAutoThreshold(savedSettings[SETTINGS_KEYS.compactAutoThresholdPercent]);
    setShowCompactSummary(savedSettings[SETTINGS_KEYS.showCompactSummary] !== "false");
    setShowReasoning(savedSettings[SETTINGS_KEYS.showReasoning] !== "false");
    setShowRawToolInfo(savedSettings[SETTINGS_KEYS.showRawToolInfo] !== "false");
    setShowPreviewAppVersions(savedSettings[SETTINGS_KEYS.showAppPreviewVersions] === "true");
    setEditorFontFamily(savedSettings[SETTINGS_KEYS.editorFontFamily]);
    setEditorFontSize(savedSettings[SETTINGS_KEYS.editorFontSize]);
    setEditorLineHeight(savedSettings[SETTINGS_KEYS.editorLineHeight]);
  }, [pendingSettingSaveCount, savedSettings, settingsQuery.isSuccess]);

  const promptMutation = useMutation({
    mutationFn: () => putUserPrompt(token, promptContent),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.userPrompt() });
      toast.success(t("settings.general.personalizationSaved"));
    },
    onError: () => toast.error(t("settings.general.saveFailed")),
  });

  const settingsMutation = useMutation({
    scope: { id: "general-settings" },
    mutationFn: (settings: Record<string, string>) => putSettings(token, settings),
    onSuccess: (_, settings) => {
      queryClient.setQueryData<{ settings: Record<string, string> }>(queryKeys.settings(), (current) => ({
        settings: { ...(current?.settings || {}), ...settings },
      }));
    },
    onError: async () => {
      toast.error(t("settings.general.saveFailed"));
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings() });
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

  const updatePendingSettingCounts = (keys: string[], delta: number) => {
    setPendingSettingCounts((current) => {
      const next = { ...current };
      keys.forEach((key) => {
        const count = Math.max(0, (next[key] || 0) + delta);
        if (count > 0) {
          next[key] = count;
        } else {
          delete next[key];
        }
      });
      return next;
    });
  };

  const saveSettingsPatch = (settings: Record<string, string>) => {
    const keys = Object.keys(settings);
    setPendingSettingSaveCount((count) => count + 1);
    updatePendingSettingCounts(keys, 1);
    settingsMutation.mutate(settings, {
      onSettled: () => {
        setPendingSettingSaveCount((count) => Math.max(0, count - 1));
        updatePendingSettingCounts(keys, -1);
      },
    });
  };

  const saveBooleanSetting = (key: string, next: boolean, setValue: (value: boolean) => void) => {
    setValue(next);
    saveSettingsPatch({ [key]: String(next) });
  };

  const saveTextSetting = (key: string, value: string, setValue: (value: string) => void) => {
    const fallback = savedSettings[key];
    const normalized = value.trim();
    if (!normalized || normalized.length > 256 || /[\r\n\0]/.test(normalized)) {
      setValue(fallback);
      toast.error(t("settings.general.saveFailed"));
      return;
    }
    setValue(normalized);
    if (normalized !== fallback) {
      saveSettingsPatch({ [key]: normalized });
    }
  };

  const saveNumberSetting = (
    key: string,
    value: string,
    setValue: (value: string) => void,
    min: number,
    max: number,
    validate?: (value: number) => boolean,
  ) => {
    const fallback = savedSettings[key];
    const raw = value.trim();
    if (!raw) {
      setValue(fallback);
      return;
    }
    const parsed = Number(raw);
    if (!Number.isInteger(parsed) || parsed < min || parsed > max || (validate && !validate(parsed))) {
      setValue(fallback);
      toast.error(t("settings.general.saveFailed"));
      return;
    }
    const normalized = String(parsed);
    setValue(normalized);
    if (normalized !== savedSettings[key]) {
      saveSettingsPatch({ [key]: normalized });
    }
  };

  const promptDirty = promptEdited && promptContent !== savedPrompt;
  const settingsDisabled = settingsQuery.isLoading || resetMutation.isPending;

  useEffect(() => {
    onDirtyChange(promptDirty);
  }, [onDirtyChange, promptDirty]);

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-6")}>
      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.general.personalization")}</h3>
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
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.general.editor")}</h3>
        </div>
        <div className={SETTINGS_GROUP_CLASS}>
          <SettingsControlRow
            description={t("settings.general.editorFontFamilyDesc")}
            disabled={settingsDisabled}
            id="pudding-editor-font-family"
            label={t("settings.general.editorFontFamily")}
          >
            <Input
              className="w-full font-mono text-xs sm:w-72"
              disabled={settingsDisabled}
              id="pudding-editor-font-family"
              maxLength={256}
              spellCheck={false}
              value={editorFontFamily}
              onBlur={() => saveTextSetting(SETTINGS_KEYS.editorFontFamily, editorFontFamily, setEditorFontFamily)}
              onChange={(event) => setEditorFontFamily(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") event.currentTarget.blur();
              }}
            />
          </SettingsControlRow>
          <SettingsNumberField
            description={t("settings.general.editorFontSizeDesc")}
            disabled={settingsDisabled}
            id="pudding-editor-font-size"
            label={t("settings.general.editorFontSize")}
            max={24}
            min={10}
            suffix="px"
            value={editorFontSize}
            onBlur={() => saveNumberSetting(SETTINGS_KEYS.editorFontSize, editorFontSize, setEditorFontSize, 10, 24)}
            onChange={setEditorFontSize}
          />
          <SettingsNumberField
            description={t("settings.general.editorLineHeightDesc")}
            disabled={settingsDisabled}
            id="pudding-editor-line-height"
            label={t("settings.general.editorLineHeight")}
            max={40}
            min={0}
            suffix="px"
            value={editorLineHeight}
            onBlur={() =>
              saveNumberSetting(
                SETTINGS_KEYS.editorLineHeight,
                editorLineHeight,
                setEditorLineHeight,
                0,
                40,
                (value) => value === 0 || value >= 12,
              )
            }
            onChange={setEditorLineHeight}
          />
        </div>
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.general.context")}</h3>
        </div>
        {settingsQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{t("settings.general.loadFailed")}</AlertDescription>
          </Alert>
        ) : null}
        <div className={SETTINGS_GROUP_CLASS}>
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
            pending={Boolean(pendingSettingCounts[SETTINGS_KEYS.showCompactSummary])}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showCompactSummary, next, setShowCompactSummary)}
          />
          <SettingsToggleRow
            checked={showReasoning}
            description={t("settings.general.showReasoningDesc")}
            disabled={settingsDisabled}
            id="pudding-show-reasoning"
            label={t("settings.general.showReasoning")}
            pending={Boolean(pendingSettingCounts[SETTINGS_KEYS.showReasoning])}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showReasoning, next, setShowReasoning)}
          />
          <SettingsToggleRow
            checked={showRawToolInfo}
            description={t("settings.general.showRawToolInfoDesc")}
            disabled={settingsDisabled}
            id="pudding-show-raw-tool-info"
            label={t("settings.general.showRawToolInfo")}
            pending={Boolean(pendingSettingCounts[SETTINGS_KEYS.showRawToolInfo])}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showRawToolInfo, next, setShowRawToolInfo)}
          />
        </div>
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.general.developer")}</h3>
        </div>
        <div className={SETTINGS_GROUP_CLASS}>
          <SettingsToggleRow
            checked={desktopUpdateState?.receivePreviewUpdates === true}
            description={t("settings.general.receivePreviewUpdatesDesc")}
            disabled={
              !desktopUpdateState ||
              desktopUpdateState.status === "unavailable" ||
              previewUpdateBusy
            }
            id="pudding-receive-preview-updates"
            label={t("settings.general.receivePreviewUpdates")}
            pending={previewUpdatesMutation.isPending}
            onChange={(next) => previewUpdatesMutation.mutate(next)}
          />
          <SettingsToggleRow
            checked={showPreviewAppVersions}
            description={t("settings.general.showPreviewAppVersionsDesc")}
            disabled={settingsDisabled}
            id="pudding-show-preview-app-versions"
            label={t("settings.general.showPreviewAppVersions")}
            pending={Boolean(pendingSettingCounts[SETTINGS_KEYS.showAppPreviewVersions])}
            onChange={(next) =>
              saveBooleanSetting(SETTINGS_KEYS.showAppPreviewVersions, next, setShowPreviewAppVersions)
            }
          />
        </div>
      </section>

      <div className={SETTINGS_CARD_CLASS}>
        <SettingsActionRow
          description={t("settings.general.resetDefaultsDesc")}
          label={t("settings.general.resetDefaults")}
        >
          <Button
            disabled={settingsDisabled || settingsMutation.isPending || previewUpdateBusy || previewUpdatesMutation.isPending}
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
