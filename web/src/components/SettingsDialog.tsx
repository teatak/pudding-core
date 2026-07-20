import {
  Activity,
  AudioLines,
  BookOpenText,
  Globe2,
  Info,
  MessageSquareText,
  Settings,
  SlidersHorizontal,
  Sparkles,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";

import { UnsavedChangesAlert } from "@/components/UnsavedChangesAlert";
import { AboutSettings } from "@/components/settings/AboutSettings";
import { GeneralSettings } from "@/components/settings/GeneralSettings";
import { ProviderSettings } from "@/components/settings/ProviderSettings";
import { SkillsSettings } from "@/components/settings/SkillsSettings";
import { ToolsSettings } from "@/components/settings/ToolsSettings";
import { UsageSettings } from "@/components/settings/UsageSettings";
import { VoiceSettings } from "@/components/settings/VoiceSettings";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { useI18n } from "@/i18n";
import { useUnsavedChangesGuard } from "@/hooks/useUnsavedChangesGuard";
import { shouldKeepDialogOpenForSelectDismiss } from "@/lib/layerGuards";
import { SETTINGS_DIALOG_OPEN_EVENT, type SettingsDialogOpenDetail, type SettingsSectionID } from "@/lib/settingsDialog";
import { cn } from "@/lib/utils";

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
  { id: "about", icon: Info, labelKey: "settings.section.about" },
];

type SettingsDialogProps = {
  token: string;
  showTrigger?: boolean;
};

export function SettingsDialog({ token, showTrigger = true }: SettingsDialogProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<SettingsSectionID>("usage");
  const [activeDirty, setActiveDirty] = useState(false);
  const [createProviderNonce, setCreateProviderNonce] = useState(0);
  const {
    confirmationOpen,
    discard: discardUnsavedChanges,
    request: requestUnsavedChanges,
    setConfirmationOpen,
  } = useUnsavedChangesGuard(activeDirty);
  const activeSection = SETTINGS_SECTIONS.find((section) => section.id === active) || SETTINGS_SECTIONS[0];

  const changeActive = useCallback((nextActive: SettingsSectionID) => {
    if (nextActive === active) {
      return;
    }
    requestUnsavedChanges(() => {
      setActiveDirty(false);
      setActive(nextActive);
    });
  }, [active, requestUnsavedChanges]);

  const changeOpen = useCallback((nextOpen: boolean) => {
    if (nextOpen) {
      setActiveDirty(false);
      setActive("usage");
      setOpen(true);
      return;
    }
    requestUnsavedChanges(() => {
      setActiveDirty(false);
      setOpen(false);
    });
  }, [requestUnsavedChanges]);

  const handleCreateProviderHandled = useCallback(() => setCreateProviderNonce(0), []);

  useEffect(() => {
    const handleOpen = (event: Event) => {
      const detail = (event as CustomEvent<SettingsDialogOpenDetail>).detail || {};
      const nextActive = detail.createProvider ? "model" : detail.section || "usage";
      const showSettings = () => {
        setActiveDirty(false);
        setActive(nextActive);
        if (detail.createProvider) {
          setCreateProviderNonce((nonce) => nonce + 1);
        }
        setOpen(true);
      };
      if (open && nextActive === active && !detail.createProvider) {
        return;
      }
      if (open) {
        requestUnsavedChanges(showSettings);
        return;
      }
      showSettings();
    };
    window.addEventListener(SETTINGS_DIALOG_OPEN_EVENT, handleOpen);
    return () => window.removeEventListener(SETTINGS_DIALOG_OPEN_EVENT, handleOpen);
  }, [active, open, requestUnsavedChanges]);

  return (
    <>
      <Dialog open={open} onOpenChange={changeOpen}>
        {showTrigger ? (
          <DialogTrigger asChild>
            <Button aria-label={t("settings.title")} size="icon" tabIndex={-1} variant="ghost">
              <Settings />
            </Button>
          </DialogTrigger>
        ) : null}
        <DialogContent
          className="top-[calc(var(--toolbar-h)+(100svh-var(--toolbar-h))/2)] h-[min(760px,calc(100svh-var(--toolbar-h)-2rem))] w-[calc(100%-0.5rem)] max-w-[430px] overflow-hidden bg-background p-0 shadow-lg sm:w-[calc(100vw-2rem)] sm:max-w-[1040px] xl:max-w-[1040px]"
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
            style={{ "--sidebar-width": "12.5rem" } as CSSProperties}
          >
            <div className="hidden h-full shrink-0 lg:flex">
              <SettingsSidebar active={active} onActiveChange={changeActive} />
            </div>
            <main className="flex h-full min-w-0 min-h-0 flex-1 flex-col overflow-hidden bg-background">
              <SettingsTopNav active={active} onActiveChange={changeActive} />
              <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border/70 bg-background/95">
                <div className="flex items-center gap-2 px-4 sm:px-6">
                  <h2 className="text-sm font-medium text-foreground">{t(activeSection.labelKey)}</h2>
                </div>
              </header>
              <div className="flex min-w-0 min-h-0 flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto px-3 pt-4 pb-5 sm:px-6 sm:pt-5 sm:pb-6">
                {active === "usage" ? <UsageSettings token={token} /> : null}
                {active === "dialogue" ? <GeneralSettings token={token} onDirtyChange={setActiveDirty} /> : null}
                {active === "voice" ? <VoiceSettings token={token} /> : null}
                {active === "model" ? (
                  <ProviderSettings
                    createNonce={createProviderNonce}
                    token={token}
                    onCreateHandled={handleCreateProviderHandled}
                  />
                ) : null}
                {active === "skills" ? <SkillsSettings token={token} /> : null}
                {active === "tools" ? <ToolsSettings token={token} onDirtyChange={setActiveDirty} /> : null}
                {active === "about" ? <AboutSettings token={token} /> : null}
              </div>
            </main>
          </SidebarProvider>
        </DialogContent>
      </Dialog>
      <UnsavedChangesAlert
        open={confirmationOpen}
        onDiscard={discardUnsavedChanges}
        onOpenChange={setConfirmationOpen}
      />
    </>
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
                  "inline-flex h-9 shrink-0 items-center gap-2 rounded-md px-3 text-sm",
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
