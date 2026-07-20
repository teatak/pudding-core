import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";

import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export type ImageLightboxItem = {
  id: string;
  name: string;
  size?: number;
  url: string;
};

export function ImageLightbox({
  images,
  openIndex,
  onOpenIndexChange,
}: {
  images: ImageLightboxItem[];
  openIndex: number | null;
  onOpenIndexChange: (index: number | null) => void;
}) {
  const open = openIndex !== null && openIndex >= 0 && openIndex < images.length;
  const currentIndex = open ? openIndex : 0;
  const current = images[currentIndex];
  const multiple = images.length > 1;
  const mainRef = useRef<HTMLDivElement | null>(null);
  const itemRefs = useRef<Array<HTMLElement | null>>([]);
  const thumbScrollerRef = useRef<HTMLDivElement | null>(null);
  const thumbRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const scrollFrameRef = useRef<number | null>(null);
  const wasOpenRef = useRef(false);
  const scrollToIndex = useCallback((index: number, behavior: ScrollBehavior = "smooth") => {
    window.requestAnimationFrame(() => {
      scrollChildToCenter(mainRef.current, itemRefs.current[index] ?? null, behavior);
      scrollChildToCenter(thumbScrollerRef.current, thumbRefs.current[index] ?? null, behavior);
    });
  }, []);
  const goTo = useCallback(
    (delta: number) => {
      if (!multiple) {
        return;
      }
      const next = (currentIndex + delta + images.length) % images.length;
      onOpenIndexChange(next);
      scrollToIndex(next);
    },
    [currentIndex, images.length, multiple, onOpenIndexChange, scrollToIndex],
  );

  useEffect(() => {
    if (!open) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        goTo(-1);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        goTo(1);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [goTo, open]);
  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) {
        window.cancelAnimationFrame(scrollFrameRef.current);
      }
    };
  }, []);
  useEffect(() => {
    if (open && !wasOpenRef.current) {
      scrollToIndex(currentIndex, "auto");
    }
    wasOpenRef.current = open;
  }, [currentIndex, open, scrollToIndex]);

  const handleMainScroll = () => {
    if (scrollFrameRef.current !== null) {
      return;
    }
    scrollFrameRef.current = window.requestAnimationFrame(() => {
      scrollFrameRef.current = null;
      const next = nearestLightboxIndex(mainRef.current, itemRefs.current, currentIndex);
      if (next !== currentIndex) {
        onOpenIndexChange(next);
        scrollChildToCenter(thumbScrollerRef.current, thumbRefs.current[next] ?? null, "auto");
      }
    });
  };

  if (!current) {
    return null;
  }

  return (
    <Dialog open={open} onOpenChange={(next) => onOpenIndexChange(next ? currentIndex : null)}>
      <DialogContent
        className="h-[min(92dvh,54rem)] w-[min(96vw,86rem)] max-w-none gap-0 overflow-hidden rounded-xl border-0 bg-black/90 p-0 text-white ring-1 ring-white/15 sm:max-w-none"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">图片预览</DialogTitle>
        <DialogDescription className="sr-only">放大查看图片，可用左右方向键切换。</DialogDescription>
        <div className="relative flex h-full min-h-0 flex-col">
          <div className="absolute inset-x-0 top-0 z-10 flex min-w-0 items-center gap-3 bg-gradient-to-b from-black/65 to-transparent px-3 py-3">
            <div className="min-w-0 text-sm">
              <div className="truncate font-medium">{current.name}</div>
              {multiple ? <div className="text-xs text-white/65">{currentIndex + 1} / {images.length}</div> : null}
            </div>
            <DialogClose asChild>
              <Button
                aria-label="关闭预览"
                className="ml-auto rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white"
                size="icon-sm"
                type="button"
                variant="ghost"
              >
                <X />
              </Button>
            </DialogClose>
          </div>

          <div
            ref={mainRef}
            className="flex min-h-0 flex-1 snap-x snap-mandatory overflow-x-auto overflow-y-hidden px-3 pt-12 pb-16 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            onScroll={handleMainScroll}
          >
            {images.map((image, index) => (
              <figure
                key={image.id}
                ref={(el) => {
                  itemRefs.current[index] = el;
                }}
                className="relative m-0 flex h-full min-h-full w-full shrink-0 snap-center items-center justify-center overflow-hidden"
              >
                <img alt={image.name} className="block max-h-full max-w-full object-contain" draggable={false} src={image.url} />
              </figure>
            ))}
          </div>

          {multiple ? (
            <>
              <Button
                aria-label="上一张"
                className="absolute top-1/2 left-3 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white"
                size="icon-lg"
                type="button"
                variant="ghost"
                onClick={() => goTo(-1)}
              >
                <ChevronLeft />
              </Button>
              <Button
                aria-label="下一张"
                className="absolute top-1/2 right-3 rounded-full bg-white/10 text-white hover:bg-white/20 hover:text-white"
                size="icon-lg"
                type="button"
                variant="ghost"
                onClick={() => goTo(1)}
              >
                <ChevronRight />
              </Button>
              <div className="absolute inset-x-0 bottom-0 z-10 flex justify-center bg-gradient-to-t from-black/70 to-transparent px-3 py-3">
                <div ref={thumbScrollerRef} className="flex max-w-full gap-2 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                  {images.map((image, index) => (
                    <button
                      key={image.id}
                      ref={(el) => {
                        thumbRefs.current[index] = el;
                      }}
                      aria-label={image.name}
                      className={cn(
                        "h-12 w-16 shrink-0 overflow-hidden rounded-md border bg-white/10 p-0.5 shadow-sm focus-visible:ring-2 focus-visible:ring-white/60 focus-visible:outline-none",
                        index === currentIndex
                          ? "border-white shadow-[0_0_0_2px_rgba(255,255,255,0.35)]"
                          : "border-white/20 opacity-70 hover:border-white/50 hover:opacity-100",
                      )}
                      tabIndex={index === currentIndex ? 0 : -1}
                      type="button"
                      onClick={() => {
                        onOpenIndexChange(index);
                        scrollToIndex(index);
                      }}
                    >
                      <img alt="" className="block h-full w-full object-contain" draggable={false} src={image.url} />
                    </button>
                  ))}
                </div>
              </div>
            </>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function scrollChildToCenter(scroller: HTMLElement | null, child: HTMLElement | null, behavior: ScrollBehavior) {
  if (!scroller || !child) {
    return;
  }
  const scrollerRect = scroller.getBoundingClientRect();
  const childRect = child.getBoundingClientRect();
  const delta = childRect.left + childRect.width / 2 - (scrollerRect.left + scrollerRect.width / 2);
  scroller.scrollTo({ left: scroller.scrollLeft + delta, behavior });
}

function nearestLightboxIndex(scroller: HTMLElement | null, items: Array<HTMLElement | null>, fallback: number) {
  if (!scroller) {
    return fallback;
  }
  const scrollerRect = scroller.getBoundingClientRect();
  const center = scrollerRect.left + scrollerRect.width / 2;
  let bestIndex = fallback;
  let bestDistance = Number.POSITIVE_INFINITY;
  items.forEach((item, index) => {
    if (!item) {
      return;
    }
    const itemRect = item.getBoundingClientRect();
    const itemCenter = itemRect.left + itemRect.width / 2;
    const distance = Math.abs(itemCenter - center);
    if (distance < bestDistance) {
      bestDistance = distance;
      bestIndex = index;
    }
  });
  return bestIndex;
}
