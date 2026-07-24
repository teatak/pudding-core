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
