import { Compass, Loader2, X } from "lucide-react";
import { useEffect, useState } from "react";

import type { BrowserTab } from "@/api/client";
import { browserTabFaviconURL, browserTabTitle } from "@/browser/helpers";
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

export function BrowserCanvasTabs({
  active,
  activeTabID,
  closingTabID,
  tabs,
  onClose,
  onSelect,
}: {
  active: boolean;
  activeTabID?: string;
  closingTabID?: string;
  tabs: BrowserTab[];
  onClose: (tabID: string) => void;
  onSelect: (tabID: string) => void;
}) {
  const { t } = useI18n();
  const scrollMask = useHorizontalScrollMask<HTMLDivElement>();
  return (
    <div
      ref={scrollMask.ref}
      className="no-drag-region w-fit max-w-full min-w-0 overflow-x-auto overflow-y-hidden rounded-lg bg-muted p-(--canvas-toolbar-tab-padding) text-muted-foreground [scrollbar-width:none] [-webkit-overflow-scrolling:touch] [&::-webkit-scrollbar]:hidden"
      style={scrollMask.style}
    >
      <div className="inline-flex min-w-max items-center gap-1">
        {tabs.map((tab) => {
          const label = browserTabTitle(tab, t("browser.newTab"), t("browser.newTab"));
          const selected = active && tab.id === activeTabID;
          const closePending = tab.id === closingTabID;
          return (
            <button
              key={tab.id}
              aria-label={label}
              aria-selected={selected}
              className="group inline-flex h-(--canvas-toolbar-tab-h) min-w-24 max-w-[44vw] shrink-0 items-center gap-1.5 rounded-md border border-transparent px-2 text-xs font-medium whitespace-nowrap transition-colors data-[active=true]:bg-background data-[active=true]:text-foreground data-[active=true]:shadow-sm hover:bg-background hover:text-foreground sm:max-w-40"
              data-active={selected}
              disabled={closePending}
              title={label}
              type="button"
              onClick={() => onSelect(tab.id)}
            >
              <BrowserTabIcon faviconURL={browserTabFaviconURL(tab)} />
              <span className="min-w-0 max-w-24 flex-1 truncate text-left">{label}</span>
              <span
                aria-label={t("browser.release")}
                className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded opacity-70 hover:bg-muted-foreground/20 hover:opacity-100"
                role="button"
                tabIndex={-1}
                onClick={(event) => {
                  event.stopPropagation();
                  onClose(tab.id);
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
