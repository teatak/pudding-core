import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

import { getProjectEntry, listProjectBrowserRoots, type ProjectBrowserRoot } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useOpenBrowserTab } from "@/browser/useOpenBrowserTab";
import { useI18n } from "@/i18n";
import { openExternalURL } from "@/lib/desktopBridge";
import { resolveMarkdownLink } from "@/lib/markdownLinks";
import { requestProjectFileReveal } from "@/state/projectRevealStore";
import { projectBrowserError } from "./projectErrors";
import { resolveProjectMarkdownLink } from "./projectMarkdownLinks";
import type { ProjectSelection } from "./types";

export function useMarkdownLink({ current, roots, sessionID, token }: {
  current?: ProjectSelection;
  roots?: ProjectBrowserRoot[];
  sessionID?: string;
  token: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const browser = useOpenBrowserTab(token);
  return useCallback((href: string) => {
    const open = async () => {
      const initial = resolveMarkdownLink(href);
      if (initial.kind === "external") { await openExternalURL(initial.url); return; }
      if (initial.kind === "web" || initial.kind === "browser-file") {
        if (!sessionID) {
          if (initial.kind === "web") await openExternalURL(initial.url);
          else toast.warning(t("project.browserLinkMissingSource"));
          return;
        }
        if (!browser.isPending) browser.mutate({ targetSessionID: sessionID, url: initial.url });
        return;
      }
      // A chat has no source directory. Do not fetch roots just to guess one.
      if (!current && initial.kind === "invalid") {
        toast.warning(t(initial.reason === "missing_source" ? "project.browserLinkMissingSource" : "project.browserLinkUnavailable"));
        return;
      }
      if (!sessionID) { toast.warning(t("project.browserLinkMissingSource")); return; }
      const effectiveRoots = roots || (await queryClient.fetchQuery({
        queryKey: queryKeys.projectBrowserRoots(sessionID),
        queryFn: () => listProjectBrowserRoots(token, sessionID),
      })).roots;
      const target = resolveProjectMarkdownLink(href, current, effectiveRoots);
      if (target.kind !== "file") {
        toast.warning(t(target.kind === "invalid" && target.reason === "outside_project" ? "project.browserLinkOutsideProject" : "project.browserLinkUnavailable"));
        return;
      }
      const root = effectiveRoots.find(item => item.id === target.selection.rootID)!;
      const entry = await getProjectEntry(token, sessionID, root.id, target.selection.path);
      const sourceLine = target.anchor?.match(/^L([1-9]\d*)(?:-L[1-9]\d*)?$/);
      requestProjectFileReveal({
        sessionID, rootPath: root.path, relativePath: entry.path, kind: entry.type,
        line: sourceLine ? Number(sourceLine[1]) : undefined,
        anchor: sourceLine ? undefined : target.anchor,
      });
    };
    void open().catch(error => toast.warning(projectBrowserError(error, t)));
    return true;
  }, [browser.isPending, browser.mutate, current, queryClient, roots, sessionID, t, token]);
}
