import { ChevronDown, ChevronUp } from "@/components/icons";
import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { BrowserFindBar } from "@/browser/BrowserFindBar";
import {
  BrowserViewportOverlay,
  BrowserViewportPlaceholder,
} from "@/browser/BrowserRuntimeProvider";
import { BrowserToolbar } from "@/browser/BrowserToolbar";
import { electronBrowserBridge } from "@/browser/electronBridge";
import {
  activateBrowserPageFindRegion,
  closeFind,
  closeOpenFind,
  openFind,
  registerBrowserPageFind,
} from "@/browser/pageFindTarget";
import type { ElectronBrowserSurfaceTab } from "@/browser/useElectronRequiredBrowserTabs";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

export const BrowserWorkspaceSurface = memo(function BrowserWorkspaceSurface({
  active,
  activeTabID,
  pending,
  sessionID,
  tabs,
  token,
}: {
  active: boolean;
  activeTabID?: string;
  pending: boolean;
  sessionID: string;
  tabs: ElectronBrowserSurfaceTab[];
  token: string;
}) {
  const { t } = useI18n();
  const [consoleCollapsed, setConsoleCollapsed] = useState(false);
  const [findOpen, setFindOpen] = useState(false);
  const activeTab = tabs.find((tab) => tab.id === activeTabID) || tabs[0];
  const browserKey = `${sessionID}:${activeTab?.id || "empty"}`;
  const findTarget = useMemo(() => ({
    id: `browser:${browserKey}`,
    open: () => setFindOpen(true),
    close: () => setFindOpen(false),
  }), [browserKey]);
  const openBrowserFind = useCallback(() => openFind(findTarget), [findTarget]);
  const changeBrowserFindOpen = useCallback((open: boolean) => {
    if (open) {
      openFind(findTarget);
      return;
    }
    closeFind(findTarget.id);
  }, [findTarget]);

  useEffect(() => {
    closeFind(findTarget.id);
  }, [activeTab?.url, findTarget.id]);

  useEffect(() => {
    if (!active || !activeTab) return;
    return registerBrowserPageFind(findTarget);
  }, [active, activeTab?.id, findTarget]);

  useEffect(() => {
    if (!active || !activeTab) return;
    return electronBrowserBridge()?.onInteraction?.((event) => {
      if (event.sessionID !== sessionID || event.tabID !== activeTab.id) return;
      activateBrowserPageFindRegion();
      if (event.key === "Escape") closeOpenFind();
    });
  }, [active, activeTab?.id, findTarget.id, sessionID]);

  useEffect(() => {
    if (!active || !activeTab) return;
    const handleShortcut = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.altKey || (!event.metaKey && !event.ctrlKey)) return;
      if (document.querySelector('[data-slot="dialog-content"][data-state="open"]')) return;
      const key = event.key.toLowerCase();
      if (key === "p") {
        event.preventDefault();
        const bridge = electronBrowserBridge();
        if (!bridge?.print) return;
        void bridge.print({ sessionID, tabID: activeTab.id }).then((result) => {
          if (!result.ok && !result.canceled) toast.error(t("browser.printFailed"), { description: result.reason });
        }).catch((error) => {
          toast.error(t("browser.printFailed"), { description: error instanceof Error ? error.message : String(error) });
        });
      }
    };
    window.addEventListener("keydown", handleShortcut);
    return () => window.removeEventListener("keydown", handleShortcut);
  }, [active, activeTab?.id, sessionID, t]);

  return (
    <>
      <div
        aria-hidden={!active}
        className={cn(
          "pudding-browser-workspace-surface pointer-events-none absolute inset-0 z-30 min-h-0 overflow-hidden text-card-foreground shadow-none",
          !active && "hidden",
        )}
        data-console-collapsed={consoleCollapsed}
        onFocusCapture={activateBrowserPageFindRegion}
        onPointerDownCapture={activateBrowserPageFindRegion}
      >
        <div className="pudding-browser-workspace-viewport absolute inset-0 flex min-h-0 flex-col overflow-hidden border-t border-[var(--workspace-border)]">
          <div className="canvas-window-drag-handle pointer-events-auto flex h-9 shrink-0 cursor-default items-center gap-2 border-b border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] px-3">
            <BrowserToolbar
              key={`toolbar:${browserKey}`}
              active={active}
              activeTab={activeTab}
              sessionID={sessionID}
              token={token}
              onOpenFind={openBrowserFind}
            />
          </div>
          <div className="relative min-h-0 flex-1 overflow-hidden">
            <BrowserViewportPlaceholder
              active={active && Boolean(activeTab)}
              sessionID={sessionID}
              tabID={activeTab?.id}
            />
            {tabs.length === 0 && pending ? <BrowserLoading /> : null}
          </div>
        </div>
      </div>
      <BrowserViewportOverlay
        active={active && Boolean(activeTab)}
        sessionID={sessionID}
        tabID={activeTab?.id}
      >
        <div className="pointer-events-auto">
          <BrowserFindBar
            key={`find:${browserKey}`}
            open={findOpen}
            sessionID={sessionID}
            tabID={activeTab?.id}
            onOpenChange={changeBrowserFindOpen}
          />
        </div>
        <Button
          aria-label={t(consoleCollapsed ? "agentConsole.showBrowserBar" : "agentConsole.hideBrowserBar")}
          className="pudding-browser-console-toggle pointer-events-auto absolute right-3 shrink-0 rounded-full border border-border/70 shadow-sm"
          data-console-collapsed={consoleCollapsed}
          size="icon-sm"
          type="button"
          variant="secondary"
          onClick={() => setConsoleCollapsed((collapsed) => !collapsed)}
        >
          {consoleCollapsed ? <ChevronUp /> : <ChevronDown />}
        </Button>
      </BrowserViewportOverlay>
    </>
  );
});

function BrowserLoading() {
  const { t } = useI18n();
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-[var(--workspace-chrome-background)] text-sm text-muted-foreground">
      <Spinner className="mr-2 h-4 w-4" />
      {t("browser.loading")}
    </div>
  );
}
