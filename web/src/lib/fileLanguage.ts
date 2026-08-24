export function languageFromPath(path: string) {
  const filename = fileNameFromPath(path).toLowerCase();
  const extension = filename.includes(".") ? filename.split(".").pop() || "" : "";
  const aliases: Record<string, string> = {
    bash: "shellscript",
    c: "c",
    cc: "cpp",
    cjs: "javascript",
    cs: "csharp",
    cxx: "cpp",
    h: "cpp",
    hh: "cpp",
    htm: "html",
    hpp: "cpp",
    js: "javascript",
    kt: "kotlin",
    kts: "kotlin",
    mjs: "javascript",
    md: "markdown",
    pyi: "python",
    py: "python",
    rb: "ruby",
    rs: "rust",
    sh: "shellscript",
    svg: "xml",
    ts: "typescript",
    yml: "yaml",
    zsh: "shellscript",
  };
  return aliases[extension] || extension || undefined;
}

export function fileNameFromPath(path: string) {
  return path.split(/[\\/]/).filter(Boolean).pop() || path;
}
