import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Plus } from "@/components/icons";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import {
  createProvider,
  createProviderRequest,
  probeProviderModels,
  type ProviderProfile,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import { Spinner } from "@/components/Spinner";
import { UnsavedChangesAlert } from "@/components/UnsavedChangesAlert";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NeutralRadioCard, NeutralRadioGroup } from "@/components/NeutralRadioGroup";
import { useI18n } from "@/i18n";
import { useUnsavedChangesGuard } from "@/hooks/useUnsavedChangesGuard";
import { openExternalURL } from "@/lib/desktopBridge";
import { cn } from "@/lib/utils";
import {
  defaultProviderPresetVariant,
  generateProviderProfileID,
  mergeProviderModelCandidate,
  providerModelDisplayName,
  providerModelDiscoveryForVariant,
  providerPresetProfileName,
  providerSupportsModelDiscovery,
  providerPresetVariant,
  type ProviderPreset,
  type ProviderPresetVariant,
} from "@/provider/presets";

export function ProviderPresetGrid({
  children,
  className,
  presets,
  onSelect,
}: {
  children?: ReactNode;
  className?: string;
  presets: ProviderPreset[];
  onSelect: (preset: ProviderPreset) => void;
}) {
  const { t } = useI18n();

  return (
    <div className={cn("pudding-provider-preset-grid", className)}>
      {presets.map((preset) => {
        const variant = defaultProviderPresetVariant(preset);
        const displayName = providerPresetDisplayName(preset, t);
        return (
          <button
            key={preset.id}
            className="pudding-provider-preset-card"
            type="button"
            onClick={() => onSelect(preset)}
          >
            <BrandIcon className="pudding-provider-preset-icon" name={preset.id} />
            <span className="pudding-provider-preset-text">
              <span className="pudding-provider-preset-title">{displayName}</span>
              <span className="pudding-provider-preset-meta">{variantModelCountLabel(variant, t)}</span>
            </span>
          </button>
        );
      })}
      {children}
    </div>
  );
}

export function ProviderCustomCard({
  onSelect,
}: {
  onSelect: () => void;
}) {
  const { t } = useI18n();

  return (
    <button
      className="pudding-provider-preset-card border-dashed"
      type="button"
      onClick={onSelect}
    >
      <span className="pudding-provider-preset-custom-icon flex items-center justify-center rounded-md bg-muted text-muted-foreground">
        <Plus className="pudding-provider-preset-custom-icon-mark" />
      </span>
      <span className="pudding-provider-preset-text">
        <span className="pudding-provider-preset-title">{t("provider.custom")}</span>
      </span>
    </button>
  );
}

