import { useTheme } from "next-themes";
import { useEffect, useRef, useState } from "react";
import Vditor from "vditor";
import "vditor/dist/index.css";
// Vditor ships these as browser side-effect scripts rather than ES modules.
import "vditor/dist/js/i18n/zh_CN.js";
import "vditor/dist/js/icons/ant.js";
import luteURL from "vditor/dist/js/lute/lute.min.js?url";

import { useI18n } from "@/i18n";

import type { ProjectEditorSelection } from "./ProjectEditor";
import type { ProjectEditorReveal } from "./projectReveal";

type SelectionAction = {
  range: ProjectEditorSelection;
  x: number;
  y: number;
};

export function ProjectMarkdownEditor({
  path,
  reveal,
  value,
  onChange,
  onSave,
  onReferenceSelection,
}: {
  path: string;
  reveal?: ProjectEditorReveal;
  value: string;
  onChange: (value: string) => void;
  onSave: () => void;
  onReferenceSelection?: (selection: ProjectEditorSelection) => void;
}) {
  const { locale, t } = useI18n();
  const { resolvedTheme } = useTheme();
  const containerRef = useRef<HTMLDivElement>(null);
  const hostRef = useRef<HTMLDivElement>(null);
  const vditorRef = useRef<Vditor | null>(null);
  const valueRef = useRef(value);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const onReferenceSelectionRef = useRef(onReferenceSelection);
  const revealRef = useRef(reveal);
  const syncingRef = useRef(false);
  const [selectionAction, setSelectionAction] = useState<SelectionAction>();

  onChangeRef.current = onChange;
  onSaveRef.current = onSave;
  onReferenceSelectionRef.current = onReferenceSelection;
  revealRef.current = reveal;

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let tableObserver: MutationObserver | undefined;
    const dark = document.documentElement.classList.contains("dark");
    const editor = new Vditor(host, {
      _lutePath: luteURL,
      cache: { enable: false },
      height: "auto",
      icon: null as never,
      i18n: window.VditorI18n,
      lang: locale === "zh-TW" ? "zh_TW" : locale === "en" ? "en_US" : "zh_CN",
      mode: "ir",
      placeholder: t("project.browserMarkdownEditor"),
      preview: {
        hljs: { enable: false },
        markdown: {
          codeBlockPreview: false,
          mathBlockPreview: false,
          sanitize: true,
        },
        maxWidth: 896,
        mode: "editor",
        theme: {
          current: "",
          path: "",
        },
      },
      theme: dark ? "dark" : "classic",
      toolbar: [],
      value: valueRef.current,
      after: () => {
        if (disposed) {
          editor.destroy();
          return;
        }
        vditorRef.current = editor;
        const editorRoot = editor.vditor.ir?.element;
        if (editorRoot) {
          tableObserver = new MutationObserver(() => scheduleTableLayout(editor));
          tableObserver.observe(editorRoot, { childList: true, subtree: true });
        }
        scheduleTableLayout(editor);
        revealEditorPosition(editor, revealRef.current);
      },
      input: (markdown) => {
        valueRef.current = markdown;
        setSelectionAction(undefined);
        scheduleTableLayout(editor);
        if (!syncingRef.current) onChangeRef.current(markdown);
      },
    });

    return () => {
      disposed = true;
      tableObserver?.disconnect();
      if (vditorRef.current === editor) vditorRef.current = null;
      editor.destroy();
    };
  }, [locale, path]);

  useEffect(() => {
    if (value === valueRef.current) return;
    valueRef.current = value;
    const editor = vditorRef.current;
    if (!editor?.vditor?.lute) return;
    syncingRef.current = true;
    editor.setValue(value, true);
    syncingRef.current = false;
    scheduleTableLayout(editor);
  }, [value]);

  useEffect(() => {
    vditorRef.current?.setTheme(resolvedTheme === "dark" ? "dark" : "classic");
  }, [resolvedTheme]);

  useEffect(() => {
    const editor = vditorRef.current;
    if (editor) revealEditorPosition(editor, reveal);
  }, [reveal?.serial]);

  const updateSelectionAction = () => {
    requestAnimationFrame(() => {
      setSelectionAction(
        resolveSelectionAction(
          vditorRef.current,
          containerRef.current,
          onReferenceSelectionRef.current,
        ),
      );
    });
  };

  return (
    <div
      ref={containerRef}
      className="pudding-vditor-editor relative h-full min-h-0 overflow-auto"
      onKeyDownCapture={(event) => {
        if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s") {
          event.preventDefault();
          onSaveRef.current();
        }
      }}
      onKeyUpCapture={updateSelectionAction}
      onMouseUpCapture={(event) => {
        if ((event.target as HTMLElement).closest("[data-project-selection-action]")) return;
        updateSelectionAction();
      }}
      onScrollCapture={() => setSelectionAction(undefined)}
    >
      <div ref={hostRef} className="pudding-vditor-host min-h-full" />
      {selectionAction ? (
        <button
          aria-label={t("project.browserReferenceSelection")}
          className="absolute z-30 inline-flex h-8 items-center rounded-md border border-border bg-popover px-3 text-xs font-medium text-popover-foreground shadow-lg shadow-black/20 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none dark:shadow-black/50"
          data-project-selection-action=""
          style={{ left: selectionAction.x, top: selectionAction.y }}
          type="button"
          onClick={() => {
            onReferenceSelectionRef.current?.(selectionAction.range);
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

function resolveSelectionAction(
  editor: Vditor | null,
  container: HTMLDivElement | null,
  onReferenceSelection: ((selection: ProjectEditorSelection) => void) | undefined,
): SelectionAction | undefined {
  const editorRoot = editor?.vditor.ir?.element;
  const selection = window.getSelection();
  if (!editor || !editorRoot || !container || !onReferenceSelection || !selection || selection.rangeCount === 0) {
    return undefined;
  }
  const domRange = selection.getRangeAt(0);
  if (
    domRange.collapsed ||
    !domRange.toString().trim() ||
    !editorRoot.contains(domRange.startContainer) ||
    !editorRoot.contains(domRange.endContainer)
  ) {
    return undefined;
  }
  const range = projectRangeFromDOMSelection(editor, domRange);
  if (!range) return undefined;

  const clientRects = Array.from(domRange.getClientRects()).filter(
    (rect) => rect.width > 0 && rect.height > 0,
  );
  const anchor = clientRects.at(-1) || domRange.getBoundingClientRect();
  const containerRect = container.getBoundingClientRect();
  const width = 116;
  const x = Math.max(
    container.scrollLeft + 8,
    Math.min(
      anchor.right - containerRect.left + container.scrollLeft - width,
      container.scrollLeft + container.clientWidth - width - 8,
    ),
  );
  const anchorTop = anchor.top - containerRect.top + container.scrollTop;
  const anchorBottom = anchor.bottom - containerRect.top + container.scrollTop;
  const above = anchorTop - 38;
  const y = above >= container.scrollTop + 8 ? above : anchorBottom + 6;
  return { range, x, y };
}

function projectRangeFromDOMSelection(
  editor: Vditor,
  domRange: Range,
): ProjectEditorSelection | undefined {
  const editorRoot = editor.vditor.ir?.element;
  if (!editorRoot) return undefined;
  const startPath = nodePath(editorRoot, domRange.startContainer);
  const endPath = nodePath(editorRoot, domRange.endContainer);
  if (!startPath || !endPath) return undefined;

  const temporaryRoot = editorRoot.cloneNode(true) as HTMLElement;
  const startNode = nodeAtPath(temporaryRoot, startPath);
  const endNode = nodeAtPath(temporaryRoot, endPath);
  if (!startNode || !endNode) return undefined;

  const markerID = `${Date.now()}${Math.random().toString(36).slice(2)}`.replace(/\W/g, "");
  const startMarker = `PUDDINGSELECTIONSTART${markerID}X`;
  const endMarker = `PUDDINGSELECTIONEND${markerID}X`;
  insertTextMarker(endNode, domRange.endOffset, endMarker);
  insertTextMarker(startNode, domRange.startOffset, startMarker);

  const markedMarkdown = editor.vditor.lute.VditorIRDOM2Md(temporaryRoot.innerHTML);
  const startOffset = markedMarkdown.indexOf(startMarker);
  const markedEndOffset = markedMarkdown.indexOf(endMarker);
  if (startOffset < 0 || markedEndOffset < 0 || markedEndOffset <= startOffset) return undefined;

  const endExclusiveOffset = markedEndOffset - startMarker.length;
  const markdown = markedMarkdown.replace(startMarker, "").replace(endMarker, "");
  const start = markdownPosition(markdown, startOffset);
  const end = markdownPosition(markdown, Math.max(startOffset, endExclusiveOffset - 1));
  return {
    startLine: start.line,
    startColumn: start.column,
    endLine: end.line,
    endColumn: end.column,
  };
}

function insertTextMarker(node: Node, offset: number, marker: string) {
  const range = document.createRange();
  const maxOffset =
    node.nodeType === Node.TEXT_NODE ? node.textContent?.length ?? 0 : node.childNodes.length;
  range.setStart(node, Math.max(0, Math.min(offset, maxOffset)));
  range.collapse(true);
  range.insertNode(document.createTextNode(marker));
}

function markdownPosition(markdown: string, offset: number) {
  const prefix = markdown.slice(0, Math.max(0, Math.min(offset, markdown.length)));
  const lines = prefix.split("\n");
  return {
    line: lines.length,
    column: (lines.at(-1)?.length ?? 0) + 1,
  };
}

function scheduleTableLayout(editor: Vditor) {
  requestAnimationFrame(() => {
    const editorRoot = editor.vditor.ir?.element;
    if (!editorRoot?.isConnected) return;
    editorRoot.querySelectorAll<HTMLTableElement>("table").forEach((table) => {
      const columnCount = Array.from(table.rows).reduce(
        (maximum, row) =>
          Math.max(
            maximum,
            Array.from(row.cells).reduce((count, cell) => count + Math.max(1, cell.colSpan), 0),
          ),
        1,
      );
      table.style.setProperty("--pudding-vditor-table-columns", String(columnCount));
    });
  });
}

function revealEditorPosition(editor: Vditor, reveal: ProjectEditorReveal | undefined) {
  const editorRoot = editor.vditor.ir?.element;
  if (!reveal || !editor.vditor?.lute || !editorRoot) return;
  const markdown = editor.getValue();
  const offset = markdownOffset(markdown, reveal.line, reveal.column || 1);
  const marker = `PUDDINGREVEAL${Date.now()}X`;
  const markedHTML = editor.vditor.lute.Md2VditorIRDOM(
    `${markdown.slice(0, offset)}${marker}${markdown.slice(offset)}`,
  );
  const temporaryRoot = document.createElement("div");
  temporaryRoot.innerHTML = markedHTML;
  const markedNode = findTextNode(temporaryRoot, marker);
  if (!markedNode) return;
  const path = nodePath(temporaryRoot, markedNode);
  const liveNode = path ? nodeAtPath(editorRoot, path) : undefined;
  if (!liveNode) return;
  const markerOffset = markedNode.textContent?.indexOf(marker) ?? -1;
  const maxOffset = liveNode.textContent?.length ?? 0;
  if (markerOffset < 0) return;

  const range = document.createRange();
  range.setStart(liveNode, Math.min(markerOffset, maxOffset));
  range.collapse(true);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
  editor.focus();
  const block = liveNode.parentElement?.closest<HTMLElement>("[data-block='0']");
  (block || liveNode.parentElement)?.scrollIntoView({ block: "center" });
}

function findTextNode(root: Node, text: string) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    if (node.textContent?.includes(text)) return node;
    node = walker.nextNode();
  }
  return undefined;
}

function nodePath(root: Node, node: Node) {
  const path: number[] = [];
  let current: Node | null = node;
  while (current && current !== root) {
    const parentNode: Node | null = current.parentNode;
    if (!parentNode) return undefined;
    path.unshift(Array.prototype.indexOf.call(parentNode.childNodes, current));
    current = parentNode;
  }
  return current === root ? path : undefined;
}

function nodeAtPath(root: Node, path: number[]) {
  let current: Node | undefined = root;
  for (const index of path) {
    current = current.childNodes[index];
    if (!current) return undefined;
  }
  return current;
}

function markdownOffset(markdown: string, line: number, column: number) {
  const lines = markdown.split("\n");
  const lineIndex = Math.max(0, Math.min(line - 1, lines.length - 1));
  let offset = 0;
  for (let index = 0; index < lineIndex; index += 1) {
    offset += lines[index].length + 1;
  }
  return offset + Math.max(0, Math.min(column - 1, lines[lineIndex]?.length ?? 0));
}
