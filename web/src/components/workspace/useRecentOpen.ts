import { useEffect } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { recordLibraryRecent } from "@/api/client";
import type { LibraryRecentInput } from "@/contracts/api";
import { useI18n } from "@/i18n";
// Only the visible resource reports an open; query refetches and edits do not.
export function useRecentOpen(token: string, sessionID: string, target: LibraryRecentInput | undefined) {
    const queryClient = useQueryClient();
    const { t } = useI18n();
    const { mutate } = useMutation({
        mutationFn: (input: {
            sessionID: string;
            target: LibraryRecentInput;
        }) => recordLibraryRecent(token, input.sessionID, input.target),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["library-recent"] }),
        onError: () => toast.error(t("workspace.recentRecordFailed")),
    });
    const kind = target?.kind;
    const rootID = target?.kind === "file" ? target.rootID : undefined;
    const path = target?.kind === "file" ? target.path : undefined;
    const itemID = target?.kind === "canvas" ? target.itemID : undefined;
    useEffect(() => {
        if (!token || !sessionID)
            return;
        if (kind === "file" && rootID && path)
            mutate({ sessionID, target: { kind, rootID, path } });
        if (kind === "canvas" && itemID)
            mutate({ sessionID, target: { kind, itemID } });
    }, [token, sessionID, kind, rootID, path, itemID, mutate]);
}
