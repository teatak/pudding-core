import { Check, Copy } from "@/components/icons";
import { useTheme } from "next-themes";
import { useEffect, useRef, useState } from "react";
import Vditor from "vditor";
import "vditor/dist/index.css";
// Vditor ships these as browser side-effect scripts rather than ES modules.
import "vditor/dist/js/i18n/zh_CN.js";
import "vditor/dist/js/icons/ant.js";
import luteURL from "vditor/dist/js/lute/lute.min.js?url";
import mermaidScriptURL from "vditor/dist/js/mermaid/mermaid.min.js?url";

import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";

import type { ProjectEditorSelection } from "./ProjectEditor";
import type { ProjectEditorReveal } from "./projectReveal";

type SelectionAction = {
  range: ProjectEditorSelection;
  x: number;
  y: number;
};

type CodeCopyAction = {
  code: string;
  key: string;
  x: number;
  y: number;
};

type MermaidRenderResult = {
  svg: string;
};

type MermaidAPI = {
  initialize: (config: Record<string, unknown>) => void;
  render: (id: string, source: string) => Promise<MermaidRenderResult>;
  puddingAsyncRenderer?: boolean;
};

type CachedMermaid = {
  renderID: string;
  svg: string;
};

const CODE_BLOCK_LANGUAGE_HINTS = [
  "plaintext",
  "bash",
  "shell",
  "go",
  "javascript",
  "typescript",
  "jsx",
  "tsx",
  "json",
  "yaml",
  "html",
  "css",
  "sql",
  "python",
  "java",
  "c",
  "cpp",
  "csharp",
  "rust",
  "swift",
  "kotlin",
  "php",
  "ruby",
  "dockerfile",
  "diff",
  "markdown",
  "toml",
  "ini",
  "xml",
  "mermaid",
  "plantuml",
  "flowchart",
  "graphviz",
  "math",
];

let vditorMermaidScriptPromise: Promise<void> | undefined;
// Vditor renders every Mermaid block while mounting. Queue the work between
// paints and reuse completed SVGs so it cannot hold up session navigation.
let mermaidRenderQueue = Promise.resolve();
const mermaidRenderCache = new Map<string, CachedMermaid>();
const mermaidRenderPending = new Map<string, Promise<CachedMermaid>>();
const mermaidRenderCacheLimit = 32;

function ensureVditorMermaidScript() {
  const scriptID = "vditorMermaidScript";
  if (vditorMermaidScriptPromise) return vditorMermaidScriptPromise;
  if (document.getElementById(scriptID)) {
    installAsyncMermaidRenderer();
    return Promise.resolve();
  }
  vditorMermaidScriptPromise = new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.id = scriptID;
    script.src = mermaidScriptURL;
    script.onload = () => {
      installAsyncMermaidRenderer();
      resolve();
    };
    script.onerror = () => {
      script.remove();
      vditorMermaidScriptPromise = undefined;
      reject(new Error("failed to load Vditor Mermaid renderer"));
    };
    document.head.appendChild(script);
  });
  return vditorMermaidScriptPromise;
}

function installAsyncMermaidRenderer() {
  const mermaid = (window as Window & { mermaid?: MermaidAPI }).mermaid;
  if (!mermaid || mermaid.puddingAsyncRenderer) return;

  const initialize = mermaid.initialize.bind(mermaid);
  const render = mermaid.render.bind(mermaid);
  let activeConfig: Record<string, unknown> = {};

  mermaid.initialize = (config) => {
    activeConfig = config;
    initialize(config);
  };
  mermaid.render = (id, source) => {
    const config = activeConfig;
    const cacheKey = `${JSON.stringify(config)}\n${source}`;
    const cached = mermaidRenderCache.get(cacheKey);
    if (cached) {
      mermaidRenderCache.delete(cacheKey);
      mermaidRenderCache.set(cacheKey, cached);
      return Promise.resolve(rebaseMermaidSVG(cached, id));
    }

    let pending = mermaidRenderPending.get(cacheKey);
    if (!pending) {
      pending = mermaidRenderQueue
        .then(waitForMermaidRenderOpportunity)
        .then(async () => {
          initialize(config);
          const result = await render(id, source);
          const next = { renderID: id, svg: result.svg };
          mermaidRenderCache.set(cacheKey, next);
          if (mermaidRenderCache.size > mermaidRenderCacheLimit) {
            const oldestKey = mermaidRenderCache.keys().next().value;
            if (oldestKey) mermaidRenderCache.delete(oldestKey);
          }
          return next;
        });
      mermaidRenderPending.set(cacheKey, pending);
      mermaidRenderQueue = pending.then(() => undefined, () => undefined);
      void pending.then(
        () => mermaidRenderPending.delete(cacheKey),
        () => mermaidRenderPending.delete(cacheKey),
      );
    }
    return pending.then((result) => rebaseMermaidSVG(result, id));
  };
  mermaid.puddingAsyncRenderer = true;
}

