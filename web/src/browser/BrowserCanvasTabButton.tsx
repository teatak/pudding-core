import { Compass, FileCode2, Loader2, SquareTerminal, X } from "lucide-react";
import { useEffect, useState } from "react";

import type { BrowserTab, Terminal } from "@/api/client";
import { browserTabFaviconURL, browserTabTitle } from "@/browser/helpers";
import type { CanvasSurface } from "@/browser/types";
import { useHorizontalScrollMask } from "@/hooks/useHorizontalScrollMask";
import { useI18n } from "@/i18n";

function BrowserTabIcon({ faviconURL }: { faviconURL?: string }) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [faviconURL]);

  if (faviconURL && !failed) {
    return (
      <span aria-hidden="true" className="inline-flex h-(--canvas-toolbar-tab-icon) w-(--canvas-toolbar-tab-icon) shrink-0 items-center justify-center overflow-hidden rounded-[5px]">
        <img alt="" className="h-full w-full object-cover" draggable={false} src={faviconURL} onError={() => setFailed(true)} />
      </span>
    );
  }

  return (
    <span aria-hidden="true" className="inline-flex h-(--canvas-toolbar-tab-icon) w-(--canvas-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px] bg-blue-600 text-white shadow-sm">
      <Compass className="h-3.5 w-3.5" />
    </span>
  );
}

function TerminalTabIcon({ exited }: { exited: boolean }) {
  return (
    <span
      aria-hidden="true"
      className="inline-flex h-(--canvas-toolbar-tab-icon) w-(--canvas-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px] bg-zinc-900 text-white shadow-sm dark:bg-zinc-100 dark:text-zinc-900"
      data-exited={exited}
    >
      <SquareTerminal className="h-3.5 w-3.5" />
    </span>
  );
}

function FilePreviewTabIcon() {
  return (
    <span
      aria-hidden="true"
      className="inline-flex h-(--canvas-toolbar-tab-icon) w-(--canvas-toolbar-tab-icon) shrink-0 items-center justify-center rounded-[5px] bg-blue-600 text-white shadow-sm"
    >
      <FileCode2 className="h-3.5 w-3.5" />
    </span>
  );
}

export type CanvasFilePreviewTab = {
  id: string;
  label: string;
  openedAt: number;
  path: string;
};

type SurfaceTab =
  | { kind: "browser"; id: string; sortAt: number; browser: BrowserTab }
  | { kind: "terminal"; id: string; sortAt: number; terminal: Terminal }
  | { kind: "file"; id: string; sortAt: number; file: CanvasFilePreviewTab };

