import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Loader2, Plus } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import {
  createProvider,
  createProviderRequest,
  type ProviderProfile,
} from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { BrandIcon } from "@/components/BrandIcons";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldLabel,
  FieldTitle,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import {
  defaultProviderPresetVariant,
  generateProviderProfileID,
  providerPresetProfileName,
  providerPresetVariant,
  type ProviderPreset,
  type ProviderPresetVariant,
} from "@/provider/presets";

const providerOptionSelectedClass = "border-primary/45 bg-primary/5 text-foreground";

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
            <BrandIcon className="pudding-provider-preset-icon" name={preset.id} radius="none" />
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
  }, [open, preset, profiles]);

  const variant = preset ? providerPresetVariant(preset, variantID) : null;
  const presetTitle = preset ? providerPresetDisplayName(preset, t) : t("provider.create");
  const presetDescription = preset
    ? providerPresetDescription(preset, t)
    : t("provider.quickCreateHint");
  const apiKeyRequired = variant ? !variant.apiKeyOptional : true;
  const activeBaseURL = variant?.baseURLEditable ? baseURL.trim() : variant?.baseURL || "";
  const activeModels = variant?.dynamicModels ? [] : variant?.models || [];
  const baseURLReady = !variant?.baseURLEditable || Boolean(activeBaseURL);
  const canCreate = Boolean(preset && variant && profileID && baseURLReady && (!apiKeyRequired || apiKey.trim()));
  const handleVariantChange = (value: string) => {
    setVariantID(value);
    const nextVariant = preset ? providerPresetVariant(preset, value) : null;
    setBaseURL(nextVariant?.baseURL || "");
    setLocalError("");
  };
  const openAPIKeyURL = () => {
    const url = preset?.apiKeyURL;
    if (!url) {
      return;
    }
    void import("@wailsio/runtime").then(({ Browser }) => Browser.OpenURL(url)).catch(() => {
      window.open(url, "_blank", "noopener,noreferrer");
    });
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

  return (
    <Dialog open={open} onOpenChange={(next) => {
      if (!mutation.isPending) {
        onOpenChange(next);
      }
    }}>
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
              {t("provider.getAPIKey")}
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
              }}
            />
          </Field>

          {variant?.dynamicModels ? (
            <div className="rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground">
              {t("provider.dynamicModelsHint")}
            </div>
          ) : null}

          <div className="text-xs text-muted-foreground">
            {t("provider.profileID")}: {profileID}
          </div>
        </div>

        <DialogFooter>
          <Button disabled={mutation.isPending} type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button disabled={mutation.isPending || !canCreate} type="button" onClick={() => mutation.mutate()}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
            {t("provider.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
      <RadioGroup value={variantID} onValueChange={onVariantChange}>
        {preset.variants.map((item) => {
          const active = item.id === variant?.id;
          const id = `provider-preset-${preset.id}-${item.id}`;
          return (
            <FieldLabel key={item.id} htmlFor={id}>
              <Field className={cn(active && providerOptionSelectedClass)} orientation="horizontal">
                <FieldContent>
                  <FieldTitle>{providerPresetAccessMethodLabel(preset, item, t)}</FieldTitle>
                </FieldContent>
                <RadioGroupItem id={id} value={item.id} />
              </Field>
            </FieldLabel>
          );
        })}
      </RadioGroup>
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
        <RadioGroup
          className="grid gap-2"
          value={current.protocol}
          onValueChange={(value) => onVariantChange(miMoVariantID(current.plan, value as MiMoProtocol))}
        >
          {protocolOptions.map((item) => {
            const active = item.id === current.protocol;
            const id = `provider-preset-${preset.id}-protocol-${item.id}`;
            return (
              <FieldLabel key={item.id} htmlFor={id}>
                <Field className={cn("h-full", active && providerOptionSelectedClass)} orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>{item.label}</FieldTitle>
                  </FieldContent>
                  <RadioGroupItem id={id} value={item.id} />
                </Field>
              </FieldLabel>
            );
          })}
        </RadioGroup>
      </div>

      <div className="grid gap-2">
        <Label>{t("provider.endpointMode")}</Label>
        <RadioGroup
          className="grid grid-cols-2 gap-2"
          value={current.plan}
          onValueChange={(value) => onVariantChange(miMoVariantID(value as MiMoPlan, current.protocol))}
        >
          {planOptions.map((item) => {
            const active = item.id === current.plan;
            const id = `provider-preset-${preset.id}-plan-${item.id}`;
            return (
              <FieldLabel key={item.id} htmlFor={id}>
                <Field className={cn("h-full", active && providerOptionSelectedClass)} orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>{item.label}</FieldTitle>
                  </FieldContent>
                  <RadioGroupItem id={id} value={item.id} />
                </Field>
              </FieldLabel>
            );
          })}
        </RadioGroup>
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
