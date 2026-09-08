import type { LibraryEntry } from "@/contracts/api";
import {
  AppDropdownMenuItem,
  AppDropdownMenuSeparator,
} from "@/components/AppMenu";
import { Star, Trash2 } from "@/components/icons";
import { useI18n } from "@/i18n";
import { LibraryResourceIcon, LibraryResourceRow } from "./LibraryResourceRow";
import { libraryDescription } from "./libraryPresentation";
import { useLibraryFavorite } from "./useLibrary";

export function LibraryRows({
  entries,
  token,
  sessionID,
  onOpen,
  onDeleteSaved,
  layout = "row",
}: {
  entries: LibraryEntry[];
  layout?: "row" | "shortcut" | "card";
  token: string;
  sessionID: string;
  onOpen: (entry: LibraryEntry) => void;
  onDeleteSaved: (item: { id: string; title?: string }) => void;
}) {
  const { t } = useI18n();
  const favorite = useLibraryFavorite(token, sessionID);
  return entries.map((entry) => {
    const display = libraryDescription(entry, t);
    return (
      <LibraryResourceRow
        layout={layout}
        key={entry.id}
        {...display}
        icon={
          <LibraryResourceIcon entry={entry} compact={layout === "shortcut"} />
        }
        date={entry.updatedAt}
        dateLabel={t("workspace.resourceUpdatedAt")}
        available={entry.available}
        onOpen={() => onOpen(entry)}
        details={[
          { label: t("workspace.recentType"), value: display.kind },
          { label: t("workspace.resourceSource"), value: display.source },
          { label: t("workspace.resourceLocation"), value: display.location },
          { label: t("workspace.resourceVersion"), value: display.version },
        ]}
        actions={
          <>
            <AppDropdownMenuItem
              disabled={favorite.isPending}
              onSelect={() => {
                if (entry.favoriteID)
                  favorite.mutate({ favoriteID: entry.favoriteID });
                else if (entry.savedItemID)
                  favorite.mutate({
                    target: { kind: "canvas", savedItemID: entry.savedItemID },
                  });
              }}
            >
              <Star className={entry.favoriteID ? "fill-current" : ""} />
              {t(
                entry.favoriteID
                  ? "workspace.unfavorite"
                  : "workspace.favorite",
              )}
            </AppDropdownMenuItem>
            {entry.savedItemID ? (
              <>
                <AppDropdownMenuSeparator />
                <AppDropdownMenuItem
                  variant="destructive"
                  onSelect={() =>
                    onDeleteSaved({
                      id: entry.savedItemID!,
                      title: entry.title,
                    })
                  }
                >
                  <Trash2 />
                  {t("canvas.deleteSavedWidget")}
                </AppDropdownMenuItem>
              </>
            ) : null}
          </>
        }
      />
    );
  });
}
