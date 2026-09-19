export type MarkdownLink =
  | { kind: "file"; absolutePath: string; anchor?: string }
  | { kind: "web" | "external"; url: string }
  | { kind: "browser-file"; url: string }
  | { kind: "invalid"; reason: "invalid" | "unsupported" | "missing_source" };

const controls = /[\u0000-\u001f\u007f]/;

// Source is a native absolute filename, not a renderer URL or a project root.
// Decode the URL path once, after separating query/hash from the filename.
export function resolveMarkdownLink(raw: string, sourcePath?: string): MarkdownLink {
  const value = raw.trim();
  if (!value || controls.test(raw)) return { kind: "invalid", reason: "invalid" };
  try {
    if (/^(https?:|\/\/)/i.test(value)) {
      const url = new URL(value.startsWith("//") ? `https:${value}` : value);
      return { kind: "web", url: url.href };
    }
    if (/^mailto:/i.test(value)) return { kind: "external", url: new URL(value).href };
    const hashIndex = value.indexOf("#");
    const anchor = hashIndex < 0 ? undefined : decodeURIComponent(value.slice(hashIndex + 1));
    if (anchor !== undefined && controls.test(anchor)) return { kind: "invalid", reason: "invalid" };
    let rawPath: string;
    if (/^file:/i.test(value)) {
      // URL() accepts file:relative and file:/path; neither is our file URL contract.
      if (!/^file:\/\/(?:localhost)?\//i.test(value) || value.includes("\\")) return { kind: "invalid", reason: "unsupported" };
      const url = new URL(value);
      if ((url.hostname && url.hostname !== "localhost") || url.pathname.startsWith("//")) return { kind: "invalid", reason: "unsupported" };
      const pathname = decodeURIComponent(url.pathname);
      if (!normalizeAbsolutePath(pathname) || /%2f|%5c/i.test(url.pathname)) return { kind: "invalid", reason: "invalid" };
      return { kind: "browser-file", url: url.href };
    } else {
      if (/^[a-z][a-z\d+.-]*:/i.test(value) && !/^[a-z]:[\\/]/i.test(value)) return { kind: "invalid", reason: "unsupported" };
      rawPath = value.split(/[?#]/, 1)[0];
    }
    const pathname = decodeURIComponent(rawPath);
    const source = sourcePath ? normalizeAbsolutePath(sourcePath) : undefined;
    const absolute = isAbsolutePath(pathname)
      ? pathname
      : !pathname ? source : source && `${source.slice(0, source.lastIndexOf("/") + 1)}${pathname}`;
    if (!absolute) return { kind: "invalid", reason: "missing_source" };
    const absolutePath = normalizeAbsolutePath(absolute);
    return absolutePath ? { kind: "file", absolutePath, anchor } : { kind: "invalid", reason: "invalid" };
  } catch {
    return { kind: "invalid", reason: "invalid" };
  }
}

function isAbsolutePath(path: string) {
  return path.startsWith("/") || /^[a-z]:[\\/]/i.test(path);
}

function normalizeAbsolutePath(path: string) {
  if (controls.test(path) || !isAbsolutePath(path)) return undefined;
  // file:///C:/... and C:\... refer to the same Windows filename.
  let value = path.replace(/^\/([a-z]:\/)/i, "$1");
  const drive = value.match(/^[a-z]:[\\/]/i);
  if (drive) value = value.replace(/\\/g, "/");
  else if (value.includes("\\")) return undefined;
  const prefix = drive ? value.slice(0, 2) : "";
  const parts: string[] = [];
  for (const part of value.slice(prefix.length).split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") parts.pop();
    else parts.push(part);
  }
  return `${prefix}/${parts.join("/")}`;
}

// Keep the raw destination in the click callback; never put an executable URL
// or an empty href (which resolves to the application itself) into the DOM.
export function markdownLinkHref(raw: string) {
  const target = resolveMarkdownLink(raw);
  return target.kind === "web" || target.kind === "external" || target.kind === "browser-file" ? target.url : undefined;
}