export function CanvasSurfaceTabs({
  activeBrowserTabID,
  activeFilePreviewID,
  activeSurface,
  activeTerminalID,
  browserTabs,
  closingBrowserTabID,
  closingTerminalID,
  filePreviewActive,
  filePreviewTabs,
  terminalTabs,
  onCloseBrowser,
  onCloseFilePreview,
  onCloseTerminal,
  onSelectBrowser,
  onSelectFilePreview,
  onSelectTerminal,
}: {
  activeBrowserTabID?: string;
  activeFilePreviewID?: string;
  activeSurface: CanvasSurface;
  activeTerminalID?: string;
  browserTabs: BrowserTab[];
  closingBrowserTabID?: string;
  closingTerminalID?: string;
  filePreviewActive: boolean;
  filePreviewTabs: CanvasFilePreviewTab[];
  terminalTabs: Terminal[];
  onCloseBrowser: (tabID: string) => void;
  onCloseFilePreview: (previewID: string) => void;
  onCloseTerminal: (terminalID: string) => void;
  onSelectBrowser: (tabID: string) => void;
  onSelectFilePreview: (previewID: string) => void;
  onSelectTerminal: (terminalID: string) => void;
}) {
  const { t } = useI18n();
  const scrollMask = useHorizontalScrollMask<HTMLDivElement>();
  const tabs: SurfaceTab[] = [
    ...browserTabs.map((browser) => ({ kind: "browser" as const, id: browser.id, sortAt: Date.parse(browser.createdAt), browser })),
    ...terminalTabs.map((terminal) => ({ kind: "terminal" as const, id: terminal.id, sortAt: Date.parse(terminal.createdAt), terminal })),
    ...filePreviewTabs.map((file) => ({ kind: "file" as const, id: file.id, sortAt: file.openedAt, file })),
  ].sort((left, right) => left.sortAt - right.sortAt || left.id.localeCompare(right.id));
  return (
    <div
      ref={scrollMask.ref}
      className="no-drag-region w-fit max-w-full min-w-0 overflow-x-auto overflow-y-hidden rounded-lg bg-muted p-(--canvas-toolbar-tab-padding) text-muted-foreground [scrollbar-width:none] [-webkit-overflow-scrolling:touch] [&::-webkit-scrollbar]:hidden"
      style={scrollMask.style}
    >
      <div className="inline-flex min-w-max items-center gap-1">
        {tabs.map((tab) => {
          const browser = tab.kind === "browser" ? tab.browser : undefined;
          const terminal = tab.kind === "terminal" ? tab.terminal : undefined;
          const file = tab.kind === "file" ? tab.file : undefined;
          const label = browser
            ? browserTabTitle(browser, t("browser.newTab"), t("browser.newTab"))
            : terminal
              ? terminalTabTitle(terminal, t("terminal.newTab"))
              : file?.label || t("terminal.newTab");
          const selected =
            (tab.kind === "browser" && activeSurface === "browser" && tab.id === activeBrowserTabID) ||
            (tab.kind === "terminal" && activeSurface === "terminal" && tab.id === activeTerminalID) ||
            (tab.kind === "file" && filePreviewActive && tab.id === activeFilePreviewID);
          const closePending =
            (tab.kind === "browser" && tab.id === closingBrowserTabID) ||
            (tab.kind === "terminal" && tab.id === closingTerminalID);
          const exited = terminal?.status === "exited";
          return (
            <button
              key={`${tab.kind}:${tab.id}`}
              aria-label={label}
              aria-selected={selected}
              className="group inline-flex h-(--canvas-toolbar-tab-h) min-w-24 max-w-[44vw] shrink-0 items-center gap-1.5 rounded-md border border-transparent px-2 text-xs font-medium whitespace-nowrap transition-colors data-[active=true]:bg-background data-[active=true]:text-foreground data-[active=true]:shadow-sm hover:bg-background hover:text-foreground sm:max-w-40"
              data-active={selected}
              disabled={closePending}
              title={exited ? `${label} · ${t("terminal.exited")}` : file?.path || label}
              type="button"
              onClick={() => {
                if (tab.kind === "browser") {
                  onSelectBrowser(tab.id);
                } else if (tab.kind === "terminal") {
                  onSelectTerminal(tab.id);
                } else {
                  onSelectFilePreview(tab.id);
                }
              }}
            >
              {browser ? (
                <BrowserTabIcon faviconURL={browserTabFaviconURL(browser)} />
              ) : terminal ? (
                <TerminalTabIcon exited={exited} />
              ) : (
                <FilePreviewTabIcon />
              )}
              <span className="min-w-0 max-w-24 flex-1 truncate text-left">{label}</span>
              <span
                aria-label={
                  tab.kind === "browser"
                    ? t("browser.release")
                    : tab.kind === "terminal"
                      ? t("terminal.close")
                      : t("canvas.filePreviewClose")
                }
                className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded opacity-70 hover:bg-muted-foreground/20 hover:opacity-100"
                role="button"
                tabIndex={-1}
                onClick={(event) => {
                  event.stopPropagation();
                  if (tab.kind === "browser") {
                    onCloseBrowser(tab.id);
                  } else if (tab.kind === "terminal") {
                    onCloseTerminal(tab.id);
                  } else {
                    onCloseFilePreview(tab.id);
                  }
                }}
              >
                {closePending ? <Loader2 className="h-3 w-3 animate-spin" /> : <X className="h-3 w-3" />}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function terminalTabTitle(terminal: Terminal, fallback: string) {
  const title = (terminal.title || "").trim();
  const shellName = basename(terminal.shell);
  if (title && title !== shellName) {
    return title;
  }
  return basename(terminal.cwd) || title || fallback;
}

function basename(path: string) {
  return path.replace(/[/\\]+$/, "").split(/[/\\]/).pop()?.trim() || "";
}
