import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { clearBrowserHistory, deleteBrowserHistoryEntry, listBrowserHistory, type BrowserHistoryEntry } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { CornerDownLeft, Globe, History, Trash2 } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ConfirmationDialog";
import { Button } from "@/components/ui/button";
import { Command, CommandGroup, CommandItem, CommandList } from "@/components/ui/command";
import { Input } from "@/components/ui/input";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { BrowserFavicon } from "./BrowserFavicon";
import { browserOpenErrorDescription } from "./browserErrors";
import { browserCompactURL } from "./helpers";

export function BrowserAddressField({ active = true, sessionID, token, value, onChange, onSubmit, pending = false }: {
  active?: boolean;
  sessionID: string;
  token: string;
  value: string;
  onChange: (value: string) => void;
  onSubmit: (address: string) => void;
  pending?: boolean;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historySearch, setHistorySearch] = useState("");
  const [selectedHistoryIndex, setSelectedHistoryIndex] = useState(-1);
  const [clearHistoryOpen, setClearHistoryOpen] = useState(false);
  const historySearchTerm = historySearch.trim();
  const addressInputRef = useRef<HTMLInputElement>(null);
  const addressBlurTimerRef = useRef<number | undefined>(undefined);
  const historyQuery = useQuery({
    enabled: Boolean(active && !pending && historyOpen && historySearchTerm && token && sessionID),
    queryKey: queryKeys.browserHistory(historySearchTerm),
    queryFn: () => listBrowserHistory(token, sessionID, historySearchTerm, 10),
    staleTime: 0,
  });
  const historyCandidates = historyQuery.data?.history || [];
  const historyEntries = historySearchTerm ? historyCandidates.slice(0, 10) : [];
  const historyVisible = active && !pending && historyOpen && historyEntries.length > 0;

  useEffect(() => {
    setSelectedHistoryIndex((current) => Math.min(current, historyEntries.length - 1));
  }, [historyEntries.length]);

  useEffect(() => () => window.clearTimeout(addressBlurTimerRef.current), []);

  useEffect(() => {
    if (!active) {
      setHistoryOpen(false);
      setClearHistoryOpen(false);
      return;
    }
    const focusAddressInput = () => {
      const input = addressInputRef.current;
      input?.focus();
      input?.select();
      setHistorySearch("");
      setSelectedHistoryIndex(-1);
      setHistoryOpen(false);
    };
    const focusAddressBar = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "l" || (!event.metaKey && !event.ctrlKey) || event.altKey) {
        return;
      }
      event.preventDefault();
      focusAddressInput();
    };
    window.addEventListener("keydown", focusAddressBar);
    return () => window.removeEventListener("keydown", focusAddressBar);
  }, [active]);

  const deleteHistoryMutation = useMutation({
    mutationFn: (historyID: string) => deleteBrowserHistoryEntry(token, sessionID, historyID),
    onSuccess: () => {
      setSelectedHistoryIndex(-1);
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
    },
    onError: (error) => {
      toast.error(t("browser.historyDeleteFailed"), { description: browserOpenErrorDescription(null, error) });
    },
  });
  const clearHistoryMutation = useMutation({
    mutationFn: () => clearBrowserHistory(token, sessionID),
    onSuccess: () => {
      setClearHistoryOpen(false);
      setHistoryOpen(false);
      void queryClient.invalidateQueries({ queryKey: queryKeys.browserHistory() });
    },
    onError: (error) => {
      toast.error(t("browser.historyClearFailed"), { description: browserOpenErrorDescription(null, error) });
    },
  });
  const submitAddress = (address: string) => {
    if (pending || !address.trim()) return;
    setHistoryOpen(false);
    onSubmit(address);
  };
  const selectHistoryEntry = (entry: BrowserHistoryEntry) => submitAddress(entry.url);

  return (
    <form className="min-w-0 flex-1" onSubmit={(event) => {
      event.preventDefault();
      submitAddress(value);
    }}>
      <Popover
        open={historyVisible}
        onOpenChange={(open) => setHistoryOpen(open && Boolean(historySearch.trim()))}
      >
        <PopoverAnchor asChild>
          <div className={cn(
            "group relative flex min-w-0 items-center rounded-full border transition-colors focus-within:border-ring",
            "h-8 border-transparent bg-muted/70 hover:bg-muted focus-within:bg-muted dark:bg-[#1f1f1f] dark:hover:bg-[#232323] dark:focus-within:bg-[#1f1f1f]",
          )}>
            <Input
              ref={addressInputRef}
              aria-label={t("browser.urlPlaceholder")}
              aria-autocomplete="list"
              aria-expanded={historyVisible}
              className={cn(
                "min-w-0 flex-1 border-0 bg-transparent shadow-none focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent",
                "h-7 pr-8",
              )}
              placeholder={t("browser.urlPlaceholder")}
              value={value}
              onBlur={() => {
                window.clearTimeout(addressBlurTimerRef.current);
                addressBlurTimerRef.current = window.setTimeout(() => {
                  const focused = document.activeElement;
                  if (focused === addressInputRef.current || (focused instanceof Element && focused.closest('[data-slot="popover-content"]'))) {
                    return;
                  }
                  setHistoryOpen(false);
                }, 0);
              }}
              onChange={(event) => {
                onChange(event.target.value);
                setHistorySearch(event.target.value);
                setSelectedHistoryIndex(-1);
                setHistoryOpen(Boolean(event.target.value.trim()));
              }}
              onFocus={(event) => {
                window.clearTimeout(addressBlurTimerRef.current);
                const input = event.currentTarget;
                setHistorySearch("");
                setSelectedHistoryIndex(-1);
                setHistoryOpen(false);
                window.setTimeout(() => {
                  if (document.activeElement === input) {
                    input.select();
                  }
                }, 0);
              }}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return;
                if (event.key === "Escape") {
                  event.preventDefault();
                  setHistoryOpen(false);
                  return;
                }
                if (event.key === "ArrowDown") {
                  event.preventDefault();
                  setHistoryOpen(Boolean(historySearch.trim()));
                  setSelectedHistoryIndex((current) => Math.min(current + 1, historyEntries.length - 1));
                  return;
                }
                if (event.key === "ArrowUp") {
                  event.preventDefault();
                  setHistoryOpen(Boolean(historySearch.trim()));
                  setSelectedHistoryIndex((current) => (current <= 0 ? historyEntries.length - 1 : current - 1));
                  return;
                }
                if (event.key === "Enter" && historyOpen && selectedHistoryIndex >= 0) {
                  const entry = historyEntries[selectedHistoryIndex];
                  if (entry) {
                    event.preventDefault();
                    selectHistoryEntry(entry);
                  }
                }
              }}
            />
            <Button
              aria-label={t("browser.openURL")}
              className={cn(
                "shrink-0 text-muted-foreground",
                "absolute top-0.5 right-0.5 size-7 rounded-md opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 disabled:opacity-0 group-focus-within:opacity-100",
              )}
              disabled={pending || !value.trim()}
              size="icon-sm"
              type="submit"
              variant="ghost"
            >
              {pending ? <Spinner className="size-4" /> : <CornerDownLeft className="size-3.5" />}
            </Button>
          </div>
        </PopoverAnchor>
        <PopoverContent
          align="start"
          collisionPadding={12}
          className="gap-0 rounded-xl border border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] p-1 shadow-[0_12px_32px_rgba(0,0,0,0.18)] ring-0"
          sideOffset={6}
          style={{ width: "var(--radix-popover-trigger-width)" }}
          onCloseAutoFocus={(event) => event.preventDefault()}
          onInteractOutside={(event) => {
            if (event.target === addressInputRef.current) {
              event.preventDefault();
            }
          }}
          onOpenAutoFocus={(event) => event.preventDefault()}
        >
          <Command
            className="rounded-md p-0"
            shouldFilter={false}
            value={selectedHistoryIndex >= 0 ? historyEntries[selectedHistoryIndex]?.id || "__none__" : "__none__"}
            onValueChange={(value) => setSelectedHistoryIndex(historyEntries.findIndex((entry) => entry.id === value))}
          >
            <CommandList className="max-h-[22.25rem]">
              <CommandGroup heading={historySearchTerm ? t("browser.historyResults") : undefined}>
                {historyEntries.map((entry, index) => (
                  <CommandItem
                    key={entry.id}
                    disabled={pending}
                    className={cn(
                      "h-10 min-w-0 py-1.5 pr-1 [&>svg:last-child]:hidden",
                      selectedHistoryIndex === index && "!bg-interactive-selected text-foreground",
                    )}
                    value={entry.id}
                    onMouseEnter={() => setSelectedHistoryIndex(index)}
                    onSelect={() => selectHistoryEntry(entry)}
                  >
                    <div className="flex size-5 shrink-0 items-center justify-center overflow-hidden rounded-sm text-muted-foreground">
                      <HistoryFavicon pageURL={entry.url} url={entry.faviconURL} />
                    </div>
                    <div className="flex min-w-0 flex-1 items-baseline gap-1.5 overflow-hidden text-sm">
                      <span className="min-w-0 truncate">{entry.title || historyURLLabel(entry.url)}</span>
                      <span aria-hidden="true" className="shrink-0 text-muted-foreground/45">·</span>
                      <span className="max-w-[45%] shrink-0 truncate text-xs text-muted-foreground">{browserCompactURL(entry.url)}</span>
                    </div>
                    <Button
                      aria-label={t("browser.historyDelete")}
                      className="size-7 shrink-0 text-muted-foreground opacity-0 group-data-selected/command-item:opacity-100 hover:text-destructive group-hover/command-item:opacity-100"
                      disabled={deleteHistoryMutation.isPending && deleteHistoryMutation.variables === entry.id}
                      size="icon-sm"
                      type="button"
                      variant="ghost"
                      onClick={(event) => {
                        event.stopPropagation();
                        deleteHistoryMutation.mutate(entry.id);
                      }}
                      onPointerDown={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                      }}
                    >
                      {deleteHistoryMutation.isPending && deleteHistoryMutation.variables === entry.id ? (
                        <Spinner className="size-3.5" />
                      ) : (
                        <Trash2 className="size-3.5" />
                      )}
                    </Button>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
          {historyEntries.length > 0 ? (
            <div className="mt-1 flex items-center justify-between border-t px-1 pt-1">
              <div className="flex items-center gap-1.5 px-2 text-xs text-muted-foreground">
                <History className="size-3.5" />
                {t("browser.historyGlobal")}
              </div>
              <Button
                className="h-7 px-2 text-xs text-muted-foreground hover:text-destructive"
                size="sm"
                type="button"
                variant="ghost"
                onClick={() => {
                  setHistoryOpen(false);
                  setClearHistoryOpen(true);
                }}
              >
                {t("browser.historyClear")}
              </Button>
            </div>
          ) : null}
        </PopoverContent>
      </Popover>
      <AlertDialog open={clearHistoryOpen} onOpenChange={setClearHistoryOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("browser.historyClearTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("browser.historyClearDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={clearHistoryMutation.isPending}
              variant="destructive"
              onClick={() => clearHistoryMutation.mutate()}
            >
              {t("browser.historyClear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}

function historyURLLabel(rawURL: string) {
  try {
    return new URL(rawURL).hostname || rawURL;
  } catch {
    return rawURL;
  }
}

function HistoryFavicon({ pageURL, url }: { pageURL: string; url?: string }) {
  return (
    <BrowserFavicon
      className="size-4 object-contain"
      fallback={<Globe className="size-4" />}
      faviconURL={url}
      pageURL={pageURL}
    />
  );
}
