import { BrowserFavicon } from "@/browser/BrowserFavicon";
import { Globe } from "@/components/icons";
import { cn } from "@/lib/utils";

export function BrowserTabIcon({
  className,
  faviconURL,
  pageURL,
}: {
  className?: string;
  faviconURL?: string;
  pageURL: string;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex shrink-0 items-center justify-center overflow-hidden rounded-[5px] bg-transparent text-current",
        className,
      )}
    >
      <BrowserFavicon
        className="size-full object-cover"
        fallback={<Globe className="size-3/4" />}
        faviconURL={faviconURL}
        pageURL={pageURL}
      />
    </span>
  );
}
