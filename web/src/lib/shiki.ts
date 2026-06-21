export type CodeBlockRenderer = (code: string, lang?: string) => string | null;

const SHIKI_THEMES = {
  dark: "dark-plus",
  light: "light-plus",
} as const;

const LANGUAGE_LOADERS = {
  bash: () => import("@shikijs/langs/bash"),
  css: () => import("@shikijs/langs/css"),
  diff: () => import("@shikijs/langs/diff"),
  go: () => import("@shikijs/langs/go"),
  html: () => import("@shikijs/langs/html"),
  javascript: () => import("@shikijs/langs/javascript"),
  json: () => import("@shikijs/langs/json"),
  jsx: () => import("@shikijs/langs/jsx"),
  markdown: () => import("@shikijs/langs/markdown"),
  python: () => import("@shikijs/langs/python"),
  shellscript: () => import("@shikijs/langs/shellscript"),
  sql: () => import("@shikijs/langs/sql"),
  tsx: () => import("@shikijs/langs/tsx"),
  typescript: () => import("@shikijs/langs/typescript"),
  yaml: () => import("@shikijs/langs/yaml"),
} as const;

type SupportedLanguage = keyof typeof LANGUAGE_LOADERS;

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

const COMMON_LANGUAGE_SET = new Set<string>(Object.keys(LANGUAGE_LOADERS));
let rendererPromise: Promise<CodeBlockRenderer> | null = null;

export function getShikiCodeRenderer() {
  rendererPromise ??= createShikiCodeRenderer();
  return rendererPromise;
}

async function createShikiCodeRenderer(): Promise<CodeBlockRenderer> {
  const [{ createHighlighterCore }, { createJavaScriptRegexEngine }, languages, themes] = await Promise.all([
    import("shiki/core"),
    import("shiki/engine/javascript"),
    loadLanguages(),
    loadThemes(),
  ]);
  const highlighter = await createHighlighterCore({
    engine: createJavaScriptRegexEngine(),
    langs: languages,
    themes,
  });

  return (code, rawLang) => {
    const lang = normalizeLanguage(rawLang);
    if (!lang) {
      return null;
    }
    try {
      return highlighter.codeToHtml(code, {
        lang,
        themes: SHIKI_THEMES,
      });
    } catch {
      return null;
    }
  };
}

async function loadLanguages() {
  const languageGroups = await Promise.all(Object.values(LANGUAGE_LOADERS).map((loadLanguage) => loadLanguage().then((module) => module.default)));
  return languageGroups.flat();
}

async function loadThemes() {
  const [lightPlus, darkPlus] = await Promise.all([
    import("@shikijs/themes/light-plus"),
    import("@shikijs/themes/dark-plus"),
  ]);
  return [lightPlus.default, darkPlus.default];
}

function normalizeLanguage(rawLang?: string): SupportedLanguage | null {
  const normalized = rawLang?.trim().toLowerCase().replace(/^\./, "");
  if (!normalized) {
    return null;
  }
  const lang = LANGUAGE_ALIASES[normalized] || (normalized as SupportedLanguage);
  return COMMON_LANGUAGE_SET.has(lang) ? lang : null;
}
