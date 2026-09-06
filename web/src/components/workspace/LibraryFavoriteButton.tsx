import { AppTooltip } from "@/components/AppTooltip";
import { Star } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import type { LibraryFavoriteInput } from "@/contracts/api";
import { useI18n } from "@/i18n";
import { useLibrary, useLibraryFavorite } from "./useLibrary";
export function LibraryFavoriteButton({ token, sessionID, target }: {
    token: string;
    sessionID: string;
    target: LibraryFavoriteInput;
}) {
    const { t } = useI18n();
    const library = useLibrary(token, sessionID);
    const mutation = useLibraryFavorite(token, sessionID);
    const entry = library.data?.entries.find((e) => e.kind === target.kind && (target.kind === "canvas" ? e.savedItemID === target.savedItemID : e.url === target.url));
    const label = t(entry?.favoriteID ? "workspace.unfavorite" : "workspace.favorite");
    return <AppTooltip content={label}><Button aria-label={label} aria-pressed={Boolean(entry?.favoriteID)} size="icon-sm" variant="ghost" type="button" disabled={!library.isSuccess || mutation.isPending} onClick={() => mutation.mutate(entry?.favoriteID ? { favoriteID: entry.favoriteID } : { target })}>
  {mutation.isPending ? <Spinner className="size-3.5"/> : <Star className={entry?.favoriteID ? "size-3.5 fill-current text-amber-500" : "size-3.5"}/>}
 </Button></AppTooltip>;
}
