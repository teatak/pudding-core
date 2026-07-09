import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AudioLines,
  BookOpenText,
  Check,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Globe2,
  Info,
  Loader2,
  MessageSquareText,
  Pencil,
  Plus,
  Settings,
  SlidersHorizontal,
  Smartphone,
  Sparkles,
  Trash,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";

import {
  createMobilePairing,
  clearASRRecordings,
  applySkillDraft,
  deleteSkillDraft,
  deleteProvider,
  deleteSkill,
  getAudioConfig,
  getDailyUsage,
  getDesktopAbout,
  getSettings,
  getUserPrompt,
  getWebTools,
  listBrowserMCPSessions,
  listBuiltinTools,
  listSkillDrafts,
  listProviders,
  listSkills,
  patchWebTools,
  putAudioConfig,
  putSettings,
  putUserPrompt,
  skillIconURL,
  type BuiltinTool,
  type BrowserMCPSession,
  type AudioConfig,
  type DailyUsageStat,
  type DesktopAboutSection,
  type MobilePairing,
  type ProviderProfile,
  type Skill,
  type SkillDraft,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item";
import { Input } from "@/components/ui/input";
import { Select, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInput,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { BrandIcon } from "@/components/BrandIcons";
import { DialogSelectContent } from "@/components/DialogSelectContent";
import { IdentityIcon } from "@/components/IdentityIcon";
import {
  cloneProviderProfileForm,
  ProviderProfileEditorDialog,
  type ProviderProfileEditorValue,
} from "@/components/ProviderProfileEditorDialog";
import { ProviderPresetCreateDialog, ProviderPresetGrid } from "@/components/ProviderPresetCreateDialog";
import { SkillDraftDiffDialog } from "@/components/SkillDraftDiffDialog";
import { useI18n } from "@/i18n";
import { SETTINGS_KEYS, settingsWithDefaults } from "@/lib/appSettings";
import { shouldKeepDialogOpenForSelectDismiss } from "@/lib/layerGuards";
import { cn } from "@/lib/utils";
import { SETTINGS_DIALOG_OPEN_EVENT, type SettingsDialogOpenDetail, type SettingsSectionID } from "@/lib/settingsDialog";
import {
  getOrderedProviderPresets,
  providerPresetForBrand,
  type ProviderPreset,
} from "@/provider/presets";
import { setShowPreviewAppVersions, useShowPreviewAppVersions } from "@/state/appCatalogPrefs";
import { toast } from "sonner";

const SETTINGS_SECTIONS: Array<{
  id: SettingsSectionID;
  icon: typeof MessageSquareText;
  labelKey: string;
}> = [
  { id: "usage", icon: Activity, labelKey: "settings.section.usage" },
  { id: "dialogue", icon: SlidersHorizontal, labelKey: "settings.section.dialogue" },
  { id: "voice", icon: AudioLines, labelKey: "settings.section.voice" },
  { id: "model", icon: Sparkles, labelKey: "settings.section.model" },
  { id: "skills", icon: BookOpenText, labelKey: "settings.section.skills" },
  { id: "tools", icon: Globe2, labelKey: "settings.section.tools" },
  { id: "mobile", icon: Smartphone, labelKey: "settings.section.mobile" },
  { id: "about", icon: Info, labelKey: "settings.section.about" },
];

const SETTINGS_CONTENT_CLASS = "@container mx-auto grid min-w-0 w-full max-w-4xl gap-5";
const SETTINGS_NARROW_CONTENT_CLASS = "@container mx-auto grid min-w-0 w-full max-w-3xl gap-5";

type SettingsDialogProps = {
  token: string;
  showTrigger?: boolean;
};

export function SettingsDialog({ token, showTrigger = true }: SettingsDialogProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<SettingsSectionID>("usage");
  const [createProviderNonce, setCreateProviderNonce] = useState(0);
  const activeSection = SETTINGS_SECTIONS.find((section) => section.id === active) || SETTINGS_SECTIONS[0];

  useEffect(() => {
    const handleOpen = (event: Event) => {
      const detail = (event as CustomEvent<SettingsDialogOpenDetail>).detail || {};
      setActive(detail.createProvider ? "model" : detail.section || "usage");
      if (detail.createProvider) {
        setCreateProviderNonce((nonce) => nonce + 1);
      }
      setOpen(true);
    };
    window.addEventListener(SETTINGS_DIALOG_OPEN_EVENT, handleOpen);
    return () => window.removeEventListener(SETTINGS_DIALOG_OPEN_EVENT, handleOpen);
  }, []);

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (nextOpen && !open) {
          setActive("usage");
        }
        setOpen(nextOpen);
      }}
    >
      {showTrigger ? (
        <DialogTrigger asChild>
          <Button aria-label={t("settings.title")} size="icon" tabIndex={-1} variant="ghost">
            <Settings />
          </Button>
        </DialogTrigger>
      ) : null}
      <DialogContent
        className="top-[calc(var(--toolbar-h)+(100svh-var(--toolbar-h))/2)] h-[min(900px,calc(100svh-var(--toolbar-h)-1.5rem))] w-[calc(100%-0.5rem)] max-w-[430px] overflow-hidden bg-background p-0 sm:w-[calc(100vw-2rem)] sm:max-w-[1180px] xl:max-w-[1240px]"
        onPointerDownOutside={(event) => {
          if (shouldKeepDialogOpenForSelectDismiss(event.target)) {
            event.preventDefault();
          }
        }}
        onInteractOutside={(event) => {
          if (shouldKeepDialogOpenForSelectDismiss(event.target)) {
            event.preventDefault();
          }
        }}
      >
        <DialogTitle className="sr-only">{t("settings.title")}</DialogTitle>
        <DialogDescription className="sr-only">{t("settings.description")}</DialogDescription>
        <SidebarProvider
          className="h-full !min-h-0 min-w-0 max-w-full items-start overflow-hidden"
          style={{ "--sidebar-width": "14rem" } as CSSProperties}
        >
          <div className="hidden h-full shrink-0 lg:flex">
            <SettingsSidebar active={active} onActiveChange={setActive} />
          </div>
          <main className="flex h-full min-w-0 min-h-0 flex-1 flex-col overflow-hidden bg-background">
            <SettingsTopNav active={active} onActiveChange={setActive} />
            <header className="flex h-14 shrink-0 items-center gap-2 transition-[width,height] ease-linear lg:h-16 group-has-data-[collapsible=icon]/sidebar-wrapper:h-12">
              <div className="flex items-center gap-2 px-4">
                <h2 className="text-sm font-normal text-foreground">{t(activeSection.labelKey)}</h2>
              </div>
            </header>
            <div className="flex min-w-0 min-h-0 flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto px-3 pb-4 sm:px-4">
              {active === "usage" ? <UsageSettings token={token} /> : null}
              {active === "dialogue" ? <GeneralSettings token={token} /> : null}
              {active === "voice" ? <VoiceSettings token={token} /> : null}
              {active === "model" ? <ProviderSettings createNonce={createProviderNonce} token={token} /> : null}
              {active === "skills" ? <SkillsSettings token={token} /> : null}
              {active === "tools" ? <ToolsSettings token={token} /> : null}
              {active === "mobile" ? <MobileSettings token={token} /> : null}
              {active === "about" ? <AboutSettings token={token} /> : null}
            </div>
          </main>
        </SidebarProvider>
      </DialogContent>
    </Dialog>
  );
}

