import { useState, type ReactNode } from "react";
import { BrowserFavicon } from "@/browser/BrowserFavicon";
import {
  AppDropdownMenuContent,
  AppDropdownMenuItem,
} from "@/components/AppMenu";
import { CanvasKindIcon } from "@/components/canvas/CanvasKindIcon";
import {
  Globe,
  Info,
  Star,
  MoreHorizontalIcon as MoreHorizontal,
} from "@/components/icons";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { formatRelative } from "@/lib/time";

export function LibraryResourceIcon({
  entry,
  compact = false,
}: {
  entry: {
    kind: "canvas" | "web";
    canvasKind?: string;
    url?: string;
    faviconURL?: string;
  };
  compact?: boolean;
}) {
  const iconClass = compact ? "size-4" : "size-5";
  return (
    <span
      className={cn(
        "flex shrink-0 items-center justify-center",
        compact ? "size-5" : "size-7",
      )}
    >
      {entry.kind === "canvas" ? (
        <CanvasKindIcon
          kind={entry.canvasKind}
          size={compact ? "xs" : "md"}
          className={compact ? "!bg-transparent" : undefined}
        />
      ) : (
        <BrowserFavicon
          pageURL={entry.url!}
          faviconURL={entry.faviconURL}
          className={iconClass}
          fallback={
            <Globe className={cn(iconClass, "text-muted-foreground")} />
          }
        />
      )}
    </span>
  );
}

export function LibraryResourceRow({
  title,
  description,
  icon,
  date,
  dateLabel,
  available = true,
  disabled = false,
  details,
  actions,
  onOpen,
  layout = "row",
  starred = false,
}: {
  layout?: "row" | "shortcut" | "card";
  starred?: boolean;
  title: string;
  description: string;
  icon: ReactNode;
  date: string;
  dateLabel: string;
  available?: boolean;
  disabled?: boolean;
  details: { label: string; value: string }[];
  actions: ReactNode;
  onOpen: () => void;
}) {
  const { t, locale } = useI18n();
  const [showDetails, setShowDetails] = useState(false);
  const exactTime = new Date(date).toLocaleString(locale);
  return (
    <div
      data-library-row
      className={cn(
        "group flex min-w-0 items-center gap-2 rounded-lg pr-1 hover:bg-accent/60 has-[:focus-visible]:bg-accent/60 has-[[aria-haspopup=menu][aria-expanded=true]]:bg-accent/60",
        layout === "row" && "pr-2",
        layout === "shortcut" && "w-full gap-1 bg-muted/30",
        layout === "card" && "border border-border/50 bg-muted/20 py-1",
      )}
    >
      <button
        data-library-open
        type="button"
        className={cn(
          "flex min-w-0 flex-1 items-center rounded-lg px-2 py-2 text-left disabled:opacity-50",
          layout === "shortcut" ? "min-h-9 gap-2" : "min-h-14 gap-2.5",
        )}
        disabled={!available || disabled}
        onClick={onOpen}
      >
        {icon}
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5 text-[13px] leading-5">
            <span className="truncate">{title}</span>
            {starred ? (
              <Star
                aria-label={t("workspace.favorite")}
                className="size-3 shrink-0 fill-current text-amber-500"
              />
            ) : null}
          </span>
          {layout !== "shortcut" || !available ? (
            <span className="mt-0.5 block truncate text-[11px] leading-4 text-muted-foreground">
              {available ? description : t("workspace.sourceUnavailable")}
            </span>
          ) : null}
        </span>
      </button>
      <div className="grid min-w-6 shrink-0 items-center justify-items-end">
        {layout === "row" ? (
          <time
            dateTime={date}
            aria-label={`${dateLabel} ${exactTime}`}
            className="col-start-1 row-start-1 whitespace-nowrap text-right text-[11px] text-muted-foreground group-hover:invisible group-has-[:focus-visible]:invisible group-has-[[aria-haspopup=menu][aria-expanded=true]]:invisible"
          >
            {formatRelative(date, locale)}
          </time>
        ) : null}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon-xs"
              variant="ghost"
              className="col-start-1 row-start-1 text-muted-foreground opacity-0 transition-none group-hover:opacity-100 group-has-[:focus-visible]:opacity-100 aria-expanded:opacity-100 dark:aria-expanded:bg-muted/50"
              aria-label={`${t("workspace.resourceActions")} ${title}`}
            >
              <MoreHorizontal className="size-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <AppDropdownMenuContent align="end">
            <AppDropdownMenuItem onSelect={() => setShowDetails(true)}>
              <Info />
              {t("workspace.resourceDetails")}
            </AppDropdownMenuItem>
            {actions}
          </AppDropdownMenuContent>
        </DropdownMenu>
      </div>
      <Dialog open={showDetails} onOpenChange={setShowDetails}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="pr-6 break-words">{title}</DialogTitle>
            <DialogDescription>
              {t("workspace.resourceDetails")}
            </DialogDescription>
          </DialogHeader>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-5 gap-y-3 text-sm">
            {[...details, { label: dateLabel, value: exactTime }]
              .filter((detail) => detail.value)
              .map((detail) => (
                <div key={detail.label} className="contents">
                  <dt className="text-muted-foreground">{detail.label}</dt>
                  <dd className="select-text whitespace-pre-wrap break-all">
                    {detail.value}
                  </dd>
                </div>
              ))}
          </dl>
        </DialogContent>
      </Dialog>
    </div>
  );
}
