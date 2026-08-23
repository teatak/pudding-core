export type ProjectDocumentPreviewKind = "structured" | "table";

export function projectDocumentPreviewKind(path: string): ProjectDocumentPreviewKind | undefined {
  if (/\.(?:csv|tsv)$/i.test(path)) {
    return "table";
  }
  if (/\.(?:json|ya?ml)$/i.test(path)) {
    return "structured";
  }
  return undefined;
}

export function isProjectPDFPath(path: string) {
  return /\.pdf$/i.test(path);
}

export function isProjectImagePath(path: string) {
  return /\.(?:avif|gif|jpe?g|png|svg|webp)$/i.test(path);
}

export function projectFileSupportsPreview(path: string) {
  return /\.(?:md|markdown)$/i.test(path)
    || Boolean(projectDocumentPreviewKind(path))
    || isProjectImagePath(path)
    || isProjectPDFPath(path);
}

export function projectFileSupportsSource(path: string) {
  return !isProjectImagePath(path) && !isProjectPDFPath(path);
}
