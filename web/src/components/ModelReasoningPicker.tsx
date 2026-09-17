import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";

import { listProviders } from "@/api/client";
import { queryKeys } from "@/api/queryKeys";
import { AppPopoverContent, appPopoverItemStateClassName } from "@/components/AppPopover";
import { BrandIcon } from "@/components/BrandIcons";
import { ChevronDown, ChevronLeft, ChevronRight, RotateCcw } from "@/components/icons";
import { ModelCatalog } from "@/components/ModelCatalog";
import { Spinner } from "@/components/Spinner";
import { SteppedSlider } from "@/components/SteppedSlider";
import { Button } from "@/components/ui/button";
import { Popover, PopoverTrigger } from "@/components/ui/popover";
import { useI18n } from "@/i18n";
import { formatModelLabel } from "@/lib/model";
import { modelSelectionKey, resolveModelSelection, type ModelSelectionValue, type ResolvedModelSelection } from "@/lib/modelSelection";
import { normalizeReasoningEffort, resolveReasoningEffort } from "@/lib/reasoningEffort";
import { cn } from "@/lib/utils";
import { providerBrandForModel } from "@/provider/presets";

type ModelReasoningPickerProps = {
  token: string;
  value: ModelSelectionValue;
  reasoningValue: string;
  onChange: (value: { provider: string; model: string }) => void | Promise<void>;
  onReasoningChange: (value: string) => void | Promise<void>;
  onAfterClose?: () => void;
  onResolvedChange?: (value: ResolvedModelSelection | null) => void;
  disabled?: boolean;
  error?: string;
  iconOnly?: boolean;
  className?: string;
};

