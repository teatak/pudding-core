import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { X } from "@/components/icons";
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
import { Spinner } from "@/components/Spinner";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";

const maxVisibleApps = 5;

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

  const visibleAppIDs = loadedAppIDs.slice(0, maxVisibleApps);
  const hiddenAppCount = Math.max(0, loadedAppIDs.length - visibleAppIDs.length);

  return (
    <div
      aria-label={t("apps.sessionLoadedCount").replace("{count}", String(loadedAppIDs.length))}
      className="group/apps flex h-8 max-w-48 items-center overflow-visible px-1"
      role="group"
    >
      {visibleAppIDs.map((appID, index) => {
        const app = appsByID.get(appID);
        const name = app ? appDisplayName(app, t) : appID;
        const pending = unloadMutation.isPending && unloadMutation.variables === appID;
        const closeLabel = t("apps.sessionUnload").replace("{name}", name);
        return (
          <div
            key={appID}
            className={cn(
              "group/app relative -ml-2 inline-grid size-7 shrink-0 place-items-center rounded-full",
              "transition-[margin] duration-150 first:ml-0 group-hover/apps:ml-0.5 group-hover/apps:first:ml-0 group-focus-within/apps:ml-0.5 group-focus-within/apps:first:ml-0",
              "hover:z-20 focus-within:z-20",
              pending && "z-20",
            )}
            style={{ zIndex: pending ? 20 : index + 1 }}
          >
            <SessionAppIcon app={app} token={token} />
            <button
              aria-label={closeLabel}
              className={cn(
                "pointer-events-none absolute -top-0.5 -right-0.5 z-10 grid size-3 place-items-center rounded-full bg-foreground text-background opacity-0 shadow-sm ring-1 ring-[var(--agent-console-chrome-background)]",
                "group-hover/app:pointer-events-auto group-hover/app:opacity-100 focus-visible:pointer-events-auto focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none",
                pending && "pointer-events-auto opacity-100",
              )}
              disabled={pending}
              type="button"
              onClick={() => unloadMutation.mutate(appID)}
            >
              {pending ? <Spinner className="size-2.5" /> : <X className="size-2" data-icon-weight="strong" />}
            </button>
          </div>
        );
      })}
      {hiddenAppCount > 0 ? (
        <span className="-ml-2 inline-grid size-7 shrink-0 place-items-center rounded-full bg-muted text-[11px] font-medium tabular-nums text-muted-foreground transition-[margin] duration-150 group-hover/apps:ml-0.5 group-focus-within/apps:ml-0.5">
          +{hiddenAppCount}
        </span>
      ) : null}
    </div>
  );
}

function SessionAppIcon({ app, token }: { app: AppDefinition | undefined; token: string }) {
  return (
    <span className="relative inline-grid size-7 place-items-center overflow-hidden rounded-full bg-[var(--agent-console-chrome-background)] ring-1 ring-[var(--agent-console-chrome-background)] [&_[data-slot=identity-icon]]:!size-7">
      {app ? (
        <AppIdentityIcon app={app} iconSrc={appIconURL(token, app)} size="md" />
      ) : (
        <AppIcon size="md" />
      )}
    </span>
  );
}
