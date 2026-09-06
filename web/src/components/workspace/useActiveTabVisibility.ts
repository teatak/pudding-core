import { useLayoutEffect, type RefObject } from "react";

// A selected tab must remain reachable when the strip narrows as well as when
// selection changes. Observe the strip once, not every tab.
export function useActiveTabVisibility(ref: RefObject<HTMLDivElement | null>, activeID?: string | null) {
  useLayoutEffect(() => {
    const strip = ref.current;
    if (!strip) return;
    const reveal = () => {
      if (strip.clientWidth > 0) strip.querySelector('.pudding-workspace-content-tab[data-selected="true"]')?.scrollIntoView({ block: "nearest", inline: "nearest" });
    };
    reveal();
    const observer = new ResizeObserver(reveal);
    observer.observe(strip);
    return () => observer.disconnect();
  }, [ref, activeID]);
}