export function ProviderPresetCreateDialog({
  open,
  preset,
  profiles,
  token,
  onCreated,
  onOpenChange,
}: {
  open: boolean;
  preset: ProviderPreset | null;
  profiles: ProviderProfile[];
  token: string;
  onCreated?: (profile: ProviderProfile, variant: ProviderPresetVariant) => void;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const { t } = useI18n();
  const [variantID, setVariantID] = useState("");
  const [apiKey, setAPIKey] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [profileID, setProfileID] = useState("");
  const [localError, setLocalError] = useState("");
  const [modelsLoading, setModelsLoading] = useState(false);
  const [candidateIDs, setCandidateIDs] = useState<string[]>([]);
  const [selectedCandidateIDs, setSelectedCandidateIDs] = useState<string[]>([]);
  const [candidateFilter, setCandidateFilter] = useState("");
  const [verifiedModelCount, setVerifiedModelCount] = useState<number | null>(null);
  const initializedPresetIDRef = useRef<string | null>(null);

  useEffect(() => {
    if (!preset || !open) {
      initializedPresetIDRef.current = null;
      return;
    }
    if (initializedPresetIDRef.current === preset.id) {
      return;
    }
    initializedPresetIDRef.current = preset.id;
    const initialVariant = defaultProviderPresetVariant(preset);
    setVariantID(initialVariant.id);
    setAPIKey("");
    setBaseURL(initialVariant.baseURL);
    setProfileID(generateProviderProfileID(profiles, preset.id));
    setLocalError("");
    setModelsLoading(false);
    setCandidateIDs([]);
    setSelectedCandidateIDs([]);
    setCandidateFilter("");
    setVerifiedModelCount(null);
  }, [open, preset, profiles]);

  const variant = preset ? providerPresetVariant(preset, variantID) : null;
  const presetTitle = preset ? providerPresetDisplayName(preset, t) : t("provider.create");
  const presetDescription = preset
    ? providerPresetDescription(preset, t)
    : t("provider.quickCreateHint");
  const apiKeyRequired = variant ? !variant.apiKeyOptional : true;
  const activeBaseURL = variant?.baseURLEditable ? baseURL.trim() : variant?.baseURL || "";
  const activeModels = variant?.dynamicModels
    ? selectedCandidateIDs.map((id) => mergeProviderModelCandidate(id, variant, variant.protocol))
    : variant?.models || [];
  const baseURLReady = !variant?.baseURLEditable || Boolean(activeBaseURL);
  const modelsReady = !variant?.dynamicModels || activeModels.length > 0;
  const supportsModelDiscovery = variant
    ? providerSupportsModelDiscovery(variant)
    : false;
  const canCreate = Boolean(preset && variant && profileID && baseURLReady && modelsReady && (!apiKeyRequired || apiKey.trim()));
  const filteredCandidateIDs = useMemo(() => {
    const query = candidateFilter.trim().toLowerCase();
    return query
      ? candidateIDs.filter((id) =>
          id.toLowerCase().includes(query) || providerModelDisplayName(id).toLowerCase().includes(query),
        )
      : candidateIDs;
  }, [candidateFilter, candidateIDs]);
  const initialVariant = preset ? defaultProviderPresetVariant(preset) : null;
  const dirty = Boolean(
    open &&
    preset &&
    initializedPresetIDRef.current === preset.id &&
    (apiKey.trim() || selectedCandidateIDs.length > 0 || variantID !== initialVariant?.id || (variant?.baseURLEditable && baseURL !== variant.baseURL)),
  );

  const resetModelProbe = () => {
    setCandidateIDs([]);
    setSelectedCandidateIDs([]);
    setCandidateFilter("");
    setVerifiedModelCount(null);
  };

  const unsavedChanges = useUnsavedChangesGuard(dirty);
  const handleVariantChange = (value: string) => {
    setVariantID(value);
    const nextVariant = preset ? providerPresetVariant(preset, value) : null;
    setBaseURL(nextVariant?.baseURL || "");
    setLocalError("");
    resetModelProbe();
  };
  const openAPIKeyURL = () => {
    const url = preset?.apiKeyURL;
    if (!url) {
      return;
    }
    void openExternalURL(url);
  };

  const verifyAndLoadModels = async () => {
    if (!preset || !variant || !baseURLReady || (apiKeyRequired && !apiKey.trim())) {
      setLocalError(apiKeyRequired && !apiKey.trim() ? t("provider.credentialRequired") : t("provider.baseURLRequired"));
      return;
    }
    setModelsLoading(true);
    setLocalError("");
    try {
      const discovery = providerModelDiscoveryForVariant(preset, variant, activeBaseURL);
      const response = await probeProviderModels(token, {
        brand: preset.id,
        protocol: discovery.protocol,
        baseURL: discovery.baseURL,
        apiKey: apiKey.trim(),
      });
      const models = Array.from(new Set(response.models.map((id) => id.trim()).filter(Boolean)));
      setVerifiedModelCount(models.length);
      if (variant.dynamicModels) {
        setCandidateIDs(models);
        setSelectedCandidateIDs(models);
        setCandidateFilter("");
      }
    } catch {
      resetModelProbe();
      setLocalError(t("provider.candidatesFailed"));
    } finally {
      setModelsLoading(false);
    }
  };

  const toggleCandidate = (id: string, checked: boolean | "indeterminate") => {
    setSelectedCandidateIDs((current) => {
      if (checked === true) {
        return current.includes(id) ? current : [...current, id];
      }
      return current.filter((item) => item !== id);
    });
  };

  const selectAllFilteredCandidates = () => {
    setSelectedCandidateIDs((current) => Array.from(new Set([...current, ...filteredCandidateIDs])));
  };

  const mutation = useMutation({
    mutationFn: async () => {
      if (!preset || !variant) {
        throw new Error("missing preset");
      }
      if (apiKeyRequired && !apiKey.trim()) {
        throw new Error(t("provider.credentialRequired"));
      }
      return createProvider(
        token,
        createProviderRequest.parse({
          id: profileID,
          displayName: providerPresetProfileName(preset, variant),
          brand: preset.id,
          group: variant.group,
          protocol: variant.protocol,
          baseURL: activeBaseURL,
          apiKey: apiKey.trim(),
          models: activeModels,
        }),
      );
    },
    onSuccess: async (profile) => {
      setLocalError("");
      await queryClient.invalidateQueries({ queryKey: queryKeys.providers() });
      if (variant) {
        onCreated?.(profile, variant);
      }
      onOpenChange(false);
    },
    onError: (error) => {
      setLocalError(error instanceof Error ? error.message : t("provider.saveFailed"));
    },
  });

  function handleDialogOpenChange(nextOpen: boolean) {
    if (mutation.isPending) {
      return;
    }
    if (nextOpen) {
      onOpenChange(true);
      return;
    }
    unsavedChanges.request(() => onOpenChange(false));
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleDialogOpenChange}>
        <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{presetTitle}</DialogTitle>
          <DialogDescription>{presetDescription}</DialogDescription>
          {preset?.apiKeyURL ? (
            <button
              className="mt-1 inline-flex w-fit items-center gap-1.5 text-sm font-normal text-foreground/80 underline-offset-4 hover:text-foreground hover:underline"
              type="button"
              onClick={openAPIKeyURL}
            >
              {t("provider.learnMore")}
              <ExternalLink className="size-3.5" />
            </button>
          ) : null}
        </DialogHeader>

        <div className="grid gap-4">
          {localError ? (
            <Alert variant="destructive">
              <AlertDescription>{localError}</AlertDescription>
            </Alert>
          ) : null}

          {preset && preset.variants.length > 1 ? (
            preset.id === "mimo" ? (
              <MiMoVariantPicker
                preset={preset}
                variantID={variantID}
                onVariantChange={handleVariantChange}
                t={t}
              />
            ) : (
              <VariantListPicker
                preset={preset}
                variant={variant}
                variantID={variantID}
                onVariantChange={handleVariantChange}
                t={t}
              />
            )
          ) : null}

          {variant?.baseURLEditable ? (
            <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
              <FieldLabel className="w-auto cursor-default text-sm font-medium" htmlFor="provider-preset-base-url">
                {t("provider.baseURL")}
              </FieldLabel>
              <Input
                id="provider-preset-base-url"
                placeholder={variant.baseURLPlaceholder || "https://api.example.com/v1"}
                value={baseURL}
              onChange={(event) => {
                setBaseURL(event.target.value);
                setLocalError("");
                resetModelProbe();
              }}
              />
            </Field>
          ) : null}

          <Field className="gap-2 rounded-none border-0 bg-transparent p-0 hover:bg-transparent">
            <FieldLabel className="w-auto cursor-default text-sm font-medium" htmlFor="provider-preset-api-key">
              {t("provider.apiKey")}
            </FieldLabel>
            <Input
              id="provider-preset-api-key"
              autoComplete="off"
              placeholder={variant?.apiKeyOptional ? t("provider.apiKeyOptional") : "sk-..."}
              type="password"
              value={apiKey}
              onChange={(event) => {
                setAPIKey(event.target.value);
                setLocalError("");
                resetModelProbe();
              }}
            />
          </Field>

          {variant?.dynamicModels ? (
            <div className="grid gap-2 rounded-lg border bg-muted/20 p-3">
              <div className="flex items-center justify-between gap-3">
                <div className="grid gap-0.5">
                  <Label>{t("provider.models")}</Label>
                  <span className="text-xs text-muted-foreground">{t("provider.dynamicModelsHint")}</span>
                </div>
                <Button
                  disabled={modelsLoading || !baseURLReady || (apiKeyRequired && !apiKey.trim())}
                  size="sm"
                  type="button"
                  variant="outline"
                  onClick={() => void verifyAndLoadModels()}
                >
                  {modelsLoading ? <Spinner /> : null}
                  {t("provider.testAndLoadModels")}
                </Button>
              </div>
              {candidateIDs.length > 8 ? (
                <Input
                  autoComplete="off"
                  placeholder={t("provider.filterCandidates")}
                  value={candidateFilter}
                  onChange={(event) => setCandidateFilter(event.target.value)}
                />
              ) : null}
              {candidateIDs.length > 0 ? (
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs text-muted-foreground">
                    {t("provider.selectedAndTotalCount")
                      .replace("{selected}", String(selectedCandidateIDs.length))
                      .replace("{total}", String(candidateIDs.length))}
                  </span>
                  <div className="flex items-center gap-1">
                    <Button
                      disabled={filteredCandidateIDs.length === 0}
                      size="xs"
                      type="button"
                      variant="ghost"
                      onClick={selectAllFilteredCandidates}
                    >
                      {t("common.selectAll")}
                    </Button>
                    <Button
                      disabled={selectedCandidateIDs.length === 0}
                      size="xs"
                      type="button"
                      variant="ghost"
                      onClick={() => setSelectedCandidateIDs([])}
                    >
                      {t("common.clear")}
                    </Button>
                  </div>
                </div>
              ) : null}
              {candidateIDs.length > 0 ? (
                <div className="max-h-48 overflow-y-auto rounded-md border bg-background p-1">
                  {filteredCandidateIDs.map((id) => (
                    <label key={id} className="flex min-w-0 cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent">
                      <Checkbox
                        checked={selectedCandidateIDs.includes(id)}
                        onCheckedChange={(checked) => toggleCandidate(id, checked)}
                      />
                      <span className="grid min-w-0 gap-0.5">
                        <span className="truncate">{providerModelDisplayName(id)}</span>
                        <span className="truncate font-mono text-xs text-muted-foreground">{id}</span>
                      </span>
                    </label>
                  ))}
                  {filteredCandidateIDs.length === 0 ? (
                    <div className="px-2 py-6 text-center text-sm text-muted-foreground">{t("provider.candidatesNoMatch")}</div>
                  ) : null}
                </div>
              ) : null}
              {verifiedModelCount !== null ? (
                <span className={cn("text-xs", verifiedModelCount > 0 ? "text-success" : "text-warning")}>
                  {verifiedModelCount > 0
                    ? t("provider.dynamicModelsLoaded").replace("{count}", String(verifiedModelCount))
                    : t("provider.candidatesEmpty")}
                </span>
              ) : null}
            </div>
          ) : supportsModelDiscovery ? (
            <div className="flex items-center gap-3">
              <Button
                disabled={modelsLoading || !baseURLReady || (apiKeyRequired && !apiKey.trim())}
                size="sm"
                type="button"
                variant="outline"
                onClick={() => void verifyAndLoadModels()}
              >
                {modelsLoading ? <Spinner /> : null}
                {t("provider.testConnection")}
              </Button>
              {verifiedModelCount !== null ? (
                <span className="text-xs text-success">
                  {t("provider.connectionVerified").replace("{count}", String(verifiedModelCount))}
                </span>
              ) : null}
            </div>
          ) : null}

          <div className="text-xs text-muted-foreground">
            {t("provider.profileID")}: {profileID}
          </div>
        </div>

        <DialogFooter>
          <Button disabled={mutation.isPending} type="button" variant="ghost" onClick={() => handleDialogOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button disabled={mutation.isPending || !canCreate} type="button" onClick={() => mutation.mutate()}>
            {mutation.isPending ? <Spinner /> : <Plus />}
            {t("provider.create")}
          </Button>
        </DialogFooter>
        </DialogContent>
      </Dialog>
      <UnsavedChangesAlert
        open={unsavedChanges.confirmationOpen}
        onDiscard={unsavedChanges.discard}
        onOpenChange={unsavedChanges.setConfirmationOpen}
      />
    </>
  );
}

function VariantListPicker({
  preset,
  variant,
  variantID,
  onVariantChange,
  t,
}: {
  preset: ProviderPreset;
  variant: ProviderPresetVariant | null;
  variantID: string;
  onVariantChange: (value: string) => void;
  t: (key: string) => string;
}) {
  return (
    <div className="grid gap-2">
      <Label>{t("provider.accessMethod")}</Label>
      <NeutralRadioGroup className="sm:grid-cols-2" value={variantID} onValueChange={onVariantChange}>
        {preset.variants.map((item) => {
          const active = item.id === variant?.id;
          const id = `provider-preset-${preset.id}-${item.id}`;
          return (
            <NeutralRadioCard
              key={item.id}
              id={id}
              selected={active}
              title={providerPresetAccessMethodLabel(preset, item, t)}
              value={item.id}
            />
          );
        })}
      </NeutralRadioGroup>
    </div>
  );
}

function MiMoVariantPicker({
  preset,
  variantID,
  onVariantChange,
  t,
}: {
  preset: ProviderPreset;
  variantID: string;
  onVariantChange: (value: string) => void;
  t: (key: string) => string;
}) {
  const current = parseMiMoVariantID(variantID);
  const protocolOptions: Array<{ id: MiMoProtocol; label: string }> = [
    {
      id: "openai",
      label: translatePresetText(t, "providerPreset.mimo.protocol.openai.label", "OpenAI"),
    },
    {
      id: "anthropic",
      label: translatePresetText(t, "providerPreset.mimo.protocol.anthropic.label", "Anthropic"),
    },
  ];
  const planOptions: Array<{ id: MiMoPlan; label: string }> = [
    {
      id: "standard",
      label: translatePresetText(t, "providerPreset.mimo.plan.standard.label", "Standard API"),
    },
    {
      id: "plan",
      label: translatePresetText(t, "providerPreset.mimo.plan.plan.label", "Plan"),
    },
  ];

  return (
    <div className="grid gap-3">
      <div className="grid gap-2">
        <Label>{t("provider.accessMethod")}</Label>
        <NeutralRadioGroup
          className="sm:grid-cols-2"
          value={current.protocol}
          onValueChange={(value) => onVariantChange(miMoVariantID(current.plan, value as MiMoProtocol))}
        >
          {protocolOptions.map((item) => {
            const active = item.id === current.protocol;
            const id = `provider-preset-${preset.id}-protocol-${item.id}`;
            return (
              <NeutralRadioCard
                key={item.id}
                className="h-full"
                id={id}
                selected={active}
                title={item.label}
                value={item.id}
              />
            );
          })}
        </NeutralRadioGroup>
      </div>

      <div className="grid gap-2">
        <Label>{t("provider.endpointMode")}</Label>
        <NeutralRadioGroup
          className="sm:grid-cols-2"
          value={current.plan}
          onValueChange={(value) => onVariantChange(miMoVariantID(value as MiMoPlan, current.protocol))}
        >
          {planOptions.map((item) => {
            const active = item.id === current.plan;
            const id = `provider-preset-${preset.id}-plan-${item.id}`;
            return (
              <NeutralRadioCard
                key={item.id}
                className="h-full"
                id={id}
                selected={active}
                title={item.label}
                value={item.id}
              />
            );
          })}
        </NeutralRadioGroup>
      </div>
    </div>
  );
}

type MiMoPlan = "standard" | "plan";
type MiMoProtocol = "openai" | "anthropic";

function parseMiMoVariantID(variantID: string): { plan: MiMoPlan; protocol: MiMoProtocol } {
  return {
    plan: variantID.startsWith("plan-") ? "plan" : "standard",
    protocol: variantID.endsWith("-anthropic") ? "anthropic" : "openai",
  };
}

function miMoVariantID(plan: MiMoPlan, protocol: MiMoProtocol) {
  return `${plan}-${protocol}`;
}

function providerPresetDisplayName(preset: ProviderPreset, t: (key: string) => string) {
  return translatePresetText(t, `providerPreset.${preset.id}.name`, preset.name);
}

function providerPresetDescription(preset: ProviderPreset, t: (key: string) => string) {
  return translatePresetText(t, `providerPreset.${preset.id}.description`, preset.description);
}

function modelCountLabel(count: number, t: (key: string) => string) {
  if (count <= 0) {
    return t("picker.noModels");
  }
  return `${count}${t("provider.modelCountSuffix")}`;
}

function variantModelCountLabel(variant: ProviderPresetVariant, t: (key: string) => string) {
  if (variant.dynamicModels) {
    return t("provider.modelCountDynamic");
  }
  return modelCountLabel(variant.models.length, t);
}

function providerPresetAccessMethodLabel(
  preset: ProviderPreset,
  variant: ProviderPresetVariant,
  t: (key: string) => string,
) {
  if (preset.id === "mimo" && variant.protocol === "openai-compatible") {
    return translatePresetText(t, "providerPreset.mimo.protocol.openai.label", "OpenAI");
  }
  if (preset.id === "mimo" && variant.protocol === "anthropic") {
    return translatePresetText(t, "providerPreset.mimo.protocol.anthropic.label", "Anthropic");
  }
  return providerPresetVariantLabel(preset, variant, t);
}

function providerPresetVariantLabel(
  preset: ProviderPreset,
  variant: ProviderPresetVariant,
  t: (key: string) => string,
) {
  return translatePresetText(t, `providerPreset.${preset.id}.variant.${variant.id}.label`, variant.label);
}

function translatePresetText(t: (key: string) => string, key: string, fallback: string) {
  const value = t(key);
  return value === key ? fallback : value;
}
