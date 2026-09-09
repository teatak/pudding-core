import { useQuery } from "@tanstack/react-query";

import { queryKeys } from "@/api/queryKeys";
import { getDesktopApplicationIdentity } from "@/lib/desktopBridge";
import { useI18n } from "@/i18n";

export function useDesktopApplicationIdentity(appID: string | undefined) {
  const { locale } = useI18n();
  const cleanAppID = appID?.trim() || "";

  return useQuery({
    queryKey: queryKeys.desktopApplicationIdentity(cleanAppID, locale),
    queryFn: async () => {
      const identity = await getDesktopApplicationIdentity(cleanAppID, locale);
      if (identity?.appID !== cleanAppID) {
        throw new Error("Desktop application identity is unavailable");
      }
      return identity;
    },
    enabled: Boolean(cleanAppID),
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
  });
}