function SettingsTopNav({
  active,
  onActiveChange,
}: {
  active: SettingsSectionID;
  onActiveChange: (section: SettingsSectionID) => void;
}) {
  const { t } = useI18n();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [showLeftHint, setShowLeftHint] = useState(false);
  const [showRightHint, setShowRightHint] = useState(false);

  useEffect(() => {
    const scrollEl = scrollRef.current;
    if (!scrollEl) {
      return;
    }
    const updateHint = () => {
      setShowLeftHint(scrollEl.scrollLeft > 1);
      setShowRightHint(scrollEl.scrollLeft + scrollEl.clientWidth < scrollEl.scrollWidth - 1);
    };
    const resizeObserver = new ResizeObserver(updateHint);
    resizeObserver.observe(scrollEl);
    scrollEl.addEventListener("scroll", updateHint, { passive: true });
    updateHint();
    return () => {
      resizeObserver.disconnect();
      scrollEl.removeEventListener("scroll", updateHint);
    };
  }, []);

  return (
    <nav className="shrink-0 border-b lg:hidden" aria-label={t("settings.title")}>
      <div className="relative w-[calc(100%-3rem)] overflow-hidden">
        <div
          ref={scrollRef}
          className="flex gap-1 overflow-x-auto px-3 py-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        >
          {SETTINGS_SECTIONS.map((section) => {
            const Icon = section.icon;
            const isActive = section.id === active;
            return (
              <button
                key={section.id}
                aria-current={isActive ? "page" : undefined}
                className={cn(
                  "inline-flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm transition-colors",
                  isActive
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
                )}
                type="button"
                onClick={() => onActiveChange(section.id)}
              >
                <Icon className="size-4" />
                <span>{t(section.labelKey)}</span>
              </button>
            );
          })}
        </div>
        {showLeftHint ? (
          <div className="pointer-events-none absolute inset-y-0 left-0 w-8 bg-gradient-to-r from-background to-transparent" />
        ) : null}
        {showRightHint ? (
          <div className="pointer-events-none absolute inset-y-0 right-0 w-10 bg-gradient-to-l from-background to-transparent" />
        ) : null}
      </div>
    </nav>
  );
}

