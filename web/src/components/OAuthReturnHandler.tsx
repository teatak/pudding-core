import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { toast } from "sonner";

import { completeAppOAuth } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { useI18n } from "@/i18n";
import { onOAuthConnected } from "@/lib/desktopBridge";

export function OAuthReturnHandler({ token }: { token: string }) {
  const queryClient = useQueryClient();
  const { t } = useI18n();

  useEffect(() => onOAuthConnected((payload) => {
    const finish = async () => {
      if (payload.provider === "github" && payload.state) {
        await completeAppOAuth(token, {
          provider: payload.provider,
          ticket: payload.ticket,
          state: payload.state,
          error: payload.error,
        });
      }
      toast.success(t("apps.oauthConnected"));
      await queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() });
      await queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] === "apps" && query.queryKey[2] === "mcp",
      });
    };
    void finish().catch(() => toast.error(t("apps.oauthCompleteFailed")));
  }), [queryClient, t, token]);

  return null;
}
