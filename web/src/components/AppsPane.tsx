import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  BadgeCheck,
  Braces,
  Camera,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  CircleCheck,
  CircleDashed,
  Copy,
  Download,
  Eye,
  EyeOff,
  FileCog,
  FileDiff,
  FilePenLine,
  FileJson2,
  FileSearch,
  FileText,
  FileX,
  FolderInput,
  GitBranch,
  GitCommitHorizontal,
  GitCompareArrows,
  Globe,
  History,
  Keyboard,
  KeyRound,
  ListMinus,
  ListPlus,
  ListChecks,
  ListTree,
  MoveVertical,
  MousePointerClick,
  Package,
  PackageCheck,
  PanelsTopLeft,
  Pencil,
  Plus,
  RotateCw,
  Route,
  ScanLine,
  SearchCode,
  Settings2,
  Share2,
  SquareTerminal,
  Trash,
  Wrench,
  X,
  type LucideIcon,
} from "@/components/icons";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { toast } from "sonner";

import {
  appIconURL,
  deleteApp,
  deleteAppConnection,
  deleteAppMCPOverride,
  getAppConnection,
  getAppMCPOverride,
  getAppMCPStatus,
  getMCPAppConfig,
  getAppSkill,
  getSettings,
  installAppPackage,
  importMCPApps,
  listAppConnections,
  listApps,
  putAppConnection,
  putAppMCPOverride,
  putMCPAppConfig,
  setAppEnabled,
  startAppOAuth,
  type AppConnection,
  type AppConnectionPayload,
  type AppDefinition,
  type AppMCPEndpointStatus,
  type AppMCPOverride,
  type AppMCPOverrideResponse,
  type AppMCPStatusResponse,
  type AppSkillDetail,
  type Session,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppIcon, type AppIconSpec } from "@/components/AppIcon";
import { AppIdentityIcon, BuiltinAppIcon, appDisplayDescription, appDisplayName } from "@/components/AppIdentity";
import { Spinner } from "@/components/Spinner";
import { DialogSelectContent } from "@/components/DialogSelectContent";
import { PageHeader } from "@/components/PageHeader";
import { ShellActionButton } from "@/components/ShellActionButton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ConfirmationDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Item, ItemContent, ItemDescription, ItemGroup, ItemMedia, ItemTitle } from "@/components/ui/item";
import { Label } from "@/components/ui/label";
import { Select, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { translate, useI18n } from "@/i18n";
import { boolSetting, SETTINGS_KEYS } from "@/lib/appSettings";
import {
  compareAppVersions,
  isPreviewRelease,
  needsAppUpgrade,
  selectAppInstallRelease,
} from "@/lib/appVersions";
import { openExternalURL } from "@/lib/desktopBridge";
import { shouldKeepDialogOpenForSelectDismiss } from "@/lib/layerGuards";
import { openSettingsDialog } from "@/lib/settingsDialog";
import { cn } from "@/lib/utils";

type AuthType = AppConnectionPayload["authType"];
type LocalizedText = string | Record<string, string>;
type I18nTranslate = ReturnType<typeof useI18n>["t"];
type AppRegistryRelease = {
  version: string;
  manifest?: string;
  package: string;
  package_sha256?: string;
  requires?: Record<string, string>;
  targets?: string[];
  released_at?: string;
  channel?: string;
  preview?: boolean;
};
type AppRegistryItem = {
  id: string;
  name?: string;
  title?: LocalizedText;
  version?: string;
  description?: LocalizedText;
  icon?: AppIconSpec;
  icon_sha256?: string;
  manifest?: string;
  package?: string;
  package_sha256?: string;
  releases?: AppRegistryRelease[];
  targets?: string[];
  tags?: string[];
};
type AppRegistry = {
  items: AppRegistryItem[];
};
type AppAuthMethod = NonNullable<NonNullable<AppDefinition["auth"]>["methods"]>[number];
type AppConnectionField = NonNullable<NonNullable<AppDefinition["connection"]>["fields"]>[number];
type AppEndpoints = NonNullable<AppDefinition["endpoints"]>;
type AppSkills = NonNullable<AppDefinition["skills"]>;
type AppSkillItem = AppSkills[number] & { content?: string };
type AppSkillItems = AppSkillItem[];
type AppToolItem = {
  key: string;
  name: string;
  title?: string;
  description?: string;
  inputSchema?: unknown;
  kind: "builtin" | "api" | "mcp";
};
type CatalogAppContent = {
  endpoints: AppEndpoints;
  skills: AppSkillItems;
};
type SelectedSkill = {
  appID?: string;
  appSource?: AppDefinition["source"];
  appName: string;
  icon?: AppIconSpec;
  iconSrc?: string;
  skill: AppSkillItem;
};
type AppPackageFile = {
  path?: string;
  content?: string;
  content_base64?: string;
};
type AppPackage = {
  kind?: string;
  files?: AppPackageFile[];
};
type CatalogInstallTarget = {
  app: AppRegistryItem;
  release: AppRegistryRelease;
};

type ConnectionForm = {
  id: string;
  name: string;
  authMethodID: string;
  authType: AuthType;
  fields: Record<string, string>;
  endpointURLs: Record<string, string>;
  token: string;
  prefix: string;
  header: string;
  username: string;
  password: string;
};
type MCPConfigForm = {
  transport: "stdio" | "streamable_http";
  command: string;
  args: string;
  url: string;
  env: string;
  headers: string;
};
type MCPAppConfigEditor = { app?: AppDefinition };

const authTypes: AuthType[] = ["none", "bearer", "token", "basic", "header", "oauth2", "token_exchange"];
const OFFICIAL_APP_REGISTRY =
  import.meta.env.VITE_PUDDING_APP_REGISTRY_URL ||
  "https://teatak.github.io/pudding-hub/apps/registry.json";
const GITHUB_APP_ACCESS_SETTINGS_URL = "https://x-t.top/oauth/providers/github/install";
const APP_CATALOG_CACHE_TTL_MS = 5 * 60 * 1000;
const APP_CATALOG_TARGET = "desktop";

export function AppsPane({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<{ app: AppDefinition; connection?: AppConnection } | null>(null);
  const [deleting, setDeleting] = useState<AppConnection | null>(null);
  const [uninstalling, setUninstalling] = useState<AppDefinition | null>(null);
  const [mcpConfigEditor, setMCPConfigEditor] = useState<MCPAppConfigEditor | null>(null);
  const [detailAppID, setDetailAppID] = useState<string | null>(null);
  const [detailCatalogID, setDetailCatalogID] = useState<string | null>(null);
  const [catalogReleaseByID, setCatalogReleaseByID] = useState<Record<string, string>>({});
  const [bulkUpgradeAppID, setBulkUpgradeAppID] = useState<string | null>(null);
  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => getSettings(token),
    enabled: Boolean(token),
    staleTime: 30_000,
  });
  const showPreviewVersions = boolSetting(
    settingsQuery.data?.settings,
    SETTINGS_KEYS.showAppPreviewVersions,
    false,
  );
  const [selectedSkill, setSelectedSkill] = useState<SelectedSkill | null>(null);
  const appsQuery = useQuery({
    queryKey: queryKeys.apps(),
    queryFn: () => listApps(token),
  });
  const catalogQuery = useQuery({
    queryKey: queryKeys.appCatalog(),
    queryFn: () => fetchAppRegistry(OFFICIAL_APP_REGISTRY),
    retry: false,
    staleTime: APP_CATALOG_CACHE_TTL_MS,
  });
  const connectionsQuery = useQuery({
    queryKey: queryKeys.appConnections(),
    queryFn: () => listAppConnections(token),
  });
  const apps = useMemo(
    () =>
      [...(appsQuery.data?.apps || [])].sort((a, b) => {
        if (a.source !== b.source) {
          return a.source === "builtin" ? -1 : 1;
        }
        return a.name.localeCompare(b.name, "en");
      }),
    [appsQuery.data?.apps],
  );
  const builtinApps = useMemo(() => apps.filter((app) => app.source === "builtin"), [apps]);
  const allInstalledApps = useMemo(() => apps.filter((app) => app.source === "installed"), [apps]);
  const installedByID = useMemo(() => new Map(allInstalledApps.map((app) => [app.id, app])), [allInstalledApps]);
  const catalogApps = useMemo(
    () =>
      [...(catalogQuery.data?.items || [])].sort((a, b) =>
        appRegistryTitle(a, "en").localeCompare(appRegistryTitle(b, "en"), "en"),
      ),
    [catalogQuery.data?.items],
  );
  const catalogByLocalID = useMemo(
    () => new Map(catalogApps.map((app) => [appRegistryLocalID(app), app])),
    [catalogApps],
  );
  const upgradeTargets = useMemo<CatalogInstallTarget[]>(
    () =>
      catalogApps.flatMap((app) => {
        const installed = installedByID.get(appRegistryLocalID(app));
        if (!installed) {
          return [];
        }
        const release =
          appRegistryDefaultRelease(app, showPreviewVersions, installed) ||
          appRegistryDefaultRelease(app, false, installed);
        return release && needsAppUpgrade(installed, release) ? [{ app, release }] : [];
      }),
    [catalogApps, installedByID, showPreviewVersions],
  );
  const connections = useMemo(() => connectionsQuery.data?.connections || [], [connectionsQuery.data?.connections]);
  const detailApp = apps.find((app) => app.id === detailAppID) || null;
  const detailCatalogForInstalled = detailApp ? catalogByLocalID.get(detailApp.id) : undefined;
  const detailCatalogApp = catalogApps.find((app) => app.id === detailCatalogID) || null;
  const detailCatalogInstalled = detailCatalogApp
    ? installedByID.get(appRegistryLocalID(detailCatalogApp))
    : undefined;
  const detailCatalogReleases = detailCatalogApp ? appRegistryReleases(detailCatalogApp, showPreviewVersions) : [];
  const detailCatalogRelease =
    detailCatalogApp && detailCatalogReleases.length > 0
      ? detailCatalogReleases.find((release) => release.version === catalogReleaseByID[detailCatalogApp.id]) ||
        appRegistryDefaultRelease(detailCatalogApp, showPreviewVersions, detailCatalogInstalled)
      : undefined;
  const catalogDetailQuery = useQuery({
    queryKey: queryKeys.appCatalogDetail(
      detailCatalogApp?.id || "",
      detailCatalogRelease?.package_sha256 || detailCatalogRelease?.version || detailCatalogRelease?.package || "",
    ),
    queryFn: () => fetchCatalogAppContent(detailCatalogApp!, OFFICIAL_APP_REGISTRY, detailCatalogRelease!),
    enabled: Boolean(detailCatalogApp && detailCatalogRelease),
    retry: false,
    staleTime: APP_CATALOG_CACHE_TTL_MS,
  });
  const selectedSkillQuery = useQuery({
    queryKey: queryKeys.appSkill(selectedSkill?.appID || "", selectedSkill?.skill.path || ""),
    queryFn: () => getAppSkill(token, selectedSkill!.appID!, selectedSkill!.skill.path),
    enabled: Boolean(selectedSkill?.appID && !selectedSkill.skill.content),
  });
  const selectedSkillDetail = useMemo<AppSkillDetail | AppSkillItem | null>(() => {
    if (!selectedSkill) {
      return null;
    }
    return selectedSkillQuery.data || selectedSkill.skill;
  }, [selectedSkill, selectedSkillQuery.data]);
  const detailConnections = detailApp ? connections.filter((conn) => conn.appID === detailApp.id) : [];
  const loadFailed = appsQuery.isError || connectionsQuery.isError;

  useEffect(() => {
    if (detailAppID && !apps.some((app) => app.id === detailAppID)) {
      setDetailAppID(null);
    }
  }, [apps, detailAppID]);

  useEffect(() => {
    if (detailCatalogID && !catalogApps.some((app) => app.id === detailCatalogID)) {
      setDetailCatalogID(null);
    }
  }, [catalogApps, detailCatalogID]);

  const installMutation = useMutation({
    mutationFn: async (target: CatalogInstallTarget) => {
      const packageJSON = await fetchAppPackage(target.app, OFFICIAL_APP_REGISTRY, target.release);
      return installAppPackage(token, {
        packageJSON,
        packageSHA256: target.release.package_sha256,
        sourceURL: OFFICIAL_APP_REGISTRY,
      });
    },
    onSuccess: async (app) => {
      toast.success(t("apps.installDone"));
      const selectedCatalogApp = detailCatalogApp;
      queryClient.setQueryData<{ apps: AppDefinition[] }>(queryKeys.apps(), (current) => {
        const existing = current?.apps || [];
        return {
          apps: [...existing.filter((item) => item.id !== app.id), app].sort((a, b) => a.name.localeCompare(b.name)),
        };
      });
      if (selectedCatalogApp && appRegistryLocalID(selectedCatalogApp) === app.id) {
        setDetailCatalogID(null);
        setDetailAppID(app.id);
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appCatalog() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() }),
      ]);
    },
    onError: () => toast.error(t("apps.installFailed")),
  });
  const upgradeAllMutation = useMutation({
    mutationFn: async (targets: CatalogInstallTarget[]) => {
      const upgraded: AppDefinition[] = [];
      let failed = 0;
      for (const target of targets) {
        setBulkUpgradeAppID(target.app.id);
        try {
          const packageJSON = await fetchAppPackage(target.app, OFFICIAL_APP_REGISTRY, target.release);
          upgraded.push(
            await installAppPackage(token, {
              packageJSON,
              packageSHA256: target.release.package_sha256,
              sourceURL: OFFICIAL_APP_REGISTRY,
            }),
          );
        } catch {
          failed += 1;
        }
      }
      return { failed, upgraded };
    },
    onSuccess: async ({ failed, upgraded }) => {
      if (upgraded.length > 0) {
        queryClient.setQueryData<{ apps: AppDefinition[] }>(queryKeys.apps(), (current) => {
          const upgradedByID = new Map(upgraded.map((app) => [app.id, app]));
          const existing = current?.apps || [];
          return {
            apps: [
              ...existing.map((app) => upgradedByID.get(app.id) || app),
              ...upgraded.filter((app) => !existing.some((item) => item.id === app.id)),
            ].sort((a, b) => a.name.localeCompare(b.name)),
          };
        });
      }
      if (failed > 0) {
        toast.error(t("apps.upgradeAllPartial"));
      } else {
        toast.success(t("apps.upgradeAllDone"));
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appCatalog() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() }),
      ]);
    },
    onSettled: () => setBulkUpgradeAppID(null),
  });
  const deleteMutation = useMutation({
    mutationFn: (connection: AppConnection) => deleteAppConnection(token, connection.id),
    onSuccess: async (_data, connection) => {
      toast.success(t("apps.connectionDeleted"));
      setDeleting(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appMCPStatus(connection.appID) }),
      ]);
    },
    onError: () => toast.error(t("apps.connectionDeleteFailed")),
  });
  const uninstallMutation = useMutation({
    mutationFn: (id: string) => deleteApp(token, id),
    onSuccess: async (_data, id) => {
      toast.success(t("apps.uninstallDone"));
      setUninstalling(null);
      if (id && detailAppID === id) {
        setDetailAppID(null);
      }
      queryClient.setQueryData<{ apps: AppDefinition[] }>(queryKeys.apps(), (current) => ({
        apps: (current?.apps || []).filter((app) => app.id !== id),
      }));
      queryClient.setQueryData<{ sessions: Session[] }>(queryKeys.sessions(), (current) =>
        current
          ? {
              sessions: current.sessions.map((session) => ({
                ...session,
                loadedAppIDs: session.loadedAppIDs?.filter((appID) => appID !== id),
              })),
            }
          : current,
      );
      queryClient.setQueriesData<Session>(
        {
          predicate: (query) => query.queryKey[0] === "session" && query.queryKey.length === 2,
        },
        (current) =>
          current
            ? { ...current, loadedAppIDs: current.loadedAppIDs?.filter((appID) => appID !== id) }
            : current,
      );
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appCatalog() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions() }),
      ]);
    },
    onError: () => toast.error(t("apps.uninstallFailed")),
  });
  const enableMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => setAppEnabled(token, id, enabled),
    onMutate: async ({ id, enabled }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.apps() });
      const previous = queryClient.getQueryData<{ apps: AppDefinition[] }>(queryKeys.apps());
      queryClient.setQueryData<{ apps: AppDefinition[] }>(queryKeys.apps(), (current) => ({
        apps: (current?.apps || []).map((app) => (app.id === id ? { ...app, enabled } : app)),
      }));
      return { previous };
    },
    onError: (_error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.apps(), context.previous);
      }
      toast.error(t("apps.enableFailed"));
    },
    onSuccess: (updated) => {
      queryClient.setQueryData<{ apps: AppDefinition[] }>(queryKeys.apps(), (current) => ({
        apps: (current?.apps || []).map((app) => (app.id === updated.id ? updated : app)),
      }));
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
  });
  return (
    <main className="flex h-full w-full min-w-0 flex-1 flex-col overflow-hidden bg-background">
      <PageHeader
        icon={
          detailApp || detailCatalogApp ? (
            <ShellActionButton
              aria-label={t("common.back")}
              className="pudding-toolbar-icon-button"
              size="icon-xs"
              onClick={() => {
                setDetailAppID(null);
                setDetailCatalogID(null);
              }}
            >
              <ArrowLeft className="size-4" />
            </ShellActionButton>
          ) : (
            <Package />
          )
        }
        title={
          detailApp
            ? appDetailName(appDisplayName(detailApp, t))
            : detailCatalogApp
              ? appDetailName(appRegistryTitle(detailCatalogApp, locale))
              : t("apps.title")
        }
      />
      <div className="min-h-0 flex-1 overflow-auto">
        <div
          className={cn(
            "mx-auto grid w-full px-6 pt-4 pb-10",
            detailApp || detailCatalogApp ? "max-w-3xl gap-8" : "max-w-5xl gap-7",
          )}
        >
          {detailApp ? (
            <AppDetail
              app={detailApp}
              catalogApp={detailCatalogForInstalled}
              connections={detailConnections}
              token={token}
              onAdd={() => setEditing({ app: detailApp })}
              onDelete={setDeleting}
              onEdit={(connection) => setEditing({ app: detailApp, connection })}
              onSkillSelect={(skill, icon, iconSrc) =>
                setSelectedSkill({
                  appID: detailApp.id,
                  appSource: detailApp.source,
                  appName: appDetailName(
                    detailCatalogForInstalled ? appRegistryTitle(detailCatalogForInstalled, locale) : appDisplayName(detailApp, t),
                  ),
                  icon,
                  iconSrc,
                  skill,
                })
              }
              onUninstall={() => setUninstalling(detailApp)}
              onEditMCPConfig={() => setMCPConfigEditor({ app: detailApp })}
              enablePending={enableMutation.isPending && enableMutation.variables?.id === detailApp.id}
              onEnabledChange={(enabled) => enableMutation.mutate({ id: detailApp.id, enabled })}
            />
          ) : detailCatalogApp ? (
            <CatalogAppDetail
              app={detailCatalogApp}
              detail={catalogDetailQuery.data}
              detailError={catalogDetailQuery.error}
              detailFailed={catalogDetailQuery.isError}
              detailLoading={catalogDetailQuery.isLoading}
              installed={detailCatalogInstalled}
              installing={
                installMutation.isPending &&
                installMutation.variables?.app.id === detailCatalogApp.id &&
                installMutation.variables.release.version === detailCatalogRelease?.version
              }
              releases={detailCatalogReleases}
              selectedRelease={detailCatalogRelease}
              showPreviewVersions={showPreviewVersions}
              onInstall={() => {
                if (detailCatalogRelease) {
                  installMutation.mutate({ app: detailCatalogApp, release: detailCatalogRelease });
                }
              }}
              onReleaseChange={(version) =>
                setCatalogReleaseByID((current) => ({ ...current, [detailCatalogApp.id]: version }))
              }
              onSkillSelect={(skill) =>
                setSelectedSkill({
                  appName: appDetailName(appRegistryTitle(detailCatalogApp, locale)),
                  icon: detailCatalogApp.icon,
                  iconSrc: appRegistryIconURL(detailCatalogApp, OFFICIAL_APP_REGISTRY),
                  skill,
                })
              }
            />
          ) : loadFailed ? (
            <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
              {t("apps.loadFailed")}
            </div>
          ) : (
            <>
              {builtinApps.length > 0 ? (
                <section className="grid gap-3">
                  <div className="border-b pb-3">
                    <h2 className="text-lg font-semibold tracking-normal">{t("apps.builtinTitle")}</h2>
                  </div>
                  <div className="flex min-w-0 flex-wrap gap-2">
                    {builtinApps.map((app) => (
                      <ManagedAppTile
                        key={app.id}
                        app={app}
                        token={token}
                        onSelect={() => setDetailAppID(app.id)}
                      />
                    ))}
                  </div>
                </section>
              ) : null}
              <section className="grid gap-3">
                <div className="flex items-center justify-between gap-4 border-b pb-3">
                  <h2 className="text-lg font-semibold tracking-normal">{t("apps.installedTitle")}</h2>
                  <Button
                    className="shrink-0 rounded-full"
                    size="sm"
                    type="button"
                    variant="outline"
                    onClick={() => setMCPConfigEditor({})}
                  >
                    <Plus className="size-3.5" />
                    {t("apps.mcpAppAdd")}
                  </Button>
                </div>
                {allInstalledApps.length > 0 ? (
                  <div className="flex min-w-0 flex-wrap gap-2">
                    {allInstalledApps.map((app) => (
                      <ManagedAppTile
                        key={app.id}
                        app={app}
                        token={token}
                        onSelect={() => setDetailAppID(app.id)}
                      />
                    ))}
                  </div>
                ) : (
                  <EmptyLine>{t("apps.noInstalled")}</EmptyLine>
                )}
              </section>
              <section className="grid gap-3">
                <div className="flex items-center justify-between gap-4 border-b pb-3">
                  <h2 className="text-lg font-semibold tracking-normal">{t("apps.availableTitle")}</h2>
                  {upgradeTargets.length > 0 ? (
                    <Button
                      className="shrink-0 rounded-full"
                      disabled={upgradeAllMutation.isPending || installMutation.isPending}
                      size="sm"
                      type="button"
                      variant="outline"
                      onClick={() => upgradeAllMutation.mutate(upgradeTargets)}
                    >
                      {upgradeAllMutation.isPending ? <Spinner className="size-3.5" /> : <Download className="size-3.5" />}
                      {t("apps.upgradeAll")}
                      <span className="text-muted-foreground">{upgradeTargets.length}</span>
                    </Button>
                  ) : null}
                </div>
                {catalogQuery.isLoading ? (
                  <SectionSpinner />
                ) : catalogQuery.isError ? (
                  <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
                    {t("apps.loadFailed")}
                  </div>
                ) : catalogApps.length > 0 ? (
                  <div className="grid w-full grid-cols-1 gap-x-6 gap-y-2 md:grid-cols-2">
                    {catalogApps.map((app) => {
                      const installed = installedByID.get(appRegistryLocalID(app));
                      const release =
                        appRegistryDefaultRelease(app, showPreviewVersions, installed) ||
                        appRegistryDefaultRelease(app, false, installed);
                      if (!release) {
                        return null;
                      }
                      return (
                        <CatalogAppItem
                          key={app.id}
                          app={app}
                          installed={installed}
                          installing={
                            (installMutation.isPending &&
                              installMutation.variables?.app.id === app.id &&
                              installMutation.variables.release.version === release.version) ||
                            (upgradeAllMutation.isPending && bulkUpgradeAppID === app.id)
                          }
                          installDisabled={upgradeAllMutation.isPending}
                          release={release}
                          showPreviewVersions={showPreviewVersions}
                          token={token}
                          onInstall={() => installMutation.mutate({ app, release })}
                          onSelect={() => {
                            if (installed && !needsAppUpgrade(installed, release)) {
                              setDetailAppID(installed.id);
                            } else {
                              setDetailCatalogID(app.id);
                            }
                          }}
                        />
                      );
                    })}
                  </div>
                ) : (
                  <EmptyLine>{t("apps.empty")}</EmptyLine>
                )}
              </section>
            </>
          )}
        </div>
      </div>
      <ConnectionDialog
        connections={connections}
        editing={editing}
        token={token}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(null);
          }
        }}
      />
      <MCPAppConfigDialog
        editor={mcpConfigEditor}
        token={token}
        onSaved={(savedApps) => {
          if (savedApps.length === 1) {
            setDetailAppID(savedApps[0].id);
          }
        }}
        onOpenChange={(open) => {
          if (!open) {
            setMCPConfigEditor(null);
          }
        }}
      />
      <SkillDetailDialog
        appID={selectedSkill?.appID}
        appSource={selectedSkill?.appSource}
        failed={selectedSkillQuery.isError}
        icon={selectedSkill?.icon}
        iconSrc={selectedSkill?.iconSrc}
        loading={selectedSkillQuery.isLoading}
        open={Boolean(selectedSkill)}
        skill={selectedSkillDetail}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedSkill(null);
          }
        }}
      />
      <AlertDialog open={Boolean(deleting)} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("apps.connectionDeleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("apps.connectionDeleteDesc")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (deleting) {
                  deleteMutation.mutate(deleting);
                }
              }}
            >
              {deleteMutation.isPending ? <Spinner /> : <Trash />}
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={Boolean(uninstalling)} onOpenChange={(open) => !open && setUninstalling(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("apps.uninstallTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("apps.uninstallDesc").replace("{name}", uninstalling?.name || "")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (uninstalling) {
                  uninstallMutation.mutate(uninstalling.id);
                }
              }}
            >
              {uninstallMutation.isPending ? <Spinner /> : <Trash />}
              {t("apps.uninstall")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  );
}

