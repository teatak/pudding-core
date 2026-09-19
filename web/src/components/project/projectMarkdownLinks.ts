import type { ProjectBrowserRoot } from "@/api/client";
import { resolveMarkdownLink, type MarkdownLink } from "@/lib/markdownLinks";
import { resolveProjectFileReveal } from "./projectReveal";
import type { ProjectSelection } from "./types";

export type ProjectMarkdownLink =
  | { kind: "file"; selection: ProjectSelection; anchor?: string }
  | Exclude<MarkdownLink, { kind: "file" }>
  | { kind: "invalid"; reason: "outside_project" };

// A document supplies its file location. Chat supplies only the session's sole
// project directory; scratch roots are not a project base and we never pick the
// first of multiple roots or infer a file for a bare fragment.
export function resolveProjectMarkdownLink(raw: string, current: ProjectSelection | undefined, roots: ProjectBrowserRoot[]): ProjectMarkdownLink {
  const sourceRoot = roots.find(root => root.id === current?.rootID);
  const source = sourceRoot && current ? `${sourceRoot.path.replace(/\/$/, "")}/${current.path}` : undefined;
  const projectRoots = roots.filter(root => !root.temporary);
  const base = source ? { kind: "file" as const, path: source }
    : !current && projectRoots.length === 1 ? { kind: "directory" as const, path: projectRoots[0].path } : undefined;
  const target = resolveMarkdownLink(raw, base);
  if (target.kind !== "file") return target;
  const selection = resolveProjectFileReveal(roots, { absolutePath: target.absolutePath });
  return selection ? { kind: "file", selection, anchor: target.anchor } : { kind: "invalid", reason: "outside_project" };
}
