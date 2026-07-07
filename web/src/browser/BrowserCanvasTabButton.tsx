import { Compass, Loader2, X } from "lucide-react";
import { useEffect, useState } from "react";

import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

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

export function BrowserCanvasTabButton({
  active,
  closePending,
  closable,
  faviconURL,
  hasTitle,
  pending,
  title,
  onClick,
  onClose,
}: {
  active: boolean;
  closePending: boolean;
  closable: boolean;
  faviconURL?: string;
  hasTitle: boolean;
  pending: boolean;
  title: string;
  onClick: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="no-drag-region w-fit max-w-full min-w-0 shrink-0 overflow-hidden rounded-lg bg-muted p-(--canvas-toolbar-tab-padding) text-muted-foreground">
      <button
        aria-label={t("browser.title")}
        aria-selected={active}
        className={cn(
          "group inline-flex h-(--canvas-toolbar-tab-h) min-w-0 shrink-0 items-center gap-1.5 rounded-md border border-transparent px-2 text-xs font-medium whitespace-nowrap transition-colors data-[active=true]:bg-background data-[active=true]:text-foreground data-[active=true]:shadow-sm hover:bg-background hover:text-foreground",
          hasTitle ? "max-w-36" : "w-10 justify-center",
        )}
        data-active={active}
        disabled={pending}
        title={title || t("browser.title")}
        type="button"
        onClick={onClick}
      >
        {pending ? <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" /> : <BrowserTabIcon faviconURL={faviconURL} />}
        {hasTitle ? <span className="min-w-0 max-w-20 truncate text-left">{title || t("browser.title")}</span> : null}
        {closable ? (
          <span
            aria-label={t("canvas.delete")}
            className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded opacity-70 hover:bg-muted-foreground/20 hover:opacity-100"
            role="button"
            tabIndex={-1}
            onClick={(event) => {
              event.stopPropagation();
              onClose();
            }}
          >
            {closePending ? <Loader2 className="h-3 w-3 animate-spin" /> : <X className="h-3 w-3" />}
          </span>
        ) : null}
      </button>
    </div>
  );
}
