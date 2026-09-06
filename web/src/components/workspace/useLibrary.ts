import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { deleteLibraryFavorite, listLibrary, putLibraryFavorite } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import type { LibraryFavoriteInput } from "@/contracts/api";
import { useI18n } from "@/i18n";
export function useLibrary(token: string, sessionID: string, enabled = true) {
    return useQuery({ queryKey: queryKeys.library(sessionID), queryFn: () => listLibrary(token, sessionID), enabled: enabled && Boolean(token && sessionID), staleTime: 10000 });
}
export function useLibraryFavorite(token: string, sessionID: string) {
    const queryClient = useQueryClient();
    const { t } = useI18n();
    return useMutation({
        mutationFn: (action: {
            favoriteID: string;
        } | {
            target: LibraryFavoriteInput;
        }) => "favoriteID" in action
            ? deleteLibraryFavorite(token, sessionID, action.favoriteID) : putLibraryFavorite(token, sessionID, action.target),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["library"] }),
        onError: () => toast.error(t("workspace.favoriteFailed")),
    });
}