function SettingsSidebar({
  active,
  onActiveChange,
}: {
  active: SettingsSectionID;
  onActiveChange: (section: SettingsSectionID) => void;
}) {
  const { t } = useI18n();

  return (
    <Sidebar collapsible="none" className="flex shrink-0 border-r">
      <SidebarHeader className="p-4 pb-0">
        <SidebarInput aria-label={t("settings.search")} placeholder={t("settings.search")} />
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup className="p-3">
          <SidebarGroupLabel>{t("settings.title")}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu className="gap-1">
              {SETTINGS_SECTIONS.map((section) => {
                const Icon = section.icon;
                const isActive = section.id === active;
                return (
                  <SidebarMenuItem key={section.id}>
                    <SidebarMenuButton asChild className="cursor-default" isActive={isActive}>
                      <a
                        aria-current={isActive ? "page" : undefined}
                        href={`#settings-${section.id}`}
                        onClick={(event) => {
                          event.preventDefault();
                          onActiveChange(section.id);
                        }}
                      >
                        <Icon />
                        <span>{t(section.labelKey)}</span>
                      </a>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}

function UsageSettings({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const usageQuery = useQuery({
    queryKey: queryKeys.usageDaily(365),
    queryFn: () => getDailyUsage(token, 365),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const days = usageQuery.data?.days || [];
  const summary = useMemo(() => summarizeDailyUsage(days), [days]);

  return (
    <div className={cn(SETTINGS_CONTENT_CLASS, "gap-6 pt-2")}>
      <section className="grid min-w-0 gap-5">
        {usageQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription className="grid gap-2">
              <span>{t("settings.usage.loadFailed")}</span>
            </AlertDescription>
          </Alert>
        ) : null}
        {usageQuery.isLoading ? <UsageSkeleton /> : null}
        {!usageQuery.isLoading ? (
          <>
            <div className="grid grid-cols-4 divide-x divide-border/70 border-b pb-5">
              <UsageMetric label={t("settings.usage.totalTokens")} value={formatUsageTokens(summary.totalTokens)} />
              <UsageMetric label={t("settings.usage.requests")} value={formatNumber(summary.requestCount)} />
              <UsageMetric label={t("settings.usage.activeDays")} value={formatNumber(summary.activeDays)} />
              <UsageMetric label={t("settings.usage.peakDay")} value={formatUsageTokens(summary.peakTokens)} />
            </div>
            <UsageHeatmap days={days} locale={locale} t={t} />
          </>
        ) : null}
      </section>
    </div>
  );
}

function AboutSettings({ token }: { token: string }) {
  const { t } = useI18n();
  const aboutQuery = useQuery({
    queryKey: queryKeys.desktopAbout(),
    queryFn: () => getDesktopAbout(token),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const sections = (aboutQuery.data?.sections || []).filter((section) => !isVoiceAboutSection(section));

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-4 pt-2")}>
      {aboutQuery.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t("settings.loadFailed")}</AlertDescription>
        </Alert>
      ) : null}
      {aboutQuery.isLoading ? <AboutSettingsSkeleton /> : null}
      {!aboutQuery.isLoading
        ? sections.map((section) => <AboutInfoSection key={section.id} section={section} />)
        : null}
    </div>
  );
}

const VOICE_ABOUT_SECTION_IDS = new Set(["audio_config", "driver", "health", "audio_bindings", "asr", "asr_vad", "aec", "ns", "tts"]);

function isVoiceAboutSection(section: DesktopAboutSection) {
  return VOICE_ABOUT_SECTION_IDS.has(section.id);
}

function AboutInfoSection({ section }: { section: DesktopAboutSection }) {
  return (
    <SettingsPanel title={section.title}>
      <dl className="grid gap-2">
        {section.rows.map((row) => (
          <div
            key={`${section.id}-${row.key}`}
            className="grid min-w-0 grid-cols-[minmax(7rem,12rem)_minmax(0,1fr)] gap-3 text-sm"
          >
            <dt className="min-w-0 truncate text-muted-foreground">{row.key}</dt>
            <dd className="min-w-0 break-words text-foreground">{row.value}</dd>
          </div>
        ))}
      </dl>
    </SettingsPanel>
  );
}

function AboutSettingsSkeleton() {
  return (
    <>
      {[0, 1, 2].map((index) => (
        <SettingsPanel key={index} title={<Skeleton className="h-4 w-24" />}>
          <div className="grid gap-2">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-3/5" />
          </div>
        </SettingsPanel>
      ))}
    </>
  );
}

function GeneralSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [promptContent, setPromptContent] = useState("");
  const [tailTurns, setTailTurns] = useState("2");
  const [autoThreshold, setAutoThreshold] = useState("80");
  const [showCompactSummary, setShowCompactSummary] = useState(true);
  const [showReasoning, setShowReasoning] = useState(true);
  const [showToolDetails, setShowToolDetails] = useState(true);
  const showPreviewAppVersions = useShowPreviewAppVersions();

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

  useEffect(() => {
    if (userPromptQuery.isSuccess) {
      setPromptContent(savedPrompt);
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
    setShowToolDetails(savedSettings[SETTINGS_KEYS.showToolDetails] !== "false");
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

  const promptDirty = promptContent !== savedPrompt;

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
            onChange={(event) => setPromptContent(event.target.value)}
          />
          <div className="flex justify-end">
            <Button
              disabled={userPromptQuery.isLoading || promptMutation.isPending || !promptDirty}
              size="sm"
              type="button"
              onClick={() => promptMutation.mutate()}
            >
              {promptMutation.isPending ? <Loader2 className="animate-spin" /> : null}
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
            disabled={settingsQuery.isLoading}
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
            disabled={settingsQuery.isLoading}
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
            disabled={settingsQuery.isLoading}
            id="pudding-show-compact-summary"
            label={t("settings.general.showCompactSummary")}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showCompactSummary, next, setShowCompactSummary)}
          />
          <SettingsToggleRow
            checked={showReasoning}
            description={t("settings.general.showReasoningDesc")}
            disabled={settingsQuery.isLoading}
            id="pudding-show-reasoning"
            label={t("settings.general.showReasoning")}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showReasoning, next, setShowReasoning)}
          />
          <SettingsToggleRow
            checked={showToolDetails}
            description={t("settings.general.showToolDetailsDesc")}
            disabled={settingsQuery.isLoading}
            id="pudding-show-tool-details"
            label={t("settings.general.showToolDetails")}
            onChange={(next) => saveBooleanSetting(SETTINGS_KEYS.showToolDetails, next, setShowToolDetails)}
          />
        </div>
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.general.developer")}</h3>
        </div>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <SettingsToggleRow
            checked={showPreviewAppVersions}
            description={t("settings.general.showPreviewAppVersionsDesc")}
            id="pudding-show-preview-app-versions"
            label={t("settings.general.showPreviewAppVersions")}
            onChange={setShowPreviewAppVersions}
          />
        </div>
      </section>
    </div>
  );
}

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

const VOICE_LANGUAGE_OPTIONS = ["zh", "en", "ja", "ko", "yue", "auto"];
const VOICE_NS_LEVEL_OPTIONS = ["low", "moderate", "high", "very_high"];

function VoiceSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [form, setForm] = useState<VoiceFormState>(defaultVoiceForm());
  const [clearRecordingsOpen, setClearRecordingsOpen] = useState(false);
  const audioQuery = useQuery({
    queryKey: queryKeys.audioConfig(),
    queryFn: () => getAudioConfig(token),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const aboutQuery = useQuery({
    queryKey: queryKeys.desktopAbout(),
    queryFn: () => getDesktopAbout(token),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const savedConfig = audioQuery.data?.config;
  const aboutSections = aboutQuery.data?.sections || [];
  const runtimeReadOnlyRows = voiceRuntimeReadOnlyRows(aboutSections, audioQuery.data?.path || "-", savedConfig?.driver.type || "-");
  const asrReadOnlyRows = voiceSectionReadOnlyRows(aboutSections, "asr", ["engine", "model_path", "tokens_path", "provider"]);
  const vadReadOnlyRows = voiceSectionReadOnlyRows(aboutSections, "asr_vad", ["model_path", "window_size"]);
  const dspReadOnlyRows = [
    ...voiceSectionReadOnlyRows(aboutSections, "aec", ["model"], "aec"),
    ...voiceSectionReadOnlyRows(aboutSections, "ns", ["model"], "ns"),
  ];
  const ttsReadOnlyRows = voiceSectionReadOnlyRows(aboutSections, "tts", ["backend"]);

  useEffect(() => {
    if (savedConfig) {
      setForm(voiceFormFromConfig(savedConfig));
    }
  }, [savedConfig]);

  const saveMutation = useMutation({
    mutationFn: (nextForm: VoiceFormState) => {
      if (!savedConfig) {
        throw new Error("audio config missing");
      }
      return putAudioConfig(token, audioConfigFromForm(savedConfig, nextForm));
    },
    onSuccess: async (response) => {
      queryClient.setQueryData(queryKeys.audioConfig(), response);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.audioConfig() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.desktopAbout() }),
      ]);
    },
    onError: () => {
      toast.error(t("settings.voice.saveFailed"));
      void queryClient.invalidateQueries({ queryKey: queryKeys.audioConfig() });
    },
  });

  const clearRecordingsMutation = useMutation({
    mutationFn: () => clearASRRecordings(token),
    onSuccess: async (response) => {
      setClearRecordingsOpen(false);
      toast.success(t("settings.voice.asrClearAudioSuccess").replace("{count}", String(response.attachments)));
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
    saveMutation.mutate(nextForm);
  };
  const saveVoicePatch = (patch: Partial<VoiceFormState>) => saveVoiceForm({ ...form, ...patch });
  const saveCurrentVoiceForm = () => {
    if (!savedConfig) {
      return;
    }
    saveMutation.mutate(form);
  };
  const disabled = audioQuery.isLoading || saveMutation.isPending;
  const clearDisabled = disabled || clearRecordingsMutation.isPending;
  const edge = savedConfig ? edgeTTSProfile(savedConfig) : {};

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-8")}>
      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.voice.runtime")}</h3>
          <p className="text-sm leading-6 text-muted-foreground">{t("settings.voice.restartRequired")}</p>
        </div>
        {audioQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{t("settings.voice.loadFailed")}</AlertDescription>
          </Alert>
        ) : null}
        {audioQuery.isLoading ? (
          <div className="grid gap-2 rounded-xl border bg-card p-4">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-3/5" />
          </div>
        ) : (
          <dl className="grid gap-2 rounded-xl border bg-card p-4 text-sm">
            {runtimeReadOnlyRows.map((row) => (
              <SettingsInfoRow key={row.id} label={row.label} value={row.value} />
            ))}
          </dl>
        )}
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.voice.asr")}</h3>
        </div>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <SettingsToggleRow
            checked={form.asrEnabled}
            description={t("settings.voice.asrEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-asr-enabled"
            label={t("settings.voice.asrEnabled")}
            onChange={(next) => saveVoicePatch({ asrEnabled: next })}
          />
          <SettingsToggleRow
            checked={form.asrSaveAudio}
            description={t("settings.voice.asrSaveAudioDesc")}
            disabled={disabled}
            id="pudding-voice-asr-save-audio"
            label={t("settings.voice.asrSaveAudio")}
            onChange={(next) => saveVoicePatch({ asrSaveAudio: next })}
          />
          <SettingsToggleRow
            checked={form.asrUseITN}
            description={t("settings.voice.asrUseITNDesc")}
            disabled={disabled}
            id="pudding-voice-asr-itn"
            label={t("settings.voice.asrUseITN")}
            onChange={(next) => saveVoicePatch({ asrUseITN: next })}
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
              <SelectTrigger id="pudding-voice-asr-language" className="w-full">
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
          <SettingsNumberField
            description={t("settings.voice.asrNumThreadsDesc")}
            disabled={disabled}
            id="pudding-voice-asr-threads"
            label={t("settings.voice.asrNumThreads")}
            max={8}
            min={1}
            value={form.asrNumThreads}
            onBlur={saveCurrentVoiceForm}
            onChange={(value) => setForm((prev) => ({ ...prev, asrNumThreads: value }))}
          />
          <SettingsReadOnlyRows rows={asrReadOnlyRows} />
          <SettingsActionRow description={t("settings.voice.asrClearAudioDesc")} label={t("settings.voice.asrClearAudio")}>
            <Button disabled={clearDisabled} size="sm" type="button" variant="outline" onClick={() => setClearRecordingsOpen(true)}>
              {clearRecordingsMutation.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash className="size-4" />}
              {t("common.clear")}
            </Button>
          </SettingsActionRow>
        </div>
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.voice.vad")}</h3>
        </div>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
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
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.voice.dsp")}</h3>
        </div>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <SettingsToggleRow
            checked={form.aecEnabled}
            description={t("settings.voice.aecEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-aec-enabled"
            label={t("settings.voice.aecEnabled")}
            onChange={(next) => saveVoicePatch({ aecEnabled: next })}
          />
          <SettingsToggleRow
            checked={form.nsEnabled}
            description={t("settings.voice.nsEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-ns-enabled"
            label={t("settings.voice.nsEnabled")}
            onChange={(next) => saveVoicePatch({ nsEnabled: next })}
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
              <SelectTrigger id="pudding-voice-ns-level" className="w-full">
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
      </section>

      <section className="grid gap-4">
        <div className="grid gap-2">
          <h3 className="text-lg font-semibold tracking-tight">{t("settings.voice.tts")}</h3>
        </div>
        <div className="divide-y overflow-hidden rounded-xl border bg-card">
          <SettingsToggleRow
            checked={form.ttsEnabled}
            description={t("settings.voice.ttsEnabledDesc")}
            disabled={disabled}
            id="pudding-voice-tts-enabled"
            label={t("settings.voice.ttsEnabled")}
            onChange={(next) => saveVoicePatch({ ttsEnabled: next })}
          />
          <SettingsControlRow
            description={t("settings.voice.ttsVoiceDesc")}
            disabled={disabled}
            id="pudding-voice-tts-voice"
            label={t("settings.voice.ttsVoice")}
          >
            <Input
              className="w-full"
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
          <SettingsReadOnlyRows rows={ttsReadOnlyRows} />
        </div>
      </section>
      <AlertDialog open={clearRecordingsOpen} onOpenChange={(open) => !open && setClearRecordingsOpen(false)}>
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
  rows.push(...voiceSectionReadOnlyRows(sections, "audio_bindings", ["input_owner", "output_owner"]));
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

function SettingsControlRow({
  children,
  description,
  disabled,
  id,
  label,
}: {
  children: ReactNode;
  description: string;
  disabled?: boolean;
  id: string;
  label: string;
}) {
  return (
    <div
      className={cn(
        "grid gap-3 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,18rem)] sm:items-center",
        disabled && "opacity-60",
      )}
    >
      <label className="grid min-w-0 gap-1" htmlFor={id}>
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs leading-5 text-muted-foreground">{description}</span>
      </label>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function SettingsActionRow({
  children,
  description,
  label,
}: {
  children: ReactNode;
  description: string;
  label: string;
}) {
  return (
    <div className="grid gap-3 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,18rem)] sm:items-center">
      <span className="grid min-w-0 gap-1">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs leading-5 text-muted-foreground">{description}</span>
      </span>
      <div className="flex min-w-0 justify-start sm:justify-end">{children}</div>
    </div>
  );
}

function SettingsNumberField({
  description,
  disabled,
  id,
  label,
  max,
  min,
  onBlur,
  onChange,
  step,
  suffix,
  value,
}: {
  description: string;
  disabled?: boolean;
  id: string;
  label: string;
  max: number;
  min: number;
  onBlur?: () => void;
  onChange: (value: string) => void;
  step?: number | string;
  suffix?: string;
  value: string;
}) {
  return (
    <SettingsControlRow description={description} disabled={disabled} id={id} label={label}>
      <div className="flex items-center gap-2">
        <Input
          className="min-w-0 flex-1"
          disabled={disabled}
          id={id}
          inputMode="numeric"
          max={max}
          min={min}
          step={step}
          type="number"
          value={value}
          onBlur={onBlur}
          onChange={(event) => onChange(event.target.value)}
        />
        {suffix ? <span className="shrink-0 text-sm text-muted-foreground">{suffix}</span> : null}
      </div>
    </SettingsControlRow>
  );
}

function SettingsToggleRow({
  checked,
  description,
  disabled,
  id,
  label,
  onChange,
}: {
  checked: boolean;
  description: string;
  disabled?: boolean;
  id: string;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  const labelID = `${id}-label`;
  const descriptionID = `${id}-description`;
  return (
    <div className={cn("flex items-center justify-between gap-4 px-3 py-3", disabled && "opacity-60")}>
      <span className="grid min-w-0 gap-1">
        <span id={labelID} className="text-sm font-medium">
          {label}
        </span>
        <span id={descriptionID} className="text-xs leading-5 text-muted-foreground">
          {description}
        </span>
      </span>
      <Switch
        aria-describedby={descriptionID}
        aria-labelledby={labelID}
        checked={checked}
        disabled={disabled}
        id={id}
        onCheckedChange={onChange}
      />
    </div>
  );
}

function UsageMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 px-3 first:pl-0 sm:px-5">
      <div className="text-2xl font-semibold tabular-nums tracking-tight text-foreground">{value}</div>
      <div className="mt-0.5 text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

function UsageHeatmap({
  days,
  locale,
  t,
}: {
  days: DailyUsageStat[];
  locale: string;
  t: (key: string) => string;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = scrollRef.current;
    if (el) {
      el.scrollLeft = el.scrollWidth;
    }
  }, [days.length]);

  if (days.length === 0) {
    return <div className="text-sm text-muted-foreground">{t("settings.usage.noData")}</div>;
  }
  const leading = parseUsageDate(days[0]?.date).getDay();
  const cells: Array<DailyUsageStat | null> = [...Array.from({ length: leading }, () => null), ...days];
  const weekCount = Math.ceil(cells.length / 7);
  const heatThresholds = usageHeatThresholds(days);
  const monthLabels = usageMonthLabels(days, leading, locale);
  const heatmapGapRem = Math.max(0, weekCount - 1) * 0.25;

  return (
    <div
      ref={scrollRef}
      className="grid min-w-0 gap-3 overflow-x-auto pb-1"
      style={
        {
          "--usage-heat-cell": `clamp(0.4rem, calc((100cqw - ${heatmapGapRem}rem) / ${weekCount}), 0.75rem)`,
        } as CSSProperties
      }
    >
      <div className="mx-auto w-max max-w-full">
        <div className="mb-3 text-sm font-medium text-muted-foreground">{t("settings.usage.last365Days")}</div>
        <div className="w-max max-w-full">
          <TooltipProvider>
            <div
              className="grid grid-flow-col grid-rows-7 gap-1"
              style={{ gridAutoColumns: "var(--usage-heat-cell)", gridTemplateRows: "repeat(7, var(--usage-heat-cell))" }}
            >
              {cells.map((day, index) =>
                day ? (
                  <UsageHeatmapCell
                    key={day.date}
                    day={day}
                    heatThresholds={heatThresholds}
                    locale={locale}
                    t={t}
                  />
                ) : (
                  <div key={`empty-${index}`} className="size-(--usage-heat-cell)" />
                ),
              )}
            </div>
          </TooltipProvider>
          <div
            className="mt-2.5 grid gap-1 text-xs text-muted-foreground"
            style={{ gridTemplateColumns: `repeat(${weekCount}, var(--usage-heat-cell))` }}
          >
            {monthLabels.map((label) => (
              <div key={`${label.month}-${label.column}`} className="whitespace-nowrap" style={{ gridColumn: `${label.column + 1} / span 4` }}>
                {label.month}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function UsageHeatmapCell({
  day,
  heatThresholds,
  locale,
  t,
}: {
  day: DailyUsageStat;
  heatThresholds: number[];
  locale: string;
  t: (key: string) => string;
}) {
  if (day.totalTokens <= 0 && day.requestCount <= 0) {
    return <div className={cn("size-(--usage-heat-cell) rounded-[2px]", usageHeatClass(day.totalTokens, heatThresholds))} />;
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          aria-label={usageDayTitle(day, t)}
          className={cn("size-(--usage-heat-cell) rounded-[2px] transition-colors", usageHeatClass(day.totalTokens, heatThresholds))}
          type="button"
        />
      </TooltipTrigger>
      <TooltipContent
        className="grid gap-1"
        side="top"
        sideOffset={8}
      >
        <div className="font-medium">{formatUsageDateLabel(day.date, locale)}</div>
        <div className="tabular-nums">{`${t("usage.totalTokens")} ${formatUsageTokens(day.totalTokens)}`}</div>
        <div className="tabular-nums">{`${t("usage.requests")} ${formatNumber(day.requestCount)}`}</div>
      </TooltipContent>
    </Tooltip>
  );
}

function UsageSkeleton() {
  return (
    <div className="grid gap-4">
      <div className="grid gap-4 sm:grid-cols-4">
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
      </div>
      <Skeleton className="h-44" />
    </div>
  );
}

function summarizeDailyUsage(days: DailyUsageStat[]) {
  return days.reduce(
    (summary, day) => {
      summary.totalTokens += day.totalTokens;
      summary.requestCount += day.requestCount;
      if (day.totalTokens > 0) {
        summary.activeDays += 1;
      }
      if (day.totalTokens > summary.peakTokens) {
        summary.peakTokens = day.totalTokens;
      }
      return summary;
    },
    { activeDays: 0, peakTokens: 0, requestCount: 0, totalTokens: 0 },
  );
}

function usageMonthLabels(days: DailyUsageStat[], leading: number, locale: string) {
  const labels: Array<{ column: number; month: string }> = [];
  let previousMonth = "";
  for (let index = 0; index < days.length; index += 1) {
    const date = parseUsageDate(days[index].date);
    const monthKey = `${date.getFullYear()}-${date.getMonth()}`;
    if (monthKey === previousMonth) {
      continue;
    }
    previousMonth = monthKey;
    labels.push({
      column: Math.floor((leading + index) / 7),
      month: new Intl.DateTimeFormat(locale, { month: "short" }).format(date),
    });
  }
  const first = labels[0];
  const second = labels[1];
  if (first && second && second.column - first.column < 5) {
    return labels.slice(1);
  }
  return labels;
}

function usageHeatThresholds(days: DailyUsageStat[]) {
  const values = days
    .map((day) => day.totalTokens)
    .filter((tokens) => tokens > 0)
    .sort((left, right) => left - right);
  if (values.length === 0) {
    return [];
  }
  return [0.2, 0.4, 0.6, 0.8].map((ratio) => values[Math.floor((values.length - 1) * ratio)]);
}

function usageHeatClass(tokens: number, thresholds: number[]) {
  if (tokens <= 0 || thresholds.length === 0) {
    return "bg-slate-100 dark:bg-muted/60";
  }
  if (tokens >= thresholds[3]) {
    return "bg-[#2563eb] dark:bg-[#75a8d5]";
  }
  if (tokens >= thresholds[2]) {
    return "bg-[#3b82f6] dark:bg-[#5f8caf]";
  }
  if (tokens >= thresholds[1]) {
    return "bg-[#60a5fa] dark:bg-[#466b86]";
  }
  if (tokens >= thresholds[0]) {
    return "bg-[#93c5fd] dark:bg-[#30485b]";
  }
  return "bg-[#dbeafe] dark:bg-[#263746]";
}

function usageDayTitle(day: DailyUsageStat, t: (key: string) => string) {
  const input = day.inputUncachedTokens + day.inputCachedTokens + day.cacheCreationTokens;
  const output = day.outputContentTokens + day.outputReasoningTokens;
  return `${day.date} · ${t("usage.totalTokens")} ${formatUsageTokens(day.totalTokens)} · ${t("usage.requests")} ${formatNumber(day.requestCount)} · ${t("usage.inputTotal")} ${formatUsageTokens(input)} · ${t("usage.outputTotal")} ${formatUsageTokens(output)}`;
}

function formatUsageDateLabel(date: string, locale: string) {
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "long" }).format(parseUsageDate(date));
}

function parseUsageDate(date: string) {
  return new Date(`${date}T00:00:00`);
}

function SkillsSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [deletingSkill, setDeletingSkill] = useState<Skill | null>(null);
  const [viewingDraft, setViewingDraft] = useState<SkillDraft | null>(null);
  const draftsQuery = useQuery({
    queryKey: queryKeys.skillDrafts(),
    queryFn: () => listSkillDrafts(token),
    enabled: Boolean(token),
    staleTime: Infinity,
  });
  const skillsQuery = useQuery({
    queryKey: queryKeys.skills(),
    queryFn: () => listSkills(token),
    enabled: Boolean(token),
    staleTime: Infinity,
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteSkill(token, id),
    onSuccess: async () => {
      setDeletingSkill(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.skills() });
      toast.success(t("settings.skills.deleted"));
    },
    onError: () => toast.error(t("settings.skills.deleteFailed")),
  });
  const applyDraftMutation = useMutation({
    mutationFn: (id: string) => applySkillDraft(token, id),
    onSuccess: async () => {
      setViewingDraft(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.skillDrafts() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.skills() }),
      ]);
      toast.success(t("settings.skills.draftApplied"));
    },
    onError: () => toast.error(t("settings.skills.draftApplyFailed")),
  });
  const rejectDraftMutation = useMutation({
    mutationFn: (id: string) => deleteSkillDraft(token, id),
    onSuccess: async (_data, id) => {
      if (viewingDraft?.id === id) {
        setViewingDraft(null);
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.skillDrafts() });
      toast.success(t("settings.skills.draftRejected"));
    },
    onError: () => toast.error(t("settings.skills.draftRejectFailed")),
  });
  const drafts = draftsQuery.data?.drafts || [];
  const skills = skillsQuery.data?.skills || [];

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      {draftsQuery.isLoading || draftsQuery.isError || drafts.length > 0 ? (
        <SettingsSection title={`${t("settings.skills.pendingTitle")} (${drafts.length})`}>
          {draftsQuery.isError ? (
            <Alert variant="destructive">
              <AlertDescription className="grid gap-2">
                <span>{t("settings.skills.draftsLoadFailed")}</span>
                <Button size="sm" type="button" variant="outline" onClick={() => void draftsQuery.refetch()}>
                  {t("common.refresh")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : null}
          {draftsQuery.isLoading ? (
            <SkillsSkeleton />
          ) : (
            <SkillDraftList
              applyingID={applyDraftMutation.isPending ? applyDraftMutation.variables : undefined}
              drafts={drafts}
              rejectingID={rejectDraftMutation.isPending ? rejectDraftMutation.variables : undefined}
              token={token}
              onApply={(draft) => applyDraftMutation.mutate(draft.id)}
              onReject={(draft) => rejectDraftMutation.mutate(draft.id)}
              onView={setViewingDraft}
            />
          )}
        </SettingsSection>
      ) : null}
      <SettingsSection title={`${t("settings.skills.title")} (${skills.length})`}>
        {skillsQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription className="grid gap-2">
              <span>{t("settings.skills.loadFailed")}</span>
              <Button size="sm" type="button" variant="outline" onClick={() => void skillsQuery.refetch()}>
                {t("common.refresh")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}
        {skillsQuery.isLoading ? (
          <SkillsSkeleton />
        ) : (
          <SkillList
            deletingID={deleteMutation.isPending ? deletingSkill?.id : undefined}
            skills={skills}
            token={token}
            onDelete={(skill) => setDeletingSkill(skill)}
          />
        )}
      </SettingsSection>
      <SkillDraftDiffDialog
        applying={applyDraftMutation.isPending && applyDraftMutation.variables === viewingDraft?.id}
        draft={viewingDraft}
        rejecting={rejectDraftMutation.isPending && rejectDraftMutation.variables === viewingDraft?.id}
        token={token}
        onApply={(draft) => applyDraftMutation.mutate(draft.id)}
        onOpenChange={(open) => !open && setViewingDraft(null)}
        onReject={(draft) => rejectDraftMutation.mutate(draft.id)}
      />
      <AlertDialog open={Boolean(deletingSkill)} onOpenChange={(open) => !open && setDeletingSkill(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("settings.skills.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("settings.skills.deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={!deletingSkill || deleteMutation.isPending}
              variant="destructive"
              onClick={(event) => {
                event.preventDefault();
                if (deletingSkill) {
                  deleteMutation.mutate(deletingSkill.id);
                }
              }}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SkillList({
  deletingID,
  onDelete,
  skills,
  token,
}: {
  deletingID?: string;
  onDelete: (skill: Skill) => void;
  skills: Skill[];
  token: string;
}) {
  const { t } = useI18n();
  if (skills.length === 0) {
    return <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">{t("settings.skills.empty")}</div>;
  }
  return (
    <ItemGroup className="gap-2">
      {skills.map((skill) => (
        <SkillRow
          key={`${skill.source}:${skill.id}`}
          deleting={deletingID === skill.id}
          skill={skill}
          token={token}
          onDelete={() => onDelete(skill)}
        />
      ))}
    </ItemGroup>
  );
}

function SkillDraftList({
  applyingID,
  drafts,
  onApply,
  onReject,
  onView,
  rejectingID,
  token,
}: {
  applyingID?: string;
  drafts: SkillDraft[];
  onApply: (draft: SkillDraft) => void;
  onReject: (draft: SkillDraft) => void;
  onView: (draft: SkillDraft) => void;
  rejectingID?: string;
  token: string;
}) {
  if (drafts.length === 0) {
    return null;
  }
  return (
    <ItemGroup className="gap-2">
      {drafts.map((draft) => (
        <SkillDraftRow
          key={draft.id}
          applying={applyingID === draft.id}
          draft={draft}
          rejecting={rejectingID === draft.id}
          token={token}
          onApply={() => onApply(draft)}
          onReject={() => onReject(draft)}
          onView={() => onView(draft)}
        />
      ))}
    </ItemGroup>
  );
}

function SkillDraftRow({
  applying,
  draft,
  onApply,
  onReject,
  onView,
  rejecting,
  token,
}: {
  applying?: boolean;
  draft: SkillDraft;
  onApply: () => void;
  onReject: () => void;
  onView: () => void;
  rejecting?: boolean;
  token: string;
}) {
  const { t } = useI18n();
  const busy = applying || rejecting;
  const iconURL = skillIconURL(token, draft);
  return (
    <Item className="items-start gap-3 rounded-lg px-3 py-3" variant="outline">
      <ItemMedia>
        <IdentityIcon fallback="skill" fit="contain" size="lg" src={iconURL || undefined} />
      </ItemMedia>
      <ItemContent className="min-w-0 gap-1">
        <ItemTitle className="flex max-w-full flex-wrap items-center gap-2">
          <span className="min-w-0 truncate text-sm font-medium">{draft.id}</span>
          <span className="min-w-0 truncate text-xs font-normal text-muted-foreground">{draft.path}</span>
          <span className="rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {t(`settings.skills.draftChange.${draft.change}`)}
          </span>
          {!draft.validation.ok ? (
            <span className="rounded border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 text-[10px] leading-none text-destructive">
              {t("settings.skills.draftInvalid")}
            </span>
          ) : null}
        </ItemTitle>
        <ItemDescription className="line-clamp-2 text-xs leading-5">
          {draft.description || draft.validation.errors?.[0] || t("settings.skills.noDescription")}
        </ItemDescription>
      </ItemContent>
      <ItemActions className="self-center">
        <Button aria-label={t("settings.skills.viewDiff")} disabled={busy} size="icon-sm" type="button" variant="ghost" onClick={onView}>
          <Eye />
        </Button>
        <Button
          aria-label={t("settings.skills.applyDraft")}
          disabled={busy || !draft.validation.ok}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onApply}
        >
          {applying ? <Loader2 className="animate-spin" /> : <Check />}
        </Button>
        <Button
          aria-label={t("settings.skills.rejectDraft")}
          className="text-destructive hover:bg-destructive/10 hover:text-destructive"
          disabled={busy}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={onReject}
        >
          {rejecting ? <Loader2 className="animate-spin" /> : <Trash />}
        </Button>
      </ItemActions>
    </Item>
  );
}

function SkillRow({ deleting, onDelete, skill, token }: { deleting?: boolean; onDelete: () => void; skill: Skill; token: string }) {
  const { t } = useI18n();
  const iconURL = skillIconURL(token, skill);
  return (
    <Item className="items-start gap-3 rounded-lg px-3 py-3" variant="outline">
      <ItemMedia>
        <IdentityIcon fallback="skill" fit="contain" size="lg" src={iconURL || undefined} />
      </ItemMedia>
      <ItemContent className="min-w-0 gap-1">
        <ItemTitle className="flex max-w-full flex-wrap items-center gap-2">
          <span className="min-w-0 truncate text-sm font-medium">{skill.id}</span>
          <span className="min-w-0 truncate text-[11px] font-normal text-muted-foreground">
            {skillDisplayPath(skill)}
          </span>
          <span className="rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {t(`settings.skills.source.${skill.source}`)}
          </span>
          <span className="rounded border border-border/70 px-1.5 py-0.5 text-[10px] leading-none text-muted-foreground">
            {t("settings.skills.scope.global")}
          </span>
        </ItemTitle>
        <ItemDescription className="line-clamp-2 text-xs leading-5">
          {skill.description || t("settings.skills.noDescription")}
        </ItemDescription>
      </ItemContent>
      {skill.source === "user" ? (
        <ItemActions className="self-center opacity-0 transition-opacity group-hover/item:opacity-100 group-focus-within/item:opacity-100">
          <Button
            aria-label={t("common.delete")}
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
            disabled={deleting}
            size="icon"
            type="button"
            variant="ghost"
            onClick={onDelete}
          >
            {deleting ? <Loader2 className="animate-spin" /> : <Trash />}
          </Button>
        </ItemActions>
      ) : null}
    </Item>
  );
}

function skillDisplayPath(skill: Skill) {
  if (skill.source === "builtin") {
    return `builtin/${skill.id}`;
  }
  return `skills/${skill.id}`;
}

function SkillsSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}

function ToolsSettings({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [apiKey, setAPIKey] = useState("");
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
  const dirty = apiKey.trim() !== savedAPIKey.trim();
  const configured = Boolean(tavily?.apiKeySet);
  const loadingTools = toolsQuery.isLoading;
  const saving = mutation.isPending;

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      <BuiltinToolsPanel
        loading={builtinToolsQuery.isFetching}
        error={builtinToolsQuery.isError}
        tools={builtinToolsQuery.data?.tools || []}
        onRetry={() => void builtinToolsQuery.refetch()}
      />
      <BrowserMCPToolsPanel
        error={browserMCPQuery.isError}
        loading={browserMCPQuery.isLoading}
        sessions={browserMCPQuery.data?.sessions || []}
        onRetry={() => void browserMCPQuery.refetch()}
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
                  onChange={(event) => setAPIKey(event.target.value)}
                />
                <button
                  aria-label={visible ? t("provider.hideAPIKey") : t("provider.showAPIKey")}
                  className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
                  type="button"
                  onClick={() => setVisible((value) => !value)}
                >
                  {visible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                </button>
              </div>
              <div className="flex gap-2">
                <Button disabled={saving || loadingTools || !dirty} type="button" onClick={() => mutation.mutate(apiKey.trim())}>
                  {mutation.isPending ? <Loader2 className="animate-spin" /> : null}
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

function MobileSettings({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const [pairing, setPairing] = useState<MobilePairing | null>(null);
  const mutation = useMutation({
    mutationFn: () => createMobilePairing(token),
    onSuccess: (next) => setPairing(next),
    onError: () => toast.error(t("settings.mobile.createFailed")),
  });
  const expiresAt = pairing?.expiresAt
    ? new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(
        new Date(pairing.expiresAt),
      )
    : "";

  function copyURL() {
    if (!pairing?.url) {
      return;
    }
    void navigator.clipboard.writeText(pairing.url).then(() => toast.success(t("common.copied")));
  }

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      <SettingsPanel
        action={
          <Button disabled={mutation.isPending} type="button" onClick={() => mutation.mutate()}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : null}
            {t("settings.mobile.generate")}
          </Button>
        }
        title={t("settings.mobile.title")}
      >
        <div className="grid gap-4">
          <p className="text-sm leading-6 text-muted-foreground">{t("settings.mobile.desc")}</p>
          {pairing ? (
            <div className="grid gap-4 sm:grid-cols-[16rem_1fr]">
              <div className="grid justify-items-center gap-2 rounded-lg border bg-background p-4">
                {pairing.qrDataURL ? (
                  <img className="size-52 rounded-md bg-white p-2" src={pairing.qrDataURL} alt={t("settings.mobile.qrAlt")} />
                ) : null}
                <div className="text-xs text-muted-foreground">
                  {expiresAt ? `${t("settings.mobile.expiresAt")} ${expiresAt}` : null}
                </div>
              </div>
              <div className="grid content-start gap-2">
                <label className="text-sm" htmlFor="pudding-mobile-pairing-url">
                  {t("settings.mobile.url")}
                </label>
                <div className="flex min-w-0 gap-2">
                  <Input
                    readOnly
                    className="font-mono text-xs"
                    id="pudding-mobile-pairing-url"
                    value={pairing.url}
                  />
                  <Button aria-label={t("common.copy")} size="icon" type="button" variant="outline" onClick={copyURL}>
                    <Copy />
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
              {t("settings.mobile.empty")}
            </div>
          )}
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
  tools: BuiltinTool[];
}) {
  const { t } = useI18n();

  return (
    <Accordion className="overflow-hidden rounded-xl border bg-card" collapsible type="single">
      <AccordionItem className="border-b-0" value="builtin-tools">
        <AccordionTrigger className="h-11 items-center rounded-none border-0 px-4 py-0 text-sm font-normal hover:no-underline focus-visible:ring-0">
          <span>{`${t("settings.tools.builtin.title")} (${tools.length})`}</span>
          {loading ? <Loader2 className="mr-2 size-4 animate-spin text-muted-foreground" /> : null}
        </AccordionTrigger>
        <AccordionContent className="p-0">
          {error ? (
            <div className="border-t p-4">
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

function ToolList({ tools }: { tools: BuiltinTool[] }) {
  const { t } = useI18n();
  if (tools.length === 0) {
    return <div className="border-t px-4 py-3 text-sm text-muted-foreground">{t("settings.tools.builtin.empty")}</div>;
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
  capability?: "chat" | "workspace";
};

function BrowserMCPToolsPanel({
  error,
  loading,
  onRetry,
  sessions,
}: {
  error: boolean;
  loading: boolean;
  onRetry: () => void;
  sessions: BrowserMCPSession[];
}) {
  const canvasTools = useMemo(() => uniqueBrowserTools(sessions, "canvas_"), [sessions]);
  const uiTools = useMemo(() => uniqueBrowserTools(sessions, "ui_"), [sessions]);
  const connected = sessions.length > 0;

  return (
    <>
      <BrowserMCPToolGroup
        connected={connected && canvasTools.length > 0}
        error={error}
        group="canvas"
        loading={loading}
        tools={canvasTools}
        onRetry={onRetry}
      />
      <BrowserMCPToolGroup
        connected={connected && uiTools.length > 0}
        error={error}
        group="ui"
        loading={loading}
        tools={uiTools}
        onRetry={onRetry}
      />
    </>
  );
}

function BrowserMCPToolGroup({
  connected,
  error,
  group,
  loading,
  onRetry,
  tools,
}: {
  connected: boolean;
  error: boolean;
  group: "canvas" | "ui";
  loading: boolean;
  onRetry: () => void;
  tools: ToolInfo[];
}) {
  const { t } = useI18n();
  return (
    <Accordion className="overflow-hidden rounded-xl border bg-card" collapsible type="single">
      <AccordionItem className="border-b-0" value={`${group}-tools`}>
        <AccordionTrigger className="h-11 items-center rounded-none border-0 px-4 py-0 text-sm font-normal hover:no-underline focus-visible:ring-0">
          <span className="flex min-w-0 items-center gap-2">
            <span>{`${t(`settings.tools.${group}.title`)} (${tools.length})`}</span>
            <span
              aria-hidden="true"
              className={cn("size-2 rounded-full", connected ? "bg-success" : "bg-muted-foreground/50")}
            />
          </span>
          {loading ? <Loader2 className="mr-2 size-4 animate-spin text-muted-foreground" /> : null}
        </AccordionTrigger>
        <AccordionContent className="p-0">
          {error ? (
            <div className="border-t p-4">
              <Alert variant="destructive">
                <AlertDescription className="grid gap-2">
                  <span>{t(`settings.tools.${group}.loadFailed`)}</span>
                  <Button size="sm" type="button" variant="outline" onClick={onRetry}>
                    {t("common.refresh")}
                  </Button>
                </AlertDescription>
              </Alert>
            </div>
          ) : tools.length === 0 ? (
            <div className="grid gap-1 border-t px-4 py-3 text-sm text-muted-foreground">
              <span>{t(`settings.tools.${group}.empty`)}</span>
              <span className="text-xs">{t(`settings.tools.${group}.desc`)}</span>
            </div>
          ) : (
            <div className="divide-y divide-border/70 border-t">
              {tools.map((tool) => (
                <ToolInfoRow key={tool.id} tool={tool} />
              ))}
            </div>
          )}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

function uniqueBrowserTools(sessions: BrowserMCPSession[], prefix: string): ToolInfo[] {
  const seen = new Set<string>();
  const tools: ToolInfo[] = [];
  for (const session of sessions) {
    for (const tool of session.tools) {
      if (!tool.name.startsWith(prefix) || seen.has(tool.name)) {
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

function ToolInfoRow({ tool }: { tool: ToolInfo }) {
  const { t } = useI18n();
  const capabilityLabel = tool.capability ? t(`mode.${tool.capability}`) : "";
  return (
    <div className="grid gap-1 px-4 py-3">
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

function ProviderSettings({ createNonce = 0, token }: { createNonce?: number; token: string }) {
  const queryClient = useQueryClient();
  const { locale, t } = useI18n();
  const [editingProfile, setEditingProfile] = useState<ProviderProfile | null>(null);
  const [editorInitialValue, setEditorInitialValue] = useState<ProviderProfileEditorValue | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [quickPreset, setQuickPreset] = useState<ProviderPreset | null>(null);
  const [presetPickerOpen, setPresetPickerOpen] = useState(false);
  const [deletingProfile, setDeletingProfile] = useState<ProviderProfile | null>(null);
  const handledCreateNonceRef = useRef(0);
  const providerPresets = getOrderedProviderPresets(locale);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteProvider(token, id),
    onSuccess: async (_, id) => {
      if (editingProfile?.id === id) {
        setEditingProfile(null);
        setEditorInitialValue(null);
        setEditorOpen(false);
      }
      setDeletingProfile(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
    },
  });

  const profiles = providersQuery.data?.providers || [];
  const showInlinePresets = !providersQuery.isLoading && !providersQuery.isError && profiles.length === 0;

  function startCreate() {
    setEditingProfile(null);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }

  useEffect(() => {
    if (!createNonce || handledCreateNonceRef.current === createNonce) {
      return;
    }
    handledCreateNonceRef.current = createNonce;
    setEditingProfile(null);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }, [createNonce]);

  function editProfile(profile: ProviderProfile) {
    setEditingProfile(profile);
    setEditorInitialValue(null);
    setEditorOpen(true);
  }

  function cloneProfile(profile: ProviderProfile) {
    setEditingProfile(null);
    setEditorInitialValue(cloneProviderProfileForm(profile, profiles, t("provider.copySuffix")));
    setEditorOpen(true);
  }

  function selectPreset(preset: ProviderPreset) {
    setPresetPickerOpen(false);
    setQuickPreset(preset);
  }

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      {showInlinePresets ? (
        <SettingsSection title={t("provider.addFromPreset")}>
          <ProviderPresetGrid presets={providerPresets} onSelect={selectPreset} />
        </SettingsSection>
      ) : null}

      <SettingsSection
        action={
          <div className="flex items-center gap-2">
            {showInlinePresets ? null : (
              <Button size="sm" type="button" variant="outline" onClick={() => setPresetPickerOpen(true)}>
                <Sparkles />
                {t("provider.addFromPreset")}
              </Button>
            )}
            <Button size="sm" type="button" variant="outline" onClick={startCreate}>
              <Plus />
              {t("provider.new")}
            </Button>
          </div>
        }
        title={t("provider.list")}
      >
        {providersQuery.isLoading ? <ProviderSkeleton /> : null}
        {providersQuery.isError ? (
          <Alert variant="destructive">
            <AlertDescription className="grid gap-2">
              <span>{t("provider.loadFailed")}</span>
              <Button size="sm" type="button" variant="outline" onClick={() => void providersQuery.refetch()}>
                {t("common.refresh")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : null}
        {showInlinePresets ? (
          <div className="rounded-xl border border-dashed px-4 py-3 text-sm text-muted-foreground">
            {t("provider.empty")}
          </div>
        ) : null}
        {profiles.length > 0 ? (
          <ItemGroup className="gap-2">
            {profiles.map((profile) => {
              return (
                <Item
                  key={profile.id}
                  className="group min-h-16 flex-nowrap rounded-xl bg-card px-4 py-3 hover:bg-accent/50"
                  role="listitem"
                  variant="outline"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3 text-left">
                    <ItemMedia>
                      <BrandIcon className="shrink-0" name={profile.brand || profile.displayName || profile.id} size="lg" />
                    </ItemMedia>
                    <ItemContent className="min-w-0 gap-0.5">
                      <ItemTitle className="w-full min-w-0 font-normal">
                        <span className="truncate text-base font-normal">{profile.displayName}</span>
                        <span
                          className={cn(
                            "size-2 shrink-0 rounded-full",
                            profile.apiKeySet ? "bg-success" : "bg-warning",
                          )}
                          title={profile.apiKeySet ? t("provider.keySet") : t("provider.keyMissing")}
                        />
                      </ItemTitle>
                      <ItemDescription className="truncate text-xs">
                        {profile.protocol} · {profileModelCountLabel(profile, t)}
                      </ItemDescription>
                    </ItemContent>
                  </div>
                  <ItemActions className="ml-auto shrink-0 gap-1">
                    <Button aria-label={t("provider.editShort")} size="icon-sm" type="button" variant="ghost" onClick={() => editProfile(profile)}>
                      <Pencil />
                    </Button>
                    <Button aria-label={t("common.copy")} size="icon-sm" type="button" variant="ghost" onClick={() => cloneProfile(profile)}>
                      <Copy />
                    </Button>
                    <Button
                      aria-label={t("common.delete")}
                      disabled={deleteMutation.isPending}
                      size="icon-sm"
                      type="button"
                      variant="ghost"
                      onClick={() => setDeletingProfile(profile)}
                    >
                      <Trash className="text-destructive" />
                    </Button>
                  </ItemActions>
                </Item>
              );
            })}
          </ItemGroup>
        ) : null}
      </SettingsSection>

      <AlertDialog open={Boolean(deletingProfile)} onOpenChange={(open) => {
        if (!open) {
          setDeletingProfile(null);
        }
      }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("provider.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("provider.deleteDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={!deletingProfile || deleteMutation.isPending}
              variant="destructive"
              onClick={() => {
                if (deletingProfile) {
                  deleteMutation.mutate(deletingProfile.id);
                }
              }}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={presetPickerOpen} onOpenChange={setPresetPickerOpen}>
        <DialogContent className="@container w-[min(1120px,calc(100vw-2rem))] max-w-none sm:max-w-none">
          <DialogHeader>
            <DialogTitle>{t("provider.addFromPreset")}</DialogTitle>
            <DialogDescription>{t("provider.addFromPresetHint")}</DialogDescription>
          </DialogHeader>
          <ProviderPresetGrid
            className="pudding-provider-preset-surface-dark"
            presets={providerPresets}
            onSelect={selectPreset}
          />
        </DialogContent>
      </Dialog>

      <ProviderPresetCreateDialog
        open={Boolean(quickPreset)}
        preset={quickPreset}
        profiles={profiles}
        token={token}
        onOpenChange={(open) => {
          if (!open) {
            setQuickPreset(null);
          }
        }}
      />

      <ProviderProfileEditorDialog
        initialValue={editorInitialValue}
        open={editorOpen}
        profile={editingProfile}
        profiles={profiles}
        token={token}
        onOpenChange={(next) => {
          setEditorOpen(next);
          if (!next) {
            setEditingProfile(null);
            setEditorInitialValue(null);
          }
        }}
      />
    </div>
  );
}

function SettingsPanel({
  action,
  children,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  title: ReactNode;
}) {
  return (
    <section className="overflow-hidden rounded-xl border bg-card">
      <div className="pudding-settings-panel-header flex h-11 items-center justify-between gap-3 border-b px-4">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      {children ? <div className="grid gap-3 p-4">{children}</div> : null}
    </section>
  );
}

function SettingsSection({
  action,
  children,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  title: string;
}) {
  return (
    <section className="grid gap-3">
      <div className="flex min-h-8 items-center justify-between gap-3">
        <h3 className="text-sm font-normal">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

function modelCountLabel(count: number, t: (key: string) => string) {
  if (count <= 0) {
    return t("picker.noModels");
  }
  return `${count}${t("provider.modelCountSuffix")}`;
}

function profileModelCountLabel(profile: ProviderProfile, t: (key: string) => string) {
  if (profile.models.length > 0) {
    return modelCountLabel(profile.models.length, t);
  }
  const preset = providerPresetForBrand(profile.brand);
  if (preset?.variants.some((variant) => variant.dynamicModels)) {
    return t("provider.modelCountDynamic");
  }
  return modelCountLabel(0, t);
}

function formatNumber(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

function formatUsageTokens(value: number) {
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`;
  }
  if (abs >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`;
  }
  if (abs >= 1_000) {
    return `${(value / 1_000).toFixed(value >= 10_000 ? 0 : 1).replace(/\.0$/, "")}K`;
  }
  return formatNumber(value);
}

function ProviderSkeleton() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </div>
  );
}
