import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import "monaco-editor/esm/vs/editor/edcore.main.js";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api.js";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "@/i18n";
import { showDesktopEditorContextMenu, type DesktopEditorCommand } from "@/lib/desktopBridge";
import { languageFromPath } from "@/lib/fileLanguage";
import { useTheme } from "@/theme/theme";

import type { ProjectEditorReveal } from "./projectReveal";

type MonacoScope = typeof self & {
  MonacoEnvironment?: { getWorker: () => Worker };
};

(self as MonacoScope).MonacoEnvironment = {
  getWorker: () => new EditorWorker(),
};

const lightTheme = "pudding-project-light";
const darkTheme = "pudding-project-dark";

monaco.editor.defineTheme(lightTheme, {
  base: "vs",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#ffffff",
    "editorGutter.background": "#ffffff",
    "editor.lineHighlightBackground": "#f4f4f5",
    "editor.selectionBackground": "#add6ff",
    "editor.inactiveSelectionBackground": "#add6ff",
    "editor.selectionHighlightBackground": "#00000000",
    "editorLineNumber.foreground": "#71717a",
    "editorLineNumber.activeForeground": "#27272a",
  },
});

monaco.editor.defineTheme(darkTheme, {
  base: "vs-dark",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#171717",
    "editorGutter.background": "#171717",
    "editor.lineHighlightBackground": "#ffffff08",
    "editor.selectionBackground": "#264f78",
    "editor.inactiveSelectionBackground": "#264f78",
    "editor.selectionHighlightBackground": "#00000000",
    "editorLineNumber.foreground": "#a1a1aa",
    "editorLineNumber.activeForeground": "#e4e4e7",
  },
});

export type ProjectEditorSelection = {
  startLine: number;
  startColumn: number;
  endLine: number;
  endColumn: number;
};

type SelectionAction = {
  range: ProjectEditorSelection;
  x: number;
  y: number;
};

