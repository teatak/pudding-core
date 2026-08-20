import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, RefreshCw, RotateCcw, Trash } from "@/components/icons";
import { useEffect, useState } from "react";

import {
  clearASRRecordings,
  getAudioConfig,
  getDesktopAbout,
  listSessions,
  putAudioConfig,
  resetAudioConfig,
  type AudioConfig,
  type DesktopAboutSection,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { DialogSelectContent } from "@/components/DialogSelectContent";
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
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Select, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { toast } from "sonner";

import {
  SETTINGS_COMPACT_SELECT_CLASS,
  SETTINGS_CARD_CLASS,
  SETTINGS_GROUP_CLASS,
  SETTINGS_NARROW_CONTENT_CLASS,
  SETTINGS_SECTION_HEADING_CLASS,
  SettingsActionRow,
  SettingsControlRow,
  SettingsNumberField,
  SettingsToggleRow,
} from "./shared";

type VoiceFormState = {
  asrEnabled: boolean;
  asrSaveAudio: boolean;
  asrLanguage: string;
  asrNumThreads: string;
  asrUseITN: boolean;
  vadMinSilenceMillis: string;
  vadMinSpeechMillis: string;
  vadPrerollMillis: string;
  vadThreshold: string;
  vadMinEnergy: string;
  vadPlaybackMinEnergy: string;
  aecEnabled: boolean;
  nsEnabled: boolean;
  nsLevel: string;
  ttsEnabled: boolean;
  ttsSpeed: string;
  ttsVoice: string;
};

type VoiceToggleKey = "asrEnabled" | "asrSaveAudio" | "asrUseITN" | "aecEnabled" | "nsEnabled" | "ttsEnabled";

const VOICE_LANGUAGE_OPTIONS = ["zh", "en", "ja", "ko", "yue", "auto"];
const VOICE_NS_LEVEL_OPTIONS = ["low", "moderate", "high", "very_high"];

export function VoiceSettings({ token, view = "general" }: { token: string; view?: "general" | "advanced" }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const initialConfig = queryClient.getQueryData<{ config: AudioConfig }>(queryKeys.audioConfig())?.config;
  const [form, setForm] = useState<VoiceFormState>(() => initialConfig ? voiceFormFromConfig(initialConfig) : defaultVoiceForm());
  const [formHydrated, setFormHydrated] = useState(Boolean(initialConfig));
  const [pendingSaveCount, setPendingSaveCount] = useState(0);
  const [clearRecordingsOpen, setClearRecordingsOpen] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const audioQuery = useQuery({
    queryKey: queryKeys.audioConfig(),
    queryFn: () => getAudioConfig(token),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const aboutQuery = useQuery({
    queryKey: queryKeys.desktopAbout(),
    queryFn: () => getDesktopAbout(token),
    enabled: Boolean(token) && view === "advanced",
    refetchOnMount: "always",
  });
  const savedConfig = audioQuery.data?.config;
  const aboutSections = aboutQuery.data?.sections || [];
  const ttsReadOnlyRows = voiceSectionReadOnlyRows(aboutSections, "tts", ["backend"]);
  const runtimeReadOnlyRows = [
    ...voiceRuntimeReadOnlyRows(aboutSections, audioQuery.data?.path || "-", savedConfig?.driver.type || "-"),
    ...ttsReadOnlyRows,
  ];
  const asrReadOnlyRows = voiceSectionReadOnlyRows(aboutSections, "asr", ["engine", "model_path", "tokens_path", "provider"]);
  const vadReadOnlyRows = voiceSectionReadOnlyRows(aboutSections, "asr_vad", ["model_path", "window_size"]);
  const dspReadOnlyRows = [
    ...voiceSectionReadOnlyRows(aboutSections, "aec", ["model"], "aec"),
    ...voiceSectionReadOnlyRows(aboutSections, "ns", ["model"], "ns"),
  ];

  useEffect(() => {
    if (savedConfig && pendingSaveCount === 0) {
      setForm(voiceFormFromConfig(savedConfig));
      setFormHydrated(true);
    }
  }, [pendingSaveCount, savedConfig]);

  const saveMutation = useMutation({
    scope: { id: "voice-config" },
    mutationFn: (nextForm: VoiceFormState) => {
      if (!savedConfig) {
        throw new Error("audio config missing");
      }
      return putAudioConfig(token, audioConfigFromForm(savedConfig, nextForm));
    },
    onSuccess: (response) => {
      queryClient.setQueryData(queryKeys.audioConfig(), response);
      void queryClient.invalidateQueries({ queryKey: queryKeys.desktopAbout() });
    },
    onError: () => {
      toast.error(t("settings.voice.saveFailed"));
      void queryClient.invalidateQueries({ queryKey: queryKeys.audioConfig() });
    },
  });

  const resetMutation = useMutation({
    mutationFn: () => resetAudioConfig(token),
    onSuccess: async (response) => {
      setResetOpen(false);
      queryClient.setQueryData(queryKeys.audioConfig(), response);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.audioConfig() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.desktopAbout() }),
      ]);
      toast.success(t("settings.voice.resetDefaultsSuccess"));
    },
    onError: () => toast.error(t("settings.voice.resetDefaultsFailed")),
  });

  const clearRecordingsMutation = useMutation({
    mutationFn: async () => {
      const { sessions } = await listSessions(token);
      const results = await Promise.allSettled(sessions.map((session) => clearASRRecordings(token, session.id)));
      const aggregate = {
        attachments: 0,
        messages: 0,
        queuedInputs: 0,
        deleteErrors: 0,
        failedSessions: 0,
      };
      for (const result of results) {
        if (result.status === "rejected") {
          aggregate.failedSessions++;
          continue;
        }
        aggregate.attachments += result.value.attachments;
        aggregate.messages += result.value.messages;
        aggregate.queuedInputs += result.value.queuedInputs;
        aggregate.deleteErrors += result.value.deleteErrors ?? 0;
      }
      return aggregate;
    },
    onSuccess: async (response) => {
      setClearRecordingsOpen(false);
      if (response.failedSessions > 0 || response.deleteErrors > 0) {
        toast.warning(
          t("settings.voice.asrClearAudioPartial")
            .replace("{count}", String(response.attachments))
            .replace("{sessions}", String(response.failedSessions))
            .replace("{files}", String(response.deleteErrors)),
        );
      } else {
        toast.success(t("settings.voice.asrClearAudioSuccess").replace("{count}", String(response.attachments)));
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["session"] }),
        queryClient.invalidateQueries({ queryKey: queryKeys.desktopAbout() }),
      ]);
    },
    onError: () => toast.error(t("settings.voice.asrClearAudioFailed")),
  });

  const saveVoiceForm = (nextForm: VoiceFormState) => {
    setForm(nextForm);
    if (!savedConfig) {
      return;
    }
    setPendingSaveCount((count) => count + 1);
    saveMutation.mutate(nextForm, {
      onSettled: () => setPendingSaveCount((count) => Math.max(0, count - 1)),
    });
  };
  const saveVoicePatch = (patch: Partial<VoiceFormState>) => saveVoiceForm({ ...form, ...patch });
  const saveVoiceToggle = (key: VoiceToggleKey, checked: boolean) => {
    saveVoiceForm({ ...form, [key]: checked });
  };
  const saveCurrentVoiceForm = () => {
    if (!savedConfig) {
      return;
    }
    saveVoiceForm(form);
  };
  const saving = pendingSaveCount > 0;
  const disabled = audioQuery.isLoading || audioQuery.isError || resetMutation.isPending;
  const clearDisabled = disabled || clearRecordingsMutation.isPending;
  const resetDisabled = disabled || saving || clearRecordingsMutation.isPending;
  const edge = savedConfig ? edgeTTSProfile(savedConfig) : {};
  const audioLoadError = audioQuery.isError ? (
    <Alert variant="destructive">
      <AlertDescription className="flex items-center justify-between gap-3">
        <span>{t("settings.voice.loadFailed")}</span>
        <Button
          disabled={audioQuery.isFetching}
          size="sm"
          type="button"
          variant="outline"
          onClick={() => void audioQuery.refetch()}
        >
          {audioQuery.isFetching ? <Spinner /> : <RefreshCw />}
          {t("common.refresh")}
        </Button>
      </AlertDescription>
    </Alert>
  ) : null;

  if (!formHydrated) {
    return (
      <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-6")}>
        {audioLoadError || (
          <div className="flex min-h-64 items-center justify-center">
            <Spinner className="size-5 text-muted-foreground" />
          </div>
        )}
      </div>
    );
  }

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-6")}>
      {audioLoadError}
      {view === "general" ? (
        <Alert>
          <AlertDescription>{t("settings.voice.restartRequired")}</AlertDescription>
        </Alert>
      ) : null}

      {view === "advanced" ? <Collapsible className={SETTINGS_CARD_CLASS} open={runtimeOpen} onOpenChange={setRuntimeOpen}>
        <CollapsibleTrigger className="flex w-full items-center justify-between gap-4 px-3 py-3 text-left hover:bg-control-hover active:bg-control-active">
          <span className="grid min-w-0 gap-0.5">
            <span className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.voice.runtime")}</span>
            <span className="text-xs leading-5 text-muted-foreground">{t("settings.voice.restartRequired")}</span>
          </span>
          <ChevronRight className={cn("size-4 shrink-0 text-muted-foreground transition-transform", runtimeOpen && "rotate-90")} />
        </CollapsibleTrigger>
        <CollapsibleContent className="border-t border-border/70 p-3">
          {audioQuery.isLoading ? (
            <div className="grid gap-2">
              <Skeleton className="h-4 w-2/3" />
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-4 w-3/5" />
            </div>
          ) : (
            <dl className="grid gap-2 text-sm">
              {runtimeReadOnlyRows.map((row) => (
                <SettingsInfoRow key={row.id} label={row.label} value={row.value} />
              ))}
            </dl>
          )}
        </CollapsibleContent>
      </Collapsible> : null}

      <section className="grid gap-3">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.voice.asr")}</h3>
        </div>
        <div className={SETTINGS_GROUP_CLASS}>
          {view === "general" ? <>
          <SettingsToggleRow
            checked={form.asrEnabled}
            description={t("settings.voice.asrEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-asr-enabled"
            label={t("settings.voice.asrEnabled")}
            onChange={(next) => saveVoiceToggle("asrEnabled", next)}
          />
          <SettingsToggleRow
            checked={form.asrSaveAudio}
            description={t("settings.voice.asrSaveAudioDesc")}
            disabled={disabled}
            id="pudding-voice-asr-save-audio"
            label={t("settings.voice.asrSaveAudio")}
            onChange={(next) => saveVoiceToggle("asrSaveAudio", next)}
          />
          <SettingsActionRow description={t("settings.voice.asrClearAudioDesc")} label={t("settings.voice.asrClearAudio")}>
            <Button disabled={clearDisabled} size="sm" type="button" variant="outline" onClick={() => setClearRecordingsOpen(true)}>
              {clearRecordingsMutation.isPending ? <Spinner className="size-4" /> : <Trash className="size-4" />}
              {t("common.clear")}
            </Button>
          </SettingsActionRow>
          <SettingsToggleRow
            checked={form.asrUseITN}
            description={t("settings.voice.asrUseITNDesc")}
            disabled={disabled}
            id="pudding-voice-asr-itn"
            label={t("settings.voice.asrUseITN")}
            onChange={(next) => saveVoiceToggle("asrUseITN", next)}
          />
          <SettingsControlRow
            description={t("settings.voice.asrLanguageDesc")}
            disabled={disabled}
            id="pudding-voice-asr-language"
            label={t("settings.voice.asrLanguage")}
          >
            <Select
              disabled={disabled}
              value={form.asrLanguage}
              onValueChange={(value) => saveVoicePatch({ asrLanguage: value })}
            >
              <SelectTrigger id="pudding-voice-asr-language" className={SETTINGS_COMPACT_SELECT_CLASS}>
                <SelectValue />
              </SelectTrigger>
              <DialogSelectContent>
                {VOICE_LANGUAGE_OPTIONS.map((language) => (
                  <SelectItem key={language} value={language}>
                    {language}
                  </SelectItem>
                ))}
              </DialogSelectContent>
            </Select>
          </SettingsControlRow>
          </> : null}
          {view === "advanced" ? <SettingsNumberField
            description={t("settings.voice.asrNumThreadsDesc")}
            disabled={disabled}
            id="pudding-voice-asr-threads"
            label={t("settings.voice.asrNumThreads")}
            max={8}
            min={1}
            value={form.asrNumThreads}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, asrNumThreads: value }))}
          /> : null}
          {view === "advanced" ? <SettingsReadOnlyRows rows={asrReadOnlyRows} /> : null}
        </div>
      </section>

      {view === "advanced" ? <section className="grid gap-3">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.voice.vad")}</h3>
        </div>
        <div className={SETTINGS_GROUP_CLASS}>
          <SettingsNumberField
            description={t("settings.voice.vadPrerollDesc")}
            disabled={disabled}
            id="pudding-voice-vad-preroll"
            label={t("settings.voice.vadPreroll")}
            max={2000}
            min={100}
            value={form.vadPrerollMillis}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, vadPrerollMillis: value }))}
          />
          <SettingsNumberField
            description={t("settings.voice.vadThresholdDesc")}
            disabled={disabled}
            id="pudding-voice-vad-threshold"
            label={t("settings.voice.vadThreshold")}
            max={0.99}
            min={0.01}
            step={0.01}
            value={form.vadThreshold}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, vadThreshold: value }))}
          />
          <SettingsNumberField
            description={t("settings.voice.vadMinEnergyDesc")}
            disabled={disabled}
            id="pudding-voice-vad-min-energy"
            label={t("settings.voice.vadMinEnergy")}
            max={1}
            min={0.001}
            step={0.001}
            value={form.vadMinEnergy}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, vadMinEnergy: value }))}
          />
          <SettingsNumberField
            description={t("settings.voice.vadPlaybackMinEnergyDesc")}
            disabled={disabled}
            id="pudding-voice-vad-playback-min-energy"
            label={t("settings.voice.vadPlaybackMinEnergy")}
            max={1}
            min={0.001}
            step={0.001}
            value={form.vadPlaybackMinEnergy}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, vadPlaybackMinEnergy: value }))}
          />
          <SettingsNumberField
            description={t("settings.voice.vadMinSilenceDesc")}
            disabled={disabled}
            id="pudding-voice-vad-min-silence"
            label={t("settings.voice.vadMinSilence")}
            max={5000}
            min={100}
            value={form.vadMinSilenceMillis}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, vadMinSilenceMillis: value }))}
          />
          <SettingsNumberField
            description={t("settings.voice.vadMinSpeechDesc")}
            disabled={disabled}
            id="pudding-voice-vad-min-speech"
            label={t("settings.voice.vadMinSpeech")}
            max={5000}
            min={100}
            value={form.vadMinSpeechMillis}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, vadMinSpeechMillis: value }))}
          />
          <SettingsReadOnlyRows rows={vadReadOnlyRows} />
        </div>
      </section> : null}

      {view === "advanced" ? <section className="grid gap-3">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.voice.dsp")}</h3>
        </div>
        <div className={SETTINGS_GROUP_CLASS}>
          <SettingsToggleRow
            checked={form.aecEnabled}
            description={t("settings.voice.aecEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-aec-enabled"
            label={t("settings.voice.aecEnabled")}
            onChange={(next) => saveVoiceToggle("aecEnabled", next)}
          />
          <SettingsToggleRow
            checked={form.nsEnabled}
            description={t("settings.voice.nsEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-ns-enabled"
            label={t("settings.voice.nsEnabled")}
            onChange={(next) => saveVoiceToggle("nsEnabled", next)}
          />
          <SettingsControlRow
            description={t("settings.voice.nsLevelDesc")}
            disabled={disabled || !form.nsEnabled}
            id="pudding-voice-ns-level"
            label={t("settings.voice.nsLevel")}
          >
            <Select
              disabled={disabled || !form.nsEnabled}
              value={form.nsLevel}
              onValueChange={(value) => saveVoicePatch({ nsLevel: value })}
            >
              <SelectTrigger id="pudding-voice-ns-level" className={SETTINGS_COMPACT_SELECT_CLASS}>
                <SelectValue />
              </SelectTrigger>
              <DialogSelectContent>
                {VOICE_NS_LEVEL_OPTIONS.map((level) => (
                  <SelectItem key={level} value={level}>
                    {t(`settings.voice.nsLevel.${level}`)}
                  </SelectItem>
                ))}
              </DialogSelectContent>
            </Select>
          </SettingsControlRow>
          <SettingsReadOnlyRows rows={dspReadOnlyRows} />
        </div>
      </section> : null}

      {view === "general" ? <section className="grid gap-3">
        <div className="grid gap-2">
          <h3 className={SETTINGS_SECTION_HEADING_CLASS}>{t("settings.voice.tts")}</h3>
        </div>
        <div className={SETTINGS_GROUP_CLASS}>
          <SettingsToggleRow
            checked={form.ttsEnabled}
            description={t("settings.voice.ttsEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-tts-enabled"
            label={t("settings.voice.ttsEnabled")}
            onChange={(next) => saveVoiceToggle("ttsEnabled", next)}
          />
          <SettingsControlRow
            description={t("settings.voice.ttsVoiceDesc")}
            disabled={disabled}
            id="pudding-voice-tts-voice"
            label={t("settings.voice.ttsVoice")}
          >
            <Input
              className="w-48 max-w-full sm:ml-auto"
              disabled={disabled}
              id="pudding-voice-tts-voice"
              placeholder={edge.voice || "zh-CN-YunxiaNeural"}
              value={form.ttsVoice}
              onBlur={saveCurrentVoiceForm}
              onChange={(event) => setForm((prev) => ({ ...prev, ttsVoice: event.target.value }))}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.currentTarget.blur();
                }
              }}
            />
          </SettingsControlRow>
          <SettingsNumberField
            description={t("settings.voice.ttsSpeedDesc")}
            disabled={disabled}
            id="pudding-voice-tts-speed"
            label={t("settings.voice.ttsSpeed")}
            max={2}
            min={0.5}
            step={0.05}
            value={form.ttsSpeed}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, ttsSpeed: value }))}
          />
        </div>
      </section> : null}
      {view === "advanced" ? <div className={SETTINGS_CARD_CLASS}>
        <SettingsActionRow
          description={t("settings.voice.resetDefaultsDesc")}
          label={t("settings.voice.resetDefaults")}
        >
          <Button
            disabled={resetDisabled}
            size="sm"
            type="button"
            variant="outline"
            onClick={() => setResetOpen(true)}
          >
            <RotateCcw />
            {t("settings.resetDefaults")}
          </Button>
        </SettingsActionRow>
      </div> : null}
      {view === "general" ? <AlertDialog open={clearRecordingsOpen} onOpenChange={(open) => !open && setClearRecordingsOpen(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("settings.voice.asrClearAudioTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("settings.voice.asrClearAudioConfirm")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={clearRecordingsMutation.isPending}
              variant="destructive"
              onClick={() => clearRecordingsMutation.mutate()}
            >
              {t("common.clear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog> : null}
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
            <AlertDialogTitle>{t("settings.voice.resetDefaultsTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("settings.voice.resetDefaultsConfirm")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={saving || resetMutation.isPending}
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

type SettingsReadOnlyRow = {
  id: string;
  label: string;
  value: string;
};

function SettingsReadOnlyRows({ rows }: { rows: SettingsReadOnlyRow[] }) {
  const { t } = useI18n();
  if (rows.length === 0) {
    return null;
  }
  return (
    <>
      {rows.map((row) => (
        <div
          key={row.id}
          className="grid gap-3 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,18rem)] sm:items-center"
        >
          <span className="text-sm font-medium">{voiceReadOnlyLabel(row.label, t)}</span>
          <span className="min-w-0 break-words text-sm text-foreground sm:text-right">{row.value}</span>
        </div>
      ))}
    </>
  );
}

function SettingsInfoRow({ label, value }: { label: string; value: string }) {
  const { t } = useI18n();
  return (
    <div className="grid min-w-0 grid-cols-[minmax(7rem,12rem)_minmax(0,1fr)] gap-3">
      <dt className="min-w-0 truncate text-muted-foreground">{voiceReadOnlyLabel(label, t)}</dt>
      <dd className="min-w-0 break-words text-foreground">{value}</dd>
    </div>
  );
}

function voiceReadOnlyLabel(label: string, t: (key: string) => string) {
  const key = `settings.voice.readOnly.${label}`;
  const translated = t(key);
  return translated === key ? label : translated;
}

function voiceRuntimeReadOnlyRows(sections: DesktopAboutSection[], path: string, driver: string): SettingsReadOnlyRow[] {
  const rows: SettingsReadOnlyRow[] = [
    { id: "path", label: "path", value: path },
    { id: "driver", label: "driver", value: driver },
  ];
  rows.push(...voiceSectionReadOnlyRows(sections, "driver", ["capture_sample_rate", "playback_sample_rate", "channels", "period_millis"]));
  rows.push(...voiceSectionReadOnlyRows(sections, "health", ["capture", "playback"]));
  rows.push(...voiceSectionReadOnlyRows(sections, "audio_bindings", ["input_owner", "input_mode", "output_owner"]));
  return rows;
}

function voiceSectionReadOnlyRows(
  sections: DesktopAboutSection[],
  sectionID: string,
  keys: string[],
  labelPrefix = "",
): SettingsReadOnlyRow[] {
  const section = sections.find((item) => item.id === sectionID);
  if (!section) {
    return [];
  }
  return keys
    .map((key) => {
      const row = section.rows.find((item) => item.key === key);
      if (!row) {
        return null;
      }
      const label = labelPrefix ? `${labelPrefix}.${row.key}` : row.key;
      return {
        id: `${sectionID}.${row.key}`,
        label,
        value: row.value || "-",
      };
    })
    .filter((row): row is SettingsReadOnlyRow => row !== null);
}

function defaultVoiceForm(): VoiceFormState {
  return {
    asrEnabled: true,
    asrSaveAudio: false,
    asrLanguage: "zh",
    asrNumThreads: "2",
    asrUseITN: false,
    vadMinSilenceMillis: "400",
    vadMinSpeechMillis: "300",
    vadPrerollMillis: "500",
    vadThreshold: "0.6",
    vadMinEnergy: "0.01",
    vadPlaybackMinEnergy: "0.015",
    aecEnabled: true,
    nsEnabled: true,
    nsLevel: "moderate",
    ttsEnabled: true,
    ttsSpeed: "1.2",
    ttsVoice: "zh-CN-YunxiaNeural",
  };
}

function voiceFormFromConfig(config: AudioConfig): VoiceFormState {
  const edge = edgeTTSProfile(config);
  return {
    asrEnabled: config.asr.enabled ?? true,
    asrSaveAudio: config.asr.saveAudio ?? false,
    asrLanguage: config.asr.language || "zh",
    asrNumThreads: String(config.asr.numThreads || 2),
    asrUseITN: config.asr.useITN ?? false,
    vadMinSilenceMillis: String(config.asr.vad.minSilenceMillis || 400),
    vadMinSpeechMillis: String(config.asr.vad.minSpeechMillis || 300),
    vadPrerollMillis: String(config.asr.vad.prerollMillis || 500),
    vadThreshold: String(config.asr.vad.threshold || 0.6),
    vadMinEnergy: String(config.asr.vad.minEnergy || 0.01),
    vadPlaybackMinEnergy: String(config.asr.vad.playbackMinEnergy || 0.015),
    aecEnabled: config.aec.enabled ?? true,
    nsEnabled: config.ns.enabled ?? true,
    nsLevel: config.ns.level || "moderate",
    ttsEnabled: config.tts.enabled ?? true,
    ttsSpeed: String(edge.speed || 1.2),
    ttsVoice: edge.voice || "zh-CN-YunxiaNeural",
  };
}

function audioConfigFromForm(config: AudioConfig, form: VoiceFormState): AudioConfig {
  const edge = edgeTTSProfile(config);
  return {
    ...config,
    asr: {
      ...config.asr,
      enabled: form.asrEnabled,
      saveAudio: form.asrSaveAudio,
      language: form.asrLanguage,
      numThreads: normalizedInteger(form.asrNumThreads, config.asr.numThreads),
      useITN: form.asrUseITN,
      vad: {
        ...config.asr.vad,
        threshold: normalizedNumber(form.vadThreshold, config.asr.vad.threshold),
        minEnergy: normalizedNumber(form.vadMinEnergy, config.asr.vad.minEnergy),
        playbackMinEnergy: normalizedNumber(form.vadPlaybackMinEnergy, config.asr.vad.playbackMinEnergy),
        minSilenceMillis: normalizedInteger(form.vadMinSilenceMillis, config.asr.vad.minSilenceMillis),
        minSpeechMillis: normalizedInteger(form.vadMinSpeechMillis, config.asr.vad.minSpeechMillis),
        prerollMillis: normalizedInteger(form.vadPrerollMillis, config.asr.vad.prerollMillis),
      },
    },
    aec: {
      ...config.aec,
      enabled: form.aecEnabled,
      model: "webrtc",
    },
    ns: {
      ...config.ns,
      enabled: form.nsEnabled,
      model: "webrtc",
      level: form.nsLevel,
    },
    tts: {
      ...config.tts,
      enabled: form.ttsEnabled,
      profile: "edge",
      profiles: {
        ...config.tts.profiles,
        edge: {
          ...edge,
          voice: form.ttsVoice.trim() || edge.voice || "zh-CN-YunxiaNeural",
          speed: normalizedNumber(form.ttsSpeed, edge.speed || 1.2),
        },
      },
    },
  };
}

function edgeTTSProfile(config: AudioConfig) {
  return config.tts.profiles.edge || config.tts.profiles[config.tts.profile] || {};
}

function normalizedInteger(raw: string, fallback: number) {
  const parsed = Number(raw);
  return Number.isInteger(parsed) ? parsed : fallback;
}

function normalizedNumber(raw: string, fallback: number) {
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : fallback;
}
