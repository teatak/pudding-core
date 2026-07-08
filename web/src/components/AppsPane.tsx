import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ChevronRight, CircleAlert, CircleCheck, CircleDashed, Download, Eye, EyeOff, KeyRound, Loader2, Package, Pencil, Plus, Settings2, Trash } from "lucide-react";
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
  getAppSkill,
  installAppPackage,
  listAppConnections,
  listApps,
  putAppConnection,
  putAppMCPOverride,
  startAppOAuth,
  type AppConnection,
  type AppConnectionPayload,
  type AppDefinition,
  type AppMCPEndpointStatus,
  type AppMCPOverride,
  type AppMCPTool,
  type AppSkillDetail,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppIcon, mergeAppIconSpec, type AppIconSpec } from "@/components/AppIcon";
import { DialogSelectContent } from "@/components/DialogSelectContent";
import { PageHeader } from "@/components/PageHeader";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
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
import { Textarea } from "@/components/ui/textarea";
import { translate, useI18n } from "@/i18n";
import { shouldKeepDialogOpenForSelectDismiss } from "@/lib/layerGuards";
import { cn } from "@/lib/utils";
import { useShowPreviewAppVersions } from "@/state/appCatalogPrefs";

type AuthType = AppConnectionPayload["authType"];
type LocalizedText = string | Record<string, string>;
type AppRegistryRelease = {
  version: string;
  manifest?: string;
  package: string;
  package_sha256?: string;
  requires?: Record<string, string>;
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
  manifest?: string;
  package?: string;
  package_sha256?: string;
  releases?: AppRegistryRelease[];
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
type CatalogAppContent = {
  endpoints: AppEndpoints;
  skills: AppSkillItems;
};
type SelectedSkill = {
  appID?: string;
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

const authTypes: AuthType[] = ["none", "bearer", "token", "basic", "header", "oauth2"];
const OFFICIAL_APP_REGISTRY =
  import.meta.env.VITE_PUDDING_APP_REGISTRY_URL ||
  "https://teatak.github.io/pudding-hub/apps/registry.json";
const APP_CATALOG_CACHE_TTL_MS = 5 * 60 * 1000;

export function AppsPane({ token }: { token: string }) {
  const { locale, t } = useI18n();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<{ app: AppDefinition; connection?: AppConnection } | null>(null);
  const [deleting, setDeleting] = useState<AppConnection | null>(null);
  const [uninstalling, setUninstalling] = useState<AppDefinition | null>(null);
  const [detailAppID, setDetailAppID] = useState<string | null>(null);
  const [detailCatalogID, setDetailCatalogID] = useState<string | null>(null);
  const [catalogReleaseByID, setCatalogReleaseByID] = useState<Record<string, string>>({});
  const showPreviewVersions = useShowPreviewAppVersions();
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
    () => [...(appsQuery.data?.apps || [])].sort((a, b) => a.name.localeCompare(b.name)),
    [appsQuery.data?.apps],
  );
  const installedByID = useMemo(() => new Map(apps.map((app) => [app.id, app])), [apps]);
  const catalogApps = useMemo(
    () =>
      [...(catalogQuery.data?.items || [])].sort((a, b) =>
        appRegistryTitle(a, locale).localeCompare(appRegistryTitle(b, locale)),
      ),
    [catalogQuery.data?.items, locale],
  );
  const catalogByLocalID = useMemo(
    () => new Map(catalogApps.map((app) => [appRegistryLocalID(app), app])),
    [catalogApps],
  );
  const connections = useMemo(() => connectionsQuery.data?.connections || [], [connectionsQuery.data?.connections]);
  const detailApp = apps.find((app) => app.id === detailAppID) || null;
  const detailCatalogForInstalled = detailApp ? catalogByLocalID.get(detailApp.id) : undefined;
  const detailCatalogApp = catalogApps.find((app) => app.id === detailCatalogID) || null;
  const detailCatalogReleases = detailCatalogApp ? appRegistryReleases(detailCatalogApp, showPreviewVersions) : [];
  const detailCatalogRelease =
    detailCatalogApp && detailCatalogReleases.length > 0
      ? detailCatalogReleases.find((release) => release.version === catalogReleaseByID[detailCatalogApp.id]) ||
        appRegistryDefaultRelease(detailCatalogApp, showPreviewVersions)
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
  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteAppConnection(token, id),
    onSuccess: async () => {
      toast.success(t("apps.connectionDeleted"));
      setDeleting(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() });
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
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.apps() }),
        queryClient.invalidateQueries({ queryKey: queryKeys.appCatalog() }),
      ]);
    },
    onError: () => toast.error(t("apps.uninstallFailed")),
  });
  return (
    <main className="flex h-full min-w-0 flex-col overflow-hidden bg-background">
      <PageHeader
        icon={
          detailApp || detailCatalogApp ? (
            <Button
              aria-label={t("common.back")}
              className="pudding-toolbar-icon-button"
              size="icon-xs"
              type="button"
              variant="ghost"
              onClick={() => {
                setDetailAppID(null);
                setDetailCatalogID(null);
              }}
            >
              <ArrowLeft className="size-4" />
            </Button>
          ) : (
            <Package className="size-4" />
          )
        }
        title={detailApp ? detailApp.name : detailCatalogApp ? appRegistryTitle(detailCatalogApp, locale) : t("apps.title")}
      />
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="mx-auto grid w-full max-w-5xl gap-8 px-6 pt-4 pb-10">
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
                  appName: detailCatalogForInstalled ? appRegistryTitle(detailCatalogForInstalled, locale) : detailApp.name,
                  icon,
                  iconSrc,
                  skill,
                })
              }
              onUninstall={() => setUninstalling(detailApp)}
            />
          ) : detailCatalogApp ? (
            <CatalogAppDetail
              app={detailCatalogApp}
              detail={catalogDetailQuery.data}
              detailError={catalogDetailQuery.error}
              detailFailed={catalogDetailQuery.isError}
              detailLoading={catalogDetailQuery.isLoading}
              installed={installedByID.get(appRegistryLocalID(detailCatalogApp))}
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
                  appName: appRegistryTitle(detailCatalogApp, locale),
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
              {apps.length > 0 ? (
                <section className="grid gap-4">
                  <div className="flex items-center justify-between border-b pb-4">
                    <h2 className="text-xl font-semibold tracking-normal">{t("apps.installedShort")}</h2>
                  </div>
                  <div className="flex min-w-0 flex-wrap gap-4">
                    {apps.map((app) => (
                      <InstalledAppTile
                        key={app.id}
                        app={app}
                        token={token}
                        onSelect={() => setDetailAppID(app.id)}
                      />
                    ))}
                  </div>
                </section>
              ) : null}
              <section className="grid gap-4">
                <div className="border-b pb-4">
                  <h2 className="text-lg font-semibold tracking-normal">{t("apps.availableTitle")}</h2>
                </div>
                {catalogQuery.isLoading ? (
                  <SectionSpinner />
                ) : catalogQuery.isError ? (
                  <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
                    {t("apps.loadFailed")}
                  </div>
                ) : catalogApps.length > 0 ? (
                  <div className="grid gap-x-12 gap-y-7 md:grid-cols-2">
                    {catalogApps.map((app) => {
                      const release = appRegistryDefaultRelease(app, false) || appRegistryDefaultRelease(app, showPreviewVersions);
                      const installed = installedByID.get(appRegistryLocalID(app));
                      if (!release) {
                        return null;
                      }
                      return (
                        <CatalogAppItem
                          key={app.id}
                          app={app}
                          installed={installed}
                          installing={
                            installMutation.isPending &&
                            installMutation.variables?.app.id === app.id &&
                            installMutation.variables.release.version === release.version
                          }
                          release={release}
                          showPreviewVersions={showPreviewVersions}
                          token={token}
                          onInstall={() => installMutation.mutate({ app, release })}
                          onSelect={() => {
                            if (installed && installedMatchesRelease(installed, release)) {
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
      <SkillDetailDialog
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
                  deleteMutation.mutate(deleting.id);
                }
              }}
            >
              {deleteMutation.isPending ? <Loader2 className="animate-spin" /> : <Trash />}
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
              {uninstallMutation.isPending ? <Loader2 className="animate-spin" /> : <Trash />}
              {t("apps.uninstall")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </main>
  );
}

function SectionTitle({ children }: { children: ReactNode }) {
  return <h2 className="border-b pb-4 text-lg font-semibold tracking-normal">{children}</h2>;
}

function SectionSpinner() {
  const { t } = useI18n();
  return (
    <div className="flex h-24 items-center justify-center text-muted-foreground">
      <Loader2 aria-label={t("common.loading")} className="size-4 animate-spin" />
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
    items: Array.isArray(data.items) ? data.items.filter(isAppRegistryItem) : [],
  };
}

async function fetchAppPackage(item: AppRegistryItem, registryURL: string, release?: AppRegistryRelease): Promise<string> {
  const target = release || appRegistryDefaultRelease(item, true);
  if (!target?.package) {
    throw new Error("app package is missing");
  }
  const packageURL = new URL(target.package, registryURL).href;
  const response = await fetch(packageURL, { cache: "reload" });
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
  for (const line of yamlSectionLines(yaml, "endpoints")) {
    const endpointMatch = line.match(/^ {2}([\w-]+):\s*$/);
    if (endpointMatch) {
      current = endpointMatch[1];
      endpoints[current] = { kind: "rest" };
      continue;
    }
    const propMatch = line.match(/^ {4}([\w-]+):\s*(.*)$/);
    if (!current || !propMatch) {
      continue;
    }
    const key = propMatch[1];
    const value = yamlScalar(propMatch[2]);
    if (key === "kind" && (value === "rest" || value === "graphql" || value === "mcp")) {
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

function appRegistryDefaultRelease(item: AppRegistryItem, includePreview: boolean): AppRegistryRelease | undefined {
  return appRegistryReleases(item, includePreview)[0];
}

function normalizedAppRegistryReleases(item: AppRegistryItem): AppRegistryRelease[] {
  const releases = (item.releases || []).map(normalizedAppRegistryRelease).filter((release): release is AppRegistryRelease => Boolean(release));
  if (item.package) {
    const topLevel: AppRegistryRelease = {
      version: item.version || "",
      manifest: item.manifest,
      package: item.package,
      package_sha256: item.package_sha256,
    };
    if (!releases.some((release) => release.version === topLevel.version && release.package === topLevel.package)) {
      releases.push(topLevel);
    }
  }
  return releases.sort(compareRegistryReleases);
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
  };
}

function compareRegistryReleases(a: AppRegistryRelease, b: AppRegistryRelease) {
  const aPreview = isPreviewRelease(a);
  const bPreview = isPreviewRelease(b);
  if (aPreview !== bPreview) {
    return aPreview ? 1 : -1;
  }
  return b.version.localeCompare(a.version, undefined, { numeric: true });
}

function appHasPreviewRelease(item: AppRegistryItem) {
  return normalizedAppRegistryReleases(item).some(isPreviewRelease);
}

function isPreviewRelease(release: AppRegistryRelease) {
  return release.preview === true;
}

function appRegistryTitle(item: AppRegistryItem, locale: string) {
  return localizedText(item.title, locale) || item.name || item.id;
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
  const cacheKey = item.package_sha256 || item.version || item.id;
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

function installedMatchesRelease(installed: AppDefinition | undefined, release: AppRegistryRelease) {
  if (!installed) {
    return false;
  }
  if (release.version && installed.version !== release.version) {
    return false;
  }
  const expectedSHA = release.package_sha256?.toLowerCase();
  return !expectedSHA || installed.packageSHA256?.toLowerCase() === expectedSHA;
}

function needsAppUpgrade(installed: AppDefinition, release: AppRegistryRelease) {
  const expectedSHA = release.package_sha256?.toLowerCase();
  if (expectedSHA && installed.packageSHA256?.toLowerCase() !== expectedSHA) {
    return true;
  }
  return Boolean(release.version && installed.version && release.version !== installed.version);
}

async function sha256Hex(text: string) {
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function InstalledAppTile({
  app,
  onSelect,
  token,
}: {
  app: AppDefinition;
  onSelect: () => void;
  token: string;
}) {
  const src = appIconURL(token, app);
  return (
    <button
      className="group grid w-20 justify-items-center gap-2 text-center"
      type="button"
      onClick={onSelect}
    >
      <AppIcon
        className="transition-transform group-hover:scale-105"
        icon={app.icon}
        size="2xl"
        src={src}
      />
      <div className="w-full truncate text-center text-xs text-muted-foreground">{app.name}</div>
    </button>
  );
}

function AppDetail({
  app,
  catalogApp,
  connections,
  onAdd,
  onDelete,
  onEdit,
  onSkillSelect,
  onUninstall,
  token,
}: {
  app: AppDefinition;
  catalogApp?: AppRegistryItem;
  connections: AppConnection[];
  onAdd: () => void;
  onDelete: (connection: AppConnection) => void;
  onEdit: (connection: AppConnection) => void;
  onSkillSelect: (skill: AppSkillItem, icon?: AppIconSpec, iconSrc?: string) => void;
  onUninstall: () => void;
  token: string;
}) {
  const { locale, t } = useI18n();
  const endpoints = Object.entries(app.endpoints || {}).sort(([a], [b]) => a.localeCompare(b));
  const skills = (app.skills || []) as AppSkillItems;
  const icon = mergeAppIconSpec(app.icon, catalogApp?.icon);
  const iconSrc = appIconURL(token, app) || (catalogApp ? appRegistryIconURL(catalogApp, OFFICIAL_APP_REGISTRY) : undefined);
  const title = catalogApp ? appRegistryTitle(catalogApp, locale) : app.name;
  const description = (catalogApp ? appRegistryDescription(catalogApp, locale) : "") || app.description;
  const authMethods = appAuthMethods(app);
  const canManageConnections = appCanManageConnections(app);
  const installedIsPreview = Boolean(
    app.version &&
      catalogApp &&
      normalizedAppRegistryReleases(catalogApp).some((release) => release.version === app.version && isPreviewRelease(release)),
  );
  const hasMCPEndpoints = endpoints.some(([, endpoint]) => endpoint.kind === "mcp");
  const mcpStatusQuery = useQuery({
    queryKey: queryKeys.appMCPStatus(app.id),
    queryFn: () => getAppMCPStatus(token, app.id),
    enabled: hasMCPEndpoints,
    retry: false,
    staleTime: 30_000,
  });
  const mcpStatusByEndpoint = useMemo(
    () => groupMCPStatusByEndpoint(mcpStatusQuery.data?.endpoints || []),
    [mcpStatusQuery.data?.endpoints],
  );

  return (
    <section className="grid gap-8">
      <div className="min-w-0 border-b pb-6">
        <div className="flex min-w-0 items-start gap-4">
          <AppIcon icon={icon} size="hero" src={iconSrc} />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-start justify-between gap-4">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <h2 className="truncate text-2xl font-semibold tracking-normal">{title}</h2>
                {app.version ? <Badge variant="outline">v{app.version}</Badge> : null}
                {installedIsPreview ? <Badge variant="secondary">{t("apps.previewVersion")}</Badge> : null}
                {catalogApp?.tags?.map((tag) => (
                  <Badge key={tag} variant="outline">
                    {tag}
                  </Badge>
                ))}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Button className="text-destructive hover:text-destructive" type="button" variant="ghost" onClick={onUninstall}>
                  <Trash className="size-3.5" />
                  {t("apps.uninstall")}
                </Button>
              </div>
            </div>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{description || t("apps.noDescription")}</p>
          </div>
        </div>
      </div>

      <div className="grid gap-8">
        {canManageConnections || connections.length > 0 ? (
          <DetailSection
            title={t("apps.connections")}
            count={connections.length}
            action={
              canManageConnections ? (
                <Button size="sm" type="button" variant="secondary" onClick={onAdd}>
                  <Plus className="size-3.5" />
                  {t("apps.addConnection")}
                </Button>
              ) : null
            }
          >
            {connections.length > 0 ? (
              connections.map((connection) => (
                <ConnectionRow
                  key={connection.id}
                  authMethods={authMethods}
                  connection={connection}
                  onDelete={onDelete}
                  onEdit={onEdit}
                />
              ))
            ) : (
              <EmptyLine>{t("apps.noConnections")}</EmptyLine>
            )}
          </DetailSection>
        ) : null}

        <AppEndpointsSection
          appID={app.id}
          endpoints={endpoints}
          mcpStatusByEndpoint={mcpStatusByEndpoint}
          mcpStatusFailed={mcpStatusQuery.isError}
          mcpStatusLoading={mcpStatusQuery.isLoading}
          token={token}
        />
        <AppSkillsSection icon={icon} iconSrc={iconSrc} skills={skills} onSkillSelect={(skill) => onSkillSelect(skill, icon, iconSrc)} />
      </div>
    </section>
  );
}

function CatalogAppItem({
  app,
  installed,
  installing,
  onInstall,
  onSelect,
  release,
  showPreviewVersions,
  token,
}: {
  app: AppRegistryItem;
  installed?: AppDefinition;
  installing: boolean;
  onInstall: () => void;
  onSelect: () => void;
  release: AppRegistryRelease;
  showPreviewVersions: boolean;
  token: string;
}) {
  const { locale, t } = useI18n();
  const title = appRegistryTitle(app, locale);
  const description = appRegistryDescription(app, locale);
  const upgradeAvailable = installed ? needsAppUpgrade(installed, release) : false;
  const alreadyInstalled = Boolean(installed) && installedMatchesRelease(installed, release);
  const previewAvailable = showPreviewVersions && appHasPreviewRelease(app);
  const icon = mergeAppIconSpec(installed?.icon, app.icon);
  const iconSrc = installed ? appIconURL(token, installed) || appRegistryIconURL(app, OFFICIAL_APP_REGISTRY) : appRegistryIconURL(app, OFFICIAL_APP_REGISTRY);

  return (
    <section className="flex min-w-0 items-center gap-4 rounded-xl px-3 py-2 transition-colors hover:bg-muted/35">
      <button className="flex min-w-0 flex-1 items-center gap-4 text-left" type="button" onClick={onSelect}>
        <AppIcon icon={icon} size="xl" src={iconSrc} />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="truncate text-sm font-semibold">{title}</h3>
            {release.version ? <span className="shrink-0 text-xs text-muted-foreground">v{release.version}</span> : null}
            {previewAvailable ? <Badge variant="secondary">{t("apps.previewAvailable")}</Badge> : null}
          </div>
          <p className="mt-0.5 truncate text-sm text-muted-foreground">{description || t("apps.noDescription")}</p>
        </div>
      </button>
      <Button
        className="h-8 shrink-0 rounded-full px-4"
        disabled={installing || alreadyInstalled}
        size="xs"
        type="button"
        variant="outline"
        onClick={onInstall}
      >
        {installing ? <Loader2 className="size-3.5 animate-spin" /> : upgradeAvailable ? <Download className="size-3.5" /> : null}
        {alreadyInstalled ? t("apps.installedAction") : upgradeAvailable ? t("apps.upgrade") : t("apps.install")}
      </Button>
    </section>
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
  const title = appRegistryTitle(app, locale);
  const description = appRegistryDescription(app, locale);
  const upgradeAvailable = installed && selectedRelease ? needsAppUpgrade(installed, selectedRelease) : false;
  const alreadyInstalled = Boolean(installed && selectedRelease && installedMatchesRelease(installed, selectedRelease));
  const endpoints = Object.entries(detail?.endpoints || {}).sort(([a], [b]) => a.localeCompare(b));
  const skills = detail?.skills || [];
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
                  disabled={installing || alreadyInstalled || !selectedRelease}
                  type="button"
                  onClick={onInstall}
                >
                  {installing ? <Loader2 className="size-4 animate-spin" /> : upgradeAvailable ? <Download className="size-4" /> : null}
                  {alreadyInstalled ? t("apps.installedAction") : upgradeAvailable ? t("apps.upgrade") : t("apps.install")}
                </Button>
              </div>
            </div>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{description || t("apps.noDescription")}</p>
          </div>
        </div>
      </div>
      <AppEndpointsSection count={detail ? endpoints.length : undefined} endpoints={endpoints}>
        {detailLoading ? (
          <DetailSkeletonRows />
        ) : detailFailed ? (
          <ContentLoadFailed error={detailError} />
        ) : (
          <EndpointRows endpoints={endpoints} />
        )}
      </AppEndpointsSection>
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
    </section>
  );
}

function AppEndpointsSection({
  appID,
  children,
  count,
  endpoints,
  mcpStatusByEndpoint,
  mcpStatusFailed,
  mcpStatusLoading,
  mcpStatusVisible = true,
  token,
}: {
  appID?: string;
  children?: ReactNode;
  count?: number;
  endpoints: Array<[string, AppEndpoints[string]]>;
  mcpStatusByEndpoint?: Map<string, AppMCPEndpointStatus[]>;
  mcpStatusFailed?: boolean;
  mcpStatusLoading?: boolean;
  mcpStatusVisible?: boolean;
  token?: string;
}) {
  const { t } = useI18n();
  return (
    <DetailSection title={t("apps.endpoints")} count={count ?? endpoints.length}>
      {children ?? (
        <EndpointRows
          appID={appID}
          endpoints={endpoints}
          mcpStatusByEndpoint={mcpStatusByEndpoint}
          mcpStatusFailed={mcpStatusFailed}
          mcpStatusLoading={mcpStatusLoading}
          mcpStatusVisible={mcpStatusVisible}
          token={token}
        />
      )}
    </DetailSection>
  );
}

function EndpointRows({
  appID,
  endpoints,
  mcpStatusByEndpoint,
  mcpStatusFailed,
  mcpStatusLoading,
  mcpStatusVisible = true,
  token,
}: {
  appID?: string;
  endpoints: Array<[string, AppEndpoints[string]]>;
  mcpStatusByEndpoint?: Map<string, AppMCPEndpointStatus[]>;
  mcpStatusFailed?: boolean;
  mcpStatusLoading?: boolean;
  mcpStatusVisible?: boolean;
  token?: string;
}) {
  const { t } = useI18n();
  const [editingMCP, setEditingMCP] = useState<{ name: string; endpoint: AppEndpoints[string] } | null>(null);
  if (endpoints.length === 0) {
    return <EmptyLine>{t("apps.none")}</EmptyLine>;
  }
  return (
    <>
      <div className="grid gap-2">
        {endpoints.map(([name, endpoint]) => {
          const statuses = mcpStatusByEndpoint?.get(name) || [];
          const isMCPConfigured = endpoint.kind === "mcp" && statuses.some((status) => status.configured);
          return (
            <DetailRow key={name}>
              <div className="flex min-w-0 items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-medium">{name}</span>
                  <Badge variant="outline">{endpoint.kind}</Badge>
                  {endpoint.kind === "mcp" && endpoint.transport ? <Badge variant="secondary">{endpoint.transport}</Badge> : null}
                </div>
                {appID && token && endpoint.kind === "mcp" ? (
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
              <EndpointTarget endpoint={endpoint} />
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

function EndpointTarget({ endpoint }: { endpoint: AppEndpoints[string] }) {
  const target = endpoint.kind === "mcp" && endpoint.transport === "stdio" ? endpoint.command : endpoint.url || endpoint.command;
  if (!target) {
    return null;
  }
  return (
    <div className="truncate text-xs text-muted-foreground" title={target}>
      {target}
    </div>
  );
}

function MCPConfigDialog({
  appID,
  endpoint,
  endpointName,
  open,
  token,
  onOpenChange,
}: {
  appID: string;
  endpoint: AppEndpoints[string];
  endpointName: string;
  open: boolean;
  token: string;
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
    onSuccess: async () => {
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
    return <MCPStatusLine icon={<Loader2 className="size-3.5 animate-spin" />} label={t("apps.mcpStatus.checking")} tone="muted" />;
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
        const meta = status.connectionID || undefined;
        const tone = mcpStatusTone(status.status);
        return (
          <div key={`${endpointName}:${status.connectionID || "default"}`} className="grid gap-2">
            {status.status === "available" ? (
              <MCPToolsList tools={status.tools || []} statusIcon={icon} statusLabel={label} statusMeta={meta} statusTone={tone} />
            ) : (
              <MCPStatusLine icon={icon} label={label} meta={meta} tone={tone} />
            )}
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

function MCPToolsList({
  statusIcon,
  statusLabel,
  statusMeta,
  statusTone,
  tools,
}: {
  statusIcon: ReactNode;
  statusLabel: string;
  statusMeta?: string;
  statusTone: "good" | "bad" | "muted";
  tools: AppMCPTool[];
}) {
  const { t } = useI18n();
  const toolCount = t("apps.mcpToolsCount").replace("{count}", String(tools.length));
  return (
    <details className="group min-w-0">
      <summary
        className={cn(
          "flex cursor-pointer list-none items-center gap-1.5 text-xs transition-colors [&::-webkit-details-marker]:hidden",
          statusTone === "good" && "text-emerald-500",
          statusTone === "bad" && "text-destructive",
          statusTone === "muted" && "text-muted-foreground",
        )}
      >
        <span className="shrink-0">{statusIcon}</span>
        <span className="font-medium">{statusLabel}</span>
        {statusMeta ? <span className="truncate text-muted-foreground">· {statusMeta}</span> : null}
        <span className="text-muted-foreground">({toolCount})</span>
        <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
      </summary>
      <div className="mt-2 grid max-h-72 min-w-0 gap-2 overflow-auto border-t border-border/50 pt-2 pr-1">
        {tools.length === 0 ? <div className="text-xs text-muted-foreground">{t("apps.mcpNoTools")}</div> : null}
        {tools.map((tool) => (
          <div key={tool.name} className="grid min-w-0 gap-1.5 border-t border-border/50 pt-2 first:border-t-0 first:pt-0">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="truncate text-sm font-medium">{tool.title || tool.name}</span>
              {tool.title && tool.title !== tool.name ? <Badge variant="outline">{tool.name}</Badge> : null}
            </div>
            {tool.description ? (
              <>
                <div className="line-clamp-3 text-xs leading-5 text-muted-foreground">{mcpToolDescriptionSummary(tool.description)}</div>
                {mcpToolDescriptionNeedsDetails(tool.description) ? (
                  <details className="group min-w-0">
                    <summary className="cursor-pointer text-xs text-muted-foreground transition-colors hover:text-foreground">
                      {t("apps.mcpFullDescription")}
                    </summary>
                    <div className="mt-1 max-h-40 max-w-full overflow-auto whitespace-pre-wrap break-words rounded-md bg-background/80 p-2 text-[11px] leading-4 text-muted-foreground">
                      {tool.description}
                    </div>
                  </details>
                ) : null}
              </>
            ) : null}
            {tool.providerName ? (
              <div className="min-w-0 truncate font-mono text-[11px] text-muted-foreground" title={tool.providerName}>
                {t("apps.mcpProviderToolName")}: {tool.providerName}
              </div>
            ) : null}
            {tool.inputSchema ? (
              <details className="group min-w-0">
                <summary className="cursor-pointer text-xs text-muted-foreground transition-colors hover:text-foreground">
                  {t("apps.mcpInputSchema")}
                </summary>
                <pre className="mt-1 max-h-44 max-w-full overflow-auto whitespace-pre-wrap break-words rounded-md bg-background/80 p-2 text-[11px] leading-4 text-muted-foreground">
                  {formatMCPInputSchema(tool.inputSchema)}
                </pre>
              </details>
            ) : null}
          </div>
        ))}
      </div>
    </details>
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

function mcpToolDescriptionSummary(description: string) {
  const text = description.replace(/\s+/g, " ").trim();
  if (text.length <= 260) {
    return text;
  }
  return `${text.slice(0, 257).trimEnd()}...`;
}

function mcpToolDescriptionNeedsDetails(description: string) {
  return description.replace(/\s+/g, " ").trim().length > 260 || description.includes("\n");
}

function AppSkillsSection({
  children,
  count,
  icon,
  iconSrc,
  onSkillSelect,
  skills,
}: {
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
      {children ?? <SkillRows icon={icon} iconSrc={iconSrc} skills={skills} onSkillSelect={onSkillSelect} />}
    </DetailSection>
  );
}

function SkillRows({
  icon,
  iconSrc,
  onSkillSelect,
  skills,
}: {
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
    <ItemGroup className="gap-2">
      {skills.map((skill) => (
        <Item key={skill.path} asChild className="items-start gap-3 rounded-lg px-3 py-3 hover:bg-muted/35" variant="outline">
          <button type="button" onClick={() => onSkillSelect?.(skill)}>
            <ItemMedia>
              <AppIcon icon={icon} size="md" src={iconSrc} />
            </ItemMedia>
            <ItemContent className="min-w-0 gap-1">
              <ItemTitle className="flex max-w-full flex-wrap items-center gap-2">
                <span className="min-w-0 truncate text-sm font-medium">{skill.name || skill.id || skill.path}</span>
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
  failed,
  icon,
  iconSrc,
  loading,
  open,
  skill,
  onOpenChange,
}: {
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
  const title = skill?.name || skill?.id || skill?.path || "";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[82vh] max-h-[760px] flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader className="shrink-0">
          <div className="mb-3">
            <AppIcon className="size-10" icon={icon} size="lg" src={iconSrc} />
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
    <section className="grid min-w-0 gap-3.5">
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

function DetailRow({ children }: { children: ReactNode }) {
  return <div className="grid min-w-0 gap-1 rounded-md bg-muted/35 px-3 py-2.5">{children}</div>;
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
  onDelete,
  onEdit,
}: {
  authMethods: AppAuthMethod[];
  connection: AppConnection;
  onDelete: (connection: AppConnection) => void;
  onEdit: (connection: AppConnection) => void;
}) {
  const { t } = useI18n();
  const name = connection.name || connection.id || t("apps.connection");
  const authLabel = connectionAuthBadgeLabel(connection, authMethods, t);
  return (
    <div className="flex min-w-0 items-center gap-3 rounded-md bg-muted/35 px-3 py-2.5">
      <KeyRound className="size-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium">{name}</span>
          {authLabel ? (
            <Badge className={cn(connection.tokenSet && "text-success")} variant="outline">
              {authLabel}
            </Badge>
          ) : null}
        </div>
        {connection.header ? <div className="truncate text-xs text-muted-foreground">{connection.header}</div> : null}
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
  const connectionFields = useMemo(() => appConnectionFields(app), [app]);
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
      });
    },
    onSuccess: async () => {
      toast.success(t("apps.connectionSaved"));
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.appConnections() });
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
      appConnectionOnlyAuthMethod({ auth: editingAppAuth, connection: editingAppConnection }, methods, appConnectionFields({ connection: editingAppConnection }));
    setForm({
      id: editingConnectionID || nextConnectionID(editingAppID, connections),
      name: editingConnectionName || nextConnectionName(editingAppName || editingAppID, editingAppID, connections),
      authMethodID: initialMethod?.id || "",
      authType: normalizeAuthType(initialMethod?.type || editingConnectionAuthType),
      fields: emptyConnectionFields(editingAppConnection),
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
  const canSave =
    Boolean(selectedAuthMethod) &&
    !isOAuth &&
    form.id.trim() !== "" &&
    form.name.trim() !== "" &&
    tokenReady &&
    basicReady &&
    headerReady &&
    fieldsReady &&
    !saveMutation.isPending &&
    !secretLoading;
  const canStartOAuth =
    Boolean(selectedAuthMethod) && isOAuth && form.id.trim() !== "" && form.name.trim() !== "" && !startOAuthMutation.isPending && !secretLoading;

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
              {startOAuthMutation.isPending ? <Loader2 className="animate-spin" /> : null}
              {connection ? t("apps.reauthorizeConnection") : t("apps.authorizeConnection")}
            </Button>
          ) : (
            <Button disabled={!canSave} type="button" onClick={() => saveMutation.mutate()}>
              {saveMutation.isPending ? <Loader2 className="animate-spin" /> : null}
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
          className="absolute inset-y-0 right-1 my-auto flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none disabled:opacity-50"
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
  app: Pick<AppDefinition, "auth" | "connection"> | null | undefined,
  methods: AppAuthMethod[],
  fields: AppConnectionField[],
): AppAuthMethod | undefined {
  if (app?.auth?.required || methods.length > 0 || fields.length === 0) {
    return undefined;
  }
  return { id: "", type: "none" } as AppAuthMethod;
}

function appCanManageConnections(app?: Pick<AppDefinition, "auth" | "connection"> | null) {
  const authMethods = appAuthMethods(app).filter((method) => normalizeAuthType(method.type) !== "none");
  return authMethods.length > 0 || appConnectionFields(app).length > 0 || app?.auth?.required === true;
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

async function openExternalURL(url: string) {
  try {
    const { Browser } = await import("@wailsio/runtime");
    await Browser.OpenURL(url);
    return;
  } catch {
    window.open(url, "_blank", "noopener,noreferrer");
  }
}