export function ProjectEditor({
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
  const { t } = useI18n();
  const { resolved } = useTheme();
  const dark = resolved === "dark";
  const containerRef = useRef<HTMLDivElement>(null);
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const darkRef = useRef(dark);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const onReferenceSelectionRef = useRef(onReferenceSelection);
  const revealRef = useRef(reveal);
  const syncingRef = useRef(false);
  const valueRef = useRef(value);
  const [selectionAction, setSelectionAction] = useState<SelectionAction>();
  darkRef.current = dark;
  onChangeRef.current = onChange;
  onSaveRef.current = onSave;
  onReferenceSelectionRef.current = onReferenceSelection;
  revealRef.current = reveal;
  valueRef.current = value;

  useEffect(() => {
    if (!hostRef.current) return;
    let cancelled = false;
    let editor: monaco.editor.IStandaloneCodeEditor | undefined;
    let model: monaco.editor.ITextModel | undefined;
    const disposables: monaco.IDisposable[] = [];
    const language = monacoLanguageFromPath(path);

    void ensureMonacoLanguage(language).then(() => {
      if (cancelled || !hostRef.current) return;
      model = monaco.editor.createModel(valueRef.current, language);
      editor = monaco.editor.create(hostRef.current, {
        model,
        theme: darkRef.current ? darkTheme : lightTheme,
        automaticLayout: true,
        bracketPairColorization: { enabled: true },
        contextmenu: false, // Delegate right-clicks to Electron's native edit menu.
        folding: true,
        fontFamily: "var(--font-mono), ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
        fontSize: 12,
        glyphMargin: false,
        hideCursorInOverviewRuler: true,
        lineHeight: 20,
        lineNumbersMinChars: 3,
        minimap: { enabled: false },
        occurrencesHighlight: "off",
        overviewRulerBorder: false,
        overviewRulerLanes: 0,
        padding: { top: 12, bottom: 12 },
        renderLineHighlight: "all",
        scrollBeyondLastLine: false,
        selectionHighlight: false,
        showFoldingControls: "mouseover",
        stickyScroll: { enabled: false },
        unicodeHighlight: { ambiguousCharacters: false },
        wordWrap: "off",
      });
      editorRef.current = editor;
      disposables.push(
        editor.onDidChangeModelContent(() => {
          if (!syncingRef.current) onChangeRef.current(editor?.getValue() || "");
        }),
        editor.onDidChangeCursorSelection(() => updateSelectionAction(editor, containerRef.current, setSelectionAction)),
        editor.onDidScrollChange(() => updateSelectionAction(editor, containerRef.current, setSelectionAction)),
        editor.onDidLayoutChange(() => updateSelectionAction(editor, containerRef.current, setSelectionAction)),
      );
      editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => onSaveRef.current());
      revealEditorPosition(editor, revealRef.current);
    });

    return () => {
      cancelled = true;
      disposables.forEach((disposable) => disposable.dispose());
      editor?.dispose();
      model?.dispose();
      if (editorRef.current === editor) editorRef.current = null;
    };
  }, [path]);

  useEffect(() => {
    if (!editorRef.current) return;
    monaco.editor.setTheme(dark ? darkTheme : lightTheme);
  }, [dark]);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor || editor.getValue() === value) return;
    syncingRef.current = true;
    editor.setValue(value);
    syncingRef.current = false;
  }, [value]);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    revealEditorPosition(editor, reveal);
  }, [reveal?.serial]);

  return (
    <div
      ref={containerRef}
      className="relative h-full min-h-0 overflow-hidden bg-background dark:bg-[#171717]"
      onContextMenu={(event) => {
        const editor = editorRef.current;
        const model = editor?.getModel();
        if (!editor || !model) return;
        event.preventDefault();
        event.stopPropagation();
        const selection = editor.getSelection();
        const hasSelection = Boolean(selection && !selection.isEmpty());
        void showDesktopEditorContextMenu({
          canCopy: hasSelection,
          canCut: hasSelection,
          canDelete: hasSelection,
          canRedo: model.canRedo(),
          canSelectAll: model.getValueLength() > 0,
          canUndo: model.canUndo(),
          selectionText: selection ? model.getValueInRange(selection) : "",
        }).then((command) => runNativeEditorCommand(editor, command));
      }}
    >
      <div ref={hostRef} className="h-full min-h-0 overflow-hidden" />
      {selectionAction ? (
        <button
          aria-label={t("project.browserReferenceSelection")}
          className="absolute z-30 inline-flex h-8 items-center rounded-md border border-border bg-popover px-3 text-xs font-medium text-popover-foreground shadow-lg shadow-black/20 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none dark:shadow-black/50"
          style={{ left: selectionAction.x, top: selectionAction.y }}
          title={t("project.browserReferenceSelection")}
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

function runNativeEditorCommand(editor: monaco.editor.IStandaloneCodeEditor, command: DesktopEditorCommand | null) {
  if (!command) return;
  editor.focus();
  const editorCommand = {
    copy: "editor.action.clipboardCopyAction",
    cut: "editor.action.clipboardCutAction",
    delete: "deleteLeft",
    paste: "editor.action.clipboardPasteAction",
    pasteAndMatchStyle: "editor.action.clipboardPasteAction",
    redo: "redo",
    selectAll: "editor.action.selectAll",
    undo: "undo",
  } satisfies Record<DesktopEditorCommand, string>;
  editor.trigger("nativeContextMenu", editorCommand[command], null);
}

function revealEditorPosition(editor: monaco.editor.IStandaloneCodeEditor, reveal?: ProjectEditorReveal) {
  const model = editor.getModel();
  if (!model || !reveal || reveal.line <= 0) return;
  const lineNumber = Math.min(reveal.line, model.getLineCount());
  const column = Math.max(1, Math.min(reveal.column || 1, model.getLineMaxColumn(lineNumber)));
  const position = { lineNumber, column };
  editor.setPosition(position);
  editor.revealPositionInCenter(position, monaco.editor.ScrollType.Smooth);
}

function selectedProjectRange(editor: monaco.editor.IStandaloneCodeEditor): ProjectEditorSelection | undefined {
  const selection = editor.getSelection();
  const model = editor.getModel();
  if (!selection || selection.isEmpty() || !model) return undefined;
  const start = selection.getStartPosition();
  const endOffset = Math.max(model.getOffsetAt(start), model.getOffsetAt(selection.getEndPosition()) - 1);
  const end = model.getPositionAt(endOffset);
  return {
    startLine: start.lineNumber,
    startColumn: start.column,
    endLine: end.lineNumber,
    endColumn: end.column,
  };
}

function updateSelectionAction(
  editor: monaco.editor.IStandaloneCodeEditor | undefined,
  container: HTMLDivElement | null,
  setSelectionAction: (action?: SelectionAction) => void,
) {
  if (!editor || !container) {
    setSelectionAction(undefined);
    return;
  }
  const selection = editor.getSelection();
  const range = selectedProjectRange(editor);
  const position = selection?.getPosition();
  const visible = position ? editor.getScrolledVisiblePosition(position) : null;
  if (!range || !visible) {
    setSelectionAction(undefined);
    return;
  }
  const width = 116;
  const x = Math.max(8, Math.min(visible.left, container.clientWidth - width - 8));
  const above = visible.top - 38;
  const y = above >= 8 ? above : visible.top + visible.height + 6;
  setSelectionAction({ range, x, y });
}

function monacoLanguageFromPath(path: string) {
  const filename = path.split(/[\\/]/).filter(Boolean).pop()?.toLowerCase() || "";
  if (filename === "dockerfile" || filename.startsWith("dockerfile.")) return "dockerfile";
  if (filename === ".env" || filename.startsWith(".env.")) return "ini";
  const language = languageFromPath(path) || "plaintext";
  if (language === "shellscript") return "shell";
  if (language === "json" || language === "jsonc") return "javascript";
  if (language === "jsx") return "javascript";
  if (language === "tsx") return "typescript";
  return language;
}

const languageLoaders: Partial<Record<string, () => Promise<unknown>>> = {
  c: () => import("monaco-editor/esm/vs/basic-languages/cpp/cpp.contribution.js"),
  cpp: () => import("monaco-editor/esm/vs/basic-languages/cpp/cpp.contribution.js"),
  csharp: () => import("monaco-editor/esm/vs/basic-languages/csharp/csharp.contribution.js"),
  css: () => import("monaco-editor/esm/vs/basic-languages/css/css.contribution.js"),
  dockerfile: () => import("monaco-editor/esm/vs/basic-languages/dockerfile/dockerfile.contribution.js"),
  go: () => import("monaco-editor/esm/vs/basic-languages/go/go.contribution.js"),
  graphql: () => import("monaco-editor/esm/vs/basic-languages/graphql/graphql.contribution.js"),
  hcl: () => import("monaco-editor/esm/vs/basic-languages/hcl/hcl.contribution.js"),
  html: () => import("monaco-editor/esm/vs/basic-languages/html/html.contribution.js"),
  ini: () => import("monaco-editor/esm/vs/basic-languages/ini/ini.contribution.js"),
  java: () => import("monaco-editor/esm/vs/basic-languages/java/java.contribution.js"),
  javascript: () => import("monaco-editor/esm/vs/basic-languages/javascript/javascript.contribution.js"),
  kotlin: () => import("monaco-editor/esm/vs/basic-languages/kotlin/kotlin.contribution.js"),
  lua: () => import("monaco-editor/esm/vs/basic-languages/lua/lua.contribution.js"),
  markdown: () => import("monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution.js"),
  "objective-c": () => import("monaco-editor/esm/vs/basic-languages/objective-c/objective-c.contribution.js"),
  perl: () => import("monaco-editor/esm/vs/basic-languages/perl/perl.contribution.js"),
  php: () => import("monaco-editor/esm/vs/basic-languages/php/php.contribution.js"),
  powershell: () => import("monaco-editor/esm/vs/basic-languages/powershell/powershell.contribution.js"),
  protobuf: () => import("monaco-editor/esm/vs/basic-languages/protobuf/protobuf.contribution.js"),
  python: () => import("monaco-editor/esm/vs/basic-languages/python/python.contribution.js"),
  ruby: () => import("monaco-editor/esm/vs/basic-languages/ruby/ruby.contribution.js"),
  rust: () => import("monaco-editor/esm/vs/basic-languages/rust/rust.contribution.js"),
  shell: () => import("monaco-editor/esm/vs/basic-languages/shell/shell.contribution.js"),
  sql: () => import("monaco-editor/esm/vs/basic-languages/sql/sql.contribution.js"),
  swift: () => import("monaco-editor/esm/vs/basic-languages/swift/swift.contribution.js"),
  typescript: () => import("monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution.js"),
  xml: () => import("monaco-editor/esm/vs/basic-languages/xml/xml.contribution.js"),
  yaml: () => import("monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution.js"),
};

const loadingLanguages = new Map<string, Promise<unknown>>();

function ensureMonacoLanguage(language: string) {
  const loader = languageLoaders[language];
  if (!loader) return Promise.resolve();
  const existing = loadingLanguages.get(language);
  if (existing) return existing;
  const loading = loader();
  loadingLanguages.set(language, loading);
  return loading;
}
