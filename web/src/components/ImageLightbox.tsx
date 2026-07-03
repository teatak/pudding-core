import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { useCallback, useEffect } from "react";

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
  const goTo = useCallback(
    (delta: number) => {
      if (!multiple) {
        return;
      }
      const next = (currentIndex + delta + images.length) % images.length;
      onOpenIndexChange(next);
    },
    [currentIndex, images.length, multiple, onOpenIndexChange],
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

          <div className="flex min-h-0 flex-1 items-center justify-center px-3 pt-12 pb-16">
            <img
              key={current.id}
              alt={current.name}
              className="max-h-full max-w-full object-contain"
              draggable={false}
              src={current.url}
            />
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
                <div className="flex max-w-full gap-2 overflow-x-auto">
                  {images.map((image, index) => (
                    <button
                      key={image.id}
                      aria-label={image.name}
                      className={cn(
                        "h-12 w-14 shrink-0 overflow-hidden rounded-md border bg-white/10",
                        index === currentIndex ? "border-white" : "border-white/20 opacity-70 hover:opacity-100",
                      )}
                      type="button"
                      onClick={() => onOpenIndexChange(index)}
                    >
                      <img alt="" className="h-full w-full object-cover" draggable={false} src={image.url} />
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