const MCP_APP_CONFIG_SAMPLE = `{
  "mcpServers": {
    "Local MCP": {
      "command": "path-to-server",
      "args": []
    }
  }
}`;

function MCPAppConfigDialog({
  editor,
  token,
  onOpenChange,
  onSaved,
}: {
  editor: MCPAppConfigEditor | null;
  token: string;
  onOpenChange: (open: boolean) => void;
  onSaved: (apps: AppDefinition[]) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const app = editor?.app;
  const open = Boolean(editor);
  const [name, setName] = useState("");
  const [configJSON, setConfigJSON] = useState(MCP_APP_CONFIG_SAMPLE);
  const [error, setError] = useState("");
  const configQuery = useQuery({
    queryKey: queryKeys.appMCPConfig(app?.id || ""),
    queryFn: () => getMCPAppConfig(token, app!.id),
    enabled: open && Boolean(app),
    retry: false,
  });

  useEffect(() => {
    if (!open) {
      return;
    }
    setError("");
    if (!app) {
      setName("");
      setConfigJSON(MCP_APP_CONFIG_SAMPLE);
      return;
    }
    setName(app.name || "");
    if (configQuery.data?.configJSON) {
      setConfigJSON(configQuery.data.configJSON);
    }
  }, [app, configQuery.data?.configJSON, open]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      JSON.parse(configJSON);
      if (app) {
        return { apps: [await putMCPAppConfig(token, app.id, configJSON, name.trim() || undefined)] };
      }
      return importMCPApps(token, configJSON, name.trim() || undefined);
    },
    onSuccess: async ({ apps }) => {
      queryClient.setQueryData<{ apps: AppDefinition[] }>(queryKeys.apps(), (current) => {
        const savedByID = new Map(apps.map((item) => [item.id, item]));
        const existing = (current?.apps || []).filter((item) => !savedByID.has(item.id));
        return { apps: [...existing, ...apps].sort((a, b) => a.name.localeCompare(b.name)) };
      });
      for (const saved of apps) {
        queryClient.removeQueries({ queryKey: queryKeys.appMCPStatus(saved.id) });
        queryClient.removeQueries({ queryKey: queryKeys.appMCPConfig(saved.id) });
      }
      toast.success(t(app ? "apps.mcpAppUpdated" : "apps.mcpAppImported"));
      onSaved(apps);
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.apps() });
    },
    onError: (cause) => {
      setError(cause instanceof Error ? cause.message : t("apps.mcpAppSaveFailed"));
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(app ? "apps.mcpAppEditTitle" : "apps.mcpAppAddTitle")}</DialogTitle>
          <DialogDescription>{app?.name || t("apps.mcpAppJSONFormat")}</DialogDescription>
        </DialogHeader>
        {configQuery.isLoading && app ? (
          <SectionSpinner />
        ) : configQuery.isError && app ? (
          <div className="rounded-md bg-destructive/5 px-3 py-2 text-sm text-destructive">
            {t("apps.mcpAppLoadFailed")}
          </div>
        ) : (
          <div className="grid gap-4">
            <div className="grid gap-2">
              <Label htmlFor="mcp-app-name">{t("apps.mcpAppName")}</Label>
              <Input
                id="mcp-app-name"
                maxLength={128}
                value={name}
                placeholder={t("apps.mcpAppNamePlaceholder")}
                onChange={(event) => {
                  setName(event.target.value);
                  setError("");
                }}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="mcp-app-config-json">JSON</Label>
              <Textarea
                id="mcp-app-config-json"
                className="min-h-72 max-h-[55vh] resize-y overflow-auto font-mono text-xs leading-5"
                spellCheck={false}
                value={configJSON}
                onChange={(event) => {
                  setConfigJSON(event.target.value);
                  setError("");
                }}
              />
            </div>
            {error ? <div className="text-sm text-destructive">{error}</div> : null}
          </div>
        )}
        <DialogFooter>
          <Button disabled={saveMutation.isPending} type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={saveMutation.isPending || Boolean(app && (configQuery.isLoading || configQuery.isError))}
            type="button"
            onClick={() => saveMutation.mutate()}
          >
            {saveMutation.isPending ? <Spinner /> : app ? <FileJson2 /> : <Plus />}
            {t(app ? "common.save" : "apps.mcpAppImport")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SectionTitle({ children }: { children: ReactNode }) {
  return <h2 className="border-b pb-4 text-lg font-semibold tracking-normal">{children}</h2>;
}

function SectionSpinner() {
  const { t } = useI18n();
  return (
    <div className="flex h-24 items-center justify-center text-muted-foreground">
      <Spinner aria-label={t("common.loading")} className="size-4" />
    </div>
  );
}

async function fetchAppRegistry(url: string): Promise<AppRegistry> {
  const response = await fetch(url, { cache: "reload" });
  if (!response.ok) {
    throw new Error(`app registry request failed: ${response.status}`);
  }
  const data = (await response.json()) as Partial<AppRegistry>;
  return {
    items: Array.isArray(data.items)
      ? data.items.filter(isAppRegistryItem).filter((item) => normalizedAppRegistryReleases(item).length > 0)
      : [],
  };
}

async function fetchAppPackage(item: AppRegistryItem, registryURL: string, release?: AppRegistryRelease): Promise<string> {
  const target = release || appRegistryDefaultRelease(item, true);
  if (!target?.package) {
    throw new Error("app package is missing");
  }
  const packageURL = new URL(target.package, registryURL);
  if (target.package_sha256) {
    packageURL.searchParams.set("v", target.package_sha256);
  }
  const response = await fetch(packageURL.href, { cache: "reload" });
  if (!response.ok) {
    throw new Error(`app package request failed: ${response.status}`);
  }
  const body = await response.text();
  if (target.package_sha256 && crypto.subtle) {
    const actual = await sha256Hex(body);
    if (actual !== target.package_sha256.toLowerCase()) {
      throw new Error("app package hash mismatch");
    }
  }
  return body;
}

async function fetchCatalogAppContent(item: AppRegistryItem, registryURL: string, release?: AppRegistryRelease): Promise<CatalogAppContent> {
  return parseCatalogAppContent(await fetchAppPackage(item, registryURL, release));
}

function parseCatalogAppContent(packageJSON: string): CatalogAppContent {
  const pkg = JSON.parse(packageJSON) as AppPackage;
  const files = new Map<string, string>();
  for (const file of pkg.files || []) {
    const path = file.path?.trim();
    if (path) {
      files.set(path, packageFileText(file));
    }
  }
  const appYaml = files.get("app.yaml") || "";
  const skillPaths = parseYamlList(appYaml, "skills");
  return {
    endpoints: parseYamlEndpoints(appYaml),
    skills: skillPaths.map((path) => {
      const content = files.get(path) || "";
      const metadata = parseSkillFrontmatter(content);
      return {
        path,
        name: metadata.name || path.split("/").at(-2) || path,
        description: metadata.description || path,
        content,
      };
    }),
  };
}

function packageFileText(file: AppPackageFile) {
  if (typeof file.content === "string") {
    return file.content;
  }
  if (!file.content_base64) {
    return "";
  }
  const binary = atob(file.content_base64);
  return new TextDecoder().decode(Uint8Array.from(binary, (char) => char.charCodeAt(0)));
}

function parseYamlEndpoints(yaml: string): AppEndpoints {
  const endpoints: AppEndpoints = {};
  let current = "";
  let inURLConfig = false;
  for (const line of yamlSectionLines(yaml, "endpoints")) {
    const endpointMatch = line.match(/^ {2}([\w-]+):\s*$/);
    if (endpointMatch) {
      current = endpointMatch[1];
      inURLConfig = false;
      endpoints[current] = { kind: "rest" };
      continue;
    }
    const urlConfigMatch = line.match(/^ {6}([\w-]+):\s*(.*)$/);
    if (current && inURLConfig && urlConfigMatch) {
      const key = urlConfigMatch[1];
      const value = yamlScalar(urlConfigMatch[2]);
      const config = endpoints[current].urlConfig || { label: "" };
      if (key === "label") {
        config.label = value;
      } else if (key === "description") {
        config.description = value;
      } else if (key === "placeholder") {
        config.placeholder = value;
      } else if (key === "required") {
        config.required = value === "true";
      }
      endpoints[current].urlConfig = config;
      continue;
    }
    const propMatch = line.match(/^ {4}([\w-]+):\s*(.*)$/);
    if (!current || !propMatch) {
      continue;
    }
    const key = propMatch[1];
    const value = yamlScalar(propMatch[2]);
    inURLConfig = key === "url_config";
    if (inURLConfig) {
      endpoints[current].urlConfig = { label: "" };
    } else if (key === "kind" && (value === "rest" || value === "graphql" || value === "mcp")) {
      endpoints[current].kind = value;
    } else if (key === "transport" && (value === "stdio" || value === "streamable_http")) {
      endpoints[current].transport = value;
    } else if (key === "url") {
      endpoints[current].url = value;
    } else if (key === "command") {
      endpoints[current].command = value;
    } else if (key === "description") {
      endpoints[current].description = value;
    }
  }
  for (const [name, endpoint] of Object.entries(endpoints)) {
    if (endpoint.kind === "mcp" && !endpoint.url && !endpoint.command) {
      delete endpoints[name];
    } else if (endpoint.kind !== "mcp" && !endpoint.url) {
      delete endpoints[name];
    }
  }
  return endpoints;
}

function parseYamlList(yaml: string, section: string) {
  return yamlSectionLines(yaml, section)
    .map((line) => line.match(/^ {2}-\s*(.*)$/)?.[1])
    .filter((value): value is string => Boolean(value))
    .map(yamlScalar);
}

function yamlSectionLines(yaml: string, section: string) {
  const lines = yaml.split(/\r?\n/);
  const out: string[] = [];
  let inSection = false;
  for (const line of lines) {
    if (!inSection) {
      if (line.trim() === `${section}:`) {
        inSection = true;
      }
      continue;
    }
    if (line.trim() && !line.startsWith(" ")) {
      break;
    }
    out.push(line);
  }
  return out;
}

function yamlScalar(value: string) {
  const trimmed = value.trim();
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"')) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function parseSkillFrontmatter(markdown: string) {
  const match = markdown.match(/^---\r?\n([\s\S]*?)\r?\n---/);
  const metadata: { name?: string; description?: string } = {};
  if (!match) {
    return metadata;
  }
  for (const line of match[1].split(/\r?\n/)) {
    const prop = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (!prop) {
      continue;
    }
    if (prop[1] === "name") {
      metadata.name = yamlScalar(prop[2]);
    } else if (prop[1] === "description") {
      metadata.description = yamlScalar(prop[2]);
    }
  }
  return metadata;
}

function isAppRegistryItem(value: unknown): value is AppRegistryItem {
  if (!value || typeof value !== "object") {
    return false;
  }
  const item = value as Record<string, unknown>;
  const releases = Array.isArray(item.releases) ? item.releases : [];
  return (
    typeof item.id === "string" &&
    (typeof item.package === "string" ||
      releases.some((release) => Boolean(release && typeof release === "object" && typeof (release as Record<string, unknown>).package === "string")))
  );
}

function appRegistryLocalID(item: AppRegistryItem) {
  return item.name || item.id.split("/").pop() || item.id;
}

function appRegistryReleases(item: AppRegistryItem, includePreview: boolean): AppRegistryRelease[] {
  const releases = normalizedAppRegistryReleases(item);
  return includePreview ? releases : releases.filter((release) => !isPreviewRelease(release));
}

function appRegistryDefaultRelease(
  item: AppRegistryItem,
  includePreview: boolean,
  installed?: AppDefinition,
): AppRegistryRelease | undefined {
  return selectAppInstallRelease(normalizedAppRegistryReleases(item), includePreview, installed);
}

function normalizedAppRegistryReleases(item: AppRegistryItem): AppRegistryRelease[] {
  const releases = (item.releases || []).map(normalizedAppRegistryRelease).filter((release): release is AppRegistryRelease => Boolean(release));
  if (item.package) {
    const topLevel: AppRegistryRelease = {
      version: item.version || "",
      manifest: item.manifest,
      package: item.package,
      package_sha256: item.package_sha256,
      targets: item.targets,
    };
    if (!releases.some((release) => release.version === topLevel.version && release.package === topLevel.package)) {
      releases.push(topLevel);
    }
  }
  return releases
    .filter((release) => supportsAppRegistryTarget(release.targets, APP_CATALOG_TARGET))
    .sort(compareRegistryReleases);
}

function normalizedAppRegistryRelease(value: AppRegistryRelease | undefined): AppRegistryRelease | null {
  const version = value?.version?.trim();
  const packagePath = value?.package?.trim();
  if (!version || !packagePath) {
    return null;
  }
  return {
    ...value,
    version,
    package: packagePath,
    manifest: value?.manifest?.trim(),
    package_sha256: value?.package_sha256?.trim(),
    channel: value?.channel?.trim(),
    targets: normalizedAppRegistryTargets(value?.targets),
  };
}

function normalizedAppRegistryTargets(values: string[] | undefined) {
  return (values || []).map((value) => value.trim().toLowerCase()).filter(Boolean);
}

function supportsAppRegistryTarget(targets: string[] | undefined, target: string) {
  const normalized = normalizedAppRegistryTargets(targets);
  // targets predates the mobile catalog; an omitted value is a legacy desktop release.
  return normalized.length === 0 ? target === "desktop" : normalized.includes(target);
}

function compareRegistryReleases(a: AppRegistryRelease, b: AppRegistryRelease) {
  return compareAppVersions(b.version, a.version);
}

function appRegistryTitle(item: AppRegistryItem, locale: string) {
  return localizedText(item.title, locale) || item.name || item.id;
}

function appDetailName(value: string, replaceHyphens = false) {
  const separators = replaceHyphens ? /[_-]+(.)?/g : /_+(.)?/g;
  const normalized = value.trim().replace(separators, (_, next: string | undefined) => ` ${next?.toUpperCase() || ""}`);
  if (!normalized) {
    return "";
  }
  return normalized.charAt(0).toUpperCase() + normalized.slice(1);
}

function skillDetailName(value: string) {
  return appDetailName(value, true);
}

function appRegistryDescription(item: AppRegistryItem, locale: string) {
  return localizedText(item.description, locale) || "";
}

function appRegistryIconURL(item: AppRegistryItem, registryURL: string) {
  const raw = item.icon?.svg?.trim();
  if (!raw) {
    return undefined;
  }
  const url = new URL(raw, registryURL);
  const cacheKey = item.icon_sha256 || item.package_sha256 || item.version || item.id;
  if (cacheKey) {
    url.searchParams.set("v", cacheKey);
  }
  return url.href;
}

function localizedText(value: LocalizedText | undefined, locale: string) {
  if (!value) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  return value[locale] || value["zh-CN"] || value.en || Object.values(value)[0] || "";
}

async function sha256Hex(text: string) {
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function ManagedAppTile({
  app,
  onSelect,
  token,
}: {
  app: AppDefinition;
  onSelect: () => void;
  token: string;
}) {
  const { t } = useI18n();
  const name = appDisplayName(app, t);
  const iconSrc = app.source === "installed" ? appIconURL(token, app) : undefined;
  return (
    <button
      className="group grid min-h-20 w-20 min-w-0 shrink-0 content-center justify-items-center gap-1.5 rounded-lg px-1 py-2 text-center transition-colors hover:bg-interactive-hover active:bg-interactive-pressed"
      type="button"
      onClick={onSelect}
    >
      <span className="relative shrink-0">
        <AppIdentityIcon app={app} iconSrc={iconSrc} size="xl" />
        <span
          aria-hidden="true"
          className={cn(
            "absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full border-2 border-background",
            app.enabled ? "bg-emerald-500" : "bg-muted-foreground/45",
          )}
        />
        <span className="sr-only">{app.enabled ? t("apps.enabled") : t("apps.disabled")}</span>
      </span>
      <span className="w-full min-w-0">
        <span className={cn("block truncate text-xs leading-5", app.enabled ? "text-foreground/80" : "text-muted-foreground/60")}>
          {name}
        </span>
      </span>
    </button>
  );
}

function AppDetail({
  app,
  catalogApp,
  connections,
  enablePending,
  onAdd,
  onDelete,
  onEdit,
  onEditMCPConfig,
  onEnabledChange,
  onSkillSelect,
  onUninstall,
  token,
}: {
  app: AppDefinition;
  catalogApp?: AppRegistryItem;
  connections: AppConnection[];
  enablePending: boolean;
  onAdd: () => void;
  onDelete: (connection: AppConnection) => void;
  onEdit: (connection: AppConnection) => void;
  onEditMCPConfig: () => void;
  onEnabledChange: (enabled: boolean) => void;
  onSkillSelect: (skill: AppSkillItem, icon?: AppIconSpec, iconSrc?: string) => void;
  onUninstall: () => void;
  token: string;
}) {
  const { locale, t } = useI18n();
  const endpoints = Object.entries(app.endpoints || {}).sort(([a], [b]) => a.localeCompare(b));
  const skills = (app.skills || []) as AppSkillItems;
  const icon = app.icon;
  const iconSrc = appIconURL(token, app);
  const title = appDetailName(catalogApp ? appRegistryTitle(catalogApp, locale) : appDisplayName(app, t));
  const description = (catalogApp ? appRegistryDescription(catalogApp, locale) : "") || appDisplayDescription(app, t);
  const authMethods = appAuthMethods(app);
  const canManageConnections = appCanManageConnections(app);
  const hasGitHubAppConnection = app.id === "github" && connections.some(
    (connection) => connection.authMethodID === "github-app" && !connection.reauthorizationRequired,
  );
  const installedIsPreview = Boolean(
    app.version &&
      catalogApp &&
      normalizedAppRegistryReleases(catalogApp).some((release) => release.version === app.version && isPreviewRelease(release)),
  );
  const hasMCPEndpoints = endpoints.some(([, endpoint]) => endpoint.kind === "mcp");
  const mcpStatusQuery = useQuery({
    queryKey: queryKeys.appMCPStatus(app.id),
    queryFn: () => getAppMCPStatus(token, app.id),
    enabled: hasMCPEndpoints && app.enabled,
    retry: false,
    staleTime: 30_000,
  });
  const mcpStatusByEndpoint = useMemo(
    () => groupMCPStatusByEndpoint(mcpStatusQuery.data?.endpoints || []),
    [mcpStatusQuery.data?.endpoints],
  );
  const tools = useMemo(
    () => collectAppTools(app, mcpStatusQuery.data?.endpoints || []),
    [app, mcpStatusQuery.data?.endpoints],
  );

  return (
    <section className="grid gap-6">
      <div className="min-w-0 border-b pb-5">
        <div className="flex min-w-0 items-start gap-4">
          <AppIdentityIcon app={app} icon={icon} iconSrc={iconSrc} size="hero" />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-start justify-between gap-4">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h2 className="truncate text-2xl font-semibold tracking-normal">{title}</h2>
                {app.source === "builtin" ? <Badge variant="secondary">{t("apps.builtinBadge")}</Badge> : null}
                {app.kind === "mcp" ? <Badge variant="secondary">MCP</Badge> : null}
                {app.version ? <Badge variant="outline">v{app.version}</Badge> : null}
                <Badge variant="outline">
                  {t("apps.requiresMode").replace("{mode}", app.requiredMode.charAt(0).toUpperCase() + app.requiredMode.slice(1))}
                </Badge>
                {installedIsPreview ? <Badge variant="secondary">{t("apps.previewVersion")}</Badge> : null}
                {catalogApp?.tags?.map((tag) => (
                  <Badge key={tag} variant="outline">
                    {tag}
                  </Badge>
                ))}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {app.id === "computer-use" ? (
                  <Button size="sm" type="button" variant="outline" onClick={() => openSettingsDialog({ section: "permissions" })}>
                    <Settings2 className="size-3.5" />
                    {t("apps.computerUse.permissions")}
                  </Button>
                ) : null}
                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground">{app.enabled ? t("apps.enabled") : t("apps.disabled")}</span>
                  <Switch
                    aria-label={t("apps.enableToggle").replace("{name}", title)}
                    checked={app.enabled}
                    disabled={enablePending}
                    onCheckedChange={onEnabledChange}
                  />
                </div>
                {app.canUninstall ? (
                  <Button className="text-destructive hover:text-destructive" type="button" variant="ghost" onClick={onUninstall}>
                    <Trash className="size-3.5" />
                    {t("apps.uninstall")}
                  </Button>
                ) : null}
              </div>
            </div>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{description || t("apps.noDescription")}</p>
          </div>
        </div>
      </div>

      <div className="grid gap-6">
        {canManageConnections || connections.length > 0 ? (
          <DetailSection
            title={t("apps.connections")}
            count={connections.length}
            action={
              canManageConnections ? (
                <Button size="sm" type="button" variant="outline" onClick={onAdd}>
                  <Plus className="size-3.5" />
                  {t("apps.addConnection")}
                </Button>
              ) : null
            }
          >
            {connections.length > 0 ? (
              <div className="grid overflow-hidden rounded-lg border border-border/70 bg-card">
                {connections.map((connection, index) => (
                  <ConnectionRow
                    key={connection.id}
                    authMethods={authMethods}
                    connection={connection}
                    divided={index > 0}
                    onDelete={onDelete}
                    onEdit={onEdit}
                  />
                ))}
              </div>
            ) : (
              <EmptyLine>{t("apps.noConnections")}</EmptyLine>
            )}
            {hasGitHubAppConnection ? (
              <div className="flex min-w-0 items-center gap-3 rounded-lg border border-border/70 bg-card px-3 py-2.5">
                <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{t("apps.githubAccessTitle")}</div>
                  <div className="text-xs leading-5 text-muted-foreground">{t("apps.githubAccessDescription")}</div>
                </div>
                <Button
                  className="shrink-0"
                  size="sm"
                  type="button"
                  variant="outline"
                  onClick={() => void openExternalURL(GITHUB_APP_ACCESS_SETTINGS_URL)}
                >
                  <Settings2 className="size-3.5" />
                  {t("apps.githubAccessAction")}
                </Button>
              </div>
            ) : null}
          </DetailSection>
        ) : null}

        {endpoints.length > 0 ? (
          <AppEndpointsSection
            appID={app.id}
            endpoints={endpoints}
            mcpConfigurable={app.kind !== "mcp"}
            mcpStatusByEndpoint={mcpStatusByEndpoint}
            mcpStatusFailed={mcpStatusQuery.isError}
            mcpStatusLoading={mcpStatusQuery.isLoading}
            mcpStatusVisible={app.enabled}
            onEditMCPConfig={app.kind === "mcp" ? onEditMCPConfig : undefined}
            token={token}
          />
        ) : null}
        <AppToolsSection tools={tools} />
        {skills.length > 0 ? (
          <AppSkillsSection
            app={app}
            icon={icon}
            iconSrc={iconSrc}
            skills={skills}
            onSkillSelect={(skill) => onSkillSelect(skill, icon, iconSrc)}
          />
        ) : null}
      </div>
    </section>
  );
}

function CatalogAppItem({
  app,
  installed,
  installing,
  installDisabled,
  onInstall,
  onSelect,
  release,
  showPreviewVersions,
  token,
}: {
  app: AppRegistryItem;
  installed?: AppDefinition;
  installing: boolean;
  installDisabled: boolean;
  onInstall: () => void;
  onSelect: () => void;
  release: AppRegistryRelease;
  showPreviewVersions: boolean;
  token: string;
}) {
  const { locale, t } = useI18n();
  const title = appDetailName(appRegistryTitle(app, locale));
  const description = appRegistryDescription(app, locale);
  const upgradeAvailable = installed ? needsAppUpgrade(installed, release) : false;
  const installedCurrentOrNewer = Boolean(installed) && !upgradeAvailable;
  const previewAvailable = showPreviewVersions && isPreviewRelease(release);
  const icon = installed ? installed.icon : app.icon;
  const iconSrc = installed ? appIconURL(token, installed) : appRegistryIconURL(app, OFFICIAL_APP_REGISTRY);

  return (
    <div className="min-w-0 border-b border-border/60 pb-2">
      <section className="flex min-h-24 min-w-0 items-center gap-3 rounded-xl px-3 py-2 transition-colors hover:bg-item-hover focus-within:bg-item-hover">
        <button className="flex min-w-0 flex-1 items-center gap-3 self-stretch overflow-hidden text-left" type="button" onClick={onSelect}>
          <div className="relative shrink-0">
            <AppIcon icon={icon} size="xl" src={iconSrc} />
            {upgradeAvailable ? (
              <span aria-hidden className="absolute -right-0.5 -top-0.5 size-2.5 rounded-full border-2 border-background bg-destructive" />
            ) : null}
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2 overflow-hidden">
              <h3 className="min-w-0 truncate text-sm font-semibold">{title}</h3>
              {release.version ? <span className="shrink-0 text-xs text-muted-foreground">v{release.version}</span> : null}
              {previewAvailable ? <Badge variant="secondary">{t("apps.previewAvailable")}</Badge> : null}
            </div>
            <p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{description || t("apps.noDescription")}</p>
          </div>
        </button>
        <Button
          className="h-8 shrink-0 rounded-full px-3"
          disabled={installDisabled || installing || installedCurrentOrNewer}
          size="xs"
          type="button"
          variant="outline"
          onClick={onInstall}
        >
          {installing ? <Spinner className="size-3.5" /> : upgradeAvailable ? <Download className="size-3.5" /> : null}
          {installedCurrentOrNewer ? t("apps.installedAction") : upgradeAvailable ? t("apps.upgrade") : t("apps.install")}
        </Button>
      </section>
    </div>
  );
}

function CatalogAppDetail({
  app,
  detail,
  detailError,
  detailFailed,
  detailLoading,
  installed,
  installing,
  onReleaseChange,
  onInstall,
  releases,
  selectedRelease,
  showPreviewVersions,
  onSkillSelect,
}: {
  app: AppRegistryItem;
  detail?: CatalogAppContent;
  detailError?: unknown;
  detailFailed: boolean;
  detailLoading: boolean;
  installed?: AppDefinition;
  installing: boolean;
  onReleaseChange: (version: string) => void;
  onInstall: () => void;
  releases: AppRegistryRelease[];
  selectedRelease?: AppRegistryRelease;
  showPreviewVersions: boolean;
  onSkillSelect: (skill: AppSkillItem) => void;
}) {
  const { locale, t } = useI18n();
  const title = appDetailName(appRegistryTitle(app, locale));
  const description = appRegistryDescription(app, locale);
  const upgradeAvailable = installed && selectedRelease ? needsAppUpgrade(installed, selectedRelease) : false;
  const installedCurrentOrNewer = Boolean(installed && selectedRelease && !upgradeAvailable);
  const endpoints = Object.entries(detail?.endpoints || {}).sort(([a], [b]) => a.localeCompare(b));
  const skills = detail?.skills || [];
  const showEndpoints = detailLoading || detailFailed || endpoints.length > 0;
  const showSkills = detailLoading || detailFailed || skills.length > 0;
  return (
    <section className="grid gap-8">
      <div className="min-w-0 border-b pb-6">
        <div className="flex min-w-0 items-start gap-4">
          <AppIcon icon={app.icon} size="hero" src={appRegistryIconURL(app, OFFICIAL_APP_REGISTRY)} />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-start justify-between gap-4">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h2 className="truncate text-2xl font-semibold tracking-normal">{title}</h2>
                {selectedRelease?.version ? <Badge variant="outline">v{selectedRelease.version}</Badge> : null}
                {selectedRelease && isPreviewRelease(selectedRelease) ? (
                  <Badge variant="secondary">{t("apps.previewVersion")}</Badge>
                ) : null}
                {app.tags?.map((tag) => (
                  <Badge key={tag} variant="outline">
                    {tag}
                  </Badge>
                ))}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {releases.length > 1 ? (
                  <Select value={selectedRelease?.version || ""} onValueChange={onReleaseChange}>
                    <SelectTrigger className="w-40">
                      <SelectValue />
                    </SelectTrigger>
                    <DialogSelectContent>
                      {releases.map((release) => (
                        <SelectItem key={release.version} value={release.version}>
                          {release.version}
                        </SelectItem>
                      ))}
                    </DialogSelectContent>
                  </Select>
                ) : null}
                <Button
                  className="shrink-0"
                  disabled={installing || installedCurrentOrNewer || !selectedRelease}
                  type="button"
                  onClick={onInstall}
                >
                  {installing ? <Spinner className="size-4" /> : upgradeAvailable ? <Download className="size-4" /> : null}
                  {installedCurrentOrNewer ? t("apps.installedAction") : upgradeAvailable ? t("apps.upgrade") : t("apps.install")}
                </Button>
              </div>
            </div>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{description || t("apps.noDescription")}</p>
          </div>
        </div>
      </div>
      {showEndpoints ? (
        <AppEndpointsSection count={detail ? endpoints.length : undefined} endpoints={endpoints}>
          {detailLoading ? (
            <DetailSkeletonRows />
          ) : detailFailed ? (
            <ContentLoadFailed error={detailError} />
          ) : (
            <EndpointRows endpoints={endpoints} />
          )}
        </AppEndpointsSection>
      ) : null}
      {showSkills ? (
        <AppSkillsSection
          count={detail ? skills.length : undefined}
          icon={app.icon}
          iconSrc={appRegistryIconURL(app, OFFICIAL_APP_REGISTRY)}
          skills={skills}
          onSkillSelect={onSkillSelect}
        >
          {detailLoading ? (
            <DetailSkeletonRows />
          ) : detailFailed ? (
            <ContentLoadFailed error={detailError} />
          ) : (
            <SkillRows
              icon={app.icon}
              iconSrc={appRegistryIconURL(app, OFFICIAL_APP_REGISTRY)}
              skills={skills}
              onSkillSelect={onSkillSelect}
            />
          )}
        </AppSkillsSection>
      ) : null}
    </section>
  );
}

function AppEndpointsSection({
  appID,
  children,
  count,
  endpoints,
  mcpConfigurable = true,
  mcpStatusByEndpoint,
  mcpStatusFailed,
  mcpStatusLoading,
  mcpStatusVisible = true,
  onEditMCPConfig,
  token,
}: {
  appID?: string;
  children?: ReactNode;
  count?: number;
  endpoints: Array<[string, AppEndpoints[string]]>;
  mcpConfigurable?: boolean;
  mcpStatusByEndpoint?: Map<string, AppMCPEndpointStatus[]>;
  mcpStatusFailed?: boolean;
  mcpStatusLoading?: boolean;
  mcpStatusVisible?: boolean;
  onEditMCPConfig?: () => void;
  token?: string;
}) {
  const { t } = useI18n();
  return (
    <DetailSection title={t("apps.endpoints")} count={count ?? endpoints.length}>
      {children ?? (
        <EndpointRows
          appID={appID}
          endpoints={endpoints}
          mcpConfigurable={mcpConfigurable}
          mcpStatusByEndpoint={mcpStatusByEndpoint}
          mcpStatusFailed={mcpStatusFailed}
          mcpStatusLoading={mcpStatusLoading}
          mcpStatusVisible={mcpStatusVisible}
          onEditMCPConfig={onEditMCPConfig}
          token={token}
        />
      )}
    </DetailSection>
  );
}

function EndpointRows({
  appID,
  endpoints,
  mcpConfigurable = true,
  mcpStatusByEndpoint,
  mcpStatusFailed,
  mcpStatusLoading,
  mcpStatusVisible = true,
  onEditMCPConfig,
  token,
}: {
  appID?: string;
  endpoints: Array<[string, AppEndpoints[string]]>;
  mcpConfigurable?: boolean;
  mcpStatusByEndpoint?: Map<string, AppMCPEndpointStatus[]>;
  mcpStatusFailed?: boolean;
  mcpStatusLoading?: boolean;
  mcpStatusVisible?: boolean;
  onEditMCPConfig?: () => void;
  token?: string;
}) {
  const { t } = useI18n();
  const [editingMCP, setEditingMCP] = useState<{ name: string; endpoint: AppEndpoints[string] } | null>(null);
  const [mcpConfiguredByEndpoint, setMCPConfiguredByEndpoint] = useState<Record<string, boolean | undefined>>({});
  if (endpoints.length === 0) {
    return <EmptyLine>{t("apps.none")}</EmptyLine>;
  }
  return (
    <>
      <div className="grid overflow-hidden rounded-lg border border-border/70 bg-card">
        {endpoints.map(([name, endpoint], index) => {
          const statuses = mcpStatusByEndpoint?.get(name) || [];
          const statusConfigured = statuses.some((status) => status.configured);
          const isMCPConfigured = endpoint.kind === "mcp" && (mcpConfiguredByEndpoint[name] ?? statusConfigured);
          return (
            <DetailRow key={name} className="relative rounded-none bg-transparent px-3 py-2.5">
              {index > 0 ? <span aria-hidden="true" className="absolute inset-x-3 top-0 border-t border-border/70" /> : null}
              <div className="flex min-w-0 items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-medium">{appDetailName(name)}</span>
                  <Badge
                    className="h-4 px-1.5 py-0 text-[10px] leading-none font-semibold text-muted-foreground"
                    variant="secondary"
                  >
                    {endpoint.kind.toUpperCase()}
                  </Badge>
                  {endpoint.kind === "mcp" && endpoint.transport ? <Badge variant="secondary">{endpoint.transport}</Badge> : null}
                </div>
                {endpoint.kind === "mcp" && onEditMCPConfig ? (
                  <Button size="sm" type="button" variant="secondary" onClick={onEditMCPConfig}>
                    <FileJson2 className="size-3.5" />
                    {t("apps.mcpAppEdit")}
                  </Button>
                ) : mcpConfigurable && appID && token && endpoint.kind === "mcp" ? (
                  <Button
                    size="sm"
                    type="button"
                    variant={isMCPConfigured ? "default" : "outline"}
                    onClick={() => setEditingMCP({ name, endpoint })}
                  >
                    <Settings2 className="size-3.5" />
                    {t(isMCPConfigured ? "apps.mcpConfigEdit" : "apps.mcpConfigAdd")}
                  </Button>
                ) : null}
              </div>
              {endpoint.description ? <div className="text-xs text-muted-foreground">{endpoint.description}</div> : null}
              {endpoint.kind === "mcp" && mcpStatusVisible ? (
                <MCPStatusDetails
                  endpointName={name}
                  failed={mcpStatusFailed}
                  loading={mcpStatusLoading}
                  statuses={statuses}
                />
              ) : null}
            </DetailRow>
          );
        })}
      </div>
      {appID && token && editingMCP ? (
        <MCPConfigDialog
          appID={appID}
          endpoint={editingMCP.endpoint}
          endpointName={editingMCP.name}
          open={Boolean(editingMCP)}
          token={token}
          onConfiguredChange={(configured) =>
            setMCPConfiguredByEndpoint((current) => ({ ...current, [editingMCP.name]: configured }))
          }
          onOpenChange={(open) => {
            if (!open) {
              setEditingMCP(null);
            }
          }}
        />
      ) : null}
    </>
  );
}

function MCPConfigDialog({
  appID,
  endpoint,
  endpointName,
  open,
  token,
  onConfiguredChange,
  onOpenChange,
}: {
  appID: string;
  endpoint: AppEndpoints[string];
  endpointName: string;
  open: boolean;
  token: string;
  onConfiguredChange?: (configured: boolean) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [form, setForm] = useState<MCPConfigForm>(() => mcpConfigFormFromEndpoint(endpoint));
  const [error, setError] = useState("");
  const overrideQuery = useQuery({
    queryKey: queryKeys.appMCPOverride(appID, endpointName),
    queryFn: () => getAppMCPOverride(token, appID, endpointName),
    enabled: open,
    retry: false,
  });
  useEffect(() => {
    if (!open) {
      return;
    }
    const effective = overrideQuery.data?.configured ? applyMCPOverrideToEndpoint(endpoint, overrideQuery.data.override) : endpoint;
    setForm(mcpConfigFormFromEndpoint(effective));
    setError("");
  }, [endpoint, open, overrideQuery.data, endpointName]);
  const saveMutation = useMutation({
    mutationFn: (override: AppMCPOverride) => putAppMCPOverride(token, appID, endpointName, override),
    onSuccess: async (data) => {
      onConfiguredChange?.(true);
      updateMCPOverrideCaches(queryClient, appID, endpointName, data, true);
      toast.success(t("apps.mcpConfigSaved"));
      onOpenChange(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appMCPStatus(appID) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appMCPOverride(appID, endpointName) }),
      ]);
    },
    onError: () => toast.error(t("apps.mcpConfigSaveFailed")),
  });
  const resetMutation = useMutation({
    mutationFn: () => deleteAppMCPOverride(token, appID, endpointName),
    onSuccess: async () => {
      onConfiguredChange?.(false);
      updateMCPOverrideCaches(queryClient, appID, endpointName, undefined, false);
      toast.success(t("apps.mcpConfigResetDone"));
      onOpenChange(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appMCPStatus(appID) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appMCPOverride(appID, endpointName) }),
      ]);
    },
    onError: () => toast.error(t("apps.mcpConfigResetFailed")),
  });
  const saving = saveMutation.isPending || resetMutation.isPending;
  const save = () => {
    try {
      setError("");
      saveMutation.mutate(mcpConfigFormToOverride(form, t));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("apps.mcpConfigTitle")}</DialogTitle>
          <DialogDescription className="grid gap-1">
            <span>{endpointName}</span>
            <span>{t("apps.mcpConfigDesc")}</span>
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <label className="grid gap-1.5">
            <span className="text-sm font-medium">{t("apps.mcpTransport")}</span>
            <Select
              value={form.transport}
              onValueChange={(value) => setForm((current) => ({ ...current, transport: value as MCPConfigForm["transport"] }))}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <DialogSelectContent>
                <SelectItem value="stdio">stdio</SelectItem>
                <SelectItem value="streamable_http">streamable_http</SelectItem>
              </DialogSelectContent>
            </Select>
          </label>
          {form.transport === "stdio" ? (
            <>
              <label className="grid gap-1.5">
                <span className="text-sm font-medium">{t("apps.mcpCommand")}</span>
                <Input value={form.command} onChange={(event) => setForm((current) => ({ ...current, command: event.target.value }))} />
              </label>
              <label className="grid gap-1.5">
                <span className="text-sm font-medium">{t("apps.mcpArgs")}</span>
                <Textarea
                  className="min-h-24 font-mono text-xs"
                  placeholder={t("apps.mcpArgsPlaceholder")}
                  value={form.args}
                  onChange={(event) => setForm((current) => ({ ...current, args: event.target.value }))}
                />
              </label>
              <label className="grid gap-1.5">
                <span className="text-sm font-medium">{t("apps.mcpEnv")}</span>
                <Textarea
                  className="min-h-24 font-mono text-xs"
                  placeholder={t("apps.mcpEnvPlaceholder")}
                  value={form.env}
                  onChange={(event) => setForm((current) => ({ ...current, env: event.target.value }))}
                />
              </label>
            </>
          ) : (
            <>
              <label className="grid gap-1.5">
                <span className="text-sm font-medium">{t("apps.mcpURL")}</span>
                <Input value={form.url} onChange={(event) => setForm((current) => ({ ...current, url: event.target.value }))} />
              </label>
              <label className="grid gap-1.5">
                <span className="text-sm font-medium">{t("apps.mcpHeaders")}</span>
                <Textarea
                  className="min-h-24 font-mono text-xs"
                  placeholder={t("apps.mcpHeadersPlaceholder")}
                  value={form.headers}
                  onChange={(event) => setForm((current) => ({ ...current, headers: event.target.value }))}
                />
              </label>
            </>
          )}
          {error ? <div className="text-sm text-destructive">{error}</div> : null}
        </div>
        <DialogFooter className="gap-2 sm:justify-between">
          <Button
            disabled={saving || !overrideQuery.data?.configured}
            type="button"
            variant="ghost"
            onClick={() => resetMutation.mutate()}
          >
            {t("apps.mcpConfigReset")}
          </Button>
          <div className="flex gap-2">
            <Button disabled={saving} type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button disabled={saving} type="button" onClick={save}>
              {t("common.save")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MCPStatusDetails({
  endpointName,
  failed,
  loading,
  statuses,
}: {
  endpointName: string;
  failed?: boolean;
  loading?: boolean;
  statuses: AppMCPEndpointStatus[];
}) {
  const { t } = useI18n();
  if (loading) {
    return <MCPStatusLine icon={<Spinner className="size-3.5" />} label={t("apps.mcpStatus.checking")} tone="muted" />;
  }
  if (failed) {
    return <MCPStatusLine icon={<CircleAlert className="size-3.5" />} label={t("apps.mcpStatus.unavailable")} tone="bad" />;
  }
  if (statuses.length === 0) {
    return <MCPStatusLine icon={<CircleDashed className="size-3.5" />} label={t("apps.mcpStatus.needs_connection")} tone="muted" />;
  }
  return (
    <div className="mt-1 grid gap-2 border-t border-border/60 pt-2">
      {statuses.map((status) => {
        const icon = mcpStatusIcon(status.status);
        const label = mcpStatusLabel(status.status, t);
        const tools = status.tools || [];
        const toolCount = tools.length > 0 ? t("apps.mcpToolsCount").replace("{count}", String(tools.length)) : "";
        const meta = [status.connectionID, toolCount].filter(Boolean).join(" · ") || undefined;
        const tone = mcpStatusTone(status.status);
        return (
          <div key={`${endpointName}:${status.connectionID || "default"}`} className="grid gap-2">
            <MCPStatusLine icon={icon} label={label} meta={meta} tone={tone} />
            {status.error ? <div className="break-words text-xs text-destructive/90">{status.error}</div> : null}
          </div>
        );
      })}
    </div>
  );
}

function MCPStatusLine({
  icon,
  label,
  meta,
  tone,
}: {
  icon: ReactNode;
  label: string;
  meta?: string;
  tone: "good" | "bad" | "muted";
}) {
  return (
    <div
      className={cn(
        "flex min-w-0 items-center gap-1.5 text-xs",
        tone === "good" && "text-emerald-500",
        tone === "bad" && "text-destructive",
        tone === "muted" && "text-muted-foreground",
      )}
    >
      <span className="shrink-0">{icon}</span>
      <span className="font-medium">{label}</span>
      {meta ? <span className="truncate text-muted-foreground">· {meta}</span> : null}
    </div>
  );
}

function mcpStatusIcon(status: string) {
  if (status === "available") {
    return <CircleCheck className="size-3.5" />;
  }
  if (status === "unavailable" || status === "unsupported") {
    return <CircleAlert className="size-3.5" />;
  }
  return <CircleDashed className="size-3.5" />;
}

function mcpStatusTone(status: string): "good" | "bad" | "muted" {
  if (status === "available") {
    return "good";
  }
  if (status === "unavailable" || status === "unsupported") {
    return "bad";
  }
  return "muted";
}

function mcpStatusLabel(status: string, t: (key: string) => string) {
  switch (status) {
    case "available":
      return t("apps.mcpStatus.available");
    case "unavailable":
      return t("apps.mcpStatus.unavailable");
    case "unsupported":
      return t("apps.mcpStatus.unsupported");
    case "needs_connection":
      return t("apps.mcpStatus.needs_connection");
    default:
      return status;
  }
}

function mcpConfigFormFromEndpoint(endpoint: AppEndpoints[string]): MCPConfigForm {
  return {
    transport: endpoint.transport === "streamable_http" ? "streamable_http" : "stdio",
    command: endpoint.command || "",
    args: (endpoint.args || []).join("\n"),
    url: endpoint.url || "",
    env: mapToLines(endpoint.env),
    headers: mapToLines(endpoint.headers),
  };
}

function applyMCPOverrideToEndpoint(endpoint: AppEndpoints[string], override?: AppMCPOverride): AppEndpoints[string] {
  if (!override) {
    return endpoint;
  }
  return {
    ...endpoint,
    transport: override.transport || endpoint.transport,
    url: override.url || endpoint.url,
    command: override.command || endpoint.command,
    args: override.args ?? endpoint.args,
    env: override.env ? { ...(endpoint.env || {}), ...override.env } : endpoint.env,
    headers: override.headers ? { ...(endpoint.headers || {}), ...override.headers } : endpoint.headers,
  };
}

function mcpConfigFormToOverride(form: MCPConfigForm, t: (key: string) => string): AppMCPOverride {
  const transport = form.transport;
  if (transport === "stdio") {
    const command = form.command.trim();
    if (!command) {
      throw new Error(t("apps.mcpConfigCommandRequired"));
    }
    return {
      transport,
      command,
      args: linesToList(form.args),
      env: linesToMap(form.env, t),
    };
  }
  const url = form.url.trim();
  if (!url) {
    throw new Error(t("apps.mcpConfigURLRequired"));
  }
  return {
    transport,
    url,
    headers: linesToMap(form.headers, t),
  };
}

function updateMCPOverrideCaches(
  queryClient: QueryClient,
  appID: string,
  endpointName: string,
  override: AppMCPOverrideResponse | undefined,
  configured: boolean,
) {
  queryClient.setQueryData<AppMCPOverrideResponse>(
    queryKeys.appMCPOverride(appID, endpointName),
    override || { configured: false, override: {} },
  );
  queryClient.setQueryData<AppMCPStatusResponse | undefined>(queryKeys.appMCPStatus(appID), (current) => {
    if (!current) {
      return current;
    }
    return {
      ...current,
      endpoints: current.endpoints.map((status) =>
        status.endpointName === endpointName ? { ...status, configured } : status,
      ),
    };
  });
}

function mapToLines(values?: Record<string, string>) {
  return Object.entries(values || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function linesToList(text: string) {
  return text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
}

function linesToMap(text: string, t: (key: string) => string) {
  const out: Record<string, string> = {};
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line) {
      continue;
    }
    const index = line.indexOf("=");
    if (index <= 0) {
      throw new Error(t("apps.mcpConfigInvalidLine"));
    }
    const key = line.slice(0, index).trim();
    const value = line.slice(index + 1).trim();
    if (!key) {
      throw new Error(t("apps.mcpConfigInvalidLine"));
    }
    out[key] = value;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function formatMCPInputSchema(schema: unknown) {
  try {
    return JSON.stringify(schema, null, 2);
  } catch {
    return String(schema);
  }
}

function collectAppTools(app: AppDefinition, statuses: AppMCPEndpointStatus[]): AppToolItem[] {
  const tools: AppToolItem[] = (app.tools || []).map((tool) => ({
    key: `static:${tool.name}`,
    name: tool.name,
    description: tool.description,
    kind: app.source === "builtin" ? "builtin" : "api",
  }));
  const seen = new Set(tools.map((tool) => tool.key));
  for (const status of statuses) {
    for (const tool of status.tools || []) {
      const key = `mcp:${tool.providerName || `${status.endpointName}:${status.connectionID || "default"}:${tool.name}`}`;
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      tools.push({
        key,
        name: tool.name,
        title: tool.title,
        description: tool.description,
        inputSchema: tool.inputSchema,
        kind: "mcp",
      });
    }
  }
  return tools;
}

function AppToolsSection({ tools }: { tools: AppToolItem[] }) {
  const { t } = useI18n();
  if (tools.length === 0) {
    return null;
  }
  return (
    <DetailSection title={t("apps.tools")} count={tools.length}>
      <div className="grid min-w-0 gap-2 sm:grid-cols-2">
        {tools.map((tool) => <AppToolCard key={tool.key} tool={tool} />)}
      </div>
    </DetailSection>
  );
}

function AppToolCard({ tool }: { tool: AppToolItem }) {
  const { t } = useI18n();

  return (
    <div className="flex min-w-0 items-start gap-3 rounded-lg border border-border/70 bg-card px-3 py-2.5">
      <AppToolGlyph kind={tool.kind} name={tool.name} />
      <div className="grid min-w-0 flex-1 gap-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium">{tool.title || appToolDisplayName(tool.name, t)}</span>
          {tool.kind === "builtin" ? null : (
            <Badge className="shrink-0" variant="outline">
              {tool.kind.toUpperCase()}
            </Badge>
          )}
        </div>
        {tool.description ? (
          <div className="line-clamp-2 min-w-0 whitespace-pre-wrap break-words text-xs leading-5 text-muted-foreground">
            {tool.description}
          </div>
        ) : null}
        {tool.inputSchema ? (
          <details className="group min-w-0">
            <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
              {t("apps.mcpInputSchema")}
            </summary>
            <pre className="mt-1 max-h-44 max-w-full overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/35 p-2 text-[11px] leading-4 text-muted-foreground">
              {formatMCPInputSchema(tool.inputSchema)}
            </pre>
          </details>
        ) : null}
      </div>
    </div>
  );
}

function AppToolGlyph({ kind, name }: { kind: AppToolItem["kind"]; name: string }) {
  const Icon = appToolIcon(name, kind);
  return (
    <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
      <Icon className="size-4" />
    </span>
  );
}

function appToolIcon(name: string, kind: AppToolItem["kind"]): LucideIcon {
  const icons: Record<string, LucideIcon> = {
    builtin_attachment_export: Download,
    builtin_browser_back: ArrowLeft,
    builtin_browser_click: MousePointerClick,
    builtin_browser_close: X,
    builtin_browser_forward: ArrowRight,
    builtin_browser_observe: FileSearch,
    builtin_browser_open: Globe,
    builtin_browser_reload: RotateCw,
    builtin_browser_screenshot: Camera,
    builtin_browser_scroll: MoveVertical,
    builtin_browser_status: PanelsTopLeft,
    builtin_browser_type: Keyboard,
    builtin_camera_capture: Camera,
    builtin_code_definition: SearchCode,
    builtin_code_diagnostics: CircleAlert,
    builtin_code_references: Share2,
    builtin_code_rename: Pencil,
    builtin_code_symbols: Braces,
    builtin_desktop_screenshot: ScanLine,
    builtin_file_copy: Copy,
    builtin_file_delete: FileX,
    builtin_file_list: ListTree,
    builtin_file_move: FolderInput,
    builtin_file_patch: FileDiff,
    builtin_file_read: FileText,
    builtin_file_search: FileSearch,
    builtin_file_slice: FileText,
    builtin_file_stat: FileCog,
    builtin_file_write: FilePenLine,
    builtin_git_commit: GitCommitHorizontal,
    builtin_git_diff: GitCompareArrows,
    builtin_git_log: History,
    builtin_git_stage: ListPlus,
    builtin_git_status: GitBranch,
    builtin_git_unstage: ListMinus,
    builtin_plan_update: ListChecks,
    builtin_app_save: PackageCheck,
    builtin_skill_validate: BadgeCheck,
  };
  if (icons[name]) {
    return icons[name];
  }
  if (name.startsWith("builtin_browser_")) {
    return Globe;
  }
  if (name.startsWith("builtin_command_")) {
    return SquareTerminal;
  }
  if (name.startsWith("canvas_")) {
    return PanelsTopLeft;
  }
  return kind === "api" ? Route : Wrench;
}

function appToolDisplayName(name: string, t: I18nTranslate) {
  const labels: Record<string, string> = {
    builtin_browser_status: t("transcript.toolBrowserStatus"),
    builtin_browser_open: t("transcript.toolBrowserOpen"),
    builtin_browser_observe: t("transcript.toolBrowserObserve"),
    builtin_browser_screenshot: t("transcript.toolBrowserScreenshot"),
    builtin_browser_back: t("transcript.toolBrowserBack"),
    builtin_browser_forward: t("transcript.toolBrowserForward"),
    builtin_browser_reload: t("transcript.toolBrowserReload"),
    builtin_browser_close: t("transcript.toolBrowserClose"),
    builtin_browser_click: t("transcript.toolBrowserClick"),
    builtin_browser_type: t("transcript.toolBrowserType"),
    builtin_browser_scroll: t("transcript.toolBrowserScroll"),
    builtin_camera_capture: t("transcript.toolCameraCapture"),
    builtin_desktop_screenshot: t("transcript.toolDesktopScreenshot"),
    builtin_command_session: t("transcript.toolCommandSession"),
    builtin_app_save: t("transcript.toolAppSave"),
    builtin_skill_validate: t("transcript.toolSkillValidate"),
    builtin_code_symbols: t("transcript.toolCodeSymbols"),
    builtin_code_definition: t("transcript.toolCodeDefinition"),
    builtin_code_references: t("transcript.toolCodeReferences"),
    builtin_code_diagnostics: t("transcript.toolCodeDiagnostics"),
    builtin_code_rename: t("transcript.toolCodeRename"),
    builtin_file_list: t("transcript.toolFileList"),
    builtin_file_stat: t("transcript.toolFileStat"),
    builtin_file_search: t("transcript.toolFileSearch"),
    builtin_file_slice: t("transcript.toolFileSlice"),
    builtin_file_write: t("transcript.toolFileWrite"),
    builtin_file_patch: t("transcript.toolFilePatch"),
    builtin_file_read: t("transcript.toolFileRead"),
    builtin_attachment_export: t("transcript.toolAttachmentExport"),
    builtin_file_delete: t("transcript.toolFileDelete"),
    builtin_file_move: t("transcript.toolFileMove"),
    builtin_file_copy: t("transcript.toolFileCopy"),
    builtin_git_status: t("transcript.toolGitStatus"),
    builtin_git_diff: t("transcript.toolGitDiff"),
    builtin_git_log: t("transcript.toolGitLog"),
    builtin_git_stage: t("transcript.toolGitStage"),
    builtin_git_unstage: t("transcript.toolGitUnstage"),
    builtin_git_commit: t("transcript.toolGitCommit"),
    builtin_plan_update: t("transcript.toolPlanUpdate"),
    builtin_rest_request: t("apps.tool.restRequest"),
    builtin_graphql_request: t("apps.tool.graphqlRequest"),
    builtin_graphql_introspect: t("apps.tool.graphqlIntrospect"),
    builtin_graphql_search: t("apps.tool.graphqlSearch"),
    canvas_chart: t("transcript.toolCanvasChart"),
    canvas_doc_read: t("transcript.toolCanvasDocRead"),
    canvas_gallery: t("transcript.toolCanvasGallery"),
    canvas_grid: t("transcript.toolCanvasGrid"),
    canvas_grid_patch: t("transcript.toolCanvasGridPatch"),
    canvas_item_clear: t("transcript.toolCanvasItemClear"),
    canvas_item_inspect: t("transcript.toolCanvasItemInspect"),
    canvas_item_list: t("transcript.toolCanvasItemList"),
    canvas_item_remove: t("transcript.toolCanvasItemRemove"),
    canvas_markdown: t("transcript.toolCanvasMarkdown"),
    canvas_table: t("transcript.toolCanvasTable"),
    canvas_timeline: t("transcript.toolCanvasTimeline"),
  };
  if (labels[name]) {
    return labels[name];
  }
  const readable = name.replace(/^builtin_/, "").replace(/^app_mcp__/, "").replace(/__[^_]+$/, "").replaceAll("_", " ").trim();
  return readable ? readable.charAt(0).toUpperCase() + readable.slice(1) : name;
}

function AppSkillsSection({
  app,
  children,
  count,
  icon,
  iconSrc,
  onSkillSelect,
  skills,
}: {
  app?: AppDefinition;
  children?: ReactNode;
  count?: number;
  icon?: AppIconSpec;
  iconSrc?: string;
  onSkillSelect?: (skill: AppSkillItem) => void;
  skills: AppSkillItems;
}) {
  const { t } = useI18n();
  return (
    <DetailSection title={t("apps.skills")} count={count ?? skills.length}>
      {children ?? <SkillRows app={app} icon={icon} iconSrc={iconSrc} skills={skills} onSkillSelect={onSkillSelect} />}
    </DetailSection>
  );
}

function SkillRows({
  app,
  icon,
  iconSrc,
  onSkillSelect,
  skills,
}: {
  app?: AppDefinition;
  icon?: AppIconSpec;
  iconSrc?: string;
  onSkillSelect?: (skill: AppSkillItem) => void;
  skills: AppSkillItems;
}) {
  const { t } = useI18n();
  if (skills.length === 0) {
    return <EmptyLine>{t("apps.none")}</EmptyLine>;
  }
  return (
    <ItemGroup className="gap-0 overflow-hidden rounded-lg border border-border/70 bg-card">
      {skills.map((skill, index) => (
        <Item
          key={skill.path}
          asChild
          className="relative items-start gap-3 rounded-none border-0 px-3 py-2.5 hover:bg-interactive-hover active:bg-interactive-pressed"
        >
          <button type="button" onClick={() => onSkillSelect?.(skill)}>
            {index > 0 ? <span aria-hidden="true" className="absolute inset-x-3 top-0 border-t border-border/70" /> : null}
            <ItemMedia>
              {app ? <AppIdentityIcon app={app} icon={icon} iconSrc={iconSrc} size="md" /> : <AppIcon icon={icon} size="md" src={iconSrc} />}
            </ItemMedia>
            <ItemContent className="min-w-0 gap-1">
              <ItemTitle className="flex max-w-full flex-wrap items-center gap-2">
                <span className="min-w-0 truncate text-sm font-medium">
                  {skillDetailName(skill.name || skill.id || skill.path)}
                </span>
              </ItemTitle>
              <ItemDescription className="line-clamp-2 text-xs leading-5">{skill.description || skill.path}</ItemDescription>
            </ItemContent>
          </button>
        </Item>
      ))}
    </ItemGroup>
  );
}

function SkillDetailDialog({
  appID,
  appSource,
  failed,
  icon,
  iconSrc,
  loading,
  open,
  skill,
  onOpenChange,
}: {
  appID?: string;
  appSource?: AppDefinition["source"];
  failed: boolean;
  icon?: AppIconSpec;
  iconSrc?: string;
  loading: boolean;
  open: boolean;
  skill: AppSkillDetail | AppSkillItem | null;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const content = stripSkillFrontmatter(skill?.content || "");
  const title = skillDetailName(skill?.name || skill?.id || skill?.path || "");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[82vh] max-h-[760px] flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader className="shrink-0">
          <div className="mb-3">
            {appSource === "builtin" && appID ? (
              <BuiltinAppIcon appID={appID} size="xl" />
            ) : (
              <AppIcon className="size-10" icon={icon} size="lg" src={iconSrc} />
            )}
          </div>
          <DialogTitle className="flex min-w-0 flex-wrap items-baseline gap-2 text-xl">
            <span className="truncate">{title}</span>
            <span className="text-sm font-medium text-muted-foreground">Skill</span>
          </DialogTitle>
          {skill?.description ? <DialogDescription className="text-sm">{skill.description}</DialogDescription> : null}
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-auto rounded-lg bg-black/[0.04] px-4 py-3 text-sm leading-6 dark:bg-black/25">
          {loading ? (
            <div className="grid gap-2">
              <Skeleton className="h-5 w-2/3" />
              <Skeleton className="h-5 w-full" />
              <Skeleton className="h-5 w-5/6" />
              <Skeleton className="h-28 w-full" />
            </div>
          ) : failed ? (
            <div className="text-sm text-muted-foreground">{t("apps.skillLoadFailed")}</div>
          ) : content ? (
            <div className="pudding-markdown">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
            </div>
          ) : (
            <div className="text-sm text-muted-foreground">{t("apps.none")}</div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function stripSkillFrontmatter(content: string) {
  return content.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, "").trim();
}

function DetailSection({ action, title, count, children }: { action?: ReactNode; title: string; count?: number; children: ReactNode }) {
  return (
    <section className="grid min-w-0 gap-3">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <div className="flex min-w-0 items-baseline gap-2">
          <h3 className="text-base font-semibold tracking-normal">{title}</h3>
          {typeof count === "number" ? <span className="text-xs text-muted-foreground">{count}</span> : null}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
      <div className="grid min-w-0 gap-2">{children}</div>
    </section>
  );
}

function DetailRow({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("grid min-w-0 gap-1 rounded-md bg-muted/35 px-3 py-2.5", className)}>{children}</div>;
}

function EmptyLine({ children }: { children: ReactNode }) {
  return <div className="rounded-md bg-muted/25 px-3 py-2.5 text-sm text-muted-foreground">{children}</div>;
}

function ContentLoadFailed({ error }: { error?: unknown }) {
  const { t } = useI18n();
  const detail = appContentErrorDetail(error, t);
  return (
    <EmptyLine>
      <div className="grid gap-1">
        <span>{t("apps.contentLoadFailed")}</span>
        {detail ? <span className="text-xs">{detail}</span> : null}
      </div>
    </EmptyLine>
  );
}

function appContentErrorDetail(error: unknown, t: (key: string) => string) {
  if (!error) {
    return "";
  }
  const message = error instanceof Error ? error.message : String(error);
  const packageStatus = message.match(/app package request failed: (\d+)/);
  if (packageStatus) {
    return `${t("apps.contentLoadPackageFailed")} (${packageStatus[1]})`;
  }
  if (message.includes("app package hash mismatch")) {
    return t("apps.contentLoadHashMismatch");
  }
  if (message.includes("JSON") || message.includes("parse")) {
    return t("apps.contentLoadParseFailed");
  }
  return message;
}

function DetailSkeletonRows() {
  return (
    <div className="grid gap-2">
      <Skeleton className="h-16 rounded-md" />
      <Skeleton className="h-16 rounded-md" />
    </div>
  );
}

function ConnectionRow({
  authMethods,
  connection,
  divided,
  onDelete,
  onEdit,
}: {
  authMethods: AppAuthMethod[];
  connection: AppConnection;
  divided: boolean;
  onDelete: (connection: AppConnection) => void;
  onEdit: (connection: AppConnection) => void;
}) {
  const { t } = useI18n();
  const name = connection.name || connection.id || t("apps.connection");
  const authLabel = connectionAuthBadgeLabel(connection, authMethods, t);
  return (
    <div className="relative flex min-w-0 items-center gap-3 px-3 py-2.5">
      {divided ? <span aria-hidden="true" className="absolute inset-x-3 top-0 border-t border-border/70" /> : null}
      <KeyRound className="size-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium">{name}</span>
          {authLabel ? (
            <Badge className={cn(connection.tokenSet && !connection.reauthorizationRequired && "text-success")} variant="outline">
              {connection.reauthorizationRequired ? t("apps.reauthorizationRequired") : authLabel}
            </Badge>
          ) : null}
        </div>
        {connection.account?.login ? <div className="truncate text-xs text-muted-foreground">@{connection.account.login}</div> : connection.header ? <div className="truncate text-xs text-muted-foreground">{connection.header}</div> : null}
      </div>
      <Button aria-label={t("apps.editConnection")} className="size-7 shrink-0" size="icon-xs" type="button" variant="ghost" onClick={() => onEdit(connection)}>
        <Pencil className="size-3.5" />
      </Button>
      <Button aria-label={t("common.delete")} className="size-7 shrink-0 text-destructive hover:text-destructive" size="icon-xs" type="button" variant="ghost" onClick={() => onDelete(connection)}>
        <Trash className="size-3.5" />
      </Button>
    </div>
  );
}

function ConnectionDialog({
  connections,
  editing,
  token,
  onOpenChange,
}: {
  connections: AppConnection[];
  editing: { app: AppDefinition; connection?: AppConnection } | null;
  token: string;
  onOpenChange: (open: boolean) => void;
}) {
  const { locale, t } = useI18n();
  const queryClient = useQueryClient();
  const app = editing?.app;
  const connection = editing?.connection;
  const editingAppID = editing?.app.id || "";
  const editingAppAuth = editing?.app.auth;
  const editingConnectionID = editing?.connection?.id || "";
  const editingConnectionName = editing?.connection?.name || "";
  const editingConnectionAuthMethodID = editing?.connection?.authMethodID || "";
  const editingConnectionAuthType = editing?.connection?.authType || "";
  const editingConnectionHeader = editing?.connection?.header || "";
  const editingAppName = editing?.app.name || "";
  const editingAppConnection = editing?.app.connection;
  const editingAppEndpoints = editing?.app.endpoints;
  const connectionFields = useMemo(() => appConnectionFields(app), [app]);
  const configurableEndpoints = useMemo(() => appConfigurableEndpoints(app), [app]);
  const [form, setForm] = useState<ConnectionForm>(emptyConnectionForm());
  const [secretVisible, setSecretVisible] = useState(false);
  const [secretLoading, setSecretLoading] = useState(false);
  const authMethods = useMemo(() => appAuthMethods(app), [app]);
  const selectedAuthMethod = findAppAuthMethod(authMethods, form.authMethodID, form.authType) || appConnectionOnlyAuthMethod(app, authMethods, connectionFields);
  const saveMutation = useMutation({
    mutationFn: () => {
      if (!app) {
        throw new Error("app required");
      }
      if (!selectedAuthMethod) {
        throw new Error("auth method required");
      }
      return putAppConnection(token, form.id.trim(), {
        appID: app.id,
        name: form.name.trim(),
        authMethodID: selectedAuthMethod.id,
        authType: normalizeAuthType(selectedAuthMethod.type),
        token: form.token,
        prefix: form.prefix,
        header: form.header,
        username: form.username,
        password: form.password,
        fields: form.fields,
        endpointURLs: form.endpointURLs,
      });
    },
    onSuccess: async (saved) => {
      toast.success(t("apps.connectionSaved"));
      onOpenChange(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appMCPStatus(saved.appID) }),
      ]);
    },
    onError: () => toast.error(t("apps.connectionSaveFailed")),
  });
  const startOAuthMutation = useMutation({
    mutationFn: async () => {
      if (!app) {
        throw new Error("app required");
      }
      if (!selectedAuthMethod) {
        throw new Error("auth method required");
      }
      const result = await startAppOAuth(token, {
        appID: app.id,
        authMethodID: selectedAuthMethod.id,
        connectionID: form.id.trim(),
        connectionName: form.name.trim(),
        fields: form.fields,
        endpointURLs: form.endpointURLs,
      });
      await openExternalURL(result.authorizationURL);
      return result;
    },
    onSuccess: () => {
      onOpenChange(false);
    },
    onError: () => toast.error(t("apps.oauthStartFailed")),
  });

  useEffect(() => {
    let cancelled = false;
    if (!editingAppID) {
      return;
    }
    setSecretVisible(false);
    const methods = appAuthMethods({ auth: editingAppAuth });
    const initialMethod =
      findAppAuthMethod(methods, editingConnectionAuthMethodID, editingConnectionAuthType) ||
      defaultAppAuthMethod(methods) ||
      appConnectionOnlyAuthMethod(
        { auth: editingAppAuth, connection: editingAppConnection, endpoints: editingAppEndpoints },
        methods,
        appConnectionFields({ connection: editingAppConnection }),
      );
    setForm({
      id: editingConnectionID || nextConnectionID(editingAppID, connections),
      name: editingConnectionName || nextConnectionName(editingAppName || editingAppID, editingAppID, connections),
      authMethodID: initialMethod?.id || "",
      authType: normalizeAuthType(initialMethod?.type || editingConnectionAuthType),
      fields: emptyConnectionFields(editingAppConnection),
      endpointURLs: emptyConnectionEndpointURLs(editingAppEndpoints),
      token: "",
      prefix: initialMethod?.prefix || "",
      header: editingConnectionHeader || initialMethod?.header || "",
      username: "",
      password: "",
    });
    if (!editingConnectionID) {
      setSecretLoading(false);
      return;
    }
    setSecretLoading(true);
    getAppConnection(token, editingConnectionID)
      .then((detail) => {
        if (cancelled) {
          return;
        }
        const detailMethod = findAppAuthMethod(methods, detail.authMethodID, detail.authType) || initialMethod;
        setForm((current) => ({
          ...current,
          name: detail.name || current.name,
          authMethodID: detailMethod?.id || detail.authMethodID || current.authMethodID,
          authType: normalizeAuthType(detailMethod?.type || detail.authType),
          token: detail.token || "",
          prefix: detail.prefix || detailMethod?.prefix || current.prefix,
          header: detail.header || detailMethod?.header || current.header,
          username: detail.username || "",
          password: detail.password || "",
          fields: emptyConnectionFields(editingAppConnection, detail.fields),
          endpointURLs: emptyConnectionEndpointURLs(editingAppEndpoints, detail.endpointURLs),
        }));
      })
      .catch(() => {
        if (!cancelled) {
          toast.error(translate("apps.connectionLoadFailed", locale));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setSecretLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [
    connections,
    editingAppAuth,
    editingAppID,
    editingAppName,
    editingConnectionAuthMethodID,
    editingConnectionAuthType,
    editingConnectionHeader,
    editingConnectionID,
    editingConnectionName,
    editingAppConnection,
    editingAppEndpoints,
    locale,
    token,
  ]);

  if (!editing || !app) {
    return null;
  }

  const isOAuth = selectedAuthMethod?.type === "oauth2";
  const canReuseExistingSecret = Boolean(connection?.tokenSet && sameConnectionAuthMethod(connection, selectedAuthMethod));
  const tokenReady = !["bearer", "token", "header"].includes(form.authType) || form.token.trim() !== "" || canReuseExistingSecret;
  const basicReady = form.authType !== "basic" || form.username.trim() !== "" || form.password !== "" || canReuseExistingSecret;
  const headerReady = form.authType !== "header" || form.header.trim() !== "";
  const fieldsReady = connectionFields.every((field) => !field.required || form.fields[field.id]?.trim());
  const endpointURLsReady = configurableEndpoints.every(({ name, endpoint }) =>
    isValidEndpointURL(form.endpointURLs[name], endpoint.urlConfig?.required === true),
  );
  const canSave =
    Boolean(selectedAuthMethod) &&
    !isOAuth &&
    form.id.trim() !== "" &&
    form.name.trim() !== "" &&
    tokenReady &&
    basicReady &&
    headerReady &&
    fieldsReady &&
    endpointURLsReady &&
    !saveMutation.isPending &&
    !secretLoading;
  const canStartOAuth =
    Boolean(selectedAuthMethod) &&
    isOAuth &&
    form.id.trim() !== "" &&
    form.name.trim() !== "" &&
    fieldsReady &&
    endpointURLsReady &&
    !startOAuthMutation.isPending &&
    !secretLoading;

  return (
    <Dialog open={Boolean(editing)} onOpenChange={saveMutation.isPending || startOAuthMutation.isPending ? undefined : onOpenChange}>
      <DialogContent
        className="sm:max-w-lg"
        onPointerDownOutside={(event) => {
          if (shouldKeepDialogOpenForSelectDismiss(event.target)) {
            event.preventDefault();
          }
        }}
        onInteractOutside={(event) => {
          if (shouldKeepDialogOpenForSelectDismiss(event.target)) {
            event.preventDefault();
          }
        }}
      >
        <DialogHeader>
          <DialogTitle>{connection ? t("apps.editConnection") : t("apps.addConnection")}</DialogTitle>
          <DialogDescription>{app.name}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3">
          <LabeledInput
            label={t("apps.connectionName")}
            value={form.name}
            onChange={(value) => setForm((current) => ({ ...current, name: value }))}
          />
          {configurableEndpoints.map(({ name, endpoint }) => (
            <LabeledInput
              key={name}
              description={endpointURLFieldDescription(endpoint, t)}
              label={`${endpoint.urlConfig?.label || name}${endpoint.urlConfig?.required ? " *" : ""}`}
              placeholder={endpoint.urlConfig?.placeholder || endpoint.url}
              value={form.endpointURLs[name] || ""}
              onChange={(value) =>
                setForm((current) => ({
                  ...current,
                  endpointURLs: { ...current.endpointURLs, [name]: value },
                }))
              }
            />
          ))}
          {authMethods.length > 0 ? (
            <div className="grid gap-1.5">
              <Label>{t("apps.authType")}</Label>
              <Select
                value={selectedAuthMethod?.id || form.authMethodID}
                onValueChange={(value) => {
                  const method = authMethods.find((item) => item.id === value);
                  if (!method) {
                    return;
                  }
                  setForm((current) => ({
                    ...current,
                    authMethodID: method.id || "",
                    authType: normalizeAuthType(method.type),
                    header: method.header || "",
                    password: "",
                    prefix: method.prefix || "",
                    token: "",
                    username: "",
                  }));
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <DialogSelectContent>
                  {authMethods.map((method) => (
                    <SelectItem key={method.id} value={method.id || method.type}>
                      {appAuthMethodLabel(method, t)}
                    </SelectItem>
                  ))}
                </DialogSelectContent>
              </Select>
            </div>
          ) : null}
          {authMethods.length === 0 && app.auth?.required ? <div className="text-sm text-destructive">{t("apps.authUnavailable")}</div> : null}
          {form.authType === "token" ? (
            <LabeledInput
              label={t("apps.prefix")}
              placeholder="Token"
              value={form.prefix}
              onChange={(value) => setForm((current) => ({ ...current, prefix: value }))}
            />
          ) : null}
          {form.authType === "header" ? (
            <LabeledInput
              label={t("apps.header")}
              placeholder="X-API-Key"
              value={form.header}
              onChange={(value) => setForm((current) => ({ ...current, header: value }))}
            />
          ) : null}
          {form.authType === "basic" ? (
            <>
              <LabeledInput
                label={t("apps.username")}
                value={form.username}
                onChange={(value) => setForm((current) => ({ ...current, username: value }))}
              />
              <LabeledSecretInput
                label={t("apps.password")}
                placeholder={canReuseExistingSecret ? t("apps.secretKeepPlaceholder") : undefined}
                disabled={secretLoading}
                visible={secretVisible}
                value={form.password}
                onVisibleChange={setSecretVisible}
                onChange={(value) => setForm((current) => ({ ...current, password: value }))}
              />
            </>
          ) : null}
          {["bearer", "token", "header"].includes(form.authType) ? (
            <LabeledSecretInput
              label={t("apps.token")}
              placeholder={canReuseExistingSecret ? t("apps.secretKeepPlaceholder") : undefined}
              disabled={secretLoading}
              visible={secretVisible}
              value={form.token}
              onVisibleChange={setSecretVisible}
              onChange={(value) => setForm((current) => ({ ...current, token: value }))}
            />
          ) : null}
          {connectionFields.map((field) => {
            const label = `${field.label || field.id}${field.required ? " *" : ""}`;
            const description = connectionFieldDescription(field, t);
            const value = form.fields[field.id] || "";
            const onFieldChange = (next: string) =>
              setForm((current) => ({
                ...current,
                fields: { ...current.fields, [field.id]: next },
              }));
            return field.secret ? (
              <LabeledSecretInput
                key={field.id}
                description={description}
                disabled={secretLoading}
                label={label}
                placeholder={field.placeholder}
                value={value}
                visible={secretVisible}
                onChange={onFieldChange}
                onVisibleChange={setSecretVisible}
              />
            ) : (
              <LabeledInput
                key={field.id}
                description={description}
                label={label}
                placeholder={field.placeholder}
                value={value}
                onChange={onFieldChange}
              />
            );
          })}
          {isOAuth ? <div className="text-sm text-muted-foreground">{t("apps.oauthConnectionHint")}</div> : null}
        </div>
        <DialogFooter>
          <Button disabled={saveMutation.isPending || startOAuthMutation.isPending} type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          {isOAuth ? (
            <Button disabled={!canStartOAuth} type="button" onClick={() => startOAuthMutation.mutate()}>
              {startOAuthMutation.isPending ? <Spinner /> : null}
              {connection ? t("apps.reauthorizeConnection") : t("apps.authorizeConnection")}
            </Button>
          ) : (
            <Button disabled={!canSave} type="button" onClick={() => saveMutation.mutate()}>
              {saveMutation.isPending ? <Spinner /> : null}
              {t("common.save")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function LabeledInput({
  description,
  disabled,
  label,
  onChange,
  placeholder,
  type = "text",
  value,
}: {
  description?: string;
  disabled?: boolean;
  label: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  value: string;
}) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      <Input disabled={disabled} placeholder={placeholder} type={type} value={value} onChange={(event) => onChange(event.target.value)} />
      {description ? <div className="whitespace-pre-line text-xs text-muted-foreground">{description}</div> : null}
    </div>
  );
}

function LabeledSecretInput({
  description,
  label,
  disabled,
  onChange,
  onVisibleChange,
  placeholder,
  value,
  visible,
}: {
  description?: string;
  disabled?: boolean;
  label: string;
  onChange: (value: string) => void;
  onVisibleChange: (visible: boolean) => void;
  placeholder?: string;
  value: string;
  visible: boolean;
}) {
  const { t } = useI18n();

  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      <div className="relative">
        <Input
          className="pr-9"
          disabled={disabled}
          placeholder={placeholder}
          type={visible ? "text" : "password"}
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
        <button
          aria-label={visible ? t("apps.hideSecret") : t("apps.showSecret")}
          className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none disabled:opacity-50"
          disabled={disabled}
          type="button"
          onClick={() => onVisibleChange(!visible)}
        >
          {visible ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
        </button>
      </div>
      {description ? <div className="whitespace-pre-line text-xs text-muted-foreground">{description}</div> : null}
    </div>
  );
}

function emptyConnectionForm(): ConnectionForm {
  return {
    authMethodID: "",
    authType: "none",
    endpointURLs: {},
    fields: {},
    header: "",
    id: "",
    name: "",
    password: "",
    prefix: "",
    token: "",
    username: "",
  };
}

function appConnectionFields(app?: Pick<AppDefinition, "connection"> | null): AppConnectionField[] {
  return app?.connection?.fields || [];
}

function appConfigurableEndpoints(app?: Pick<AppDefinition, "endpoints"> | null) {
  return Object.entries(app?.endpoints || {})
    .filter(
      ([, endpoint]) =>
        (endpoint.kind === "rest" || endpoint.kind === "graphql") &&
        Boolean(endpoint.url?.trim()) &&
        Boolean(endpoint.urlConfig),
    )
    .map(([name, endpoint]) => ({ name, endpoint }));
}

function emptyConnectionEndpointURLs(
  endpoints?: AppDefinition["endpoints"],
  values?: Record<string, string>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const { name } of appConfigurableEndpoints({ endpoints })) {
    out[name] = values?.[name] || "";
  }
  return out;
}

function isValidEndpointURL(value: string | undefined, required: boolean) {
  const raw = value?.trim();
  if (!raw) {
    return !required;
  }
  try {
    const parsed = new URL(raw);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && Boolean(parsed.host) && !parsed.username && !parsed.password && !parsed.search && !parsed.hash;
  } catch {
    return false;
  }
}

function endpointURLFieldDescription(
  endpoint: AppEndpoints[string],
  t: (key: string) => string,
) {
  const lines = [endpoint.urlConfig?.description?.trim()];
  if (!endpoint.urlConfig?.required && endpoint.url) {
    lines.push(`${t("apps.defaultEndpointURL")}: ${endpoint.url}`);
  }
  return lines.filter(Boolean).join("\n");
}

function connectionFieldDescription(field: AppConnectionField, t: (key: string) => string) {
  const lines = [field.description?.trim(), connectionFieldInjectionDescription(field, t)].filter(Boolean);
  return lines.join("\n");
}

function connectionFieldInjectionDescription(field: AppConnectionField, t: (key: string) => string) {
  if (!field.inject?.length) {
    return "";
  }
  const targets = field.inject.map((rule) => {
    const target = connectionFieldInjectTargetLabel(rule.target, t);
    const name = rule.name || field.id;
    const methods = rule.methods?.length ? ` (${rule.methods.join(", ")})` : "";
    return `${target}.${name}${methods}`;
  });
  return `${t("apps.fieldInjectsInto")}: ${targets.join(", ")}`;
}

function connectionFieldInjectTargetLabel(target: string, t: (key: string) => string) {
  switch (target) {
    case "body":
      return t("apps.fieldInjectTarget.body");
    case "env":
      return t("apps.fieldInjectTarget.env");
    case "header":
      return t("apps.fieldInjectTarget.header");
    case "query":
      return t("apps.fieldInjectTarget.query");
    default:
      return target;
  }
}

function emptyConnectionFields(config?: AppDefinition["connection"], values?: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const field of config?.fields || []) {
    out[field.id] = values?.[field.id] || "";
  }
  return out;
}

function nextConnectionName(appName: string, appID: string, connections: AppConnection[]) {
  const existing = new Set(
    connections
      .filter((connection) => connection.appID === appID)
      .map((connection) => (connection.name || "").trim().toLowerCase())
      .filter(Boolean),
  );
  const base = appName.trim() || "Connection";
  if (!existing.has(base.toLowerCase())) {
    return base;
  }
  for (let index = 2; ; index += 1) {
    const name = `${base} ${index}`;
    if (!existing.has(name.toLowerCase())) {
      return name;
    }
  }
}

function nextConnectionID(appID: string, connections: AppConnection[]) {
  const used = new Set(connections.map((connection) => connection.id));
  const base = `${appID}-main`;
  if (!used.has(base)) {
    return base;
  }
  for (let index = 2; ; index += 1) {
    const id = `${appID}-${index}`;
    if (!used.has(id)) {
      return id;
    }
  }
}

function normalizeAuthType(value: unknown): AuthType {
  return authTypes.includes(value as AuthType) ? (value as AuthType) : "none";
}

function appAuthMethods(app?: Pick<AppDefinition, "auth"> | null): AppAuthMethod[] {
  const methods = app?.auth?.methods || [];
  return methods
    .map((method) => {
      const type = normalizeAuthType(method.type);
      const id = (method.id || method.type || type).trim();
      return {
        ...method,
        id,
        type,
        header: method.header?.trim(),
        label: method.label?.trim(),
        prefix: method.prefix?.trim(),
        provider: method.provider?.trim(),
      };
    })
    .filter((method) => method.id && method.type);
}

function appConnectionOnlyAuthMethod(
  app: Pick<AppDefinition, "auth" | "connection" | "endpoints"> | null | undefined,
  methods: AppAuthMethod[],
  fields: AppConnectionField[],
): AppAuthMethod | undefined {
  if (app?.auth?.required || methods.length > 0 || (fields.length === 0 && appConfigurableEndpoints(app).length === 0)) {
    return undefined;
  }
  return { id: "", type: "none" } as AppAuthMethod;
}

function appCanManageConnections(app?: Pick<AppDefinition, "auth" | "connection" | "endpoints"> | null) {
  const authMethods = appAuthMethods(app).filter((method) => normalizeAuthType(method.type) !== "none");
  return authMethods.length > 0 || appConnectionFields(app).length > 0 || appConfigurableEndpoints(app).length > 0 || app?.auth?.required === true;
}

function groupMCPStatusByEndpoint(statuses: AppMCPEndpointStatus[]) {
  const out = new Map<string, AppMCPEndpointStatus[]>();
  for (const status of statuses) {
    const endpointName = status.endpointName?.trim();
    if (!endpointName) {
      continue;
    }
    out.set(endpointName, [...(out.get(endpointName) || []), status]);
  }
  return out;
}

function defaultAppAuthMethod(methods: AppAuthMethod[]) {
  return methods.find((method) => method.default) || methods[0];
}

function findAppAuthMethod(methods: AppAuthMethod[], methodID?: string, authType?: string) {
  const id = (methodID || "").trim();
  if (id) {
    return methods.find((method) => method.id === id);
  }
  const type = normalizeAuthType(authType);
  const matched = methods.filter((method) => method.type === type);
  if (matched.length === 1) {
    return matched[0];
  }
  return defaultAppAuthMethod(methods);
}

function appAuthMethodLabel(method: AppAuthMethod, t: (key: string) => string) {
  return method.label || authTypeLabel(normalizeAuthType(method.type), t);
}

function connectionAuthBadgeLabel(connection: AppConnection, methods: AppAuthMethod[], t: (key: string) => string) {
  const method = findConnectionAuthMethod(methods, connection.authMethodID, connection.authType);
  if (method && isPATAuthMethod(method)) {
    return "PAT";
  }
  const type = normalizeAuthType(method?.type || connection.authType);
  if (connection.appID === "github" && connection.authMethodID === "github-app") {
    return "GitHub App";
  }
  if (type === "none") {
    return "";
  }
  return authTypeLabel(type, t);
}

function findConnectionAuthMethod(methods: AppAuthMethod[], methodID?: string, authType?: string) {
  const id = (methodID || "").trim();
  if (id) {
    return methods.find((method) => method.id === id);
  }
  const rawType = (authType || "").trim();
  if (!rawType) {
    return undefined;
  }
  const type = normalizeAuthType(rawType);
  if (type === "none") {
    return undefined;
  }
  const matched = methods.filter((method) => method.type === type);
  return matched.length === 1 ? matched[0] : undefined;
}

function sameConnectionAuthMethod(connection: AppConnection, method?: AppAuthMethod) {
  if (!method) {
    return false;
  }
  const connectionMethodID = (connection.authMethodID || "").trim();
  const methodID = (method.id || "").trim();
  if (connectionMethodID && methodID) {
    return connectionMethodID === methodID;
  }
  return normalizeAuthType(connection.authType) === normalizeAuthType(method.type);
}

function isPATAuthMethod(method: AppAuthMethod) {
  const value = `${method.id || ""} ${method.label || ""}`.toLowerCase();
  return value.includes("pat") || value.includes("personal access token");
}

function authTypeLabel(type: AuthType, t: (key: string) => string) {
  return t(`apps.auth.${type}`);
}