export function ModelReasoningPicker({
  token, value, reasoningValue, onChange, onReasoningChange, onAfterClose,
  onResolvedChange, disabled = false, error, iconOnly = false, className,
}: ModelReasoningPickerProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const closedFromOutside = useRef(false);
  const providersQuery = useQuery({
    queryKey: queryKeys.providers(),
    queryFn: () => listProviders(token),
    enabled: Boolean(token),
  });
  const profiles = useMemo(
    () => (providersQuery.data?.providers ?? []).filter((profile) => profile.models.some((model) => model.id)),
    [providersQuery.data?.providers],
  );
  const selection = useMemo(
    () => resolveModelSelection(profiles, value),
    [profiles, value.provider, value.model],
  );
  const reasoning = resolveReasoningEffort(selection, reasoningValue);
  const hasReasoning = reasoning.options.length > 0;
  const showCatalog = catalogOpen || !hasReasoning;
  const profile = profiles.find((item) => item.id === value.provider);
  const modelLabel = selection ? formatModelLabel(selection.model) : t("picker.selectModel");
  const brand = selection
    ? providerBrandForModel(selection.model) || profile?.brand || profile?.displayName || selection.provider
    : "";

  function effortLabel(effort: string) {
    const key = `provider.reasoningEffort.${effort}`;
    const translated = t(key);
    return translated === key ? effort : translated;
  }
  const reasoningLabel = reasoning.inherited
    ? reasoning.effective
      ? `${t("provider.reasoningEffort.auto")} (${effortLabel(reasoning.effective)})`
      : t("provider.reasoningEffort.auto")
    : effortLabel(reasoning.value);
  const label = hasReasoning ? `${modelLabel} · ${reasoningLabel}` : modelLabel;

  useEffect(() => {
    if (providersQuery.isSuccess) onResolvedChange?.(selection);
  }, [onResolvedChange, providersQuery.isSuccess, selection]);

  async function selectModel(next: { provider: string; model: string }) {
    if (disabled) return;
    try {
      if (next.provider !== value.provider || next.model !== value.model) await onChange(next);
      setCatalogOpen(false);
    } catch {
      // The owner keeps the confirmed selection and displays the save error.
    }
  }

  async function selectReasoning(next: string) {
    if (disabled) return;
    try {
      await onReasoningChange(normalizeReasoningEffort(next));
    } catch {
      // Failed writes do not become a second local selection.
    }
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setCatalogOpen(false);
          closedFromOutside.current = false;
        }
        setOpen(next);
      }}
    >
      <PopoverTrigger asChild>
        <Button
          ref={triggerRef}
          aria-label={`${t("session.model")}: ${label}`}
          aria-busy={disabled}
          className={cn(
            "pudding-composer-model-picker group/model-picker h-8 shrink rounded-full border-0 bg-transparent py-0 text-xs font-normal text-[var(--composer-control-foreground)] transition-none",
            iconOnly ? "w-8 max-w-8 flex-none justify-center p-0" : "min-w-0 max-w-[14rem] gap-1 pr-1.5 sm:max-w-[16rem]",
            !iconOnly && (selection ? "pl-1" : "pl-2"),
            className,
          )}
          size="sm"
          variant="ghost"
        >
          {selection ? <BrandIcon className="size-5 shrink-0" name={brand} shape="circle" /> : null}
          {iconOnly ? null : (
            <span className={cn("flex h-5 min-w-0 flex-1 items-center gap-1 overflow-hidden", !selection && "text-warning")}>
              <span className="pudding-composer-model-label min-w-0 flex-1 truncate">{modelLabel}</span>
              {hasReasoning ? (
                <span className="pudding-composer-reasoning-detail shrink-0 font-medium text-muted-foreground/80">
                  · {reasoningLabel}
                </span>
              ) : null}
              {disabled ? <Spinner className="size-3 shrink-0" /> : <ChevronDown className="size-3 shrink-0" />}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <AppPopoverContent
        align="center"
        className={cn(
          "max-h-[min(28rem,var(--radix-popover-content-available-height))] max-w-[calc(100vw-1rem)] gap-0 overflow-hidden p-0",
          showCatalog ? profiles.length > 1 ? "w-[19rem]" : "w-[13rem]" : "w-[22rem]",
        )}
        collisionPadding={8}
        side="top"
        sideOffset={8}
        onInteractOutside={(event) => {
          closedFromOutside.current = !triggerRef.current?.contains(event.target as Node);
        }}
        onCloseAutoFocus={(event) => {
          if (!closedFromOutside.current && onAfterClose) {
            event.preventDefault();
            onAfterClose();
          }
        }}
      >
        {showCatalog ? (
          <>
            {hasReasoning ? (
              <div className="flex h-8 shrink-0 items-center border-b border-border/60 px-2.5">
                <button type="button" className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground" onClick={() => setCatalogOpen(false)}>
                  <ChevronLeft className="size-3.5" />
                  {t("common.back")}
                </button>
              </div>
            ) : null}
            <ModelCatalog
              key={modelSelectionKey(value)}
              profiles={profiles}
              selectedProvider={value.provider || ""}
              selectedModel={value.model || ""}
              onSelect={(next) => void selectModel(next)}
              disabled={disabled}
              isLoading={providersQuery.isPending}
            />
          </>
        ) : (
          <div className="min-h-0 overflow-y-auto p-2.5">
            <div className="flex min-h-8 items-center justify-between gap-1.5 border-b border-border/60 pb-1.5">
              <button type="button" className={cn("flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 text-left", appPopoverItemStateClassName)} onClick={() => setCatalogOpen(true)}>
                <BrandIcon className="size-4 shrink-0" name={brand} shape="circle" />
                <span className="min-w-0 truncate text-xs font-medium">{modelLabel}</span>
                <ChevronRight className="size-3.5 shrink-0" />
              </button>
              <button
                type="button"
                aria-label={t("provider.reasoningEffort.reset")}
                title={t("provider.reasoningEffort.reset")}
                disabled={disabled || reasoning.value === reasoning.recommended}
                className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-interactive-hover hover:text-foreground disabled:opacity-35"
                onClick={() => void selectReasoning(reasoning.recommended)}
              >
                <RotateCcw className="size-3.5" />
              </button>
            </div>
            <div className="px-0.5 pt-2.5">
              <SteppedSlider key={modelSelectionKey(value)} options={reasoning.options} value={reasoning.value} onChange={(next) => void selectReasoning(next)} disabled={disabled} />
            </div>
          </div>
        )}
        {error || providersQuery.isError ? (
          <p className="shrink-0 border-t border-border/60 px-2.5 py-2 text-xs text-destructive" role="alert">
            {error || t("provider.loadFailed")}
          </p>
        ) : null}
      </AppPopoverContent>
    </Popover>
  );
}
