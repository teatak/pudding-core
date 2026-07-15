import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Package, X } from "lucide-react";
import { useMemo } from "react";
import { toast } from "sonner";

import {
  appIconURL,
  listApps,
  unloadSessionApp,
  type AppDefinition,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppIdentityIcon, appDisplayName } from "@/components/AppIdentity";
import { AppIcon } from "@/components/AppIcon";
import { AppPopoverContent as PopoverContent } from "@/components/AppPopover";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useI18n } from "@/i18n";

export function SessionAppsControl({ session, token }: { session: Session; token: string }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const loadedAppIDs = session.loadedAppIDs ?? [];
  const appsQuery = useQuery({
    queryKey: queryKeys.apps(),
    queryFn: () => listApps(token),
    enabled: Boolean(token && loadedAppIDs.length),
    staleTime: 30_000,
  });
  const appsByID = useMemo(
    () => new Map((appsQuery.data?.apps ?? []).map((app) => [app.id, app])),
    [appsQuery.data?.apps],
  );
  const unloadMutation = useMutation({
    mutationFn: (appID: string) => unloadSessionApp(token, session.id, appID),
    onSuccess: (updated) => {
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (previous) =>
        previous
          ? {
              sessions: previous.sessions.map((item) => (item.id === updated.id ? updated : item)),
            }
          : previous,
      );
      queryClient.setQueryData(queryKeys.session(updated.id), updated);
    },
    onError: () => toast.error(t("apps.sessionUnloadFailed")),
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
  });

  if (!loadedAppIDs.length) {
    return null;
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          aria-label={t("apps.sessionLoadedCount").replace("{count}", String(loadedAppIDs.length))}
          className="h-7 gap-1 px-2 text-muted-foreground"
          size="sm"
          variant="ghost"
        >
          <Package className="size-4" />
          <span className="tabular-nums">{loadedAppIDs.length}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-56 gap-1.5" sideOffset={6}>
        <PopoverHeader className="px-1 py-0.5">
          <PopoverTitle>{t("apps.sessionLoadedTitle")}</PopoverTitle>
        </PopoverHeader>
        <div className="grid gap-0.5">
          {loadedAppIDs.map((appID) => {
            const app = appsByID.get(appID);
            const pending = unloadMutation.isPending && unloadMutation.variables === appID;
            return (
              <div key={appID} className="flex min-w-0 items-center gap-2 rounded-md px-1 py-1 hover:bg-muted/70">
                <SessionAppIcon app={app} token={token} />
                <span className="min-w-0 flex-1 truncate text-sm">{app ? appDisplayName(app, t) : appID}</span>
                <Button
                  aria-label={t("apps.sessionUnload").replace("{name}", app ? appDisplayName(app, t) : appID)}
                  disabled={unloadMutation.isPending}
                  size="icon-xs"
                  title={t("apps.sessionUnload").replace("{name}", app ? appDisplayName(app, t) : appID)}
                  variant="ghost"
                  onClick={() => unloadMutation.mutate(appID)}
                >
                  {pending ? <Spinner className="size-3" /> : <X className="size-3.5" />}
                </Button>
              </div>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function SessionAppIcon({ app, token }: { app: AppDefinition | undefined; token: string }) {
  if (app) {
    return <AppIdentityIcon app={app} iconSrc={appIconURL(token, app)} size="sm" />;
  }
  return <AppIcon className="size-7" size="sm" />;
}
