import { EllipsisVertical, KeyRound, Minus, Plus, Printer, RotateCcw, Search } from "@/components/icons";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import type { BrowserTab } from "@/api/client";
import {
  electronBrowserBridge,
  type ElectronBrowserCredentialState,
  type ElectronBrowserZoom,
} from "@/browser/electronBridge";
import { Spinner } from "@/components/Spinner";
import { ShellActionButton } from "@/components/ShellActionButton";
import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuItem as DropdownMenuItem,
  AppDropdownMenuSeparator as DropdownMenuSeparator,
} from "@/components/AppMenu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { openSettingsDialog } from "@/lib/settingsDialog";

const emptyCredentialState: ElectronBrowserCredentialState = {
  available: false,
  reason: "",
  origin: "",
  formDetected: false,
  credentials: [],
  prompt: null,
};

export function BrowserOptionsMenu({
  active,
  activeTab,
  sessionID,
  onOpenFind,
}: {
  active: boolean;
  activeTab?: BrowserTab;
  sessionID: string;
  onOpenFind: () => void;
}) {
  const { t } = useI18n();
  const [credentialState, setCredentialState] = useState<ElectronBrowserCredentialState>(emptyCredentialState);
  const [menuOpen, setMenuOpen] = useState(false);
  const [promptOpen, setPromptOpen] = useState(false);
  const [pendingAction, setPendingAction] = useState("");
  const [zoom, setZoom] = useState<ElectronBrowserZoom>({ factor: 1, percent: 100 });
  const shownPromptIDRef = useRef("");
  const bridge = electronBrowserBridge();
  const request = activeTab ? { sessionID, tabID: activeTab.id } : null;

  const loadCredentialState = useCallback(async () => {
    if (!active || !request || !bridge?.getCredentialState) {
      setCredentialState(emptyCredentialState);
      return;
    }
    setCredentialState(await bridge.getCredentialState(request));
  }, [active, activeTab?.id, bridge, sessionID]);

  useEffect(() => {
    void loadCredentialState().catch(() => setCredentialState(emptyCredentialState));
  }, [loadCredentialState]);

  useEffect(() => {
    if (!active) {
      setMenuOpen(false);
      setPromptOpen(false);
    }
  }, [active]);

  useEffect(() => {
    const promptID = credentialState.prompt?.id || "";
    if (!promptID) {
      shownPromptIDRef.current = "";
      setPromptOpen(false);
      return;
    }
    if (active && promptID !== shownPromptIDRef.current) {
      shownPromptIDRef.current = promptID;
      setMenuOpen(false);
      setPromptOpen(true);
    }
  }, [active, credentialState.prompt?.id]);

  useEffect(() => {
    if (!menuOpen || !request || !bridge?.getZoom) return;
    void bridge.getZoom(request).then(setZoom).catch(() => undefined);
  }, [activeTab?.id, bridge, menuOpen, sessionID]);

  useEffect(() => {
    if (!bridge) return;
    const stopState = bridge.onCredentialState?.((next) => {
      if (next.sessionID === sessionID && next.tabID === activeTab?.id) setCredentialState(next);
    });
    const stopChanged = bridge.onCredentialsChanged?.(() => {
      void loadCredentialState().catch(() => undefined);
    });
    const stopManage = bridge.onCredentialManage?.((manageRequest) => {
      if (manageRequest.sessionID !== sessionID || manageRequest.tabID !== activeTab?.id) return;
      setMenuOpen(false);
      setPromptOpen(false);
      openSettingsDialog({ section: "browser" });
    });
    return () => {
      stopState?.();
      stopChanged?.();
      stopManage?.();
    };
  }, [activeTab?.id, bridge, loadCredentialState, sessionID]);

  if (!activeTab || !bridge) return null;

  const runCredentialAction = async (name: string, operation: () => Promise<void>) => {
    setPendingAction(name);
    try {
      await operation();
    } catch (error) {
      toast.error(t("browser.passwordActionFailed"), { description: error instanceof Error ? error.message : String(error) });
    } finally {
      setPendingAction("");
    }
  };
  const changeZoom = (action: "in" | "out" | "reset") => {
    if (!request || !bridge.zoom) return;
    void bridge.zoom({ ...request, action }).then(setZoom).catch((error) => {
      toast.error(t("browser.zoomFailed"), { description: error instanceof Error ? error.message : String(error) });
    });
  };
  const print = () => {
    if (!request || !bridge.print) return;
    void bridge.print(request).then((result) => {
      if (!result.ok && !result.canceled) toast.error(t("browser.printFailed"), { description: result.reason });
    }).catch((error) => {
      toast.error(t("browser.printFailed"), { description: error instanceof Error ? error.message : String(error) });
    });
  };
  const prompt = credentialState.prompt;

  return (
    <Popover open={Boolean(prompt) && promptOpen} onOpenChange={setPromptOpen}>
      <PopoverAnchor asChild>
        <span className="inline-flex shrink-0">
          <DropdownMenu open={menuOpen} onOpenChange={(open) => {
            setMenuOpen(open);
            if (open) {
              setPromptOpen(false);
              void loadCredentialState().catch(() => undefined);
            }
          }}>
            <DropdownMenuTrigger asChild>
              <ShellActionButton
                aria-label={t("browser.options")}
                className="relative h-7 w-7 shrink-0 rounded-md text-muted-foreground hover:text-foreground"
                size="icon-sm"
                type="button"
              >
                <EllipsisVertical className="size-4" />
                {prompt ? <span className="absolute top-1 right-1 size-1.5 rounded-full bg-primary" /> : null}
              </ShellActionButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-72" sideOffset={7}>
              <DropdownMenuItem onSelect={onOpenFind}>
                <Search />
                {t("browser.findInPage")}
                <DropdownMenuShortcut>{navigator.platform.includes("Mac") ? "⌘F" : "Ctrl+F"}</DropdownMenuShortcut>
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={print}>
                <Printer />
                {t("browser.print")}
                <DropdownMenuShortcut>{navigator.platform.includes("Mac") ? "⌘P" : "Ctrl+P"}</DropdownMenuShortcut>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <div className="flex h-9 items-center gap-1 px-1.5 text-sm">
                <RotateCcw className="size-4 text-muted-foreground" />
                <span className="min-w-0 flex-1">{t("browser.zoom")}</span>
                <div className="flex items-center rounded-md border bg-muted/35">
                  <Button aria-label={t("browser.zoomOut")} className="size-7 rounded-r-none" size="icon-sm" type="button" variant="ghost" onClick={() => changeZoom("out")}>
                    <Minus className="size-3.5" />
                  </Button>
                  <Button aria-label={t("browser.zoomReset")} className="h-7 w-14 rounded-none border-x px-1 text-xs tabular-nums" size="sm" type="button" variant="ghost" onClick={() => changeZoom("reset")}>
                    {zoom.percent}%
                  </Button>
                  <Button aria-label={t("browser.zoomIn")} className="size-7 rounded-l-none" size="icon-sm" type="button" variant="ghost" onClick={() => changeZoom("in")}>
                    <Plus className="size-3.5" />
                  </Button>
                </div>
              </div>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => openSettingsDialog({ section: "browser" })}>
                <KeyRound />
                {t("browser.passwordManage")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </span>
      </PopoverAnchor>
      {prompt ? (
        <PopoverContent align="end" className="w-80 gap-0 overflow-hidden p-0" sideOffset={7}>
          <div className="space-y-1 px-4 pt-4 pb-3">
            <div className="font-medium">
              {prompt.kind === "update" ? t("browser.passwordUpdateTitle") : t("browser.passwordSaveTitle")}
            </div>
            <div className="truncate text-sm text-muted-foreground">{originLabel(prompt.origin)}</div>
            <div className="truncate pt-1 text-sm">{prompt.username || originLabel(prompt.origin)}</div>
            <div className="flex justify-end gap-2 pt-3">
              <Button
                size="sm"
                type="button"
                variant="ghost"
                onClick={() => void runCredentialAction("dismiss", async () => {
                  await bridge.dismissCredential?.({ ...request!, pendingID: prompt.id });
                  await loadCredentialState();
                })}
              >
                {t("browser.passwordNotNow")}
              </Button>
              <Button
                size="sm"
                type="button"
                onClick={() => void runCredentialAction("save", async () => {
                  await bridge.saveCredential?.({ ...request!, pendingID: prompt.id });
                  await loadCredentialState();
                })}
              >
                {pendingAction === "save" ? <Spinner /> : prompt.kind === "update" ? t("browser.passwordUpdate") : t("browser.passwordSave")}
              </Button>
            </div>
          </div>
          <div className="border-t px-2 py-1.5">
            <Button
              className="h-8 w-full justify-start px-2 text-xs text-muted-foreground"
              size="sm"
              type="button"
              variant="ghost"
              onClick={() => void runCredentialAction("never", async () => {
                await bridge.dismissCredential?.({ ...request!, pendingID: prompt.id, neverSave: true });
                await loadCredentialState();
              })}
            >
              {t("browser.passwordNever")}
            </Button>
          </div>
        </PopoverContent>
      ) : null}
    </Popover>
  );
}

function originLabel(origin: string) {
  try {
    return new URL(origin).host;
  } catch {
    return origin;
  }
}
