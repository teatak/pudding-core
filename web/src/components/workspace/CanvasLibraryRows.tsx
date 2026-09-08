import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  deleteLibraryFavorite,
  putLibraryFavorite,
  saveCanvasItem,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { CanvasItem } from "@/contracts/api";
import {
  AppDropdownMenuItem,
  AppDropdownMenuSeparator,
} from "@/components/AppMenu";
import { Star, Trash2 } from "@/components/icons";
import { useI18n } from "@/i18n";
import { openCanvasReveal } from "@/state/canvasRevealStore";
import { LibraryResourceIcon, LibraryResourceRow } from "./LibraryResourceRow";

export function CanvasLibraryRows({
  entries,
  token,
  sessionID,
  favoriteIDs,
  onRemoveCanvas,
}: {
  entries: CanvasItem[];
  token: string;
  sessionID: string;
  favoriteIDs: ReadonlyMap<string, string>;
  onRemoveCanvas: (item: CanvasItem) => void;
}) {
  const { t } = useI18n();
  const client = useQueryClient();
  const favorite = useMutation({
    mutationFn: async (entry: CanvasItem) => {
      const favoriteID = favoriteIDs.get(entry.sourceSavedItemID || "");
      if (favoriteID)
        return deleteLibraryFavorite(token, sessionID, favoriteID);
      if (!entry.sourceSavedItemID)
        return saveCanvasItem(token, sessionID, entry.id);
      return putLibraryFavorite(token, sessionID, {
        kind: "canvas",
        savedItemID: entry.sourceSavedItemID,
      });
    },
    onSuccess: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: ["library"] }),
        client.invalidateQueries({
          queryKey: queryKeys.canvasItems(sessionID),
        }),
      ]),
    onError: () => toast.error(t("workspace.favoriteFailed")),
  });
  return entries.map((entry) => (
    <LibraryResourceRow
      key={entry.id}
      layout="card"
      title={entry.title || t("canvas.untitled")}
      description={t(`workspace.kind.${entry.kind}`)}
      starred={favoriteIDs.has(entry.sourceSavedItemID || "")}
      icon={
        <LibraryResourceIcon
          entry={{ kind: "canvas", canvasKind: entry.kind }}
        />
      }
      date={entry.updatedAt}
      dateLabel={t("workspace.resourceUpdatedAt")}
      onOpen={() => openCanvasReveal(sessionID, entry.id)}
      details={[
        {
          label: t("workspace.recentType"),
          value: t(`workspace.kind.${entry.kind}`),
        },
      ]}
      actions={
        <>
          <AppDropdownMenuItem
            disabled={favorite.isPending}
            onSelect={() => favorite.mutate(entry)}
          >
            <Star />
            {t(
              favoriteIDs.has(entry.sourceSavedItemID || "")
                ? "workspace.unfavorite"
                : "workspace.favorite",
            )}
          </AppDropdownMenuItem>
          <AppDropdownMenuSeparator />
          <AppDropdownMenuItem
            variant="destructive"
            onSelect={() => onRemoveCanvas(entry)}
          >
            <Trash2 />
            {t("canvas.delete")}
          </AppDropdownMenuItem>
        </>
      }
    />
  ));
}
