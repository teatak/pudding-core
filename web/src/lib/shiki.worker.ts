import { createHighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
import bash from "@shikijs/langs/bash";
import css from "@shikijs/langs/css";
import diff from "@shikijs/langs/diff";
import go from "@shikijs/langs/go";
import html from "@shikijs/langs/html";
import javascript from "@shikijs/langs/javascript";
import json from "@shikijs/langs/json";
import jsx from "@shikijs/langs/jsx";
import markdown from "@shikijs/langs/markdown";
import python from "@shikijs/langs/python";
import shellscript from "@shikijs/langs/shellscript";
import sql from "@shikijs/langs/sql";
import tsx from "@shikijs/langs/tsx";
import typescript from "@shikijs/langs/typescript";
import yaml from "@shikijs/langs/yaml";
import darkPlus from "@shikijs/themes/dark-plus";
import lightPlus from "@shikijs/themes/light-plus";

const languages = { bash, css, diff, go, html, javascript, json, jsx, markdown, python, shellscript, sql, tsx, typescript, yaml };
type SupportedLanguage = keyof typeof languages;

const LANGUAGE_ALIASES: Record<string, SupportedLanguage> = {
  golang: "go",
  js: "javascript",
  md: "markdown",
  py: "python",
  shell: "shellscript",
  sh: "shellscript",
  ts: "typescript",
  yml: "yaml",
  zsh: "shellscript",
};

const highlighter = createHighlighterCore({
  engine: createJavaScriptRegexEngine(),
  langs: Object.values(languages).flat(),
  themes: [lightPlus, darkPlus],
});

// This module is only loaded as a Worker: regex tokenization never runs on the UI thread.
self.onmessage = async ({ data }: MessageEvent<{ id: number; code: string; lang?: string }>) => {
  let html: string | null = null;
  const rawLang = data.lang?.trim().toLowerCase().replace(/^\./, "") || "";
  const lang = LANGUAGE_ALIASES[rawLang] || rawLang;
  try {
    if (Object.hasOwn(languages, lang)) {
      html = (await highlighter).codeToHtml(data.code, {
        lang,
        themes: { dark: "dark-plus", light: "light-plus" },
      });
    }
  } catch (error) {
    console.error("Code highlighting failed", error);
  }
  self.postMessage({ id: data.id, html });
};
