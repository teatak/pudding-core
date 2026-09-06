import { AppTooltip } from "@/components/AppTooltip";
import { ChevronDown, Check, Search } from "@/components/icons";
import { useState, type ReactNode } from "react";
import { AppPopoverContent } from "@/components/AppPopover";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";

export type OpenTabEntry = { id: string; title: string; description?: string; icon?: ReactNode };
export function OpenTabsMenu({ tabs, activeID, onSelect }: { tabs: OpenTabEntry[]; activeID?: string | null; onSelect: (id: string) => void }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  if (tabs.length < 2) return null;
  const label = t("workspace.openTabs").replace("{count}", String(tabs.length));
  const needle = search.trim().toLocaleLowerCase();
  const results = tabs.filter((tab) => `${tab.title} ${tab.description || ""}`.toLocaleLowerCase().includes(needle));
  return (
    <Popover open={open} onOpenChange={(next) => { setOpen(next); if (!next) { setSearch(""); } }}>
      <AppTooltip content={label} open={open ? false : undefined}><PopoverTrigger asChild>
        <Button aria-label={label} className="no-drag-region h-7 shrink-0 gap-1 rounded-md px-1.5 text-xs text-muted-foreground" size="sm" variant="ghost">
          <span aria-hidden="true" className="min-w-3 text-center tabular-nums">{tabs.length}</span><ChevronDown className="size-3" />
        </Button>
      </PopoverTrigger></AppTooltip>
      <AppPopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)] gap-1 p-2">
        <div className="relative"><Search className="absolute top-2.5 left-2 size-4 text-muted-foreground" /><Input aria-label={t("workspace.searchTabs")} className="h-9 pl-8" placeholder={t("workspace.searchTabs")} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
        <div aria-label={label} className="max-h-80 overflow-y-auto" role="list">
          {results.length === 0 ? <p className="p-4 text-center text-xs text-muted-foreground">{t("workspace.noResults")}</p> : results.map((tab) => (
            <div key={tab.id} role="listitem"><button aria-current={tab.id === activeID ? "true" : undefined} className="flex w-full min-w-0 items-center gap-2 rounded-md p-2 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none" type="button" onClick={() => { onSelect(tab.id); setOpen(false); setSearch(""); }}>
              <span className="flex size-4 shrink-0 items-center justify-center">{tab.icon}</span><span className="min-w-0 flex-1"><span className="block truncate text-sm">{tab.title}</span>{tab.description ? <span className="block truncate text-xs text-muted-foreground">{tab.description}</span> : null}</span>{tab.id === activeID ? <Check className="size-3.5 shrink-0" /> : null}
            </button></div>
          ))}
        </div>
      </AppPopoverContent>
    </Popover>
  );
}
