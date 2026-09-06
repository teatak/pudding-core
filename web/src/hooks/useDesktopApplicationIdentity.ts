import { useQuery } from "@tanstack/react-query";

import { queryKeys } from "@/api/queryKeys";
import { getDesktopApplicationIdentity } from "@/lib/desktopBridge";

export function useDesktopApplicationIdentity(appID: string | undefined) {
  const cleanAppID = appID?.trim() || "";

  return useQuery({
    queryKey: queryKeys.desktopApplicationIdentity(cleanAppID),
    queryFn: async () => {
      const identity = await getDesktopApplicationIdentity(cleanAppID);
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
