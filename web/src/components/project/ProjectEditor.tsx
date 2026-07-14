import { HighlightStyle, LanguageDescription, syntaxHighlighting } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { Compartment } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { basicSetup } from "codemirror";
import { useEffect, useMemo, useRef } from "react";

import { useTheme } from "@/theme/theme";

import type { ProjectEditorReveal } from "./projectReveal";

export function ProjectEditor({
  path,
  reveal,
  value,
  onChange,
  onSave,
}: {
  path: string;
  reveal?: ProjectEditorReveal;
  value: string;
  onChange: (value: string) => void;
  onSave: () => void;
}) {
  const { resolved } = useTheme();
  const dark = resolved === "dark";
  const hostRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const syncingRef = useRef(false);
  const themeCompartment = useMemo(() => new Compartment(), []);
  onChangeRef.current = onChange;
  onSaveRef.current = onSave;

  useEffect(() => {
    if (!hostRef.current) {
      return;
    }
    let cancelled = false;
    const languageCompartment = new Compartment();
    const view = new EditorView({
      doc: value,
      parent: hostRef.current,
      extensions: [
        basicSetup,
        themeCompartment.of(editorThemeExtensions(dark)),
        EditorView.lineWrapping,
        languageCompartment.of([]),
        keymap.of([{
          key: "Mod-s",
          preventDefault: true,
          run: () => {
            onSaveRef.current();
            return true;
          },
        }]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !syncingRef.current) {
            onChangeRef.current(update.state.doc.toString());
          }
        }),
      ],
    });
    viewRef.current = view;
    const language = languageForPath(path);
    if (language) {
      void language.load().then((support) => {
        if (!cancelled && viewRef.current === view) {
          view.dispatch({ effects: languageCompartment.reconfigure(support) });
        }
      }).catch(() => undefined);
    }
    return () => {
      cancelled = true;
      view.destroy();
      viewRef.current = null;
    };
  }, [path, themeCompartment]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: themeCompartment.reconfigure(editorThemeExtensions(dark)),
    });
  }, [dark, themeCompartment]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || view.state.doc.toString() === value) {
      return;
    }
    syncingRef.current = true;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
    syncingRef.current = false;
  }, [value]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view || !reveal || reveal.line <= 0) {
      return;
    }
    const lineNumber = Math.min(reveal.line, view.state.doc.lines);
    const line = view.state.doc.line(lineNumber);
    const columnOffset = Math.max(0, Math.min((reveal.column || 1) - 1, line.length));
    const position = line.from + columnOffset;
    view.dispatch({
      selection: { anchor: position },
      effects: EditorView.scrollIntoView(position, { y: "center" }),
    });
  }, [reveal?.serial]);

  return <div ref={hostRef} className="h-full min-h-0 overflow-hidden" />;
}

function editorThemeExtensions(dark: boolean) {
  return [dark ? darkEditorTheme : lightEditorTheme, syntaxHighlighting(dark ? darkHighlightStyle : lightHighlightStyle)];
}

const editorThemeSpec = {
  "&": {
    height: "100%",
    backgroundColor: "var(--card)",
    color: "var(--foreground)",
    fontSize: "12px",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-content": { caretColor: "var(--foreground)", padding: "12px 0" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--foreground)" },
  ".cm-gutters": {
    backgroundColor: "color-mix(in srgb, var(--muted) 35%, transparent)",
    borderRight: "1px solid var(--border)",
    color: "var(--muted-foreground)",
  },
  ".cm-activeLine, .cm-activeLineGutter": {
    backgroundColor: "color-mix(in srgb, var(--muted) 45%, transparent)",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "color-mix(in srgb, var(--primary) 22%, transparent)",
  },
  ".cm-scroller": { fontFamily: "var(--font-mono), ui-monospace, monospace", overflow: "auto" },
};

const lightEditorTheme = EditorView.theme(editorThemeSpec, { dark: false });
const darkEditorTheme = EditorView.theme(editorThemeSpec, { dark: true });

const lightHighlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.modifier, tags.operatorKeyword], color: "#0000ff" },
  { tag: [tags.string, tags.special(tags.string), tags.inserted], color: "#a31515" },
  { tag: [tags.number, tags.bool, tags.null], color: "#098658" },
  { tag: [tags.comment, tags.meta], color: "#008000", fontStyle: "italic" },
  { tag: [tags.function(tags.variableName), tags.labelName], color: "#795e26" },
  { tag: [tags.typeName, tags.className, tags.namespace], color: "#267f99" },
  { tag: [tags.propertyName, tags.attributeName, tags.definition(tags.variableName)], color: "#001080" },
  { tag: [tags.regexp, tags.escape], color: "#811f3f" },
  { tag: tags.invalid, color: "#cd3131", textDecoration: "underline" },
]);

const darkHighlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.modifier, tags.operatorKeyword], color: "#c586c0" },
  { tag: [tags.string, tags.special(tags.string), tags.inserted], color: "#ce9178" },
  { tag: [tags.number, tags.bool, tags.null], color: "#b5cea8" },
  { tag: [tags.comment, tags.meta], color: "#6a9955", fontStyle: "italic" },
  { tag: [tags.function(tags.variableName), tags.labelName], color: "#dcdcaa" },
  { tag: [tags.typeName, tags.className, tags.namespace], color: "#4ec9b0" },
  { tag: [tags.propertyName, tags.attributeName, tags.definition(tags.variableName)], color: "#9cdcfe" },
  { tag: [tags.regexp, tags.escape], color: "#d16969" },
  { tag: tags.invalid, color: "#f44747", textDecoration: "underline" },
]);

function languageForPath(path: string) {
  const filename = path.split(/[\\/]/).filter(Boolean).pop() || path;
  const alias = languageAliasForFilename(filename);
  return alias
    ? LanguageDescription.matchLanguageName(languages, alias)
    : LanguageDescription.matchFilename(languages, filename);
}

function languageAliasForFilename(filename: string) {
  const lower = filename.toLowerCase();
  if (lower === ".env" || lower.startsWith(".env.")) return "properties";
  if (lower === "dockerfile" || lower.startsWith("dockerfile.")) return "dockerfile";
  if (lower.endsWith(".jsonc")) return "json";
  if (lower.endsWith(".pyi")) return "python";
  if (lower.endsWith(".zsh")) return "shell";
  return "";
}
