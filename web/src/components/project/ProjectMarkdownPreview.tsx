import { useMemo, useState } from "react";

import type { ProjectFile } from "@/api/client";
import { MarkdownBody } from "@/components/transcript/TurnParts";
import { useScopedSelectAll } from "@/hooks/useScopedSelectAll";

import { parseProjectMarkdownLink, projectMarkdownResolvers } from "./projectPaths";
import type { ProjectSelection } from "./types";

export function ProjectMarkdownPreview({
  file,
  sessionID,
  token,
  onOpenPreview,
}: {
  file: ProjectFile;
  sessionID: string;
  token: string;
  onOpenPreview: (selection: ProjectSelection) => void;
}) {
  const links = useMemo(() => projectMarkdownResolvers(file, token, sessionID), [file, sessionID, token]);
  const [previewNode, setPreviewNode] = useState<HTMLDivElement | null>(null);
  useScopedSelectAll(previewNode);
  return (
    <div ref={setPreviewNode} className="mx-auto w-full max-w-4xl p-6" data-select-all-scope="project-markdown">
      <MarkdownBody
        allowHtmlImages={false}
        enableMermaid
        resolveImageURL={links.resolveImageURL}
        resolveLinkURL={links.resolveLinkURL}
        text={file.content}
        token={token}
        onResolvedLinkClick={(href) => {
          if (href.startsWith("#")) {
            return true;
          }
          const target = parseProjectMarkdownLink(href);
          if (!target || target.rootID !== file.rootID) {
            return false;
          }
          onOpenPreview(target);
          return true;
        }}
      />
    </div>
  );
}
