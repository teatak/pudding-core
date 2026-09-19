import type { ProjectBrowserRoot } from "@/api/client";
import { resolveMarkdownLink, type MarkdownLink } from "@/lib/markdownLinks";
import { resolveProjectFileReveal } from "./projectReveal";
import type { ProjectSelection } from "./types";

export type ProjectMarkdownLink =
  | { kind: "file"; selection: ProjectSelection; anchor?: string }
  | Exclude<MarkdownLink, { kind: "file" }>
  | { kind: "invalid"; reason: "outside_project" };

// Relative links belong to the document's directory, never to the renderer URL.
export function resolveProjectMarkdownLink(raw: string, current: ProjectSelection | undefined, roots: ProjectBrowserRoot[]): ProjectMarkdownLink {
  const sourceRoot = roots.find(root => root.id === current?.rootID);
  const source = sourceRoot && current ? `${sourceRoot.path.replace(/\/$/, "")}/${current.path}` : undefined;
  const target = resolveMarkdownLink(raw, source);
  if (target.kind !== "file") return target;
  const selection = resolveProjectFileReveal(roots, { absolutePath: target.absolutePath });
  return selection ? { kind: "file", selection, anchor: target.anchor } : { kind: "invalid", reason: "outside_project" };
}