function rebaseMermaidSVG(cached: CachedMermaid, renderID: string): MermaidRenderResult {
  return {
    svg: cached.renderID === renderID
      ? cached.svg
      : cached.svg.split(cached.renderID).join(renderID),
  };
}

function waitForMermaidRenderOpportunity() {
  return new Promise<void>((resolve) => {
    requestAnimationFrame(() => window.setTimeout(resolve, 0));
  });
}

function scheduleAfterPaint(callback: () => void) {
  let firstFrame = 0;
  let secondFrame = 0;
  let idleCallback = 0;
  firstFrame = requestAnimationFrame(() => {
    secondFrame = requestAnimationFrame(() => {
      idleCallback = window.requestIdleCallback(callback, { timeout: 300 });
    });
  });
  return () => {
    cancelAnimationFrame(firstFrame);
    cancelAnimationFrame(secondFrame);
    if (idleCallback) window.cancelIdleCallback(idleCallback);
  };
}

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
  const copiedResetTimerRef = useRef<number | undefined>(undefined);
  const [editorReady, setEditorReady] = useState(false);
  const [selectionAction, setSelectionAction] = useState<SelectionAction>();
  const [codeCopyActions, setCodeCopyActions] = useState<CodeCopyAction[]>([]);
  const [hoveredCodeKey, setHoveredCodeKey] = useState<string>();
  const [copiedCodeKey, setCopiedCodeKey] = useState<string>();

  onChangeRef.current = onChange;
  onSaveRef.current = onSave;
  onReferenceSelectionRef.current = onReferenceSelection;
  revealRef.current = reveal;

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let tableObserver: MutationObserver | undefined;
    let editorResizeObserver: ResizeObserver | undefined;
    let codeCopyLayoutFrame = 0;
    setEditorReady(false);
    const scheduleCodeCopyLayout = (editor: Vditor) => {
      window.cancelAnimationFrame(codeCopyLayoutFrame);
      codeCopyLayoutFrame = window.requestAnimationFrame(() => {
        if (disposed) return;
        setCodeCopyActions(resolveCodeCopyActions(editor, containerRef.current));
      });
    };
    const dark = document.documentElement.classList.contains("dark");
    let editor: Vditor | undefined;
    const cancelStart = scheduleAfterPaint(() => {
      void ensureVditorMermaidScript().catch(() => undefined).then(() => {
        if (disposed) return;
        const nextEditor = new Vditor(host, {
          _lutePath: luteURL,
          cache: { enable: false },
          height: "auto",
          icon: null as never,
          i18n: window.VditorI18n,
          lang: locale === "zh-TW" ? "zh_TW" : locale === "en" ? "en_US" : "zh_CN",
          mode: "ir",
          placeholder: t("project.browserMarkdownEditor"),
          preview: {
            hljs: {
              enable: false,
              langs: CODE_BLOCK_LANGUAGE_HINTS,
            },
            markdown: {
              codeBlockPreview: true,
              mathBlockPreview: false,
              sanitize: true,
            },
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
              nextEditor.destroy();
              return;
            }
            vditorRef.current = nextEditor;
            setEditorReady(true);
            const editorRoot = nextEditor.vditor.ir?.element;
            if (editorRoot) {
              tableObserver = new MutationObserver(() => {
                scheduleTableLayout(nextEditor);
                scheduleCodeCopyLayout(nextEditor);
              });
              tableObserver.observe(editorRoot, {
                attributes: true,
                attributeFilter: ["class"],
                characterData: true,
                childList: true,
                subtree: true,
              });
            }
            if (containerRef.current) {
              editorResizeObserver = new ResizeObserver(() => scheduleCodeCopyLayout(nextEditor));
              editorResizeObserver.observe(containerRef.current);
            }
            scheduleTableLayout(nextEditor);
            scheduleCodeCopyLayout(nextEditor);
            revealEditorPosition(nextEditor, revealRef.current);
          },
          input: (markdown) => {
            valueRef.current = markdown;
            setSelectionAction(undefined);
            scheduleTableLayout(nextEditor);
            scheduleCodeCopyLayout(nextEditor);
            if (!syncingRef.current) onChangeRef.current(markdown);
          },
        });
        editor = nextEditor;
      });
    });

    return () => {
      disposed = true;
      cancelStart();
      tableObserver?.disconnect();
      editorResizeObserver?.disconnect();
      window.cancelAnimationFrame(codeCopyLayoutFrame);
      if (editor && vditorRef.current === editor) vditorRef.current = null;
      editor?.destroy();
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

  useEffect(() => {
    return () => {
      if (copiedResetTimerRef.current) {
        window.clearTimeout(copiedResetTimerRef.current);
      }
    };
  }, []);

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
        if (
          (event.target as HTMLElement).closest(
            "[data-project-selection-action], [data-vditor-code-copy]",
          )
        ) {
          return;
        }
        updateSelectionAction();
      }}
      onPointerMoveCapture={(event) => {
        const target = event.target as HTMLElement;
        const copyButton = target.closest<HTMLElement>("[data-vditor-code-copy]");
        if (copyButton) {
          setHoveredCodeKey(copyButton.dataset.codeKey);
          return;
        }
        const codeBlock = target.closest<HTMLElement>(
          '[data-type="code-block"].vditor-ir__node',
        );
        const editorRoot = vditorRef.current?.vditor.ir?.element;
        if (!codeBlock || !editorRoot) {
          setHoveredCodeKey(undefined);
          return;
        }
        const codeElements = Array.from(
          editorRoot.querySelectorAll<HTMLElement>(
            '[data-type="code-block"].vditor-ir__node > pre.vditor-ir__marker--pre > code',
          ),
        );
        const index = codeElements.findIndex((element) =>
          codeBlock.contains(element),
        );
        setHoveredCodeKey(index >= 0 ? String(index) : undefined);
      }}
      onPointerLeave={() => setHoveredCodeKey(undefined)}
      onScrollCapture={() => setSelectionAction(undefined)}
    >
      <div ref={hostRef} className="pudding-vditor-host min-h-full" />
      {!editorReady ? (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-muted-foreground">
          <Spinner aria-label={t("common.loading")} className="size-5" />
        </div>
      ) : null}
      {codeCopyActions.map((action) => {
        const copied = copiedCodeKey === action.key;
        return (
          <button
            key={action.key}
            aria-label={t(copied ? "common.copied" : "common.copy")}
            className="absolute z-20 inline-flex size-5 items-center justify-center rounded-md border border-border/70 bg-background text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground hover:opacity-100 data-[visible=1]:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
            data-code-key={action.key}
            data-visible={
              copied || hoveredCodeKey === action.key ? "1" : undefined
            }
            data-vditor-code-copy=""
            style={{ left: action.x, top: action.y }}
            tabIndex={-1}
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              void navigator.clipboard.writeText(action.code).then(() => {
                setCopiedCodeKey(action.key);
                if (copiedResetTimerRef.current) {
                  window.clearTimeout(copiedResetTimerRef.current);
                }
                copiedResetTimerRef.current = window.setTimeout(
                  () => setCopiedCodeKey(undefined),
                  1500,
                );
              });
            }}
            onMouseDown={(event) => event.preventDefault()}
          >
            {copied ? <Check className="size-3 text-success" /> : <Copy className="size-3" />}
          </button>
        );
      })}
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

