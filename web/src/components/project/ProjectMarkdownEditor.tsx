import { redo, redoDepth, undo, undoDepth } from "@codemirror/commands";
import { markdown } from "@codemirror/lang-markdown";
import { EditorState, Facet, type Range, Transaction } from "@codemirror/state";
import {
  Decoration,
  EditorView,
  keymap,
  ViewPlugin,
  WidgetType,
  type DecorationSet,
  type ViewUpdate,
} from "@codemirror/view";
import { syntaxTree } from "@codemirror/language";
import { GFM } from "@lezer/markdown";
import { minimalSetup } from "codemirror";
import { useEffect, useRef, useState } from "react";

import { useI18n } from "@/i18n";
import { showDesktopEditorContextMenu, type DesktopEditorCommand } from "@/lib/desktopBridge";

import type { ProjectEditorSelection } from "./ProjectEditor";
import type { ProjectEditorReveal } from "./projectReveal";

type SelectionAction = {
  range: ProjectEditorSelection;
  x: number;
  y: number;
};

type FrontmatterBlock = {
  contentFrom: number;
  endLine: number;
  from: number;
  startLine: number;
  to: number;
};

const frontmatterLabelFacet = Facet.define<string, string>({
  combine: (values) => values[0] || "Properties",
});

const headingClasses: Record<string, string> = {
  ATXHeading1: "cm-live-heading cm-live-h1",
  ATXHeading2: "cm-live-heading cm-live-h2",
  ATXHeading3: "cm-live-heading cm-live-h3",
  ATXHeading4: "cm-live-heading cm-live-h4",
  ATXHeading5: "cm-live-heading cm-live-h5",
  ATXHeading6: "cm-live-heading cm-live-h6",
  SetextHeading1: "cm-live-heading cm-live-h1",
  SetextHeading2: "cm-live-heading cm-live-h2",
};

const hiddenMarkerNames = new Set([
  "CodeMark",
  "EmphasisMark",
  "HeaderMark",
  "LinkMark",
  "StrikethroughMark",
]);

