import { useQuery } from "@tanstack/react-query";

import { getDesktopAbout, type DesktopAboutSection } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

import { SETTINGS_NARROW_CONTENT_CLASS, SettingsPanel } from "./shared";

export function AboutSettings({ token }: { token: string }) {
  const { t } = useI18n();
  const aboutQuery = useQuery({
    queryKey: queryKeys.desktopAbout(),
    queryFn: () => getDesktopAbout(token),
    enabled: Boolean(token),
    refetchOnMount: "always",
  });
  const sections = (aboutQuery.data?.sections || []).filter((section) => !isVoiceAboutSection(section));

  return (
    <div className={cn(SETTINGS_NARROW_CONTENT_CLASS, "gap-4")}>
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

const VOICE_ABOUT_SECTION_IDS = new Set(["audio_config", "driver", "health", "audio_bindings", "asr", "asr_vad", "aec", "ns"]);

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
