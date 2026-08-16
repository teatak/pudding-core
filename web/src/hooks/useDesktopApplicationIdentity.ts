import { useEffect, useState } from "react";

import { getDesktopApplicationIdentity, type DesktopApplicationIdentity } from "@/lib/desktopBridge";

const identityCache = new Map<string, DesktopApplicationIdentity>();
const identityRequests = new Map<string, Promise<DesktopApplicationIdentity | null>>();

export function useDesktopApplicationIdentity(appID: string | undefined) {
  const cleanAppID = appID?.trim() || "";
  const [identity, setIdentity] = useState<DesktopApplicationIdentity | null>(() => identityCache.get(cleanAppID) || null);

  useEffect(() => {
    let cancelled = false;
    const cached = identityCache.get(cleanAppID) || null;
    setIdentity(cached);
    if (!cleanAppID || cached) {
      return () => {
        cancelled = true;
      };
    }

    void loadDesktopApplicationIdentity(cleanAppID).then((next) => {
      if (!cancelled) {
        setIdentity(next);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [cleanAppID]);

  return identity?.appID === cleanAppID ? identity : null;
}

function loadDesktopApplicationIdentity(appID: string) {
  const existing = identityRequests.get(appID);
  if (existing) {
    return existing;
  }
  const request = getDesktopApplicationIdentity(appID)
    .then((identity) => {
      if (identity?.appID === appID) {
        identityCache.set(appID, identity);
        return identity;
      }
      return null;
    })
    .finally(() => {
      identityRequests.delete(appID);
    });
  identityRequests.set(appID, request);
  return request;
}
