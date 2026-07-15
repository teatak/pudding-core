import { useEffect, useMemo, useState } from "react";

import type { ProjectFile } from "@/api/client";
import { MarkdownBody } from "@/components/transcript/TurnParts";
import { useScopedSelectAll } from "@/hooks/useScopedSelectAll";
import { useI18n } from "@/i18n";

import type { ProjectEditorSelection } from "./ProjectEditor";
import { parseProjectMarkdownLink, projectMarkdownResolvers } from "./projectPaths";
import type { ProjectSelection } from "./types";

type SelectionAction = {
  range: ProjectEditorSelection;
  x: number;
  y: number;
};

export function ProjectMarkdownPreview({
  file,
  sessionID,
  token,
  onOpenPreview,
  onReferenceSelection,
}: {
  file: ProjectFile;
  sessionID: string;
  token: string;
  onOpenPreview: (selection: ProjectSelection) => void;
  onReferenceSelection: (selection: ProjectEditorSelection) => void;
}) {
  const { t } = useI18n();
  const links = useMemo(() => projectMarkdownResolvers(file, token, sessionID), [file, sessionID, token]);
  const [previewNode, setPreviewNode] = useState<HTMLDivElement | null>(null);
  const [selectionAction, setSelectionAction] = useState<SelectionAction>();
  useScopedSelectAll(previewNode);

  useEffect(() => {
    if (!previewNode) return;
    let frame = 0;
    const updateSelection = () => {
      if (frame) {
        window.cancelAnimationFrame(frame);
      }
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        setSelectionAction(markdownSelectionAction(previewNode, file.content));
      });
    };
    document.addEventListener("selectionchange", updateSelection);
    previewNode.addEventListener("dblclick", updateSelection);
    previewNode.addEventListener("pointerup", updateSelection);
    previewNode.addEventListener("keyup", updateSelection);
    return () => {
      if (frame) {
        window.cancelAnimationFrame(frame);
      }
      document.removeEventListener("selectionchange", updateSelection);
      previewNode.removeEventListener("dblclick", updateSelection);
      previewNode.removeEventListener("pointerup", updateSelection);
      previewNode.removeEventListener("keyup", updateSelection);
    };
  }, [file.content, previewNode]);

  return (
    <div ref={setPreviewNode} className="relative min-h-full w-full bg-background dark:bg-[#171717]" data-select-all-scope="project-markdown">
      <div className="mx-auto w-full max-w-4xl p-6">
        <MarkdownBody
          allowHtmlImages={false}
          enableMermaid
          resolveImageURL={links.resolveImageURL}
          resolveLinkURL={links.resolveLinkURL}
          text={file.content}
          token={token}
          onResolvedLinkClick={(href) => {
            if (href.startsWith("#")) {
              return true;
            }
            const target = parseProjectMarkdownLink(href);
            if (!target || target.rootID !== file.rootID) {
              return false;
            }
            onOpenPreview(target);
            return true;
          }}
        />
      </div>
      {selectionAction ? (
        <button
          aria-label={t("project.browserReferenceSelection")}
          className="absolute z-30 inline-flex h-8 items-center rounded-md border border-border bg-popover px-3 text-xs font-medium text-popover-foreground shadow-lg shadow-black/20 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none dark:shadow-black/50"
          style={{ left: selectionAction.x, top: selectionAction.y }}
          title={t("project.browserReferenceSelection")}
          type="button"
          onClick={() => {
            onReferenceSelection(selectionAction.range);
            window.getSelection()?.removeAllRanges();
            setSelectionAction(undefined);
          }}
          onMouseDown={(event) => event.preventDefault()}
        >
          {t("project.browserReferenceSelectionShort")}
        </button>
      ) : null}
    </div>
  );
}

