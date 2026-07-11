import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

import { putSettings } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { SETTINGS_KEYS } from "@/lib/appSettings";

const LEGACY_SHOW_PREVIEW_KEY = "pudding.apps.showPreviewVersions";

export function clearLegacyAppSettingsPreference() {
  localStorage.removeItem(LEGACY_SHOW_PREVIEW_KEY);
}

export function useAppSettingsMigration(token: string) {
  const queryClient = useQueryClient();
  const attemptedTokenRef = useRef("");

  useEffect(() => {
    if (!token || attemptedTokenRef.current === token) {
      return;
    }
    const legacyValue = localStorage.getItem(LEGACY_SHOW_PREVIEW_KEY);
    attemptedTokenRef.current = token;
    if (legacyValue === null) {
      return;
    }

    void putSettings(token, {
      [SETTINGS_KEYS.showAppPreviewVersions]: legacyValue === "1" ? "true" : "false",
    }).then(async () => {
      if (localStorage.getItem(LEGACY_SHOW_PREVIEW_KEY) === legacyValue) {
        clearLegacyAppSettingsPreference();
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings() });
    }).catch(() => {
      // 保留旧值，下次启动时重试迁移。
    });
  }, [queryClient, token]);
}
