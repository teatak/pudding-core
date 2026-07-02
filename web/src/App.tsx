import { useQueryClient } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { PanelRight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useGroupRef } from "react-resizable-panels";
import { toast } from "sonner";

import { queryKeys } from "@/api/queryKeys";
import { CanvasPane } from "@/components/CanvasPane";
import { ChatPane } from "@/components/ChatPane";
import { AppsPane } from "@/components/AppsPane";
import { SessionRail } from "@/components/SessionRail";
import { SettingsDialog } from "@/components/SettingsDialog";
import { PairingGate, TokenGate } from "@/components/TokenGate";
import { claimMobilePairing } from "@/api/client";
import { Button } from "@/components/ui/button";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Toaster } from "@/components/ui/sonner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { WorkspaceResizableHandle } from "@/components/WorkspaceResizableHandle";
import { useIsMobile } from "@/hooks/use-mobile";
import { useVisibleSessionEvents } from "@/hooks/useSessionEvents";
import { translate, useI18n } from "@/i18n";
import {
  layoutStorageKeys,
  resizeTargetMinimumSize,
  splitLayout,
  workspaceLayout,
} from "@/lib/layoutConstants";
import { readPanelLayout, savePanelLayout } from "@/lib/panelLayout";
import { useCanvasMCP } from "@/mcp/canvasTools";
import { setCanvasOpen, useCanvasOpen } from "@/state/canvasStore";
import { clearPendingPairingCode, pendingPairingCode } from "@/state/token";
import { setToken, useToken } from "@/state/tokenStore";

const APP_OAUTH_CONNECTED_EVENT = "pudding:app-oauth-connected";