function markdownSelectionAction(container: HTMLDivElement, source: string): SelectionAction | undefined {
  const selection = window.getSelection();
  if (
    !selection ||
    selection.isCollapsed ||
    !selection.anchorNode ||
    !selection.focusNode ||
    !container.contains(selection.anchorNode) ||
    !container.contains(selection.focusNode)
  ) return undefined;

  const range = projectMarkdownSelectionRange(source, selection.toString());
  if (!range || selection.rangeCount === 0) return undefined;
  const browserRange = selection.getRangeAt(0);
  const containerRect = container.getBoundingClientRect();
  const rect = selectionClientRect(browserRange, containerRect);
  if (!rect) return undefined;
  const width = 116;
  const preferredX = rect.right - containerRect.left + 8;
  const x = Math.max(8, Math.min(preferredX, container.clientWidth - width - 8));
  const above = rect.top - containerRect.top - 38;
  const y = above >= 8 ? above : rect.bottom - containerRect.top + 6;
  return { range, x, y };
}

function selectionClientRect(range: Range, containerRect: DOMRect): DOMRect | undefined {
  const rects = Array.from(range.getClientRects()).filter((rect) => {
    return (
      rect.width > 0 &&
      rect.height > 0 &&
      rect.bottom >= containerRect.top &&
      rect.top <= containerRect.bottom &&
      rect.right >= containerRect.left &&
      rect.left <= containerRect.right
    );
  });
  if (rects.length > 0) {
    return rects.at(-1);
  }
  const fallback = range.getBoundingClientRect();
  if (fallback.width > 0 && fallback.height > 0) {
    return fallback;
  }
  return undefined;
}

export function projectMarkdownSelectionRange(source: string, selectedText: string): ProjectEditorSelection | undefined {
  const selected = selectedText.trim();
  if (!selected) return undefined;
  const directStart = source.indexOf(selected);
  if (directStart >= 0) return sourceOffsetsToRange(source, directStart, directStart + selected.length - 1);

  const sourceProjection = markdownTextProjection(source);
  const selectedProjection = markdownTextProjection(selected);
  if (!selectedProjection.text) return undefined;
  const projectedStart = sourceProjection.text.indexOf(selectedProjection.text);
  if (projectedStart >= 0) {
    const start = sourceProjection.offsets[projectedStart];
    const end = sourceProjection.offsets[projectedStart + selectedProjection.text.length - 1];
    if (start === undefined || end === undefined) return undefined;
    return sourceOffsetsToRange(source, start, end);
  }

  const sourceCompact = compactTextProjection(sourceProjection);
  const selectedCompact = compactTextProjection(selectedProjection);
  if (!selectedCompact.text) return undefined;
  const compactStart = sourceCompact.text.indexOf(selectedCompact.text);
  if (compactStart < 0) return undefined;
  const start = sourceCompact.offsets[compactStart];
  const end = sourceCompact.offsets[compactStart + selectedCompact.text.length - 1];
  if (start === undefined || end === undefined) return undefined;
  return sourceOffsetsToRange(source, start, end);
}

function compactTextProjection(projection: ReturnType<typeof markdownTextProjection>) {
  let text = "";
  const offsets: number[] = [];
  for (let index = 0; index < projection.text.length; index += 1) {
    const character = projection.text[index];
    if (!character || /\s/.test(character)) continue;
    text += character;
    offsets.push(projection.offsets[index]);
  }
  return { offsets, text };
}

function markdownTextProjection(value: string) {
  let text = "";
  const offsets: number[] = [];
  const append = (character: string, offset: number) => {
    text += character;
    offsets.push(offset);
  };
  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (/[\u200B-\u200D\uFEFF]/.test(character)) continue;
    if (character === "]" && value[index + 1] === "(") {
      const close = value.indexOf(")", index + 2);
      if (close >= 0) {
        index = close;
        continue;
      }
    }
    if ("[]*_~`#>|-+".includes(character)) continue;
    if (/\s/.test(character) || character === "\u00a0") {
      if (text && text.at(-1) !== " ") append(" ", index);
      continue;
    }
    append(character, index);
  }
  if (text.endsWith(" ")) {
    text = text.slice(0, -1);
    offsets.pop();
  }
  return { offsets, text };
}

function sourceOffsetsToRange(source: string, start: number, end: number): ProjectEditorSelection {
  const position = (offset: number) => {
    const lines = source.slice(0, offset).split("\n");
    return { column: (lines.at(-1)?.length || 0) + 1, line: lines.length };
  };
  const from = position(start);
  const to = position(end);
  return {
    startLine: from.line,
    startColumn: from.column,
    endLine: to.line,
    endColumn: to.column,
  };
}
