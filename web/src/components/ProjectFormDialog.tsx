import { FolderClosed, FolderPlus, X } from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type DragEvent,
  type FormEvent,
} from "react";
import { toast } from "sonner";

import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";
import { droppedLocalItemsFromDataTransfer } from "@/lib/localFolders";
import { cn } from "@/lib/utils";

export function ProjectFormDialog({
  description,
  directoryPaths,
  isPending,
  name,
  open,
  submitDisabled,
  submitLabel,
  title,
  onChooseDirectories,
  onDirectoryPathsChange,
  onNameChange,
  onOpenChange,
  onSubmit,
}: {
  description: string;
  directoryPaths: string[];
  isPending: boolean;
  name: string;
  open: boolean;
  submitDisabled: boolean;
  submitLabel: string;
  title: string;
  onChooseDirectories: () => void;
  onDirectoryPathsChange: (paths: string[]) => void;
  onNameChange: (name: string) => void;
  onOpenChange: (open: boolean) => void;
  onSubmit: () => void;
}) {
  const { t } = useI18n();
  const [dropActive, setDropActive] = useState(false);
  const directoryRowsRef = useRef(new Map<string, HTMLDivElement>());
  const pendingDirectoryRectsRef = useRef<Map<string, DOMRect> | null>(null);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSubmit();
  };

  useEffect(() => {
    const resetDropState = () => setDropActive(false);
    window.addEventListener("dragend", resetDropState);
    window.addEventListener("drop", resetDropState);
    window.addEventListener("blur", resetDropState);
    return () => {
      window.removeEventListener("dragend", resetDropState);
      window.removeEventListener("drop", resetDropState);
      window.removeEventListener("blur", resetDropState);
    };
  }, []);

  useLayoutEffect(() => {
    const previousRects = pendingDirectoryRectsRef.current;
    pendingDirectoryRectsRef.current = null;
    if (
      previousRects &&
      !window.matchMedia("(prefers-reduced-motion: reduce)").matches
    ) {
      for (const path of directoryPaths) {
        const row = directoryRowsRef.current.get(path);
        if (!row) continue;
        for (const animation of row.getAnimations()) animation.cancel();
        const previousRect = previousRects.get(path);
        const nextRect = row.getBoundingClientRect();
        const offsetY = previousRect ? previousRect.top - nextRect.top : 0;
        if (Math.abs(offsetY) < 1) continue;
        row.animate(
          [{ transform: `translateY(${offsetY}px)` }, { transform: "translateY(0)" }],
          { duration: 180, easing: "cubic-bezier(0.2, 0, 0, 1)" },
        );
      }
    }
  }, [directoryPaths]);

  const captureDirectoryRects = () => {
    const rects = new Map<string, DOMRect>();
    for (const path of directoryPaths) {
      const row = directoryRowsRef.current.get(path);
      if (row) rects.set(path, row.getBoundingClientRect());
    }
    pendingDirectoryRectsRef.current = rects;
  };

  const handleDragEnter = (event: DragEvent<HTMLDivElement>) => {
    if (isPending || !dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    setDropActive(true);
  };

  const handleDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (isPending || !dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    setDropActive(true);
  };

  const handleDragLeave = (event: DragEvent<HTMLDivElement>) => {
    if (!dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    const nextTarget = event.relatedTarget;
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) return;
    setDropActive(false);
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    if (isPending || !dataTransferHasFiles(event.dataTransfer)) return;
    event.preventDefault();
    event.stopPropagation();
    setDropActive(false);
    const dropped = droppedLocalItemsFromDataTransfer(event.dataTransfer);
    if (dropped.folderPaths.length > 0) {
      const paths = Array.from(
        new Set(
          [...directoryPaths, ...dropped.folderPaths]
            .map((path) => path.trim())
            .filter(Boolean),
        ),
      );
      onDirectoryPathsChange(paths);
      if (!name.trim() && paths[0]) {
        onNameChange(basename(paths[0]));
      }
    }
    if (dropped.folderPathUnavailable) {
      toast.error(t("project.folderDropPathUnavailable"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[min(680px,calc(100svh-2rem))] sm:max-w-xl">
        <form className="contents" onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription className="sr-only">{description}</DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 gap-4 overflow-y-auto pr-1">
            <label className="grid gap-1.5">
              <span className="sr-only">{t("project.name")}</span>
              <div className="flex overflow-hidden rounded-lg border bg-background focus-within:border-ring">
                <span className="grid w-11 shrink-0 place-items-center border-r text-muted-foreground">
                  <FolderClosed className="size-4" />
                </span>
                <Input
                  autoFocus
                  className="h-10 rounded-none border-0 bg-transparent shadow-none focus-visible:border-transparent focus-visible:ring-0"
                  disabled={isPending}
                  maxLength={120}
                  placeholder={t("project.namePlaceholder")}
                  value={name}
                  onChange={(event) => onNameChange(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && event.nativeEvent.isComposing) {
                      event.preventDefault();
                    }
                  }}
                />
              </div>
            </label>
            <div className="grid gap-2">
              <span className="text-sm font-medium">{t("project.directories")}</span>
              <div
                className={cn(
                  "relative overflow-hidden rounded-lg border",
                  dropActive && "border-primary bg-accent/40",
                )}
                onDragEnter={handleDragEnter}
                onDragLeave={handleDragLeave}
                onDragOver={handleDragOver}
                onDrop={handleDrop}
              >
                {directoryPaths.map((path, index) => (
                  <div
                    key={path}
                    ref={(element) => {
                      if (element) {
                        directoryRowsRef.current.set(path, element);
                      } else {
                        directoryRowsRef.current.delete(path);
                      }
                    }}
                    className="flex min-w-0 items-center gap-2 border-b px-3 py-2.5"
                  >
                    <FolderClosed className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate text-sm">{path}</span>
                    {directoryPaths.length > 1 && index === 0 ? (
                      <span className="shrink-0 rounded-md border px-2 py-0.5 text-xs text-muted-foreground">
                        {t("project.primaryDirectory")}
                      </span>
                    ) : null}
                    {directoryPaths.length > 1 && index > 0 ? (
                      <Button
                        className="h-7 shrink-0 px-2 text-xs"
                        disabled={isPending}
                        type="button"
                        variant="ghost"
                        onClick={() => {
                          captureDirectoryRects();
                          onDirectoryPathsChange([
                            path,
                            ...directoryPaths.filter((entry) => entry !== path),
                          ]);
                        }}
                      >
                        {t("project.makePrimaryDirectory")}
                      </Button>
                    ) : null}
                    <Button
                      aria-label={t("project.removeDirectory").replace("{name}", basename(path))}
                      disabled={isPending}
                      size="icon-xs"
                      type="button"
                      variant="ghost"
                      onClick={() =>
                        onDirectoryPathsChange(directoryPaths.filter((entry) => entry !== path))
                      }
                    >
                      <X />
                    </Button>
                  </div>
                ))}
                <div className="p-1">
                  <Button
                    className="h-9 w-full justify-start rounded-md px-2"
                    disabled={isPending}
                    type="button"
                    variant="ghost"
                    onClick={onChooseDirectories}
                  >
                    <FolderPlus className="size-4" />
                    {t("project.addFolder")}
                  </Button>
                </div>
                {dropActive ? (
                  <div className="pointer-events-none absolute inset-1 z-10 grid place-items-center rounded-md bg-background/95 px-4 text-center text-sm font-medium">
                    {t("project.folderDropHint")}
                  </div>
                ) : null}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button
              disabled={isPending}
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={isPending || submitDisabled} type="submit">
              {isPending ? <Spinner /> : null}
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function basename(path: string) {
  const normalized = path.replace(/[\\/]+$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || path;
}

function dataTransferHasFiles(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types || []).includes("Files");
}