export function App() {
  const token = useToken();
  const { session: selectedSessionID, draft, split: splitSessionID, view } = useSearch({ from: "/" });
  const { locale, t } = useI18n();
  const queryClient = useQueryClient();
  const isMobile = useIsMobile();
  const canvasOpen = useCanvasOpen();
  const [pairingCode] = useState(() => pendingPairingCode());
  const [pairingFailed, setPairingFailed] = useState(false);
  const workspaceGroupRef = useGroupRef();
  const splitGroupRef = useGroupRef();
  const savedSplitLayout = useMemo(
    () =>
      readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
        minPercent: splitLayout.minPercent,
        maxPercent: splitLayout.maxPercent,
      }),
    [],
  );
  const savedWorkspaceLayout = useMemo(
    () =>
      readPanelLayout(layoutStorageKeys.workspaceRatio, workspaceLayout.fallback, {
        minPercent: workspaceLayout.minPercent,
        maxPercent: workspaceLayout.maxPercent,
      }),
    [],
  );
  // 上下分屏(docs/design.md 2.2):pane 三件套整体复用,路由是唯一事实源;
  // split 与主 pane 相同的会话不重复渲染
  const appsActive = view === "apps";
  const showSplit = !appsActive && Boolean(splitSessionID && splitSessionID !== selectedSessionID);
  const draftActive = !appsActive && draft === "1" && !selectedSessionID;
  const activeSessionIDs = (appsActive ? [] : [selectedSessionID, showSplit ? splitSessionID : undefined]).filter(
    (sessionID): sessionID is string => Boolean(sessionID),
  );

  // SSE 是 session-scoped,不是 pane-scoped。visible sessions 在 App 层统一去重订阅,
  // ChatPane 只负责 pane-local UI/滚动状态。
  useVisibleSessionEvents(activeSessionIDs, token);
  useCanvasMCP(token);

  useEffect(() => {
    if (!token) {
      return;
    }
    let off: (() => void) | undefined;
    let cancelled = false;
    void import("@wailsio/runtime")
      .then(({ Events }) => {
        if (cancelled) {
          return;
        }
        off = Events.On(APP_OAUTH_CONNECTED_EVENT, (event) => {
          const payload = event.data as { ok?: boolean } | undefined;
          if (payload?.ok === false) {
            toast.error(translate("apps.oauthFailed", locale));
          } else {
            toast.success(translate("apps.oauthConnected", locale));
          }
          void Promise.all([
            queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
            queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() }),
          ]);
        });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      off?.();
    };
  }, [locale, queryClient, token]);

  useEffect(() => {
    if (token) {
      return;
    }
    if (!pairingCode) {
      return;
    }
    let cancelled = false;
    void claimMobilePairing(pairingCode, { deviceName: navigator.userAgent || "Mobile device" })
      .then((result) => {
        if (cancelled) {
          return;
        }
        clearPendingPairingCode();
        setToken(result.token);
      })
      .catch(() => {
        if (!cancelled) {
          clearPendingPairingCode();
          setPairingFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [pairingCode, token]);

  useEffect(() => {
    if (isMobile) {
      return;
    }
    const group = workspaceGroupRef.current;
    if (!group) {
      return;
    }
    if (!canvasOpen) {
      group.setLayout(workspaceLayout.closed);
      return;
    }
    group.setLayout(
      readPanelLayout(layoutStorageKeys.workspaceRatio, workspaceLayout.fallback, {
        minPercent: workspaceLayout.minPercent,
        maxPercent: workspaceLayout.maxPercent,
      }),
    );
  }, [canvasOpen, isMobile, workspaceGroupRef]);

  useEffect(() => {
    const group = splitGroupRef.current;
    if (!group) {
      return;
    }
    const showSplit = Boolean(splitSessionID && splitSessionID !== selectedSessionID);
    if (!showSplit) {
      group.setLayout(splitLayout.closed);
      return;
    }
    group.setLayout(
      readPanelLayout(layoutStorageKeys.splitRatio, splitLayout.fallback, {
        minPercent: splitLayout.minPercent,
        maxPercent: splitLayout.maxPercent,
      }),
    );
  }, [selectedSessionID, splitGroupRef, splitSessionID]);

  if (!token) {
    if (pairingCode) {
      return <PairingGate failed={pairingFailed} />;
    }
    return <TokenGate />;
  }

  const chatArea = (
    <main className="flex h-full min-w-0 flex-col overflow-hidden bg-background">
      <ResizablePanelGroup
        className="min-h-0 flex-1"
        defaultLayout={showSplit ? savedSplitLayout : splitLayout.closed}
        groupRef={splitGroupRef}
        id="split-workspace"
        orientation="vertical"
        resizeTargetMinimumSize={resizeTargetMinimumSize}
        onLayoutChanged={(layout) => {
          if (showSplit && typeof layout.split === "number" && layout.split > 0) {
            savePanelLayout(layoutStorageKeys.splitRatio, layout);
          }
        }}
      >
        <ResizablePanel
          id="primary"
          className="min-h-0"
          minSize={splitLayout.minPanePx}
        >
          <ChatPane
            reserveTopRightAction={!canvasOpen}
            token={token}
            sessionID={selectedSessionID}
            draftActive={draftActive}
            role="primary"
          />
        </ResizablePanel>
        <WorkspaceResizableHandle
          aria-label={t("layout.resizeHint")}
          className={showSplit ? undefined : "hidden"}
          disabled={!showSplit}
        />
        <ResizablePanel
          id="split"
          className="min-h-0"
          collapsedSize="0%"
          collapsible
          minSize={splitLayout.minPanePx}
        >
          {showSplit ? <ChatPane token={token} sessionID={splitSessionID} role="split" /> : null}
        </ResizablePanel>
      </ResizablePanelGroup>
    </main>
  );

  return (
    <TooltipProvider delayDuration={250}>
      <div className="relative flex h-[100svh] overflow-hidden">
        <div aria-hidden="true" className="drag-region absolute inset-x-0 top-0 z-20 h-(--toolbar-h)" />
        <SessionRail
          activeSessionIDs={activeSessionIDs}
          draftActive={draftActive}
          selectedSessionID={appsActive ? undefined : selectedSessionID}
          token={token}
        />
        {/* rail 固定宽度,不参与 resize。核心工作区只在 chat/canvas 之间分配空间。 */}
        <div className="relative h-full min-w-0 flex-1 bg-background">
          {appsActive ? (
            <AppsPane token={token} />
          ) : (
            <>
          <div className="pudding-canvas-toggle no-drag-region absolute top-0 right-[13px] z-40 flex h-(--toolbar-h) items-center">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  aria-label={t("canvas.toggle")}
                  aria-pressed={canvasOpen}
                  size="icon-sm"
                  tabIndex={-1}
                  variant="ghost"
                  onClick={() => setCanvasOpen(!canvasOpen)}
                >
                  <PanelRight />
                </Button>
              </TooltipTrigger>
              <TooltipContent align="end" side="bottom">
                {t("canvas.toggle")}
              </TooltipContent>
            </Tooltip>
          </div>
          {isMobile ? (
            <>
              {chatArea}
              <Sheet open={canvasOpen} onOpenChange={setCanvasOpen}>
                <SheetContent
                  className="w-[min(28rem,92vw)] max-w-none gap-0 p-0"
                  side="right"
                >
                  <SheetHeader className="sr-only">
                    <SheetTitle>{t("canvas.title")}</SheetTitle>
                    <SheetDescription>{t("canvas.empty")}</SheetDescription>
                  </SheetHeader>
                  <CanvasPane token={token} sessionID={selectedSessionID} />
                </SheetContent>
              </Sheet>
            </>
          ) : (
            <ResizablePanelGroup
              className="h-full min-w-0"
              defaultLayout={canvasOpen ? savedWorkspaceLayout : workspaceLayout.closed}
              groupRef={workspaceGroupRef}
              id="workspace"
              orientation="horizontal"
              resizeTargetMinimumSize={resizeTargetMinimumSize}
              onLayoutChanged={(layout) => {
                if (canvasOpen && typeof layout.canvas === "number" && layout.canvas > 0) {
                  savePanelLayout(layoutStorageKeys.workspaceRatio, layout);
                }
              }}
            >
              <ResizablePanel
                id="chat"
                className="min-w-0"
                minSize={workspaceLayout.minChatPx}
              >
                {chatArea}
              </ResizablePanel>
              <WorkspaceResizableHandle
                id="chat-canvas"
                aria-label={t("layout.resizeHint")}
                className={canvasOpen ? undefined : "hidden"}
                disabled={!canvasOpen}
              />
              <ResizablePanel
                id="canvas"
                className="min-w-0"
                collapsedSize="0%"
                collapsible
                minSize={workspaceLayout.minCanvasPx}
              >
                {canvasOpen ? <CanvasPane token={token} sessionID={selectedSessionID} /> : null}
              </ResizablePanel>
            </ResizablePanelGroup>
          )}
            </>
          )}
        </div>
      </div>
      <SettingsDialog token={token} showTrigger={false} />
      <Toaster />
    </TooltipProvider>
  );
}