const livePreviewPlugin = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = livePreviewDecorations(view);
    }

    update(update: ViewUpdate) {
      if (
        update.docChanged ||
        update.selectionSet ||
        update.viewportChanged ||
        update.geometryChanged ||
        update.focusChanged
      ) {
        this.decorations = livePreviewDecorations(update.view);
      }
    }
  },
  { decorations: (plugin) => plugin.decorations },
);

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
  const { t } = useI18n();
  const editorLabel = t("project.browserMarkdownEditor");
  const frontmatterLabel = t("project.browserFrontmatter");
  const containerRef = useRef<HTMLDivElement>(null);
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const onReferenceSelectionRef = useRef(onReferenceSelection);
  const revealRef = useRef(reveal);
  const syncingRef = useRef(false);
  const mouseSelectingRef = useRef(false);
  const valueRef = useRef(value);
  const [selectionAction, setSelectionAction] = useState<SelectionAction>();
  onChangeRef.current = onChange;
  onSaveRef.current = onSave;
  onReferenceSelectionRef.current = onReferenceSelection;
  revealRef.current = reveal;
  valueRef.current = value;

  useEffect(() => {
    if (!hostRef.current) return;
    const view = new EditorView({
      parent: hostRef.current,
      state: EditorState.create({
        doc: valueRef.current,
        selection: { anchor: initialMarkdownCursor(valueRef.current) },
        extensions: [
          minimalSetup,
          markdown({ extensions: GFM }),
          frontmatterLabelFacet.of(frontmatterLabel),
          EditorView.lineWrapping,
          livePreviewPlugin,
          EditorView.domEventHandlers({
            mousedown: (event) => {
              if (event.button !== 0) return false;
              mouseSelectingRef.current = true;
              setSelectionAction(undefined);
              return false;
            },
          }),
          EditorView.contentAttributes.of({
            "aria-label": editorLabel,
            "data-project-markdown-input": "true",
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !syncingRef.current) {
              onChangeRef.current(update.state.doc.toString());
            }
            if (
              !mouseSelectingRef.current
              && (update.docChanged || update.selectionSet || update.viewportChanged || update.geometryChanged)
            ) {
              updateSelectionAction(update.view, containerRef.current, setSelectionAction);
            }
          }),
          keymap.of([
            {
              key: "Mod-s",
              preventDefault: true,
              run: () => {
                onSaveRef.current();
                return true;
              },
            },
          ]),
        ],
      }),
    });
    editorRef.current = view;
    const finishMouseSelection = () => {
      if (!mouseSelectingRef.current) return;
      mouseSelectingRef.current = false;
      requestAnimationFrame(() => {
        if (editorRef.current === view) {
          updateSelectionAction(view, containerRef.current, setSelectionAction);
        }
      });
    };
    window.addEventListener("mouseup", finishMouseSelection);
    revealEditorPosition(view, revealRef.current);
    return () => {
      window.removeEventListener("mouseup", finishMouseSelection);
      view.destroy();
      if (editorRef.current === view) editorRef.current = null;
    };
  }, [editorLabel, frontmatterLabel, path]);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor || editor.state.doc.toString() === value) return;
    syncingRef.current = true;
    editor.dispatch({
      changes: { from: 0, to: editor.state.doc.length, insert: value },
      annotations: Transaction.addToHistory.of(false),
    });
    syncingRef.current = false;
  }, [value]);

  useEffect(() => {
    if (editorRef.current) revealEditorPosition(editorRef.current, reveal);
  }, [reveal?.serial]);

  return (
    <div
      ref={containerRef}
      className="pudding-markdown-live-editor selectable-text relative h-full min-h-0 overflow-hidden bg-[var(--workspace-file-editor-background)]"
      onContextMenu={(event) => {
        const editor = editorRef.current;
        if (!editor) return;
        event.preventDefault();
        event.stopPropagation();
        const selection = editor.state.selection.main;
        const hasSelection = !selection.empty;
        void showDesktopEditorContextMenu({
          canCopy: hasSelection,
          canCut: hasSelection,
          canDelete: hasSelection,
          canRedo: redoDepth(editor.state) > 0,
          canSelectAll: editor.state.doc.length > 0,
          canUndo: undoDepth(editor.state) > 0,
          selectionText: editor.state.sliceDoc(selection.from, selection.to),
        }).then((command) => runNativeEditorCommand(editor, command));
      }}
    >
      <div ref={hostRef} className="h-full min-h-0 overflow-hidden" />
      {selectionAction ? (
        <button
          aria-label={t("project.browserReferenceSelection")}
          className="absolute z-30 inline-flex h-8 items-center rounded-md border border-border bg-popover px-3 text-xs font-medium text-popover-foreground shadow-lg shadow-black/20 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none dark:shadow-black/50"
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

function livePreviewDecorations(view: EditorView) {
  const decorations: Range<Decoration>[] = [];
  const activeLines = selectedLines(view.state);
  const lineDecorations = new Set<string>();
  const document = view.state.doc;
  const frontmatter = markdownFrontmatter(document);
  const frontmatterActive = Boolean(
    frontmatter
    && linesIntersect(activeLines, frontmatter.startLine, frontmatter.endLine),
  );

  const addLineDecoration = (position: number, className: string) => {
    const line = document.lineAt(position);
    const key = `${line.from}:${className}`;
    if (lineDecorations.has(key)) return;
    lineDecorations.add(key);
    decorations.push(Decoration.line({ class: className }).range(line.from));
  };

  for (let lineNumber = 1; lineNumber <= document.lines; lineNumber += 1) {
    const line = document.line(lineNumber);
    if (frontmatter && lineNumber >= frontmatter.startLine && lineNumber <= frontmatter.endLine) {
      if (frontmatterActive) {
        addLineDecoration(line.from, "cm-live-frontmatter");
      } else {
        addLineDecoration(
          line.from,
          lineNumber === frontmatter.startLine
            ? "cm-live-frontmatter-summary-line"
            : "cm-live-frontmatter-hidden-line",
        );
      }
      continue;
    }
    if (!line.text.trim() && !activeLines.has(lineNumber)) {
      addLineDecoration(line.from, "cm-live-empty-line");
    }
  }

  if (frontmatter && !frontmatterActive) {
    decorations.push(
      Decoration.widget({
        widget: new FrontmatterWidget(
          view.state.facet(frontmatterLabelFacet),
          frontmatterSummary(document, frontmatter),
          frontmatter.contentFrom,
        ),
      }).range(frontmatter.from),
    );
  }

  syntaxTree(view.state).iterate({
    enter(node) {
      const name = node.name;
      if (frontmatter && node.from >= frontmatter.from && node.to <= frontmatter.to) {
        return false;
      }
      const active = rangeTouchesLines(document, node.from, node.to, activeLines);
      const headingClass = headingClasses[name];
      if (headingClass) {
        addLineDecoration(node.from, headingClass);
      } else if (name === "ListItem") {
        forEachLine(document, node.from, node.to, (lineFrom) => addLineDecoration(lineFrom, "cm-live-list-item"));
      } else if (name === "Blockquote") {
        forEachLine(document, node.from, node.to, (lineFrom) => addLineDecoration(lineFrom, "cm-live-blockquote"));
      } else if (name === "FencedCode" || name === "CodeBlock") {
        forEachLine(document, node.from, node.to, (lineFrom) => addLineDecoration(lineFrom, "cm-live-code-block"));
      } else if (name === "StrongEmphasis") {
        decorations.push(Decoration.mark({ class: "cm-live-strong" }).range(node.from, node.to));
      } else if (name === "Emphasis") {
        decorations.push(Decoration.mark({ class: "cm-live-emphasis" }).range(node.from, node.to));
      } else if (name === "Strikethrough") {
        decorations.push(Decoration.mark({ class: "cm-live-strikethrough" }).range(node.from, node.to));
      } else if (name === "InlineCode") {
        decorations.push(Decoration.mark({ class: "cm-live-inline-code" }).range(node.from, node.to));
      } else if (name === "Link") {
        decorations.push(Decoration.mark({ class: "cm-live-link" }).range(node.from, node.to));
      } else if (name === "Image" && !active) {
        const raw = document.sliceString(node.from, node.to);
        decorations.push(
          Decoration.replace({ widget: new ImageLabelWidget(imageAlt(raw)) }).range(node.from, node.to),
        );
        return false;
      } else if (name === "HorizontalRule" && !active) {
        decorations.push(Decoration.replace({ widget: new HorizontalRuleWidget() }).range(node.from, node.to));
        return false;
      } else if (name === "ListMark" && !active) {
        const raw = document.sliceString(node.from, node.to);
        decorations.push(Decoration.replace({ widget: new ListMarkWidget(raw) }).range(node.from, node.to));
      } else if (name === "TaskMarker" && !active) {
        const raw = document.sliceString(node.from, node.to);
        decorations.push(Decoration.replace({ widget: new TaskMarkerWidget(/x/i.test(raw)) }).range(node.from, node.to));
      } else if (name === "QuoteMark" && !active) {
        decorations.push(Decoration.replace({}).range(node.from, trailingSpaceEnd(document, node.to)));
      } else if (hiddenMarkerNames.has(name) && !active) {
        const to = name === "HeaderMark" ? trailingSpaceEnd(document, node.to) : node.to;
        decorations.push(Decoration.replace({}).range(node.from, to));
      } else if (name === "URL" && node.node.parent?.name === "Link" && !active) {
        decorations.push(Decoration.replace({}).range(node.from, node.to));
      }
      return undefined;
    },
  });

  return Decoration.set(decorations, true);
}

function selectedLines(state: EditorState) {
  const lines = new Set<number>();
  for (const range of state.selection.ranges) {
    const start = state.doc.lineAt(range.from).number;
    const end = state.doc.lineAt(range.to).number;
    for (let line = start; line <= end; line += 1) lines.add(line);
  }
  return lines;
}

function rangeTouchesLines(
  document: EditorState["doc"],
  from: number,
  to: number,
  lines: Set<number>,
) {
  const start = document.lineAt(from).number;
  const end = document.lineAt(Math.max(from, to - 1)).number;
  for (let line = start; line <= end; line += 1) {
    if (lines.has(line)) return true;
  }
  return false;
}

function forEachLine(document: EditorState["doc"], from: number, to: number, visit: (lineFrom: number) => void) {
  let line = document.lineAt(from);
  const end = document.lineAt(Math.max(from, to - 1)).number;
  while (line.number <= end) {
    visit(line.from);
    if (line.number >= document.lines) break;
    line = document.line(line.number + 1);
  }
}

function trailingSpaceEnd(document: EditorState["doc"], position: number) {
  const line = document.lineAt(position);
  let end = position;
  while (end < line.to && /\s/.test(document.sliceString(end, end + 1))) end += 1;
  return end;
}

function linesIntersect(lines: Set<number>, start: number, end: number) {
  for (let line = start; line <= end; line += 1) {
    if (lines.has(line)) return true;
  }
  return false;
}

function markdownFrontmatter(document: EditorState["doc"]): FrontmatterBlock | undefined {
  if (document.lines < 2 || document.line(1).text.trim() !== "---") return undefined;
  for (let lineNumber = 2; lineNumber <= document.lines; lineNumber += 1) {
    const line = document.line(lineNumber);
    if (line.text.trim() !== "---") continue;
    const nextLineFrom = lineNumber < document.lines ? document.line(lineNumber + 1).from : line.to;
    return {
      contentFrom: document.line(2).from,
      endLine: lineNumber,
      from: 0,
      startLine: 1,
      to: nextLineFrom,
    };
  }
  return undefined;
}

function frontmatterSummary(document: EditorState["doc"], frontmatter: FrontmatterBlock) {
  let count = 0;
  let title = "";
  for (let lineNumber = frontmatter.startLine + 1; lineNumber < frontmatter.endLine; lineNumber += 1) {
    const match = /^([\w.-]+):\s*(.*)$/.exec(document.line(lineNumber).text.trim());
    if (!match) continue;
    count += 1;
    if (match[1] === "title") title = match[2];
  }
  return { count, title };
}

function initialMarkdownCursor(value: string) {
  const match = /^(?:\uFEFF)?---\r?\n[\s\S]*?\r?\n---(?:\r?\n|$)/.exec(value);
  return match?.[0].length || 0;
}

class FrontmatterWidget extends WidgetType {
  constructor(
    private readonly label: string,
    private readonly summary: { count: number; title: string },
    private readonly revealPosition: number,
  ) {
    super();
  }

  eq(other: FrontmatterWidget) {
    return other.label === this.label
      && other.summary.count === this.summary.count
      && other.summary.title === this.summary.title
      && other.revealPosition === this.revealPosition;
  }

  toDOM(view: EditorView) {
    const element = document.createElement("button");
    const details = [this.label, this.summary.title, String(this.summary.count)].filter(Boolean);
    element.className = "cm-live-frontmatter-summary";
    element.type = "button";
    element.textContent = details.join(" · ");
    element.addEventListener("mousedown", (event) => event.preventDefault());
    element.addEventListener("click", () => {
      view.dispatch({
        selection: { anchor: this.revealPosition },
        effects: EditorView.scrollIntoView(this.revealPosition, { y: "nearest" }),
      });
      view.focus();
    });
    return element;
  }

  ignoreEvent() {
    return true;
  }
}

class ListMarkWidget extends WidgetType {
  constructor(private readonly source: string) {
    super();
  }

  eq(other: ListMarkWidget) {
    return other.source === this.source;
  }

  toDOM() {
    const element = document.createElement("span");
    const ordered = /^\d/.test(this.source);
    element.className = "cm-live-list-mark";
    element.textContent = ordered ? this.source : "•";
    return element;
  }

  ignoreEvent() {
    return false;
  }
}

class TaskMarkerWidget extends WidgetType {
  constructor(private readonly checked: boolean) {
    super();
  }

  eq(other: TaskMarkerWidget) {
    return other.checked === this.checked;
  }

  toDOM() {
    const element = document.createElement("span");
    element.className = "cm-live-task-marker";
    element.textContent = this.checked ? "☑" : "☐";
    return element;
  }

  ignoreEvent() {
    return false;
  }
}

class ImageLabelWidget extends WidgetType {
  constructor(private readonly alt: string) {
    super();
  }

  eq(other: ImageLabelWidget) {
    return other.alt === this.alt;
  }

  toDOM() {
    const element = document.createElement("span");
    element.className = "cm-live-image-label";
    element.textContent = `▧ ${this.alt || "Image"}`;
    return element;
  }

  ignoreEvent() {
    return false;
  }
}

class HorizontalRuleWidget extends WidgetType {
  eq() {
    return true;
  }

  toDOM() {
    const element = document.createElement("span");
    element.className = "cm-live-horizontal-rule";
    return element;
  }

  ignoreEvent() {
    return false;
  }
}

function imageAlt(source: string) {
  return /^!\[([^\]]*)\]/.exec(source)?.[1] || "";
}

async function runNativeEditorCommand(editor: EditorView, command: DesktopEditorCommand | null) {
  if (!command) return;
  editor.focus();
  const selection = editor.state.selection.main;
  if (command === "undo") {
    undo(editor);
    return;
  }
  if (command === "redo") {
    redo(editor);
    return;
  }
  if (command === "selectAll") {
    editor.dispatch({ selection: { anchor: 0, head: editor.state.doc.length } });
    return;
  }
  if (command === "delete") {
    if (!selection.empty) editor.dispatch({ changes: { from: selection.from, to: selection.to } });
    return;
  }
  if (command === "copy" || command === "cut") {
    if (selection.empty) return;
    await navigator.clipboard.writeText(editor.state.sliceDoc(selection.from, selection.to));
    if (command === "cut") editor.dispatch({ changes: { from: selection.from, to: selection.to } });
    return;
  }
  if (command === "paste" || command === "pasteAndMatchStyle") {
    const text = await navigator.clipboard.readText();
    editor.dispatch(editor.state.replaceSelection(text));
  }
}

function revealEditorPosition(editor: EditorView, reveal?: ProjectEditorReveal) {
  if (!reveal || reveal.line <= 0) return;
  const line = editor.state.doc.line(Math.min(reveal.line, editor.state.doc.lines));
  const position = Math.min(line.to, line.from + Math.max(0, (reveal.column || 1) - 1));
  editor.dispatch({
    selection: { anchor: position },
    effects: EditorView.scrollIntoView(position, { y: "center" }),
  });
}

function selectedProjectRange(editor: EditorView): ProjectEditorSelection | undefined {
  const selection = editor.state.selection.main;
  if (selection.empty) return undefined;
  const start = editor.state.doc.lineAt(selection.from);
  const endOffset = Math.max(selection.from, selection.to - 1);
  const end = editor.state.doc.lineAt(endOffset);
  return {
    startLine: start.number,
    startColumn: selection.from - start.from + 1,
    endLine: end.number,
    endColumn: endOffset - end.from + 1,
  };
}

function updateSelectionAction(
  editor: EditorView,
  container: HTMLDivElement | null,
  setSelectionAction: (action?: SelectionAction) => void,
) {
  const range = selectedProjectRange(editor);
  const selection = editor.state.selection.main;
  const coordinates = editor.coordsAtPos(selection.head);
  if (!range || !coordinates || !container) {
    setSelectionAction(undefined);
    return;
  }
  const bounds = container.getBoundingClientRect();
  const width = 116;
  const relativeLeft = coordinates.left - bounds.left;
  const relativeTop = coordinates.top - bounds.top;
  const x = Math.max(8, Math.min(relativeLeft, container.clientWidth - width - 8));
  const above = relativeTop - 38;
  const y = above >= 8 ? above : coordinates.bottom - bounds.top + 6;
  setSelectionAction({ range, x, y });
}
