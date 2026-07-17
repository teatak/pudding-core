import { Check, Copy, FileCode2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { useI18n } from "@/i18n";
import { fileNameFromPath, languageFromPath } from "@/lib/fileLanguage";
import { cn } from "@/lib/utils";
import { getShikiCodeRenderer } from "@/lib/shiki";
import type { FilePreview } from "@/state/filePreviewStore";
import { TurnFileDiffSurface } from "./TurnFileDiffSurface";

export function FilePreviewSurface({ active, preview, token }: { active: boolean; preview: FilePreview; token: string }) {
  if (preview.source === "turn-diff") {
    return <TurnFileDiffSurface active={active} preview={preview} token={token} />;
  }
  return <TextFilePreviewSurface active={active} preview={preview} />;
}

function TextFilePreviewSurface({ active, preview }: { active: boolean; preview: FilePreview }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const [highlighted, setHighlighted] = useState<string | null>(null);
  const copiedTimerRef = useRef<number | null>(null);
  const language = useMemo(() => languageFromPath(preview.path), [preview.path]);
  const content = useMemo(() => preview.content.replace(/\r\n/g, "\n"), [preview.content]);
  const characterCount = useMemo(() => Array.from(content).length, [content]);
  const lineCount = useMemo(() => content.split("\n").length, [content]);
  const lineNumbers = useMemo(
    () => Array.from({ length: lineCount }, (_unused, index) => preview.lineStart + index * preview.lineStep),
    [lineCount, preview.lineStart, preview.lineStep],
  );
  const focusLineIndex = preview.focusLine == null ? -1 : lineNumbers.indexOf(preview.focusLine);

  useEffect(() => {
    let cancelled = false;
    setHighlighted(null);
    void getShikiCodeRenderer().then((render) => {
      if (!cancelled) {
        setHighlighted(render(content, language));
      }
    });
    return () => {
      cancelled = true;
    };
  }, [content, language]);

  useEffect(
    () => () => {
      if (copiedTimerRef.current) {
        window.clearTimeout(copiedTimerRef.current);
      }
    },
    [],
  );

  const copyContent = () => {
    void navigator.clipboard.writeText(preview.content).then(() => {
      setCopied(true);
      if (copiedTimerRef.current) {
        window.clearTimeout(copiedTimerRef.current);
      }
      copiedTimerRef.current = window.setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <div
      aria-hidden={!active}
      className={cn(
        "absolute inset-0 z-10 flex min-h-0 flex-col overflow-hidden bg-[var(--workspace-background)] text-card-foreground",
        !active && "pointer-events-none invisible opacity-0",
      )}
    >
      <div className="flex h-9 shrink-0 items-center gap-2 border-b border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] px-3">
        <FileCode2 className="size-4 shrink-0 text-muted-foreground" />
        <code className="min-w-0 flex-1 cursor-text select-text truncate font-mono text-xs" title={preview.path}>
          {preview.path}
        </code>
        {language ? <span className="shrink-0 text-[10px] uppercase text-muted-foreground">{language}</span> : null}
        <Button
          aria-label={copied ? t("common.copied") : t("common.copy")}
          size="icon-sm"
          type="button"
          variant="ghost"
          onClick={copyContent}
        >
          {copied ? <Check className="text-success" /> : <Copy />}
        </Button>
      </div>
      {preview.truncated ? (
        <div className="shrink-0 border-b bg-warning/10 px-3 py-1.5 text-[11px] text-warning">
          {t("canvas.filePreviewTruncated")}
        </div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-auto bg-[var(--workspace-panel-background)]">
        <div className="relative flex min-h-full min-w-max items-stretch">
          {focusLineIndex >= 0 ? (
            <div
              aria-hidden="true"
              className="pointer-events-none absolute inset-x-0 z-0 h-6 border-l-2 border-primary/60 bg-primary/[0.06]"
              style={{ top: `${12 + focusLineIndex * 24}px` }}
            />
          ) : null}
          <div
            aria-hidden="true"
            className="sticky left-0 z-[1] m-0 shrink-0 border-r bg-muted/35 px-3 py-3 text-right font-mono text-[12px] leading-6 text-muted-foreground/60 select-none"
          >
            {lineNumbers.map((line) => (
              <div key={line} className={cn("h-6", line === preview.focusLine && "font-semibold text-primary")}>{line}</div>
            ))}
          </div>
          {highlighted ? (
            <div className="pudding-file-preview relative z-[1] min-w-max" dangerouslySetInnerHTML={{ __html: highlighted }} />
          ) : (
            <pre className="relative z-[1] m-0 min-w-max px-4 py-3 font-mono text-[12px] leading-6 text-foreground/90">{content}</pre>
          )}
        </div>
      </div>
      <div className="flex h-7 shrink-0 items-center gap-2 border-t border-[var(--workspace-border)] bg-[var(--workspace-chrome-background)] px-3 text-[11px] text-muted-foreground">
        <span>{t("canvas.filePreviewSnapshot")}</span>
        <span aria-hidden="true">·</span>
        <span>{replace(t("canvas.filePreviewLines"), { count: String(lineCount) })}</span>
        <span aria-hidden="true">·</span>
        <span>{replace(t("canvas.filePreviewCharacters"), { count: String(characterCount) })}</span>
      </div>
    </div>
  );
}

export function filePreviewTitle(path: string) {
  return fileNameFromPath(path);
}

function replace(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce((text, [key, value]) => text.replaceAll(`{${key}}`, value), template);
}