function resolveCodeCopyActions(
  editor: Vditor,
  container: HTMLDivElement | null,
): CodeCopyAction[] {
  const editorRoot = editor.vditor.ir?.element;
  if (!editorRoot || !container) return [];

  const containerRect = container.getBoundingClientRect();
  const buttonSize = 20;
  const inset = 8;
  return Array.from(
    editorRoot.querySelectorAll<HTMLElement>(
      '[data-type="code-block"].vditor-ir__node > pre.vditor-ir__marker--pre > code',
    ),
  ).map((codeElement, index) => {
    const codeBlock = codeElement.closest<HTMLElement>(
      '[data-type="code-block"].vditor-ir__node',
    );
    const preview = codeBlock?.querySelector<HTMLElement>(
      ":scope > .vditor-ir__preview",
    );
    const previewContent = preview?.querySelector<Element>(
      "svg, canvas, img, pre",
    );
    const anchor = codeBlock?.classList.contains("vditor-ir__node--expand")
      ? codeElement
      : previewContent || preview || codeElement;
    const rect = anchor.getBoundingClientRect();
    return {
      code: (codeElement.textContent || "").replace(/\n$/, ""),
      key: String(index),
      x: Math.max(
        container.scrollLeft + inset,
        Math.min(
          rect.right - containerRect.left + container.scrollLeft - buttonSize - inset,
          container.scrollLeft + container.clientWidth - buttonSize - inset,
        ),
      ),
      y: rect.top - containerRect.top + container.scrollTop + inset,
    };
  });
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
      table.dataset.puddingVditorTableLayout = "grid";
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
