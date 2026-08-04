import { ChevronDown, ChevronUp, Search, X } from "@/components/icons";
import { useEffect, useRef, useState } from "react";

import { electronBrowserBridge } from "@/browser/electronBridge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";

export function BrowserFindBar({
  open,
  sessionID,
  tabID,
  onOpenChange,
}: {
  open: boolean;
  sessionID: string;
  tabID?: string;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [activeMatch, setActiveMatch] = useState(0);
  const [matches, setMatches] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const bridge = electronBrowserBridge();
  const request = tabID ? { sessionID, tabID } : null;

  useEffect(() => {
    if (!open) return;
    window.requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape" || event.isComposing) return;
      event.preventDefault();
      onOpenChange(false);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onOpenChange, open]);

  useEffect(() => {
    if (!bridge?.onFoundInPage) return;
    return bridge.onFoundInPage((result) => {
      if (result.sessionID !== sessionID || result.tabID !== tabID) return;
      setActiveMatch(result.activeMatchOrdinal);
      setMatches(result.matches);
    });
  }, [bridge, sessionID, tabID]);

  useEffect(() => {
    setActiveMatch(0);
    setMatches(0);
    if (!open || !request || !bridge?.findInPage || !bridge.stopFindInPage) return;
    if (!query) {
      void bridge.stopFindInPage(request).catch(() => undefined);
      return;
    }
    const timer = window.setTimeout(() => {
      void bridge.findInPage?.({ ...request, text: query }).catch(() => undefined);
    }, 80);
    return () => window.clearTimeout(timer);
  }, [bridge, open, query, sessionID, tabID]);

  useEffect(() => {
    const stopFindInPage = bridge?.stopFindInPage;
    if (!open || !request || !stopFindInPage) return;
    return () => {
      void stopFindInPage(request).catch(() => undefined);
    };
  }, [bridge, open, sessionID, tabID]);

  if (!open || !tabID) return null;

  const findNext = (forward: boolean) => {
    if (!query || !request || !bridge?.findInPage) return;
    void bridge.findInPage({ ...request, text: query, findNext: true, forward }).catch(() => undefined);
  };
  const close = () => {
    onOpenChange(false);
  };

  return (
    <div className="absolute top-2 right-3 z-40 flex h-10 w-[22rem] max-w-[calc(100%-1.5rem)] items-center gap-1 rounded-lg border bg-popover px-2 text-popover-foreground shadow-lg">
      <Search className="size-4 shrink-0 text-muted-foreground" />
      <Input
        ref={inputRef}
        aria-label={t("browser.findPlaceholder")}
        className="h-8 min-w-0 flex-1 border-0 bg-transparent px-1 shadow-none focus-visible:ring-0 dark:bg-transparent"
        placeholder={t("browser.findPlaceholder")}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            findNext(!event.shiftKey);
          }
        }}
      />
      {matches > 0 ? (
        <span className="min-w-12 text-center text-xs tabular-nums text-muted-foreground">
          {activeMatch}/{matches}
        </span>
      ) : null}
      <Button aria-label={t("browser.findPrevious")} className="size-7" disabled={!query} size="icon-sm" type="button" variant="ghost" onClick={() => findNext(false)}>
        <ChevronUp className="size-4" />
      </Button>
      <Button aria-label={t("browser.findNext")} className="size-7" disabled={!query} size="icon-sm" type="button" variant="ghost" onClick={() => findNext(true)}>
        <ChevronDown className="size-4" />
      </Button>
      <Button aria-label={t("browser.findClose")} className="size-7" size="icon-sm" type="button" variant="ghost" onClick={close}>
        <X className="size-4" />
      </Button>
    </div>
  );
}
