import { BrowserFavicon } from "@/browser/BrowserFavicon";
import { Globe } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { cn } from "@/lib/utils";

export function BrowserTabIcon({
  className,
  faviconURL,
  loading = false,
  pageURL,
}: {
  className?: string;
  faviconURL?: string;
  loading?: boolean;
  pageURL: string;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-[5px] bg-transparent text-current",
        className,
      )}
    >
      {loading ? <Spinner className="absolute inset-0 size-full" /> : null}
      <span className={cn(
        "inline-flex items-center justify-center overflow-hidden transition-[width,height] duration-150",
        loading ? "size-[70%]" : "size-full",
      )}>
        <BrowserFavicon
          className="size-full object-cover"
          fallback={<Globe className="size-[calc(100%_-_2px)]" />}
          faviconURL={faviconURL}
          pageURL={pageURL}
        />
      </span>
    </span>
  );
}
