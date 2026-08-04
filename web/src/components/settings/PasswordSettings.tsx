import { Search, Trash2, Upload } from "@/components/icons";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import {
  electronBrowserBridge,
  type ElectronBrowserCredential,
  type ElectronBrowserCredentialList,
} from "@/browser/electronBridge";
import { Spinner } from "@/components/Spinner";
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
import { SETTINGS_CONTENT_CLASS } from "@/components/settings/shared";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/i18n";

const emptyCredentialList: ElectronBrowserCredentialList = {
  available: false,
  reason: "",
  credentials: [],
  neverSaveOrigins: [],
};

export function PasswordSettings() {
  const { t } = useI18n();
  const bridge = electronBrowserBridge();
  const [list, setList] = useState<ElectronBrowserCredentialList>(emptyCredentialList);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [clearOpen, setClearOpen] = useState(false);
  const [pendingAction, setPendingAction] = useState("");

  const loadList = useCallback(async () => {
    if (!bridge?.listCredentials) {
      setList(emptyCredentialList);
      setLoading(false);
      return;
    }
    try {
      setList(await bridge.listCredentials());
    } finally {
      setLoading(false);
    }
  }, [bridge]);

  useEffect(() => {
    void loadList().catch(() => setList(emptyCredentialList));
    return bridge?.onCredentialsChanged?.(() => void loadList().catch(() => undefined));
  }, [bridge, loadList]);

  const filteredCredentials = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    if (!query) return list.credentials;
    return list.credentials.filter((credential) =>
      `${credential.username}\n${originLabel(credential.origin)}`.toLocaleLowerCase().includes(query),
    );
  }, [list.credentials, search]);

  const run = async (name: string, operation: () => Promise<void>) => {
    setPendingAction(name);
    try {
      await operation();
    } catch (error) {
      toast.error(t("browser.passwordActionFailed"), { description: error instanceof Error ? error.message : String(error) });
    } finally {
      setPendingAction("");
    }
  };

  return (
    <div className={SETTINGS_CONTENT_CLASS}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative min-w-56 flex-1 sm:max-w-sm">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-9"
            placeholder={t("browser.passwordSearch")}
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        <Button type="button" variant="outline" onClick={() => void run("import", async () => {
          const result = await bridge?.importChromePasswords?.();
          if (!result || result.canceled) return;
          await loadList();
          toast.success(interpolate(t("browser.passwordImportResult"), result));
        })}>
          {pendingAction === "import" ? <Spinner /> : <Upload className="size-4" />}
          {t("browser.passwordImportChrome")}
        </Button>
      </div>

      {!list.available && !loading ? (
        <div className="rounded-lg border border-border/70 bg-card px-5 py-10 text-center text-sm text-muted-foreground">
          {t("browser.passwordStorageUnavailable")}
        </div>
      ) : (
        <section className="overflow-hidden rounded-lg border border-border/70 bg-card">
          <div className="flex h-10 items-center justify-between border-b border-border/70 bg-muted/20 px-3">
            <h3 className="text-sm font-normal">{t("browser.passwordSavedAccounts")}</h3>
            <span className="text-xs tabular-nums text-muted-foreground">{filteredCredentials.length}</span>
          </div>
          {loading ? (
            <div className="flex justify-center py-12"><Spinner className="size-5" /></div>
          ) : filteredCredentials.length > 0 ? filteredCredentials.map((credential) => (
            <CredentialRow
              key={credential.id}
              credential={credential}
              deleting={pendingAction === `delete:${credential.id}`}
              deleteLabel={t("browser.passwordDelete")}
              onDelete={() => void run(`delete:${credential.id}`, async () => {
                await bridge?.deleteCredential?.({ credentialID: credential.id });
                await loadList();
              })}
            />
          )) : (
            <div className="px-5 py-12 text-center text-sm text-muted-foreground">{t("browser.passwordEmpty")}</div>
          )}
        </section>
      )}

      {list.neverSaveOrigins.length > 0 ? (
        <section className="overflow-hidden rounded-lg border border-border/70 bg-card">
          <div className="flex h-10 items-center border-b border-border/70 bg-muted/20 px-3">
            <h3 className="text-sm font-normal">{t("browser.passwordNeverSites")}</h3>
          </div>
          {list.neverSaveOrigins.map((origin) => (
            <div key={origin} className="flex min-h-12 items-center justify-between gap-4 border-b border-border/60 px-3 last:border-b-0">
              <span className="min-w-0 truncate text-sm">{originLabel(origin)}</span>
              <Button size="sm" type="button" variant="ghost" onClick={() => void run(`allow:${origin}`, async () => {
                await bridge?.allowCredentialOrigin?.({ origin });
                await loadList();
              })}>
                {t("browser.passwordAllowSite")}
              </Button>
            </div>
          ))}
        </section>
      ) : null}

      <div className="flex justify-end border-t border-border/70 pt-4">
        <Button
          disabled={list.credentials.length === 0}
          type="button"
          variant="ghost"
          onClick={() => setClearOpen(true)}
        >
          {t("browser.passwordClear")}
        </Button>
      </div>

      <AlertDialog open={clearOpen} onOpenChange={setClearOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("browser.passwordClearTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("browser.passwordClearDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void run("clear", async () => {
              await bridge?.clearCredentials?.();
              setClearOpen(false);
              await loadList();
            })}>
              {pendingAction === "clear" ? <Spinner /> : t("browser.passwordClear")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CredentialRow({
  credential,
  deleteLabel,
  deleting,
  onDelete,
}: {
  credential: ElectronBrowserCredential;
  deleteLabel: string;
  deleting: boolean;
  onDelete: () => void;
}) {
  return (
    <div className="grid min-h-14 grid-cols-[minmax(0,1fr)_minmax(9rem,0.8fr)_2rem] items-center gap-4 border-b border-border/60 px-3 last:border-b-0 hover:bg-muted/30">
      <div className="min-w-0 truncate text-sm font-medium">{credential.username || originLabel(credential.origin)}</div>
      <div className="min-w-0 truncate text-sm text-muted-foreground">{originLabel(credential.origin)}</div>
      <Button aria-label={deleteLabel} disabled={deleting} size="icon-sm" type="button" variant="ghost" onClick={onDelete}>
        {deleting ? <Spinner /> : <Trash2 className="size-3.5" />}
      </Button>
    </div>
  );
}

function originLabel(origin: string) {
  try {
    return new URL(origin).host;
  } catch {
    return origin;
  }
}

function interpolate(template: string, values: Record<string, unknown>) {
  return Object.entries(values).reduce((message, [key, value]) => message.replaceAll(`{${key}}`, String(value)), template);
}
